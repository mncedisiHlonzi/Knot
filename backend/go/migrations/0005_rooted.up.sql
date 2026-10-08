-- 0005_rooted.up.sql
--
-- Rooted: a user's self-declared connection to a place, plus a duration bucket.
-- Column reference: docs/DATA_MODEL.md
--
-- Rooted is Knot's trust primitive. A signal is self-declared and never verified.
-- It carries a place at city or region precision only (no coordinates, no address)
-- and a duration bucket from a closed set. See KNOT-ADR-016 and KNOT-ADR-017.
--
-- is_primary exists for future multi-signal support: a person can be rooted in
-- more than one place. The partial unique index below enforces the MVP invariant
-- that a user has exactly one primary signal.
--
-- The migration runner wraps this file in a transaction, so the table and its
-- indexes either all appear or none do.

CREATE TABLE rooted_signals (
    id              UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    user_id         UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    place           TEXT NOT NULL,
    duration_bucket TEXT NOT NULL CHECK (duration_bucket IN
                        ('lifelong','many_years','several_years','a_few_years','recently')),
    is_public       BOOLEAN NOT NULL DEFAULT true,
    is_primary      BOOLEAN NOT NULL DEFAULT true,
    created_at      TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at      TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX rooted_signals_user_id_idx ON rooted_signals (user_id);

-- At most one primary signal per user. The predicate is what makes the index
-- partial, so a future multi-signal feature can add non-primary rows freely while
-- this invariant still holds.
CREATE UNIQUE INDEX rooted_signals_one_primary_per_user ON rooted_signals (user_id) WHERE is_primary = true;
