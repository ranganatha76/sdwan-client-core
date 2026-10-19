# PulseRoute Design

## Purpose

PulseRoute is a software-defined WAN control and decision prototype for remote devices. It identifies business and productivity traffic, selects a preferred path for it, and exposes routing status to an end user. The repository separates browser presentation, control-plane state, policy distribution, data-plane decisions, and observability so each can evolve independently.

The Go service layer is safe to run locally: it makes and records routing decisions without intercepting host traffic. On Linux, a separately installed, root-owned native enforcer can consume the same data-plane API to apply policy routing and QoS to WireGuard overlay traffic. macOS and Windows enforcement remain platform-specific delivery work.

## Problem It Solves

Remote users often reach work applications through changing home, mobile, and public networks. A corporate VPN or security service such as Cisco AnyConnect or Zscaler remains responsible for identity, secure access, inspection, and corporate routing, but it does not necessarily make per-application decisions based on current path quality. A collaboration call, source-control operation, and personal streaming session can otherwise compete for the same constrained link or follow a path with excessive latency, jitter, or loss.

PulseRoute is an optional, policy-controlled decision layer alongside that approved security client. It maps recognized work destinations to a priority policy, consumes measurements from an authorized device probe or gateway, selects the highest-quality permitted path, and records the resulting baseline versus optimized performance. Personal and unmatched traffic stays on its ordinary internet route. If policy, telemetry, the agent, or the overlay is unavailable, the system fails open to normal routing rather than blocking access.

PulseRoute is not a VPN replacement or a mechanism to bypass corporate controls. It is only deployable where the organization permits split routing or an approved SD-WAN overlay integrated with its existing VPN/SSE policy.

## Goals

- Prioritize recognized work traffic such as Zoom, Slack, Teams, GitHub, Bitbucket, and productivity applications.
- Keep unrelated household traffic on its ordinary internet route.
- Make routing decisions inspectable through the local UI and APIs.
- Run the service layer unchanged on macOS, Linux/Ubuntu, and Windows.
- Retain performance and bandwidth measurements for seven days.
- Fail open: unavailable policy, agent, or overlay components must not block ordinary internet access.

## Non-Goals

- Go services do not configure OS routes, WireGuard peers, VPN tunnels, eBPF, or Windows Filtering Platform rules.
- The Linux enforcer does not create a WireGuard peer, discover application hostnames, or guarantee upstream DSCP treatment. A deployment-managed WireGuard configuration and an authoritative DNS/SNI flow collector remain required.
- Docker containers do not intercept macOS, Windows, or host Linux traffic.
- Control-plane state is in memory and resets on restart.
- The initial policy feed is static Go data, not yet remotely administered or signed.

## Architecture

```mermaid
flowchart LR
    Browser[Browser] -->|HTTP :8080| UI[UI service]
    UI -->|Internal /api proxy| CP[Control plane]
    CP --> State[In-memory control state]
    Native[Native platform adapter] -->|Hostname or SNI| Agent[Data-plane agent]
    Agent -->|Bearer-authenticated fetch| Policy[Policy service]
    Agent --> Decision[Priority overlay or direct internet]
    Native -->|Apply decision| Tunnel[Managed overlay]
    Native -->|Measurements| Metrics[Metrics server]
    Metrics --> Influx[(InfluxDB: 7 days)]
    Grafana[Grafana] --> Influx
```

Only `ui-service`, Grafana, and the local decision and metrics endpoints are published to host loopback by Compose. The control plane and policy service stay internal to the Compose network.

## Repository Layout

```text
cmd/
  ui-service/                       Browser UI and internal API proxy
  control-plane/                    Dashboard control state and APIs
    policy-service/                 Authenticated policy snapshot provider
  data-plane/agent/                 Flow-decision API for native adapters
  observability/metrics-server/     Metrics composition root and server lifecycle
internal/policy/                    Shared policy snapshot and hostname matching
internal/telemetry/                 Typed models, validation, HTTP API, and InfluxDB writer
deploy/                             Docker image, Compose stack, environment template
native/
  linux/enforcer/                   Root-owned WireGuard route and QoS enforcer
  linux/telemetry/                  Observe-only endpoint health collector
  linux/systemd/                    Fail-open enforcement watchdog unit
  macos/                            Host app and Packet Tunnel Xcode project
CodeWalkThrough.md                  Service and request-flow guide
README.md                           Setup and API reference
```

## Service Responsibilities

### UI Service

`ui-service` serves the dashboard at `GET /` and forwards `/api/*` unchanged to the internal control plane. This lets a browser use simple relative URLs while keeping internal service addresses and control APIs off the host network.

### Control Plane

`control-plane` owns application enablement, selected path, reported path health, and activity history. Its APIs query state, toggle one application, enable all applications, accept a complete timestamped path-measurement cycle, apply the current selection, and pause/resume protection. A device probe or SD-WAN gateway supplies latency, loss, and jitter for every configured path; the control plane scores those values and moves enabled applications to the best path.

### Policy Service

`policy-service` returns a versioned policy snapshot. A policy rule maps an application ID to hostname suffixes. Callers must provide `POLICY_SERVICE_TOKEN` as a bearer token; comparison uses constant-time equality. In production, the policy needs remote administration, signing, version history, and rollback.

### Data-Plane Agent

`dataplane-agent` accepts an observed hostname at `POST /v1/flows/evaluate`. It caches the policy snapshot for 30 seconds. A matched hostname returns `priority-overlay` and high priority; unmatched traffic returns `direct-internet` and standard priority. `DATAPLANE_MODE=observe` reports only. `enforce` permits a platform-native adapter to apply its own routing and QoS behavior; the agent never executes privileged operating-system commands.

### Observability

`cmd/observability/metrics-server` is deliberately a thin composition root. It reads InfluxDB configuration, creates `internal/telemetry.InfluxWriter`, and starts an HTTP server. `internal/telemetry` owns the typed telemetry models, validation, HTTP request decoding, InfluxDB line-protocol serialization, storage readiness checks, and tests. This keeps native collectors and future platform adapters on one contract rather than duplicating JSON schemas or storage code in each executable.

The metrics API accepts five independent measurements: existing application path performance, endpoint quality, endpoint diagnostics, application SLO outcomes, and route-decision explanations. All writes are telemetry only; no metrics endpoint can alter policies, routes, WireGuard, QoS, VPN state, or a native adapter. The `timeseries-db` bucket retains data for `168h`. Compose provisions Grafana's Flux datasource and the `PulseRoute Performance` dashboard, which includes baseline/optimized application performance, endpoint VPN and quality views, SLO before/after/target values, avoided incidents, and candidate-path decision scores.

## API Contracts

### Control Plane

| Method | Path | Result |
| --- | --- | --- |
| `GET` | `/api/state` | Connection, applications, paths, and activity. |
| `POST` | `/api/policies/{id}/toggle` | Switches one application policy. |
| `POST` | `/api/policies/enable-all` | Enables all applications at high priority. |
| `POST` | `/api/paths/measurements` | Accepts a complete timestamped path-measurement cycle and selects the best path. |
| `POST` | `/api/optimize` | Reapplies the most recently reported path measurements. |
| `POST` | `/api/connection` | Pauses or resumes protection. |

### Data Plane

| Method | Path | Request | Result |
| --- | --- | --- |
| `POST` | `/v1/flows/evaluate` | `{"hostname":"api.github.com"}` | Priority-overlay or direct-internet decision. |

A decision includes `action`, `priority`, optional `matchedRule`, `policyVersion`, and `mode`. The native adapter must use a priority overlay only while it is healthy; a failure falls back to direct internet. On Linux, QoS is eligible only when all three conditions hold: `mode` is `enforce`, `action` is `priority-overlay`, and `priority` is `high`.

### Observability

| Method | Path | Request |
| --- | --- | --- |
| `POST` | `/v1/measurements` | Application, baseline/optimized latency and loss, bandwidth bytes. |
| `POST` | `/v1/endpoint-telemetry` | Device/network/VPN state, aggregate path quality, interface byte counters. |
| `POST` | `/v1/endpoint-diagnostics` | Coarse active-interface, route, optional probe, Wi-Fi signal, and host resource state. |
| `POST` | `/v1/application-experience` | Supported application SLO baseline, optimized, target, and avoided-incident state. |
| `POST` | `/v1/route-decisions` | Policy reason/result/rollback state and one selected path among candidate paths. |

All telemetry writes return `202 Accepted` only after InfluxDB returns `204 No Content`. Invalid JSON, invalid tags, invalid quality ranges, unsupported SLO names, or ambiguous route decisions return `400 Bad Request`. Storage or readiness failures return `503 Service Unavailable`; callers own retry behavior. Request bodies are bounded to 16 KiB except route decisions, which allow 64 KiB for up to eight candidate paths.

## Endpoint Experience Telemetry

### Design Intent

Endpoint experience telemetry is an **observe-only** diagnostic plane. It answers: “Was a supported work application measurably better after the control plane chose a path, and what local endpoint condition or candidate comparison explains that result?” It does not inspect application payloads, capture browsing history, or make enforcement decisions. The existing Linux enforcer remains the only native component capable of changing Linux packet state, and it remains separately installed, root-owned, and gated by `DATAPLANE_MODE=enforce`.

The feature is intentionally staged:

1. A collector records local health metadata and optional approved-probe results.
2. The control plane records the candidate paths and policy reason after it accepts a full path-measurement cycle.
3. An approved native or application-specific integration can submit a supported application SLO outcome.
4. Grafana joins the time series by time, device, application, and the non-indexed decision correlation field for investigation.
5. Operators review the evidence before a separate approved enforcement rollout changes traffic behavior.

No application-specific SLO is fabricated by the control plane. Zoom/Teams/Git/Slack integrations must submit real measurements from an authorized source. This preserves the distinction between a routing decision, an endpoint probe, and an application experience claim.

### Component and Data Flow

```mermaid
flowchart LR
  Collector[Linux telemetry collector] -->|EndpointDiagnostics| Metrics[Metrics HTTP API]
  Collector -->|Optional approved probe quality| Metrics
  CP[Control plane] -->|Application performance and route explanations| Metrics
  AppProbe[Approved app or native SLO integration] -->|ApplicationExperience| Metrics
  Metrics -->|Typed validation| Telemetry[internal/telemetry]
  Telemetry -->|Influx line protocol| Influx[(InfluxDB performance bucket)]
  Grafana[Grafana] -->|Flux queries| Influx
  Grafana --> Operator[Support and network operator]
```

`internal/telemetry` is the contract boundary. It defines models, validates data before serialization, provides the `net/http` handler, and persists accepted points through `InfluxWriter`. The metrics command does not own schemas or line protocol. The Linux collector imports the shared endpoint models, serializes them as JSON, and sends them to the loopback metrics API; it does not need InfluxDB credentials.

The control plane emits route explanations only after a complete measurement cycle is validated and a best path is selected. It sends an application performance record and a separate route-decision record for every active application. Metrics delivery runs after the state lock is released and does not delay route selection. Failure to publish telemetry is logged; it cannot reverse, block, or alter the selected control-plane route.

### Measurement Contracts and Influx Schema

| Measurement | Tags | Fields | Producer | Purpose |
| --- | --- | --- | --- | --- |
| `application_performance` | `application` | Baseline/optimized latency and loss, bandwidth bytes | Control plane or approved adapter | Existing path-performance comparison. |
| `endpoint_telemetry` | `device_id`, `network_type`, `vpn_provider` | VPN state, latency, jitter, loss, receive/transmit bytes | Linux collector with optional ICMP probe | Endpoint quality and VPN trend. |
| `endpoint_diagnostics` | `device_id`, `network_type`, `active_interface`, `vpn_provider` | Interface change, default route, optional DNS/TLS, CPU load, memory, optional Wi-Fi dBm | Linux collector | Support triage for endpoint conditions. |
| `application_experience` | `device_id`, `application`, `slo` | Baseline, optimized, target, avoided incident, decision ID | Approved application/native integration | SLO evidence before and after a decision. |
| `route_decision` | `device_id`, `application`, `destination_class`, `policy_id`, `candidate_path` | Latency, jitter, loss, score, selected, decision ID, reason, result, rollback event | Control plane | Explain why one candidate won or a rollback occurred. |

`decision_id`, `reason`, `result`, and `rollback_event` are InfluxDB fields, not tags. Decision IDs are high-cardinality by nature; using them as tags would create an unbounded series count. A single route-decision request creates one correlated Influx point per candidate path, all with the same timestamp and decision ID. Validation requires between one and eight candidates and exactly one `selected: true` value.

Supported SLO names are explicitly allowlisted: `zoom_jitter_ms`, `zoom_loss_pct`, `teams_rtt_ms`, `git_transfer_duration_ms`, `github_transfer_duration_ms`, and `slack_reconnect_count`. The allowlist prevents arbitrary metric dimensions and makes alerts and Grafana units reviewable. Adding an SLO requires a source implementation, validation change, dashboard treatment, and documentation update.

### Linux Collector Operation

`native/linux/telemetry` builds `pulseroute-telemetry`, a Linux-only command with `once` and `watch` modes. It requires a stable managed `--device-id`, an administrator-supplied `--vpn-provider`, and a positive watch interval. Its default sink is `http://127.0.0.1:8083`, so endpoint data remains local to the host’s deployed PulseRoute stack before InfluxDB persistence.

For each collection cycle, the collector reads the IPv4 default interface from `/proc/net/route`, derives only a coarse transport category, checks for a local VPN interface prefix, and reads aggregate load and available memory from procfs. It can optionally read Wi-Fi dBm through `iw` but never reads SSID, BSSID, channel, access-point identity, carrier identity, gateway address, or full route-table contents. Interface byte counters come from `/sys/class/net/<interface>/statistics` and are aggregate counters, not packet or flow samples.

DNS, TLS, and ICMP probes are disabled unless the operator supplies `--dns-host`, `--tls-host`, or `--probe-host`. The configured target names remain local command configuration and are never included in the payload or persisted to InfluxDB. DNS produces only a boolean resolver result; TLS produces only a boolean TLS 1.2-or-newer handshake result; ICMP produces aggregate loss, average latency, and population-standard-deviation jitter from three packets. A fully lost ICMP probe is valid telemetry with `100%` loss. The collector never invokes the enforcer, changes routes, changes QoS, creates a tunnel, captures packets, or parses DNS/SNI application traffic.

### Privacy and Security Boundary

Collected and persisted metadata is intentionally limited to opaque device ID, coarse network type, interface name, VPN provider and availability, aggregate resource state, optional Wi-Fi signal strength, approved-probe booleans and aggregates, route-decision metadata, and approved application/SLO identifiers. The system does **not** collect or persist packet payloads, URLs, hostnames, DNS query history, destination history, SSIDs, BSSIDs, Wi-Fi channels, MAC addresses, gateway addresses, IP route entries, application content, process names, commands, keystrokes, or user activity.

The metrics API validates all tag values against InfluxDB line-protocol separators, bounds string field values, bounds loss to 0 through 100, bounds Wi-Fi signal to plausible negative dBm values, and rejects negative counters or quality values. Native collector subprocesses use explicit arguments without a shell. Metrics endpoint health returns generic failures and never exposes backend credentials. In production, the loopback API must be protected by endpoint identity or mutually authenticated local IPC, and InfluxDB must use a scoped write token rather than the bootstrap administrator token.

### Failure and Rollout Behavior

Telemetry is non-blocking and fail-open. If InfluxDB is unavailable, the metrics API returns `503`; native collectors report the failure and retry on their next watch interval, while the control plane logs the failure and retains its already-selected state. A collector outage, malformed telemetry record, optional-probe failure, or Grafana outage cannot alter an endpoint route or disable ordinary internet access.

Deployment should begin with a small managed cohort in `DATAPLANE_MODE=observe`, with no Linux enforcer installed. Operators should first verify collection rate, device identity quality, privacy review, metric completeness, Influx retention, dashboard usefulness, and support workflow. Only after SLO evidence, controlled gateway health checks, device enrollment, and rollback validation are available should a separately approved cohort move to native enforce mode. The telemetry collector does not become an enforcement prerequisite.

## Security Boundaries

- `POLICY_SERVICE_TOKEN` protects policy snapshot access. It must be a unique runtime secret of at least 32 characters.
- `INFLUXDB_ADMIN_TOKEN` initializes the local database. `GRAFANA_ADMIN_PASSWORD` is a distinct Grafana login secret. Production must use separate, scoped service credentials from a secret manager; neither administrator credential should be used by an application workload.
- Custom Go containers run non-root with a read-only filesystem, `/tmp` tmpfs, dropped Linux capabilities, and `no-new-privileges`.
- UI responses include `X-Content-Type-Options: nosniff` and `X-Frame-Options: DENY`.
- Data-plane and metrics request bodies are size-limited before JSON decoding.
- Endpoint diagnostics are metadata-only; approved probe targets remain local collector configuration and are not persisted.
- Route-decision correlation IDs are storage fields rather than tags to prevent high-cardinality InfluxDB series growth.
- TLS, device identity, policy signing, persistent audit logs, RBAC, and secret rotation remain production requirements.

## Deployment and Operations

```sh
cp deploy/.env.example .env
docker compose -f deploy/docker-compose.yml up --build
```

The UI runs on `http://localhost:8080`, Grafana on `http://localhost:3000`, and local decision and metrics APIs on `8082` and `8083`. Compose health-gates dependent services: UI waits for control-plane liveness; the agent waits for policy readiness; metrics and Grafana wait for InfluxDB. Run `go test ./...` before packaging changes. The agent's `/readyz` endpoint validates that a policy snapshot is available; metrics `/readyz` validates InfluxDB reachability.

## Platform-Native Enforcement

The Go service layer is portable. Only interception and enforcement differ by OS:

- **Linux/Ubuntu**: the implemented `pulseroute-enforcer` uses nftables destination sets, an output-chain firewall mark, and an `ip rule` plus dedicated route table to steer approved IPv4 destinations through a pre-existing WireGuard device. It configures HTB classes on that device. High-priority overlay decisions are entered in a separate set, receive configured DSCP (`cs5` by default), and select HTB class `1:10`; standard overlay traffic uses the default class. `uninstall` deletes only PulseRoute's nftables table, route rule/table, and root qdisc. The `pulseroute-enforcer.service` watchdog checks local agent readiness and WireGuard device existence; on failure it removes those rules so traffic returns to the normal direct route. The root-owned JSON configuration includes the WireGuard device, route table, firewall mark, link rate, high-priority allocation, DSCP, and watchdog interval. It must not contain WireGuard private keys.
- **macOS**: `PulseRoute.xcodeproj` builds an unsigned host app and embedded Packet Tunnel target. The provider reads `remoteAddress`, `ipv4Address`, `ipv4SubnetMask`, and `agentURL` from managed `providerConfiguration`, refuses incomplete values, and consults the local agent for hostname decisions. It requires an Apple Developer Team, Network Extension entitlement, provisioning, user approval, signing, and notarized packaging before installation. Packet-flow classification and gateway forwarding are not yet implemented.
- **Windows**: a signed least-privileged service uses Windows Filtering Platform to associate flows with hostname metadata, calls the agent, and selects overlay or direct routing.

Linux operators must set WireGuard `PersistentKeepalive` and add gateway-path monitoring: interface existence alone does not prove the overlay peer is reachable. The enforcer accepts IPv4 destinations only; IPv6 policy enforcement is not implemented. Every adapter should begin in observe mode, emit measurements, be canaried to managed devices, and preserve direct internet as the failure path.

### Linux Packet and Tunnel Flow

The WireGuard tunnel is a device-to-gateway transport. It is not an application-specific tunnel and it does not terminate at Zoom, Slack, Teams, GitHub, or any other SaaS provider. The laptop encrypts selected inner IP packets and sends the resulting WireGuard UDP packets to the SD-WAN gateway. The gateway decrypts them and forwards the original packets to the SaaS destination using its managed egress path.

```mermaid
flowchart LR
  App[Zoom, Slack, Teams, GitHub] --> Collector[DNS/SNI hostname collector]
  Collector --> Agent[Local data-plane agent]
  Agent -->|enforce + priority-overlay| Enforcer[Linux nftables and ip rule]
  Enforcer -->|Selected inner IPv4 packet| WG[WireGuard interface on laptop]
  WG -->|Encrypted UDP tunnel| Gateway[SD-WAN gateway]
  Gateway -->|Managed egress route| SaaS[SaaS destination]
  SaaS --> Gateway
  Gateway -->|Encrypted return packet| WG
  WG --> App
  Personal[Unmatched personal traffic] --> Direct[Normal direct internet route]
```

#### Policy Decision to Packet State

1. A hostname collector associates a DNS answer, TLS SNI value, or approved application rule with a destination IP address.
2. The collector calls `POST /v1/flows/evaluate` on the local data-plane agent.
3. The agent returns an auditable decision containing `action`, `priority`, `matchedRule`, `policyVersion`, and `mode`.
4. The Linux enforcer changes kernel packet state only when `mode` is `enforce` and `action` is `priority-overlay`.
5. The enforcer adds the approved IPv4 destination to `inet pulseroute priority_destinations` with a ten-minute expiry. Destinations that also have `priority: high` are added to `high_priority_destinations`.

The expiry limits stale SaaS IP mappings. A production collector must refresh mappings before expiry, remove mappings when policy no longer matches, and avoid routing a shared CDN address through the overlay unless policy explicitly authorizes it.

#### Packet Changes on the Laptop

For a destination in `priority_destinations`, the nftables output route chain applies `firewallMark`. The kernel `ip rule` matches that mark and looks up the configured dedicated route table. That route table sends the inner IPv4 packet to the configured WireGuard device, for example `wg-pulseroute`, rather than the laptop's normal default route.

For a destination in `high_priority_destinations`, the QoS output chain additionally sets the configured DSCP class, `cs5` by default, and selects Linux traffic-control class `1:10`. The enforcer installs an HTB root qdisc on the WireGuard device with:

- Class `1:10`: high-priority work traffic, configured minimum `highPriorityRateMbit`, allowed to borrow up to `linkRateMbit`.
- Class `1:30`: default overlay traffic, configured as the lower scheduling priority.

This scheduling decision is local to the laptop's egress queue. The gateway and intermediate networks may rewrite or ignore DSCP, so PulseRoute does not claim end-to-end QoS. The packet's WireGuard outer UDP encapsulation is encrypted between laptop and gateway; the inner packet is decrypted only at the gateway before normal forwarding.

#### Gateway and Return-Path Requirements

The SD-WAN gateway is outside this repository and must be provisioned separately. It needs a WireGuard peer for the laptop, a tunnel address plan, forwarding enabled, firewall policy, and either source NAT or explicit return routes so replies to selected SaaS flows return through the same tunnel. It must have a managed egress path that is measurably better than the laptop's direct ISP path for the selected services; otherwise the extra tunnel hop can increase latency.

The WireGuard configuration owns endpoint address and port, gateway public key, laptop private key, tunnel address, `AllowedIPs`, and `PersistentKeepalive`. Private keys and gateway credentials stay in root-owned deployment configuration or a secrets system and are never written to the PulseRoute repository or Docker environment.

#### Direct Traffic and Fail-Open Behavior

Traffic that has no matching policy destination receives no PulseRoute firewall mark, no dedicated route-table lookup, and no WireGuard QoS class. It follows the operating system's existing normal default route directly to the internet.

The watchdog checks local data-plane-agent readiness and WireGuard device existence. When either check fails, it removes the PulseRoute nftables table, policy rule, dedicated route table entries, and WireGuard qdisc. This makes the normal direct route the remaining path. Interface existence does not prove gateway reachability, so production deployments also need WireGuard peer handshake checks and active gateway-path health probes before retaining overlay routing.

#### Current Enforcement Boundary

The repository implements explicit Linux destination evaluation and application of nftables, policy-routing, DSCP, and HTB state. It does not yet implement the authoritative DNS/SNI collector, WireGuard peer provisioning, gateway forwarding/NAT, IPv6 split tunneling, or active gateway-path measurement. Those components are required before claiming autonomous per-application production steering.

### Scenario Sequences

The following sequences describe the intended production flow. The collector is a required future component; today an operator or an external collector invokes the Linux enforcer explicitly with a hostname and destination IPv4 address.

#### 1. Unmatched Traffic Uses Direct Internet

This scenario covers personal or unrecognized traffic, such as a streaming, gaming, or household destination that does not match a work policy rule.

```mermaid
sequenceDiagram
  participant App as Application
  participant Collector as DNS/SNI collector
  participant Agent as Data-plane agent
  participant Policy as Policy service
  participant Kernel as Linux routing
  participant Internet as Direct internet

  App->>Collector: Resolve or connect to unmatched hostname
  Collector->>Agent: POST /v1/flows/evaluate(hostname)
  Agent->>Policy: GET /v1/policy/snapshot with bearer token
  Policy-->>Agent: Versioned policy snapshot
  Agent-->>Collector: direct-internet, standard, policyVersion
  Note over Collector,Kernel: No destination is added to PulseRoute nftables sets
  App->>Kernel: Send IP packet
  Kernel->>Internet: Use existing default route
```

The packet receives no PulseRoute firewall mark, DSCP value, or traffic-control class. It does not enter WireGuard and the SD-WAN gateway never sees it.

#### 2. High-Priority Work Traffic Uses the Overlay

This scenario covers a matched work hostname when the agent is in `enforce` mode, for example Zoom, Slack, Teams, or GitHub.

```mermaid
sequenceDiagram
  participant App as Work application
  participant Collector as DNS/SNI collector
  participant Agent as Data-plane agent
  participant Policy as Policy service
  participant Enforcer as Linux enforcer
  participant Kernel as nftables, ip rule, tc
  participant WG as WireGuard laptop interface
  participant Gateway as SD-WAN gateway
  participant SaaS as SaaS destination

  App->>Collector: Resolve or connect to matched hostname
  Collector->>Agent: POST /v1/flows/evaluate(hostname)
  Agent->>Policy: Fetch or use cached policy snapshot
  Policy-->>Agent: Matching high-priority rule
  Agent-->>Collector: priority-overlay, high, enforce, matchedRule
  Collector->>Enforcer: Evaluate hostname and destination IPv4
  Enforcer->>Kernel: Add IP to priority_destinations for 10 minutes
  Enforcer->>Kernel: Add IP to high_priority_destinations for 10 minutes
  App->>Kernel: Send inner IPv4 packet to matched destination
  Kernel->>Kernel: Set firewall mark then select policy route table
  Kernel->>Kernel: Set DSCP cs5 then select HTB class 1:10
  Kernel->>WG: Send selected inner packet
  WG->>Gateway: Encrypt and send WireGuard UDP packet
  Gateway->>SaaS: Decrypt and forward through managed egress
  SaaS-->>Gateway: Return packet
  Gateway-->>WG: Encrypt return packet to laptop peer
  WG-->>App: Decrypt and deliver reply
```

The high-priority DSCP and HTB class are local laptop egress controls. The gateway needs a WireGuard peer, forwarding/firewall policy, and NAT or return routing for the symmetric reply path. The gateway's egress path must be better than direct internet for this route to improve application performance.

#### 3. Matched Work Traffic in Observe Mode

Observe mode is used for rollout validation and telemetry. A policy match is returned but must not change kernel packet state.

```mermaid
sequenceDiagram
  participant Collector as DNS/SNI collector
  participant Agent as Data-plane agent
  participant Policy as Policy service
  participant Enforcer as Linux enforcer
  participant Kernel as Linux routing
  participant Internet as Direct internet

  Collector->>Agent: POST /v1/flows/evaluate(work hostname)
  Agent->>Policy: Fetch or use cached policy snapshot
  Policy-->>Agent: Matching work rule
  Agent-->>Collector: priority-overlay, high, observe
  Collector->>Enforcer: Evaluate hostname and destination IPv4
  Enforcer->>Enforcer: Reject route change because mode is observe
  Note over Enforcer,Kernel: No nftables set, route mark, DSCP, or tc update
  Kernel->>Internet: Keep normal default route
```

This validates policy coverage and decision behavior without sending customer traffic through a new overlay path.

#### 4. Policy or Agent Is Unavailable

The adapter must not create new overlay routing state when it cannot obtain a trustworthy policy decision. Existing overlay state is handled by the watchdog scenario below.

```mermaid
sequenceDiagram
  participant Collector as DNS/SNI collector
  participant Agent as Data-plane agent
  participant Policy as Policy service
  participant Enforcer as Linux enforcer
  participant Kernel as Linux routing
  participant Internet as Direct internet

  Collector->>Agent: POST /v1/flows/evaluate(hostname)
  Agent->>Policy: Request fresh policy snapshot
  Policy--x Agent: Timeout, unavailable, or unauthorized
  Agent-->>Collector: HTTP 503 policy unavailable
  Collector->>Enforcer: Do not request destination insertion
  Note over Enforcer,Kernel: No new PulseRoute packet state is created
  Kernel->>Internet: New unmatched flows use normal default route
```

The agent caches a valid policy snapshot for 30 seconds. Within that cache interval it can continue evaluating flows from the last authenticated snapshot; after expiry, unavailable policy produces `503` rather than an untrusted decision.

#### 5. WireGuard or Agent Health Fails Open

The systemd watchdog periodically checks the local agent readiness endpoint and WireGuard device existence. It removes only PulseRoute-owned networking state when either check fails.

```mermaid
sequenceDiagram
  participant Watchdog as pulseroute-enforcer.service
  participant Agent as Data-plane agent /readyz
  participant WG as WireGuard device
  participant Enforcer as Linux enforcer
  participant Kernel as nftables, ip rule, tc
  participant Internet as Direct internet

  loop Every healthCheckIntervalSec
    Watchdog->>Agent: GET /readyz
    Watchdog->>WG: ip link show device
  end
  alt Agent ready and WireGuard device exists
    Watchdog->>Enforcer: Reconcile install state
    Enforcer->>Kernel: Retain PulseRoute nftables, route, and QoS state
  else Agent unavailable or device absent
    Watchdog->>Enforcer: Run uninstall rollback
    Enforcer->>Kernel: Delete PulseRoute nftables table
    Enforcer->>Kernel: Delete firewall-mark ip rule and route table entries
    Enforcer->>Kernel: Delete WireGuard root qdisc
    Kernel->>Internet: Send all subsequent traffic by normal default route
  end
```

This is fail-open, not fail-closed: a control-plane, agent, or local WireGuard-device failure must not block laptop internet access. Gateway reachability needs stronger checks than device existence, such as WireGuard handshake age and active probes, before a production rollout relies on this watchdog alone.

#### 6. Performance Measurement and Dashboarding

The metrics path is independent from packet steering. A native adapter or collector submits measured baseline and optimized values; Grafana displays only the values it receives.

```mermaid
sequenceDiagram
  participant Adapter as Native adapter or collector
  participant Metrics as Metrics server
  participant Influx as InfluxDB performance bucket
  participant Grafana as Grafana dashboard
  participant Operator as Operator

  Adapter->>Metrics: POST /v1/measurements(application, latency, loss, bandwidth)
  Metrics->>Metrics: Validate tag and non-negative fields
  Metrics->>Influx: Write application_performance line protocol
  Influx-->>Metrics: 204 accepted
  Grafana->>Influx: Flux query for selected time range
  Influx-->>Grafana: Latency, loss, bandwidth measurements
  Grafana-->>Operator: Baseline versus optimized panels and improvement trend
```

The dashboard does not prove an overlay benefit by itself. Measurements must compare equivalent workloads and time windows, and the adapter must record both direct-path baseline and overlay-path values before any performance claim is made.

## Production Readiness Roadmap

1. Replace static policy data with signed, versioned, remotely managed policies.
2. Add mutual TLS or per-device credentials for services and gateway.
3. Deliver an authenticated, health-checked WireGuard gateway configuration and an authoritative DNS/SNI flow collector for the Linux enforcer.
4. Build, sign, and distribute the macOS Packet Tunnel and Windows Filtering Platform adapters.
5. Add durable state, audit logs, Grafana provisioning, alerts, and dashboards.
6. Add Linux integration tests for policy refresh, WireGuard loss, fail-open, upgrades, rollback, and DSCP/HTB behavior.
7. Add pinned image digests, vulnerability scans, SBOMs, image signing, and CI release artifacts.
