-- 0021_identity_domain.down.sql
--
-- Reverses 0021_identity_domain.up.sql (KNOT-030).
--
-- This down migration is genuinely destructive: the credentials, sessions and
-- verification tokens tables hold nothing that can be recomputed, so dropping
-- them signs every user out and discards every password credential stored in
-- `credentials`. `users.password_hash` is untouched by both directions, which is
-- exactly why the up migration left it in place: rolling back leaves password
-- sign-in working through the old column.
--
-- Order matters: the three tables go before the columns they do not depend on,
-- and each drop is IF EXISTS so the file is safe to run twice or after a partial
-- manual change.

DROP TABLE IF EXISTS verification_tokens;
DROP TABLE IF EXISTS sessions;
DROP TABLE IF EXISTS credentials;

-- Dropping the columns drops `users_deleted_at_idx` with them; the explicit DROP
-- INDEX keeps the intent readable and is a no-op if PostgreSQL got there first.
DROP INDEX IF EXISTS users_deleted_at_idx;

ALTER TABLE users DROP COLUMN IF EXISTS deletion_scheduled_at;
ALTER TABLE users DROP COLUMN IF EXISTS deleted_at;
