# Knot — Data Model

**Status: MVP, subject to change.** Eight tables exist today: `users`, `stories`,
`story_versions`, `comments`, `bridges`, `rooted_signals`, `story_media`, and
`notifications`.

Migrations live in `backend/go/migrations/` and are applied with
`go run ./cmd/knot migrate up`. See the "Migrations" section of
[`docs/DEVELOPMENT.md`](DEVELOPMENT.md).

## Migration history

| Version | Name      | What it creates                              |
| ------- | --------- | -------------------------------------------- |
| `0001`  | `users`   | The `users` table and its email index        |
| `0002`  | `stories` | The `stories` table, its indexes, and its pillar constraint |
| `0003`  | `story_versions` | The `story_versions` table, its indexes and constraints, and the `stories` refactor that moves content into a root version |
| `0004`  | `conversations` | The `comments` and `bridges` tables, their indexes and constraints |
| `0005`  | `rooted`  | The `rooted_signals` table, its indexes, and its duration-bucket constraint |
| `0006`  | `discovery` | The `stories.approximate_location_lower` column, its backfill, and its index |
| `0007`  | `user_avatar` | The `users.avatar_url` column |
| `0008`  | `story_media` | The `story_media` table, its indexes and constraints |
| `0009`  | `structured_place` | The `stories` and `rooted_signals` `latitude`/`longitude`/`place_country` columns |
| `0010`  | `notifications` | The `notifications` table, its inbox index, and its unread partial index |
| `0011`  | `iso_639_3` | Rewrites every stored language from a two-letter ISO 639-1 code to its three-letter ISO 639-3 counterpart |

Applied versions are recorded in the `schema_migrations` table, which the runner creates
on first use.

### Migration `0011`: ISO 639-1 to ISO 639-3

KNOT-015d stored ISO 639-1 codes. KNOT-015d-fix moved the contract to ISO 639-3
(KNOT-ADR-046), which names every language rather than the 184 that happen to have a
two-letter code, so existing rows had to be rewritten. The migration touches the
four columns that hold a language — `story_versions.language`, `comments.language`,
`bridges.target_language`, and `users.preferred_languages` — and nothing else.

It carries the 184 ISO 639-1 codes and their ISO 639-3 counterparts in a temporary
mapping table, taken from the `Part1` column of the SIL reference table, so the map
agrees with the canonical list by construction. Every statement matches only values
that are exactly two characters, which makes the migration idempotent and means a
two-letter value with no counterpart is left alone rather than guessed at. The map is
dropped at the end of the same transaction, so no permanent object is added and the
schema's shape is unchanged — only the values in four columns.

The `down` migration is **best effort**: it reverses the 184 codes that have a
two-letter counterpart and leaves every other three-letter code as it is. Codes such
as `nso` (Sepedi) therefore survive a round trip unchanged, because a stored code
carries no record of which migration wrote it. Nothing is deleted and no field is
nulled in either direction.

## `users`

| Column                 | Type                     | Nullable | Default            | Notes                                            |
| ---------------------- | ------------------------ | -------- | ------------------ | ------------------------------------------------ |
| `id`                   | `uuid`                   | no       | `gen_random_uuid()` | Primary key. `gen_random_uuid()` is built into PostgreSQL 13+; Knot targets 16 |
| `email`                | `text`                   | no       | —                  | Unique. Stored lower-cased by the application and matched case-insensitively |
| `phone`                | `text`                   | yes      | `NULL`             | Optional contact number                          |
| `password_hash`        | `text`                   | no       | —                  | argon2id PHC string. Never returned by the API   |
| `display_name`         | `text`                   | no       | —                  | 1-80 characters                                  |
| `preferred_languages`  | `text[]`                 | no       | `'{}'`             | ISO 639-3 codes, at most 20. The application always writes an array, never `NULL`, and rejects any code outside the canonical list (KNOT-ADR-046) |
| `approximate_location` | `text`                   | yes      | `NULL`             | Coarse location only; precise location is out of scope |
| `avatar_url`           | `text`                   | yes      | `NULL`             | Object key of the avatar in the media bucket, **not** a public URL. See below |
| `created_at`           | `timestamptz`            | no       | `now()`            |                                                  |
| `updated_at`           | `timestamptz`            | no       | `now()`            | Set on insert, and refreshed by the avatar update. No trigger yet: updates are a later task |

### Indexes and constraints

| Name              | Kind                       | Columns | Purpose                                   |
| ----------------- | -------------------------- | ------- | ----------------------------------------- |
| `users_pkey`      | Primary key                | `id`    | Row identity                              |
| `users_email_key` | Unique constraint (btree)  | `email` | Prevents duplicate accounts; named so operational tooling can refer to it |

The uniqueness rule is enforced in the database, not only in application code: a
duplicate insert surfaces to the service as `identity.ErrEmailTaken`, which the API
reports as `409 email_taken`.

### `avatar_url` holds a key, not a URL

The column name is `avatar_url`, but the value is the **object key** inside the media
bucket — for example `avatars/7c0c1bfb-acf5-48ad-a3ba-4ea6617e05d8/0f8b1c2d-….png`.

It stores the key rather than deriving one from the user id because the object name ends
in a random UUID: given only the public path there would be no way to find the object
again. `NULL` means the user has never uploaded an avatar.

The object store itself is never addressed by a client. The API translates the key into
`/users/{id}/avatar?v=<object name>`, and only the backend reads the bytes back out
(KNOT-ADR-028, KNOT-ADR-029). Replacing an avatar overwrites this one column; the previous
object is deleted best-effort by the upload handler.

### Deliberate omissions

- **No `updated_at` trigger.** The avatar update sets `updated_at = now()` itself; a
trigger would be the right move once several columns need it, and is a later task.
- **No soft deletes, no roles, no email-verification flags.** None of them are needed by
  the identity foundation, and each would be a decision to make on its own evidence.

## `stories`

A story is one cultural artefact. Every later content feature (Tell My People, the
Language Tree, Conversations, Bridges) references `stories.id`.

| Column                 | Type           | Nullable | Default             | Notes                                                       |
| ---------------------- | -------------- | -------- | ------------------- | ----------------------------------------------------------- |
| `id`                   | `uuid`         | no       | `gen_random_uuid()` | Primary key                                                 |
| `author_id`            | `uuid`         | no       | —                   | References `users(id)`. The original author of the story, and the author of its root version. Taken from the access token, never the request body |
| `pillar`               | `text`         | no       | —                   | `CHECK (pillar IN ('wonder', 'heritage'))`                   |
| `root_version_id`      | `uuid`         | no       | —                   | References `story_versions(id)`. The story's root version; see [the root version invariant](#the-root-version-invariant) |
| `approximate_location` | `text`         | yes      | `NULL`              | Coarse location only; precise location is out of scope        |
| `latitude`             | `double precision` | yes | `NULL`           | Structured place coordinate, or `NULL`. Set together with `longitude` (migration `0009`, KNOT-ADR-034) |
| `longitude`            | `double precision` | yes | `NULL`           | Structured place coordinate, or `NULL`. The pair is the cluster key in discovery |
| `place_country`        | `text`         | yes      | `NULL`              | The country the geocoder reported, or `NULL`; at most 100 characters |
| `approximate_location_lower` | `text`   | yes      | `NULL`              | Normalised `lower(trim(approximate_location))`, maintained on write and backfilled by `0006`; groups stories by place. No coordinates, no geocoding |
| `media_urls`           | `text[]`       | no       | `'{}'`              | The application always writes an array, never `NULL`          |
| `sensitive`            | `boolean`      | no       | `false`             | Marks a story that should not be surfaced without care        |
| `created_at`           | `timestamptz`  | no       | `now()`             | The feed's primary sort key                                  |
| `updated_at`           | `timestamptz`  | no       | `now()`             | Set on insert. No trigger yet: editing a story is a later task |

The `language`, `title`, and `body` columns that lived here before migration `0003` now
live on `story_versions`, where the story's content belongs. See
[`story_versions`](#story_versions).

### Indexes and constraints

| Name                      | Kind                         | Columns                        | Purpose                                                        |
| ------------------------- | ---------------------------- | ------------------------------ | --------------------------------------------------------------- |
| `stories_pkey`            | Primary key                  | `id`                           | Row identity                                                     |
| `stories_created_at_id_idx` | Btree index (descending)   | `(created_at DESC, id DESC)`   | Serves the feed's `ORDER BY` and its keyset seek together        |
| `stories_author_id_idx`   | Btree index                  | `author_id`                    | "Stories by this person", and the cascade on user deletion       |
| `stories_pillar_idx`      | Btree index                  | `pillar`                       | Filtering the feed by pillar                                     |
| `stories_approximate_location_lower_idx` | Btree index | `approximate_location_lower` | Groups stories into place clusters for discovery                 |
| `stories_root_version_id_unique` | Unique index (btree)  | `root_version_id`              | A root version anchors at most one story, and a story's root is looked up by it |
| `stories_author_id_fkey`  | Foreign key                  | `author_id` → `users(id)`      | `ON DELETE CASCADE`: deleting an account removes its stories     |
| `stories_root_version_id_fkey` | Foreign key             | `root_version_id` → `story_versions(id)` | `ON DELETE RESTRICT`: a story always has a root, so its root version cannot be deleted while the story exists |
| `stories_pillar_check`    | Check constraint             | `pillar`                       | The pillar set is closed in the database, not only in Go         |

The composite index is load-bearing, not decorative: the feed selects a page with
`WHERE (created_at, id) < ($1, $2) ORDER BY created_at DESC, id DESC LIMIT $3`. That
index is **descending on both columns and in the same order**, so PostgreSQL seeks
directly to the resume point instead of sorting the table. It is deliberately separate
from `stories_author_id_idx`: a filter on `author_id` needs a different column order
than a feed page does.

### Place normalisation for discovery

`approximate_location_lower` is a normalised copy of `approximate_location`, added in migration
`0006`. Grouping stories by place has to fold "Cape Town" and "cape town" into one place, so
discovery groups on `lower(trim(approximate_location))` rather than on the raw column. The
application writes the column on story create — the `INSERT` computes `lower(trim($3))`, so it
cannot drift from the raw value — and migration `0006` backfilled the rows that already
existed.

This is normalisation-for-grouping only: there are no coordinates, no spatial index, and no
geocoding. The mobile client resolves a place name to a point with a local lookup table, and a
place it does not know is listed rather than plotted. See KNOT-ADR-020, and
[`Discovery`](API.md#discovery) for the read endpoints.

### Structured place data (migration `0009`)

`latitude`, `longitude`, and `place_country` were added to `stories` (and to
`rooted_signals`) in migration `0009`. They carry the point the mobile client's geocoding
picker returned, so the Discovery Map plots a place exactly instead of resolving its name
through a hand-maintained table of well-known cities (KNOT-ADR-034, KNOT-ADR-035).

The three columns are nullable and additive:

- `latitude` and `longitude` are **set together or not at all** — the service rejects a lone
  one. `NULL`/`NULL` is a story with no place, or one created before this migration.
- `place_country` is the country name the geocoder reported, or `NULL`.
- `approximate_location` (the free text field) is **kept**: a new story still stores the
  chosen place's name there for display, and `approximate_location_lower` still groups legacy
  rows. Discovery prefers the coordinate when it is present and falls back to the normalised
  name otherwise.

There is deliberately **no** spatial index, no PostGIS, and no reverse geocoding. Discovery
groups by the coordinate pair with a plain `GROUP BY`, which is enough at MVP scale.

### Deliberate omissions

- **No `updated_at` trigger.** Nothing edits a story yet; the column exists so the first
  editing task does not need a migration for it.
- **No feed filtering by author, language, or pillar.** The indexes exist so those
  filters can be added without a schema change, but no endpoint offers them yet.
- **No tags or reactions.** Each is a separate feature with its own migration and its own
  review.
- **No full-text search.** Search is a later problem, and the right index for it depends
  on how it is used.

## `story_versions`

A version is one telling of a story: the original, or a human adaptation of it into
another language. Every version belongs to exactly one story, and a story's versions form
its **Language Tree** — an adjacency list in which each version points at the version it
was adapted from. The tree is what Tell My People builds. See KNOT-ADR-011 and
KNOT-ADR-012.

| Column               | Type          | Nullable | Default             | Notes                                                                 |
| -------------------- | ------------- | -------- | ------------------- | --------------------------------------------------------------------- |
| `id`                 | `uuid`        | no       | `gen_random_uuid()` | Primary key                                                           |
| `story_id`           | `uuid`        | no       | —                   | References `stories(id)`; the story this version belongs to            |
| `parent_version_id`  | `uuid`        | yes      | `NULL`              | References `story_versions(id)`. The version this one adapts. `NULL` marks a story's root version |
| `author_id`          | `uuid`        | no       | —                   | References `users(id)`. The person who wrote this version; for a root version, the story's original author |
| `language`           | `text`        | no       | —                   | Three-letter ISO 639-3 code, lower case, from the canonical list (KNOT-ADR-046) |
| `title`              | `text`        | no       | —                   | 1-200 characters                                                      |
| `body`               | `text`        | no       | —                   | The version itself, 1-10000 characters, stored verbatim                |
| `adaptation_note`    | `text`        | yes      | `NULL`              | The adapter's optional note, up to 1000 characters                     |
| `created_at`         | `timestamptz` | no       | `now()`             |                                                                       |
| `updated_at`         | `timestamptz` | no       | `now()`             | Set on insert. No trigger yet: editing a version is a later task        |

### Indexes and constraints

| Name                                  | Kind                        | Columns                                   | Purpose                                                                 |
| ------------------------------------- | --------------------------- | ----------------------------------------- | ----------------------------------------------------------------------- |
| `story_versions_pkey`                 | Primary key                 | `id`                                      | Row identity                                                            |
| `story_versions_story_id_idx`         | Btree index                 | `story_id`                                | Reads every version of a story, and the cascade on story deletion        |
| `story_versions_parent_version_id_idx`| Btree index                 | `parent_version_id`                       | Finds a version's children, and the cascade on parent deletion           |
| `story_versions_author_id_idx`        | Btree index                 | `author_id`                               | "Versions by this person", and the cascade on user deletion              |
| `story_versions_story_id_root_unique` | Partial unique index (btree)| `(story_id)` where `parent_version_id IS NULL` | Exactly one root version per story — the invariant, enforced by the database |
| `story_versions_story_id_fkey`        | Foreign key                 | `story_id` → `stories(id)`                | `ON DELETE CASCADE`: deleting a story removes its versions               |
| `story_versions_parent_version_id_fkey` | Foreign key               | `parent_version_id` → `story_versions(id)`| `ON DELETE CASCADE`: deleting a version removes the adaptations of it     |
| `story_versions_author_id_fkey`       | Foreign key                 | `author_id` → `users(id)`                 | `ON DELETE CASCADE`: deleting an account removes its versions            |
| `story_versions_check`                | Check constraint            | `parent_version_id`                       | A version cannot be its own parent                                       |

### The adjacency list, and why

The tree stores only each version's immediate parent. Reading a story's whole tree is a
single `SELECT ... WHERE story_id = $1 ORDER BY created_at`, and the nesting is assembled
by the client. A materialized path or a closure table would answer deeper queries more
cheaply, but they cost maintenance on every write, and this tree is shallow — the floor is
a person retelling a story — and small. See KNOT-ADR-011.

### The root version invariant

Every story has exactly one root version: a `story_versions` row whose `parent_version_id`
is `NULL`, referenced by `stories.root_version_id`. Two database facts hold the invariant:

- `stories.root_version_id` is `NOT NULL` and unique, so a story cannot exist without a
  root version and no two stories share one.
- `story_versions_story_id_root_unique` is a partial unique index on `(story_id)` where
  `parent_version_id IS NULL`, so a story cannot gain a second root version.

Because a story row must name a root version and the root version must exist, the only way
to create a story is to create its root version at the same time. The stories store does
this in a single statement with two data-modifying CTEs: the story insert generates the
version id, the version insert uses it, and the mutually-referencing foreign keys are both
satisfied by the end of the statement. There is no window in which a story exists without
content.

### How the tree is queried

`GET /stories/{id}/tree` reads every version of one story ordered oldest first. Because a
parent is always created before its children, that order is also a valid depth-first
presentation, and the client indents each version by walking its `parent_version_id` chain.
No recursive query is used: the flat list is the wire format, and `parent_version_id` is
the whole of the structure.

## `comments`

A comment is one thing a person said about a story version. Comments are flat: there is no
`parent_comment_id`, and a reply in another language is a **bridge**, not a child comment.
See KNOT-ADR-014.

| Column       | Type          | Nullable | Default             | Notes                                              |
| ------------ | ------------- | -------- | ------------------- | -------------------------------------------------- |
| `id`         | `uuid`        | no       | `gen_random_uuid()` | Primary key                                        |
| `version_id` | `uuid`        | no       | —                   | References `story_versions(id)`; the version commented on |
| `author_id`  | `uuid`        | no       | —                   | References `users(id)`; the commenter               |
| `language`   | `text`        | no       | —                   | Three-letter ISO 639-3 code, lower case, from the canonical list (KNOT-ADR-046) |
| `body`       | `text`        | no       | —                   | 1-5000 characters, stored verbatim                  |
| `created_at` | `timestamptz` | no       | `now()`             | The thread's primary sort key                       |
| `updated_at` | `timestamptz` | no       | `now()`             | Set on insert. No trigger yet: editing a comment is a later task |

### Indexes and constraints

| Name                                 | Kind                     | Columns                                  | Purpose                                                     |
| ------------------------------------ | ------------------------ | ---------------------------------------- | ----------------------------------------------------------- |
| `comments_pkey`                      | Primary key              | `id`                                     | Row identity                                                 |
| `comments_version_id_created_at_idx` | Btree index (descending) | `(version_id, created_at DESC, id DESC)` | Serves the thread's `ORDER BY` and its keyset seek together  |
| `comments_author_id_idx`             | Btree index              | `author_id`                              | "Comments by this person", and the cascade on user deletion  |
| `comments_language_idx`              | Btree index              | `language`                               | Filtering by language                                        |
| `comments_version_id_fkey`           | Foreign key              | `version_id` → `story_versions(id)`      | `ON DELETE CASCADE`: deleting a version removes its comments  |
| `comments_author_id_fkey`            | Foreign key              | `author_id` → `users(id)`                | `ON DELETE CASCADE`: deleting an account removes its comments |

The composite index mirrors `stories_created_at_id_idx`: it is descending on both columns
and in the same order as the thread's `WHERE (created_at, id) < ($2, $3) ORDER BY
created_at DESC, id DESC` seek, so PostgreSQL seeks to the resume point instead of sorting
the table.

## `bridges`

A bridge connects a comment in one language to a comment in another. It is a first-class
object: it names a source comment and a target comment, and it is the record that one is
an adaptation of the other. Creating a bridge writes a new comment (the target) and the
bridge row in **one transaction**. The target comment belongs to the story's version
written in `target_language`, so a bridge joins two conversations rather than adding to
one; both comments remain ordinary members of their own conversation. See KNOT-ADR-014
and KNOT-ADR-015.

| Column              | Type          | Nullable | Default             | Notes                                                               |
| ------------------- | ------------- | -------- | ------------------- | ------------------------------------------------------------------- |
| `id`                | `uuid`        | no       | `gen_random_uuid()` | Primary key                                                          |
| `source_comment_id` | `uuid`        | no       | —                   | References `comments(id)`; the comment bridged from                  |
| `target_comment_id` | `uuid`        | no       | —                   | References `comments(id)`; the new comment in the target language     |
| `author_id`         | `uuid`        | no       | —                   | References `users(id)`; the bridger, and the target comment's author  |
| `target_language`   | `text`        | no       | —                   | Three-letter ISO 639-3 code, lower case, from the canonical list (KNOT-ADR-046) |
| `adaptation_note`   | `text`        | yes      | `NULL`              | The bridger's optional note, up to 1000 characters                    |
| `created_at`        | `timestamptz` | no       | `now()`             |                                                                      |

### Indexes and constraints

| Name                              | Kind             | Columns                                  | Purpose                                                              |
| --------------------------------- | ---------------- | ---------------------------------------- | -------------------------------------------------------------------- |
| `bridges_pkey`                    | Primary key      | `id`                                     | Row identity                                                         |
| `bridges_source_comment_id_idx`   | Btree index      | `source_comment_id`                      | Bridges out of a comment, and the cascade on comment deletion         |
| `bridges_target_comment_id_idx`   | Btree index      | `target_comment_id`                      | Bridges into a comment, and the cascade on comment deletion           |
| `bridges_author_id_idx`           | Btree index      | `author_id`                              | "Bridges by this person", and the cascade on user deletion            |
| `bridges_unique_pair`             | Unique index     | `(source_comment_id, target_comment_id)` | A source-target pair is bridged at most once                          |
| `bridges_one_per_target_language` | Unique index     | `(source_comment_id, target_language)`   | A comment is bridged into any one language at most once               |
| `bridges_source_comment_id_fkey`  | Foreign key      | `source_comment_id` → `comments(id)`     | `ON DELETE CASCADE`: deleting a comment removes bridges out of it     |
| `bridges_target_comment_id_fkey`  | Foreign key      | `target_comment_id` → `comments(id)`     | `ON DELETE CASCADE`: deleting a comment removes bridges into it       |
| `bridges_author_id_fkey`          | Foreign key      | `author_id` → `users(id)`                | `ON DELETE CASCADE`: deleting an account removes its bridges          |
| `bridges_check`                   | Check constraint | `source_comment_id`, `target_comment_id` | A comment cannot be bridged to itself                                 |

### Creating a bridge is one transaction

A bridge first resolves its target version: the source comment gives the story, and
`target_language` gives the version of that story written in the language (the oldest,
when several share it). The story must have such a version, or the request is rejected.
The target version's conversation is the one the target comment joins.

Unlike the story-plus-root-version write in migration `0003`, a bridge has no circular
foreign key: the target comment is inserted first, then the bridge that references it and
the source comment. Both rows are written in a single transaction, so a failure while
inserting the bridge rolls back the target comment it had already created. There is no
window in which a comment exists with no bridge to say where it came from.

`bridges_one_per_target_language` is a product policy rather than a structural necessity:
without it, the same comment could be bridged into French repeatedly, producing many
near-duplicate targets claiming to be the same adaptation. It is the default this task
chose (see KNOT-ADR-014) and can be relaxed later by dropping the one index.

## `rooted_signals`

A signal is a person's declared connection to a place: a place name at city or region
precision, and a duration bucket. It is Knot's trust primitive, and it is deliberately
honest (self-declared, never verified), privacy-conscious (place only — no coordinates,
no address), and simple (no vouching, no score, no gating). See KNOT-ADR-016.

| Column            | Type          | Nullable | Default             | Notes                                                               |
| ----------------- | ------------- | -------- | ------------------- | ------------------------------------------------------------------- |
| `id`              | `uuid`        | no       | `gen_random_uuid()` | Primary key                                                         |
| `user_id`         | `uuid`        | no       | —                   | References `users(id)`. The person the signal belongs to             |
| `place`           | `text`        | no       | —                   | A city or region name, 1-80 characters; never a precise location      |
| `latitude`        | `double precision` | yes | `NULL`            | Structured place coordinate, or `NULL`; set together with `longitude` (migration `0009`, KNOT-ADR-034) |
| `longitude`       | `double precision` | yes | `NULL`            | Structured place coordinate, or `NULL`                                |
| `place_country`   | `text`        | yes      | `NULL`              | The country the geocoder reported, or `NULL`; at most 100 characters   |
| `duration_bucket` | `text`        | no       | —                   | How long the connection has been held; see the closed set below      |
| `is_public`       | `boolean`     | no       | `true`              | `false` hides the signal from public read; the owner still sees it    |
| `is_primary`      | `boolean`     | no       | `true`              | Marks the one active signal; exists for later multi-signal support    |
| `created_at`      | `timestamptz` | no       | `now()`             |                                                                     |
| `updated_at`      | `timestamptz` | no       | `now()`             | Set on insert and on replacement. No trigger yet                      |

### Indexes and constraints

| Name                                   | Kind                         | Columns                               | Purpose                                                        |
| -------------------------------------- | ---------------------------- | ------------------------------------- | -------------------------------------------------------------- |
| `rooted_signals_pkey`                  | Primary key                  | `id`                                  | Row identity                                                    |
| `rooted_signals_user_id_idx`           | Btree index                  | `user_id`                             | Reads a user's signals, and the cascade on user deletion        |
| `rooted_signals_one_primary_per_user`  | Partial unique index (btree) | `(user_id)` where `is_primary = true` | At most one primary signal per user — the MVP invariant          |
| `rooted_signals_user_id_fkey`          | Foreign key                  | `user_id` → `users(id)`               | `ON DELETE CASCADE`: deleting an account removes its signals     |
| `rooted_signals_duration_bucket_check` | Check constraint             | `duration_bucket`                     | The bucket set is closed in the database, not only in Go         |

### The duration buckets

`duration_bucket` is one of `lifelong`, `many_years`, `several_years`, `a_few_years`, or
`recently`. The set is closed by both the CHECK constraint and the domain's
`DurationBucket.Valid`, so a value outside it is rejected before it reaches the database.

### One primary signal per user

`rooted_signals_one_primary_per_user` is a **partial** unique index on `(user_id)` where
`is_primary = true`. It enforces the MVP invariant that a person has exactly one active
signal: setting a second signal *replaces* the first rather than adding another. The store
does this in one statement — `INSERT ... ON CONFLICT (user_id) WHERE is_primary = true DO
UPDATE` — so a replacement cannot race itself into two primaries.

The index is partial, and `is_primary` exists at all, so a future multi-signal feature (a
person rooted in more than one place) can add non-primary rows without a schema change.
The MVP UI handles one signal.

### The visibility model

A signal is **public by default** (`is_public` defaults to `true`). Its owner hides it by
setting `is_public` to `false`. Two read rules follow from that:

- `GET /users/me/rooted` returns the owner's signals **including** hidden ones.
- `GET /users/{id}/rooted` returns **only** public signals.

The inline `author_rooted` enrichment on content responses is a summary, not a lookup: it
carries only `place` and `duration_bucket`, never the signal's id, its owner, or its
timestamps. See KNOT-ADR-017.

## `story_media`

A piece of media is one image or video attached to a story. The bytes live in the media
bucket; a row records where the object is, how to render it, and — the point of the
feature — whether it was captured with the device camera or chosen from the gallery. See
KNOT-ADR-030.

| Column          | Type          | Nullable | Default             | Notes                                                                 |
| --------------- | ------------- | -------- | ------------------- | --------------------------------------------------------------------- |
| `id`            | `uuid`        | no       | `gen_random_uuid()` | Primary key                                                           |
| `story_id`      | `uuid`        | no       | —                   | References `stories(id)`; the story the media belongs to               |
| `uploader_id`   | `uuid`        | no       | —                   | References `users(id)`; who uploaded it (the story's author in the MVP) |
| `storage_key`   | `text`        | no       | —                   | The object key in the media bucket, `story-media/{story_id}/{uuid}.{ext}`, **not** a public URL |
| `media_type`    | `text`        | no       | —                   | `CHECK (media_type IN ('image', 'video'))`                             |
| `mime_type`     | `text`        | no       | —                   | The sniffed content type (JPEG/PNG/WebP/MP4/MOV)                       |
| `source`        | `text`        | no       | —                   | `CHECK (source IN ('camera', 'gallery'))`                              |
| `width`         | `integer`     | yes      | `NULL`              | Pixel width when the client reported it                                |
| `height`        | `integer`     | yes      | `NULL`              | Pixel height when the client reported it                               |
| `duration_ms`   | `integer`     | yes      | `NULL`              | A video's duration in milliseconds when known                          |
| `size_bytes`    | `bigint`      | no       | —                   | The stored object's size                                               |
| `display_order` | `integer`     | no       | `0`                 | Position within the story; lower comes first. Appended on upload        |
| `created_at`    | `timestamptz` | no       | `now()`             |                                                                        |

### Indexes and constraints

| Name                             | Kind        | Columns                        | Purpose                                                        |
| -------------------------------- | ----------- | ------------------------------ | -------------------------------------------------------------- |
| `story_media_pkey`               | Primary key | `id`                           | Row identity                                                    |
| `story_media_story_id_order_idx` | Btree index | `(story_id, display_order)`    | Reads every item of a story in presentation order                |
| `story_media_uploader_id_idx`    | Btree index | `uploader_id`                  | "Media by this uploader", and the cascade on user deletion       |
| `story_media_story_id_fkey`      | Foreign key | `story_id` → `stories(id)`     | `ON DELETE CASCADE`: deleting a story removes its media          |
| `story_media_uploader_id_fkey`   | Foreign key | `uploader_id` → `users(id)`    | `ON DELETE CASCADE`: deleting an account removes its media       |
| `story_media_media_type_check`   | Check       | `media_type`                   | The media-type set is closed in the database, not only in Go      |
| `story_media_source_check`       | Check       | `source`                       | The source set (`camera`/`gallery`) is closed in the database     |

### `storage_key` holds a key, not a URL

Like `users.avatar_url`, `storage_key` is the **object key** inside the media bucket — for
example `story-media/d6b53a2c-…/0f8b1c2d-….jpg` — never a link to object storage. The
bucket stays private and every byte reaches a client through the backend (KNOT-ADR-029).
API responses translate the key into `/stories/{id}/media/{mid}/content`, which is the
only form a client ever sees; the key itself is never serialised.

The object name ends in a random UUID so uploading a new file never overwrites the bytes
an in-flight response is still serving, and the key cannot be derived from the public path.

### `source` is the feature

`source` is `camera` when the file was captured with the device camera and `gallery` when
it was chosen from the photo library. The mobile picker sets it; the server stores and
returns it; the UI draws a capture badge from it. It is the reason the table exists rather
than the older `stories.media_urls` array.

### Deliberate omissions

- **No thumbnails, transcoding, or moderation.** Each is a separate feature with its own
  task; the row stores the original as uploaded.
- **No hard cap on media count per story** in the database. The client aims for 10, and
  `display_order` keeps the order stable whatever the count.
- **No `updated_at`.** Media is immutable once uploaded; replacing it means deleting and
  re-uploading.

## `notifications`

A notification records that **another user acted on your content**: someone adapted a
version you wrote, commented on it, or bridged one of your comments into another language.
It is one row per event, addressed to one recipient, with the id of the thing the client
should open. See KNOT-ADR-038 and KNOT-ADR-039.

| Column        | Type          | Nullable | Default             | Notes                                                                  |
| ------------- | ------------- | -------- | ------------------- | ---------------------------------------------------------------------- |
| `id`          | `uuid`        | no       | `gen_random_uuid()` | Primary key                                                             |
| `user_id`     | `uuid`        | no       | —                   | References `users(id)`; the **recipient** — whose content was acted on   |
| `actor_id`    | `uuid`        | no       | —                   | References `users(id)`; the person who acted                            |
| `event_type`  | `text`        | no       | —                   | What happened; a closed set, see below                                   |
| `entity_type` | `text`        | no       | —                   | The kind of thing `entity_id` names; a closed set, see below              |
| `entity_id`   | `uuid`        | no       | —                   | The id of the thing to open. **No foreign key** — see below              |
| `read_at`     | `timestamptz` | yes      | `NULL`              | When the recipient read it; `NULL` means unread                          |
| `created_at`  | `timestamptz` | no       | `now()`             | The inbox's primary sort key                                            |

### Indexes and constraints

| Name                                | Kind                         | Columns                                          | Purpose                                                             |
| ----------------------------------- | ---------------------------- | ------------------------------------------------ | -------------------------------------------------------------------- |
| `notifications_pkey`                | Primary key                  | `id`                                              | Row identity                                                          |
| `notifications_user_created_idx`    | Btree index (descending)     | `(user_id, created_at DESC, id DESC)`             | Serves the inbox's `ORDER BY` and its keyset seek together            |
| `notifications_user_unread_idx`     | Partial index (btree)        | `(user_id)` where `read_at IS NULL`               | Makes the unread count a cheap index-only scan over a small set        |
| `notifications_user_id_fkey`        | Foreign key                  | `user_id` → `users(id)`                           | `ON DELETE CASCADE`: deleting an account removes its inbox             |
| `notifications_actor_id_fkey`       | Foreign key                  | `actor_id` → `users(id)`                           | `ON DELETE CASCADE`: deleting an actor removes what they caused        |
| `notifications_check`               | Check constraint             | `actor_id`, `user_id`                             | `actor_id <> user_id`: you are never notified about your own action    |
| `notifications_event_type_check`    | Check constraint             | `event_type`                                      | The event set is closed in the database, not only in Go                |
| `notifications_entity_type_check`   | Check constraint             | `entity_type`                                     | The entity set is closed in the database                               |

The composite index mirrors `comments_version_id_created_at_idx` and
`stories_created_at_id_idx`: it is descending on both columns and in the same order as the
inbox's `WHERE (created_at, id) < ($2, $3) ORDER BY created_at DESC, id DESC` seek, so
PostgreSQL seeks to the resume point instead of sorting the table.

### The event and entity sets are closed

`event_type` is one of `version.created`, `comment.created`, or `bridge.created`, and
`entity_type` is one of `story`, `version`, `comment`, or `bridge`. Both sets are CHECK
constraints *and* Go constants with a `Valid` method, so a value outside them is refused
before it reaches the database.

Each event maps to exactly one entity type (`version.created` → `version`,
`comment.created` → `comment`, `bridge.created` → `bridge`), and the service enforces the
mapping. A notification therefore always points at something a client can render, and
"an adaptation" can never be filed against a comment.

### `entity_id` has no foreign key

Unlike every other reference in this schema, `entity_id` is **not** a foreign key. It
points at one of three tables depending on `entity_type`, and PostgreSQL cannot express
"references one of several tables" without a discriminator trigger or a nullable column
per entity. A polymorphic pointer is the deliberate trade: the id is opaque to this table,
and the entity types it can name are closed by a CHECK constraint instead.

The consequence is that a notification can outlive the thing it points at when the entity
row is deleted without the notification being deleted with it — the actor or recipient
deletion cascades still remove the row. The client treats an unresolvable entity as "nothing
to open" rather than an error.

### `read_at` is nullable, and never moves

The inbox needs "unread" as a first-class fact, and a `NULL` marks it without a sentinel
timestamp or a second column. Marking a notification read is one `UPDATE ... SET read_at =
COALESCE(read_at, now())`, so a second tap on an already-read row is not an error and does
not change the timestamp it was read at.

### Deliberate omissions

- **No push delivery, no device tokens, and no delivery state.** Delivery is in-app only:
the row is the whole feature, and the client polls its own inbox (KNOT-ADR-039).
- **No `updated_at`.** A notification is a record of a past event; only `read_at` changes.
- **No grouping or de-duplication.** Ten adaptations are ten rows, because each points at a
different thing to open.

## The profile wall (a read model, no new table)

The profile wall (`GET /users/{id}/profile`) is **not backed by a table**. It is a read
model over the four entity tables that already exist — `stories`, `story_versions`,
`comments`, and `bridges` — unioned in one query, filtered by the author, and ordered
chronologically:

```sql
SELECT kind, id, created_at, payload
FROM (
    SELECT 'story'   AS kind, s.id, s.created_at,
           jsonb_build_object('title',        rv.title,
                              'pillar',       s.pillar,
                              'language',     rv.language)          AS payload
    FROM stories s
    JOIN story_versions rv ON rv.id = s.root_version_id
    WHERE s.author_id = $1

    UNION ALL
    SELECT 'version' AS kind, v.id, v.created_at,
           jsonb_build_object('story_id',     v.story_id,
                              'story_title',  rv.title,
                              'language',     v.language)
    FROM story_versions v
    JOIN stories s         ON s.id = v.story_id
    JOIN story_versions rv ON rv.id = s.root_version_id
    WHERE v.author_id = $1 AND v.parent_version_id IS NOT NULL

    UNION ALL
    SELECT 'comment' AS kind, c.id, c.created_at,
           jsonb_build_object('version_id',   c.version_id,
                              'story_id',     v.story_id,
                              'body_preview', left(c.body, 200))
    FROM comments c
    JOIN story_versions v ON v.id = c.version_id
    WHERE c.author_id = $1
      AND NOT EXISTS (SELECT 1 FROM bridges b WHERE b.target_comment_id = c.id)

    UNION ALL
    SELECT 'bridge'  AS kind, b.id, b.created_at,
           jsonb_build_object('source_comment_id', b.source_comment_id,
                              'version_id',        sc.version_id,
                              'target_language',   b.target_language)
    FROM bridges b
    JOIN comments sc ON sc.id = b.source_comment_id
    WHERE b.author_id = $1
) AS activities
WHERE (created_at, id) < ($2::timestamptz, $3::uuid)   -- when a cursor is sent
ORDER BY created_at DESC, id DESC
LIMIT $4
```

**Why a union and not a materialised `activities` table.** The four entities are already
the source of truth; a fifth table would have to be written on every create in four
domains, kept in sync on every delete, and backfilled. A read model with one query costs
one scan of each author-indexed table and stays correct by construction (KNOT-ADR-042).

**Why the payload is built in SQL.** `jsonb_build_object` attaches the context a card needs
— a story's title, an adaptation's story title, a comment's preview, a bridge's source —
to the row, so a whole page is one query rather than one per activity. The comment preview
is `left(body, 200)`, so a full body never travels to a client through the wall.

**Two acts are deliberately not double-counted.** A story's root version is excluded from
the `version` branch (the root *is* the story), and a bridge's target comment is excluded
from the `comment` branch (the target is the bridge's artifact). Both are single acts that
write two rows each, so listing both rows would list one action twice.

**Indexes it relies on.** `stories_author_id_idx`, `story_versions_author_id_idx`,
`comments_author_id_idx`, and `bridges_author_id_idx` narrow each branch to the author;
the `ORDER BY (created_at DESC, id DESC)` is a sort over the merged rows, which is bounded
by the four per-author filters. Each branch's primary-key lookup for the join is an index
scan.
