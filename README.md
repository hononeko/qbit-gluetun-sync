# qBit-Gluetun Sync Sidecar

[![GitHub Release](https://img.shields.io/github/v/release/hononeko/qbit-gluetun-sync)](https://github.com/hononeko/qbit-gluetun-sync/releases)
[![Build Status](https://img.shields.io/github/actions/workflow/status/hononeko/qbit-gluetun-sync/main.yml)](https://github.com/hononeko/qbit-gluetun-sync/actions/workflows/main.yml)
[![Docker Image](https://img.shields.io/badge/Image-hononeko/qbit--gluetun--sync-blue?logo=docker)](https://github.com/hononeko/qbit-gluetun-sync/pkgs/container/qbit-gluetun-sync)

A lightweight, resilient, and secure sidecar written in Go to synchronize the dynamic forwarded port from Gluetun (ProtonVPN) to qBitTorrent.

## Introduction

When using VPN providers like ProtonVPN with WireGuard through [Gluetun](https://github.com/qdm12/gluetun), the forwarded port changes dynamically. qBitTorrent needs to be updated with this new port to allow incoming peer connections.

Instead of relying on heavy polling shell scripts, this project provides a **Go-based sidecar** that:

- **Resilient File Watching:** Watches the `/tmp/gluetun/forwarded_port` file using `fsnotify` for instant updates. Automatically detects delayed directory creation and handles volume remounts gracefully.
- **Self-Healing Reconciliation:** Runs a periodic reconciliation loop (`SYNC_INTERVAL`) to ensure qBitTorrent stays synchronized even after container reboots or network blips.
- **Flexible Authentication:** Supports standard username/password sessions as well as API Key / Bearer tokens (`QBIT_API_KEY`, `QBIT_API_KEY_FILE`).
- **Secret Management & File Mounts:** Supports secret files (`QBIT_PASS_FILE`, `QBIT_USER_FILE`, `QBIT_API_KEY_FILE`) for Docker Secrets and Kubernetes Secrets integration.
- **Hardened Container Security:** Runs as unprivileged `nonroot` inside Google's minimal `distroless/static-debian12` image (zero C shared library footprint).
- **Graceful Lifecycle Management:** Traps `SIGINT`/`SIGTERM` to perform clean draining and termination.
- **Health Probing:** Exposes `/healthz` for Kubernetes and Docker liveness checks.

## Configuration

The application is configured using Environment Variables:

| Variable                     | Default                       | Description                                                                  |
| :--------------------------- | :---------------------------- | :--------------------------------------------------------------------------- |
| `QBIT_ADDR`                  | `http://localhost:8080`       | The address of your qBitTorrent Web UI.                                      |
| `QBIT_USER`                  | _(empty)_                     | Username for qBitTorrent.                                                    |
| `QBIT_USER_FILE`             | _(empty)_                     | Path to file containing qBitTorrent username (takes precedence over `QBIT_USER`).|
| `QBIT_PASS`                  | _(empty)_                     | Password for qBitTorrent.                                           |
| `QBIT_PASS_FILE`             | _(empty)_                     | Path to file containing qBitTorrent password (takes precedence over `QBIT_PASS`).|
| `QBIT_API_KEY`               | _(empty)_                     | API Key / Bearer token for qBitTorrent or reverse proxy auth.                 |
| `QBIT_API_KEY_FILE`          | _(empty)_                     | Path to file containing API Key (takes precedence over `QBIT_API_KEY`).       |
| `QBIT_API_KEY_HEADER`        | `X-Api-Key`                   | Header name used when sending API Key.                                       |
| `QBIT_INSECURE_SKIP_VERIFY`  | `false`                       | Skip TLS certificate verification for self-signed HTTPS endpoints.           |
| `QBIT_CA_CERT_FILE`          | _(empty)_                     | Path to custom CA certificate PEM file for verifying HTTPS endpoints.       |
| `PORT_FILE`                  | `/tmp/gluetun/forwarded_port` | Path to the port file written by Gluetun.                                    |
| `LISTEN_ADDR`                | _(empty)_                     | Bind IP address for healthcheck server (e.g. `127.0.0.1` or `0.0.0.0`).      |
| `LISTEN_PORT`                | `9090`                        | The port this sidecar listens on for health checks.                          |
| `SYNC_INTERVAL`              | `10m`                         | Periodic reconciliation interval (e.g., `5m`, `10m`, `0` to disable).         |
| `LOG_LEVEL`                  | `info`                        | Log verbosity (`debug`, `info`, `warn`, `error`).                            |

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
    volumes:
      - gluetun_data:/tmp/gluetun:ro
```

### Kubernetes (Sidecar Pattern with Secrets)

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
      volumes:
        - name: gluetun-sync
          emptyDir: {}
        - name: qbit-secret
          secret:
            secretName: qbit-credentials
```

## API Endpoints

- `GET /healthz` - Returns `200 OK` if the sidecar is running.

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
