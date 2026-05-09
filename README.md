# RafikiClaw

> **Local-first, policy-enforced agent execution engine for Rafiki OS**

[![License: MIT](https://img.shields.io/badge/License-MIT-yellow.svg)](LICENSE)
[![Go Version](https://img.shields.io/badge/Go-1.21%2B-blue)](https://go.dev/)

RafikiClaw is a fork of [fpp-125/metaclaw](https://github.com/fpp-125/metaclaw), customized for the Rafiki OS ecosystem. It provides a secure, reproducible, auditable automation layer for AI agents — with POPIA-ready audit logs, South African data residency support, and native OpenClaw integration.

## Key Features

- **Deny-by-default habitat policies** — no network, mount, or env access unless explicitly declared
- **Immutable ClawCapsule artifacts** — compile once, verify always
- **Secret hygiene** — API keys injected at runtime via env vars, never stored in artifacts
- **Container isolation** — Docker / Podman / Apple Container runtimes
- **Capability contracts** — skill permissions validated at compile time
- **Signed releases** — tamper-evident provenance with Ed25519
- **POPIA-ready audit logs** — append-only JSONL logs stored locally at `~/.rafikiclaw/`
- **Multi-language skill support** — extensible for Zulu/Afrikaans skill contracts

## Quick Start

Prereqs:
- Go 1.21+
- Docker, Podman, or Apple Container runtime
- `git`, `jq` (optional)

```bash
# Build the binary
git clone https://github.com/gmmakoele-ship-it/rafikiclaw.git
cd rafikiclaw
go build -o ./bin/rafikiclaw ./cmd/metaclaw

# Run a health check
./bin/rafikiclaw doctor --runtime=auto

# Initialize a new agent
./bin/rafikiclaw init my-agent.claw

# Validate a .claw file
./bin/rafikiclaw validate my-agent.claw

# Compile to a capsule
./bin/rafikiclaw compile my-agent.claw

# Run an agent
./bin/rafikiclaw run my-agent.claw \
  --llm-api-key-env=OPENAI_API_KEY \
  --runtime=docker
```

## Architecture

```
rafikiclaw (Go CLI)
├── cmd/              — CLI entry point (run, init, validate, doctor, compile, release...)
├── internal/
│   ├── cli/          — Command handlers, flags
│   ├── compiler/     — .claw file → ClawCapsule IR
│   ├── policy/       — Deny-by-default habitat policies
│   ├── runtime/      — Docker / Podman / Apple Container adapters
│   ├── capsule/      — Immutable artifact bundle creation
│   ├── signing/      — Ed25519 key generation and signing
│   ├── store/sqlite/ — Lifecycle state and run history
│   └── logs/         — Append-only JSONL event logs
└── docs/             — Architecture diagrams
```

## Configuration

- **State dir:** `~/.rafikiclaw/` (or `.rafikiclaw` in project)
- **SQLite state:** `~/.rafikiclaw/state.db`
- **Audit logs:** `~/.rafikiclaw/logs/*.jsonl`
- **Capsules:** `~/.rafikiclaw/capsules/`
- **Keys:** `~/.rafikiclaw/keys/`

## `.claw` File Format

```yaml
apiVersion: rafikiclaw/v1
kind: Agent
agent:
  name: hello-rafiki
  species: nano
  lifecycle: ephemeral
  habitat:
    network:
      mode: none       # none | outbound | all
    mounts: []
    env: {}
  runtime:
    target: docker
    image: alpine:3.20
  command:
    - sh
    - -lc
    - echo "Hello from RafikiClaw!"
```

## For Rafiki OS Agents

RafikiClaw is the trusted execution engine for Floki and other Rafiki OS agents. See the [Rafiki OS documentation](https://github.com/gmmakoele-ship-it/rafiki-openclaw-workspace) for integration details.

## Security

See [SECURITY.md](SECURITY.md) for the full security model, POPIA compliance notes, and vulnerability reporting process.

## License

MIT License. See [LICENSE](LICENSE). RafikiClaw is not affiliated with the upstream metaclaw project.

## Upstream

Forked from [fpp-125/metaclaw](https://github.com/fpp-125/metaclaw) (MIT License) — we track upstream changes and will contribute security improvements back where appropriate.