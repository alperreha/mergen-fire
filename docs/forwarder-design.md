# Forwarder Design (Pre-Envoy)

This document describes the `mergen-forwarder` design used before Envoy/xDS integration.

## Goal

Route external TLS traffic from wildcard domains such as:

- `app1.localhost`
- `<uuid>.localhost`
- `app1.vm.example.com` (when prefix/suffix configured)

to running Firecracker guest services through VM network namespace.

## Flow

1. Client connects to forwarder TLS listener.
2. Forwarder terminates TLS with wildcard certificate (`*.{prefix.}suffix`, configured at runtime).
3. Forwarder reads SNI (`ServerName`) from TLS handshake.
4. SNI label is resolved to VM metadata from `/etc/mergen/vm.d/<id>/meta.json`.
5. Forwarder enters VM netns (`/var/run/netns/<name>`) for dial operation.
6. Forwarder dials guest IP + target guest port.
7. Bidirectional TCP stream copy starts.
8. Response flows back to client over the same TLS connection.

## Port model

Forwarder listener mapping is explicit:

- Example: `:8443=8080,:9443=443,:10022=22`
  - Left side: external listen address
  - Right side: guest port inside VM

Allowed guest ports are controlled by `FWD_ALLOWED_GUEST_PORTS` (default `22,8080,443`).

Note: listeners are TLS listeners. If you map `:10022=22`, client must connect with TLS to `:10022`; plain SSH protocol on raw TCP is not handled by this component.

## Domain mapping model

Runtime config:

- `FWD_DOMAIN_PREFIX` (optional)
- `FWD_DOMAIN_SUFFIX` (required)

Match rule:

- If prefix empty: `<label>.<suffix>`
- If prefix set: `<label>.<prefix>.<suffix>`

Examples:

- `FWD_DOMAIN_PREFIX=""`, `FWD_DOMAIN_SUFFIX="localhost"` -> `app1.localhost`
- `FWD_DOMAIN_PREFIX="vm"`, `FWD_DOMAIN_SUFFIX="example.com"` -> `app1.vm.example.com`

## SNI to VM mapping

Resolver aliases:

- VM ID (full)
- VM ID short prefix (first 8 chars)
- `tags.host`, `tags.hostname`, `tags.app`, `tags.name`
- `metadata.host`, `metadata.hostname`, `metadata.app`, `metadata.name`

Example:

- SNI `app1.localhost` -> VM with `tags.app=app1`
- SNI `084604f6.localhost` -> VM with ID starting `084604f6`

## Why TLS termination now

- Matches future production path where TLS is always present.
- Enables immediate HTTPS validation without waiting for Envoy integration.
- Keeps routing logic simple and observable.

## Linux dependency

Namespace-based dialing uses `setns()` and works only on Linux hosts.

On non-Linux platforms, forwarder starts but netns dial returns an explicit error.

## Migration to Envoy later

`mergen-forwarder` is a temporary bridge:

- Replace SNI resolver with xDS/Consul source.
- Move L7 policies/retries/observability to Envoy.
- Keep VM metadata and lifecycle state model unchanged.
