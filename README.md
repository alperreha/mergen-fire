# mergen-fire

[![Go](https://img.shields.io/badge/go-1.24+-00ADD8?logo=go)](https://go.dev/)
[![Firecracker](https://img.shields.io/badge/firecracker-microVM-orange)](https://firecracker-microvm.github.io/)
[![Platform](https://img.shields.io/badge/platform-linux%20host-lightgrey)](#requirements)

Minimal **Firecracker control-plane + TLS forwarder** in Go.

`mergen-fire` provides:

- `mergend`: VM lifecycle manager (control-plane)
- `mergen-forwarder`: TLS SNI terminating netns-aware TCP proxy (pre-Envoy dataplane bridge)
- `mergen-converter`: OCI/Docker registry image -> OCI-aligned MicroVM rootfs converter
- `mergen-init-snapshot`: Go PID1 init binary for converted rootfs

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
- Per-VM HTTP target port metadata (`httpPort`) for TLS-terminated `:443` routing
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
- **Image conversion plane:** Registry-image-to-rootfs converter (`cmd/mergen-converter`)
- **Data plane:** `systemd` + Firecracker/Jailer processes
- **State source:** filesystem under `MGR_CONFIG_ROOT`, `MGR_RUN_ROOT`, `MGR_DATA_ROOT`

Forwarder design details: `docs/forwarder-design.md`

## Repository layout

- `cmd/mergend`: manager daemon API entrypoint
- `cmd/mergen-forwarder`: TLS SNI forwarder
- `cmd/mergen-converter`: registry image conversion CLI
- `cmd/mergen-init-snapshot`: in-guest init/PID1 runtime
- `internal/api`: REST handlers
- `internal/manager`: orchestration/service layer
- `internal/forwarder`: SNI resolver + TLS proxy + netns dialer
- `internal/converter`: native image pull/cache/rootfs/ext4 conversion pipeline
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
  - `mkfs.ext4` + `truncate`
- Go 1.24+

Optional (only for legacy helper script `scripts/build-rootfs-from-dockerhub.sh`):

- `docker`
- `jq`

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

3. Create VM: (you can start vm after run mergend and mergen-forwarder services)

```bash
curl -s -X POST http://127.0.0.1:8080/v1/vms \
  -H 'content-type: application/json' \
  -d '{
    "rootfs": "/var/lib/mergen/base/rootfs.ext4",
    "kernel": "/var/lib/mergen/base/vmlinux",
    "vcpu": 1,
    "memMiB": 512,
    "ports": [{"guest": 80, "host": 0}],
    "httpPort": 80,
    "tags": {"app": "app1"},
    "autoStart": false
  }'

# after creation get generated uuid and set below.
export APPID="<generated-uuid>"

# start vm 
systemctl start mergen@$APPID.service

# if http ports are right. You can test with curl after forwarder is started.
curl -k --resolve "$APPID.localhost:443:127.0.0.1" https://$APPID.localhost/

# stop vm
systemctl stop mergen@$APPID.service

# debug logs
systemctl status mergen@$APPID.service
journalctl -u mergen@$APPID.service

# delete vm
curl -s -X POST http://127.0.0.1:8080/v1/vms/$APPID/stop
curl -s -X DELETE http://127.0.0.1:8080/v1/vms/$CURID

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
sudo install -d -m 0755 /etc/mergen/certs
sudo ./scripts/gen-wildcard-cert.sh /etc/mergen/certs
```

Example for custom domain pattern (`*.vm.example.com`):

```bash
CERT_DOMAIN_PREFIX=vm \
CERT_DOMAIN_SUFFIX=example.com \
sudo ./scripts/gen-wildcard-cert.sh /etc/mergen/certs
```

### Convert image with `mergen-converter`

Place your custom init binary first:

```bash
./scripts/build-sbin-init-from-go.sh
# or place your own binary manually at:
# ./artifacts/sbin-init/sbin-init
```

Run converter:

```bash
go run ./cmd/mergen-converter \
  -image nginx:alpine
```

`mergen-converter` pulls image layers natively with `containers/image` (`go.podman.io/image/v5`) and does not execute Docker CLI.
Use `-skip-pull` to reuse `output-dir/image-cache` from a previous conversion run.
Injected `/sbin/init` is expected to be built from `cmd/mergen-init-snapshot`.
Default path is under `/var/lib/mergen/images`, so run with sufficient permissions (or override with `-output-dir`).
Default output path follows image reference hierarchy under `/var/lib/mergen/images`:

- `nginx:alpine` -> `/var/lib/mergen/images/nginx:alpine`
- `ghcr.io/org/app:1.2.3` -> `/var/lib/mergen/images/ghcr.io/org/app:1.2.3`

Converter outputs:

- `rootfs/` extracted filesystem
- `rootfs.tar`
- `rootfs.ext4`
- `image-meta.json` (entrypoint/cmd/env/startCmd metadata for init)
- `suggested-bootargs.txt` (`init=/sbin/init`)
- `suggested-vm-request.json` (ready-to-edit payload for `POST /v1/vms`)

Delete a converted image rootfs bundle:

```bash
go run ./cmd/mergen-converter \
  -image nginx:alpine \
  -delete-rootfs
```

Use `-output-dir` if you want to delete a non-default conversion location.

### Run TLS SNI forwarder

```bash
FWD_DOMAIN_PREFIX= \
FWD_DOMAIN_SUFFIX=localhost \
FWD_TLS_CERT_FILE=/etc/mergen/certs/wildcard.localhost.crt \
FWD_TLS_KEY_FILE=/etc/mergen/certs/wildcard.localhost.key \
FWD_LOG_LEVEL=debug \
go run ./cmd/mergen-forwarder
```

Forwarder behavior:

- Listens on HTTPS `:443` by default (`FWD_HTTPS_ADDR`).
- Terminates TLS and resolves SNI label to VM metadata.
- Routes to `guestIP:httpPort` from VM `meta.json`.
- Returns `502` when resolved VM has no valid `httpPort`.

Example requests:

```bash
# HTTPS
# subdomain by tag
curl -k --resolve app1.localhost:443:127.0.0.1 https://app1.localhost/
# subdomain by uuid
curl -k --resolve <uuid>.localhost:443:127.0.0.1 https://<uuid>.localhost/
```

With custom prefix/suffix:

```bash
# FWD_DOMAIN_PREFIX=vm, FWD_DOMAIN_SUFFIX=example.com
curl -k --resolve app1.vm.example.com:443:127.0.0.1 https://app1.vm.example.com/
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

`POST /v1/vms` supports:

- `httpPort` (optional): Guest HTTP port for TLS-terminated `:443` forwarder routing.

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
- `FWD_HTTPS_ADDR` (default `:443`)
- `FWD_DIAL_TIMEOUT_SECONDS` (default `5`)
- `FWD_RESOLVER_CACHE_TTL_SECONDS` (default `5`)
- `FWD_SHUTDOWN_TIMEOUT_SECONDS` (default `15`)
- `FWD_LOG_LEVEL` (default `debug`)
- `FWD_LOG_FORMAT` (default `console`)

SNI matching:

- prefix empty: `<label>.<suffix>`
- prefix set: `<label>.<prefix>.<suffix>`

This SNI rule applies to HTTPS listener routing.

## Systemd template and scripts

- Unit template: `deploy/systemd/mergen@.service`
- Helper scripts:
  - `scripts/mergen-net-setup`
  - `scripts/mergen-jailer-start`
  - `scripts/mergen-configure-start`
  - `scripts/mergen-graceful-stop`
  - `scripts/mergen-net-cleanup`

`mergen-jailer-start` and `mergen-configure-start` now run real Firecracker API flow (socket + config + InstanceStart). Networking scripts are still minimal and should be hardened for production (NAT/filtering/policy).

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
