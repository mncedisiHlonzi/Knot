# Knot — Architecture

This document describes the _intended_ architecture. It describes direction, not
delivery: only the foundation exists today.

## Guiding constraint

Knot is built as a **modular monolith**, not microservices. We start with one deployable
backend, one mobile application, and one database, and split only when real pressure
justifies it.

> **Rule:** No microservices, no service mesh, no message queues, and no Kubernetes until
> an approved task explicitly calls for them.

## Systems

### Mobile — React Native + TypeScript

- React Native with TypeScript in **strict mode**.
- Located in `apps/mobile/`.
- Owns presentation and client-side state only. No business rules that the backend
  cannot enforce.
- Package manager: **npm**. No monorepo workspace tooling in the foundation.

### Backend — Go modular monolith

- Go, located in `backend/go/`, module path `github.com/knot/backend`.
- Layout:
  - `cmd/` — entry points (thin; wiring only).
  - `internal/` — application code that must not be imported externally.
  - `pkg/` — code safe for external import (kept empty until needed).
- Modular monolith: one binary composed of well-bounded internal modules. Module
  boundaries are enforced by package structure and review, not by network calls.

### Data — PostgreSQL (future)

- PostgreSQL is the intended primary datastore.
- **Not implemented yet.** No schema, migrations, or connections exist in the foundation.

## Future capabilities (explicitly deferred)

These are known future needs. None of them are implemented, and none of them should be
introduced without an approved task:

| Capability                          | Status  |
| ----------------------------------- | ------- |
| PostgreSQL schema & migrations      | Deferred |
| Authentication / identity           | Deferred |
| Redis, S3-compatible object storage | Deferred |
| FCM / APNs push                     | Deferred |
| WebSockets                          | Deferred (where justified) |
| Docker / GitHub Actions deploys     | Deferred |
| Machine learning / Knot Brain       | Deferred |

## Intelligence — last, not first

Intelligence is the **third** build order step, after the human network and the data
foundation. No embeddings, recommendation systems, or AI components are added
prematurely. Any future intelligence work is expected to be a separate, justified
component — possibly in Python — and never a shortcut around human adaptation.

## Boundaries summary

```
apps/mobile  ──HTTP──▶  backend/go  ──▶  PostgreSQL (future)
  (RN + TS)              (Go monolith)
```

One client, one backend, one database. Anything more is a future, approved decision.
