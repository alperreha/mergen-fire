# mergen-fire

[![Go](https://img.shields.io/badge/go-1.24+-00ADD8?logo=go)](https://go.dev/)
[![Firecracker](https://img.shields.io/badge/firecracker-microVM-orange)](https://firecracker-microvm.github.io/)
[![Platform](https://img.shields.io/badge/platform-linux%20host-lightgrey)](#requirements)

Minimal **Firecracker VM manager** in Go.

`mergen-fire` provides a small control-plane to manage microVM lifecycle on a single host, with `systemd` as process supervisor and file-based state.

## Why this project

- Run Firecracker VMs with simple REST endpoints.
- Keep VM processes alive even if manager process crashes (`systemd` owns VM services).
- Use deterministic filesystem layout for config, runtime, and data.
- Prepare hook points for future integrations (Envoy xDS, Consul, webhooks).

## Current scope (v0.1)

- Lifecycle endpoints:
  - `POST /v1/vms`
  - `POST /v1/vms/:id/start`
  - `POST /v1/vms/:id/stop`
  - `DELETE /v1/vms/:id`
  - `GET /v1/vms/:id`
  - `GET /v1/vms`
- File store:
  - `vm.json` (Firecracker config)
  - `meta.json` (manager metadata)
  - `hooks.json` (optional VM hooks)
  - `env` (systemd env file)
- `systemd` service model: `fc@<uuid>.service`
- Basic port publish + sequential IP allocation
- Per-VM lock file to prevent lifecycle race conditions
- Structured logging with configurable level/format
- Best-effort hooks:
  - `onCreate`
  - `onDelete`
  - `onStart`
  - `onStop`

## Architecture

- **Control plane:** Go HTTP API server (`cmd/manager`)
- **Data plane:** `systemd` + Firecracker/Jailer processes
- **State source:** filesystem under `MGR_CONFIG_ROOT`, `MGR_RUN_ROOT`, `MGR_DATA_ROOT`

## Repository layout

- `cmd/manager`: API entrypoint
- `internal/api`: REST handlers
- `internal/manager`: orchestration/service layer
- `internal/store`: filesystem persistence
- `internal/systemd`: `systemctl` wrapper
- `internal/firecracker`: VM config rendering and socket probe
- `internal/network`: host-port and guest-IP allocation
- `internal/hooks`: hook runner
- `deploy/systemd/fc@.service`: systemd unit template
- `scripts/fc-*`: host helper script stubs

## Requirements

- Linux host with:
  - `systemd`
  - `firecracker`
  - `jailer`
  - `ip` + `iptables`/`nft`
- Go 1.24+

> Note: This repo can be developed on macOS, but actual VM runtime requires a Linux host with `systemd` and Firecracker.

## Quick start

1. Run manager:

```bash
go run ./cmd/manager
```

2. Health check:

```bash
curl -s http://127.0.0.1:8080/healthz
```

3. Create VM:

```bash
curl -s -X POST http://127.0.0.1:8080/v1/vms \
  -H 'content-type: application/json' \
  -d '{
    "rootfs": "/var/lib/firecracker/base/rootfs.ext4",
    "kernel": "/var/lib/firecracker/base/vmlinux",
    "vcpu": 1,
    "memMiB": 512,
    "ports": [{"guest": 8080, "host": 0}],
    "autoStart": false
  }'
```

## API behavior notes

- `start` is idempotent: already running VM still returns success.
- `stop` is idempotent: already stopped VM still returns success.
- `delete` returns `404` if VM does not exist.
- Dependency issues (for example missing/unsupported `systemd`) return `503`.

## Configuration

Environment variables:

- `MGR_HTTP_ADDR` (default `:8080`)
- `MGR_CONFIG_ROOT` (default `/etc/firecracker/vm.d`)
- `MGR_DATA_ROOT` (default `/var/lib/firecracker`)
- `MGR_RUN_ROOT` (default `/run/firecracker`)
- `MGR_GLOBAL_HOOKS_DIR` (default `/etc/firecracker/hooks.d`)
- `MGR_UNIT_PREFIX` (default `fc`)
- `MGR_SYSTEMCTL_PATH` (default `systemctl`)
- `MGR_COMMAND_TIMEOUT_SECONDS` (default `10`)
- `MGR_PORT_START` (default `20000`)
- `MGR_PORT_END` (default `40000`)
- `MGR_GUEST_CIDR` (default `172.30.0.0/24`)
- `MGR_LOG_LEVEL` (default `info`, values: `debug|info|warn|error`)
- `MGR_LOG_FORMAT` (default `json`, values: `json|text`)

Enable verbose debugging:

```bash
MGR_LOG_LEVEL=debug MGR_LOG_FORMAT=text go run ./cmd/manager
```

## Systemd template and scripts

- Unit template: `deploy/systemd/fc@.service`
- Helper scripts:
  - `scripts/fc-net-setup`
  - `scripts/fc-jailer-start`
  - `scripts/fc-configure-start`
  - `scripts/fc-graceful-stop`
  - `scripts/fc-net-cleanup`

Current scripts are deterministic stubs to define boundaries. Replace with host-specific networking and jailer commands for production.

## Firecracker SDK note

`internal/firecracker/configurator_sdk.go` is build-tagged (`firecracker_sdk`) as a placeholder path for `github.com/firecracker-microvm/firecracker-go-sdk`.

Default build path uses the raw Unix-socket configurator and does **not** require the SDK.

## Testing

```bash
go test ./...
```

## Roadmap

- Real netns/tap/iptables implementation in helper scripts
- Graceful stop via vsock guest agent
- Envoy/Consul integration via hooks
- Stronger authn/authz for manager API
