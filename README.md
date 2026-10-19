# PulseRoute SD-WAN Client

PulseRoute is a lightweight Go prototype for a laptop SD-WAN client control plane. It presents routing health, prioritizes business and productivity applications, and lets a user simulate route optimization from a local web dashboard.

## Prerequisites

- Go 1.26 or newer

## Run locally

From the repository root:

```sh
go run .
```

Open http://localhost:8080 in a browser.

The dashboard starts with high-priority policies enabled for collaboration, engineering, customer-operations, and productivity applications, including Zoom, Slack, Microsoft Teams, GitHub, Bitbucket, Microsoft 365, Google Workspace, Jira, Asana, Notion, and Figma.

## Test

```sh
go test ./...
```

## Run with Docker or Colima

Docker Desktop or a running Colima profile is required. Copy the environment template, replace the sample secret with a unique value of at least 32 characters, then start the stack:

```sh
cp .env.example .env
docker compose up --build
```

The control plane is available at http://localhost:8080. The data-plane decision API is available only on loopback at http://localhost:8082. The Compose stack runs every service as non-root with a read-only filesystem and no Linux capabilities.

For Colima:

```sh
colima start
docker compose up --build
```

To stop the stack:

```sh
docker compose down
```

## API

| Method | Path | Purpose |
| --- | --- | --- |
| `GET` | `/api/state` | Read connection, application, path, and activity state. |
| `POST` | `/api/policies/{id}/toggle` | Enable or disable one application priority policy. |
| `POST` | `/api/optimize` | Re-score paths and steer active applications to the best route. |
| `POST` | `/api/connection` | Pause or resume SD-WAN protection. |
| `POST` | `http://localhost:8082/v1/flows/evaluate` | Evaluate an observed hostname against the centrally served work-traffic policy. |

For example:

```sh
curl -X POST http://localhost:8082/v1/flows/evaluate \
	-H 'Content-Type: application/json' \
	--data '{"hostname":"api.github.com"}'
```

The response contains either `priority-overlay` for matched work traffic or `direct-internet` for unmatched traffic. `DATAPLANE_MODE=observe` is the default; it returns decisions without changing host routing.

## Scope

The containerized agent is a policy-decision service, not a host packet interceptor. It does not yet configure a real tunnel, VPN, QoS, or eBPF rules.

## Native Enforcement Prerequisites

Actual traffic interception is platform-specific and must not be enabled solely by starting Docker containers.

- **Linux**: requires a root-installed component, an approved WireGuard peer configuration, and an eBPF or nftables classifier that sends hostname or SNI-derived flow metadata to the agent. The component must preserve a direct-route fail-open path.
- **macOS**: requires a signed application bundle, an Apple Developer Team with the Network Extension entitlement, and a Packet Tunnel provider. Docker and Colima cannot intercept macOS host traffic.

Do not store WireGuard private keys, gateway credentials, or Apple signing credentials in `.env`, source control, or this repository.