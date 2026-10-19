# Linux WireGuard Assisted Pilot

## Scope and Release Status

The first implemented slice is operator-managed WireGuard identity and configuration provisioning for a Linux endpoint and Linux gateway. The tool generates persistent local private keys, validates public peer plans, and publishes private `wg-quick` configuration files without overwriting existing identities or configurations. It does not invoke a shell or change network state.

This is a tested provisioning foundation, not a generally available production SD-WAN service. Device enrollment, automatic key rotation, a gateway management API, forwarding/NAT automation, automatic DNS/SNI discovery, fail-closed enforcement, IPv6, tenant isolation, signed releases, and service-level commitments remain outside this slice. IPsec and vendor VPN management are not included.

## Build and Local Identity

Use Go 1.26.9 or a newer patched release. The initial Go 1.26.2 build baseline was upgraded after vulnerability scanning identified reachable standard-library advisories. Re-run vulnerability checks for every release; a passing scan is not a security audit.

On each approved Linux machine, build and install the provisioner using a reviewed source revision:

```sh
CGO_ENABLED=0 go build -o wireguard-provision ./cmd/wireguard-provision
sudo install -o root -g root -m 0755 wireguard-provision /usr/local/sbin/
sudo install -d -o root -g root -m 0700 /var/lib/pulseroute
sudo /usr/local/sbin/wireguard-provision --identity-dir /var/lib/pulseroute/wireguard init
```

Run `init` separately on the endpoint and gateway. It creates `private.key` and `public.key` with no group/other permissions in a new private directory. Share only the public key through an authenticated administrator channel. Never transfer private keys between machines, paste them into support tickets, or generate identities under a source checkout. Do not run this root command against an identity path controlled by an untrusted user.

Repeating `init` fails rather than replacing the identity. An interrupted initialization may leave a partial directory: quarantine it, check whether its public key was already registered, and only then create a fresh identity in a new directory. Do not blindly delete identities during recovery.

## Public Peer Plans

The plan has no private-key field. Substitute the actual gateway public key and approved reachable gateway hostname in the endpoint plan:

```json
{
  "interface": "wg-pulseroute",
  "address": "10.90.0.2/32",
  "listenPort": 51821,
  "peers": [{
    "publicKey": "REPLACE_WITH_GATEWAY_PUBLIC_KEY",
    "allowedIPs": ["10.90.0.1/32", "203.0.113.10/32"],
    "endpoint": "gateway.example.net:51820",
    "keepalive": 25
  }]
}
```

The gateway plan uses its own address and the endpoint's public key. Keep each endpoint's gateway-side allowed IPs restricted to its assigned tunnel address:

```json
{
  "interface": "wg-pulseroute",
  "address": "10.90.0.1/32",
  "listenPort": 51820,
  "peers": [{
    "publicKey": "REPLACE_WITH_ENDPOINT_PUBLIC_KEY",
    "allowedIPs": ["10.90.0.2/32"]
  }]
}
```

The example service address `203.0.113.10` is documentation-only, not a reachable SaaS service. Replace it with a customer-authorized destination. `AllowedIPs` is WireGuard's cryptokey routing and source-address check, not an egress firewall or an application authorization policy.

Unknown JSON fields, invalid keys, duplicate peers, overlapping peer prefixes, default routes, IPv6, unsafe names, self-peering, and prefixes containing the local interface address are rejected. Limit this pilot to narrowly scoped IPv4 destinations. No claim of IPv6 leak prevention is made.

## Generate and Activate

Store a reviewed public plan outside the identity directory, then configure locally:

```sh
sudo /usr/local/sbin/wireguard-provision --identity-dir /var/lib/pulseroute/wireguard --plan /etc/pulseroute/wireguard-plan.json configure
```

The resulting configuration contains a private key. It is created with private permissions using atomic no-replace publication. Never print it in logs or upload it as a CI artifact. The generated file sets `Table = off`, intentionally leaving routing to a separate, approved routing configuration or the existing PulseRoute enforcer.

Before activation, an administrator must review the gateway ingress firewall, per-peer forwarding/egress rules, NAT or return routing, UDP reachability, MTU, monitoring, and management access. Do not enable unrestricted IP forwarding or use this as an open VPN exit node. The provisioner does not perform those reviews or configure those controls.

On a test machine with no existing interface/configuration of that name, install the approved file under `/etc/wireguard/` with owner root and mode `0600`, and start it using `wg-quick`. Keep an independent management session open. With `Table = off`, a successful interface startup does not itself route an application through the tunnel. Validate handshake and explicit routes before enabling any enforcer.

## Verification

Portable checks:

```sh
go test -race ./internal/wireguard
go test ./...
go vet ./...
go run golang.org/x/vuln/cmd/govulncheck@latest ./...
```

On a disposable Linux VM, install WireGuard tools, iproute2, and ping. Install the built provisioner on PATH and run:

```sh
sudo bash deploy/wireguard/integration.sh
```

The script refuses to replace existing test namespaces. It creates two isolated namespaces, connects a veth underlay, generates independent identities and configurations, establishes a real kernel WireGuard handshake, verifies bidirectional packets, and removes a gateway peer to verify traffic stops. It cleans up its namespaces and transient key material on exit. It does not test internet forwarding, automatic enrollment, uptime, endpoint reboot, or performance improvement.

For macOS/Colima, use a Linux ARM64 binary in a Colima-shared path, not macOS `/tmp`:

```sh
CGO_ENABLED=0 GOOS=linux GOARCH=arm64 go build -o "$HOME/pulseroute-wireguard-provision-test" ./cmd/wireguard-provision
docker build -t pulseroute-wireguard-test -f deploy/wireguard/Dockerfile.test deploy/wireguard
docker run --rm --network none --cap-add NET_ADMIN --cap-add SYS_ADMIN --security-opt apparmor=unconfined -v "$HOME/pulseroute-wireguard-provision-test:/usr/local/bin/wireguard-provision:ro" pulseroute-wireguard-test
```

These extra capabilities are exclusively for the disposable namespace test. Do not use them for the normal service containers. The Linux CI workflow repeats unit/race tests, static checks, and the namespace proof. Its first hosted run must still be verified after publishing.

## Recovery, Rotation, and Offboarding

- Back up local key material only using an approved encrypted, access-controlled system; document retention and deletion ownership.
- The tool will not overwrite a configuration. For a planned change, stage a new configuration under a separately reviewed path/interface, validate it, and perform a maintenance-window switch. Retain the previous configuration encrypted for an agreed rollback window.
- Rotate keys by creating a new identity and updating the corresponding authorized public peer on both sides. This is an operator procedure, not automatic enrollment or zero-downtime rotation.
- To revoke a device, remove its public peer from both the running gateway and persisted gateway configuration. A runtime-only `wg set ... peer ... remove` is lost on configuration reload.
- To roll back enforcement, stop the PulseRoute enforcer and run its `uninstall` command before taking the WireGuard interface down. Preserve the organization's existing VPN/security requirements; normal routing may still be a corporate tunnel rather than direct internet.
- Do not delete identity directories as part of routine uninstall or rollback. Offboarding must first revoke peers, disable service startup, and securely handle retained backups.

## Revenue Path: Assisted Evaluation, Not a Production SLA

The earliest honest offer is a fixed-scope, paid engineering evaluation for an organization that authorizes an overlay. It buys installation assistance, measured testing, findings, and a production-gap report. It does not buy a guaranteed improvement, unattended production security, or an availability SLA.

Before quoting, agree in writing on:

1. Customer and technical owner, authorized Linux machines, test gateway, and permitted destinations.
2. A fixed evaluation period and device limit; start with two to five noncritical devices before expanding.
3. A fixed service fee and separately disclosed VM, bandwidth, and support costs. Establish the price through customer discovery; no willingness-to-pay evidence exists in this repository.
4. Baseline and overlay tests using equivalent workloads/time windows, with customer-defined success thresholds.
5. Support hours, stop/rollback authority, maintenance windows, privacy terms, and limits on liability and access.
6. Deliverables: installation record, repeatable test results, support findings, operating-cost estimate, and a go/no-go recommendation.

Production expansion requires independently reviewed security, signed release artifacts, persistent peer revocation, tested recovery/rotation, approved gateway egress controls, monitoring/on-call coverage, and actual customer acceptance. Application optimization additionally requires a validated destination collector and enforcement path. Do not sell dashboard selections or namespace latency as proof of real customer savings.

Revenue milestones are an agreed scope, a signed paid evaluation, successful delivery, and a customer decision to renew or expand. None is implied by the automated tests.