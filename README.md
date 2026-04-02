# RooK Servicechannel Gateway

The RooK Servicechannel Gateway is the browser-facing terminal gateway for the RooK remote support workflow.

It is responsible for:

* accepting the browser connection,
* validating backend-issued terminal grants,
* preparing the server-side path towards the target console, and
* eventually bridging browser terminal traffic to the console.

This repository is in the review phase for Plan 06. Plans 01-05 are implemented and reviewed; the gateway currently uses a browser-compatible WebSocket authorization message instead of a custom handshake header.

## Current scope

The current codebase provides:

* a Go service bootstrap under `cmd/gateway`,
* explicit configuration loading via environment variables and an optional local config file,
* structured startup logging,
* `/healthz` and `/readyz` HTTP endpoints,
* a real `GET /gateway/terminal` WebSocket handshake that upgrades first and then expects an initial `authorize` message carrying the terminal grant,
* a central browser session registry with lifecycle tracking and cleanup hooks,
* a strict control-message parser for `authorize`, `authorized`, `input`, `resize`, `error`, and `close`-related flows,
* a server-side SSH and PTY bridge that connects the browser session to the console, and
* repository-native tests that cover the mock-backed browser-to-SSH MVP path without requiring the final backend integration.

The following are intentionally not fully closed yet:

* real Debian install/runtime validation on a Debian development machine,
* host-key verification hardening beyond the current MVP compromise.

## Repository structure

Key directories at this stage:

* `cmd/gateway/` - service entrypoint
* `internal/config/` - configuration loading and validation
* `internal/httpserver/` - HTTP runtime and browser WebSocket handshake
* `internal/grants/` - backend terminal grant validation client
* `internal/session/` - browser session lifecycle management
* `internal/websocket/` - WebSocket upgrade, frame handling, and protocol parsing
* `internal/sshbridge/` - SSH and PTY bridge to the target console
* `internal/audit/` - follow-up interfaces for later plans
* `plans/` - implementation plans and review gates
* `spec/` - architecture, OpenAPI contracts, and cross-component status documents
* `secrets/` - local-only sensitive development artifacts, intentionally not committed

## Local development

### Prerequisites

* Go 1.26 or newer

### Required configuration

The service currently expects at least:

* `GATEWAY_LISTEN_ADDRESS`
* `GATEWAY_HTTP_READ_HEADER_TIMEOUT`
* `GATEWAY_BACKEND_BASE_URL`

Optional settings with defaults:

* `GATEWAY_BACKEND_TIMEOUT` (default: `5s`)
* `GATEWAY_LOG_LEVEL` (default: `info`)
* `GATEWAY_SSH_PRIVATE_KEY_PATH` (default: `secrets/gateway_ssh_ed25519`)
* `GATEWAY_SSH_PUBLIC_KEY_PATH` (default: `secrets/gateway_ssh_ed25519.pub`)
* `GATEWAY_SSH_USERNAME` (default: `pi`)
* `GATEWAY_SSH_PORT` (default: `22`)
* `GATEWAY_SSH_CONNECT_TIMEOUT` (default: `5s`)
* `GATEWAY_SSH_INSECURE_IGNORE_HOST_KEY` (default: `true` for the current MVP)
* `GATEWAY_SESSION_AUTHORIZE_TIMEOUT` (default: `2m`)
* `GATEWAY_SESSION_IDLE_TIMEOUT` (legacy fallback alias for `GATEWAY_SESSION_AUTHORIZE_TIMEOUT`)
* `GATEWAY_SESSION_MAX_CONCURRENT` (default: `32`)
* `GATEWAY_SESSION_OUTBOUND_QUEUE_DEPTH` (default: `16`)
* `GATEWAY_WEBSOCKET_MAX_MESSAGE_BYTES` (default: `65536`)
* `GATEWAY_WEBSOCKET_KEEPALIVE_INTERVAL` (default: `30s`)
* `GATEWAY_WEBSOCKET_KEEPALIVE_TIMEOUT` (default: `75s`)

For local development you can also point `GATEWAY_CONFIG_FILE` to a simple `KEY=VALUE` file. Environment variables override values from that file.

### Common commands

Run tests:

```bash
make test
```

Run the dedicated end-to-end suite:

```bash
make test-e2e
```

Build all packages:

```bash
make build
```

Run the full verification path:

```bash
make verify
```

Run the gateway locally:

```bash
GATEWAY_LISTEN_ADDRESS=127.0.0.1:8080 \
GATEWAY_BACKEND_BASE_URL=https://backend.example.test \
make run
```

Check the health endpoints:

```bash
curl http://127.0.0.1:8080/healthz
curl http://127.0.0.1:8080/readyz
```

At this stage `GET /gateway/terminal` performs a real WebSocket upgrade without requiring a custom auth header. The browser must then send `{"type":"authorize","token":"..."}` as its first protocol message. After backend validation and successful SSH session setup, the gateway responds with `{"type":"authorized"}` and starts the terminal data flow.

For the current MVP, host-key verification is intentionally bypassed because the console host keys are not yet distributable in a verifiable way. This is a known hardening gap that must be revisited in the next plan.

The gateway now also enforces basic operating limits:

* HTTP header read timeout
* session inactivity timeout
* maximum concurrent gateway sessions
* bounded outbound queue depth per session
* maximum WebSocket message size

## Local end-to-end verification

The repository now contains a reproducible local end-to-end suite in `tests/e2e/`.

It uses:

* a mock backend HTTP server for terminal-grant validation,
* a local in-process SSH test server, and
* a WebSocket client as the browser stand-in.

Covered paths currently include:

* successful browser -> gateway -> SSH echo flow,
* backend unavailable during `authorize`,
* SSH connection failure after a valid grant, and
* idle-session timeout handling.

Run it with:

```bash
make test-e2e
```

## Operations and deployment

The repository now includes a first `systemd` delivery path:

* unit file: `deploy/systemd/rook-servicechannel-gateway.service`
* example environment file: `deploy/systemd/gateway.env.example`

It also now includes an `nfpm`-based Debian packaging path:

* package definition: `nfpm.yaml`
* maintainer scripts: `packaging/nfpm/`

The example environment file intentionally contains no secrets. For real deployment, move the private/public SSH key pair into an external secret store or secret mount and point:

* `GATEWAY_SSH_PRIVATE_KEY_PATH`
* `GATEWAY_SSH_PUBLIC_KEY_PATH`

to the mounted file paths.

### Minimal runbook

Build and verify before rollout:

```bash
make verify
```

Build a Debian package:

```bash
make package
```

Inspect the generated package archive without installing it:

```bash
make package-inspect
```

By default the package architecture follows the local `go env GOARCH`. Override it when needed, for example:

```bash
make package PACKAGE_ARCH=amd64 PACKAGE_VERSION=0.1.0
```

The package installs the binary, the `systemd` unit, and an example environment file, but it **does not automatically enable or start** the service.

Because this repository is often worked on from macOS, the packaging flow is designed so that:

* package build works via `go run ... nfpm` without a local Debian toolchain,
* package archive contents can be inspected locally via `ar` and `tar`,
* real install/runtime validation on Debian can be done later by a teammate on a Debian machine.

Basic runtime checks after start:

```bash
curl http://127.0.0.1:8080/healthz
curl http://127.0.0.1:8080/readyz
```

Typical failure indicators:

* `backend_unreachable` means grant validation could not reach the backend.
* `ssh_bridge_failed` means the grant was valid but the server-side SSH session could not be opened.
* `authorize_timeout` means the browser opened the websocket but did not send the initial `authorize` message before the configured authorization timeout.
* `keepalive_timeout` means the websocket transport stopped answering server-side keepalive expectations.
* `session_limit_reached` means the configured maximum number of concurrent sessions is exhausted.

## Planning and specifications

This repository follows a sequential implementation flow with mandatory review gates.

Start here:

* `plans/README.md`
* `plans/01-runtime-grundgeruest-und-backend-validierung.md`

The `spec/` submodule is the contract source for architecture and API expectations, especially:

* `spec/docs/architecture/servicechannel-concept.md`
* `spec/openapi/04-browser-gateway-websocket.openapi.yaml`
* `spec/openapi/05-gateway-console-ssh.openapi.yaml`
* `spec/openapi/06-backend-gateway-terminal-grant.openapi.yaml`
* `spec/implementation/05-browser-terminal-gateway-status.md`

## Status and next step

The next action is to review the completed Plan 06 implementation before any further follow-up work starts.
