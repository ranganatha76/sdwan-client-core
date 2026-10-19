# PulseRoute Code Walkthrough

This document follows the code in execution order. It explains what each service does and how browser actions and future network flows move through the system.

## Executables

| Command | Role | Default port |
| --- | --- | --- |
| `cmd/ui-service` | Browser UI and API proxy | `8080` |
| `cmd/control-plane` | User policy and path state | `8080` internally |
| `cmd/control-plane/policy-service` | Authenticated policy snapshots | `8081` |
| `cmd/data-plane/agent` | Hostname-to-route decisions | `8082` |
| `cmd/observability/metrics-server` | InfluxDB performance ingestion | `8083` |

The two `8080` listeners do not conflict under Compose because they run in separate containers. Only the UI service maps its port to the host.

## Shared Policy: `internal/policy`

`DefaultSnapshot` creates the current policy version and the application rules. A `Rule` has an ID, display name, priority, and hostname suffixes. `Snapshot.Match` normalizes a hostname and returns a rule when the hostname equals a configured domain or is one of its subdomains.

For example, `api.github.com` matches the `github.com` suffix and receives the GitHub high-priority rule. `example.net` has no match. The policy package test protects subdomain matching behavior.

## UI Service: `cmd/ui-service/main.go`

`main` reads `CONTROL_PLANE_URL`, defaulting to the Compose address `http://control-plane:8080`. It builds an `httputil.ReverseProxy` and registers three routes:

- `/api/` proxies browser API requests to the control plane.
- `/healthz` supports container health checks.
- `/` serves the embedded dashboard HTML, CSS, and JavaScript.

The dashboard calls relative URLs such as `/api/state`. Therefore the browser talks only to the UI service, and the UI forwards the request through the internal network. This avoids browser CORS configuration and keeps the internal control plane unexposed.

## Control Plane: `cmd/control-plane/main.go`

`main` creates a `server` from `initialState` and registers control APIs. The server wraps its `clientState` in a `sync.RWMutex`: state reads use a read lock while policy updates use an exclusive lock.

`initialState` supplies default applications, simulated paths, and initial activity. It is deliberately in memory, so it is reset by a restart.

### State request

1. UI JavaScript requests `GET /api/state`.
2. `getState` locks for reading and JSON-encodes `clientState`.
3. The UI renders applications, selected paths, and activity.

### Application policy request

`togglePolicy` extracts the application `id` from `/api/policies/{id}/toggle`, finds the matching app, and flips `Active`.

- When enabled, it sets `High` priority, `Protected` health, and the selected route.
- When disabled, it sets `Standard` priority and `Direct internet`.

It records the change through `addActivity`, which maintains a bounded recent-event list. `enableAllPolicies` applies the enabled state to every known application.

### Path optimization request

`optimize` is a prototype probe simulation. It alters each path's latency and jitter, computes a score, marks the highest score selected, and assigns active applications to that route. A production path replaces this synthetic data with native probe telemetry.

`toggleConnection` changes the overall UI protection state. `writeJSON` applies the JSON content type and serializes API responses. The control-plane tests confirm default high-priority apps and bulk enable behavior.

## Policy Service: `cmd/control-plane/policy-service/main.go`

At startup, `main` refuses to run without a sufficiently long `POLICY_SERVICE_TOKEN`. It registers a health route and `GET /v1/policy/snapshot`.

`snapshot` reads `Authorization: Bearer <token>`, compares it in constant time, and returns `policy.DefaultSnapshot` only for an authorized caller. It sets a 30-second private cache header. `securityHeaders` adds `X-Content-Type-Options: nosniff` to every response.

## Data-Plane Agent: `cmd/data-plane/agent/main.go`

`main` loads the policy token, agent mode, and policy service URL. Valid modes are `observe` and `enforce`; the latter is a signal to a native adapter, not host routing by itself.

`evaluate` reads a JSON `hostname`, rejects a missing value, asks `getSnapshot` for policy, and calls `Snapshot.Match`:

- A matching work hostname returns `priority-overlay`, high priority, and the rule ID.
- An unmatched hostname returns `direct-internet` and standard priority.

`getSnapshot` serializes updates using a mutex, serves the cached snapshot until it expires after 30 seconds, then retrieves a new bearer-authenticated snapshot. If policy refresh fails and no valid snapshot is available, it returns `503`. A production native adapter treats that error as direct-route fail-open.

## Metrics Service: `cmd/observability/metrics-server/main.go`

`main` requires the InfluxDB connection settings and starts the metrics ingestion endpoint. `writeMeasurement` limits the request body, decodes a `measurement`, validates it, then calls `influxWriter.write`.

`measurement.valid` rejects empty or unsafe application names and negative values. `write` converts data to InfluxDB line protocol, adds the database token, and posts it to the configured bucket. The `bandwidth_bytes` field supports utilization charts; baseline/optimized latency and loss enable improvement calculations.

## Compose Deployment: `deploy/docker-compose.yml`

The shared multi-stage `deploy/Dockerfile` builds each Go executable. Compose launches:

- `ui-service`: host loopback `8080`; depends on control plane.
- `control-plane`: internal-only.
- `policy-service`: internal-only and token-protected.
- `dataplane-agent`: local loopback `8082` for a native adapter.
- `metrics-server`: local loopback `8083`.
- `timeseries-db`: InfluxDB named volume and seven-day retention.
- `grafana`: host loopback `3000` and a named configuration volume.

Custom Go services run non-root, have read-only filesystems, use temporary `/tmp`, drop Linux capabilities, and set `no-new-privileges`. The local `.env` file supplies secrets and must not be committed.

## End-to-End Flows

### Browser action

1. A user opens `http://localhost:8080`.
2. UI service returns the dashboard page.
3. Dashboard requests `/api/state`.
4. UI service proxies the request to control plane.
5. Control plane returns state; the UI renders it.

### Future native traffic flow

1. The OS adapter sees a connection to `api.github.com`.
2. It posts the hostname to the data-plane agent on `localhost:8082`.
3. The agent reuses or refreshes the policy snapshot.
4. GitHub matches and produces `priority-overlay`.
5. The native adapter sends that one flow through a healthy overlay and records measurements through metrics server.
6. Grafana reads the seven-day InfluxDB history for performance and bandwidth reports.

## Local Validation

```sh
go test ./...
docker compose -f deploy/docker-compose.yml config --quiet
docker compose -f deploy/docker-compose.yml up --build
```

Container services intentionally do not alter host routes. Real traffic enforcement requires the native adapters and gateway credentials described in `DESIGN.md`.