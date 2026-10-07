# Knot — Roadmap

The build order is fixed: **Human Network First → Data Foundation Second → Intelligence
Third.** Phases below are directional; each phase is delivered through individually
approved tasks.

## Task queue

Approved or anticipated work, in the order it is expected to be dispatched.

| Task | Title | Follows | Purpose |
| --- | --- | --- | --- |
| `KNOT-001b` | Design System Implementation (mobile) | `KNOT-001a` | Implement the design tokens in [`docs/BRAND.md`](BRAND.md) as React Native style primitives — colours, typography, spacing, corner radius, and elevation — so screens stop re-deciding them individually. Dispatch once the design language is stable. |

## Phase 0 — Foundation (current)

**Goal:** a clean, reproducible repository that can accept real work.

- Repository structure and conventions.
- Node/React Native + TypeScript mobile scaffold.
- Go modular-monolith scaffold.
- Formatting, linting, type-checking, and testing wired up.
- CI foundation (verify only, no deployment).
- Documentation foundation.

**Exit criteria:** the foundation passes lint, type-check, and tests locally, and CI runs
on push and pull requests.

## Phase 1 — Human Network First

**Goal:** make the core loop possible for real people.

- Identity, accounts, and profiles.
- Story creation (text and basic media).
- Per-language conversations with human adaptation between languages.
- Reactions and reporting basics.

**Exit criteria:** a story created in one language can be adapted by a human in a second
language and receive responses from both.

## Phase 2 — Data Foundation Second

**Goal:** make the network durable and queryable.

- PostgreSQL schema and migrations for the real domain model.
- Media storage strategy.
- Search and discovery over places, languages, and stories.
- Operational basics: configuration, observability, backups.

**Discovery Map — confirmed in Phase 2.** It is a discovery feature and needs stories and
language versions to exist before it can be useful, so it does not move earlier.

**Ordering.** Phase 2 follows Phase 1 and does not begin until Phase 1's loop is in use. Its
product work — **Rooted**, the **Discovery Map**, **Curious Inquiries**, and
**notifications** — depends on Phase 1's identity, stories, adaptations, and conversations,
and is queued here rather than pulled earlier.

**Exit criteria:** the product runs on a real, versioned database with reproducible
migrations.

## Phase 3 — Intelligence Third

**Goal:** assist the human network without replacing it.

- Only capabilities that measurably strengthen the core loop.
- Introduced as an isolated, justified component — never as a substitute for human
  adaptation.

**Exit criteria:** intelligence improves the North Star metric without displacing human
adapters.

## Explicitly out of scope for now

Microservices, service mesh, message queues, Kubernetes, a public developer API,
monetization, sophisticated follower graphs, autonomous moderation, and product
analytics.
