# Knot — Decisions

Architecture Decision Records (ADRs) for Knot. Each decision is recorded before (or at
the time it is) implemented. Statuses: **Proposed**, **Accepted**, **Superseded**.

---

## KNOT-ADR-001 — Clean-room start; `as-told-by` is out of scope

**Decision ID:** KNOT-ADR-001
**Date:** 2026-10-07
**Status:** Accepted

**Context:** An earlier prototype at `~/Projects/as-told-by` existed before the current
Knot architecture and product direction were established.

**Decision:** Knot starts as a completely fresh, clean-room project. The `as-told-by`
project is legacy and explicitly out of scope. No code, database schema, migrations, UI,
dependencies, configuration, or architecture from `as-told-by` will be adopted,
refactored, migrated, copied, or treated as an architectural reference.

**Reason:** The current Knot Master Project Brief is the authoritative direction. A
clean-room implementation protects architectural discipline.

**Consequences:** `as-told-by` is out of scope. No Knot task depends on it. KNOT-000a is
cancelled. The roadmap proceeds directly to KNOT-001.

---

## KNOT-ADR-002 — Mobile scaffold is hand-authored

**Decision ID:** KNOT-ADR-002
**Date:** 2026-10-07
**Status:** Accepted

**Context:**
The mobile scaffold needed to be created in apps/mobile/ for KNOT-001. Options were: (a) generate via create-expo-app or react-native init, or (b) hand-author a minimal scaffold.

**Decision:**
The React Native + TypeScript mobile scaffold is hand-authored rather than generated.

**Alternatives Considered:**
1. create-expo-app — rejected. Pulls in native folders, navigation, and template cruft we do not want at foundation stage.
2. react-native init — rejected. Same reasoning, plus generator-specific assumptions.
3. Hand-authored minimal scaffold — accepted.

**Reason:**
Reproducibility, reviewability, and minimalism. A hand-authored scaffold contains exactly what the foundation needs and nothing more.

**Consequences:**
Native build tooling (Android Studio, Xcode) is not yet configured. The first task requiring a real native build (likely when navigation is introduced) must add it deliberately.

---

## KNOT-ADR-003 — Toolchain versions pinned to supported peer ranges

**Decision ID:** KNOT-ADR-003
**Date:** 2026-10-07
**Status:** Accepted

**Context:**
Registry-latest versions of TypeScript (7.x) and ESLint (10.x) are not yet in the supported peer ranges of the lint stack (typescript-eslint 8.x requires TypeScript < 6.1 and ESLint < 10).

**Decision:**
Pin to: TypeScript ~5.9.x, ESLint 9.x (flat config), typescript-eslint 8.x, Babel 7.x, Jest 30.x. Node 20 LTS is the CI target. Go 1.22+ is the declared Go target.

**Reason:**
The supported peer ranges are more important than registry-latest. Chasing latest would break the lint and typecheck stack.

**Consequences:**
Revisit these pins when typescript-eslint and ESLint configurations support the newer majors. This is deliberate, not a defect.

---

## KNOT-ADR-004 — Go backend is dependency-free at foundation stage

**Decision ID:** KNOT-ADR-004
**Date:** 2026-10-07
**Status:** Accepted

**Context:**
The Go backend foundation needs a minimal cmd/ entry point and one internal package. No product feature requires third-party dependencies yet.

**Decision:**
The Go backend uses only the standard library at the foundation stage. No HTTP framework, no DB driver, no Redis client, no config library.

**Reason:**
Keeps CI simple and network-independent for the backend job. Avoids pulling in dependencies before their need is concrete.

**Consequences:**
The first backend feature task (likely identity or stories) will introduce the first real Go dependency. That introduction must be a deliberate, documented decision.

---

## KNOT-ADR-005 — JWT-only authentication for the MVP

**Decision ID:** KNOT-ADR-005
**Date:** 2026-10-07
**Status:** Accepted

**Context:**
Knot needs authentication for its first product feature, identity. The choices were server-side sessions (with a store), or stateless JWTs. Knot already runs Redis in local development, but nothing in the backend uses it yet.

**Decision:**
Authentication is JWT-only. No server-side sessions, and no Redis involvement in auth. Registration and login issue two HS256 tokens: a 15-minute access token (subject, issued-at, expiry) and a 30-day refresh token (subject, issued-at, expiry, and a random token id). The signing key comes from KNOT_JWT_SECRET, and the backend fails fast in non-local environments when it is missing or shorter than 32 bytes. Verification pins the accepted algorithm to HS256, and the token type claim is checked so a refresh token cannot be presented as an access token.

**Alternatives Considered:**
1. Server-side sessions in Postgres — rejected for the MVP. It adds a session table, a lookup on every request, and cleanup work before there is any evidence we need revocability.
2. Server-side sessions in Redis — rejected. It would make Redis a hard dependency of every request before we have a second reason to run it.
3. Opaque tokens with a database lookup — rejected. Same cost as sessions with fewer benefits.

**Reason:**
JWTs let the first identity slice ship without a session store, a lookup on the request path, or a new infrastructure dependency. Statelessness is a deliberate MVP trade, not an oversight.

**Consequences:**
Access tokens cannot be revoked before expiry, so the lifetime is kept short and the refresh token carries a `jti` for the rotation a later task will add. No refresh endpoint, logout, or protected endpoint exists yet — only issuance. Key rotation, revocation lists, and refresh rotation are all open follow-ups, and the first of them will need its own decision.

---

## KNOT-ADR-006 — argon2id password hashing

**Decision ID:** KNOT-ADR-006
**Date:** 2026-10-07
**Status:** Accepted

**Context:**
Storing user passwords requires a memory-hard, well-reviewed KDF. Knot has no existing password storage and no compatibility constraint with any legacy system.

**Decision:**
Passwords are hashed with argon2id from golang.org/x/crypto/argon2, with time=1, memory=64 MiB, threads=4, keyLen=32, and a 16-byte random salt. The hash is stored as a PHC string with the parameters embedded, so verification always uses the parameters recorded alongside the hash rather than the current constants. Verification uses a constant-time comparison. bcrypt, SHA, MD5, and plaintext are all explicitly rejected. Passwords are never logged and never returned by the API.

**Alternatives Considered:**
1. bcrypt — rejected. It is memory-light, so it resists GPU attacks less well than argon2id at comparable cost.
2. SHA-256 or SHA-512 with a salt — rejected outright. General-purpose digests are not password KDFs.
3. scrypt — rejected. Comparable in intent to argon2id, but argon2id is the current password-hashing recommendation and is available in x/crypto.
4. A third-party password-hashing wrapper — rejected. It would add a dependency for a small, auditable amount of code.

**Reason:**
argon2id is the current best practice for new password storage, it is memory-hard, and it is available in a dependency we already need. Embedding the parameters in the stored string means the cost can be raised later without invalidating existing hashes.

**Consequences:**
Each hash costs roughly 64 MiB and a few tens of milliseconds, which is intentional and must be accounted for when sizing the server. Changing the parameters affects only new hashes; existing ones keep verifying. The stored PHC string is parsed on every verification, so a malformed hash is reported as an internal error rather than as a bad password.

---

## KNOT-ADR-007 — Minimal Go dependency set

**Decision ID:** KNOT-ADR-007
**Date:** 2026-10-07
**Status:** Accepted

**Context:**
KNOT-ADR-004 kept the backend standard-library-only at foundation stage and stated that the first feature task would introduce the first real dependency deliberately. KNOT-003 is that task. Every candidate dependency had to be justified individually.

**Decision:**
The backend takes exactly three direct dependencies: github.com/jackc/pgx/v5 for PostgreSQL, github.com/golang-jwt/jwt/v5 for JWT signing and verification, and golang.org/x/crypto for argon2id. The HTTP layer uses the standard library net/http and its method-qualified ServeMux patterns, so no router is added. Database migrations are hand-rolled: SQL files embedded with go:embed, a schema_migrations table, and a small runner. No migration library is used. Versions are pinned to the newest releases that still build on the declared Go 1.22 target, which is deliberately older than the newest published versions.

**Alternatives Considered:**
1. chi or gin for routing — rejected. Go 1.22's ServeMux handles three method-qualified routes without a dependency, and frameworks tend to pull middleware ecosystems in behind them.
2. goose, golang-migrate, or atlas for migrations — rejected. The runner is roughly 200 lines, fully auditable, and has exactly the behaviour we need with none we do not.
3. database/sql with lib/pq — rejected. pgx is the maintained, actively developed PostgreSQL driver, and it is the one we would move to anyway.
4. Latest-published versions of every dependency — rejected for now. The newest pgx and x/crypto releases declare a Go 1.25 toolchain requirement, while ADR-003 declares Go 1.22 and CI installs Go 1.22. Bumping the Go target is a separate decision that also touches CI.

**Reason:**
Three dependencies, each with a single clear responsibility, keep the supply chain small and the security review tractable. Hand-rolled migrations keep schema changes auditable and avoid a library whose behaviour we would only partially use.

**Consequences:**
We own the migration runner, including its own tests and any future features such as down migrations or checksums. Dependency versions currently trail the newest releases until the Go target is revisited, so upgrading Go is a prerequisite for taking newer pgx and x/crypto releases. Any fourth dependency should be justified as explicitly as these three were.

---

## KNOT-ADR-008 — Keyset pagination with an opaque cursor

**Decision ID:** KNOT-ADR-008
**Date:** 2026-10-07
**Status:** Accepted

**Context:**
The story feed is unbounded and must be readable a page at a time. The two conventional options are offset pagination (`LIMIT n OFFSET m`) and keyset pagination (seek to a sort position). The feed is ordered newest-first and people publish while others read, so consecutive pages are not requested against a frozen snapshot.

**Decision:**
The feed is paginated by keyset. A page is selected with `WHERE (created_at, id) < ($1, $2) ORDER BY created_at DESC, id DESC LIMIT $3`, matching `stories_created_at_id_idx` exactly. The resume position is returned to the client as a `next_cursor`: an opaque, URL-safe token that carries the last row's `(created_at, id)` pair, base64url-encoded without padding. A cursor is validated on the way in — the timestamp must parse and the id must be canonical UUID text — and a malformed cursor is a `400 validation_error`. The page size is defaulted and capped by the API (20, maximum 50). The store fetches `limit + 1` rows to decide whether a next page exists, then discards the extra row rather than returning it.

**Alternatives Considered:**
1. Offset pagination — rejected. `OFFSET m` makes PostgreSQL walk and discard `m` rows, so cost grows with depth, and a story published between two requests shifts every later page by one, duplicating a story the reader has already seen.
2. Cursors signed or encrypted to be tamper-proof — rejected as unnecessary. The cursor is validated on the way in, so a forged one can only produce a bad request or a legitimate page; it grants no access, since the feed is public.
3. A cursor carrying only the id — rejected. The feed sorts by `created_at` first, so a cursor without the timestamp cannot seek into the composite index.
4. Returning the page size rather than a cursor — rejected. It leaves the client to invent the resume position, which is exactly the logic the API should own.

**Reason:**
Keyset pagination makes each page's cost independent of how deep the reader is, and it makes pages stable against inserts because a page is a range of the sort order rather than a row count. Encoding `(created_at, id)` reuses the index already needed for ordering, so the feature adds no query cost beyond the page itself. Keeping the token opaque lets the encoding change later without breaking clients.

**Consequences:**
The sort key and the cursor encoding are coupled: changing the feed's order means changing both, and old cursors become invalid rather than silently wrong. Cursors are positional and not durable — a cursor held across a deletion simply resumes at the next row. Clients must treat the token as opaque and must not construct one.

---

## KNOT-ADR-009 — Per-route bearer authentication; the author comes from the token

**Decision ID:** KNOT-ADR-009
**Date:** 2026-10-07
**Status:** Accepted

**Context:**
Publishing a story needs an author. The identity foundation already issues HS256 access tokens whose `sub` claim is the user id, and `GET /stories` and `GET /stories/{id}` must stay readable without an account.

**Decision:**
Authentication is HTTP middleware applied **per route**, not to the whole router: only `POST /stories` is wrapped. The middleware reads `Authorization: Bearer <token>`, verifies it with the same issuer that minted it, and puts the `sub` claim on the request context, where handlers read it through `UserIDFromContext`. Every verification failure returns the same `401 unauthorized` body. The author id is taken from the context and never from the request body; because unknown JSON fields are rejected, a body containing `author_id` is a `400` rather than a silently ignored field. The stories service re-validates that the id is canonical UUID text before it reaches the database.

**Alternatives Considered:**
1. Global authentication with an allow-list of public routes — rejected. It makes public exposure the default and protection the exception, so a new route is public unless someone remembers to exempt it. Per-route wrapping means a route is protected because it says so at the point of definition.
2. Accepting `author_id` in the body and checking it against the token — rejected. It invites a client to send a mismatched value and leaves the service deciding between two sources of truth for the same fact.
3. Distinguishing the 401 causes (expired, malformed, wrong type) in the response — rejected. The client's remedy is identical in every case, and a distinguishable answer confirms guesses about the token.
4. Session cookies — rejected in KNOT-ADR-005; the mobile client holds tokens in memory.

**Reason:**
Protection that travels with the route is auditable by reading the route table, and deriving the author from a verified claim removes an entire class of "publish as someone else" mistakes rather than defending against it. A uniform 401 keeps token probing uninformative without costing an honest client anything.

**Consequences:**
A handler's author is only trustworthy when it is reached through the middleware; `POST /stories` therefore also fails closed with `401` if `UserIDFromContext` reports no user, so a wiring mistake cannot publish an authorless story. Routes are individually responsible for being protected, which is why the route table is the place to review.

---

## KNOT-ADR-010 — Ids stay plain strings; no fourth dependency

**Decision ID:** KNOT-ADR-010
**Date:** 2026-10-07
**Status:** Accepted

**Context:**
The stories task needed an id type to thread through pagination cursors, the auth middleware, the stories service and store, and their tests. `github.com/google/uuid` was the obvious candidate, and it is present in the local module cache. KNOT-ADR-007 limits the backend to three direct dependencies and states that any fourth must be justified as explicitly as the first three.

**Decision:**
User ids and story ids are plain `string` values holding canonical UUID text. Validation is a small hand-rolled canonical-form check (`8-4-4-4-12` hexadecimal), mirroring the one identity already uses for `FindUserByID`. `golang.org/x/crypto` and the two existing modules remain the only direct dependencies; no fourth is added.

**Alternatives Considered:**
1. `github.com/google/uuid` — rejected. It would be the backend's fourth direct dependency to save roughly fifteen lines of character testing, and ADR-007 requires a fourth dependency to earn its place. The module being cached locally is not a reason to depend on it.
2. `pgtype.UUID` from pgx, already a dependency — rejected. It is a database-mapping type, so using it as the domain id type would leak a driver type into the domain and change how the value is JSON-encoded at the edges.
3. A shared `internal/uuids` package for the validator — rejected for now. Two small copies are cheaper than a package and a public API for a function with no behaviour to configure.

**Reason:**
Staying with strings keeps one id convention across the codebase instead of introducing a second one that the middleware would immediately convert back to a string, and it keeps the dependency set at the size ADR-007 deliberately chose. Ids are validated at the boundary — the cursor decoder, the middleware, and the service — so a malformed id cannot reach a query.

**Consequences:**
Nothing stops a non-UUID string from being passed as an id at compile time; the guarantee is enforced at runtime by validation and by the `uuid` column itself. The validator is duplicated in two packages rather than shared, which is a deliberate trade for keeping `identity`'s API surface unchanged. If id handling ever needs parsing, formatting, or comparison helpers, that is the moment to reconsider a uuid package — with the same justification a fourth dependency has always required.

---

## KNOT-ADR-011 — The version tree is an adjacency list

**Decision ID:** KNOT-ADR-011
**Date:** 2026-10-07
**Status:** Accepted

**Context:** Tell My People records how a story is adapted from one language into another, and those adaptations form a tree: a version descends from the version it was adapted from. The tree had to be stored in a way that fits how it is written and read. Three conventional models were on the table: an adjacency list, a materialized path, and a closure table.

**Decision:** The tree is stored as an **adjacency list**. Each row in `story_versions` carries a `parent_version_id` that references another row in the same table; a story's root version has `parent_version_id IS NULL`. There is no materialized path and no closure table. Only the direct parent is recorded.

**Alternatives Considered:**
1. Materialized path — rejected. It stores the full ancestry on every row, which makes ancestor lookups cheap but rewrites every descendant row when the tree is re-parented, and it duplicates state that can drift from the parent pointers.
2. Closure table — rejected. It adds a second table and a row per ancestor-descendant pair, which is the right cost for deep trees and frequent "all descendants" queries, but neither is true here: the floor of the tree is a human who tells a story, so it stays shallow, and a story's whole tree is small enough to read in one query.
3. Adjacency list — accepted. One nullable column, no second table, and the truth of each edge written in exactly one place.

**Reason:** The workload is "add a leaf" and "read one story's tree". An adjacency list makes the write a single insert with no bookkeeping, and the read a single `WHERE story_id = $1` scan. Both alternatives pay maintenance cost on writes or storage cost on rows for queries the product does not make yet. The simplest model that answers the current queries is the one to start with.

**Consequences:** Reading a whole tree means reading every version of a story and assembling the nesting in application code, which the API does not do at all — it returns a flat list and lets the client assemble it. A query like "every descendant of this version" would need a recursive CTE or a new structure; if deep trees or cross-tree queries become common, that is the moment to introduce a closure table, and it is a change to one table rather than a rewrite. The tree cannot be re-parented cheaply, which is fine: a version's parent is historical fact, not mutable state.

---

## KNOT-ADR-012 — Every story has a root version; content lives in versions

**Decision ID:** KNOT-ADR-012
**Date:** 2026-10-07
**Status:** Accepted

**Context:** Before Tell My People, the `stories` table held a single version of content directly: `language`, `title`, and `body`. Tell My People needs a story to have many versions, one per language or retelling, while a story remains one thing that can be found, read, and adapted. The schema had to divide story-level facts from version-level content.

**Decision:** Content moves out of `stories` and into `story_versions`. Every story has exactly **one root version** — a `story_versions` row with `parent_version_id IS NULL` — and `stories.root_version_id` points at it (`NOT NULL`, unique, `REFERENCES story_versions(id) ON DELETE RESTRICT`). Exactly one root per story is enforced by the partial unique index `story_versions_story_id_root_unique ON story_versions (story_id) WHERE parent_version_id IS NULL`. `stories` keeps only story-level metadata: the original `author_id`, `pillar`, `approximate_location`, `media_urls`, `sensitive`, `root_version_id`, and timestamps. The version-level fields are `story_id`, `parent_version_id`, `author_id`, `language`, `title`, `body`, `adaptation_note`, and timestamps. Story creation writes the story row and its root version in one statement; reading a story resolves its content from the root version.

**Alternatives Considered:**
1. Keep content on `stories` and copy it into the root version — rejected. Two copies of the same text can drift, and there is no single answer to "what does this story say".
2. Allow a story to exist with no versions — rejected. A story with no content cannot be read or adapted, so making the root version required (via `root_version_id NOT NULL`) means the invalid state cannot be stored.
3. Make the root directly an editable field on `stories` rather than a version — rejected. The root is the anchor every adaptation descends from, so it has to be a version like any other, or the tree has two kinds of nodes.

**Reason:** One place holds content, so a story's text is stored once and every adaptation is a version of it rather than an edit to it. Anchoring the root in `stories` keeps a story navigable from its id without walking the tree, while the partial unique index and the `NOT NULL` foreign key make "exactly one root per story" a database fact rather than a convention. Because a story row cannot exist without a root, and a version's content lives only on the version, there is no schema state in which a story's content is missing or duplicated.

**Consequences:** Reading a story is a join against its root version, so every story query names `story_versions`; the feed's keyset index is untouched because ordering is still `stories.(created_at, id)`. Story creation writes two rows that reference each other, which the store does in a single statement so the write is atomic. Content is no longer editable in place: editing a title or body becomes a new version once version editing exists, which is a future task. The original author is preserved separately from version authorship, so the story keeps a stable author as adapters add their own versions.

---

## KNOT-ADR-013 — Any authenticated user may adapt any story

**Decision ID:** KNOT-ADR-013
**Date:** 2026-10-07
**Status:** Accepted

**Context:** Tell My People is the feature that makes Knot Knot: a person reads a story and retells it in their own language. The task had to decide who is allowed to adapt. Knot has no trust system, no reputation, no roles, and no moderation yet, and the core loop is not yet in use enough to know what a sensible gate would be.

**Decision:** Adaptation authorization is **open to any authenticated user**. Any signed-in account may adapt any version of any story. The adapter is taken from the access token, never from the request body, and the only structural constraint is that `parent_version_id` must belong to the story named in the path — a version cannot be attached to the wrong tree. There are no ownership checks, no allow-lists, and no reputation requirement. This is recorded as an MVP decision, not a permanent one.

**Alternatives Considered:**
1. Only the original author may adapt — rejected. It defeats the feature: the whole point is that *other* people adapt a story for their own people.
2. A trust or reputation gate — rejected for now. There is no trust model to gate on, and building one before the loop is used would be guessing at the shape of a problem that has not appeared.
3. An allow-list of approved adapters — rejected. It re-creates the gate a trust system would own, in a form that cannot grow, and it is a moderation mechanism without a moderation policy.

**Reason:** The feature exists to be used, and gating it before anyone has used it would prevent the behaviour the product is trying to learn about. Every version records its author explicitly, and the tree records every parent edge, so the information a future trust or moderation rule would need is already captured. Leaving the gate open now costs nothing that cannot be added later, because adding a rule later is a change to one authorization check, not to the data.

**Consequences:** Nothing today stops any signed-in user from adapting any story, so abuse is possible and is accepted for the MVP. The same-story parent check and the foreign keys keep the tree internally consistent regardless of who adapts what. Rooted, trust, reputation, reporting, blocking, and moderation are deliberately out of scope and each requires its own task and its own decision before anything is enforced.

---

## KNOT-ADR-014 — A bridge creates a new target-language comment; both conversations stay intact

**Decision ID:** KNOT-ADR-014
**Date:** 2026-10-08
**Status:** Accepted

**Context:** Conversations are per version and per language: a comment is written in one language and belongs to one story version. The core Knot loop needs a person reading a comment in one language to answer in another, so the product needed a way to connect a comment in language A to the same thought expressed in language B. The choice was between mutating the comment, holding translations inside it, or creating a second comment and a link.

**Decision:** A bridge creates a **new comment** in the target language, on the story's version written in that language, and a `bridges` row that references both the source and the target comment. The source comment is never modified, and each comment remains an ordinary member of its own conversation — so a bridge **joins two conversations** rather than adding to one. The bridge is a **first-class object** with its own id, author, target language, optional adaptation note, and timestamp. **Comment threading (a `parent_comment_id`) is deliberately not part of the model**; replies happen only by bridging, and threading is deferred to a future task. Two constraints keep the graph well formed: `bridges_unique_pair` (a given source-target pair is bridged once) and `bridges_one_per_target_language` (a source can be bridged into any one language once). Creating the target comment and the bridge is one transaction.

**Alternatives Considered:**
1. One comment carrying many translations — rejected. It makes a comment a container of languages rather than a thing written in one language, and it gives the same comment two authors, which the model cannot express cleanly.
2. A comment that is edited in place to the new language — rejected outright. It destroys the original, which defeats the point: both conversations must survive so each language's readers keep their own thread.
3. An automatic machine translation stored on the comment — rejected. Knot adapts by human hands (Knot Brain is deliberately last), and a machine translation is not an adaptation.
4. A `parent_comment_id` for replies — rejected for now. Replies-by-bridging is the loop this task completes; a general reply tree is a larger design that should be decided on its own evidence.

**Reason:** Making the bridge a first-class object keeps each comment a simple, single-language, single-author row, and puts the relationship between two comments in one place where it can be queried, counted, and later given a workflow of its own (confirmation, removal). Writing the target comment on the story's target-language version means a bridge connects the English conversation to the French conversation that already exists because someone adapted the story into French — the two threads stay separate and readable in their own languages, joined by the bridge rather than merged. The two unique indexes make "one adaptation per language per source" and "no duplicate pairs" database facts, so a race cannot create two competing translations of the same comment into the same language.

**Consequences:** Bridging writes two rows, and they are written in one transaction so a failure inserts neither. The story must already have a version in the target language — a comment cannot be bridged into a language the story is not told in, and such a request is rejected as a validation error — and when the story has several versions in that language the oldest is chosen, so the choice is deterministic rather than dependent on row order. A comment can appear in several bridges (as a source once per language, and as a target once), so code that walks bridges must handle both directions. Because threading is deferred, a conversation is a flat list and a reply is a bridge rather than a child; if general threading is later wanted it is an additive migration, not a rewrite. `bridges_one_per_target_language` is a product policy enforced by the schema — the same comment cannot be bridged into the same language twice — which is the default this task chose and can be relaxed later by dropping one index.

---

## KNOT-ADR-015 — Any authenticated user may bridge any comment

**Decision ID:** KNOT-ADR-015
**Date:** 2026-10-08
**Status:** Accepted

**Context:** Bridging is the second half of the core loop: after a person adapts a story, others discuss it, and bridging lets a discussion cross a language boundary. The task had to decide who may bridge. Knot still has no trust system, so this decision had to be consistent with how adaptation was already opened up.

**Decision:** Bridge authorization is **open to any authenticated user**. Any signed-in account may bridge any comment into any language other than the source comment's own, and the bridger is taken from the access token, never from the request body. The only structural constraint is that the target language must differ from the source comment's language, and the schema's uniqueness indexes prevent duplicate pairs and duplicate target languages for one source. There are no ownership checks, no allow-lists, and no reputation gate. This is recorded as an MVP decision, not a permanent one.

**Alternatives Considered:**
1. Only the comment's author may bridge it — rejected. It defeats the feature: the reason to bridge is that someone *else* can carry the thought into their language.
2. A trust or Rooted-based gate — rejected for now, and explicitly a non-goal of this task. There is no trust model to gate on yet; building one before the loop is used would be guessing at the problem.
3. A bridge confirmation workflow (source author approves) — rejected for now. It is a moderation mechanism without a moderation policy, and it would block the loop this task exists to complete.

**Reason:** This mirrors KNOT-ADR-013 for adaptation, so the two halves of the core loop apply the same rule rather than an inconsistent pair. Every bridge records its author, and the source and target comments record theirs, so the attribution a future trust or moderation rule would need is already captured. Opening the gate now costs nothing that cannot be added later, because a future rule is a change to one authorization check, not to the data.

**Consequences:** Nothing today stops any signed-in user from bridging any comment, so abuse is possible and accepted for the MVP. The same-language check and the foreign keys keep the bridge graph internally consistent regardless of who bridges what. Rooted-gated bridging, bridge confirmation, and moderation remain out of scope and each requires its own task and its own decision before anything is enforced.

---

## KNOT-ADR-016 — Rooted signal model: place plus duration bucket, self-declared

**Decision ID:** KNOT-ADR-016
**Date:** 2026-10-08
**Status:** Accepted

**Context:** Knot needs a trust and community layer before it can honour its core loop across languages. The first primitive had to answer "who is this person, and where are they from" without inventing a reputation system, without collecting anything precise enough to endanger anyone, and without a verification mechanism that does not exist yet. The options ranged from a full vouch graph or a Rooted Score to nothing at all, and the task had to pick the smallest honest thing.

**Decision:** A Rooted signal is a **place** plus a **duration bucket**, and it is **self-declared**. The place is free text at **city or region precision only**: 1-80 characters after trimming, no coordinates, no address, and a line break or a tab is rejected. The duration bucket is one of a **closed set of five** — `lifelong`, `many_years`, `several_years`, `a_few_years`, `recently` — enforced by both a domain check and a `CHECK` constraint. A user has **exactly one primary signal**: setting a second replaces the first, enforced by a partial unique index on `(user_id)` where `is_primary`. The `is_primary` column exists so a later multi-signal feature can add non-primary rows without a migration. In this task there is **no verification, no vouching, no score, and no gating** of any action.

**Alternatives Considered:**
1. A vouch graph (people confirm each other's place) — rejected for now. It is a second, larger model that needs its own trust rules, its own abuse story, and its own UI; building it before the simple signal has been used would be guessing at the shape of a problem that has not appeared.
2. A Rooted Score or any derived number — rejected outright. A score invites optimising the number rather than declaring the truth, and it would make an honest self-declaration into a competition.
3. Precise location or geocoding — rejected. It is a safety and privacy hazard with no product benefit here: a city or region is enough for a reader to understand where a telling comes from, and precision cannot be un-collected once leaked.
4. Gating an action (adapting, bridging, commenting) on having a signal — rejected for now. It would make the trust layer a toll gate before anyone has used the feature, and it is explicitly a non-goal of this task.
5. Free-text duration ("about ten years") — rejected. It does not sort, filter, or render consistently, and it invites precision the model cannot verify.

**Reason:** Place plus a duration bucket is the smallest model that answers the question Rooted exists to answer, and every part of it is something the person can state about themselves without anyone else's involvement. Keeping the set of buckets closed and the place bounded to a single short line makes the honesty of the signal structural rather than aspirational: there is nothing to score, nothing to verify, and nowhere to put an address. One primary signal keeps the MVP honest about its own limits while the partial index and `is_primary` flag leave the door open for the multi-place case later.

**Consequences:** Every signal is unverified by construction, so a signal is a self-description and must never be presented as a fact or used as an authorisation input. The public-read path exposes a place name, which is a real, if coarse, piece of personal information: a person who hides their signal is the only one who can see it. Because `is_primary` exists and the unique index is partial, adding multi-signal support is additive — a schema-supported change rather than a rewrite — but the MVP UI deliberately handles one signal. Vouching, scores, community aggregation, and gating each remain out of scope and require their own ADR before anything is enforced.

---

## KNOT-ADR-017 — Rooted visibility: public by default, with a per-signal private flag, and a minimal inline enrichment

**Decision ID:** KNOT-ADR-017
**Date:** 2026-10-08
**Status:** Accepted

**Context:** A Rooted signal is a person's declared connection to a place, so it is simultaneously the thing that makes a stranger legible and the thing that could expose them. The task had to decide (a) whether a signal is visible by default, (b) how a person hides one, and (c) how much of a signal leaves the Rooted endpoints when it is attached to a story, a version, a comment, or a bridge for inline display.

**Decision:** A Rooted signal is **public by default**, and a per-signal boolean `is_public` lets its owner hide it. The server applies the default: an omitted `is_public` on `POST /users/me/rooted` means public, so a client cannot accidentally publish or accidentally hide by omission. Two read rules follow: `GET /users/me/rooted` (protected) returns the owner's own signals **including** hidden ones, and `GET /users/{id}/rooted` (public) returns **only** public signals, answering `404` for a user that does not exist so an empty array means "declared nothing public" rather than "no such user". Inline enrichment on the story detail, version, comment, and bridge responses attaches an `author_rooted` summary that carries **only `place` and `duration_bucket`** — no `id`, no `user_id`, and no timestamps — or `null` when the author has no public signal. Enrichment is a single batched read of the distinct authors in a response, performed at the HTTP layer, and a failure to read Rooted is logged and swallowed rather than failing the content response.

**Alternatives Considered:**
1. Private by default, with an opt-in to publish — rejected. It would make the trust layer invisible until people actively opted in, which is the opposite of what a legibility primitive is for, and it makes the common case the one that requires an extra step.
2. A global "Rooted visibility" setting rather than a per-signal flag — rejected. The flag belongs on the signal that is being hidden, and a per-signal flag is what the multi-signal future needs anyway; the column is per-signal precisely so the rule never has to be reinterpreted later.
3. Enriching with the whole signal (id, owner, timestamps) so the client can be lazily rich — rejected. The inline field is a display hint, not a lookup: shipping the owner and the timestamps would spread personal data across every content response for no display benefit and would couple every content endpoint to the Rooted schema.
4. Enriching by having the stories, versions, and conversations stores join `rooted_signals` — rejected. It would change three store interfaces and push an unrelated domain's table into each of their queries; the batch happens once, at the HTTP layer, where the response shape is already being assembled.
5. Failing the content response when the Rooted read fails — rejected. Enrichment is supplementary decoration; a Rooted outage must not make stories, versions, comments, or bridges unreadable.

**Reason:** Public-by-default makes legibility the ordinary case, and the server applying the default means the client's omission is never mistaken for a decision to hide. Keeping hidden signals visible to their owner, and only to their owner, gives a person a real way to withdraw without losing their own view of what they declared. Trimming the inline summary to two fields keeps the enrichment honest about what it is — a place and a duration for a badge — and keeps the owner's identity out of responses that already name the author only by id. Doing the enrichment once per response at the HTTP layer keeps it off the store interfaces and makes the "one batch, not N+1" property testable with a lookup that counts its calls.

**Consequences:** A hidden signal is still a row and still counts toward the one-primary invariant; hiding is a read-time decision, not a delete, so a person can unhide without re-declaring. Every content response that carries `author_rooted` may show `null`, which a client must treat as "no public signal" rather than an error. Because the enrichment is best-effort, a Rooted outage degrades the badge to `null` while the content still loads — an availability choice, not a correctness one, and one that is visible in the logs. The summary deliberately cannot be used to look up the full signal, so a client that wants more must call the Rooted endpoints, where the visibility rules apply. `GET /stories` (the feed) and `POST /stories` do not populate the field; extending the enrichment to the feed is a small, additive change for whichever task first renders a badge there.

---

## KNOT-ADR-018 — Map library: react-native-maps

**Decision ID:** KNOT-ADR-018
**Date:** 2026-10-08
**Status:** Accepted

**Context:** The Discovery Map (KNOT-008) is the first Knot screen that draws a map, and it is the first feature to need a native module. The task had to choose how the map is rendered, knowing that the mobile app is a hand-authored minimal scaffold with no `ios/` or Android project and a deliberately tiny dependency set (KNOT-ADR-002, KNOT-ADR-007), and that adding a native dependency changes the build story even though no native build runs yet.

**Decision:** Use **`react-native-maps`**, pinned to `^1.29.11` — the first new mobile dependency since KNOT-001. It is used for `MapView` and `Marker` only. Because there is no native project to build, the **native setup is deferred to a future task** and documented in [`docs/DEVELOPMENT.md`](DEVELOPMENT.md): iOS needs `pod install` once an `ios/` project exists, and Android needs a Google Maps API key in the manifest. The **JavaScript layer** (lint, typecheck, Jest) is verified now and does not touch native code. Knot stores no coordinates, so the map is populated from a client-side place-name lookup, not from the API.

**Alternatives Considered:**
1. A web map in a `WebView` — rejected. It would still be a native dependency (`react-native-webview`), add a bridge and a second runtime, and give a worse gesture story than a native map.
2. A hand-drawn or SVG map — rejected. There is no offline vector basemap to hand-draw at world scale, and building one is a product-size undertaking unrelated to discovery.
3. A map provider SDK (Google Maps SDK directly) — rejected. It is lower-level than Knot needs and would tie the client to one provider with no web fallback, whereas `react-native-maps` already wraps the platform maps.
4. No map at all: a plain list of places — rejected for this task. The whole point of KNOT-008 is geographic discovery; a list is a fine fallback inside the screen but not the feature.

**Reason:** `react-native-maps` is the de-facto React Native map binding, it is one dependency, and its peer requirements (`react-native >= 0.76`) match the app's React Native version. Keeping the feature to `MapView` and `Marker` keeps the surface small enough that the deferred native setup is a build concern rather than a design one: nothing in the JS depends on a Google-specific API. Documenting the native steps now, without running a native build, keeps the dependency honest about what it will cost later while unblocking the feature.

**Consequences:** The mobile app now has a dependency that cannot be exercised in CI or in a Jest test — the JS layer is verified, the native rendering is not. A future task must generate the native scaffold, run `pod install`, supply an Android API key, and verify the map on a device; until then the map screen type-checks and lints but is not run. Because Knot stores no coordinates, the map's usefulness depends on the client lookup table (`apps/mobile/src/data/placeCoordinates.ts`), which is a curated list that will need extending as places are used; a place it does not know is shown in a list rather than silently dropped.

---

## KNOT-ADR-019 — Navigation: a hand-rolled bottom tab bar

**Decision ID:** KNOT-ADR-019
**Date:** 2026-10-08
**Status:** Accepted

**Context:** KNOT-008 introduces four primary destinations (Home, Map, Create, Profile) and, for the first time, a reason to keep a persistent chrome element on screen. The app has never used a navigation library: `App.tsx` held a single `ScreenName` in state and rendered one screen for it (KNOT-ADR-002). The task had to decide whether to finally adopt a router, or to model tabs by hand.

**Decision:** Model navigation **by hand**, in `App.tsx`, as two levels: an **active tab** (`feed`, `discoveryMap`, `createStory`, `profile`) and an optional **overlay** — a discriminated union of the non-tab screens, each carrying the ids it needs to render and to return. A new `TabBar` component renders four buttons and highlights the active one; it is rendered **only when there is no overlay**, so a secondary screen replaces the whole surface rather than sitting inside the bar. **No navigation library is added** — not `react-navigation`, not `react-native-screens`, not `react-native-safe-area-context`.

**Alternatives Considered:**
1. `react-navigation` (bottom tabs + a native stack) — rejected for now. It is a large dependency with its own native peers, adopted to serve four tabs and one overlay slot where every transition is already explicit and type-checked; the behaviour it would add (back gestures, deep links, per-tab state) is not asked for yet.
2. A single flat `ScreenName` including the four tabs — rejected. It cannot express "a screen over a tab", so it would either lose the bar on every secondary screen or duplicate each tab's screen at each entry point.
3. A tab bar with per-tab navigation state (retained scroll/stack per tab) — rejected. Retaining per-tab state is what a router is for; hand-rolling it now would be building the thing that was just declined.

**Reason:** Four tabs plus one overlay is small enough that a plain discriminated union is the cheapest correct model: the type checker proves every transition, each overlay carries its own back target, and the whole thing is one file with no dependency. It also keeps the tab bar's relationship to overlays explicit — the bar is a sibling of the active tab, shown only for a primary destination — which is exactly the behaviour wanted and is easy to get wrong behind a library's abstractions.

**Consequences:** Back behaviour is hard-coded per overlay rather than derived from a stack, so a new screen must state where its back button goes; that is a small, local cost and is visible at the call site. There are still no deep links and no hardware/gesture back handling. The decision is deliberately revisitable: when a concrete flow needs what this cannot express (a deep link into a nested screen, a real per-tab stack, a modal that survives a tab switch), a router is the right answer, and this ADR should be superseded rather than worked around. Until then, `docs/NAVIGATION.md` records the remaining proposals.

---

## KNOT-ADR-020 — Place representation: free text with a lowercased indexed copy

**Decision ID:** KNOT-ADR-020
**Date:** 2026-10-08
**Status:** Accepted

**Context:** Discovery groups stories by place, but a place in Knot is a free-text field an author types (KNOT-ADR-016 for Rooted, and the story's `approximate_location`). The same place arrives spelled differently — "Cape Town", "cape town", "Cape Town " — and grouping on the raw column would treat each as a distinct place. The task had to decide how places are represented and compared, without introducing coordinates.

**Decision:** A place stays **free text**, and discovery groups on a **normalised copy**, `stories.approximate_location_lower`, added in migration `0006` as `lower(trim(approximate_location))` with an index. The application maintains the column on story create (the `INSERT` computes `lower(trim($3))`), and the migration backfilled existing rows. The read side matches a place by normalising the input the same way, so comparison is case- and surrounding-whitespace-insensitive. There are **no coordinates, no spatial index, and no geocoding server-side**; the mobile client resolves a place name to a point with a **local lookup table** (`apps/mobile/src/data/placeCoordinates.ts`), and a place it does not know is listed rather than plotted.

**Alternatives Considered:**
1. Store coordinates (lat/lng) on the story — rejected. It asks authors for precision Knot deliberately avoids collecting, and it is a privacy hazard with no display benefit a city name does not already give.
2. PostGIS and a geometry column — rejected. It is a large extension and a spatial model bought for a grouping problem that a text column solves; it also makes "the same place" a fuzzy geometric question rather than an exact string one.
3. Geocode place names on write — rejected. It needs a third-party service, a network dependency on the write path, and a cache; it would also resolve the same text differently over time.
4. Group on the raw `approximate_location` — rejected. It would fragment one place into many clusters on casing and stray whitespace, which is the exact problem this task exists to solve.
5. A canonical place table with ids — rejected for now. It is the right shape once places need to be curated, but it is a second model (and an editing surface) that this task does not need; the normalised column is the smallest thing that makes grouping correct.

**Reason:** Normalising a text field is the least invasive way to make "the same place" mean the same thing in a query. Keeping the author's original spelling in `approximate_location` preserves what they typed, while the lowercased copy is a pure derivation the application can maintain in the same statement that writes the row — so the two cannot drift. Pushing coordinates to the client keeps the server free of precise location data and keeps the map a presentation concern, which is where an exact-match lookup table is adequate for an MVP.

**Consequences:** Place matching is **exact after normalisation**: "Cape Town" and "cape town" are one place, but "Cape Town, South Africa" is a different one, and the map will list rather than plot it — a known MVP limit, not a silent failure. The client lookup table is curated and must be extended as places are used; it is not a geocoder and does not guess. If places later need to be curated or merged, a canonical place table is the natural next step, and the normalised column is the bridge to it. The `languages` figure in a cluster is aggregated from **root versions only**, while the language *filter* matches any version; both are deliberate and recorded here so the asymmetry is a choice rather than an accident.

---

## KNOT-ADR-021 — New Architecture disabled for MVP

**Decision ID:** KNOT-ADR-021
**Date:** 2026-10-08
**Status:** Accepted

**Context:** React Native 0.76 enables the New Architecture (Fabric renderer + TurboModules) by default, and the KNOT-008a native Android scaffold inherited `newArchEnabled=true`. Building with the New Architecture on Android compiles the React Native C++ core, which requires the Android NDK — a roughly 1.5 GB download that repeatedly timed out and hung on the founder's network, blocking the first physical-device build entirely.

**Decision:** Set `newArchEnabled=false` in `apps/mobile/android/gradle.properties`, opting the Android build into the legacy architecture for the MVP. The `ndkVersion` pins in `android/build.gradle` and `android/app/build.gradle` are commented out to match, since the NDK is only pulled in for New Architecture C++ compilation.

**Alternatives Considered:**
1. Install the NDK manually and keep the New Architecture on — rejected for now. It is a ~1.5 GB download that already failed on the founder's network, and it buys a performance feature the app cannot yet use.
2. Leave `newArchEnabled=true` and accept a build that does not complete — rejected outright. The app must build and run on a device before any further product work is verifiable.

**Reason:** Knot has **no custom native C++ code**. The New Architecture is a performance and interop story for large component trees and for libraries that adopt its APIs; at MVP scale, with a handful of screens and one native module, it provides no benefit that justifies a 1.5 GB toolchain download. Disabling it is the smallest change that makes the native build reproducible on an ordinary connection.

**Consequences:** The app runs on the legacy architecture until this ADR is revisited. Revisit when (a) a required native library only ships New Architecture support and cannot be worked around, or (b) Knot's component tree grows large enough that the Fabric renderer's performance is needed. Re-enabling requires the NDK to be installed, so it is a deliberate toolchain change, not a one-line toggle.

---

## KNOT-ADR-022 — react-native-maps pinned to 1.14.0

> **Superseded by KNOT-ADR-026.** The Discovery Map no longer uses `react-native-maps`; it uses Mapbox (`@rnmapbox/maps`).

**Decision ID:** KNOT-ADR-022
**Date:** 2026-10-08
**Status:** Superseded by KNOT-ADR-026

**Context:** KNOT-008 (KNOT-ADR-018) added `react-native-maps` at `^1.29.11`. With the New Architecture disabled (KNOT-ADR-021), the Android build of `1.29.11` fails to compile: it uses `ViewManagerWithGeneratedInterface`, an API that only exists under the New Architecture, so its generated interfaces are not produced on the legacy architecture.

**Decision:** Pin `react-native-maps` to `^1.14.0` — the version that supports the legacy architecture — in `apps/mobile/package.json` (and the lockfile). The API surface used by the app (`MapView`, `Marker`, `UrlTile`) is identical across the two versions, so no screen code changes.

**Alternatives Considered:**
1. Re-enable the New Architecture to keep `1.29.11` — rejected: that is KNOT-ADR-021, which exists precisely to avoid the NDK download this would require.
2. Switch the map to Mapbox (`@rnmapbox/maps`) — rejected for now. It is a different native dependency with its own account, token, and setup, replacing a working module to solve a version conflict that a pin already solves.
3. Remove maps entirely — rejected. The Discovery Map is a Phase 1 feature (KNOT-008); dropping it is a product decision, not a build workaround.

**Reason:** `1.14.0` provides everything the current screen needs — `MapView`, `Marker`, and `UrlTile` — with the same component API, so the downgrade is invisible in application code. It is the version that matches the architecture the app is actually building, which is the correct thing to pin against.

**Consequences:** The map library trails the newest release until the New Architecture is enabled. The caret range is kept as `^1.14.0` in `package.json`; if patch or minor releases under `1.x` ever diverge from the legacy architecture again, the version should be pinned exactly (no `^`) to prevent a surprise upgrade in a lockfile refresh. Upgrading back to a New-Architecture release is a follow-up to KNOT-ADR-021, not a separate decision.

---

## KNOT-ADR-023 — OpenStreetMap tile provider for MVP

> **Superseded by KNOT-ADR-026.** OSM's tile server returns an `x-blocked` header to anonymous clients and `react-native-maps` cannot send identifying headers, so its tiles never rendered; the map now uses Mapbox instead.

**Decision ID:** KNOT-ADR-023
**Date:** 2026-10-08
**Status:** Superseded by KNOT-ADR-026

**Context:** `react-native-maps` on Android draws its basemap with the Google Maps SDK by default. Rendering it requires a Google Cloud project with a valid billing method and an API key, even for usage that stays inside the free tier. The founder has no billing-enabled Cloud account available, so the default basemap cannot render and the map shows no tiles.

**Decision:** Draw the map with **OpenStreetMap raster tiles** via the `UrlTile` component inside `MapView`, pointing at `https://tile.openstreetmap.org/{z}/{x}/{y}.png` with `maximumZ={19}`. No Google Maps API key is required, and no signup or billing account is needed. `UrlTile` ships inside `react-native-maps@1.14.0`, so no new dependency is added.

**Alternatives Considered:**
1. Google Maps SDK — rejected for now. It requires a billing-enabled Google Cloud project and an API key before a single tile renders; that is setup cost and a card on file for an MVP that has no revenue yet.
2. Mapbox — rejected. Its free tier is usable, but it is a native module change (a different package, token, and build configuration) to solve what a single `UrlTile` component already solves.

**Reason:** OSM is the cheapest viable option at MVP scale: zero setup cost, no key, no account, no billing. It is a raster tile layer over the map component the app already has, so the change is one component and no dependency.

**Consequences:** OSM's tile usage policy discourages bulk or high-volume use and asks that clients identify themselves and keep traffic modest — fine for on-device development and a small user base, not for production scale. When Knot's user base grows, migrate the basemap to Google Maps (with a key) or Mapbox; because only the `<UrlTile>` block changes, this is a contained change to `DiscoveryMapScreen`. Tracking that migration as a Phase 2+ task is the exit from this decision. The Google Maps API key placeholder in `android/app/src/main/res/values/strings.xml` is left in place (unused, harmless) so the Google path needs no re-plumbing if it is chosen.

---

## KNOT-ADR-024 — Session persistence via AsyncStorage

**Decision ID:** KNOT-ADR-024
**Date:** 2026-10-09
**Status:** Accepted

**Context:** The mobile session — the access token, the refresh token, and the user profile — lived only in React component state, so closing the app lost it and every restart meant registering again. On-device verification flagged this as the most blocking UX problem. The task had to decide where the session is persisted, and whether to protect it.

**Decision:** Persist the session in **`@react-native-async-storage/async-storage`** under a single, versioned key, `knot.session.v1`, as JSON holding `{ accessToken, refreshToken, user }`. `App.tsx` loads it on launch and restores it before rendering; login and register save it; sign-out clears it. AsyncStorage is the **only** new dependency. The storage layer is **total**: every failure is logged and swallowed, a missing or corrupted value resolving to `null` ("not signed in") rather than throwing.

**Alternatives Considered:**
1. Secure/encrypted storage (Keychain/Keystore via `react-native-keychain`, or `expo-secure-store`) — deferred, not rejected on merit. It is the correct home for credentials, but it is a heavier native dependency with per-platform APIs, and the app today stores nothing sensitive: the tokens are short-lived MVP credentials and the profile is a name and an email. Deferring keeps this change small and the new-dependency count at one.
2. No persistence (in-memory only) — rejected. That is the bug being fixed.
3. Persisting only the tokens and re-fetching the user on launch — rejected for now. There is no "GET /users/me" endpoint, and adding one is a non-goal; the register/login response already carries the profile, so storing it avoids a new endpoint and an extra round-trip.

**Reason:** AsyncStorage is the standard, smallest, zero-configuration key-value store for React Native, and the session is a single small JSON blob. A versioned key means a future change to the stored shape can migrate or discard an old value instead of crashing on it. Making the storage layer total — never throwing — means an unavailable or corrupt store degrades to "logged out", which is the safe state.

**Consequences:** The tokens are stored **unencrypted**, readable by anything that can read the app's sandbox. That is accepted only because there is no sensitive data yet; the trigger to migrate to encrypted storage is the moment Knot stores anything sensitive or ships to real users, and the versioned key exists so that migration can be additive. There is **no refresh-token rotation or auto-refresh**: an expired access token means a signed-out user until a later task adds refresh (a non-goal here). Sign-out clears the stored key, so signing out survives a restart.

---

## KNOT-ADR-025 — Navigation via header back buttons and an overlay stack, no navigation library yet

**Decision ID:** KNOT-ADR-025
**Date:** 2026-10-09
**Status:** Accepted

**Context:** KNOT-008 modelled navigation (KNOT-ADR-019) as a single optional overlay over one of four tabs, with each overlay hard-coding where its "back" went. Two problems surfaced on device: the way back was unclear on some screens, and the **Android hardware back button was not handled** at all, so pressing it exited the app rather than returning to the previous screen. The task had to decide whether to adopt a navigation library now or extend the hand-rolled model.

**Decision:** Keep the hand-rolled model and extend it in two ways. First, the overlay becomes a **stack**: `apps/mobile/src/navigation/overlayStack.ts` provides pure `push`/`pop`/`replace`/`popAll` helpers over an array, and `App.tsx` holds the stack in state, so back is always "pop". Second, **every overlay screen renders a "← Back" affordance** at the top-left and `App.tsx` wires the **Android hardware back button** (`BackHandler`) to pop one overlay, falling through to the OS only when the stack is empty. **No navigation library is added.**

**Alternatives Considered:**
1. Adopt `@react-navigation` (native stack) — rejected for now. It is the right answer for deep stacks, deep linking, per-tab stacks, and gesture transitions, but none of those are needed yet, and it is a large dependency with its own native peers (`react-native-screens`, `react-native-safe-area-context`) that would have to be built and verified on the device.
2. Keep the single overlay and only fix each back target — rejected. It cannot express "return to where I came from" when one screen is reachable from two places (a story opened from the feed versus from a place's stories), which is a case that was losing its parent.

**Reason:** A stack is the smallest model that makes back correct and uniform — pop always reveals the previous screen, whether that is a tab or another overlay, so every screen's back action is the same function. It is pure array arithmetic, trivially testable, and adds no navigation dependency. Handling the Android hardware back button is part of the platform contract and was the concrete "stuck" bug.

**Consequences:** Back targets are now derived from the stack rather than written per screen, so adding a screen means pushing it, not wiring a return path. There is still **no deep linking**, no per-tab navigation state, and no gesture or transition animation — the device back button and the on-screen "← Back" are the whole of back handling. The trigger to revisit is a flow the stack cannot express: a deep link into a nested screen, a real per-tab stack, or a modal that must survive a tab switch; at that point a router should supersede this ADR rather than be worked around. The four tab screens stay top-level. `CreateStoryScreen` and `ProfileScreen` also keep their pre-existing "Cancel"/"Back" affordances — those are tab-level actions, not overlay back navigation — and were left unchanged.

---

## KNOT-ADR-026 — The Discovery Map renders with Mapbox, not OSM

**Decision ID:** KNOT-ADR-026
**Date:** 2026-10-09
**Status:** Accepted

**Context:** The Discovery Map has never rendered real tiles on Android. KNOT-008b drew OpenStreetMap raster tiles through react-native-maps' `UrlTile` (ADR-023), and KNOT-011 tried to force them to appear by disabling the Google base layer (`mapType="none"`) and ordering the overlay beneath the markers. Neither worked. The reason is outside our control: OpenStreetMap's tile server returns an `x-blocked` header to clients it cannot identify and expects an identifying `User-Agent`/`Referer`, while `react-native-maps` on Android fetches tiles through the platform SDK and exposes no way to set those headers. The screen showed an empty container (with a Google watermark) and no map, and every configuration avenue inside `react-native-maps` was exhausted.

**Decision:** Render the map with **Mapbox** through **`@rnmapbox/maps`**, pinned to **10.2.10** — the newest release whose peer range (`react-native >=0.69`) covers the app's React Native 0.76 on the **legacy architecture** (10.3.0 moved to `>=0.79`). The map uses Mapbox's **public** access token (`pk.…`), which is safe to commit, and the Dark style so it sits on the navy canvas. `react-native-maps` is removed. The Android build fetches the native Mapbox SDK from Mapbox's Maven repository, which requires a **secret** downloads token (`sk.…`, scope `downloads:read`) held in the developer's user-level `~/.gradle/gradle.properties` and never committed. This **supersedes ADR-022 and ADR-023**.

**Alternatives Considered:**
1. **OSM through a WebView + Leaflet** — rejected. It would render, and it lets us set request headers, but a WebView map has worse gestures, no native annotation taps, and a second runtime inside the screen; it is a workaround, not a map.
2. **Google Maps** — rejected. The Google Maps SDK for Android needs a billing-enabled Cloud project and an API key before a single tile renders, and the founder has no credit line available (the same reason ADR-023 originally avoided it).
3. **MapTiler, Stadia, or Thunderforest raster tiles** — rejected. All are viable but have smaller free tiers and still ride on `UrlTile`, the component that could not send identifying headers in the first place; that is the exact failure being left behind.
4. **No map** — rejected. The Discovery Map is a headline Phase 1 feature; removing it is a product decision, not a bug fix.
5. **Re-enable the New Architecture to keep a newer `@rnmapbox/maps`** — rejected. That is ADR-021's NDK download, and 10.2.10 already supports the architecture we build.

**Reason:** Mapbox ships a **native** map SDK, so rendering is done by the Mapbox engine using its own access token rather than by a tile overlay bolted onto the Google Maps SDK; the class of failure that produced the blank map cannot recur, because no `UrlTile` and no identifying-header problem are involved. `@rnmapbox/maps` is the maintained React Native binding for that SDK, and 10.2.10 matches both the React Native version and the architecture. A public client token keeps the setup honest: the only secret is a build-time downloads token that never enters the repository.

**Consequences:** The app now depends on Mapbox. It needs a Mapbox account, and the free tier is **50,000 monthly active users** — generous for an MVP and a real ceiling to watch. The build needs a **secret** downloads token on each developer machine (`~/.gradle/gradle.properties`); without it Gradle cannot resolve `com.mapbox.maps:android`, so a new machine must be set up before its first Android build. The library logs a **deprecation warning** on the legacy architecture (it would prefer the New Architecture), so re-enabling that is a future driver — a reason to revisit ADR-021, not this one. Mapbox's logo and attribution are left enabled, as its terms require. The public token is **not** committed: it lives in the gitignored `src/config/secrets.local.ts` and `res/values/mapbox.xml`, copied from the committed `.example` templates (KNOT-ADR-027), and must be rotated in both local files if abused. `react-native-maps@1.14.0` (ADR-022) and the OSM tile provider (ADR-023) are superseded; their ADRs are kept for history. Replacing Mapbox later (MapLibre with self-hosted tiles, or Google Maps once billing exists) remains a contained change to one screen, because `DiscoveryMapScreen` still owns the whole map surface.

---

## KNOT-ADR-027 — Client-side configuration uses committed `.example` files and gitignored `.local` files

**Decision ID:** KNOT-ADR-027
**Date:** 2026-10-09
**Status:** Accepted

**Context:** KNOT-011a committed a Mapbox **public** token (`pk.…`) directly into `apps/mobile/android/app/src/main/res/values/strings.xml` and `apps/mobile/src/config/dev.ts`. GitHub's Push Protection then blocked the push: its scanner cannot distinguish Mapbox's public (`pk.`) and secret (`sk.`) token formats, so it flagged both paths. The token was not a secret — public Mapbox tokens are designed to ship inside a client app — but a repository must not contain anything the scanner treats as a credential, or pushes stop working.

**Decision:** Any client-side configuration that could be flagged as a token (public or secret) moves to a **gitignored** `.local` file, with a committed `.example` file beside it documenting the required shape. Developers copy `.example` → `.local` after cloning and fill in the real values. Concretely:
- `apps/mobile/src/config/secrets.local.ts` (gitignored) ← `secrets.example.ts` (committed); a committed `secrets.local.d.ts` declares the module's type so TypeScript still compiles before the local file exists.
- `apps/mobile/android/app/src/main/res/values/mapbox.xml` (gitignored) ← `apps/mobile/android/app/mapbox.example.xml` (committed); `strings.xml` no longer defines `mapbox_access_token`. The template sits **outside** the `res/` tree because Android compiles every XML file under `res/values/`, so an `.example` file there would define `mapbox_access_token` twice and fail the build with `Duplicate resources`.

Both local paths are in `.gitignore`, and `docs/DEVELOPMENT.md`, "Local secrets setup (after cloning)", documents the copy step.

**Alternatives Considered:**
1. **Allow the push by marking the token a false positive** (GitHub's "allow this secret" / push-protection bypass) — rejected. It disables future protection at exactly those paths, so a real secret committed there later would pass.
2. **Split the token across `strings.xml` and `dev.ts` so no single file matches the scanner** — rejected. It is fragile (the patterns change, and the pieces still reconstruct the token) and it hides the token shape instead of removing it from source control.
3. **Server-side proxy for the token** — rejected. Mapbox's client token is designed for the client; a proxy adds a service, latency, and cost to solve a repository-hygiene problem.

**Reason:** `.example` + gitignored `.local` is the standard, dependency-free way to keep real values out of git while giving every clone a working, self-documenting starting point. Plain TypeScript and Android XML are used rather than `.env` files because the bundler and Android's resource merger already understand them, so no loader or new dependency is introduced.

**Consequences:** A fresh clone needs one manual step — create the two `.local` files from their `.example` templates — before the Android build or the Metro bundle will work. The committed `secrets.local.d.ts` keeps `npm run typecheck` green even before that step, so a missing file surfaces as a build/bundle error rather than a type error, which is where the remedy (copy the example) belongs. When mobile CI is added it must inject the token at build time (writing the `.local` files, or an equivalent), and that is a future task. The `.example` placeholders (`pk.REPLACE_WITH_YOUR_PUBLIC_MAPBOX_TOKEN`) are never valid tokens, so committing them cannot trip the scanner.
