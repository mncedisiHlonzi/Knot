-- 0022_email_verified.down.sql
--
-- Reverses 0022_email_verified.up.sql (KNOT-031).
--
-- This is destructive in one direction only, and it is worth being explicit about
-- which: dropping the column erases every proof that an address was verified, so
-- rolling back returns every account to "never proven". Accounts and passwords are
-- untouched, so nobody is locked out permanently — the next sign-in simply has to
-- verify again once the column is restored.
--
-- IF EXISTS on both statements keeps the file safe to run twice or after a partial
-- manual change.

DROP INDEX IF EXISTS users_email_verified_at_idx;

ALTER TABLE users DROP COLUMN IF EXISTS email_verified_at;
