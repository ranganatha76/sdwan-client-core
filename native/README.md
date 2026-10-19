# Native Enforcement Adapters

The services in `cmd/` decide policy but do not modify an operating system's packet routes. This directory holds the native integration points that call the local data-plane agent and apply its result.

## Linux and Ubuntu

`linux/enforcer` is a Linux-only Go command. It assumes a separately managed WireGuard device already exists. It can:

1. Install nftables destination sets, an output-chain route mark, a DSCP class, HTB traffic-control classes on the WireGuard device, a policy rule, and a dedicated route table.
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

`evaluate` changes the routing destination set only when the agent returns both `action: priority-overlay` and `mode: enforce`; it is inert in observe mode. A `priority: high` result also enters the high-priority destination set, sets the configured DSCP (`cs5` by default), and selects HTB class `1:10` on the WireGuard device. This is an egress scheduling preference, not a guarantee of end-to-end bandwidth or upstream DSCP preservation. Roll back routing and QoS explicitly with:

```sh
sudo /usr/local/sbin/pulseroute-enforcer uninstall
```

Install the watchdog after changing the `wg-pulseroute` name in the provided unit if your device uses a different name:

```sh
sudo install -o root -g root -m 0644 native/linux/systemd/pulseroute-enforcer.service /etc/systemd/system/
sudo systemctl daemon-reload
sudo systemctl enable --now pulseroute-enforcer
```

The watchdog runs the rollback when the WireGuard device or local agent is unavailable. It checks device availability only; configure WireGuard `PersistentKeepalive` and external gateway health monitoring for stronger peer-path detection. An authoritative DNS/SNI flow collector is not implemented yet.

## macOS

`macos/PulseRoute.xcodeproj` contains a host application and Packet Tunnel Network Extension target. The host application supplies hostname metadata through `handleAppMessage`; the extension calls the local Go agent and returns the decision to the host app.

Build the unsigned source locally after installing Xcode:

```sh
xcodebuild -project native/macos/PulseRoute.xcodeproj -scheme PulseRoute -configuration Debug -sdk macosx CODE_SIGNING_ALLOWED=NO build
```

For a deployable build, open the project in Xcode, assign a unique bundle ID in place of `com.pulseroute.client`, select an enrolled Apple Developer Team for both targets, and enable the Packet Tunnel Network Extension capability for that app identifier. Xcode must then generate an Apple Development identity and matching provisioning profiles. Docker and Colima cannot grant the entitlement or intercept Mac host traffic.

The MDM or host app must provide `remoteAddress`, `ipv4Address`, `ipv4SubnetMask`, and `agentURL` in `NEPacketTunnelProviderProtocol.providerConfiguration`. The extension rejects incomplete settings. `agentURL` must use HTTPS unless it is the loopback address `127.0.0.1` for the local decision agent.

The current extension source proves the adapter-to-agent contract. Full per-flow forwarding needs a deployed gateway protocol and packet flow classification logic, which must be implemented and tested under the approved entitlement.

## Windows

Windows requires a signed Windows Filtering Platform service or callout driver that performs the same operation: classify a hostname-associated flow, post it to `localhost:8082`, then select a managed overlay or direct route. It is intentionally not scaffolded here because WFP enforcement requires a signing and installer pipeline before it can be safely tested.