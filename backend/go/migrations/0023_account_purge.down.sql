-- 0023_account_purge.down.sql
--
-- Reverses 0023_account_purge.up.sql (KNOT-034).
--
-- Rolling this back does **not** un-anonymise anything: a purge that has already run
-- has overwritten the account's email, display name and contact fields, cleared its
-- password, and deleted its credentials, sessions and verification tokens. Dropping
-- the column removes the record that it happened, which is why this down migration
-- is only safe on a database whose purges have not run yet.
--
-- What it does restore is the *ability* to run one: without `purged_at` the job
-- cannot tell a purged account from a merely deleted one, so the up migration has to
-- be re-applied before the purge is usable again.
--
-- IF EXISTS on both statements keeps the file safe to run twice or after a partial
-- manual change.

DROP INDEX IF EXISTS users_purged_at_idx;

ALTER TABLE users DROP COLUMN IF EXISTS purged_at;
