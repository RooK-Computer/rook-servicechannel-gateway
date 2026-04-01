# RooK Servicechannel Gateway

The RooK Servicechannel Gateway is the browser-facing terminal gateway for the RooK remote support workflow.

It is responsible for:

* accepting the browser connection,
* validating backend-issued terminal grants,
* preparing the server-side path towards the target console, and
* eventually bridging browser terminal traffic to the console.

This repository is still in an early implementation phase. Plan 03 (SSH bridge and terminal data path) is implemented and waiting for review. Hardening and delivery work remain for the next step.

## Current scope

The current codebase provides:

* a Go service bootstrap under `cmd/gateway`,
* explicit configuration loading via environment variables and an optional local config file,
* structured startup logging,
* `/healthz` and `/readyz` HTTP endpoints,
* a real `GET /gateway/terminal` WebSocket handshake that validates the terminal grant before upgrading,
* a central browser session registry with lifecycle tracking and cleanup hooks,
* a strict control-message parser for `input`, `resize`, `error`, and `close`-related flows,
* a server-side SSH and PTY bridge that connects the browser session to the console, and
* repository-native tests that cover the mock-backed browser-to-SSH MVP path without requiring the final backend integration.

The following are intentionally not implemented yet:

* production hardening and delivery assets.

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
* `GATEWAY_BACKEND_BASE_URL`

Optional settings with defaults:

* `GATEWAY_BACKEND_TIMEOUT` (default: `5s`)
* `GATEWAY_GRANT_HEADER_NAME` (default: `X-Rook-Terminal-Grant`)
* `GATEWAY_LOG_LEVEL` (default: `info`)
* `GATEWAY_SSH_PRIVATE_KEY_PATH` (default: `secrets/gateway_ssh_ed25519`)
* `GATEWAY_SSH_PUBLIC_KEY_PATH` (default: `secrets/gateway_ssh_ed25519.pub`)
* `GATEWAY_SSH_USERNAME` (default: `pi`)
* `GATEWAY_SSH_PORT` (default: `22`)
* `GATEWAY_SSH_CONNECT_TIMEOUT` (default: `5s`)
* `GATEWAY_SSH_INSECURE_IGNORE_HOST_KEY` (default: `true` for the current MVP)

For local development you can also point `GATEWAY_CONFIG_FILE` to a simple `KEY=VALUE` file. Environment variables override values from that file.

### Common commands

Run tests:

```bash
make test
```

Build all packages:

```bash
make build
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

At this stage `GET /gateway/terminal` performs a real WebSocket upgrade after validating the configured terminal-grant header against the backend and then opens a server-side SSH session to the target console. Authorization happens entirely in the handshake header; the active runtime protocol does not use `authorize` or `authorized` messages anymore.

For the current MVP, host-key verification is intentionally bypassed because the console host keys are not yet distributable in a verifiable way. This is a known hardening gap that must be revisited in the next plan.

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

The next action is to review the completed Plan 03 implementation. Plan 04 (hardening, operations, and delivery) should only start after explicit approval.
