-- 0008_story_media.up.sql
--
-- Story media: the images and videos attached to a story.
-- Column reference: docs/DATA_MODEL.md, KNOT-ADR-030.
--
-- A story's media is a first-class table rather than the `stories.media_urls`
-- array that preceded it. A row records where the object lives in the media
-- bucket (`storage_key`), how it should be rendered (`media_type`, `mime_type`,
-- `width`, `height`, `duration_ms`), and — the point of the feature — whether it
-- was captured with the device camera or chosen from the gallery (`source`).
--
-- `storage_key` holds the OBJECT KEY in the media bucket (for example
-- "story-media/<story id>/<uuid>.jpg"), never a public URL: the bucket stays
-- private and every byte reaches a client through the backend (KNOT-ADR-029).
--
-- Deletion cascades from both the story and the uploader. An uploader is
-- normally the story's author (only the author may attach media in the MVP), but
-- the column is recorded separately so a future collaborative story can keep
-- attributing each file to whoever contributed it.
--
-- The migration runner wraps this file in a transaction.

CREATE TABLE story_media (
	id              UUID PRIMARY KEY DEFAULT gen_random_uuid(),
	story_id        UUID NOT NULL REFERENCES stories(id) ON DELETE CASCADE,
	uploader_id     UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
	storage_key     TEXT NOT NULL,
	media_type      TEXT NOT NULL CHECK (media_type IN ('image', 'video')),
	mime_type       TEXT NOT NULL,
	source          TEXT NOT NULL CHECK (source IN ('camera', 'gallery')),
	width           INTEGER NULL,
	height          INTEGER NULL,
	duration_ms     INTEGER NULL,
	size_bytes      BIGINT NOT NULL,
	display_order   INTEGER NOT NULL DEFAULT 0,
	created_at      TIMESTAMPTZ NOT NULL DEFAULT now()
);

-- Reads every item of one story in presentation order, which is what the story
-- detail and the media list both do.
CREATE INDEX story_media_story_id_order_idx ON story_media (story_id, display_order);

-- Serves "media by this uploader" and the cascade on user deletion.
CREATE INDEX story_media_uploader_id_idx ON story_media (uploader_id);
