# OmniMonitor Harness Report

## 1. Project Purpose and Module Layout

### Purpose
OmniMonitor is a **self-hosted, privacy-focused monitoring platform** that consolidates telemetry from:
- Cloud servers (AWS, Azure)
- Bare-metal/Linux/macOS machines (via push agents)
- Media servers (Jellyfin, Subsonic, Plesk)
- DNS ad-blockers (AdGuard Home)

It provides a **unified real-time dashboard** with automatic alerting, live logs, and role-based access control (RBAC). All data remains on-premises, with no cloud dependency.

### Module Layout

#### Core Directories
- **`cmd/`**: Entrypoints for the server, agent, and database migrations.
- **`internal/`**: Core business logic, organized by domain:
  - `storage/`: TimescaleDB repositories (metrics, hosts, alerts, logs).
  - `ingest/`: gRPC/mTLS ingestion pipeline with rate limiting.
  - `alerting/`: Rule evaluator, state machine, and auto-provisioner for default alerts.
  - `cloudpoll/`: AWS/Azure/XenAPI/Plesk pollers (decrypts credentials from vault).
  - `ws/`: WebSocket hub for live browser streaming.
  - `auth/`: JWT/RBAC middleware.
  - `vault/`: AES-256-GCM encrypted credential storage.

#### Agents
- **`jellyfin-agent/`**, **`subsonic-agent/`**, **`dns-agent/`**: Standalone gateways that translate media/DNS APIs into OmniMonitor’s gRPC protocol.
- **`cmd/agent/`**: Push agent for bare-metal/OpenVZ metrics.

#### Frontend
- **`web/`**: SvelteKit 2 + Svelte 5 frontend with 5 switchable themes (TailwindCSS + Chart.js).

#### Infrastructure
- **`deploy/`**: Docker Compose, certs, and `.env` templates.
- **`migrations/`**: Goose SQL migrations for TimescaleDB.
- **`scripts/`**: Enrollment helpers (e.g., `enroll-jellyfin-agent.sh`).

---

## 2. Jev Integration
OmniMonitor uses **Jev** (jev-1.13.0) for tool routing in its harness tests. Key integration points:

- **Tool Selection**: Jev routes tasks to the appropriate MCP tool (e.g., `filesystem`, `docker`) based on confidence scores.
- **Harness Tests**: Jev decisions are recorded in test reports (e.g., `harness_report.md`) to validate routing logic.
- **Safety**: Jev is consulted **before** executing side effects (e.g., `run_command`).

Example workflow:
```
1. Harness test calls `jev_route("verify local storage health", ["filesystem", "sqlite"])`
2. Jev returns `filesystem` (confidence 0.98).
3. Harness executes filesystem checks (e.g., disk space, permissions).
```

---

## 3. Jev Routing Results

| Scenario                          | Tools Offered               | Jev Decision | Confidence | Probabilities               |
|----------------------------------|-----------------------------|--------------|------------|-----------------------------|
| verify local storage health      | filesystem, sqlite          | filesystem   | 0.98       | filesystem=0.99, sqlite=0.01 |
| inspect container deployment     | docker, filesystem, github  | docker       | 0.99       | docker=0.99, filesystem=0.01, github=0.00 |

---

## 4. Summary
- **Purpose**: Unified, self-hosted monitoring with privacy guarantees.
- **Architecture**: gRPC ingestion → TimescaleDB → REST/WS API → SvelteKit frontend.
- **Jev Role**: Routes harness tasks to the correct tool (e.g., `docker` for containers).
- **Next Steps**: Expand harness tests to cover cloud pollers and alerting scenarios.