Place your guest bootstrap binaries in this directory before running `mergen-converter`.

Expected default path:

- `./artifacts/sbin-init/sbin-init`
- `./artifacts/sbin-init/mergen-telemetry`
- `./artifacts/sbin-init/mergen-supervisor`

`mergen-converter` injects these binaries into golden rootfs at:

- `/sbin/init` (used by kernel boot args)
- `/sbin/mergen-init` (preserved copy)
- `/sbin/mergen-telemetry`
- `/sbin/mergen-supervisor`

Build Go-based `mergen-init` + telemetry + supervisor into this path with:

- `./scripts/build-sbin-init-from-go.sh`

Legacy option (Rust/Fly snapshot build):

- `./scripts/build-sbin-init-from-fly.sh`
