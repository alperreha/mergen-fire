# mergen-fire

Minimal Firecracker VM manager written in Go.

## Features (v0.1)

- VM lifecycle API:
  - `POST /v1/vms`
  - `POST /v1/vms/:id/start`
  - `POST /v1/vms/:id/stop`
  - `DELETE /v1/vms/:id`
  - `GET /v1/vms/:id`
  - `GET /v1/vms`
- File-based state store (`vm.json`, `meta.json`, optional `hooks.json`, `env`)
- `systemd` integration through `fc@<uuid>.service`
- Per-VM lock file to avoid lifecycle races
- Host port publish allocation + guest IP allocation (simple sequential IPAM)
- Hook mechanism (`onCreate`, `onDelete`, `onStart`, `onStop`)

## Repository layout

- `cmd/manager`: Echo HTTP server
- `internal/api`: REST handlers
- `internal/manager`: Lifecycle service
- `internal/store`: Filesystem persistence
- `internal/systemd`: `systemctl` wrapper
- `internal/firecracker`: Firecracker config rendering + socket probe
- `internal/network`: Port/IP allocator
- `internal/hooks`: Hook runner
- `deploy/systemd/fc@.service`: systemd template unit
- `scripts/fc-*`: host helper script stubs

## Environment variables

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

## Firecracker SDK note

`internal/firecracker/configurator_sdk.go` contains a build-tagged (`firecracker_sdk`) placeholder path for `github.com/firecracker-microvm/firecracker-go-sdk`.  
Default build path does not require the SDK and uses a raw Unix-socket API configurator.

## Run

```bash
go run ./cmd/manager
```

## Test

```bash
go test ./...
```
