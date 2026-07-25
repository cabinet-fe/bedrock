# Bedrock (磐石)

[中文](./README.md) | [English](./README.en.md)

A **project development foundation** platform: host operations, **CI/CD delivery**, **product collaboration** (requirements and API docs), and **AI agents with open Agent Skills**—so teams can close the loop from code → build → deploy → collaborate → automate.

> 2.0 supports **fresh installs only**. There is **no** 1.x → 2.0 data migration.

## Features

| Domain | Capabilities |
| --- | --- |
| **Dashboard** | Configurable overview; cards and menus are RBAC-gated |
| **CI/CD** | Repositories, build jobs/runs, artifact download, Webhook/Cron; distribute via rsync / sftp / scp / local / Deploy Agent |
| **Resources** | Shared repositories, servers, credentials, AI CLI runtimes, personal access tokens (PAT) |
| **Projects** | Product projects, member ACL, requirements, Markdown API doc trees; loosely coupled with CI/CD |
| **AI** | Agent orchestration, async Agent Runs, open [Agent Skills](https://agentskills.io/specification) library |
| **Ops** | Process management, dev environments (Go / Node / Java / Python, etc.); write ops are super-admin only |
| **System** | Users, multi-role RBAC, dictionaries, operation logs |

## Architecture

```text
┌─────────────────────────────────────────────┐
│  Bedrock Server (single binary)             │
│  - Go API / WebSocket / scheduler / Cron    │
│  - Frontend assets embedded                 │
│  - Local builds + local AI CLI              │
└──────────────────────┬──────────────────────┘
                       │ rsync / sftp / scp / agent / local
                       ▼
┌─────────────────────────────────────────────┐
│  Target host + optional Deploy Agent        │
│  (separate binary)                          │
└─────────────────────────────────────────────┘
```

| Component | Support |
| --- | --- |
| Server (production) | Linux amd64 / arm64 |
| Server (development) | macOS |
| Deploy Agent | Linux / Windows (shipped separately, same version as Server) |
| Database | `sqlite` (default, zero external deps), `postgres` / `postgresql`, `mysql` |
| Frontend | Embedded in Server for production; Vite proxy in development |

## Quick deploy (release package)

For production or a trial: use a prebuilt binary with default SQLite.

```bash
# 1. Obtain release artifacts (example: Linux amd64)
#    bedrock-linux-amd64, bedrock-agent-linux-amd64 (+ .sha256)

# 2. Prepare data directory and config
mkdir -p ./data
cp config.example.yaml config.yaml

# 3. Before production, change at least:
#    - encryption.key (64 hex chars)
#    - jwt.secret
#    - admin.username / admin.password (seeded super admin on first boot)

# 4. Start (empty DB → migrations → seed admin)
./bedrock-linux-amd64 --config ./config.yaml

# 5. Verify
curl -fsS http://127.0.0.1:8080/api/v1/health
# Open http://<host>:8080 and sign in with the admin account
```

To use PostgreSQL or MySQL, change `database.driver` and connection settings, then restart—**no rebuild required**. Switching drivers does **not** migrate data; move data yourself if needed.

For install, backup, rollback, and Deploy Agent details, see [docs/ops-handbook.md](./docs/ops-handbook.md).

## Build and run from source

### Requirements

- Go **1.26+**
- [Bun](https://bun.sh/) for the frontend (`vp` workflow used by this repo)
- Optional: PostgreSQL / MySQL

### Development

```bash
# Prepare config (encryption.key must match the frontend inject key)
cp config.example.yaml config.yaml

# Backend on :8080 (-tags dev) + Vite frontend proxy
make dev
```

Vite proxies API calls to the backend; open the local URL printed by Vite.

### Production-style binary

```bash
make build                 # web → cmd/server/dist → ./bedrock
./bedrock --config ./config.yaml

# Cross-compile
make build-linux           # bedrock-linux-amd64
make build-linux-arm64     # bedrock-linux-arm64
make build-agent-linux
make build-agent-linux-arm64
make checksums
```

### Checks

```bash
go test ./...
cd web && bun install && vp check
make smoke                 # fresh-install + api-e2e + recovery + 3 DB + linux package
make ga-guardrails         # reject treating 1.x migration as a supported path
```

## Configuration highlights

See [`config.example.yaml`](./config.example.yaml) for a full example.

| Key | Notes |
| --- | --- |
| `server.host` / `server.port` | Default `0.0.0.0:8080` |
| `database.driver` | `sqlite` \| `postgres` \| `mysql` |
| `database.path` | SQLite file (default `./data/bedrock.sqlite`) |
| `encryption.key` | 64 hex chars; must change in production; must match `VITE_BEDROCK_ENCRYPTION_KEY` in dev |
| `admin.*` | Super admin seeded on first empty-DB start |
| `build.*` / `storage.root` | Workspace, artifacts, logs, object storage dirs |

Override with `BEDROCK_`-prefixed environment variables (Viper).

## Documentation

| Doc | Purpose |
| --- | --- |
| [docs/PRD.md](./docs/PRD.md) | Product requirements (source of truth) |
| [docs/DESIGN.md](./docs/DESIGN.md) | Technical design (source of truth) |
| [docs/ops-handbook.md](./docs/ops-handbook.md) | Install, backup, upgrade, rollback |
| [docs/release-checklist.md](./docs/release-checklist.md) | Release checklist |
| [api/README.md](./api/README.md) | HTTP API contracts by domain |
| [AGENTS.md](./AGENTS.md) | Repo collaboration & common commands |

## Security boundaries

1. **Same-UID execution**: Build scripts, AI CLIs, and custom super-admin commands run as the Bedrock process user. RBAC is **not** an OS sandbox.
2. **HTTP & sessions**: Prefer HTTPS in production. `access_token` lives in Web Storage; `refresh_token` is an HttpOnly cookie (Secure is not set).
3. **Custom commands**: Super-admin only; keep least privilege and audit.

See [docs/DESIGN.md](./docs/DESIGN.md) and [docs/ops-handbook.md](./docs/ops-handbook.md).
