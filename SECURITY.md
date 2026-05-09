# Security Policy — RafikiClaw

> **RafikiClaw** is a hardened, local-first agent execution engine built for the Rafiki OS ecosystem.
> Report vulnerabilities via GitHub Issues with the label `security`.

## Supported Versions

| Version | Supported          |
| ------- | ------------------ |
| v0.1.x  | ✅ Security patches |

## Reporting a Vulnerability

If you discover a security vulnerability in RafikiClaw, please report it responsibly:

1. **Do NOT** open a public GitHub Issue.
2. Email the maintainer directly or use GitHub's private vulnerability reporting.
3. Include:
   - Description of the issue
   - Steps to reproduce
   - Affected version(s)
   - Any known mitigations

We aim to acknowledge reports within **48 hours** and provide a fix timeline based on severity.

## Security Model

RafikiClaw is built on a deny-by-default policy enforcement model:

| Control | Description |
|---------|-------------|
| **Network isolation** | No network access unless `agent.habitat.network.mode` explicitly allows it |
| **Secrets at runtime** | API keys injected via `--llm-api-key-env` / `--secret-env` only; never stored in artifacts |
| **Immutable capsules** | Once compiled, `.capsule` bundles cannot be modified without detection |
| **Deny-by-default mounts** | No host paths mounted unless explicitly declared in `agent.habitat.mounts` |
| **Container isolation** | All agent code runs inside Docker/Podman containers, never on the host |
| **Signed releases** | `rafikiclaw release --sign` produces tamper-evident provenance bundles |
| **JSONL audit logs** | Append-only event logs for compliance and incident response |

## Deny-By-Default Policy

The policy engine rejects any `.claw` file that requests:

- `--mount` / `--network` / `--env` CLI overrides at runtime (always blocked)
- Network mode `all` or `outbound` without explicit justification
- Environment variables not declared in `agent.habitat.env`

## POPIA / South African Data Compliance

RafikiClaw is designed to support South African POPIA (Protection of Personal Information Act) requirements:

- All audit logs are stored **locally** at `~/.rafikiclaw/logs/` — no external data transmission
- JSONL logs are append-only and can be exported for regulatory review
- Secrets are never written to logs
- Capsule artifacts can be signed for supply-chain integrity

## Dependency Security

- Go dependencies are pinned via `go.sum`
- Critical dependencies are reviewed at each upgrade cycle
- We recommend running `go mod verify` in CI to ensure no supply-chain tampering

## Container Security

- RafikiClaw uses rootless containers where possible (Podman on Linux)
- Seccomp and AppArmor profiles are respected when the runtime supports them
- Containers do not run in privileged mode unless explicitly required by the agent profile

## Prompt Injection Defenses

- Capability contracts validate skill permissions at compile time
- Skill files are not automatically executed; they must be declared in `agent.skills`
- The LLM proxy layer can be configured to filter or sanitize skill injections

## Security Best Practices (For Users)

1. Always use `--llm-api-key-env` instead of `--llm-api-key` to avoid key leakage in shell history
2. Prefer `network.mode: none` for agents that don't need internet access
3. Use `--secret-env` to inject only the specific env vars each agent needs
4. Run `rafikiclaw verify <capsule-dir>` before running untrusted artifacts
5. Enable secret scanning in your GitHub org to block accidental secret commits

## Past Security Advisories

*None yet — this is a new project. The upstream MetaClaw (fpp-125) had no published security advisories at time of fork.*

---

*Inspired by: https://github.com/fpp-125/metaclaw — MIT License*
*RafikiClaw is not affiliated with the upstream MetaClaw project.*