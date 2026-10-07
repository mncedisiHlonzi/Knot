# Knot — Decisions

Architecture Decision Records (ADRs) for Knot. Each decision is recorded before (or at
the time it is) implemented. Statuses: **Proposed**, **Accepted**, **Superseded**.

---

## KNOT-ADR-001 — Clean-room start; `as-told-by` is out of scope

- **Decision ID:** KNOT-ADR-001
- **Status:** Accepted

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
