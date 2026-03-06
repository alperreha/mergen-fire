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
- `mergen-vsock-guest`: in-guest VSock shell listener
- `mergen-vsock-host`: host-side VSock terminal client

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
- [Systemd Template and Mergen Lifecycle](#systemd-template-and-mergen-lifecycle)
- [VSock Emergency Shell](#vsock-emergency-shell)
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
  - `GET /v1/vms/:id/meta.json`
  - `GET /v1/vms/:id/vm.json`
  - `GET /v1/vms/:id/hooks.json`
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
- **Emergency access plane:** Firecracker VSock host<->guest terminal bridge
- **State source:** filesystem under `MGR_CONFIG_ROOT`, `MGR_RUN_ROOT`, `MGR_DATA_ROOT`

Forwarder design details: `docs/forwarder-design.md`

## Repository layout

- `cmd/mergend`: manager daemon API entrypoint
- `cmd/mergen-forwarder`: TLS SNI forwarder
- `cmd/mergen-converter`: registry image conversion CLI
- `cmd/mergen-init`: in-guest init/PID1 runtime
- `cmd/mergen-vsock-guest`: in-guest VSock listener that spawns shell
- `cmd/mergen-vsock-host`: host-side VSock connector
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
- `cmd/mergen-lifecycle`: lifecycle binary used by systemd hooks
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

Install systemd template + lifecycle binary (Linux host):

```bash
sudo install -D -m 0644 deploy/systemd/mergen@.service /etc/systemd/system/mergen@.service
sudo install -D -m 0644 deploy/systemd/mergen-failure@.service /etc/systemd/system/mergen-failure@.service
CGO_ENABLED=0 GOOS=linux GOARCH=amd64 \
  go build -o /tmp/mergen-lifecycle ./cmd/mergen-lifecycle
sudo install -m 0755 /tmp/mergen-lifecycle /usr/local/bin/mergen-lifecycle
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

Generate wildcard cert into `/var/lib/mergen/certs`:

```bash
sudo install -d -m 0755 /var/lib/mergen/certs

# for *.vm.example.com domain
export FWD_DOMAIN="vm.example.com"
export FWD_DOMAIN_CERT_DIR="/var/lib/mergen/certs"

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
export FWD_TLS_CERT_FILE="/var/lib/mergen/certs/wildcard.${FWD_DOMAIN}.crt"
export FWD_TLS_KEY_FILE="/var/lib/mergen/certs/wildcard.${FWD_DOMAIN}.key"
export FWD_LOG_LEVEL=debug

go run ./cmd/mergen-forwarder
```

Forwarder behavior:

- Listens on HTTPS `:443` by default (`FWD_HTTPS_ADDR`).
- Terminates TLS and resolves SNI label to VM metadata.
- Routes to `guestIP:httpPort` from VM `meta.json`.
- Returns `502` when resolved VM has no valid `httpPort`.

### 3. Convert OCI image with `mergen-converter` - Terminal-3

Build and install guest/base binaries into a versioned base directory:

```bash
export BASE_DIR="/var/lib/mergen/base"
export BASE_VERSION="v$(date -u +%Y%m%d%H%M%S)"

sudo ./scripts/build-sbin-init-from-go.sh \
  --goos linux --goarch amd64 \
  --install-base \
  --base-dir "${BASE_DIR}" \
  --base-version "${BASE_VERSION}"
```

This installs binaries under `${BASE_DIR}/${BASE_VERSION}/bin/`:

- `sbin-init`
- `mergen-agent`
- `mergen-vsock-guest`
- optional: `mergen-supervisor`, `mergen-telemetry` (built only if corresponding `cmd/...` has Go files)

And updates `${BASE_DIR}/current -> ${BASE_VERSION}` symlink (unless `--no-current-link` is used).

Place immutable base assets into the same version directory:

```bash
export BASE_DIR="/var/lib/mergen/base"
export BASE_VERSION="v20260306120000" # example

sudo install -d -m 0755 "${BASE_DIR}/${BASE_VERSION}"
sudo install -m 0444 /path/to/vmlinux "${BASE_DIR}/${BASE_VERSION}/vmlinux"
sudo install -m 0444 /path/to/golden-rootfs.ext4 "${BASE_DIR}/${BASE_VERSION}/golden-rootfs.ext4"
sudo install -m 0444 /path/to/agent-rootfs.ext4 "${BASE_DIR}/${BASE_VERSION}/agent-rootfs.ext4"
# optional env disk:
sudo install -m 0444 /path/to/env-rootfs.ext4 "${BASE_DIR}/${BASE_VERSION}/env-rootfs.ext4"

sudo ln -sfn "${BASE_VERSION}" "${BASE_DIR}/current"
```

Only binaries and disk/kernel artifacts should be placed in `/var/lib/mergen/base/...` (do not place README/docs there).

Host-side binaries are separate and should be installed to `/usr/local/bin` (example set):

```bash
CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -o /tmp/mergend ./cmd/mergend
CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -o /tmp/mergen-lifecycle ./cmd/mergen-lifecycle
CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -o /tmp/mergen-forwarder ./cmd/mergen-forwarder
CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -o /tmp/mergen-converter ./cmd/mergen-converter
CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -o /tmp/mergen-vsock-host ./cmd/mergen-vsock-host

sudo install -m 0755 /tmp/mergend /usr/local/bin/mergend
sudo install -m 0755 /tmp/mergen-lifecycle /usr/local/bin/mergen-lifecycle
sudo install -m 0755 /tmp/mergen-forwarder /usr/local/bin/mergen-forwarder
sudo install -m 0755 /tmp/mergen-converter /usr/local/bin/mergen-converter
sudo install -m 0755 /tmp/mergen-vsock-host /usr/local/bin/mergen-vsock-host
```

Optional: build a reusable BusyBox-based golden rootfs (disk0) with Buildroot:

```bash
# install build_root dependencies
sudo apt install -y build-essential unzip
# if you are root add FORCE_UNSAFE_CONFIGURE=1 to the command
FORCE_UNSAFE_CONFIGURE=1 ./scripts/build-golden-rootfs.sh
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

Run converter (default mode is payload-only):

```bash
export IMAGE="nginx:alpine"

go run ./cmd/mergen-converter \
  convert \
  -image "$IMAGE" \
  -vsock-enable
```

Generate or refresh `agent-rootfs.ext4` directly from base binaries (`/var/lib/mergen/base/<version>/bin`):

```bash
go run ./cmd/mergen-converter \
  convert \
  --agent-rootfs \
  --base-assets-dir /var/lib/mergen/base/current
```

Optional output path and size:

```bash
go run ./cmd/mergen-converter \
  convert \
  --agent-rootfs \
  --base-assets-dir /var/lib/mergen/base/current \
  --agent-rootfs-output /var/lib/mergen/base/current/agent-rootfs.ext4 \
  --agent-rootfs-size-mib 64
```

`--agent-rootfs` mode does not pull image and does not build payload; it only builds mountable ext4 agent disk from base `bin` binaries and marks output as read-only.

`mergen-converter` pulls image layers natively with `containers/image` (`go.podman.io/image/v5`) and does not execute Docker CLI.
Use `-skip-pull` to reuse `output-dir/image-cache` from a previous conversion run.
Default behavior creates only payload artifacts (`payload-rootfs/`, `payload-rootfs.ext4`, metadata files).  
If you really need old behavior (golden/agent/env bundle per image), use `-legacy-full-bundle`.
Legacy bundle mode can still use `-golden-rootfs-dir` and `-sbin-init`/`-sbin-agent`.
Use `-vsock-enable` to include VSock runtime metadata in generated runtime JSON.
Injected `/sbin/init` is expected to be built from `cmd/mergen-init`.
Default path is under `/var/lib/mergen/images`, so run with sufficient permissions (or override with `-output-dir`).
Default output path follows image reference hierarchy under `/var/lib/mergen/images`:

- `nginx:alpine` -> `/var/lib/mergen/images/nginx:alpine`
- `ghcr.io/org/app:1.2.3` -> `/var/lib/mergen/images/ghcr.io/org/app:1.2.3`

Converter outputs:

- `payload-rootfs/` extracted image filesystem
- `payload-rootfs.tar`
- `payload-rootfs.ext4` (disk2)
- `image-meta.json` (entrypoint/cmd/env/startCmd metadata from image)
- `mergen.runtime.json` (runtime metadata output for orchestration/debug)
- `suggested-bootargs.txt` (`init=/sbin/init`)
- `suggested-vm-request.json` (ready-to-edit payload for `POST /v1/vms`, points kernel/rootfs/agent to base dir)

Base assets (kernel, golden rootfs, agent disk, optional env disk) should be managed under `/var/lib/mergen/base/<version>/...` and exposed via `/var/lib/mergen/base/current`.

Delete a converted image rootfs bundle:

```bash
export IMAGE="nginx:alpine"

go run ./cmd/mergen-converter \
  convert \
  -image "$IMAGE" \
  -delete-rootfs
```

Use `-output-dir` if you want to delete a non-default conversion location.

Run converter doctor checks:

```bash
go run ./cmd/mergen-converter doctor
```

Example JSON output:

```bash
go run ./cmd/mergen-converter doctor --json
```

`doctor` validates base assets under `/var/lib/mergen/base/current` by default:

- `vmlinux`
- `golden-rootfs.ext4`
- `agent-rootfs.ext4`
- `bin/sbin-init`
- `bin/mergen-agent`
- `base/current` symlink integrity (`current -> <version>`) when `--base-dir` ends with `current`
- optional: `env-rootfs.ext4`, `bin/mergen-vsock-guest`, `bin/mergen-supervisor`, `bin/mergen-telemetry`, `mergen-vsock-host` in `PATH`

`doctor` does not check payload rootfs because payload is image-specific and provided at VM create time.

CLI output uses emojis for readability:

- `✅ PASS`
- `❌ FAIL`
- `⚠️ WARN`

### 4. End-to-end test with API and curl - Terminal-4

```bash
# create vm (payload from converter output, base rootfs/agent from /var/lib/mergen/base/current)
export IMAGE="nginx:alpine"
export PORT=80
export SUBDOMAIN="app1"
export FWD_DOMAIN="vm.example.com"

export VM_JSON="$(curl -s -X POST http://127.0.0.1:8080/v1/vms \
  -H 'content-type: application/json' \
  -d '{
    "rootfs": "/var/lib/mergen/base/current/golden-rootfs.ext4",
    "agentDisk": "/var/lib/mergen/base/current/agent-rootfs.ext4",
    "payloadDisk": "/var/lib/mergen/images/$IMAGE/payload-rootfs.ext4",
    "kernel": "/var/lib/mergen/base/current/vmlinux",
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
- `MGR_CONFIG_ROOT` (default `/var/lib/mergen/vm.d`)
- `MGR_DATA_ROOT` (default `/var/lib/mergen`)
- `MGR_RUN_ROOT` (default `/run/mergen`)
- `MGR_GLOBAL_HOOKS_DIR` (default `/var/lib/mergen/hooks.d`)
- `MGR_UNIT_PREFIX` (default `mergen`)
- `MGR_SYSTEMCTL_PATH` (default `systemctl`)
- `MGR_COMMAND_TIMEOUT_SECONDS` (default `10`)
- `MGR_SHUTDOWN_TIMEOUT_SECONDS` (default `15`)
- `MGR_PORT_START` (default `20000`)
- `MGR_PORT_END` (default `40000`)
- `MGR_GUEST_CIDR` (default `172.30.0.0/24`)
- `MGR_BASE_ASSETS_DIR` (default `/var/lib/mergen/base/current`)
- `MGR_DEFAULT_KERNEL` (default `<MGR_BASE_ASSETS_DIR>/vmlinux`)
- `MGR_DEFAULT_ROOTFS` (default `<MGR_BASE_ASSETS_DIR>/golden-rootfs.ext4`)
- `MGR_DEFAULT_AGENT_DISK` (default `<MGR_BASE_ASSETS_DIR>/agent-rootfs.ext4`)
- `MGR_DEFAULT_ENV_DISK` (default empty, optional)
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

- `FWD_CONFIG_ROOT` (default `/var/lib/mergen/vm.d`)
- `FWD_NETNS_ROOT` (default `/run/netns`)
- `FWD_TLS_CERT_FILE` (default `/var/lib/mergen/certs/wildcard.localhost.crt`)
- `FWD_TLS_KEY_FILE` (default `/var/lib/mergen/certs/wildcard.localhost.key`)
- `FWD_DOMAIN` (default `localhost`)
- `FWD_HTTPS_ADDR` (default `:443`)
- `FWD_DIAL_TIMEOUT_SECONDS` (default `5`)
- `FWD_RESOLVER_CACHE_TTL_SECONDS` (default `5`)
- `FWD_SHUTDOWN_TIMEOUT_SECONDS` (default `15`)
- `FWD_LOG_LEVEL` (default `debug`)
- `FWD_LOG_FORMAT` (default `console`)

SNI matching:

- `<label>.<domain>` (for example `app1.vm.example.com`)

This SNI rule applies to HTTPS listener routing.

## Systemd Template and Mergen Lifecycle

- Unit template: `deploy/systemd/mergen@.service`
- Failure hook template: `deploy/systemd/mergen-failure@.service` (`OnFailure=`)
- Lifecycle binary:
  - `cmd/mergen-lifecycle` (install as `/usr/local/bin/mergen-lifecycle`)
  - Systemd stages: `pre-start`, `start`, `post-start`, `stop`, `post-stop`, `on-failure`
  - Extra toolkit commands: `status`, `restart`, `pre-delete`, `delete`, `post-delete`

`mergen-lifecycle post-start` runs the Firecracker API flow (socket + config + InstanceStart) through `github.com/firecracker-microvm/firecracker-go-sdk`. `mergen-lifecycle stop` sends `SendCtrlAltDel` via SDK as best-effort stop signal. `mergen-lifecycle status` reads instance info from the Firecracker API socket, and `mergen-lifecycle restart` sends the same guest restart action. `MGN_ENABLE_ENTROPY_DEVICE` is kept for forward compatibility, but current pinned SDK version (`v1.0.0`) does not expose the entropy endpoint wrapper yet, so entropy setup is skipped with a debug log. Stage extensions run sequentially and wait for each hook to complete before the next hook starts. Lifecycle stage events are append-only written to `MGN_DAEMON_LOG_FILE` (default `/var/lib/mergen/daemon.log`).

For per-VM extension hooks, create `lifecycle-hooks.json` under the VM config dir (`/var/lib/mergen/vm.d/<vm-id>/lifecycle-hooks.json`) with stage arrays (`preStart`, `postStart`, `preStop`, `postStop`, `preDelete`, `delete`, `postDelete`). You can also pass command hooks via env (`MGN_LIFECYCLE_PRE_START_HOOKS`, `MGN_LIFECYCLE_POST_STOP_HOOKS`, etc.), using `;` as separator. `{{vm_id}}` and `${VM_ID}` tokens are supported.

## VSock Emergency Shell

Firecracker terminal access for host -> guest shell can be added with native VSock support.

- No extra disk is required by Firecracker for VSock itself.  
  VSock is a VM device (`vm.json -> vsock`), not a block disk.
- `mergen-vsock-guest` runs inside guest and listens on fixed VSock channel `7000`.
- `mergen-vsock-host` runs on host and connects to Firecracker VSock UDS path.

### 1. Create VM with VSock enabled

Add these fields to `POST /v1/vms` payload:

```json
{
  "vsockEnabled": true,
  "vsockGuestCID": 3
}
```

Optional:

- `vsockUDSPath`: custom host-side UDS path (default: `<runDir>/mergen.vsock`)
- `vsockID`: custom device id (default: `mergen`)

### 2. Start guest helper from converter/agent flow

If you use converter with `-vsock-enable`, runtime metadata includes:

- `vsockEnabled`
- `vsockGuestPath`
- `vsockAuthToken` (if provided)

`mergen-agent` starts `mergen-vsock-guest` automatically when `vsockEnabled=true`.

### 3. Connect from host

Build host binary:

```bash
CGO_ENABLED=0 GOOS=linux GOARCH=amd64 \
  go build -o ./artifacts/sbin-init/mergen-vsock-host ./cmd/mergen-vsock-host
```

Connect using VM id (reads `vm.json` automatically):

```bash
./artifacts/sbin-init/mergen-vsock-host -vm-id <vm-id>
```

One-shot command mode:

```bash
./artifacts/sbin-init/mergen-vsock-host -vm-id <vm-id> -command 'uname -a'
```

The host now sends a framed one-shot command (`__MERGEN_EXEC_BEGIN__/__MERGEN_EXEC_END__`) and waits for completion marker (`__MERGEN_EXEC_DONE__:<code>`), so command mode does not depend on interactive shell exit behavior.

Useful debug flags:

```bash
./artifacts/sbin-init/mergen-vsock-host \
  -vm-id <vm-id> \
  -command 'echo hello' \
  -debug \
  -command-timeout 30s
```

Enable guest-side debug logs with environment variable:

```bash
export MERGEN_VSOCK_DEBUG=1
```

Troubleshooting when host appears stuck on dial:

- `udsPath=""` in host debug output is normal when `-uds-path` is not passed; it means host will resolve path from `vm.json`.
- Verify Firecracker vsock device exists in VM config:
  - `jq '.vsock' /var/lib/mergen/vm.d/<vm-id>/vm.json`
- Verify guest runtime actually enables vsock listener:
  - follow VM logs: `journalctl -fu mergen@<vm-id>.service`
  - look for:
    - `runtime spec resolved ... vsockEnabled=true`
    - `vsock guest listener started`
- If logs show `vsockEnabled=false`, guest listener is intentionally not started.
  Most common reason: `mergen.runtime.json` is not mounted from env disk.
- New host flag `-dial-timeout` prevents infinite wait and returns a clear timeout error.

If auth token is configured in guest runtime metadata, provide it with:

```bash
./artifacts/sbin-init/mergen-vsock-host -vm-id <vm-id> -auth-token '<token>'
```

## Testing

```bash
go test ./...
```

## Roadmap

- Real netns/tap/iptables implementation in lifecycle stage extensions
- Graceful stop via vsock guest agent
- Envoy/Consul integration via hooks
- Stronger authn/authz for manager API

## How To: Add a Post-Start Extension Hook

This example prints a console log right after the `post-start` stage completes.

### 1. Create extension file

Create `cmd/mergen-lifecycle/hooks/poststart/log_ready.go`:

```go
package poststart

import (
	"context"
	"fmt"

	lifecyclehooks "github.com/alperreha/mergen-fire/cmd/mergen-lifecycle/hooks"
)

// HandleLogReady runs in post-start stage and prints a human-readable message.
func HandleLogReady(_ context.Context, req lifecyclehooks.Request) error {
	fmt.Printf("MERGEN_HOOK_POSTSTART: VM Start Success of vm_id=%s\n", req.VMID)
	return nil
}
```

### 2. Register the hook in lifecycle manager

Open `cmd/mergen-lifecycle/lifecycle.go` and add the new hook to `stagePostStart`:

```go
manager.Register(string(stagePostStart),
	lifecyclehooks.Definition{Name: "template", Strict: true, Handle: hookpoststart.HandleTemplate},
	lifecyclehooks.Definition{Name: "log-ready", Strict: true, Handle: hookpoststart.HandleLogReady},
)
```

Hooks run sequentially in the order you register them. The next hook starts only after the previous one finishes.

### 3. Build and install lifecycle binary

```bash
go build -o /tmp/mergen-lifecycle ./cmd/mergen-lifecycle
sudo install -m 0755 /tmp/mergen-lifecycle /usr/local/bin/mergen-lifecycle
```

### 4. Restart VM unit and verify output

```bash
sudo systemctl restart mergen@<vm-id>.service
journalctl -u mergen@<vm-id>.service -n 100 --no-pager
```

Expected line:

```text
MERGEN_HOOK_POSTSTART: VM Start Success of vm_id=<vm-id>
```
