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

## API

| Method | Path | Purpose |
| --- | --- | --- |
| `GET` | `/api/state` | Read connection, application, path, and activity state. |
| `POST` | `/api/policies/{id}/toggle` | Enable or disable one application priority policy. |
| `POST` | `/api/optimize` | Re-score paths and steer active applications to the best route. |
| `POST` | `/api/connection` | Pause or resume SD-WAN protection. |

## Scope

This prototype simulates policy decisions and path quality in memory. It does not yet intercept operating-system traffic or configure real tunnel, VPN, QoS, or eBPF rules.