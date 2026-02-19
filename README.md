# mergen-fire

[![Go](https://img.shields.io/badge/go-1.24+-00ADD8?logo=go)](https://go.dev/)
[![Firecracker](https://img.shields.io/badge/firecracker-microVM-orange)](https://firecracker-microvm.github.io/)
[![Platform](https://img.shields.io/badge/platform-linux%20host-lightgrey)](#requirements)

Minimal **Firecracker control-plane + TLS forwarder** in Go.

`mergen-fire` provides:

- `mergend`: VM lifecycle manager (control-plane)
- `mergen-forwarder`: TLS SNI terminating netns-aware TCP proxy (pre-Envoy dataplane bridge)
- `mergen-converter`: OCI/Docker registry image -> OCI-aligned MicroVM rootfs converter
- `mergen-init`: Go PID1 init binary for converted rootfs

## Table of Contents

- [Requirements](#requirements)
- [Quick start](#quick-start)
  - [1. Run `mergend` daemon](#1-run-mergend-daemon)
  - [2. Set up and run `mergen-forwarder`](#2-set-up-and-run-mergen-forwarder)
  - [3. Convert OCI image with `mergen-converter`](#3-convert-oci-image-with-mergen-converter)
  - [4. End-to-end test with API and curl](#4-end-to-end-test-with-api-and-curl)
- [API behavior notes](#api-behavior-notes)
- [Configuration](#configuration)
- [Forwarder Configuration](#forwarder-configuration)
- [Systemd template and scripts](#systemd-template-and-scripts)
- [Testing](#testing)

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
- `cmd/mergen-init`: in-guest init/PID1 runtime
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
- `scripts/build-golden-rootfs.sh`: Buildroot + BusyBox based golden rootfs (disk0) builder

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

### 1. Run `mergend` daemon - Terminal-1

Install systemd template + helper scripts (Linux host):

```bash
sudo install -D -m 0644 deploy/systemd/mergen@.service /etc/systemd/system/mergen@.service
sudo install -m 0755 scripts/mergen-net-setup /usr/local/bin/mergen-net-setup
sudo install -m 0755 scripts/mergen-jailer-start /usr/local/bin/mergen-jailer-start
sudo install -m 0755 scripts/mergen-configure-start /usr/local/bin/mergen-configure-start
sudo install -m 0755 scripts/mergen-graceful-stop /usr/local/bin/mergen-graceful-stop
sudo install -m 0755 scripts/mergen-net-cleanup /usr/local/bin/mergen-net-cleanup
sudo systemctl daemon-reload
```

Run daemon:

```bash
go run ./cmd/mergend
```

Health check:

```bash
curl -s http://127.0.0.1:8080/healthz
```

Install latest kernel:  

```bash
ARCH="$(uname -m)"
release_url="https://github.com/firecracker-microvm/firecracker/releases"
latest_version="$(basename "$(curl -fsSLI -o /dev/null -w '%{url_effective}' ${release_url}/latest)")"
CI_VERSION="${latest_version%.*}"

latest_kernel_key="$(curl "http://spec.ccfc.min.s3.amazonaws.com/?prefix=firecracker-ci/${CI_VERSION}/${ARCH}/vmlinux-&list-type=2" \
  | grep -oP "(firecracker-ci/${CI_VERSION}/${ARCH}/vmlinux-[0-9]+\.[0-9]+\.[0-9]{1,3})" \
  | sort -V | tail -1)"

sudo wget -O /var/lib/mergen/base/vmlinux "https://s3.amazonaws.com/spec.ccfc.min/${latest_kernel_key}"
```

### 2. Set up and run `mergen-forwarder` - Terminal-2

Generate wildcard cert into `/etc/mergen/certs`:

```bash
sudo install -d -m 0755 /etc/mergen/certs

# for *.vm.example.com domain
export FWD_DOMAIN="vm.example.com"
export FWD_DOMAIN_CERT_DIR="/etc/mergen/certs"

export FWD_DOMAIN_CERT_DAYS=365 # 1 year
export FWD_DOMAIN_CERT_FILE="${FWD_DOMAIN_CERT_DIR}/wildcard.${FWD_DOMAIN}.crt"
export FWD_DOMAIN_CERT_KEY_FILE="${FWD_DOMAIN_CERT_DIR}/wildcard.${FWD_DOMAIN}.key"

openssl req \
  -x509 \
  -newkey rsa:2048 \
  -sha256 \
  -nodes \
  -days "${FWD_DOMAIN_CERT_DAYS}" \
  -subj "/CN=*.${FWD_DOMAIN}" \
  -addext "subjectAltName=DNS:*.${FWD_DOMAIN},DNS:${FWD_DOMAIN}" \
  -keyout "${FWD_DOMAIN_CERT_KEY_FILE}" \
  -out "${FWD_DOMAIN_CERT_FILE}"
```

Run forwarder:

```bash
# set env values before start
export FWD_DOMAIN="vm.example.com"
export FWD_TLS_CERT_FILE="/etc/mergen/certs/wildcard.${FWD_DOMAIN}.crt"
export FWD_TLS_KEY_FILE="/etc/mergen/certs/wildcard.${FWD_DOMAIN}.key"
export FWD_LOG_LEVEL=debug

go run ./cmd/mergen-forwarder
```

Forwarder behavior:

- Listens on HTTPS `:443` by default (`FWD_HTTPS_ADDR`).
- Terminates TLS and resolves SNI label to VM metadata.
- Routes to `guestIP:httpPort` from VM `meta.json`.
- Returns `502` when resolved VM has no valid `httpPort`.

### 3. Convert OCI image with `mergen-converter` - Terminal-3

Build in-guest runtime binaries first (`mergen-init`, `mergen-agent`):

```bash
mkdir -p ./artifacts/sbin-init

CGO_ENABLED=0 GOOS=linux GOARCH=amd64 \
  go build -o ./artifacts/sbin-init/sbin-init ./cmd/mergen-init

CGO_ENABLED=0 GOOS=linux GOARCH=amd64 \
  go build -o ./artifacts/sbin-init/mergen-agent ./cmd/mergen-agent
```

Equivalent helper command:

```bash
./scripts/build-sbin-init-from-go.sh --goos linux --goarch amd64
```

Optional: build a reusable BusyBox-based golden rootfs (disk0) with Buildroot:

```bash
./scripts/build-golden-rootfs.sh
# output:
#   ./artifacts/golden-rootfs/golden-rootfs
#   ./artifacts/golden-rootfs/golden-rootfs.ext4
```

If you do not want to run Buildroot as root, you can create a temporary passwordless builder user and run the script with `sudo -u` from project root:

```bash
REPO_ROOT="$(pwd)"

sudo useradd -m -s /bin/bash mergen-builder || true

sudo -u mergen-builder -H bash -lc "cd '${REPO_ROOT}' && ./scripts/build-golden-rootfs.sh"

# optional cleanup after build
sudo pkill -u mergen-builder || true
sudo userdel -r mergen-builder
```

Run converter:

```bash
export IMAGE="nginx:alpine"

go run ./cmd/mergen-converter \
  -image $IMAGE \
  -golden-rootfs-dir ./artifacts/golden-rootfs/golden-rootfs
```

`mergen-converter` pulls image layers natively with `containers/image` (`go.podman.io/image/v5`) and does not execute Docker CLI.
Use `-skip-pull` to reuse `output-dir/image-cache` from a previous conversion run.
Injected `/sbin/init` is expected to be built from `cmd/mergen-init`.
Default path is under `/var/lib/mergen/images`, so run with sufficient permissions (or override with `-output-dir`).
Default output path follows image reference hierarchy under `/var/lib/mergen/images`:

- `nginx:alpine` -> `/var/lib/mergen/images/nginx:alpine`
- `ghcr.io/org/app:1.2.3` -> `/var/lib/mergen/images/ghcr.io/org/app:1.2.3`

Converter outputs:

- `golden-rootfs/` (disk0 filesystem with `mergen-init`)
- `golden-rootfs.ext4` (disk0)
- `agent-rootfs/` (disk1 filesystem with `mergen-agent`)
- `agent-rootfs.tar`
- `agent-rootfs.ext4` (disk1)
- `payload-rootfs/` extracted image filesystem
- `payload-rootfs.tar`
- `payload-rootfs.ext4` (disk2)
- `env-rootfs/` generated env filesystem
- `env-rootfs.ext4` (disk3)
- `image-meta.json` (entrypoint/cmd/env/startCmd metadata from image)
- `mergen.runtime.json` (agent runtime spec placed into env disk and consumed from `/mnt/env/mergen.runtime.json`)
- `suggested-bootargs.txt` (`init=/sbin/init`)
- `suggested-vm-request.json` (ready-to-edit payload for `POST /v1/vms`)

Delete a converted image rootfs bundle:

```bash
export IMAGE="nginx:alpine"

go run ./cmd/mergen-converter \
  -image $IMAGE \
  -delete-rootfs
```

Use `-output-dir` if you want to delete a non-default conversion location.

### 4. End-to-end test with API and curl - Terminal-4

```bash
# create vm (rootfs path from converter output)
export IMAGE="nginx:alpine"
export PORT=80
export SUBDOMAIN="app1"
export FWD_DOMAIN="vm.example.com"

export VM_JSON="$(curl -s -X POST http://127.0.0.1:8080/v1/vms \
  -H 'content-type: application/json' \
  -d '{
    "rootfs": "/var/lib/mergen/images/$IMAGE/golden-rootfs.ext4",
    "agentDisk": "/var/lib/mergen/images/$IMAGE/agent-rootfs.ext4",
    "payloadDisk": "/var/lib/mergen/images/$IMAGE/payload-rootfs.ext4",
    "envDisk": "/var/lib/mergen/images/$IMAGE/env-rootfs.ext4",
    "kernel": "/var/lib/mergen/base/vmlinux",
    "vcpu": 1,
    "memMiB": 512,
    "ports": [{"guest": $PORT, "host": 0}],
    "httpPort": $PORT,
    "tags": {"app": "$SUBDOMAIN"},
    "autoStart": false  
  }')"

echo "$VM_JSON"
export APPID="$(echo "$VM_JSON" | jq -r '.id')"

# start vm service
curl -s -X POST "http://127.0.0.1:8080/v1/vms/${APPID}/start"

# check logs
journalctl -xeu mergen@${APPID}.service

# TEST-1: uuid based
curl -k --resolve "${APPID}.${FWD_DOMAIN}:443:127.0.0.1" "https://${APPID}.${FWD_DOMAIN}/"

# TEST-2: tag based
curl -k --resolve "${SUBDOMAIN}.${FWD_DOMAIN}:443:127.0.0.1" https://${SUBDOMAIN}.${FWD_DOMAIN}/

# stop + delete
curl -s -X POST "http://127.0.0.1:8080/v1/vms/${APPID}/stop"
curl -s -X DELETE "http://127.0.0.1:8080/v1/vms/${APPID}"

# if you want to run vm netns commands
# 1 - show all available ips in netns of vm 
sudo ip netns exec mergen-$(cut -d '-' -f 1 <<< $APPID) ip neigh show dev tap-$(cut -d '-' -f 1 <<< $APPID)
# 2 - show ip addr in netns of vm 
sudo ip netns exec mergen-$(cut -d '-' -f 1 <<< $APPID) ip addr show dev tap-$(cut -d '-' -f 1 <<< $APPID)
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

`mergen-jailer-start` and `mergen-configure-start` now run real Firecracker API flow (socket + config + InstanceStart). `mergen-configure-start` also performs best-effort `PUT /entropy` before VM start (disable with `MGN_ENABLE_ENTROPY_DEVICE=0` if needed). Networking scripts are still minimal and should be hardened for production (NAT/filtering/policy).

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
