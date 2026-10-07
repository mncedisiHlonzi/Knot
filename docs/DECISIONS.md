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
