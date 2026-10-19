
The dashboard starts in a pending-measurement state and refuses optimization until it accepts one complete path-probe cycle. It does not publish route-performance telemetry from the former fixture values, so Grafana improvement panels represent accepted probe data rather than dashboard defaults.

For a real Linux endpoint, use the [native real-time path probe](native/README.md#real-time-path-probe) to measure approved targets on each configured underlay and continuously feed the control plane. It provides live path scoring only; automatic application flow discovery and production tunnel provisioning remain separate components.

For organizations that authorize OpenConnect, the [Linux OpenConnect adapter](native/README.md#openconnect-compatible-vpn-adapter) can manage an OpenConnect-compatible Cisco AnyConnect or Palo Alto GlobalProtect tunnel without storing authentication secrets. It does not support Zscaler or control a vendor-installed VPN client.
# PulseRoute SD-WAN Client

PulseRoute is a lightweight Go prototype for a laptop SD-WAN client control plane. It presents routing health, prioritizes business and productivity applications, and lets a user simulate route optimization from a local web dashboard.

## What This App Is For

PulseRoute is an application-aware routing and observability layer for remote work. It is designed to complement, not replace, an organization-approved Cisco AnyConnect, Zscaler, or other corporate VPN/SSE client.

An enterprise VPN provides identity, secure access, inspection, and corporate routing. PulseRoute adds a decision layer that can identify approved work applications, compare approved network paths, and choose a preferred path for latency-sensitive traffic such as Zoom, Teams, Slack, GitHub, Bitbucket, and productivity SaaS applications. Unmatched personal traffic remains on its ordinary internet route.

This is useful only where corporate network policy permits the required split-routing or approved overlay integration. It must never bypass a full-tunnel, inspection, or security policy imposed by the corporate VPN. In a production deployment, an authorized native adapter and gateway enforce the selected path; the local Go services alone do not alter host traffic.

### Corporate VPN Deployment Models

PulseRoute supports two organization-controlled deployment models; the organization, not an end user, selects and provisions one.

1. **Existing VPN/SSE integration:** Cisco AnyConnect, Zscaler, GlobalProtect, or a comparable corporate client remains the tunnel owner. PulseRoute observes its health and may apply only local QoS or routing actions explicitly supported by the organization's VPN and split-tunnel policy. It does not create an alternate path around that VPN.
2. **Approved PulseRoute overlay:** the organization provisions WireGuard or IPsec device-to-gateway tunnels, gateway egress, identity, and policy. PulseRoute can steer eligible work traffic through that overlay. This overlay is itself an organization-managed VPN and must meet the same security, inspection, logging, and access-control requirements as the existing remote-access service.

When a corporate VPN is full-tunnel, PulseRoute must retain that VPN as the required work-traffic path. `direct-internet` is only valid for traffic and deployment modes that the organization's policy explicitly permits to use local breakout.

## Prerequisites

- Go 1.26.9 or newer patched release

## Run locally

From the repository root:

```sh
go run ./cmd/control-plane
```

Open http://localhost:8080 in a browser.

The dashboard starts with high-priority policies enabled for collaboration, engineering, customer-operations, and productivity applications, including Zoom, Slack, Microsoft Teams, GitHub, Bitbucket, Microsoft 365, Google Workspace, Jira, Asana, Notion, and Figma.

## Test

```sh
go test ./...
```

## Zero-Cost Performance Lab

For the first Linux managed-tunnel slice, see the [WireGuard provisioning and assisted-pilot runbook](deploy/wireguard/README.md). It covers persistent local identities, validated peer configurations, a real Linux kernel test, rollback, and commercial acceptance gates. It is not an automatic enrollment service or a production-readiness certification.

Use the [Linux namespace lab](deploy/lab/README.md) on a Linux VM to create two ephemeral WireGuard paths, impair the primary path with `tc netem`, select the healthy backup path, and capture baseline/degraded/recovered latency and loss. It uses only Linux networking tools and temporary in-memory keys; it does not require a paid VPN or SSE provider.

## Run with Docker or Colima

Docker Desktop or a running Colima profile is required. Copy the environment template, replace every sample secret with a distinct value of at least 32 characters, then start the stack:

```sh
cp deploy/.env.example .env
docker compose -f deploy/docker-compose.yml up --build
```

Or use the production-stack launcher, which validates distinct secrets, builds and starts every Compose service, waits for health checks, and verifies the local HTTP endpoints:

```sh
umask 077
{
	printf 'POLICY_SERVICE_TOKEN='
	openssl rand -hex 32
	printf 'INFLUXDB_ADMIN_TOKEN='
	openssl rand -hex 32
	printf 'GRAFANA_ADMIN_PASSWORD='
	openssl rand -hex 32
} > .env

chmod 600 .env
python3 start_up.py
```

This creates three distinct 256-bit local secrets without printing them. `.env` is ignored by Git and must never be committed. Use `python3 start_up.py --skip-build` to reuse existing images. The launcher starts the browser UI, control plane, policy service, data-plane decision agent, InfluxDB, metrics service, and Grafana. It does not install or start the platform-native Linux enforcer or macOS Packet Tunnel because those require separately provisioned operating-system privileges, WireGuard gateway settings, and Apple entitlement/signing.

The UI service is available at http://localhost:8080, Grafana at http://localhost:3000, the data-plane decision API on loopback at http://localhost:8082, and metrics ingestion on loopback at http://localhost:8083. InfluxDB retains performance data for seven days. Grafana automatically provisions the `PulseRoute Performance` dashboard and its InfluxDB data source. The Compose stack runs every custom Go service as non-root with a read-only filesystem and no Linux capabilities. Compose health checks require the control plane, policy snapshot, and InfluxDB before dependent services are treated as ready.

## Telemetry Ingestion Authentication

Before a managed pilot, set `METRICS_AUTH_TOKEN` in the local environment file to a distinct random value of at least 32 characters. Compose passes it to the metrics server and control-plane publisher. Supply the same environment variable to the Linux telemetry collector through its approved service configuration, not a command-line argument. Other telemetry clients must send `Authorization: Bearer <credential>` for all five ingestion POST endpoints.

An absent or incorrect credential returns `401` when authentication is enabled. Health/readiness checks remain accessible. Empty configuration preserves legacy local-development behavior; it is not a production security setting. Keep host ports bound to loopback, and use TLS or an authenticated encrypted transport for any remote ingestion. Bearer authentication alone does not encrypt telemetry or establish device/tenant identity.

Rotate the credential by coordinating publishers and the metrics server in a maintenance window; the current implementation accepts only one credential at a time. To roll back this opt-in feature in a controlled local environment, clear the setting on the server and publishers and recreate those services. Do not disable authentication on an exposed service.

## UI Service

`ui-service` is the browser-facing microservice. It serves the PulseRoute dashboard and forwards only `/api/*` browser requests to the internal control plane. The control plane is not exposed on a host port in the Compose deployment.

The UI does not replace the API: it uses the API to read state and perform actions such as enabling all policies or optimizing routes. Keeping this contract lets the browser UI, a future desktop app, and automated clients use the same tested control-plane behavior without exposing internal services directly to the browser.

For Colima:

```sh
colima start
docker compose -f deploy/docker-compose.yml up --build
```

To stop the stack:

```sh
docker compose -f deploy/docker-compose.yml down
```

## API

| Method | Path | Purpose |
| --- | --- | --- |
| `GET` | `/api/state` | Read connection, application, path, and activity state. |
| `POST` | `/api/policies/{id}/toggle` | Enable or disable one application priority policy. |
| `POST` | `/api/policies/enable-all` | Enable every configured application policy at high priority. |
| `POST` | `/api/paths/measurements` | Submit a full timestamped probe cycle; score paths and steer active applications to the best route. |
| `POST` | `/api/optimize` | Apply the most recently reported path measurements to active applications. |
| `POST` | `/api/connection` | Pause or resume SD-WAN protection. |
| `POST` | `http://localhost:8082/v1/flows/evaluate` | Decide whether an observed hostname should use the priority overlay or normal internet. |
| `POST` | `http://localhost:8083/v1/endpoint-telemetry` | Store observe-only endpoint network and VPN health for Grafana. |
| `POST` | `http://localhost:8083/v1/endpoint-diagnostics` | Store observe-only interface, reachability, and resource diagnostics. |
| `POST` | `http://localhost:8083/v1/application-experience` | Store supported application SLO outcomes before and after a route decision. |
| `POST` | `http://localhost:8083/v1/route-decisions` | Store a policy decision, its reason, rollback outcome, and candidate paths. |

### Dashboard control API

The first four endpoints are used by the local dashboard at http://localhost:8080. They manage the client state shown in the UI:

- `GET /api/state` returns protected applications, available routes, current connection state, and recent routing activity.
- `POST /api/policies/{id}/toggle` enables or disables one configured application. An enabled work application is assigned `High` priority; a disabled one uses normal direct internet.
- `POST /api/policies/enable-all` restores every configured application to enabled, `High` priority protection on the selected path. Use it to recover from individual policy changes.
- `POST /api/paths/measurements` accepts one measurement for every configured path: `name`, `latency` in milliseconds, `lossPercent`, `jitter` in milliseconds, and an RFC 3339 `measuredAt`. The control plane ranks the fresh measurements and assigns protected applications to the best path. Native device probes or the SD-WAN gateway must supply these values; a container cannot independently measure separate Fiber, LTE, and direct-network links.
- `POST /api/optimize` reapplies the most recently reported measurements.
- `POST /api/connection` pauses or resumes the dashboard's SD-WAN protection state.

These dashboard APIs model the control-plane experience. They are not operating-system packet-routing commands.

### Endpoint Telemetry API

`POST http://localhost:8083/v1/endpoint-telemetry` accepts an observe-only endpoint health sample for Grafana. It stores a device identifier, network type, VPN provider/connection state, latency, jitter, loss, and byte counters. It does not accept packet payloads, destination hostnames, DNS queries, or application content.

```sh
curl -X POST http://localhost:8083/v1/endpoint-telemetry \
	-H 'Content-Type: application/json' \
	--data '{"deviceId":"laptop-001","networkType":"wifi","vpnProvider":"cisco-anyconnect","vpnConnected":true,"latencyMs":32,"jitterMs":4,"lossPct":0.1,"receiveBytes":1048576,"transmitBytes":524288}'
```

The `PulseRoute Performance` Grafana dashboard includes endpoint VPN-state and latency/jitter panels after samples are submitted.

### Application Experience and Explainability

PulseRoute accepts the following application SLOs: `zoom_jitter_ms`, `zoom_loss_pct`, `teams_rtt_ms`, `git_transfer_duration_ms`, `github_transfer_duration_ms`, and `slack_reconnect_count`. An application experience record compares baseline, optimized, and target values and marks whether a decision avoided an SLO incident. A related route-decision record stores the application, destination class, policy ID, selected and rejected candidate paths, reason, result, and rollback event. It never stores hostnames or packet content.

Grafana now shows the before/after/target SLO values, avoided-incident count, and candidate-path scores. Decision IDs correlate the two record types as fields instead of InfluxDB tags to avoid unbounded series cardinality.

### Flow-decision API

`POST http://localhost:8082/v1/flows/evaluate` is the contract between a future native traffic collector and the Go data-plane decision agent. The native collector identifies a destination hostname from DNS, TLS SNI, or a platform application rule, then asks the agent how that flow should be handled.

For example, this asks how to handle a connection to GitHub:

```sh
curl -X POST http://localhost:8082/v1/flows/evaluate \
	-H 'Content-Type: application/json' \
	--data '{"hostname":"api.github.com"}'
```

GitHub is included in the work-traffic policy, so the response is similar to:

```json
{
	"action": "priority-overlay",
	"priority": "high",
	"matchedRule": "github",
	"policyVersion": "2026-10-19.1",
	"mode": "observe"
}
```

`priority-overlay` means the future native adapter should send that flow through the managed, health-checked tunnel. `matchedRule` identifies the policy rule responsible for the decision, and `policyVersion` makes the decision auditable.

For a hostname that does not match a work rule, the response is similar to:

```json
{
	"action": "direct-internet",
	"priority": "standard",
	"policyVersion": "2026-10-19.1",
	"mode": "observe"
}
```

`direct-internet` means household and unrelated traffic keeps its normal route. This avoids consuming tunnel capacity or degrading personal traffic.

## Split-Tunnel Routing and Performance

PulseRoute uses split tunneling: the WireGuard tunnel runs between the managed device and the SD-WAN gateway, not between an application and the gateway. Recognized work traffic, such as Zoom, Slack, Teams, GitHub, Bitbucket, and configured productivity services, is eligible for the tunnel. Other applications keep using the device's normal direct internet route.

```text
Matched work hostname
	-> data-plane policy decision
	-> Linux nftables route mark and QoS class
	-> WireGuard tunnel to the SD-WAN gateway
	-> gateway route to the SaaS service

Unmatched hostname
	-> direct normal internet route
```

On Linux, an enforce-mode `priority-overlay` decision adds an IPv4 destination to the WireGuard policy-route set. A `priority: high` decision also receives the configured DSCP value (`cs5` by default) and preferred HTB scheduling on the WireGuard interface. This protects collaboration and productivity traffic from local uplink congestion; it does not guarantee bandwidth or preserve DSCP beyond the local network.

The overlay improves performance only when the SD-WAN gateway has a better onward path to the SaaS service, such as lower congestion, better peering, or a private backbone. Without that better path, encryption and the extra gateway hop can make traffic slightly slower.

The Linux enforcer can apply an explicit hostname and destination-IP evaluation. Automatic application routing still requires an authoritative DNS/SNI flow collector to observe hostname-to-IP mappings and refresh them safely as SaaS destinations change.

### Observe and enforce modes

`DATAPLANE_MODE=observe` is the default. The agent records and returns decisions but makes no operating-system routing changes. It is the appropriate mode for local validation and rollout telemetry.

`DATAPLANE_MODE=enforce` still requires a platform-native adapter to apply the decision. On macOS, that is a signed Network Extension Packet Tunnel; on Linux/Ubuntu, the supplied nftables/WireGuard enforcer applies policy routing and QoS after an authoritative hostname collector invokes it; on Windows, it is a Windows Filtering Platform adapter. The current Linux adapter fails open to normal routing if the tunnel or local agent becomes unhealthy. A production tunnel-only policy needs a separately approved, fail-closed native implementation with explicit bootstrap and emergency-access allowlists; it is not implemented by this prototype.

## Scope

The containerized agent is a policy-decision service, not a host packet interceptor. It does not configure a WireGuard peer, gateway, or host routes. The separately installed Linux enforcer configures only PulseRoute-owned nftables, policy-routing, and QoS state; it does not create the WireGuard tunnel itself.

## Native Enforcement Prerequisites

Actual traffic interception is platform-specific and must not be enabled solely by starting Docker containers.

- **Linux**: requires a root-installed component, an approved WireGuard peer configuration, and an eBPF or nftables classifier that sends hostname or SNI-derived flow metadata to the agent. The component must preserve a direct-route fail-open path.
- **macOS**: requires a signed application bundle, an Apple Developer Team with the Network Extension entitlement, and a Packet Tunnel provider. Docker and Colima cannot intercept macOS host traffic.

Do not store WireGuard private keys, gateway credentials, or Apple signing credentials in `.env`, source control, or this repository.

## Platform Support

The Go control plane, policy service, data-plane decision agent, metrics service, InfluxDB, and Grafana run unchanged on macOS, Linux (including Ubuntu), and Windows through Docker Desktop or Colima where available. Actual packet interception is implemented as a small native adapter that calls the same local agent API: Network Extension on macOS, eBPF or nftables with WireGuard on Linux/Ubuntu, and Windows Filtering Platform on Windows.