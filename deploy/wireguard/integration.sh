#!/usr/bin/env bash
set -euo pipefail

client_namespace="pr-proof-client"
gateway_namespace="pr-proof-gateway"
for namespace_name in "$client_namespace" "$gateway_namespace"; do
	if ip netns list | grep -Eq "^${namespace_name}([[:space:]]|$)"; then
		echo "refusing to replace an existing test namespace" >&2
		exit 1
	fi
done
workspace=$(mktemp -d)

cleanup() {
	ip netns del "$client_namespace" 2>/dev/null || true
	ip netns del "$gateway_namespace" 2>/dev/null || true
	rm -rf "$workspace"
}
trap cleanup EXIT

wireguard-provision --identity-dir "$workspace/client" init
wireguard-provision --identity-dir "$workspace/gateway" init
client_key=$(<"$workspace/client/public.key")
gateway_key=$(<"$workspace/gateway/public.key")

printf '{"interface":"wg-client","address":"10.90.0.2/32","listenPort":51821,"peers":[{"publicKey":"%s","allowedIPs":["10.90.0.1/32"],"endpoint":"192.0.2.1:51820","keepalive":5}]}\n' "$gateway_key" > "$workspace/client.json"
printf '{"interface":"wg-gateway","address":"10.90.0.1/32","listenPort":51820,"peers":[{"publicKey":"%s","allowedIPs":["10.90.0.2/32"],"endpoint":"192.0.2.2:51821","keepalive":5}]}\n' "$client_key" > "$workspace/gateway.json"

wireguard-provision --identity-dir "$workspace/client" --plan "$workspace/client.json" configure
wireguard-provision --identity-dir "$workspace/gateway" --plan "$workspace/gateway.json" configure

if wireguard-provision --identity-dir "$workspace/client" init >/dev/null 2>&1; then
	echo "FAIL: identity overwrite allowed" >&2
	exit 1
fi
if wireguard-provision --identity-dir "$workspace/client" --plan "$workspace/client.json" configure >/dev/null 2>&1; then
	echo "FAIL: configuration overwrite allowed" >&2
	exit 1
fi

ip netns add "$client_namespace"
ip netns add "$gateway_namespace"
ip link add pr-client type veth peer name pr-gateway
ip link set pr-client netns "$client_namespace"
ip link set pr-gateway netns "$gateway_namespace"
ip -n "$client_namespace" addr add 192.0.2.2/30 dev pr-client
ip -n "$gateway_namespace" addr add 192.0.2.1/30 dev pr-gateway
ip -n "$client_namespace" link set pr-client up
ip -n "$gateway_namespace" link set pr-gateway up
ip -n "$client_namespace" link set lo up
ip -n "$gateway_namespace" link set lo up
ip -n "$client_namespace" link add wg-client type wireguard
ip -n "$gateway_namespace" link add wg-gateway type wireguard

wg-quick strip "$workspace/client/wg-client.conf" | ip netns exec "$client_namespace" wg setconf wg-client /dev/stdin
wg-quick strip "$workspace/gateway/wg-gateway.conf" | ip netns exec "$gateway_namespace" wg setconf wg-gateway /dev/stdin
ip -n "$client_namespace" addr add 10.90.0.2/32 dev wg-client
ip -n "$gateway_namespace" addr add 10.90.0.1/32 dev wg-gateway
ip -n "$client_namespace" link set wg-client up
ip -n "$gateway_namespace" link set wg-gateway up
ip -n "$client_namespace" route add 10.90.0.1/32 dev wg-client
ip -n "$gateway_namespace" route add 10.90.0.2/32 dev wg-gateway

ip netns exec "$client_namespace" ping -c 3 -W 2 10.90.0.1
ip netns exec "$gateway_namespace" ping -c 3 -W 2 10.90.0.2
ip netns exec "$client_namespace" wg show wg-client latest-handshakes | awk '$2 > 0 { found = 1 } END { exit !found }'

ip netns exec "$gateway_namespace" wg set wg-gateway peer "$client_key" remove
if ip netns exec "$client_namespace" ping -c 1 -W 1 10.90.0.1 >/dev/null 2>&1; then
	echo "FAIL: revoked peer still exchanges traffic" >&2
	exit 1
fi
echo "PASS: persistent configuration, bidirectional WireGuard traffic, handshake, overwrite protection, and kernel peer removal"