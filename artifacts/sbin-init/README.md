Place your `sbin init` binary in this directory before running `mergen-converter`.

Expected default path:

- `./artifacts/sbin-init/sbin-init`

`mergen-converter` injects this binary into converted rootfs at:

- `/sbin/init` (used by kernel boot args)
- `/sbin/mergen-init` (preserved copy)

You can build Fly.io `init-snapshot` into this path with:

- `./scripts/build-sbin-init-from-fly.sh`
