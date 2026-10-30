# Repository Guidelines

## Project Overview

Chinese-language household ledger: an environment-bootstrapped `admin` creates members who share their household's accounts, income, expenses, and account-to-account transfers. PostgreSQL is the sole datastore; there is no SQLite or Redis dependency. User-facing text is Chinese.

## Architecture & Data Flow

- **Backend:** `backend/cmd/server/main.go` loads environment config, opens the pgx pool, applies embedded SQL migrations, bootstraps/updates the admin, then starts Gin with graceful shutdown. `api` owns HTTP validation/auth/response mapping; `store` owns SQL and transactions. Dependencies are constructor-injected via `api.New(cfg, store, logger)`, not a DI framework or ORM.
- **Frontend:** Vue 3 + TypeScript, Vue Router, Element Plus, and ECharts. `frontend/src/main.ts` restores authentication before routing. Views/dialogs call API wrappers through `api/client.ts`; requests use relative `/api/.../` URLs, session cookies, and CSRF headers.
- **State:** module-level Vue `reactive`/`ref` stores, not Pinia. `stores/auth.ts` owns session identity; `stores/data.ts` exposes `dataVersion` and `markDataChanged()` for cross-view refreshes after mutations. Async view loaders use sequence counters to discard stale responses.
- **Serving:** production Go serves `/api/*` and an external frontend build from `FRONTEND_DIST`; only SQL migrations are embedded. Vite development proxies `/api` from `127.0.0.1:5173` to `127.0.0.1:8000`. API paths must never fall back to the SPA.

## Key Directories

| Path | Responsibility |
| --- | --- |
| `backend/internal/api/` | Gin routes, session/CSRF middleware, request validation, SPA serving, HTTP integration tests |
| `backend/internal/store/` | Household-scoped SQL, balances/summaries, admin bootstrap, sessions; versioned SQL in `migrations/` |
| `backend/internal/money/`, `backend/internal/credentials/` | Exact cents conversion and credential policy/hash helpers |
| `backend/internal/config/` | Environment parsing and validation |
| `frontend/src/api/` | Wire types, endpoint wrappers, shared fetch/error/CSRF handling |
| `frontend/src/views/`, `components/` | Route pages and reusable mutation dialogs |
| `frontend/src/stores/`, `router/`, `utils/` | Shared reactive state, access guards, date/money formatting |
| `test/` | Manual build/run helper, not an automated test suite |
| `docs/changes/` | Per-task and per-requirement change records; naming and content conventions in `docs/changes/README.md` |

## Development Commands

Run commands from the indicated directory. Create a PostgreSQL database/user first. For backend development, export `DATABASE_URL` and an initial `ADMIN_PASSWORD` (at least 10 characters, at most 72 UTF-8 bytes); set `COOKIE_SECURE=false` for local HTTP.

| Task | Directory | Command |
| --- | --- | --- |
| Install frontend dependencies | `frontend/` | `npm ci` |
| Frontend dev server | `frontend/` | `npm run dev` |
| Type-check and build frontend | `frontend/` | `npm run build` |
| Preview frontend build | `frontend/` | `npm run preview` |
| Run backend for Vite | `backend/` | `LISTEN_ADDR=127.0.0.1:8000 go run ./cmd/server` |
| Build backend binary | `backend/` | `CGO_ENABLED=0 go build -trimpath -o household-bills ./cmd/server` |
| Backend tests; DB tests skip if unconfigured | `backend/` | `go test ./...` |
| Full backend tests | `backend/` | `TEST_DATABASE_URL='postgres://USER:PASSWORD@127.0.0.1:5432/household_bills_test?sslmode=disable' go test ./...` |
| Build/run containers | root | `docker compose up -d --build` |

There is no configured lint command. Format Go with `gofmt`; frontend `build` runs `vue-tsc --noEmit && vite build`, not lint or behavioral tests. Compose setup: copy `.env.example` to `.env`, supply `POSTGRES_PASSWORD` and `ADMIN_PASSWORD`, and set `COOKIE_SECURE=false` only for local HTTP.

## Code Conventions & Common Patterns

- **Money:** store/calculate integer cents in Go/SQL; use `money.Parse`/`money.Format` at JSON boundaries and `store/cents.go` for SQL aggregates. API money responses are decimal strings. Do not introduce floating-point accounting. Transfers are one row with source/destination accounts; they affect account balances but not income/expense totals.
- **Isolation/security:** derive household identity from the authenticated session, never request input; scope every store query by household. Protected routes check authentication before CSRF (missing session: `401`; authenticated CSRF failure: `403`). Preserve trailing-slash API routes; automatic slash redirects are disabled. Never log credentials, password hashes, or session tokens.
- **Backend patterns:** propagate request contexts; use explicit SQL transactions with deferred rollback and explicit commit. Wrap internal errors with `%w`, classify sentinel `Err...` values, log details server-side, and return generic client errors. Validation responses use field-to-string-array maps; general errors use `{"detail":"..."}`.
- **PATCH semantics:** pointers distinguish omitted fields; nullable transfer destinations additionally use `TransferAccountSet` to distinguish omission from explicit `null`. Preserve this distinction.
- **Schema changes:** add a new numbered migration such as `backend/internal/store/migrations/0004_description.sql`; do not rewrite applied migrations. Startup serializes migration/bootstrap work with advisory locks. Keep handler validation and database constraints aligned.
- **Frontend style:** `<script setup lang="ts">`, PascalCase component names, `*View.vue` route pages, camelCase functions, snake_case API fields, single-quoted TypeScript with no semicolons, scoped styles and `--hb-*` CSS tokens. Follow nearby formatting; there is no formatter config.
- **Frontend behavior:** call `markDataChanged()` after account/transaction mutations; retain stale-response guards in loaders. Shared `ApiError` handling lives in `api/client.ts`; add field labels there when introducing validation fields. A shared `401` handler clears auth state; do not treat CSRF `403` as logout or retry it automatically.

## Documentation Maintenance

- **MUST** store task-specific changes, requirement details, implementation decisions, and verification records in `docs/changes/`, not append them to the root `README.md` or a single growing changelog.
- **MUST** use one Markdown file per task or requirement, named `YYYY-MM-DD-topic.md` (creation date plus a concise lowercase, hyphen-separated topic). Update the existing file for follow-up work on the same task; create a separate file for an independent requirement.
- Write records in Chinese. Include background/goals, actual changes, verification performed and results, and compatibility or migration notes when applicable. Explicitly identify checks not run; never present planned work as completed or include secrets.
- Keep the root `README.md` focused on the project overview, setup, configuration, operations, and essential usage. Update it only when those instructions actually change; keep a stable link to `docs/changes/README.md` instead of adding an entry for every task.
- Keep this file focused on durable repository rules, not task history. Follow `docs/changes/README.md` for record organization; do not duplicate full records across documents.

## Important Files

- `backend/cmd/server/main.go`: process lifecycle; `backend/internal/api/server.go`: routing/middleware composition; `security.go` in the same directory: session/CSRF contract.
- `backend/internal/store/accounts.go`, `transactions.go`, `summary.go`: balance, transfer, and monthly-statistics invariants.
- `frontend/src/main.ts`, `frontend/src/router/index.ts`, `frontend/src/api/client.ts`: startup, route guards, and request/error behavior.
- `frontend/package.json`, `frontend/package-lock.json`, `frontend/vite.config.ts`, `frontend/tsconfig.json`: scripts, locked dependencies, proxy, strict TypeScript.
- `backend/go.mod`, `Dockerfile`, `compose.yaml`, `.env.example`: toolchains and deployment contract.
- `README.md`: setup/operations reference, but its “no transfers” statement and balance formula are stale; implemented transfer behavior in source/tests is authoritative.

## Runtime/Tooling Preferences

- Use Go **1.26.0+**, Node.js **22+**, **npm** with the committed lockfile, and PostgreSQL **17+**. Docker builds use Node 22 and Go 1.26. There is no Bun workflow.
- Go reads process environment only; it does **not** load `.env`. Docker Compose reads the root `.env`. `DATABASE_URL` is required; `ADMIN_PASSWORD` is required on first bootstrap and rotates the existing admin password when changed.
- `COOKIE_SECURE` defaults to `true`; use HTTPS outside trusted local testing. `ALLOWED_ORIGINS` is optional; otherwise preserve same-origin/Host behavior. `PORT` controls only Compose's host port (default `8080`), not the Go listener (`LISTEN_ADDR`, default `:8000`).
- `FRONTEND_DIST` defaults to `../frontend/dist`, relative to the process working directory. Build assets before serving with Go; `frontend/dist/` and `backend/household-bills` are generated, not source.
- `test/start.sh --build` builds and runs the binary; `--help` documents overrides. It neither reads `.env` nor creates/starts PostgreSQL. Override `DATABASE_URL`: the script contains local test credentials and defaults to LAN binding `0.0.0.0:9890` with insecure HTTP cookies. Do not reuse those credentials for deployment.
- Never use `docker compose down -v` as an ordinary stop: it deletes the database volume.

## Testing & QA

- Backend tests use standard Go `testing`, table-driven cases, `httptest`, real cookie jars, and real PostgreSQL rather than mocks. Reuse helpers in `backend/internal/api/integration_test.go`; filesystem fixtures use `t.TempDir`, environment cases use `t.Setenv`.
- **Integration tests truncate business tables.** Pre-create a dedicated disposable database for `TEST_DATABASE_URL`; never point it at development/production data. The guard rejects only identical URL strings, not aliases to the same database. Do not parallelize tests or separate test runs against the same database.
- Without `TEST_DATABASE_URL`, most API/security tests skip. Use `go test -v ./...` to see skips; a default pass does not prove API integration behavior. Money, aggregate parsing, config, and static-SPA tests run without a database.
- **Keep verification lightweight:** run only simple, change-focused tests or build/type checks as needed; do not run broad regression suites unless explicitly requested. Report checks run and any skips.
- **Do not use a browser for page-display or visual testing.** Leave page appearance and interactive UI verification to the user; address issues when the user reports them.
- No frontend test framework, CI pipeline, or coverage threshold is configured. A frontend build checks types and compilation, not page appearance or runtime UI behavior.
