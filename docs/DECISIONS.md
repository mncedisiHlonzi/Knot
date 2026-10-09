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

## KNOT-ADR-028 — MinIO provides object storage for local development

**Decision ID:** KNOT-ADR-028
**Date:** 2026-10-09
**Status:** Accepted

**Context:** KNOT-012 adds avatars, which are the first thing Knot stores that is not a row in PostgreSQL. Local development already runs PostgreSQL and Redis as containers declared in `infrastructure/docker/docker-compose.yml` (KNOT-007 era), so a developer needs no cloud account and no credentials to run the whole product. Object storage had to fit that pattern, and it had to be reachable by the backend through an S3-compatible API so the storage code is the same code that a hosted provider would use.

**Decision:** `infrastructure/docker/docker-compose.yml` gains a `minio` service:
- Host ports **9000** (S3 API) and **9001** (web console), matching the brief, with a `knot_minio_data` volume so objects survive a restart.
- Root credentials come from `.env` (`KNOT_MINIO_ROOT_USER`, `KNOT_MINIO_ROOT_PASSWORD`) and are the same values the backend uses (`KNOT_S3_ACCESS_KEY`, `KNOT_S3_SECRET_KEY`), so there is one local credential pair, not two.
- The media bucket (`KNOT_S3_BUCKET`, default `knot-media`) is created on first start through `MINIO_DEFAULT_BUCKETS`.
- The healthcheck is an HTTP GET on `/minio/health/live`, so `docker ps` reports readiness the same way it does for PostgreSQL and Redis.

**Image substitution.** The brief pinned `minio/minio`, which can no longer be pulled: MinIO withdrew its Docker Hub repository, so the Hub API answers `object not found`; `quay.io/minio/minio` answers `401 UNAUTHORIZED`; `bitnami/minio` has no published tags. The service therefore uses **`bitnamilegacy/minio:2025.7.23-debian-12-r5`** — the last Bitnami-published MinIO build, pinned exactly, and genuine MinIO rather than a substitute implementation. The compose file carries a comment saying why, so the deviation is visible at the point of use and not only here.

**Alternatives Considered:**
1. **Filesystem storage behind the `storage.Storage` interface** — rejected. It is the smallest possible change, but it would mean the S3 client, the endpoint configuration, and the path-style addressing were untested until a production store appeared, which is the point at which they are hardest to get right.
2. **A hosted S3-compatible service (AWS S3, Cloudflare R2, Backblaze B2)** — rejected for local development. It makes running the product depend on network access, an account, and credentials that must be distributed to every developer.
3. **Store avatar bytes in PostgreSQL (`bytea`)** — rejected. It works, and it would avoid a service, but it puts binary blobs in the same database as the query workload, and it teaches the codebase a storage shape that is wrong for every other kind of media the product will add.
4. **`minio/minio` by digest from a mirror or an unofficial tag** — rejected. Pinning a digest whose origin cannot be verified trades one supply-chain risk for a worse one.

**Reason:** A local, S3-compatible, credential-free-for-the-developer object store is exactly the shape the backend needs, and it is the shape the deployed product will use. Running the same code path locally as in production is worth an extra container. The image substitution is the only deviation and it is pinned, documented, and still MinIO.

**Consequences:** `scripts/dev-up.sh` starts one more container, and a developer needs roughly another 200 MB of images and disk. The local root credentials are in `.env` and are deliberately weak; they are for a loopback-only service and must not be reused anywhere real. The substituted image differs from upstream in two details that compose now compensates for: the server's data directory is `/bitnami/minio/data` rather than `/data` (mounting the wrong one looks healthy while silently losing every object on the next container recreation), and `mc` is installed off `PATH`. The core MinIO project's own images are unreachable today, so the pinned legacy Bitnami image is a temporary dependency that should be revisited when MinIO publishes a pullable image again — the compose file names the pinned tag in one place, so that change is one line. Versioning, lifecycle rules, and object locking are not configured: this is local development storage, and the deployed choice of object store is a separate decision that has not been made yet.

## KNOT-ADR-029 — Uploads and downloads are mediated by the backend

**Decision ID:** KNOT-ADR-029
**Date:** 2026-10-09
**Status:** Accepted

**Context:** KNOT-012 needs a client to upload an avatar and every client to read one. The object store offers two obvious shortcuts: give the client a presigned URL that talks to the store directly, or make the bucket public so an object URL can simply be linked. Both remove the backend from the byte path. The task had to decide whether to take either.

**Decision:** The backend is the only party that talks to the object store. The bucket stays private and no presigned URL, public bucket, CDN, or store hostname is ever exposed:
- **Upload:** `POST /users/me/avatar` (`Bearer`), `multipart/form-data`, field `file`; at most **5 MiB**; the format is decided by **sniffing the uploaded bytes**, and only JPEG, PNG, and WebP are accepted.
- **Object key:** `avatars/{user_id}/{random UUID}.{ext}`, minted by the backend. The handler passes it to the identity service, which refuses any key that is not a single object directly inside the caller's own prefix, so a bug in the HTTP layer cannot repoint another account's avatar.
- **Storage:** `users.avatar_url` holds that **object key**, not a URL. The column name is pinned, but the value has to be the key, because the object name ends in a random UUID and cannot be derived from a fixed path.
- **Download:** `GET /users/{id}/avatar` (public) streams the bytes with the stored `Content-Type`, `Cache-Control: public, max-age=3600`, and `X-Content-Type-Options: nosniff`.
- **Wire format:** API responses never contain the key. `avatar_url` is `/users/{id}/avatar?v=<object name>`, so replacing an avatar yields a different URL and a cache keyed on the old one is not reused. Image resizing, moderation, and non-avatar media are out of scope.

**Alternatives Considered:**
1. **Presigned `PUT` for upload and presigned `GET` for download** — rejected. It hands a client a signed credential for a bucket, which means the bucket must be reachable from the internet and must answer CORS; the bytes land without the backend ever seeing them, so format and size are unenforceable; and revocation is per-URL and effectively impossible, since a signed URL is valid until it expires. It also makes the object store's hostname part of the client's contract.
2. **Public bucket with plain object URLs** — rejected. There is no authorization at all: anyone who learns a key reads the object, and "the key contains a UUID" is obscurity, not access control. It also means an avatar cannot later be made private without a migration and a client change.
3. **Return `/users/{id}/avatar` and derive the key from the path** — rejected. It reads better, but the object name must be unique per upload to avoid overwriting bytes an in-flight response is still serving, and a unique name cannot be derived from a constant path. The `v` parameter gives the same readability without losing the uniqueness.
4. **Store avatar bytes in the database** — rejected for the same reason as in KNOT-ADR-028.

**Reason:** Mediating every byte keeps one authorization check, one format check, and one size check in a place the backend controls, and it keeps the storage hostname out of the client contract entirely. The cost is that the API process carries avatar traffic, which at MVP scale is small and, more importantly, is not the forever-shape of the system: the same backend-mediated path is what lets a CDN or a presigned `GET` be introduced later without changing the wire format clients already use.

**Consequences:** Avatar bandwidth and CPU run through the API process; if that ever becomes the bottleneck, the fix is to introduce a cache in front of `GET /users/{id}/avatar`, not to expose the bucket. Storing a key in a column named `avatar_url` is a small, permanent wart that has to be explained wherever the schema is read, which is why `docs/DATA_MODEL.md` says so at the column. Replacing an avatar deletes the previous object best-effort: if that deletion fails, the result is an orphaned object rather than a broken profile, and nothing currently sweeps orphans. The `storage.Storage` interface keeps the store swappable — a hosted S3 in production is a configuration change plus a bucket policy, not a handler change. Because the object key is not derived from anything public, a schema rollback (`0007` down) drops the column and intentionally leaves the objects in place rather than destroying user data.

## KNOT-ADR-030 — Story media is a table with a `source` column

**Decision ID:** KNOT-ADR-030
**Date:** 2026-10-09
**Status:** Accepted

**Context:** KNOT-013 introduces images and videos into stories, and the feature that makes it *this* feature rather than a generic attachment: a file captured with the device camera must be distinguishable from one chosen out of the gallery, so the UI can show a capture badge. A story already has a `media_urls text[]` column, but an array of strings can hold neither the origin of a file nor the metadata (dimensions, duration, size) the client needs to render it, and it cannot reference an object in the media bucket introduced in KNOT-012.

**Decision:** Story media is a new `story_media` table (migration `0008`), not an extension of `stories.media_urls`:
- Columns: `id`, `story_id` (FK, cascade), `uploader_id` (FK, cascade), `storage_key`, `media_type` (`image`/`video`), `mime_type`, `source` (`camera`/`gallery`), `width`, `height`, `duration_ms`, `size_bytes`, `display_order`, `created_at`.
- `storage_key` holds the **object key** in the media bucket (`story-media/{story_id}/{uuid}.{ext}`), never a URL, exactly as `users.avatar_url` does (KNOT-ADR-029).
- `source` is a `CHECK`-constrained closed set, so the camera/gallery distinction is enforced by the database, not only by the application.
- `display_order` is appended by the server on upload, so the order matches upload order without the client managing it.
- The existing `stories.media_urls` column is left as it is: it still accepts free-text links, and the new table is the home for uploaded files. Removing `media_urls` is a separate cleanup.

**Alternatives Considered:**
1. **Store an array of objects (JSONB) on `stories`** — rejected. PostgreSQL would not enforce the MIME, media-type, or source sets, and querying "media for this story" would mean unpacking JSON in the application.
2. **Separate `story_images` and `story_videos` tables** — rejected. Every read would union them, and the source badge would be duplicated; one table with a `media_type` discriminator is simpler and the row shape is otherwise identical.
3. **Keep only `media_urls` and encode the source in the URL** — rejected. It puts application meaning into an opaque string and still cannot store dimensions or duration.

**Reason:** Media is a first-class entity per story with its own metadata and lifecycle, and the camera/gallery origin is the feature. A table models that directly, and the foreign keys make deletion correct for free (deleting a story or a user removes its rows).

**Consequences:** A story now has two media concepts during the transition — the legacy `media_urls` array and `story_media` — which the API keeps separate. The `source` distinction is stored and returned but not yet used for anything beyond the badge; moderation or filtering on it is a later task. An orphaned object can result if a row insert fails after the object is written; the service deletes it best-effort, and nothing sweeps leftovers yet (the same gap as avatars).

## KNOT-ADR-031 — `react-native-image-picker` covers camera and gallery

**Decision ID:** KNOT-ADR-031
**Date:** 2026-10-09
**Status:** Accepted

**Context:** The mobile app must let a user capture a photo or video with the camera *and* choose one from the gallery, for both story media and the profile picture. It is the first camera/library integration in the app, which runs on React Native 0.76 with the New Architecture disabled (KNOT-ADR-021) and has a deliberately small dependency set (KNOT-ADR-002).

**Decision:** Use **`react-native-image-picker`** (`^8.2.1`) for all four paths — camera photo, camera video, gallery photo, gallery video — through `launchCamera` and `launchImageLibrary`. One dependency covers both sources and both media kinds, and its `Asset` result carries the URI, MIME type, size, and dimensions the upload needs. The picker reports no Android permission requirement in its own documentation, so the app declares no `CAMERA` permission: declaring it would force a runtime permission request and a `SecurityException` if denied. Only the Android 11+ `<queries>` entries for the two capture intents are declared.

**Alternatives Considered:**
1. **`react-native-image-crop-picker`** — rejected. It pulls in cropping, which is an explicit non-goal of this task, and a heavier native surface.
2. **`expo-image-picker`** — rejected. The app is a bare React Native project with no Expo runtime, and adding one for a single module is disproportionate.
3. **A separate camera library plus a separate picker** — rejected. Two native modules where one suffices, and two permission stories.

**Reason:** One well-maintained library covers exactly the four entry points the task needs, returns the metadata the backend stores, and needs no manifest permission, which keeps the Android build simple on the legacy architecture.

**Consequences:** Camera capture and video recording can only be verified on a physical device; the simulator has no camera, so those paths are unverified by this task (see the task's Known issues). The library is a native module, so the Android build must be re-run after installing it. iOS usage strings (`NSCameraUsageDescription`, `NSPhotoLibraryUsageDescription`, `NSMicrophoneUsageDescription`) are declared in `Info.plist`; without them iOS refuses to show the picker.

## KNOT-ADR-032 — Videos up to 100 MiB, streamed with HTTP Range requests

**Decision ID:** KNOT-ADR-032
**Date:** 2026-10-09
**Status:** Accepted

**Context:** Story media includes video, which a phone camera produces in tens of megabytes. Two questions follow: how large a video the API accepts, and how a client plays one without downloading the whole file before it can start. Mobile data in Knot's target context is metered and often slow, so making a user download 100 MiB to watch ten seconds is unacceptable.

**Decision:**
- **Upload limits:** images (JPEG/PNG/WebP) at most **10 MiB**; videos (MP4/MOV) at most **100 MiB**. The whole `POST .../media` body is capped at 100 MiB plus multipart overhead, and the per-type limit is enforced by the domain after the sniffed type is known.
- **Playback:** `GET /stories/{id}/media/{mid}/content` honours a single-range HTTP `Range: bytes=start-end` header, answering **206 Partial Content** with `Content-Range` and `Accept-Ranges: bytes`; the S3 object is fetched with a native range so only the requested bytes leave the object store (KNOT-ADR-033). A request with no Range, or an unusable one, gets the whole object with **200**.
- **Client:** `react-native-video` (`^6.19.3`) plays the video, requesting ranges automatically.

**Alternatives Considered:**
1. **Progressive download only (no Range)** — rejected. The player could not seek, and it would buffer the whole file before playback in the worst case.
2. **HLS/DASH segmentation with transcode** — rejected as out of scope. Transcoding is an explicit non-goal of this task; ranges over the original MP4 are enough for MVP playback.
3. **A 25 MiB video cap** — rejected. It is below what a short phone video produces, which would make the feature frustrating for its main use case.
4. **Presigned URLs for playback** — rejected for the same reasons as KNOT-ADR-029: it exposes the bucket and moves format and authorization decisions out of the backend.

**Reason:** Range requests are the minimal mechanism that makes video seekable and startable without a full download, and they need no transcoding pipeline. A per-type size cap keeps a single upload bounded while allowing a real video.

**Consequences:** The API process carries video bytes, and a 100 MiB upload over a slow link can take a while; the server's `ReadTimeout`/`WriteTimeout` (10 s) are tuned for small JSON bodies and may need raising before large videos are uploaded over the public internet — recorded as a known issue, not changed here (no CI/infra changes in this task). Mobile uploads are sequential (one file at a time) for the same reason. Playback quality is whatever the device recorded; there is no adaptive bitrate. Oversized uploads are refused with `413` before storage.

## KNOT-ADR-033 — `GetRange` added to the Storage interface for video streaming

**Decision ID:** KNOT-ADR-033
**Date:** 2026-10-09
**Status:** Accepted

**Context:** KNOT-ADR-032 requires the backend to serve part of an object. The storage layer introduced in KNOT-012 exposes only `Put`, `Get` (whole object), `Delete`, and `Exists`. Answering a Range request by fetching the whole object and discarding the parts outside the range would work, but it would transfer a 100 MiB object to send a 1 MiB seek, which defeats the purpose.

**Decision:** Add one method to `internal/storage`:

```go
// GetRange returns a reader for the byte range [start, end] inclusive.
GetRange(ctx context.Context, key string, start, end int64) (io.ReadCloser, error)
```

The S3 implementation sets the SDK's `GetObjectInput.Range` field, so the object store itself returns only the requested bytes. The in-memory test double implements it by slicing, and the shared storage contract test asserts it, so the double and the real store cannot drift. The handler never applies a range itself; it parses the header once with `storymedia.ParseRange` to decide 206-vs-200 and to build `Content-Range`, and the service uses the same helper to fetch the matching bytes, so the two cannot disagree.

**Alternatives Considered:**
1. **Reuse `Get` and seek/discard** — rejected as the general case. It is correct but transfers the whole object; it is noted in the interface doc as the fallback an implementation without native range support may use.
2. **Return range metadata from `Get`** — rejected. It would change the shape of every `Get` call for a need only media has.
3. **A media-specific storage method** — rejected. Ranges are a property of object storage, not of story media; the storage contract is the right home.

**Reason:** Native ranges are the whole point of the feature, and the S3 API supports them directly. One narrow method keeps the change small and confined, and the shared contract test keeps the in-memory double honest.

**Consequences:** The `Storage` interface is five methods wide rather than four, and every implementation (the S3 store and the test doubles in `internal/storage` and `internal/httpapi`) must provide `GetRange`. A range that is syntactically valid but unsatisfiable for the object's size is never sent to storage: the handler clamps against the row's `size_bytes` and falls back to a full 200, so the object store is not asked for a range it would reject with a 416.

## KNOT-ADR-034 — Structured place data replaces free-text location for map compatibility

**Decision ID:** KNOT-ADR-034
**Date:** 2026-10-09
**Status:** Accepted

**Context:** A story's place was free text, and discovery grouped stories by a normalised copy of it (KNOT-ADR-020). The Discovery Map resolved each place name to a point with a hand-maintained, client-side table of ~66 well-known cities, so a small town or a rural place typed as free text never appeared on the map. The founder's on-device pass (KNOT-013) made the gap obvious: the map showed only the cities someone had already added to the table.

**Decision:** Store place **structure**, not only text. Migration `0009` adds `latitude`, `longitude`, and `place_country` to `stories` and to `rooted_signals`:
- The three columns are **nullable and additive**. A story with no place, and every row created before the migration, has `NULL`/`NULL`/`NULL`.
- `latitude` and `longitude` are **set together or not at all**; the service rejects a lone one. Latitude is bounded to [-90, 90], longitude to [-180, 180], and NaN is rejected explicitly.
- `approximate_location` (and Rooted's `place`) is **kept** for display and backward compatibility: a new story still writes the chosen place's name there, and the `approximate_location_lower` copy still groups legacy rows.
- Discovery groups by the `(latitude, longitude)` pair when present, and falls back to `approximate_location_lower` when it is not, so old and new rows coexist.

**Alternatives Considered:**
1. **Enlarge the client-side lookup table** — rejected. It is unbounded manual work, and it never covers the small places the feature exists for.
2. **Backfill coordinates for existing free-text rows** — rejected. It needs a geocoder on the backend (a credential the backend should not need) and would guess at places that may be ambiguous.
3. **A PostGIS geography column and a spatial index** — rejected. The MVP groups and plots points; a plain `GROUP BY` over `double precision` is enough, and PostGIS is a deployment dependency with no MVP payoff.
4. **Replace `approximate_location` outright** — rejected. It would break every existing row and the display name a story already carries.

**Reason:** A coordinate is the smallest change that makes every place mappable, and making it additive means the transition needs no backfill and no destructive migration. Keeping the text field means the place a person typed is still what is shown, while the coordinate is what the map uses.

**Consequences:** A place now has two representations during the transition — text and structure — and the API carries both. A story created before `0009` has no coordinate and still relies on the client lookup table until it is re-created; that table is therefore kept as a fallback rather than deleted. There is no spatial index, no reverse geocoding, and no prediction of a coordinate from a name.

## KNOT-ADR-035 — Mapbox Geocoding API for location search

**Decision ID:** KNOT-ADR-035
**Date:** 2026-10-09
**Status:** Accepted

**Context:** KNOT-ADR-034 needs a coordinate for a place, which means geocoding a typed name into a point and a country. The app already uses Mapbox for the Discovery Map (KNOT-ADR-026) with a public token in the gitignored `secrets.local.ts`.

**Decision:** Use the **Mapbox Geocoding API** (`geocoding/v5/mapbox.places`) for place search, called **directly from the mobile client** with the existing public token (the founder adds the `mapbox.places` scope to it). No new provider and no new signup. The request asks for `types=place,locality,region` and `limit=5`, and the client maps each feature's `text`, `center`, and country context onto `{place, latitude, longitude, country}`. The **backend needs no Mapbox token**: it receives structured place data and never calls a geocoder.

**Alternatives Considered:**
1. **Geocode on the backend** — rejected. It makes the backend depend on a third-party credential and network call on the write path, and it duplicates a lookup the client can do in one request.
2. **A different provider (Google, Nominatim, HERE)** — rejected. It is a new account, a new key, and a second mapping provider for a product that already uses Mapbox.
3. **A bundled offline gazetteer** — rejected. It is the enlarged lookup table of KNOT-ADR-034 under a different name, with the same coverage problem.
4. **Mapbox Search Box API** — rejected for MVP. It is a newer, session-billed surface; the classic Geocoding API is sufficient for a place-and-region picker.

**Reason:** The same provider the map already uses, with the same token and no backend involvement, is the least new surface that solves the problem.

**Consequences:** The public token needs the `mapbox.places` scope, and the free tier's request budget (100k/month) is now consumed by typing; the picker debounces (300 ms) and requires two characters, and caches results in memory for the session, which bounds the request rate. Geocoding is a network dependency of the picker only: if the geocoder is unreachable the picker shows "Could not search" and the form cannot gain a location, which is correct — a place without a coordinate would break the invariant from KNOT-ADR-034.

## KNOT-ADR-036 — A location requires selecting a suggestion; free text alone is not accepted

**Decision ID:** KNOT-ADR-036
**Date:** 2026-10-09
**Status:** Accepted

**Context:** KNOT-ADR-034 makes a coordinate the thing the map plots. The picker lets a person type freely, and typing without choosing a suggestion would leave text with no coordinate — exactly the state the feature exists to remove.

**Decision:** A story's location must be a **selected suggestion**. The location picker is a controlled component: typing updates the text but clears any prior selection, and choosing a row sets both the text and the coordinate. On submit, the Create Story screen rejects a form whose location field has text but no selection with *"Please select a place from the suggestions."* The Rooted setup screen applies the same rule. A story with **no** location at all remains valid.

**Alternatives Considered:**
1. **Accept free text with null coordinates** — rejected. It reintroduces the exact gap KNOT-ADR-034 closes and would let a story claim a place the map cannot show.
2. **Geocode free text on submit** — rejected. It hides a network call behind "Publish", and an ambiguous name has no single right point.
3. **Silently drop unselected text** — rejected. It publishes a story the author believes is located when it is not.

**Reason:** Requiring a selection is what guarantees the invariant — every located story has a coordinate — and it is checked on the client before a request is sent.

**Consequences:** A person on a poor connection, or looking for a place the geocoder does not know, cannot attach a location; they can still publish without one. The backend does not *require* a coordinate (a story without a place is valid), so this is a client-side rule enforced where the text is entered; the server's own rule is only that the pair travels together.

## KNOT-ADR-037 — Media viewer uses FlatList paging and Animated gestures, no new dependency

**Decision ID:** KNOT-ADR-037
**Date:** 2026-10-09
**Status:** Accepted

**Context:** KNOT-013's full-screen viewer showed one item and closed; it could not swipe between a story's media, zoom an image, or scrub a video. The polish task needed a swipeable gallery with pinch-zoom and video controls, without adding a dependency (the app deliberately keeps a tiny native surface, KNOT-ADR-002).

**Decision:** Build the gallery from React Native's built-ins:
- **Swipe:** a horizontal `FlatList` with `pagingEnabled` and `getItemLayout`, so a swipe moves exactly one item and the list can open at the tapped index.
- **Dismiss:** a `PanResponder` on the backdrop that follows a downward drag and closes past a threshold; it claims only vertical-dominant moves, so it does not steal the pager's horizontal swipe or an image's pinch.
- **Pinch-zoom:** an `Animated` scale with a `PanResponder` that claims the gesture only for two touches (pinch) or a drag while already zoomed, and a double-tap to reset. Panning is clamped by scale, not by bounds — full edge-clamping is deferred.
- **Video:** `react-native-video` with a custom overlay (play/pause, a tap-to-seek scrub bar, and a current/total readout) rather than the platform's built-in controls, so it matches the dark theme.

**Alternatives Considered:**
1. **`react-native-gesture-handler` + `react-native-reanimated`** — rejected. Two more native dependencies for one screen, against the project's stated preference (KNOT-ADR-002).
2. **A third-party lightbox/gallery package** — rejected. It would bring its own theming, its own dependency tree, and styling the product has not designed yet.
3. **The platform's built-in video controls** — rejected. They cannot be themed and look foreign on the dark canvas; a small custom overlay covers the required behaviour.

**Reason:** The built-in APIs cover the required gestures, and keeping the dependency set unchanged is worth a hand-written responder.

**Consequences:** The gesture code is small but hand-rolled, so pinch and swipe must be verified on a device (a simulator has no multi-touch). Panning does not clamp to image edges, which is acceptable for MVP and can be tightened later. The `PanResponder` negotiation between the pager, the image, and the dismiss handler is subtle; the claim rules are documented in the code so a future change does not silently break one of the three.


## KNOT-ADR-038 — Notifications are written after the primary write, and never for your own action

**Decision ID:** KNOT-ADR-038
**Date:** 2026-10-09
**Status:** Accepted

**Context:** KNOT-015 introduces the in-app notification: adapting a version, commenting on one, or bridging a comment should tell the affected author. Three questions had to be settled: who is notified, when the row is written relative to the write that caused it, and what happens when the notification write fails.

**Decision:**
- **One event set, three events:** `version.created`, `comment.created`, `bridge.created`. Each maps to exactly one entity type, so every notification points at something a client can open. The set is closed by a CHECK constraint and by Go constants.
- **No self-notification.** A notification whose actor and recipient are the same user is refused in the service *and* by a `CHECK (actor_id <> user_id)` constraint. Acting on your own content is ordinary and is simply not news.
- **The notification is a non-critical side effect.** It is written after the primary row is committed, and a failure is logged and ignored: the adaptation or comment must not be lost because an inbox write failed. The hook returns an error so a caller *could* care, and the callers deliberately do not.
- **The recipient is resolved from the stored row, not the request.** The version's author (for a comment) and the source comment's author (for a bridge) are read inside the domain, so a client cannot aim a notification at someone else.

**Alternatives Considered:**
1. **Write the notification in the same transaction as the content** — rejected. It couples two bounded contexts in one transaction, and it makes a content write fail for a reason the user cannot act on.
2. **Notify the story's author rather than the version's author** — rejected. The person whose words were retold is the version's author, and a story has many versions with different authors.
3. **Allow a self-notification and filter it in the client** — rejected. It stores rows that can never legitimately be shown, and it leaves the rule in one place only (the client).
4. **Notify on a background worker reading a queue** — rejected for MVP. It needs a queue, a worker, and a delivery state for a feature whose whole surface is a row the client polls.

**Reason:** The affected author is the person who gains something from the event, and keeping the write off the critical path keeps the content write predictable. Refusing self-notifications at the schema level makes the rule impossible to bypass.

**Consequences:** A failed notification write is silently lost — there is no retry, and the warning log is the only trace. Because the row is written after the content row, a crash between the two loses the notification but keeps the content, which is the correct order of priorities. The `CHECK` constraint means a caller that gets the recipient/actor pair backwards gets a database error rather than a bad row.

## KNOT-ADR-039 — Notifications are in-app only; push is deferred

**Decision ID:** KNOT-ADR-039
**Date:** 2026-10-09
**Status:** Accepted

**Context:** The natural next step for a notification feature is a push notification, and the brief for KNOT-015 scoped notifications as in-app. A push channel would need device tokens, OS permissions, a provider (APNs/FCM), a delivery state, and retry semantics — none of which the inbox needs.

**Decision:** Delivery is **in-app only**. The mobile app reads its own inbox and the unread count from the API and shows a bell with a badge on the feed. There is no device-token table, no push provider, no delivery state, and no background worker. The endpoints are polled, not pushed.

**Alternatives Considered:**
1. **Firebase Cloud Messaging in this task** — rejected. It is a new external account, a new native dependency, a token lifecycle, and a delivery state, for a feature whose data model is already complete without it.
2. **A long-poll or SSE stream** — rejected. It holds a connection per client to avoid a poll that costs one indexed query, and it adds a second way for the inbox to be read.
3. **Send an email per event** — rejected. Email is a different product surface with its own deliverability, unsubscribe, and privacy questions.

**Reason:** The inbox is the feature. Push changes *how a client learns* there is something to read, not what there is to read, so it can be added later without touching the schema — `read_at` and `created_at` are already everything a push payload would carry.

**Consequences:** A person does not learn about a new notification until they open the app; the badge is read on mount, so returning to the feed refreshes it. Adding push later means a device-token table and a send step after the row is stored, not a redesign — the notification write already happens at the right moment (KNOT-ADR-038) and the row already carries the id the payload needs.

## KNOT-ADR-040 — The notifications package is domain-agnostic, and content domains depend on a one-method hook

**Decision ID:** KNOT-ADR-040
**Date:** 2026-10-09
**Status:** Accepted

**Context:** Three content domains produce notifications (`versions`, `conversations`) and one stores them. The obvious shape — have `versions` and `conversations` import `notifications` and call its service — creates a dependency from every content domain onto a package that knows about their content. The notifications service must also resolve "who should be told", which means reading a version's author from inside the conversations domain.

**Decision:**
- A notification is described by **primitive ids only**: recipient, actor, event, entity type, and entity id. The `notifications` package imports no content domain and has no idea what a story, version, comment, or bridge is.
- Each producing domain declares **its own small interface** (`versions.Notifier` with one method, `conversations.Notifier` with two) and calls it. The concrete implementation is the notifications service, wired in `cmd/knot`. Neither domain imports the other's types or the notifications package's types.
- The recipient is resolved **inside the producing domain**, through its own store: `conversations` gained `VersionAuthor(ctx, versionID)` rather than reaching into `versions` for a whole `StoryVersion`.
- The HTTP layer enriches a page of notifications with the actors' names and avatars in **one batched lookup** (`identity.UsersByIDs`), the same shape as the existing Rooted batch enrichment.

**Alternatives Considered:**
1. **`versions` and `conversations` import `notifications` directly** — rejected. It points the dependency the wrong way: a leaf that stores rows would be named by the domains whose content it records, and every content package would compile the notification types.
2. **An event bus or a domain-event publisher** — rejected for three events. It is indirection and an ordering question for a call that is one INSERT.
3. **Have the notifications service read the content tables itself** to resolve recipients — rejected. It would make this package know stories from versions from comments, which is exactly the coupling the layering avoids.
4. **Send one lookup per notification for actor details** — rejected. A page of 20 would be 20 queries; the batch read is one.

**Reason:** A one-method interface declared by the caller is the smallest contract that keeps the dependency graph acyclic and each package ignorant of the others' types, and it matches the existing pattern for cross-domain reads (the Narrow interfaces in `httpapi/enrich.go`).

**Consequences:** There is a small amount of duplicated shape — two `Notifier` interfaces, and a `VersionAuthor` read on the conversations store that exists only for this — but no package grows an API it does not need, and the notifications package can be tested with primitives and no fixtures from other domains. Adding a fourth event means adding a method to the interface of the domain that produces it and one hook call; nothing else changes.

## KNOT-ADR-041 — Every authored response carries a display name and avatar, enriched in one batched lookup

**Decision ID:** KNOT-ADR-041
**Date:** 2026-10-09
**Status:** Accepted

**Context:** Content responses named their author only by `author_id`, a UUID. The mobile app had no way to render a name or an avatar from that alone: there is no public "read a user by id" route, and adding one would fan a feed page out into a request per card. Attribution had therefore been filled in ad hoc — the inbox had actor names and avatars, comments had a name, the language tree and the feed had almost nothing — so the same author rendered differently on every screen.

**Decision:**
- Every response that names an author carries **`author_display_name`** (string) and **`author_avatar_url`** (a path on this API, or `null` when the author has no avatar), in addition to the existing `author_id` and `author_rooted`.
- The fields are attached at the **HTTP layer** by a single `authorAttributions` helper in `internal/httpapi/enrich.go`, which resolves every distinct author id in a response in **one batched call** to `identity.Service.UsersByIDs`. There is never a query per row, and no entity table is widened.
- `author_avatar_url` is the backend path `/users/{id}/avatar?v=…`, never a link to object storage. A client resolves it against the API base URL and renders the author's initials when it is `null`.
- A failed lookup is swallowed: attribution is supplementary, so the content still returns, with an empty name and a `null` avatar — the rule the Rooted enrichment already followed (KNOT-ADR-017).
- The mobile app renders attribution through **one component**, `AuthorLine`, so a story, a version, a comment, and a bridge all read the same way.

**Alternatives Considered:**
1. **Add name and avatar columns to stories, versions, comments, and bridges** — rejected. It denormalizes identity into four domains, goes stale on a rename or an avatar change, and each row already stores the author's id.
2. **A public `GET /users/{id}` route for the client to call** — rejected for this. It turns a feed page into N+1 requests, and it exposes a profile surface the product has not designed (that is KNOT-015c).
3. **Resolve attribution per row inside each handler** — rejected. A page of 20 comments would be 20 lookups; the batch is one.
4. **Enrich only the detail responses** — rejected. The feed and the language tree were exactly the screens missing attribution.

**Reason:** Attribution is a presentation concern over an id the entity already stores, and it has the same shape in every domain. Doing it once, in one batched read at the edge, keeps it consistent, keeps the storage model normalized, and keeps the cost independent of page size.

**Consequences:** Every authored response gains two fields and every content handler gains one dependency (`AuthorLookup`), plus one batched read per response. The wire format grows additively, so a client that ignores the fields is unaffected. The notifications inbox already resolved actors this way; its `NotificationActors` is now an alias of the same `AuthorLookup` contract, so there is one enrichment interface rather than two.

## KNOT-ADR-042 — The profile wall unions the four content tables; chronological, cursor-paginated, public

**Decision ID:** KNOT-ADR-042
**Date:** 2026-10-09
**Status:** Accepted

**Context:** A profile page must answer "who is this person, and what have they made?" from any author mention. Everything a user authors lives in four tables owned by four domains: `stories`, `story_versions`, `comments`, and `bridges`. The obvious shapes were a new `activities` table written by every create, or four separate paginated lists merged on the client.

**Decision:**
- The wall is a **read model**, not a table. `internal/profile` holds an `ActivityStore` whose one method runs a single `UNION ALL` over the four tables, filtered by `author_id = $1`, ordered by `(created_at DESC, id DESC)`, and cursor-paginated with the same opaque `(created_at, id)` cursor the feed and a thread use.
- Each branch builds its **payload** in SQL with `jsonb_build_object`, so the context a card needs travels with the row. A page is one query, never N+1. A comment preview is `left(body, 200)`, so a full body never leaves through the wall.
- The kinds are `story`, `version`, `comment`, and `bridge`. **One act is one activity:** a story's root version is excluded from the `version` branch (the root *is* the story), and a bridge's target comment is excluded from the `comment` branch (the target is the bridge's artifact).
- The wall is **public**. There is no private activity and no per-viewer filtering. It exposes only what the content endpoints already expose publicly, plus the owner's display name and avatar.
- `GET /users/{id}/profile` returns the owner header (`id`, `display_name`, `avatar_url`, `rooted`, `joined_at`) and one page of activities. The owner is resolved through `identity.UserByID`, so an unknown id is a 404 even for an empty wall; the Rooted summary is attached with the existing batched helper.

**Alternatives Considered:**
1. **A materialised `activities` table** — rejected. It denormalizes the four domains, must be written on every create in each of them, kept correct on delete (comments and bridges cascade), and backfilled; the read model is correct by construction.
2. **Four paginated lists merged on the client** — rejected. Correct interleaving and a single cursor are exactly what a union gives for free; the client would need to hold four cursors and merge-sort pages.
3. **Listing root versions and bridge target comments as separate activities** — rejected. Both are second rows written by one act; listing them makes one action read as two.
4. **A per-activity author object** — rejected. Every activity is by the wall's owner, so the author is the header; repeating it per row is payload bloat.

**Reason:** The four tables are already the source of truth and each has an author-id index. A single indexed union with a JSON payload is one query, stays correct when content changes, and adds no write path to four domains.

**Consequences:** The wall is eventually consistent only with the tables themselves (there is nothing else to keep in sync), and a new authored entity type means one more `UNION ALL` branch. Ordering across kinds relies on `gen_random_uuid` ids being distinct enough for the `id` tiebreaker, which holds in practice. The endpoint is public and unauthenticated, matching the content endpoints it summarizes.

## KNOT-ADR-043 — AuthorLine is a tap target for the author's profile; a nested Pressable isolates the tap

**Decision ID:** KNOT-ADR-043
**Date:** 2026-10-09
**Status:** Accepted

**Context:** Attribution is rendered in one component, `AuthorLine`, on the feed, story detail, language tree, comment thread, and bridge screen. The product wants tapping an author to open their profile everywhere — but on the feed, an `AuthorLine` sits inside a card whose own tap opens the story, so a tap must open exactly one thing.

**Decision:**
- `AuthorLine` takes an optional `onPress`. When it is given, the **avatar and name** are wrapped in a `Pressable`; when it is omitted, they are plain and are not a tap target.
- The **Rooted badge and the relative timestamp are never part of the tap target** — they are informational, and keeping them out means the tap area is exactly "the author".
- Every screen that renders attribution passes `onPress={() => onOpenUserProfile(authorId)}`, and `App.tsx` pushes a single `userProfile` overlay. The overlay serves both the owner and everyone else: it shows the owner controls (Edit avatar, Sign out) only when the id is the signed-in user's.
- The nested `Pressable` is what avoids conflict with the parent card: React Native's responder system grants the touch to the deepest view that wants it, so the child's `onPress` fires and the parent card's does not.

**Alternatives Considered:**
1. **Make the whole `AuthorLine` pressable** — rejected. It would swallow the timestamp and badge into a link and enlarge the tap area beyond "the author".
2. **A separate "view profile" button per card** — rejected. It adds chrome to every card and to the design surface, and the author's name is the natural affordance.
3. **Stop propagation manually from the parent card** — rejected. It puts the child's knowledge in the parent and is the wrong direction; the child owning its tap is the correct model.
4. **Only make the name tappable, not the avatar** — rejected. The avatar is the more common tap target on a phone, and the two read as one identity.

**Reason:** Attribution already flows through one component; adding one optional callback keeps the tap behavior in one place, and the responder system already resolves the nested-tap case without global state or event plumbing.

**Consequences:** The nested-tap behavior must be verified on a device (Android especially), because the responder system's behavior in a `FlatList` cell is not exercised by the unit tests. Screens that show an `AuthorLine` but cannot navigate to a profile simply omit `onPress`, and the line renders unchanged.

---

## KNOT-ADR-044 — The Profile tab and an author's wall are the same screen

**Decision ID:** KNOT-ADR-044
**Date:** 2026-10-09
**Status:** Accepted

**Context:** Two screens claimed the word "profile". The Profile tab rendered `ProfileScreen`, a Rooted-centric view: the signed-in user's avatar, display name, email, and their Rooted signals. Tapping an author anywhere else opened `UserProfileScreen`, the activity wall introduced with KNOT-ADR-042. They disagreed about what a profile is, and on device the founder saw the Profile tab showing a wall-less screen while an author tap showed the new wall.

**Decision:**
- `UserProfileScreen` is the only profile screen. The Profile tab renders it for the signed-in user, and the `userProfile` overlay renders it for anyone else.
- `showBackButton` (default `true`) hides the "← Back" link on the tab, and `onBack` is optional: a tab is a destination, not something to return from. The link renders only when both are satisfied.
- The owner controls — Edit avatar, Set Rooted, Sign out — all live on that one screen and render only when `isOwnProfile` is true.
- `ProfileScreen.tsx` is deleted.

**Alternatives Considered:**
1. **Add the wall to `ProfileScreen` and keep both screens** — rejected. Two screens rendering one concept is the drift this decision removes; every future change would have to be made twice, and the two would diverge again.
2. **Keep `ProfileScreen` for the tab, the wall only for overlay taps** — rejected. This is the status quo the founder rejected on device.
3. **Delete `UserProfileScreen` and use the Rooted screen everywhere** — rejected. It removes the wall, which is the feature.
4. **Show the back link on the tab as well, pointing at the feed** — rejected. A back affordance on a bottom-tab root tells the user something untrue about the navigation model.

**Reason:** One screen, one definition of "profile". The tab and the pushed overlay then differ only in chrome, so a change to the wall lands in both by construction rather than by discipline.

**Consequences:** The Profile tab now issues the same `GET /users/{id}/profile` request as any other wall instead of `GET /users/me/rooted`. A consequence to be aware of: the old screen listed every Rooted signal, private ones included, and the wall shows only the current Rooted badge plus the Set Rooted action, so **that list is no longer reachable in the app**. Restoring it means adding it to the wall, not reviving a second screen. `rootedApi.getMySignals` and `rootedApi.getUserSignals` in the mobile client are now unused but are kept, because they bind endpoints that still exist.

---

## KNOT-ADR-045 — A language is a canonical ISO 639-1 code, listed once per tier and matched exactly

**Decision ID:** KNOT-ADR-045
**Date:** 2026-10-09
**Status:** Accepted

**Context:** Every domain that stores a language — `users.preferred_languages`, `stories`/`story_versions.language`, `comments.language`, `bridges.target_language`, and the discovery cluster filter — validated with its own hand-written rule: trim, lower-case, then require 2-8 ASCII letters. That accepted `english`, `tsonga`, and `EN` as three spellings of what a person meant, so the same language could be stored several ways and grouping by language would silently split. The mobile app asked users to type a code into a text field, in one case comma-separated.

**Decision:**
- `backend/go/internal/language` is the single source of truth: 103 `{code, name}` entries, sorted by name, exposed as a compile-time constant rather than a database table.
- Every language field is validated with `language.IsValid(code)`, which matches **exactly**. `en` is accepted; `EN`, `Eng`, `eng`, `English`, `e`, `nso`, `zz`, and `en-ZA` are rejected with `400 validation_error`. Whitespace is still trimmed, and a blank entry in `preferred_languages` is still dropped.
- `GET /languages` serves the list publicly, so a client can offer a picker before anyone has registered. It is a `Router` method, like `handleHealth`, rather than a new handler type, which keeps `NewRouter`'s signature unchanged.
- `apps/mobile/src/data/languages.ts` mirrors the list, and `LanguagePicker` is a searchable in-memory picker over it, in `single` and `multiple` modes. It replaces the free-text language fields on Register, Create Story, Adapt Story, and Bridge.
- Language display goes through `languageName(code)`, so a wall or a comment card shows "Zulu" rather than "zu", falling back to the raw code for a value the list does not know.
- Region variants (`pt-BR`) and three-letter codes with no ISO 639-1 code are out of scope, so **Northern Sotho's `nso` is not offered**.

**Alternatives Considered:**
1. **A `languages` database table** — rejected. The list changes rarely, and a compile-time constant is a lookup that cannot fail or need seeding, migrating, or an extra round trip.
2. **Normalise case and accept `EN`** — rejected. It re-introduces the second spelling the decision exists to remove, and it makes "did the client send what I expect" unanswerable.
3. **A BCP 47 library** — rejected. It would be a new dependency on both tiers for a rule the product does not need: Knot stores a language, not a locale.
4. **Validate against a list fetched from the server on each app launch** — rejected. It makes every form depend on a second request and on connectivity, for data that ships with the binary.
5. **A `CHECK` constraint in PostgreSQL** — rejected as the primary mechanism. It would need a migration per language added and would put the list in two places; the service layer is where every other field rule already lives.
6. **Force the picker into the comment composer** — rejected. The composer is a fixed-height bar and the picker's results list has nowhere to expand there, so the composer keeps a compact code field.

**Reason:** Consistency at the boundary is cheaper than reconciliation afterwards. A language is an identifier, not free text, and the only way to guarantee that one language has one spelling is to accept exactly the codes that exist.

**Consequences:** Adding a language is a two-file change (Go and TypeScript) plus a deploy, and the mobile test suite reads the Go source to fail when the two lists drift, skipping that check when only the mobile folder is present. Two lists rather than one is a real duplication cost, accepted because the app must work offline and the server cannot be the app's only source of truth. The comment composer's code field is still validated against the canonical list, so it cannot store a non-canonical code, but it offers no suggestions — a disclosed inconsistency with the other four forms. Finally, tightening validation is not retroactive: rows written under the old rule keep their values, and any such value simply no longer validates if it is sent back.
