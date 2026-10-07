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
