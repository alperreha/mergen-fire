# Forwarder Design (HTTPS-Only Bridge)

This document describes the current `mergen-forwarder` behavior before Envoy/xDS integration.

## Goal

Route external HTTPS traffic from wildcard domains such as:

- `app1.localhost`
- `<uuid>.localhost`
- `app1.vm.example.com` (when prefix/suffix configured)

to HTTP services running inside Firecracker VMs.

## Flow

1. Client connects to forwarder HTTPS listener (`FWD_HTTPS_ADDR`, default `:443`).
2. Forwarder terminates TLS with wildcard certificate (`*.{prefix.}suffix`, configured at runtime).
3. Forwarder reads SNI (`ServerName`) from TLS handshake.
4. SNI label is resolved to VM metadata from `/etc/mergen/vm.d/<id>/meta.json`.
5. Forwarder reads VM `httpPort` from resolved metadata.
6. Forwarder enters VM netns (`${FWD_NETNS_ROOT}/<name>`, default `/run/netns`) for dial operation.
7. Forwarder dials `guestIP:httpPort`.
8. Bidirectional TCP stream copy starts and response flows back to client.

## Port Model

- Forwarder only listens on HTTPS (`FWD_HTTPS_ADDR`).
- Backend target is always `meta.httpPort` of the resolved VM.
- If VM has no valid `httpPort`, forwarder returns `502`.

## Domain Mapping Model

Runtime config:

- `FWD_DOMAIN_PREFIX` (optional)
- `FWD_DOMAIN_SUFFIX` (required)

Match rule:

- If prefix empty: `<label>.<suffix>`
- If prefix set: `<label>.<prefix>.<suffix>`

Examples:

- `FWD_DOMAIN_PREFIX=""`, `FWD_DOMAIN_SUFFIX="localhost"` -> `app1.localhost`
- `FWD_DOMAIN_PREFIX="vm"`, `FWD_DOMAIN_SUFFIX="example.com"` -> `app1.vm.example.com`

## SNI to VM Mapping

Resolver aliases:

- VM ID (full)
- VM ID short prefix (first 8 chars)
- `tags.host`, `tags.hostname`, `tags.app`, `tags.name`
- `metadata.host`, `metadata.hostname`, `metadata.app`, `metadata.name`

Example:

- SNI `app1.localhost` -> VM with `tags.app=app1`
- SNI `084604f6.localhost` -> VM with ID starting `084604f6`

## Linux Dependency

Namespace-based dialing uses `setns()` and works only on Linux hosts.

On non-Linux platforms, forwarder starts but netns dial returns an explicit error.

## Migration to Envoy Later

`mergen-forwarder` is a temporary bridge:

- Replace filesystem-based resolver with xDS/Consul source.
- Move L7 policies/retries/observability to Envoy.
- Keep VM metadata and lifecycle state model unchanged.
