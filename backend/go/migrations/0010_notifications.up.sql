-- 0010_notifications.up.sql
--
-- Notifications: an in-app record that one user's content was acted on by
-- another user.
-- Column reference: docs/DATA_MODEL.md, KNOT-ADR-038, KNOT-ADR-039.
--
-- A notification names the affected user (`user_id`), the user who acted
-- (`actor_id`), what happened (`event_type`), and the entity the client should
-- open (`entity_type`, `entity_id`). It is delivered in the app only: there is no
-- push channel, no device token, and no delivery state here (KNOT-ADR-039).
--
-- `entity_id` is a bare UUID with no foreign key. The entity it names may be a
-- version, a comment, or a bridge, and a single polymorphic column cannot
-- reference three tables. Nothing deletes the referenced row today; if that
-- changes, the notification is a dangling pointer rather than a broken read, and
-- the client treats a missing entity as a dead end.
--
-- The event set is closed by a CHECK, so an unknown event cannot be written even
-- by a future mistake in Go. `actor_id <> user_id` is enforced here as well as in
-- the service, because a self-notification is never meaningful: acting on your own
-- content does not notify you (KNOT-ADR-038).
--
-- Notifications are never deleted, only marked read: `read_at` is the single
-- piece of mutable state, set by one UPDATE.
--
-- The migration runner wraps this file in a transaction.

CREATE TABLE notifications (
	id           UUID PRIMARY KEY DEFAULT gen_random_uuid(),
	user_id      UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
	actor_id     UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
	event_type   TEXT NOT NULL CHECK (event_type IN
	                ('version.created', 'comment.created', 'bridge.created')),
	entity_type  TEXT NOT NULL CHECK (entity_type IN
	                ('story', 'version', 'comment', 'bridge')),
	entity_id    UUID NOT NULL,
	read_at      TIMESTAMPTZ NULL,
	created_at   TIMESTAMPTZ NOT NULL DEFAULT now(),
	CHECK (actor_id <> user_id)
);

-- Serves the inbox page: one user's notifications, newest first, and the keyset
-- seek that resumes after the last row of a page.
CREATE INDEX notifications_user_created_idx ON notifications (user_id, created_at DESC, id DESC);

-- Serves the unread count and "mark all read" without scanning read rows. It is a
-- partial index, so it only covers the rows that are still unread.
CREATE INDEX notifications_user_unread_idx ON notifications (user_id) WHERE read_at IS NULL;
