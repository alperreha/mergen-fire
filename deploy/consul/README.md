# Consul Starter Files

This directory contains starter Consul files for trying `mergen-xds-center`.

## Why these files are here

- `cmd/` is for Go binaries.
- `internal/` is for reusable Go modules.
- `deploy/consul/` is for runtime deployment manifests (Consul agent/service registration).
- `scripts/` should stay for shell automation helpers.

## Files

- `mergen-xds-center-service.hcl`: local Consul service registration for the XDS Center HTTP API.

## Quick local flow (dev)

1. Start Consul dev agent:

```bash
consul agent -dev -client=0.0.0.0
```

2. Register service:

```bash
consul services register ./deploy/consul/mergen-xds-center-service.hcl
```

3. Run XDS Center with Consul KV publishing enabled:

```bash
XDS_DOMAIN=vm.example.com \
XDS_CONSUL_HTTP_ADDR=http://127.0.0.1:8500 \
go run ./cmd/mergen-xds-center serve
```

4. Trigger one-shot sync to Consul KV:

```bash
go run ./cmd/mergen-xds-center sync-consul
```

5. Inspect a stored key:

```bash
consul kv get mergen/xds/routes/app1.vm.example.com
```

The stored JSON can be used by your next real xDS translator module (ADS/SDS/RDS/CDS/EDS producer).
