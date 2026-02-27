# Envoy Compose Starter

This starter keeps your existing `mergen-forwarder` unchanged and puts Envoy in front as an L4 TLS passthrough edge.

## What it does

- Envoy listens on `:8443`.
- Envoy forwards raw TLS stream to `host.docker.internal:443`.
- `mergen-forwarder` still performs TLS termination + SNI resolution + netns-aware backend dial.

## Why this mode first

- Zero change to current forwarder/business logic.
- Safe first step before full ADS/xDS control-plane implementation.

## Dynamic push note

Current compose stack syncs VM route records into Consul KV (`mergen-xds-sync`), but Envoy config here is static passthrough.

For real dynamic Envoy updates (LDS/RDS/CDS/EDS/SDS), you need an ADS-compatible Go service that speaks Envoy xDS gRPC. `mergen-xds-center` today is a route catalog + HTTP API starter, not a full ADS server yet.
