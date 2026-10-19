#!/usr/bin/env python3
"""Start and verify the supported PulseRoute container stack."""

import argparse
import os
import subprocess
import sys
import time
from pathlib import Path
from urllib.error import URLError
from urllib.request import urlopen


REPOSITORY_ROOT = Path(__file__).resolve().parent
COMPOSE_FILE = REPOSITORY_ROOT / "deploy" / "docker-compose.yml"
ENV_FILE = REPOSITORY_ROOT / ".env"
REQUIRED_SECRETS = (
    "POLICY_SERVICE_TOKEN",
    "INFLUXDB_ADMIN_TOKEN",
    "GRAFANA_ADMIN_PASSWORD",
)
HEALTH_ENDPOINTS = (
    ("PulseRoute UI", "http://127.0.0.1:8080/healthz"),
    ("Data-plane agent", "http://127.0.0.1:8082/readyz"),
    ("Metrics server", "http://127.0.0.1:8083/readyz"),
    ("Grafana", "http://127.0.0.1:3000/api/health"),
)


def parse_env_file(path: Path) -> dict:
    """Read simple KEY=VALUE entries without printing secret values."""
    values = {}
    for raw_line in path.read_text(encoding="utf-8").splitlines():
        line = raw_line.strip()
        if not line or line.startswith("#") or "=" not in line:
            continue
        key, value = line.split("=", 1)
        values[key.strip()] = value.strip().strip('"').strip("'")
    return values


def validate_environment() -> None:
    """Require a local environment file with unique non-placeholder secrets."""
    if not ENV_FILE.is_file():
        raise RuntimeError(".env is missing; copy deploy/.env.example and set three unique secrets")
    values = parse_env_file(ENV_FILE)
    secrets = []
    for key in REQUIRED_SECRETS:
        value = values.get(key, "")
        if len(value) < 32 or "replace-this" in value:
            raise RuntimeError(".env must set a unique value of at least 32 characters for " + key)
        secrets.append(value)
    if len(set(secrets)) != len(secrets):
        raise RuntimeError("POLICY_SERVICE_TOKEN, INFLUXDB_ADMIN_TOKEN, and GRAFANA_ADMIN_PASSWORD must be distinct")


def compose_command(*arguments: str) -> list:
    """Build a Compose command pinned to this repository and its local environment file."""
    return [
        "docker",
        "compose",
        "--project-directory",
        str(REPOSITORY_ROOT),
        "--env-file",
        str(ENV_FILE),
        "-f",
        str(COMPOSE_FILE),
        *arguments,
    ]


def run_compose(*arguments: str) -> None:
    """Run a Compose command and preserve its output for operational diagnosis."""
    subprocess.run(compose_command(*arguments), check=True, cwd=REPOSITORY_ROOT)


def wait_for_endpoint(name: str, endpoint: str, timeout_seconds: int) -> None:
    """Wait for an HTTP endpoint to return a success status after Compose is ready."""
    deadline = time.monotonic() + timeout_seconds
    while time.monotonic() < deadline:
        try:
            with urlopen(endpoint, timeout=3) as response:
                if 200 <= response.status < 300:
                    return
        except URLError:
            pass
        time.sleep(2)
    raise RuntimeError(name + " did not become healthy at " + endpoint)


def main() -> int:
    """Start the stack, wait for Compose and HTTP readiness, and print local endpoints."""
    parser = argparse.ArgumentParser(description="Start the PulseRoute production container stack")
    parser.add_argument("--timeout", type=int, default=180, help="maximum seconds to wait for Compose and HTTP health")
    parser.add_argument("--skip-build", action="store_true", help="reuse existing images instead of rebuilding Go services")
    arguments = parser.parse_args()
    if arguments.timeout < 30:
        parser.error("--timeout must be at least 30 seconds")

    validate_environment()
    run_compose("config", "--quiet")
    up_arguments = ["up", "--detach", "--wait", "--wait-timeout", str(arguments.timeout)]
    if not arguments.skip_build:
        up_arguments.append("--build")
    run_compose(*up_arguments)
    for name, endpoint in HEALTH_ENDPOINTS:
        wait_for_endpoint(name, endpoint, arguments.timeout)

    print("PulseRoute stack is ready")
    print("UI:      http://localhost:8080")
    print("Grafana: http://localhost:3000")
    print("Agent:   http://localhost:8082/healthz")
    print("Metrics: http://localhost:8083/healthz")
    return 0


if __name__ == "__main__":
    try:
        sys.exit(main())
    except (RuntimeError, subprocess.CalledProcessError) as error:
        print("start_up.py: " + str(error), file=sys.stderr)
        sys.exit(1)