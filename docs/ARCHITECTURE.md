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

## The Relevance Engine (KNOT-026)

The feed is a knowledge-discovery surface, not a social graph. Personalisation is built
on a deterministic, explainable engine with an explicit seam for a future model; no ML
runs today.

**The data pipeline.** Every significant action is appended to `user_interactions`
(migration `0019`): one row per view, full read, reaction, comment, and adaptation,
carrying the user, the entity (story or version), a timestamp, and the weight the Go
service applied. The weights live in `internal/interactions/log.go` as constants
(1 / 3 / 5 / 7 / 10), so the ladder is reviewable in one place and a stored row keeps the
weight that was in force when it happened. The write is **fire-and-forget**: the HTTP
layer detaches the request context, bounds the write with a timeout, runs it on its own
goroutine, and logs — but never surfaces — a failure. Logging can therefore neither
slow nor fail the action the reader took.

**The pluggable ranker.** `internal/relevance` defines

```go
type Ranker interface {
    Rank(ctx context.Context, userID string, candidates []StoryCandidate) ([]RankedStory, error)
}
```

The first implementation, `SimpleRanker`, is a pure function of its arguments plus an
injected clock. The caller fetches the reader's profile and engagement summary (the
weighted sum per pillar, joined to the read model below) and injects them at
construction; the ranker never touches the database. The ladder is base 1, +100 for the
reader's own content, +50 for a preferred language, +20 for the pillar they engage with
most, and −1 per hour since the candidate was written. Ordering is total and
deterministic (score, then recency, then id). An ML-backed implementation can satisfy the
same interface without changing a caller.

**The ML-ready read model.** `entity_features` (migration `0020`) denormalises the three
features a ranking join needs — language, pillar and place — for every story and version.
It is derived data: the migration backfills existing rows and
`stories.PostgresStore.BackfillEntityFeatures` can rebuild it; a future task will maintain
it on write. Its primary key is the entity id, so a candidate can be enriched with one
lookup.

## Boundaries summary

```
apps/mobile  ──HTTP──▶  backend/go  ──▶  PostgreSQL (future)
  (RN + TS)              (Go monolith)
```

One client, one backend, one database. Anything more is a future, approved decision.
