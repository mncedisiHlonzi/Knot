-- 0015_moderation.up.sql
--
-- Moderation foundation: user roles, blocks, and content reports.
-- Column reference: docs/DATA_MODEL.md, docs/SAFETY.md, KNOT-ADR-059..062.
--
-- This is part 1 of 2. It creates the whole moderation schema, but KNOT-017a
-- only *uses* blocks, reports, moderation_cases, and audit_log. The
-- moderation_actions table is created here so 017b can add the moderator queue
-- and actions without a second schema change.
--
--   role            on users, one of 'user' | 'moderator' | 'admin'. Assigned
--                   manually (there is no API to assign a role at MVP).
--   blocks          a directed block. Blocking is mutual hiding + write
--                   prevention, which the API layer applies; the table stores
--                   only the direction the blocker created.
--   reports         one report of one entity by one user. A user may report the
--                   same entity only once (unique index); multiple users' reports
--                   on the same entity aggregate into one moderation case.
--   moderation_cases    one open case per (entity_type, entity_id), with a
--                   denormalised report_count so the queue can sort without a
--                   COUNT per row.
--   moderation_actions  what a moderator did to a case (017b).
--   audit_log       an append-only trail of moderation events.
--
-- Idempotency: every table and index is created with IF NOT EXISTS, and the role
-- CHECK is added behind a pg_constraint guard, so this file can be re-applied by
-- hand without erroring. The migration runner also wraps it in a transaction.
--
-- `role` is a closed set enforced by a CHECK rather than a lookup table: roles
-- are a product decision (three values), not registry data, exactly as the
-- stories pillar and the notifications event set are handled.

ALTER TABLE users
	ADD COLUMN IF NOT EXISTS role TEXT NOT NULL DEFAULT 'user';

DO $$
BEGIN
	IF NOT EXISTS (
		SELECT 1
		FROM pg_constraint
		WHERE conname = 'users_role_check'
		  AND conrelid = 'users'::regclass
	) THEN
		ALTER TABLE users
			ADD CONSTRAINT users_role_check
			CHECK (role IN ('user', 'moderator', 'admin'));
	END IF;
END
$$;

-- Blocks. A row says blocker_id has blocked blocked_id. The unique index makes a
-- block idempotent (a second POST is a no-op), and the CHECK forbids blocking
-- yourself. Both directions are consulted when the API builds a viewer's hidden
-- set, so the reverse index on blocked_id is what makes "who blocked me" cheap.
CREATE TABLE IF NOT EXISTS blocks (
	id         UUID PRIMARY KEY DEFAULT gen_random_uuid(),
	blocker_id UUID NOT NULL REFERENCES users (id) ON DELETE CASCADE,
	blocked_id UUID NOT NULL REFERENCES users (id) ON DELETE CASCADE,
	created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
	CHECK (blocker_id <> blocked_id)
);

CREATE UNIQUE INDEX IF NOT EXISTS blocks_unique_pair
	ON blocks (blocker_id, blocked_id);

CREATE INDEX IF NOT EXISTS blocks_blocked_id_idx
	ON blocks (blocked_id);

-- Reports. entity_type is one of the six reportable kinds; entity_id points at
-- the row in its domain table. There is deliberately no foreign key on
-- entity_id: the six kinds live in six tables, so a single column cannot
-- reference them, and the API validates existence before inserting.
--
-- reports_one_per_user_per_entity makes a repeat report from the same user on the
-- same entity a unique violation (HTTP 409), which is how "you already reported
-- this" is detected.
CREATE TABLE IF NOT EXISTS reports (
	id          UUID PRIMARY KEY DEFAULT gen_random_uuid(),
	reporter_id UUID NOT NULL REFERENCES users (id) ON DELETE CASCADE,
	entity_type TEXT NOT NULL CHECK (entity_type IN
		('story', 'version', 'comment', 'bridge', 'inquiry', 'inquiry_answer')),
	entity_id   UUID NOT NULL,
	category    TEXT NOT NULL CHECK (category IN
		('harassment', 'hate_speech', 'misinformation', 'spam', 'sensitive_content', 'other')),
	reason      TEXT NULL,
	created_at  TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE UNIQUE INDEX IF NOT EXISTS reports_one_per_user_per_entity
	ON reports (reporter_id, entity_type, entity_id);

CREATE INDEX IF NOT EXISTS reports_entity_idx
	ON reports (entity_type, entity_id);

-- Moderation cases. One case per entity; report_count is incremented in the same
-- transaction that inserts a report, under a row lock, so two simultaneous
-- reports cannot both read the same count (the KNOT-ADR-054 pattern).
CREATE TABLE IF NOT EXISTS moderation_cases (
	id           UUID PRIMARY KEY DEFAULT gen_random_uuid(),
	entity_type  TEXT NOT NULL,
	entity_id    UUID NOT NULL,
	status       TEXT NOT NULL DEFAULT 'open' CHECK (status IN ('open', 'actioned', 'dismissed')),
	report_count INTEGER NOT NULL DEFAULT 0,
	created_at   TIMESTAMPTZ NOT NULL DEFAULT now(),
	updated_at   TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE UNIQUE INDEX IF NOT EXISTS moderation_cases_entity_unique
	ON moderation_cases (entity_type, entity_id);

-- Serves the 017b queue, which reads open/actioned/dismissed cases newest first.
CREATE INDEX IF NOT EXISTS moderation_cases_status_updated_idx
	ON moderation_cases (status, updated_at DESC);

-- Moderator actions on a case (017b). Created here so the schema is complete.
CREATE TABLE IF NOT EXISTS moderation_actions (
	id          UUID PRIMARY KEY DEFAULT gen_random_uuid(),
	case_id     UUID NOT NULL REFERENCES moderation_cases (id) ON DELETE CASCADE,
	actor_id    UUID NOT NULL REFERENCES users (id) ON DELETE CASCADE,
	action_type TEXT NOT NULL CHECK (action_type IN ('hide', 'warn', 'suspend', 'dismiss', 'unhide')),
	notes       TEXT NULL,
	created_at  TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX IF NOT EXISTS moderation_actions_case_id_idx
	ON moderation_actions (case_id, created_at ASC);

-- Audit log. Append-only. target_id is nullable because some actions (a role
-- change, a policy toggle) name no single entity. metadata carries the
-- event-specific detail as JSON so the trail can grow without a migration.
CREATE TABLE IF NOT EXISTS audit_log (
	id          UUID PRIMARY KEY DEFAULT gen_random_uuid(),
	actor_id    UUID NOT NULL REFERENCES users (id) ON DELETE CASCADE,
	action      TEXT NOT NULL,
	target_type TEXT NOT NULL,
	target_id   UUID NULL,
	metadata    JSONB NOT NULL DEFAULT '{}',
	created_at  TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX IF NOT EXISTS audit_log_created_idx
	ON audit_log (created_at DESC, id DESC);

CREATE INDEX IF NOT EXISTS audit_log_actor_idx
	ON audit_log (actor_id);
