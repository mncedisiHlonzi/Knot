# Knot — Development

How to set up and run the Knot foundation locally.

## Prerequisites

| Tool           | Local machine      | CI      | Notes                                           |
| -------------- | ------------------ | ------- | ----------------------------------------------- |
| Node.js        | 24                 | 20      | Mobile tooling and package manager              |
| npm            | Bundled with Node.js | (from Node.js) | The only package manager used          |
| Go             | 1.27               | 1.22    | Backend; `backend/go/go.mod` declares `go 1.22`  |
| Docker Desktop | Any recent version | n/a     | Local PostgreSQL + Redis only (Docker Compose)  |
| Git            | Any recent version | n/a     | Version control                                 |
| actionlint     | Optional           | 1.7.7   | Lints `.github/workflows/`; pinned in CI        |

Docker is required **only** for local PostgreSQL and Redis. The mobile app and the Go
backend always run on the host — there is deliberately no application container.

### Version drift — read this before trusting the pins

Local tool versions and CI tool versions are **not** identical today:

- **Node:** local development runs **Node 24**; CI pins **Node 20**.
- **Go:** local development runs **Go 1.27**; `backend/go/go.mod` declares `go 1.22` and
  CI installs **Go 1.22**.

This drift is accepted for the foundation phase, but it means a green local run is **not**
proof that CI will be green.

> **The first CI run is the true test of the pins.** If a pin is wrong, CI is what fails —
> correct the pin rather than lowering the local toolchain.

## Repository layout

```
knot/
├── apps/mobile/            React Native + TypeScript app
├── backend/go/             Go modular monolith
├── docs/                   Documentation
├── infrastructure/docker/  Local Docker Compose definitions (Postgres + Redis)
├── scripts/                Developer scripts (dev-up / dev-down / dev-reset)
└── ...                     Placeholder directories (see README.md)
```

## Local infrastructure (PostgreSQL + Redis)

Local PostgreSQL and Redis run in Docker via
`infrastructure/docker/docker-compose.yml`. That file contains **no application
service** — the mobile app and the Go backend run on the host and reach the containers
over `localhost`.

- Images are pinned to specific minor versions (`postgres:16.4-alpine`,
  `redis:7.4.0-alpine`). `latest` is never used.
- Ports bind to `127.0.0.1` only, so nothing is exposed to your network.
- **PostgreSQL is published on host port `5433`, not the default `5432`.** This avoids a
  clash with any Postgres already installed on the developer's machine. Only the *host*
  mapping changed — the container's internal port is still `5432`. Redis stays on `6379`.
  Override the host port with `KNOT_POSTGRES_PORT` if `5433` is also taken.
- Data lives in the named volumes `knot_postgres_data` and `knot_redis_data`.
- The CI "Services smoke" job uses the **same pinned versions**, so local and CI do not
  drift apart.

### Developer scripts

| Script                 | What it does                                                       |
| ---------------------- | ------------------------------------------------------------------ |
| `scripts/dev-up.sh`    | Start PostgreSQL + Redis. Idempotent — safe to re-run.              |
| `scripts/dev-down.sh`  | Stop them. **Named volumes are preserved**, so data survives.       |
| `scripts/dev-reset.sh` | Stop them and **delete** the named volumes. Destructive.            |

```bash
scripts/dev-up.sh             # start Postgres + Redis
scripts/dev-down.sh           # stop, keep data
scripts/dev-reset.sh          # wipe all local data — prompts for confirmation
scripts/dev-reset.sh --yes    # wipe all local data without a prompt
```

`dev-reset.sh` is destructive: it requires the literal word `yes`, or the `--yes` flag.
All three scripts drive Docker Compose only — they never touch mobile or Go source, and
they exit with a clear message if the Docker CLI is unavailable.

To drive Compose directly instead:

```bash
docker compose -f infrastructure/docker/docker-compose.yml --env-file .env up -d
docker compose -f infrastructure/docker/docker-compose.yml --env-file .env down
docker compose -f infrastructure/docker/docker-compose.yml config   # syntax check only
```

## Mobile (`apps/mobile/`)

```bash
cd apps/mobile
npm install
npm run lint        # ESLint
npm run typecheck   # tsc --noEmit (strict)
npm test            # Jest
npm run format:check # Prettier
```

There is intentionally no runnable product app yet — the scaffold has no navigation and
no product screens.

## Backend (`backend/go/`)

```bash
cd backend/go
gofmt -l .          # should print nothing
go vet ./...
go test ./...
go test ./... -race
```

### Running the backend locally

The backend needs a reachable PostgreSQL. Start the local infrastructure first:

```bash
scripts/dev-up.sh                # Postgres on host port 5433, Redis on 6379
cd backend/go
go run ./cmd/knot migrate up     # apply pending migrations
go run ./cmd/knot                # serve the API on :8080
```

`knot` with no arguments starts the HTTP API. `knot migrate up` applies pending
migrations and exits. Migrations are **never** applied automatically when the server
starts, because schema changes should stay an explicit action.

The server logs its startup line, then one line per request containing `request_id`,
`method`, `path`, `status`, and `duration_ms`. Tokens, passwords, and DSNs are never
logged. It shuts down gracefully on SIGINT or SIGTERM, draining in-flight requests for
up to 10 seconds.

To try it by hand:

```bash
curl -s http://localhost:8080/health
curl -s -X POST http://localhost:8080/auth/register \
  -H 'Content-Type: application/json' \
  -d '{"email":"you@example.com","password":"at-least-8-chars","display_name":"You"}'
```

### Routes

| Route                 | Auth     | Handler file                  |
| --------------------- | -------- | ----------------------------- |
| `GET /health`         | public   | `internal/httpapi/router.go`  |
| `POST /auth/register` | public   | `internal/httpapi/auth_handler.go` |
| `POST /auth/login`    | public   | `internal/httpapi/auth_handler.go` |
| `POST /stories`       | `Bearer` | `internal/httpapi/stories_handler.go` |
| `GET /stories`        | public   | `internal/httpapi/stories_handler.go` |
| `GET /stories/{id}`   | public   | `internal/httpapi/stories_handler.go` |

Routes are declared in one place, `NewRouter` in `internal/httpapi/router.go`, using
Go 1.22 method-qualified `ServeMux` patterns. There is no router dependency.

Authentication is **per route**, not global: `POST /stories` is wrapped in
`AuthMiddleware.Require`, and the public routes are not. A handler that needs the caller
reads `UserIDFromContext(ctx)`; the second return value is `false` when no authenticated
user is on the context, which handlers must treat as unauthenticated. Because protection
travels with the route, adding a protected route means wrapping it explicitly.

`GET /stories/{id}` uses a wildcard segment, so be aware that `/stories/` (with a
trailing slash) does **not** match it — it is a 404, and the handler is never reached.
There is a test for exactly that, because the behaviour is easy to assume wrongly.

The composition root is `cmd/knot/main.go`: it builds the pool, the stores, the services,
the handlers, and the middleware, then hands them to `NewRouter`. That is the only place
that knows how the layers are wired together.

### Migrations

- SQL lives in `backend/go/migrations/`, named `<version>_<name>.up.sql` and
  `<version>_<name>.down.sql`, and is embedded into the binary with `go:embed`.
- Applied versions are recorded in the `schema_migrations` table, so re-running
  `migrate up` is safe and reports that the schema is already up to date.
- The runner only goes **up**. The `.down.sql` files exist so that a rollback is
  explicit and reviewable; there is no `migrate down` command.
- There is deliberately no third-party migration library.

## Backend configuration

`backend/go/internal/config` reads configuration from the environment. It uses the Go
standard library only, and it stores values without connecting to anything.

| Variable            | Required                        | Notes                                          |
| ------------------- | ------------------------------- | ---------------------------------------------- |
| `KNOT_ENV`          | No — defaults to `local`        | `local`, `ci`, or `test`                       |
| `KNOT_POSTGRES_DSN` | Only when `ci` / `test`         | Has a safe local default                       |
| `KNOT_REDIS_ADDR`   | Only when `ci` / `test`         | Has a safe local default                       |
| `KNOT_HTTP_PORT`    | No — defaults to `8080`         | Must be 1-65535 when set                       |
| `KNOT_LOG_LEVEL`    | No — defaults to `info`         | `debug`, `info`, `warn`, or `error`            |
| `KNOT_JWT_SECRET`   | Only when `ci` / `test`         | Must be at least 32 bytes outside `local`      |

- `local` (or unset): missing values fall back to the safe local defaults listed in
  `.env.example`. A missing JWT secret becomes the documented placeholder and the server
  logs a prominent warning.
- `ci` / `test`: the Postgres DSN, Redis address, and JWT secret are all **required**,
  the secret must be at least 32 bytes, and the backend fails fast naming every problem.
- Any other value: treated as required too, so an unrecognised environment never
  silently receives local defaults.

```bash
cd backend/go
KNOT_ENV=local go run ./cmd/knot                 # warns about the placeholder secret
KNOT_ENV=ci go run ./cmd/knot                    # fails fast naming the missing variables
```

Only the environment name and the port are ever logged. The DSN and the JWT secret may
carry credentials and are never logged.

## Environment configuration

- All expected environment variable names live in `.env.example` at the repository root.
- **Convention:** names and placeholder values only. Real secrets are **never** committed.
- Copy `.env.example` to `.env` locally and fill in real values.
- **`.env` is git-ignored; `.env.example` is tracked.** `.gitignore` ignores `.env` and
  `.env.*`, then re-allows `!.env.example`. You can confirm both facts at any time:

```bash
git check-ignore -v .env          # should report a matching ignore rule
git ls-files --error-unmatch .env.example   # should succeed (file is tracked)
```

- The active variables are the `KNOT_*` ones:
  - `KNOT_ENV`, `KNOT_POSTGRES_DSN`, `KNOT_REDIS_ADDR`, `KNOT_HTTP_PORT`,
    `KNOT_LOG_LEVEL`, `KNOT_JWT_SECRET` — read by the Go backend.
  - `KNOT_POSTGRES_USER`, `KNOT_POSTGRES_PASSWORD`, `KNOT_POSTGRES_DB`,
    `KNOT_POSTGRES_PORT`, `KNOT_REDIS_PORT` — consumed by
    `infrastructure/docker/docker-compose.yml`.
  - `KNOT_API_URL` — the API base URL for the mobile app, read by
    `apps/mobile/src/config/api.ts` when the bundler inlines `process.env`.
- The "reserved for later phases" block (`REDIS_URL`) is placeholder-only and **not used
  yet**. `DATABASE_URL` was removed in KNOT-002b and `JWT_SECRET` in KNOT-003, in favour
  of `KNOT_POSTGRES_DSN` and `KNOT_JWT_SECRET`.

```bash
cp .env.example .env
```

## npm install scripts (recorded from KNOT-001)

During the KNOT-001 verification run, `npm ci` in `apps/mobile/` **skipped the install
scripts** of some transitive dependencies. Two were observed explicitly:

- `@parcel/watcher` (2.6.0)
- `unrs-resolver` (1.12.2)

Both are present in `apps/mobile/package-lock.json`. They ship optional native binaries,
so a skipped install script can leave a missing or stale native binding on some machines.
In KNOT-001 the skips did **not** affect `lint`, `typecheck`, or `test`.

Practical guidance:

- A skipped install script is not automatically a failure — confirm the behaviour still
  works before changing anything.
- If a later task starts depending on file-watching or module-resolution performance,
  re-check whether these scripts need to run, and whether npm's install-script policy
  (its allow-list / `allowScripts` behaviour) is what is blocking them.
- Do not add `--ignore-scripts`, or change the install-script policy, without an approved
  task.

## Formatting and linting conventions

- **Prettier** formats the mobile app (see `apps/mobile/.prettierrc.json`).
- **ESLint** (flat config) lints the mobile app; TypeScript strict mode is on.
- **gofmt** is the single source of truth for Go formatting.
- `.editorconfig` defines shared whitespace rules (2 spaces; tabs for Go files).

## Testing — tampering with encoded values

**Never tamper with the final character of a base64-encoded value in a test.** The last
character of an encoding whose raw bytes are not a multiple of three carries fewer than
six significant bits, and Go's non-strict decoders ignore those padding bits. An edit that
changes only them decodes to byte-identical output, so the "tamper" is a no-op.

Instead, **decode the value, mutate the decoded bytes, and re-encode** — use
`testutil.TamperBase64Body` in `backend/go/internal/testutil/tamper.go`, which does that,
handles both base64 alphabets, and fails the test if the mutation changed nothing.

It lives in its own package rather than in a `_test.go` file because a helper in a
`_test.go` file is only visible to tests in that same package. `internal/testutil` is
importable by every test that needs it — the JWT and password tests in
`internal/identity` and the cursor test in `internal/stories` all use it today.

This is not hypothetical: it has already caused two flaky tests (a JWT signature's final
character, an argon2id key's final character), each failing about one run in sixteen.

## Continuous integration

`.github/workflows/ci.yml` runs on every push and pull request. It has four jobs:

- **Mobile job:** install → lint → type-check → test.
- **Backend job:** gofmt check → `go vet` → `go test`.
- **Workflows job:** `actionlint`, pinned to `v1.7.7`, lints `.github/workflows/`. It is
  installed from source with `go install`, so no third-party action has to be trusted and
  the version never floats.
- **Services smoke job:** starts PostgreSQL and Redis with GitHub Actions `services:`
  using the **same pinned image versions as** `infrastructure/docker/docker-compose.yml`,
  then proves reachability with `psql` (`SELECT 1;`) and `redis-cli ping`. It creates
  **no schema, no migrations, and no product tables**.

There are **no deployment steps**. CI verifies the foundation only.

> The first CI run is the real test of the version pins — see "Version drift" above.
