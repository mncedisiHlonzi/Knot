-- 0001_users.up.sql
--
-- Identity foundation: the users table.
-- Column reference: docs/DATA_MODEL.md
--
-- gen_random_uuid() is built into PostgreSQL 13+ (no pgcrypto extension needed);
-- Knot targets PostgreSQL 16.
-- The email uniqueness index is explicitly named (users_email_key) so it can be
-- referred to by name in operational tooling.

CREATE TABLE users (
    id                   UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    email                TEXT NOT NULL,
    phone                TEXT NULL,
    password_hash        TEXT NOT NULL,
    display_name         TEXT NOT NULL,
    preferred_languages  TEXT[] NOT NULL DEFAULT '{}',
    approximate_location TEXT NULL,
    created_at           TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at           TIMESTAMPTZ NOT NULL DEFAULT now(),
    CONSTRAINT users_email_key UNIQUE (email)
);
