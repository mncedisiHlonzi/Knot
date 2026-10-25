-- 0022_email_verified.up.sql
--
-- Email verification becomes a condition of sign-in (KNOT-031; platform ADR
-- KNOT-ADR-083).
--
-- Until now an account could log in the moment it existed, whether or not the
-- address on it belonged to anyone. That is fine for a prototype and wrong for a
-- platform: an unverified address is how an account is taken over (register
-- someone else's email, or reset a password into an inbox that is not yours), and
-- it is how a notification system ends up sending mail to strangers.
--
-- `users.password_hash` and everything else on the row are untouched. The column
-- is nullable with no default because an account begins unverified: NULL is the
-- honest representation of "never proven", and it is what the sign-in check tests.
--
-- The migration runner wraps this file in a transaction and every statement is
-- IF NOT EXISTS, so the file is safe to run twice by hand.

ALTER TABLE users ADD COLUMN IF NOT EXISTS email_verified_at TIMESTAMPTZ;

-- Partial, because the index exists for the opposite of the common case: it
-- answers "who has not verified yet", which is a small and shrinking set, and a
-- full index would be almost entirely NULL once the product has real users.
CREATE INDEX IF NOT EXISTS users_email_verified_at_idx ON users (email_verified_at) WHERE email_verified_at IS NULL;
