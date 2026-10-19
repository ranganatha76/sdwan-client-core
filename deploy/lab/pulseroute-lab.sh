#!/usr/bin/env bash
set -euo pipefail

client_namespace="pulseroute-client"
gateway_namespace="pulseroute-gateway"
saas_namespace="pulseroute-saas"
service_ip="203.0.113.10"

require_root() {
	if [[ ${EUID} -ne 0 ]]; then
		echo "run this lab as root" >&2
		exit 1
	fi
}

namespace() {
	ip netns exec "$1" "${@:2}"
}

remove_namespaces() {
	for name in "$client_namespace" "$gateway_namespace" "$saas_namespace"; do
		ip netns del "$name" 2>/dev/null || true
	done
}

create_link() {
	local client_interface=$1
	local gateway_interface=$2
	local client_address=$3
	local gateway_address=$4

	ip link add "$client_interface" type veth peer name "$gateway_interface"
	ip link set "$client_interface" netns "$client_namespace"
	ip link set "$gateway_interface" netns "$gateway_namespace"
	namespace "$client_namespace" ip addr add "$client_address" dev "$client_interface"
	namespace "$gateway_namespace" ip addr add "$gateway_address" dev "$gateway_interface"
	namespace "$client_namespace" ip link set "$client_interface" up
	namespace "$gateway_namespace" ip link set "$gateway_interface" up
}

set_wireguard_private_key() {
	local namespace_name=$1
	local device=$2
	local private_key=$3
	printf '%s' "$private_key" | namespace "$namespace_name" wg set "$device" private-key /dev/stdin
}

setup_wireguard_path() {
	local device=$1
	local client_interface=$2
	local client_endpoint=$3
	local gateway_endpoint=$4
	local client_tunnel=$5
	local gateway_tunnel=$6

	local client_private_key gateway_private_key client_public_key gateway_public_key
	client_private_key=$(wg genkey)
	gateway_private_key=$(wg genkey)
	client_public_key=$(printf '%s' "$client_private_key" | wg pubkey)
	gateway_public_key=$(printf '%s' "$gateway_private_key" | wg pubkey)

	namespace "$client_namespace" ip link add "$device" type wireguard
	namespace "$gateway_namespace" ip link add "$device" type wireguard
	set_wireguard_private_key "$client_namespace" "$device" "$client_private_key"
	set_wireguard_private_key "$gateway_namespace" "$device" "$gateway_private_key"
	namespace "$client_namespace" ip addr add "$client_tunnel" dev "$device"
	namespace "$gateway_namespace" ip addr add "$gateway_tunnel" dev "$device"
	namespace "$client_namespace" wg set "$device" peer "$gateway_public_key" endpoint "$gateway_endpoint:51820" allowed-ips "$service_ip/32" persistent-keepalive 5
	namespace "$gateway_namespace" wg set "$device" peer "$client_public_key" allowed-ips "${client_tunnel%/*}/32"
	namespace "$client_namespace" ip link set "$device" up
	namespace "$gateway_namespace" ip link set "$device" up

	# The endpoint keeps using its physical veth route while the protected SaaS IP uses WireGuard.
	namespace "$client_namespace" ip route replace "$gateway_endpoint/32" dev "$client_interface"
}

up() {
	remove_namespaces
	modprobe wireguard

	ip netns add "$client_namespace"
	ip netns add "$gateway_namespace"
	ip netns add "$saas_namespace"
	for name in "$client_namespace" "$gateway_namespace" "$saas_namespace"; do
		namespace "$name" ip link set lo up
	done

	create_link pr-primary pr-gateway-primary 192.0.2.2/24 192.0.2.1/24
	create_link pr-backup pr-gateway-backup 192.0.3.2/24 192.0.3.1/24
	ip link add pr-gateway-saas type veth peer name pr-saas-gateway
	ip link set pr-gateway-saas netns "$gateway_namespace"
	ip link set pr-saas-gateway netns "$saas_namespace"
	namespace "$gateway_namespace" ip addr add 198.51.100.1/24 dev pr-gateway-saas
	namespace "$saas_namespace" ip addr add 198.51.100.2/24 dev pr-saas-gateway
	namespace "$gateway_namespace" ip link set pr-gateway-saas up
	namespace "$saas_namespace" ip link set pr-saas-gateway up
	namespace "$saas_namespace" ip addr add "$service_ip/32" dev lo
	namespace "$gateway_namespace" sysctl -qw net.ipv4.ip_forward=1
	namespace "$gateway_namespace" ip route add "$service_ip/32" via 198.51.100.2
	namespace "$saas_namespace" ip route add 10.20.1.0/24 via 198.51.100.1
	namespace "$saas_namespace" ip route add 10.20.2.0/24 via 198.51.100.1

	setup_wireguard_path wg-primary pr-primary 192.0.2.2 192.0.2.1 10.20.1.2/24 10.20.1.1/24
	setup_wireguard_path wg-backup pr-backup 192.0.3.2 192.0.3.1 10.20.2.2/24 10.20.2.1/24
	prefer primary
	echo "lab is ready: protected service is $service_ip"
}

prefer() {
	local path=$1
	local device
	case "$path" in
		primary) device=wg-primary ;;
		backup) device=wg-backup ;;
		*) echo "path must be primary or backup" >&2; exit 2 ;;
	esac
	namespace "$client_namespace" ip route replace "$service_ip/32" dev "$device"
	echo "selected $path WireGuard path"
}

impair_primary() {
	namespace "$client_namespace" tc qdisc replace dev pr-primary root netem delay 120ms 25ms loss 8% rate 8mbit
	echo "primary path impaired: 120ms delay, 25ms jitter, 8% loss, 8mbit cap"
}

restore_primary() {
	namespace "$client_namespace" tc qdisc del dev pr-primary root 2>/dev/null || true
	echo "primary path restored"
}

measure() {
	namespace "$client_namespace" ping -c 10 -W 1 "$service_ip"
}

require_root
case "${1:-}" in
	up) up ;;
	down) remove_namespaces ;;
	prefer) prefer "${2:-}" ;;
	impair-primary) impair_primary ;;
	restore-primary) restore_primary ;;
	measure) measure ;;
	*)
		echo "usage: $0 {up|down|prefer primary|backup|impair-primary|restore-primary|measure}" >&2
		exit 2
		;;
esac