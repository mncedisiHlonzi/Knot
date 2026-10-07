# Knot — Development

How to set up and run the Knot foundation locally.

## Prerequisites

| Tool       | Version      | Notes                                  |
| ---------- | ------------ | -------------------------------------- |
| Node.js    | 20 LTS or later | Mobile tooling and package manager   |
| npm        | Bundled with Node.js | The only package manager used   |
| Go         | 1.22 or later | Backend                                |
| Git        | Any recent version | Version control                    |

Docker is **not** required yet — it is a future, deferred concern.

## Repository layout

```
knot/
├── apps/mobile/     React Native + TypeScript app
├── backend/go/      Go modular monolith
├── docs/            Documentation
└── ...              Placeholder directories (see README.md)
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
go run ./cmd/knot   # prints a single startup line
```

The binary performs no I/O beyond printing its startup line. There is no HTTP server and
no database.

## Environment configuration

- All expected environment variable names live in `.env.example` at the repository root.
- **Convention:** names and placeholder values only. Real secrets are **never** committed.
- Copy `.env.example` to `.env` locally and fill in real values.
- `.env` and `.env.*` are git-ignored; `.env.example` is explicitly allowed.
- Variables for later phases (database, cache, secrets) are listed but **not used yet**.

```bash
cp .env.example .env
```

## Formatting and linting conventions

- **Prettier** formats the mobile app (see `apps/mobile/.prettierrc.json`).
- **ESLint** (flat config) lints the mobile app; TypeScript strict mode is on.
- **gofmt** is the single source of truth for Go formatting.
- `.editorconfig` defines shared whitespace rules (2 spaces; tabs for Go files).

## Continuous integration

`.github/workflows/ci.yml` runs on every push and pull request:

- **Mobile job:** install → lint → type-check → test.
- **Backend job:** gofmt check → `go vet` → `go test`.

There are **no deployment steps**. CI verifies the foundation only.
