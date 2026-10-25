-- 0021_identity_domain.up.sql
--
-- The identity domain's storage (KNOT-030; platform ADRs KNOT-ADR-079 through
-- KNOT-ADR-081).
--
-- Until now a user was one row in `users` and one argon2id hash in
-- `users.password_hash`. That is enough for a single sign-in method and a
-- stateless JWT, and not enough for anything else: no way to add a second method
-- to an account, no way to see or end a session, no way to prove an address
-- belongs to its owner, and no way to delete an account without destroying the
-- content other people are still reading.
--
-- Three tables carry that weight:
--
--   * `credentials`  — one row per way a user can prove who they are.
--   * `sessions`     — one row per signed-in device, keyed by the hash of an
--                      opaque refresh token. The token itself is never stored,
--                      so a database dump cannot be replayed.
--   * `verification_tokens` — one row per single-use proof of address ownership.
--
-- `users.password_hash` is deliberately NOT dropped and NOT touched. It is now
-- the deprecated home of the password secret; moving it into `credentials` is a
-- later task that also has to re-hash nothing and break no session. Until then
-- both rows can exist for the same user, and the service layer is what keeps
-- them in step.
--
-- Soft delete is two columns on `users` rather than a table of its own:
-- `deleted_at` marks the account as gone to the API, `deletion_scheduled_at`
-- marks when the 30-day recovery window closes and personal data is purged
-- (KNOT-ADR-081). Rows are never removed on request, because a story, a
-- comment or a rooted signal outlives the account that wrote it and is read by
-- people who are not that account.
--
-- The migration runner wraps this file in a transaction, and every statement is
-- IF NOT EXISTS, so the file is safe to run twice by hand.

-- 1a. Credentials: one row per authentication method.
--
-- `credential_data` is TEXT rather than JSONB because each method stores exactly
-- one opaque string: an argon2id PHC string for 'password', the provider's
-- subject id for 'oauth_google', a single-use secret for 'magic_link'. The
-- column therefore needs no shape, and the CHECK constraint is what keeps the
-- set of methods closed.
CREATE TABLE IF NOT EXISTS credentials (
    id              UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    user_id         UUID NOT NULL REFERENCES users (id) ON DELETE CASCADE,
    credential_type TEXT NOT NULL CHECK (credential_type IN ('password', 'oauth_google', 'magic_link')),
    credential_data TEXT NOT NULL,
    created_at      TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at      TIMESTAMPTZ NOT NULL DEFAULT now(),
    UNIQUE (user_id, credential_type)
);

-- One row per user per method, so this index serves the lookups the store makes
-- ("every method for this user") as well as the cascade from `users`.
CREATE INDEX IF NOT EXISTS credentials_user_id_idx ON credentials (user_id);

-- 1b. Sessions: one row per signed-in device.
--
-- `refresh_token_hash` is a SHA-256 of the opaque token handed to the client;
-- the token itself exists only in the client's hands and in Redis, never here.
-- `device_info` is free-form JSONB on purpose: the shape of a user agent is not
-- the database's business, and a device-management screen wants to render
-- whatever the client sent rather than a column per field.
CREATE TABLE IF NOT EXISTS sessions (
    id                 UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    user_id            UUID NOT NULL REFERENCES users (id) ON DELETE CASCADE,
    refresh_token_hash TEXT NOT NULL UNIQUE,
    device_info        JSONB NOT NULL DEFAULT '{}',
    ip_address         INET,
    last_used_at       TIMESTAMPTZ NOT NULL DEFAULT now(),
    revoked_at         TIMESTAMPTZ,
    created_at         TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX IF NOT EXISTS sessions_user_id_idx ON sessions (user_id);
-- The UNIQUE constraint above already has a btree behind it, so this index is
-- strictly redundant today. It is created because the approved schema lists it
-- explicitly; dropping it later is a one-line, zero-risk change if it proves to
-- be pure write overhead.
CREATE INDEX IF NOT EXISTS sessions_refresh_token_hash_idx ON sessions (refresh_token_hash);

-- 1c. Verification tokens: single-use, hashed, expiring.
--
-- Like session tokens, only the hash is stored, so the table is useless to
-- anyone who reads it. `consumed_at` is a timestamp rather than a boolean so a
-- support question ("when was this used?") has an answer without a second table.
CREATE TABLE IF NOT EXISTS verification_tokens (
    id         UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    user_id    UUID NOT NULL REFERENCES users (id) ON DELETE CASCADE,
    token_hash TEXT NOT NULL UNIQUE,
    token_type TEXT NOT NULL CHECK (token_type IN ('email_verification', 'password_reset')),
    expires_at TIMESTAMPTZ NOT NULL,
    consumed_at TIMESTAMPTZ,
    created_at  TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX IF NOT EXISTS verification_tokens_user_id_idx ON verification_tokens (user_id);
CREATE INDEX IF NOT EXISTS verification_tokens_token_hash_idx ON verification_tokens (token_hash);

-- 1d. Soft delete on users.
--
-- Both columns are nullable and neither has a default: an account that has never
-- been deleted has no deletion to record. `deletion_scheduled_at` is written at
-- the same moment as `deleted_at` (now() + 30 days) but kept separate so the
-- window can be shortened or extended for one account without rewriting history.
ALTER TABLE users ADD COLUMN IF NOT EXISTS deleted_at TIMESTAMPTZ;
ALTER TABLE users ADD COLUMN IF NOT EXISTS deletion_scheduled_at TIMESTAMPTZ;

-- Partial, because the overwhelming majority of rows have no deletion and a full
-- index would be almost entirely NULL. This is the index a purge job scans.
CREATE INDEX IF NOT EXISTS users_deleted_at_idx ON users (deleted_at) WHERE deleted_at IS NOT NULL;
