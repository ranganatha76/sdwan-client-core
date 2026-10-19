# Native Enforcement Adapters

The services in `cmd/` decide policy but do not modify an operating system's packet routes. This directory holds the native integration points that call the local data-plane agent and apply its result.

## Linux and Ubuntu

`linux/enforcer` is a Linux-only Go command. It assumes a separately managed WireGuard device already exists. It can:

1. Install an nftables destination set, output-chain packet mark, policy rule, and dedicated route table.
2. Call `http://127.0.0.1:8082/v1/flows/evaluate` for a hostname and destination IP.
3. Add priority destinations to the nftables set for ten minutes only when the agent returns `priority-overlay`.

Build it on Linux:

```sh
go build -o pulseroute-enforcer ./native/linux/enforcer
sudo install -o root -g root -m 0755 pulseroute-enforcer /usr/local/sbin/pulseroute-enforcer
sudo install -o root -g root -m 0600 native/linux/enforcer/enforcer.example.json /etc/pulseroute/enforcer.json
```

Create and bring up the WireGuard device with a separately provisioned `/etc/wireguard/wg-pulseroute.conf`. Do not put a private key in this repository. After verifying the device and the configuration values, install the policy-routing primitives:

```sh
sudo /usr/local/sbin/pulseroute-enforcer install
sudo /usr/local/sbin/pulseroute-enforcer --hostname api.github.com --destination-ip 140.82.112.6 evaluate
```

`evaluate` changes the nftables destination set only when the agent returns both `action: priority-overlay` and `mode: enforce`; it is inert in observe mode. Roll back routing explicitly with:

```sh
sudo /usr/local/sbin/pulseroute-enforcer uninstall
```

The adapter must be operated with a health watchdog that runs the rollback when the WireGuard tunnel or local agent is unavailable. That watchdog and the authoritative DNS/SNI flow collector are not implemented yet.

## macOS

`macos/PulseRouteNetworkExtension/PacketTunnelProvider.swift` is source for an Xcode Packet Tunnel Network Extension target. The host application supplies hostname metadata through `handleAppMessage`; the extension calls the local Go agent and returns the decision to the host app.

To build and install it, create a signed macOS app and Packet Tunnel target in Xcode, add this provider source, configure an Apple Developer Team with the Network Extension entitlement, and provision real gateway settings from device policy. Docker and Colima cannot grant the entitlement or intercept Mac host traffic.

The current extension source proves the adapter-to-agent contract. Full per-flow forwarding needs a deployed gateway protocol and packet flow classification logic, which must be implemented and tested under the approved entitlement.

## Windows

Windows requires a signed Windows Filtering Platform service or callout driver that performs the same operation: classify a hostname-associated flow, post it to `localhost:8082`, then select a managed overlay or direct route. It is intentionally not scaffolded here because WFP enforcement requires a signing and installer pipeline before it can be safely tested.