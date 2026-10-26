-- 0023_account_purge.up.sql
--
-- The purge half of the account lifecycle (KNOT-034; platform ADR-089).
--
-- KNOT-030 added `deleted_at` and `deletion_scheduled_at`: a deleted account keeps
-- its row so that its content stays readable and attributable, and the window in
-- which the person can come back is recorded on the row itself. This migration adds
-- the one column the *purge* needs.
--
-- `purged_at` is what makes the purge idempotent. The job anonymises rather than
-- deletes, so a second run would otherwise rewrite the same rows forever, and —
-- worse — could rewrite the email address of an account that had already been
-- anonymised, changing a stable value under the eyes of anything holding it. With
-- `purged_at` set, "due" becomes a question the index can answer and a purged row
-- can never be purged again.
--
-- Nothing else changes: `deleted_at` and `deletion_scheduled_at` stay exactly as
-- ADR-081 defined them, and `users.password_hash` is cleared by the job rather than
-- by the schema.
--
-- The migration runner wraps this file in a transaction and every statement is
-- IF NOT EXISTS, so the file is safe to run twice by hand.

ALTER TABLE users ADD COLUMN IF NOT EXISTS purged_at TIMESTAMPTZ;

-- The index the purge job scans: "deleted, not yet purged, and past its window".
--
-- It is partial on purpose. Almost every row is neither deleted nor due, so a full
-- index would be nearly all NULL; this one holds only the accounts a purge can act
-- on, which is a small and shrinking set. The leading column is
-- `deletion_scheduled_at` because that is the predicate's range: the job asks for
-- everything due before now, and a btree on that column answers it with one range
-- scan.
CREATE INDEX IF NOT EXISTS users_purged_at_idx
    ON users (deletion_scheduled_at)
    WHERE purged_at IS NULL AND deletion_scheduled_at IS NOT NULL;
