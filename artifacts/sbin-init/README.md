Place your guest bootstrap binaries in this directory before running `mergen-converter`.

Expected default path:

- `./artifacts/sbin-init/sbin-init`
- `./artifacts/sbin-init/mergen-agent`

`mergen-converter` injects init binaries into golden rootfs at:

- `/sbin/init` (used by kernel boot args)
- `/sbin/mergen-init` (preserved copy)

`mergen-converter` injects agent binary into agent disk at:

- `/mergen-agent`

Build Go-based `mergen-init` + `mergen-agent` into this path with:

- `./scripts/build-sbin-init-from-go.sh`

Legacy option (Rust/Fly snapshot build):

- `./scripts/build-sbin-init-from-fly.sh`
