# mergen-fire

[![Go](https://img.shields.io/badge/go-1.24+-00ADD8?logo=go)](https://go.dev/)
[![Firecracker](https://img.shields.io/badge/firecracker-microVM-orange)](https://firecracker-microvm.github.io/)
[![Platform](https://img.shields.io/badge/platform-linux%20host-lightgrey)](#requirements)

Minimal **Firecracker control-plane + TLS forwarder** in Go.

`mergen-fire` provides:

- `mergend`: VM lifecycle manager (control-plane)
- `mergen-forwarder`: TLS SNI terminating netns-aware TCP proxy (pre-Envoy dataplane bridge)

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
- `systemd` service model: `mergen@<uuid>.service`
- Basic port publish + sequential IP allocation
- Per-VM lock file to prevent lifecycle race conditions
- Structured logging with configurable level/format
- Best-effort hooks:
  - `onCreate`
  - `onDelete`
  - `onStart`
  - `onStop`

## Architecture

- **Control plane:** Go HTTP API server (`cmd/mergend`)
- **Forwarding plane (pre-Envoy):** TLS SNI proxy (`cmd/mergen-forwarder`)
- **Data plane:** `systemd` + Firecracker/Jailer processes
- **State source:** filesystem under `MGR_CONFIG_ROOT`, `MGR_RUN_ROOT`, `MGR_DATA_ROOT`

Forwarder design details: `docs/forwarder-design.md`

## Repository layout

- `cmd/mergend`: manager daemon API entrypoint
- `cmd/mergen-forwarder`: TLS SNI forwarder
- `internal/api`: REST handlers
- `internal/manager`: orchestration/service layer
- `internal/forwarder`: SNI resolver + TLS proxy + netns dialer
- `internal/store`: filesystem persistence
- `internal/systemd`: `systemctl` wrapper
- `internal/firecracker`: VM config rendering and socket probe
- `internal/network`: host-port and guest-IP allocation
- `internal/hooks`: hook runner
- `deploy/systemd/mergen@.service`: systemd unit template
- `deploy/systemd/mergen-forwarder.service`: forwarder systemd unit
- `scripts/mergen-*`: host helper script stubs
- `scripts/gen-wildcard-cert.sh`: self-signed wildcard TLS cert generator

## Requirements

- Linux host with:
  - `systemd`
  - `firecracker`
  - `jailer`
  - `ip` + `iptables`/`nft`
- Go 1.24+

> Note: This repo can be developed on macOS, but actual VM runtime requires a Linux host with `systemd` and Firecracker.

## Quick start

1. Run manager daemon:

```bash
go run ./cmd/mergend
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
    "rootfs": "/var/lib/mergen/base/rootfs.ext4",
    "kernel": "/var/lib/mergen/base/vmlinux",
    "vcpu": 1,
    "memMiB": 512,
    "ports": [{"guest": 8080, "host": 0}],
    "tags": {"app": "app1"},
    "autoStart": false
  }'
```

### Systemd template install (required on Linux host)

If you see `Unit mergen@<id>.service not found`, install the template and helper scripts:

```bash
sudo install -D -m 0644 deploy/systemd/mergen@.service /etc/systemd/system/mergen@.service
sudo install -m 0755 scripts/mergen-net-setup /usr/local/bin/mergen-net-setup
sudo install -m 0755 scripts/mergen-jailer-start /usr/local/bin/mergen-jailer-start
sudo install -m 0755 scripts/mergen-configure-start /usr/local/bin/mergen-configure-start
sudo install -m 0755 scripts/mergen-graceful-stop /usr/local/bin/mergen-graceful-stop
sudo install -m 0755 scripts/mergen-net-cleanup /usr/local/bin/mergen-net-cleanup
sudo systemctl daemon-reload
```

### Generate wildcard certificate (prefix + suffix aware)

```bash
./scripts/gen-wildcard-cert.sh ./certs
```

Example for custom domain pattern (`*.vm.example.com`):

```bash
CERT_DOMAIN_PREFIX=vm \
CERT_DOMAIN_SUFFIX=example.com \
./scripts/gen-wildcard-cert.sh /etc/mergen/certs
```

### Run TLS SNI forwarder

```bash
FWD_DOMAIN_PREFIX= \
FWD_DOMAIN_SUFFIX=localhost \
FWD_TLS_CERT_FILE=/etc/mergen/certs/wildcard.localhost.crt \
FWD_TLS_KEY_FILE=/etc/mergen/certs/wildcard.localhost.key \
FWD_LOG_LEVEL=debug \
go run ./cmd/mergen-forwarder
```

Default forwarder listeners:

- `:8443 -> guest:8080`
- `:9443 -> guest:443`
- `:10022 -> guest:22`

All listeners are TLS listeners (SNI is required). `:10022` is for TLS-wrapped traffic to guest `22`, not raw SSH protocol passthrough.

Example request:

```bash
curl -k --resolve app1.localhost:8443:127.0.0.1 https://app1.localhost:8443/
curl -k --resolve 084604f6.localhost:8443:127.0.0.1 https://084604f6.localhost:8443/
```

With custom prefix/suffix:

```bash
# FWD_DOMAIN_PREFIX=vm, FWD_DOMAIN_SUFFIX=example.com
curl -k --resolve app1.vm.example.com:8443:127.0.0.1 https://app1.vm.example.com:8443/
```

## API behavior notes

- `start` is idempotent: already running VM still returns success.
- `stop` is idempotent: already stopped VM still returns success.
- `delete` returns `404` if VM does not exist.
- Dependency issues (for example missing/unsupported `systemd`) return `503`.

## Configuration

Environment variables:

- `MGR_HTTP_ADDR` (default `:8080`)
- `MGR_CONFIG_ROOT` (default `/etc/mergen/vm.d`)
- `MGR_DATA_ROOT` (default `/var/lib/mergen`)
- `MGR_RUN_ROOT` (default `/run/mergen`)
- `MGR_GLOBAL_HOOKS_DIR` (default `/etc/mergen/hooks.d`)
- `MGR_UNIT_PREFIX` (default `mergen`)
- `MGR_SYSTEMCTL_PATH` (default `systemctl`)
- `MGR_COMMAND_TIMEOUT_SECONDS` (default `10`)
- `MGR_SHUTDOWN_TIMEOUT_SECONDS` (default `15`)
- `MGR_PORT_START` (default `20000`)
- `MGR_PORT_END` (default `40000`)
- `MGR_GUEST_CIDR` (default `172.30.0.0/24`)
- `MGR_LOG_LEVEL` (default `info`, values: `debug|info|warn|error`)
- `MGR_LOG_FORMAT` (default `console`, values: `console|json|text`)

Enable verbose debugging:

```bash
MGR_LOG_LEVEL=debug MGR_LOG_FORMAT=console go run ./cmd/mergend
```

`console` format prints colored output in this order: `[LEVEL] TIMESTAMP MESSAGE key=value...`

- `INFO` is blue
- `WARN` is yellow
- `ERROR` is red
- `DEBUG` is cyan

Forwarder logging uses:

- `FWD_LOG_LEVEL` (default `debug`, values: `debug|info|warn|error`)
- `FWD_LOG_FORMAT` (default `console`, values: `console|json|text`)

To emit JSON for Elastic:

```bash
FWD_LOG_FORMAT=json go run ./cmd/mergen-forwarder
```

## Forwarder Configuration

Environment variables:

- `FWD_CONFIG_ROOT` (default `/etc/mergen/vm.d`)
- `FWD_NETNS_ROOT` (default `/run/netns`)
- `FWD_TLS_CERT_FILE` (default `/etc/mergen/certs/wildcard.localhost.crt`)
- `FWD_TLS_KEY_FILE` (default `/etc/mergen/certs/wildcard.localhost.key`)
- `FWD_DOMAIN_PREFIX` (default empty)
- `FWD_DOMAIN_SUFFIX` (default `localhost`)
- `FWD_LISTENERS` (default `:8443=8080,:9443=443,:10022=22`)
- `FWD_ALLOWED_GUEST_PORTS` (default `22,8080,443`)
- `FWD_DIAL_TIMEOUT_SECONDS` (default `5`)
- `FWD_RESOLVER_CACHE_TTL_SECONDS` (default `5`)
- `FWD_SHUTDOWN_TIMEOUT_SECONDS` (default `15`)
- `FWD_LOG_LEVEL` (default `debug`)
- `FWD_LOG_FORMAT` (default `console`)

SNI matching:

- prefix empty: `<label>.<suffix>`
- prefix set: `<label>.<prefix>.<suffix>`

## Systemd template and scripts

- Unit template: `deploy/systemd/mergen@.service`
- Helper scripts:
  - `scripts/mergen-net-setup`
  - `scripts/mergen-jailer-start`
  - `scripts/mergen-configure-start`
  - `scripts/mergen-graceful-stop`
  - `scripts/mergen-net-cleanup`

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
