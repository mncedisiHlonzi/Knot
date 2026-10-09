-- 0007_user_avatar.up.sql
--
-- Identity: the user's avatar object.
-- Column reference: docs/DATA_MODEL.md
--
-- `avatar_url` holds the OBJECT KEY of the user's current avatar in the media
-- bucket (for example "avatars/<user id>/<uuid>.jpg"), not a public URL and not
-- the object store's internal URL. The key is what the server needs to find the
-- object again, and it cannot be derived from the public path because its
-- filename ends in a random UUID. API responses translate this key into the
-- backend path `/users/{id}/avatar?v=<uuid>.<ext>`, which is the only form a
-- client ever sees; the bucket stays private (KNOT-ADR-029).
--
-- NULL means the user has never uploaded an avatar. Replacing an avatar overwrites
-- this single column, and the handler deletes the previous object best-effort.
--
-- The migration runner wraps this file in a transaction.

ALTER TABLE users ADD COLUMN avatar_url TEXT NULL;
