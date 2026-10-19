# PulseRoute SD-WAN Client

PulseRoute is a lightweight Go prototype for a laptop SD-WAN client control plane. It presents routing health, prioritizes business and productivity applications, and lets a user simulate route optimization from a local web dashboard.

## Prerequisites

- Go 1.26 or newer

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

## Run with Docker or Colima

Docker Desktop or a running Colima profile is required. Copy the environment template, replace every sample secret with a distinct value of at least 32 characters, then start the stack:

```sh
cp deploy/.env.example .env
docker compose -f deploy/docker-compose.yml up --build
```

The UI service is available at http://localhost:8080, Grafana at http://localhost:3000, the data-plane decision API on loopback at http://localhost:8082, and metrics ingestion on loopback at http://localhost:8083. InfluxDB retains performance data for seven days. Grafana automatically provisions the `PulseRoute Performance` dashboard and its InfluxDB data source. The Compose stack runs every custom Go service as non-root with a read-only filesystem and no Linux capabilities. Compose health checks require the control plane, policy snapshot, and InfluxDB before dependent services are treated as ready.

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
| `POST` | `/api/optimize` | Re-score paths and steer active applications to the best route. |
| `POST` | `/api/connection` | Pause or resume SD-WAN protection. |
| `POST` | `http://localhost:8082/v1/flows/evaluate` | Decide whether an observed hostname should use the priority overlay or normal internet. |

### Dashboard control API

The first four endpoints are used by the local dashboard at http://localhost:8080. They manage the client state shown in the UI:

- `GET /api/state` returns protected applications, available routes, current connection state, and recent routing activity.
- `POST /api/policies/{id}/toggle` enables or disables one configured application. An enabled work application is assigned `High` priority; a disabled one uses normal direct internet.
- `POST /api/policies/enable-all` restores every configured application to enabled, `High` priority protection on the selected path. Use it to recover from individual policy changes.
- `POST /api/optimize` re-scores the configured paths using the prototype's latency and jitter measurements, then assigns protected applications to the best path.
- `POST /api/connection` pauses or resumes the dashboard's SD-WAN protection state.

These dashboard APIs model the control-plane experience. They are not operating-system packet-routing commands.

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

`DATAPLANE_MODE=enforce` still requires a platform-native adapter to apply the decision. On macOS, that is a signed Network Extension Packet Tunnel; on Linux/Ubuntu, the supplied nftables/WireGuard enforcer applies policy routing and QoS after an authoritative hostname collector invokes it; on Windows, it is a Windows Filtering Platform adapter. That component must fail open to direct internet if the tunnel or local agent becomes unhealthy.

## Scope

The containerized agent is a policy-decision service, not a host packet interceptor. It does not configure a WireGuard peer, gateway, or host routes. The separately installed Linux enforcer configures only PulseRoute-owned nftables, policy-routing, and QoS state; it does not create the WireGuard tunnel itself.

## Native Enforcement Prerequisites

Actual traffic interception is platform-specific and must not be enabled solely by starting Docker containers.

- **Linux**: requires a root-installed component, an approved WireGuard peer configuration, and an eBPF or nftables classifier that sends hostname or SNI-derived flow metadata to the agent. The component must preserve a direct-route fail-open path.
- **macOS**: requires a signed application bundle, an Apple Developer Team with the Network Extension entitlement, and a Packet Tunnel provider. Docker and Colima cannot intercept macOS host traffic.

Do not store WireGuard private keys, gateway credentials, or Apple signing credentials in `.env`, source control, or this repository.

## Platform Support

The Go control plane, policy service, data-plane decision agent, metrics service, InfluxDB, and Grafana run unchanged on macOS, Linux (including Ubuntu), and Windows through Docker Desktop or Colima where available. Actual packet interception is implemented as a small native adapter that calls the same local agent API: Network Extension on macOS, eBPF or nftables with WireGuard on Linux/Ubuntu, and Windows Filtering Platform on Windows.