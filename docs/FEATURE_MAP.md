# Meta Pulse: capability map

Verified from the local checkout on 2026-09-29. This is a scoped navigation aid, not a complete product audit or a replacement for [existing guidance](../docs/ARCHITECTURE.md). Recheck implementation when using it; extend entries only as tasks confirm the relevant behavior. Do not treat roadmap ideas as implemented features.

| Capability | Entry / UI | API / orchestration | Implementation / data | Review boundary |
|---|---|---|---|---|
| Community navigation and adapters | [metar-frontend/production/src/router.js](../metar-frontend/production/src/router.js) | [metar-frontend/production/src/adapters.js](../metar-frontend/production/src/adapters.js) | [services/forum-plugin/user-center-pulse](../services/forum-plugin/user-center-pulse) | Production UI is separate from offline prototypes. Answer owns community identity and content. |
| Usage ingest and ledger | [services/pulse/cmd/api/routes.go](../services/pulse/cmd/api/routes.go) | [services/pulse/internal/service/usage_ingest.go](../services/pulse/internal/service/usage_ingest.go) | [services/pulse/internal/store/mysql](../services/pulse/internal/store/mysql) | new-api is the usage and funds source; keep ledger, idempotency and paid-funding boundaries. |
| Actions, rewards and settlement | [services/pulse/internal/service/action.go](../services/pulse/internal/service/action.go) | [services/pulse/internal/service/settlement.go](../services/pulse/internal/service/settlement.go) | [services/pulse/internal/adapter/newapi](../services/pulse/internal/adapter/newapi) | Do not put Pulse on the relay critical path or write new-api balances directly. |
| Deployment and frontend verification | [Makefile](../Makefile) | [deploy/update.sh](../deploy/update.sh) | [docs/IMPLEMENTATION_PLAN.md](../docs/IMPLEMENTATION_PLAN.md) | Existing milestone exits and production acceptance remain separate from release/build completion. |

Keep README, architecture, community and implementation-plan reading requirements. The root is a Go workspace, not a module; use existing explicit module test targets.

Before adding a feature, inspect adjacent flows and the current source of truth; shared filters, data definitions and access rules must not diverge across entry points.
