# qBit-Gluetun Sync Sidecar

[![GitHub Release](https://img.shields.io/github/v/release/hononeko/qbit-gluetun-sync)](https://github.com/hononeko/qbit-gluetun-sync/releases)
[![Build Status](https://img.shields.io/github/actions/workflow/status/hononeko/qbit-gluetun-sync/main.yml)](https://github.com/hononeko/qbit-gluetun-sync/actions/workflows/main.yml)
[![Docker Image](https://img.shields.io/badge/Image-hononeko/qbit--gluetun--sync-blue?logo=docker)](https://github.com/hononeko/qbit-gluetun-sync/pkgs/container/qbit-gluetun-sync)

A lightweight, resilient, and secure sidecar written in Go to synchronize the dynamic forwarded port from Gluetun (ProtonVPN) to qBitTorrent.

## Introduction

When using VPN providers like ProtonVPN with WireGuard through [Gluetun](https://github.com/qdm12/gluetun), the forwarded port changes dynamically. qBitTorrent needs to be updated with this new port to allow incoming peer connections.

Instead of relying on heavy polling shell scripts, this project provides a **Go-based sidecar** that:

- **Resilient File Watching & REST API:** Watches the `/tmp/gluetun/forwarded_port` file using `fsnotify` and optionally polls Gluetun's REST API (`GLUETUN_ADDR`) as a dynamic fallback or alternative source.
- **Self-Healing Reconciliation:** Runs a periodic reconciliation loop (`SYNC_INTERVAL`) to ensure qBitTorrent stays synchronized even after container reboots or network blips.
- **Flexible Authentication:** Supports standard username/password sessions as well as API Key / Bearer tokens (`QBIT_API_KEY`, `QBIT_API_KEY_FILE`).
- **Secret Management & File Mounts:** Supports secret files (`QBIT_PASS_FILE`, `QBIT_USER_FILE`, `QBIT_API_KEY_FILE`) for Docker Secrets and Kubernetes Secrets integration.
- **Webhook Notifications:** Dispatches HTTP POST notifications to Discord or generic webhook endpoints (`WEBHOOK_URL`) on forwarded port updates.
- **Manual Sync Webhook:** Exposes `POST /sync` to allow external tools and CI pipelines to trigger immediate synchronization.
- **Hardened Container Security:** Runs as unprivileged `nonroot` inside Google's minimal `distroless/static-debian12` image (zero C shared library footprint).
- **Observability & Probes:** Exposes `/healthz` (liveness), `/readyz` (readiness), `/status` (JSON diagnostics), and `/metrics` (Prometheus exposition).
- **Distroless Native Healthcheck:** Built-in CLI `-healthcheck` flag for native container health checking without `curl` or `sh`.

## Configuration

The application is configured using Environment Variables, grouped by logical domain.

### Core Settings (Required)

| Variable | Default | Description |
| :--- | :--- | :--- |
| `QBIT_ADDR` | `http://localhost:8080` | URL of the target qBitTorrent Web UI. |
| `PORT_FILE` | `/tmp/gluetun/forwarded_port` | Path to the forwarded port file written by Gluetun. |

### Authentication (Optional)

| Variable | Default | Description |
| :--- | :--- | :--- |
| `QBIT_USER` | _(empty)_ | Username for qBitTorrent Web UI. |
| `QBIT_USER_FILE` | _(empty)_ | File path to read username from (takes precedence over `QBIT_USER`). |
| `QBIT_PASS` | _(empty)_ | Password for qBitTorrent Web UI. |
| `QBIT_PASS_FILE` | _(empty)_ | File path to read password from (takes precedence over `QBIT_PASS`). |
| `QBIT_API_KEY` | _(empty)_ | API Key / Bearer token for qBitTorrent (bypasses cookie login). |
| `QBIT_API_KEY_FILE` | _(empty)_ | File path to read API Key from (takes precedence over `QBIT_API_KEY`). |
| `QBIT_API_KEY_HEADER` | `X-Api-Key` | Custom HTTP header name used when sending API Key. |

### Integrations & Webhooks (Optional)

| Variable | Default | Description |
| :--- | :--- | :--- |
| `GLUETUN_ADDR` | _(empty)_ | URL to Gluetun Control Server REST API (e.g. `http://gluetun:8000`). |
| `WEBHOOK_URL` | _(empty)_ | Webhook URL (Discord or generic JSON) to notify when forwarded port changes. |
| `QBIT_DISABLE_UPNP` | `false` | When `true`, explicitly disables UPnP (`upnp: false`) in qBitTorrent preferences. |

### TLS & Network Security (Optional)

| Variable | Default | Description |
| :--- | :--- | :--- |
| `QBIT_INSECURE_SKIP_VERIFY` | `false` | Skip TLS certificate verification for self-signed HTTPS endpoints. |
| `QBIT_CA_CERT_FILE` | _(empty)_ | File path to custom root CA certificate PEM for HTTPS validation. |

### Server & Health Probing (Optional)

| Variable | Default | Description |
| :--- | :--- | :--- |
| `LISTEN_PORT` | `9090` | HTTP port for health check endpoints. |
| `LISTEN_ADDR` | _(empty / all interfaces)_ | Host/IP to bind HTTP server (e.g. `127.0.0.1` for local pod binding). |

### Sync Engine & Logging (Optional)

| Variable | Default | Description |
| :--- | :--- | :--- |
| `SYNC_INTERVAL` | `10m` | Periodic reconciliation interval (e.g. `5m`, `10m`, `0` to disable). |
| `LOG_LEVEL` | `info` | Log verbosity level (`debug`, `info`, `warn`, `error`). |
| `LOG_FORMAT` | `text` | Log format: `text` or `json`. |

## API & Health Endpoints

| Endpoint | Method | Description |
| :--- | :--- | :--- |
| `/healthz` | `GET` | **Liveness probe.** Returns `200 OK` as long as the process is alive. |
| `/readyz` | `GET` | **Readiness probe.** Returns `200 OK` when initial sync is complete and qBitTorrent is reachable, `503 Service Unavailable` otherwise. |
| `/status` | `GET` | **Diagnostics.** Returns a detailed JSON payload with sync status, current port, timestamps, and error counters. |
| `/metrics` | `GET` | **Prometheus metrics.** Exposes `qbit_gluetun_sync_current_port`, `qbit_gluetun_sync_operations_total`, and `qbit_gluetun_sync_qbittorrent_reachable`. |
| `/sync` | `POST` / `GET` | **Manual Sync Trigger.** Forces an immediate source check (file and Gluetun API) and syncs the port to qBitTorrent. |

## Usage

### Docker Compose

In a compose file, run `qbit-gluetun-sync` in the same network namespace as qBitTorrent, sharing the `/tmp/gluetun` volume.

```yaml
version: "3.8"

services:
  gluetun:
    image: qmcgaw/gluetun
    cap_add:
      - NET_ADMIN
    environment:
      - VPN_SERVICE_PROVIDER=protonvpn
      - VPN_TYPE=wireguard
      - WIREGUARD_PRIVATE_KEY=your_private_key
      - SERVER_COUNTRIES=Netherlands
      - VPN_PORT_FORWARDING=on
      - VPN_PORT_FORWARDING_STATUS_FILE=/tmp/gluetun/forwarded_port
      - FIREWALL_INPUT_PORTS=8080,9090
    volumes:
      - gluetun_data:/tmp/gluetun
    ports:
      - "9090:9090" # Expose the Sync Proxy port, NOT the raw qBitTorrent port

  qbittorrent:
    image: lscr.io/linuxserver/qbittorrent:latest
    network_mode: "service:gluetun"
    environment:
      - WEBUI_PORT=8080
    volumes:
      - qbit_config:/config
      - qbit_downloads:/downloads

  sync-sidecar:
    image: ghcr.io/hononeko/qbit-gluetun-sync:latest
    network_mode: "service:gluetun"
    environment:
      - QBIT_ADDR=http://localhost:8080
      - PORT_FILE=/tmp/gluetun/forwarded_port
      - LISTEN_PORT=9090
      - SYNC_INTERVAL=10m
      - LOG_FORMAT=json
      # Optional: Webhook alert on port changes
      - WEBHOOK_URL=https://discord.com/api/webhooks/xxx/yyy
    volumes:
      - gluetun_data:/tmp/gluetun:ro
    healthcheck:
      test: ["CMD", "/usr/local/bin/qbit-gluetun-sync", "-healthcheck"]
      interval: 30s
      timeout: 5s
      retries: 3
```

### Kubernetes (Sidecar Pattern with Probes and Secrets)

If deploying in Kubernetes, deploy inside the same Pod as qBitTorrent with an `emptyDir` volume shared with Gluetun and mounted Secrets.

```yaml
apiVersion: apps/v1
kind: Deployment
metadata:
  name: qbittorrent
spec:
  template:
    spec:
      containers:
        # 1. Gluetun Container
        - name: gluetun
          image: qmcgaw/gluetun
          env:
            - name: VPN_PORT_FORWARDING_STATUS_FILE
              value: /tmp/gluetun/forwarded_port
            - name: FIREWALL_INPUT_PORTS
              value: "8080,9090"
          volumeMounts:
            - name: gluetun-sync
              mountPath: /tmp/gluetun

        # 2. qBitTorrent Container
        - name: qbittorrent
          image: lscr.io/linuxserver/qbittorrent:latest

        # 3. Sync Sidecar
        - name: qbit-sync
          image: ghcr.io/hononeko/qbit-gluetun-sync:latest
          env:
            - name: QBIT_ADDR
              value: "http://localhost:8080"
            - name: QBIT_PASS_FILE
              value: "/etc/secrets/qbit/password"
            - name: LOG_FORMAT
              value: "json"
          ports:
            - containerPort: 9090
              name: healthz
          volumeMounts:
            - name: gluetun-sync
              mountPath: /tmp/gluetun
              readOnly: true
            - name: qbit-secret
              mountPath: /etc/secrets/qbit
              readOnly: true
          livenessProbe:
            httpGet:
              path: /healthz
              port: 9090
          readinessProbe:
            httpGet:
              path: /readyz
              port: 9090
      volumes:
        - name: gluetun-sync
          emptyDir: {}
        - name: qbit-secret
          secret:
            secretName: qbit-credentials
```

## Development

All pre-commit gates and guidelines are documented in [AGENTS.md](AGENTS.md).

```bash
git clone https://github.com/hononeko/qbit-gluetun-sync.git
cd qbit-gluetun-sync
gofmt -s -w .
go test -v -race ./...
golangci-lint run
go build -o qbit-gluetun-sync ./cmd/sync
```
