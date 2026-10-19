# PulseRoute Design

## Purpose

PulseRoute is a software-defined WAN control and decision prototype for remote devices. It identifies business and productivity traffic, selects a preferred path for it, and exposes routing status to an end user. The repository separates browser presentation, control-plane state, policy distribution, data-plane decisions, and observability so each can evolve independently.

The Go service layer is safe to run locally: it makes and records routing decisions without intercepting host traffic. On Linux, a separately installed, root-owned native enforcer can consume the same data-plane API to apply policy routing and QoS to WireGuard overlay traffic. macOS and Windows enforcement remain platform-specific delivery work.

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
  observability/metrics-server/     Performance and bandwidth ingestion
internal/policy/                    Shared policy snapshot and hostname matching
deploy/                             Docker image, Compose stack, environment template
native/
  linux/enforcer/                   Root-owned WireGuard route and QoS enforcer
  linux/systemd/                    Fail-open enforcement watchdog unit
  macos/                            Host app and Packet Tunnel Xcode project
CodeWalkThrough.md                  Service and request-flow guide
README.md                           Setup and API reference
```

## Service Responsibilities

### UI Service

`ui-service` serves the dashboard at `GET /` and forwards `/api/*` unchanged to the internal control plane. This lets a browser use simple relative URLs while keeping internal service addresses and control APIs off the host network.

### Control Plane

`control-plane` owns application enablement, selected path, simulated path health, and activity history. Its APIs query state, toggle one application, enable all applications, optimize paths, and pause/resume protection. Optimization currently changes synthetic latency and jitter values, selects the highest score, and moves enabled applications to that path.

### Policy Service

`policy-service` returns a versioned policy snapshot. A policy rule maps an application ID to hostname suffixes. Callers must provide `POLICY_SERVICE_TOKEN` as a bearer token; comparison uses constant-time equality. In production, the policy needs remote administration, signing, version history, and rollback.

### Data-Plane Agent

`dataplane-agent` accepts an observed hostname at `POST /v1/flows/evaluate`. It caches the policy snapshot for 30 seconds. A matched hostname returns `priority-overlay` and high priority; unmatched traffic returns `direct-internet` and standard priority. `DATAPLANE_MODE=observe` reports only. `enforce` permits a platform-native adapter to apply its own routing and QoS behavior; the agent never executes privileged operating-system commands.

### Observability

`metrics-server` receives per-application baseline and optimized latency/loss plus bandwidth bytes. It writes `application_performance` points to InfluxDB. The `timeseries-db` bucket retains data for `168h`. Compose provisions Grafana's Flux datasource and a `PulseRoute Performance` dashboard with baseline versus optimized latency/loss, bandwidth by application, and latency-improvement panels.

## API Contracts

### Control Plane

| Method | Path | Result |
| --- | --- | --- |
| `GET` | `/api/state` | Connection, applications, paths, and activity. |
| `POST` | `/api/policies/{id}/toggle` | Switches one application policy. |
| `POST` | `/api/policies/enable-all` | Enables all applications at high priority. |
| `POST` | `/api/optimize` | Selects the best simulated path. |
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

## Security Boundaries

- `POLICY_SERVICE_TOKEN` protects policy snapshot access. It must be a unique runtime secret of at least 32 characters.
- `INFLUXDB_ADMIN_TOKEN` initializes the local database. `GRAFANA_ADMIN_PASSWORD` is a distinct Grafana login secret. Production must use separate, scoped service credentials from a secret manager; neither administrator credential should be used by an application workload.
- Custom Go containers run non-root with a read-only filesystem, `/tmp` tmpfs, dropped Linux capabilities, and `no-new-privileges`.
- UI responses include `X-Content-Type-Options: nosniff` and `X-Frame-Options: DENY`.
- Data-plane and metrics request bodies are size-limited before JSON decoding.
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

## Production Readiness Roadmap

1. Replace static policy data with signed, versioned, remotely managed policies.
2. Add mutual TLS or per-device credentials for services and gateway.
3. Deliver an authenticated, health-checked WireGuard gateway configuration and an authoritative DNS/SNI flow collector for the Linux enforcer.
4. Build, sign, and distribute the macOS Packet Tunnel and Windows Filtering Platform adapters.
5. Add durable state, audit logs, Grafana provisioning, alerts, and dashboards.
6. Add Linux integration tests for policy refresh, WireGuard loss, fail-open, upgrades, rollback, and DSCP/HTB behavior.
7. Add pinned image digests, vulnerability scans, SBOMs, image signing, and CI release artifacts.
