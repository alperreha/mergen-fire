# mergen-converter

`mergen-converter` is the standalone payload builder for Mergen.

It can:

- pull OCI/Docker images directly from registries
- convert them into `payload-rootfs.ext4`
- store outputs under `/var/lib/mergen/images/<image-ref>`
- push and pull only the ready-to-run `payload-rootfs.ext4` file to a user S3 / MinIO registry

## Commands

Convert an image locally:

```bash
go run ./cmd/mergen-converter create --image nginx:alpine
```

Initialize a user S3 registry profile:

```bash
go run ./cmd/mergen-converter init \
  --registry default \
  --endpoint http://127.0.0.1:9000 \
  --region us-east-1 \
  --bucket mergen-user \
  --prefix users \
  --access-key minioadmin \
  --secret-key minioadmin \
  --username alice \
  --use-path-style
```

For AWS S3, you can omit `--endpoint` and rely on region-based AWS endpoints instead.

Store username/password without remote validation:

```bash
go run ./cmd/mergen-converter login \
  --registry default \
  --username alice \
  --password demo-password
```

Push a converted payload ext4:

```bash
go run ./cmd/mergen-converter push \
  --registry default \
  --image nginx:alpine
```

Pull a payload ext4 back to the local image store:

```bash
go run ./cmd/mergen-converter pull \
  --registry default \
  --image nginx:alpine
```

## Remote object layout

Only the ext4 payload file is transferred.

Remote key format:

```text
<prefix>/<username>/payload/<url-escaped-image-ref>/payload-rootfs.ext4
```

Example:

```text
users/alice/payload/nginx:alpine/payload-rootfs.ext4
```

If the username or image reference contains `/`, it is URL-escaped before upload.

## Environment variables

You can skip `init` / `login` and provide credentials by environment variables instead:

- `MERGEN_CONVERTER_CONFIG_FILE`
- `MERGEN_CONVERTER_OUTPUT_ROOT`
- `MERGEN_CONVERTER_USER_S3_ENDPOINT`
- `MERGEN_CONVERTER_USER_S3_REGION`
- `MERGEN_CONVERTER_USER_S3_BUCKET`
- `MERGEN_CONVERTER_USER_S3_PREFIX`
- `MERGEN_CONVERTER_USER_S3_ACCESS_KEY_ID`
- `MERGEN_CONVERTER_USER_S3_SECRET_ACCESS_KEY`
- `MERGEN_CONVERTER_USER_S3_SESSION_TOKEN`
- `MERGEN_CONVERTER_USER_S3_USE_PATH_STYLE`
- `MERGEN_CONVERTER_USERNAME`
- `MERGEN_CONVERTER_PASSWORD`

`MERGEN_CONVERTER_USERNAME` is used in the S3 key path. `MERGEN_CONVERTER_PASSWORD` is currently only stored locally for future auth flows.
