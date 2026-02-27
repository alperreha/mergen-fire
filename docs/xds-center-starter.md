# Mergen XDS Center (Starter)

`mergen-xds-center` is a starter control-plane helper. It does not replace `mergen-forwarder`.

## Current goals

- Reuse existing VM metadata under `MGR_CONFIG_ROOT`/`XDS_CONFIG_ROOT`.
- Resolve `<label>.<domain>` to VM target (`guestIP:httpPort`) with the same alias model as forwarder.
- Expose a small HTTP API for route listing and host resolution.
- Optionally push resolved route records to Consul KV.

## Why this helps now

- You can test control-plane flow without touching your current data-plane (`mergen-forwarder`).
- You get a concrete API and Consul integration surface before full Envoy xDS implementation.

## Commands

```bash
go run ./cmd/mergen-xds-center serve
go run ./cmd/mergen-xds-center resolve --host app1.vm.example.com
go run ./cmd/mergen-xds-center list-routes
go run ./cmd/mergen-xds-center sync-consul
```

## HTTP API

- `GET /healthz`
- `GET /v1/routes`
- `GET /v1/routes/resolve?host=<fqdn>`
- `POST /v1/consul/sync`

## Environment

- `XDS_HTTP_ADDR` (default `:18080`)
- `XDS_CONFIG_ROOT` (default `/var/lib/mergen/vm.d`)
- `XDS_NETNS_ROOT` (default `/run/netns`)
- `XDS_DOMAIN` (default `localhost`)
- `XDS_RESOLVER_CACHE_TTL_SECONDS` (default `5`)
- `XDS_REQUEST_TIMEOUT_SECONDS` (default `10`)
- `XDS_SHUTDOWN_TIMEOUT_SECONDS` (default `15`)
- `XDS_CONSUL_HTTP_ADDR` (optional, e.g. `http://127.0.0.1:8500`)
- `XDS_CONSUL_HTTP_TOKEN` (optional)
- `XDS_CONSUL_KV_PREFIX` (default `mergen/xds/routes`)
- `XDS_LOG_LEVEL` (default `info`)
- `XDS_LOG_FORMAT` (default `console`)

## Next step to real Envoy xDS

Use this starter as source-of-truth builder, then add an ADS gRPC module that maps `RouteRecord` set into Envoy resources (LDS/RDS/CDS/EDS) and cert delivery (SDS).
