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
| PostgreSQL schema & migrations      | Implemented (migrations `0001`–`0021`) |
| Authentication / identity           | Foundation only (KNOT-030); the service is KNOT-031 |
| Redis, S3-compatible object storage | Implemented (`internal/cache`, `internal/storage`) |
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

## Identity & Authentication (KNOT-030)

Knot's identity layer is being rebuilt from "one password hash per row and a stateless JWT"
into a domain model with sessions, credentials and verification tokens. KNOT-030 adds **the
storage and the boundaries only**: no endpoint changes, no service, no token minting, no
email. Registration and sign-in behave exactly as they did before it.

**The model.** Four tables, three of them new in migration `0021`:

| Table                 | Holds                                                        | Decided by    |
| --------------------- | ------------------------------------------------------------ | ------------- |
| `users`               | The account, plus the soft-delete columns `deleted_at` and `deletion_scheduled_at` | KNOT-ADR-079, KNOT-ADR-081 |
| `credentials`         | One row per way a user can prove who they are — `password`, `oauth_google`, `magic_link` — unique per user and type | KNOT-ADR-079 |
| `sessions`            | One row per signed-in device, keyed by the hash of an opaque refresh token, with `revoked_at` | KNOT-ADR-080 |
| `verification_tokens` | Single-use, expiring proofs of address ownership, stored as hashes | KNOT-ADR-079 |

**Secrets are hashes.** Neither a refresh token nor a verification token exists in the
database: `sessions.refresh_token_hash` and `verification_tokens.token_hash` hold SHA-256
digests, so a backup cannot be replayed. `users.password_hash` is deprecated but still
present and still NOT NULL, because removing it in the same migration that introduces
`credentials` would make a rollback lose every password.

**Two packages, two jobs.** `internal/auth` owns the three store interfaces
(`SessionStore`, `CredentialStore`, `VerificationTokenStore`) and their pgx implementations.
It deliberately contains no service: minting, hashing, rotating and reuse detection are
policy, and KNOT-031 puts them in one `AuthService` above these stores. Until then the
interfaces exist and nothing calls them.

`internal/cache` owns Redis. It exposes seven operations — `Get`, `Set`, `SetEX`, `Delete`,
`Incr`, `Ping`, `Close` — behind one `Cache` interface, and is the only package that opens a
socket to Redis (KNOT-ADR-082). A missing key is reported as an error wrapping `ErrMiss`, so
a cache outage can never be mistaken for "the user has no session".

**Where each store lives.**

| State                                            | Store         | Why |
| ------------------------------------------------ | ------------- | --- |
| Accounts, credentials, sessions, verification tokens | PostgreSQL | Must survive a flush and be queryable |
| Rate-limit counters, cached reads, refresh-token state | Redis     | Ephemeral, expiring, shared by instances |

**Configuration.** `KNOT_REDIS_ADDR`, `KNOT_REDIS_PASSWORD` and `KNOT_REDIS_DB` are read by
`internal/config`; the address has a local default, the password is optional (the local
stack has none) and the database number defaults to 0. `cmd/knot` constructs the client at
startup and closes it on shutdown without using it yet, so a bad address fails on boot
rather than at the first sign-in.

## The precomputed feed (KNOT-036)

The default feed is the product's hottest read and its costliest query: the merged
`UNION ALL` of stories and adaptations plus five batched lookups and four count
statements. KNOT-036 moves that work from read time to write time (KNOT-ADR-093).

**One sorted set per reader.** `knot:feed:{user_id}` holds story ids scored by
`created_at`, trimmed to `KNOT_FEED_CAP` (800) on every insert. A read is one
`ZREVRANGE` plus one batched row fetch, instead of nine to eleven statements.

**Hybrid fan-out.** Creating a story resolves its audience in a single bounded
statement — readers whose primary Rooted place (or its country) matches the
author's, or whose `preferred_languages` contains the story's language, minus the
author and deleted accounts:

- **normal authors** — the story id is written into every target reader's set;
- **high-fanout authors** (audience above `KNOT_FEED_FANOUT_THRESHOLD`) — the
  story is written **once** to `knot:author_feed:{author_id}` and merged in at
  read time.

The fan-out runs on a detached, bounded goroutine from the publish handler. It is
best-effort by design: a Redis failure is logged at ERROR and never fails a
publish, because a stored story is a story.

**Read path.** `GET /stories` on the first page, authenticated, with the cache
wired:

| Situation | Answer |
| --- | --- |
| ids in the reader's set | `stories.StoriesByIDs` (one query), `next_cursor` empty |
| empty set, anonymous reader, or cursor present | the SQL feed, unchanged |
| Redis or the row read failed | the SQL feed, unchanged |

A miss for an authenticated reader is repaired off the request path: the page
PostgreSQL just served is written back into the set. An anonymous read never
writes.

**Boundaries.** `internal/feedcache` owns the keys, the cap and the fan-out rules;
`internal/cache` gained a two-method `SortedSet` view over the **same** Redis
client, so there is still one connection pool and one package that opens a socket.
The `for_you` ranked feed is untouched and always uses SQL.

**Known limits** (full list in KNOT-ADR-093): the cached page carries story
entries only — adaptation entries return on page two or with the cache off; there
is no cursor on the cached path; story deletion does not remove a cached id (the
read skips ids that no longer resolve); and the language half of the audience
query is a `users` scan until `preferred_languages` is indexed.

## Boundaries summary

```
apps/mobile  ──HTTP──▶  backend/go  ──┬──▶  PostgreSQL
  (RN + TS)              (Go monolith)└──▶  Redis (ephemeral)
```

One client, one backend, one database, and one cache for state that is allowed to disappear.
Anything more is a future, approved decision.
