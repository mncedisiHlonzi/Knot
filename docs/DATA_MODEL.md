# Knot — Data Model

**Status: MVP, subject to change.** Eight tables exist today: `users`, `stories`,
`story_versions`, `comments`, `bridges` (**dormant** — see below), `rooted_signals`,
`story_media`, and `notifications`.

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
| `0012`  | `comment_replies` | The `comments.parent_comment_id` column, its index and foreign key, and the self-parent check constraint |
| `0013`  | `reactions` | The `reactions` table and its indexes; the `notifications.reaction_type` column; and the widened notification event CHECK |

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
Language Tree, Conversations) references `stories.id`.

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
| `created_at`           | `timestamptz`  | no       | `now()`             | The feed's primary sort key                                  |
| `updated_at`           | `timestamptz`  | no       | `now()`             | Set on insert. No trigger yet: editing a story is a later task |

The `sensitive` column was **dropped by migration `0018`** (KNOT-023c, KNOT-ADR-072).
The flag had no server-side effect beyond a client blur; the create path no longer accepts
it and the feed no longer reads it.

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
| `story_versions_created_at_id_idx`    | Partial Btree index (descending) | `(created_at DESC, id DESC)` where `parent_version_id IS NOT NULL` | Serves the feed's **version branch**: the adaptations ordered exactly as the merged page orders them, so the branch can be read as an ordered index scan (migration `0017`, KNOT-022) |
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

### Attribution is derived, not stored

Every version response carries an `attribution_line` and a `version_depth`, and **neither is
a column**. Both are resolved at read time by walking `parent_version_id` up to the root:
the chain's length gives the depth (0 for the root, 1 for a first adaptation, and so on) and
the chain's links — each one's author and language — give the sentence
(KNOT-ADR-069 in [`docs/DECISIONS.md`](DECISIONS.md)).

Storing a materialised sentence per row would be a second copy of the tree, and the two
would drift: re-parenting or reordering a branch would leave a row asserting a lineage that
no longer exists. Deriving it means the tree remains the only source of truth, at the cost
of resolving a chain per version returned.

That cost is why the resolution is **batched**. `GET /versions/{id}` walks one chain (a
handful of reads, guarded against a cycle in the parent pointers), while
`GET /stories/{id}/tree` loads the story's versions once and then resolves every version's
chain from that in-memory list, so a page of N versions issues no attribution query at all.
Author names are likewise resolved in one batched read per response rather than one per
link.

Attribution on the **feed** (`GET /stories`) is batched the same way, with one addition:
the page's version entries are attributed from a single **ancestry closure** read. One
recursive query walks `parent_version_id` from every requested version to its root and
returns the union of those rows, so a page of N adaptations costs one read rather than N
walks (migration `0017` adds the ordering index the feed's own query seeks by).

The version feed entry also carries the root author's structured identity —
`root_author_id`, `root_author_display_name`, and `root_author_avatar_url` — taken from the
**first link** of that same chain and the same batched author read. It is a projection, not
a column: no schema change, no migration, and no extra query. The fields exist so a card can
head an adaptation with "{name}'s original story" without parsing the attribution sentence
(KNOT-023a).

The **direct parent's** author rides alongside them as `parent_author_id`,
`parent_author_display_name`, and `parent_author_avatar_url`, taken from the
**second-to-last link** (the last link is the entry itself). For a first adaptation the
parent IS the root, so the two sets name the same person. These are projections too: the
chain and the page's single author read already hold them. A card draws the parent and the
root as its lineage avatar stack, keeping the poster — the entry's own `author_*` — in the
author line below (KNOT-022-fix2).

### The feed reads both tables

The feed is a **union of the two tables above**, not a query over `stories` alone:
`GET /stories` returns stories and human adaptations of them in one newest-first stream,
each entry tagged with a `kind` (`story` or `version`).

- The **story branch** reads `stories`, joined to its root version for the content, exactly
  as a story read always has. Its sort key is `stories.created_at`.
- The **version branch** reads `story_versions` rows whose `parent_version_id IS NOT NULL`,
  joined to their story for the pillar, place, and media the version row does not own. Its
  sort key is the **version's own** `story_versions.created_at`, so an adaptation appears
  where it was written rather than where its story was published.

**A root version is never an entry of its own.** A story IS its root version — the story
branch already carries that content — so the version branch excludes `parent_version_id IS
NULL` rows. Without that predicate every story would appear twice, once as itself and once
as its own "adaptation".

The two branches select the same columns in the same order so the union can be ordered and
scanned as one stream; a column that does not apply to a branch (a version's parent on a
story entry, a story's root version on a version entry) is selected as an empty value
rather than omitted. Paging is keyset over the merged stream, so the cursor is a position
in `(created_at, id)` over both kinds and a page never repeats or skips an entry.

The block filter is applied **per branch, on its own author**: a viewer who blocks A stops
seeing A's story and stops seeing any version A wrote, while a version B wrote of A's story
stays visible. A block hides an author, not a lineage (KNOT-ADR-060).

This is also why the `media` and place fields on a version entry are the story's: a version
row has no media, pillar, or place columns to read, so the branch joins the story for them.

## `comments`

A comment is one thing a person said about a story version. A comment can answer another
comment on the same version, one level deep: `parent_comment_id` names a **top-level**
comment, and a reply to a reply is stored against that reply's top-level comment. A thread
is therefore always a top-level comment followed by its replies. See KNOT-ADR-047.

| Column              | Type          | Nullable | Default             | Notes                                              |
| ------------------- | ------------- | -------- | ------------------- | -------------------------------------------------- |
| `id`                | `uuid`        | no       | `gen_random_uuid()` | Primary key                                        |
| `version_id`        | `uuid`        | no       | —                   | References `story_versions(id)`; the version commented on |
| `author_id`         | `uuid`        | no       | —                   | References `users(id)`; the commenter               |
| `language`          | `text`        | no       | —                   | Three-letter ISO 639-3 code, lower case, from the canonical list (KNOT-ADR-046) |
| `body`              | `text`        | no       | —                   | 1-5000 characters, stored verbatim                  |
| `parent_comment_id` | `uuid`        | yes      | `NULL`              | References `comments(id)`; the top-level comment replied to, or `NULL` for a top-level comment |
| `created_at`        | `timestamptz` | no       | `now()`             | The thread's primary sort key                       |
| `updated_at`        | `timestamptz` | no       | `now()`             | Set on insert. No trigger yet: editing a comment is a later task |

### Indexes and constraints

| Name                                 | Kind                     | Columns                                  | Purpose                                                     |
| ------------------------------------ | ------------------------ | ---------------------------------------- | ----------------------------------------------------------- |
| `comments_pkey`                      | Primary key              | `id`                                     | Row identity                                                 |
| `comments_version_id_created_at_idx` | Btree index (descending) | `(version_id, created_at DESC, id DESC)` | Serves the thread's `ORDER BY` and its keyset seek together  |
| `comments_author_id_idx`             | Btree index              | `author_id`                              | "Comments by this person", and the cascade on user deletion  |
| `comments_language_idx`              | Btree index              | `language`                               | Filtering by language                                        |
| `comments_parent_comment_id_idx`     | Btree index              | `parent_comment_id`                      | The batched reply lookup for a whole page of parents, and the cascade below |
| `comments_version_id_fkey`           | Foreign key              | `version_id` → `story_versions(id)`      | `ON DELETE CASCADE`: deleting a version removes its comments  |
| `comments_author_id_fkey`            | Foreign key              | `author_id` → `users(id)`                | `ON DELETE CASCADE`: deleting an account removes its comments |
| `comments_parent_comment_id_fkey`    | Foreign key              | `parent_comment_id` → `comments(id)`     | `ON DELETE CASCADE`: deleting a top-level comment removes its replies |
| `comments_parent_comment_id_not_self` | Check constraint        | `parent_comment_id IS NULL OR parent_comment_id <> id` | A comment can never reply to itself |

The composite index mirrors `stories_created_at_id_idx`: it is descending on both columns
and in the same order as the thread's `WHERE (created_at, id) < ($2, $3) ORDER BY
created_at DESC, id DESC` seek, so PostgreSQL seeks to the resume point instead of sorting
the table.

The thread's page query adds `AND parent_comment_id IS NULL`, so `limit` counts top-level
comments and the keyset advance is never disturbed by a reply. The page's replies are then
fetched in one statement, `WHERE parent_comment_id = ANY($1::uuid[]) ORDER BY created_at
ASC, id ASC`, which `comments_parent_comment_id_idx` serves. Replies are ordered oldest
first within their parent, so a thread's replies read in the order they were written even
though the top-level comments are newest first.

The depth limit is enforced when the comment is written, not by a constraint: there is no
expression index or trigger that could express "the parent has no parent" without a
lookup. The service resolves the parent, refuses one from another version, and rewrites a
reply's parent to that parent's own parent. The self-parent check is the one part the
schema can state, and it does — an `UPDATE` cannot slip past it.

## `bridges` — **dormant**

> **Dormant since KNOT-023.** The `bridges` table is present and preserved: it was not
dropped and no rows were deleted, because comment bridging was retired as a first-class
mechanic after real data existed (KNOT-ADR-071). **No active code path reads or writes it.**
The API routes are gone, the service and store methods are deleted, the notification event
is retired, and the profile wall no longer unions this table. A future cleanup task will
drop the table after a cooling-off period.
>
> Leaving it dormant is safe: both foreign keys are `ON DELETE CASCADE` **into** `bridges`,
> so deleting a user or a comment still cascades to its bridge rows. No constraint or
trigger on any live table depends on `bridges`, so nothing a live write does can fail
> because the table exists.

The table records a historical bridge: it names a source comment and a target comment, and
it was the record that one was an adaptation of the other (KNOT-ADR-014, KNOT-ADR-015). The
schema below is the historical shape, kept for reference until the drop.

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

### Historical write path (retired)

A bridge resolved its target version from the source comment's story and
`target_language` (the oldest version when several shared it), then inserted the target
comment and the bridge row in one transaction. That path was removed in KNOT-023; the
columns and constraints above remain only because the rows do.

`bridges_one_per_target_language` was a product policy rather than a structural necessity:
without it, the same comment could be bridged into French repeatedly, producing many
near-duplicate targets claiming to be the same adaptation.

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
- **No hard cap on media count per story in the database.** A story holds at most **10** items,
  images and videos together, but the cap is enforced in the `POST /stories/{id}/media` insert
  transaction — which locks the `stories` row and counts `story_media` before it writes — rather
  than by a constraint, because a CHECK cannot count rows (KNOT-ADR-054). `display_order` keeps
  the order stable whatever the count.
- **No `updated_at`.** Media is immutable once uploaded; replacing it means deleting and
  re-uploading.

## `notifications`

A notification records that **another user acted on your content**: someone adapted a
version you wrote, commented on it, replied to a comment you wrote, reacted to it, or
asked about the place you are Rooted in. It is one row per event, addressed to one
recipient, with the id of the thing the client should open. See KNOT-ADR-038,
KNOT-ADR-039, and KNOT-ADR-047.

A `comment.created` row's recipient depends on what was written: for a top-level comment it
is the **version's author**, and for a reply it is the **author of the comment that was
replied to** — including when a reply answers a reply, where the recipient is the author of
the reply that was tapped rather than the top-level comment's author. Only one notification
is written per comment, so neither case can notify twice.

| Column        | Type          | Nullable | Default             | Notes                                                                  |
| ------------- | ------------- | -------- | ------------------- | ---------------------------------------------------------------------- |
| `id`          | `uuid`        | no       | `gen_random_uuid()` | Primary key                                                             |
| `user_id`     | `uuid`        | no       | —                   | References `users(id)`; the **recipient** — whose content was acted on   |
| `actor_id`    | `uuid`        | no       | —                   | References `users(id)`; the person who acted                            |
| `event_type`  | `text`        | no       | —                   | What happened; a closed set, see below                                   |
| `entity_type` | `text`        | no       | —                   | The kind of thing `entity_id` names; a closed set, see below              |
| `entity_id`   | `uuid`        | no       | —                   | The id of the thing to open. **No foreign key** — see below              |
| `reaction_type` | `text`      | yes      | `NULL`              | Which perspective signal a `reaction.created` names; `NULL` for every other event (KNOT-ADR-051) |
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
| `notifications_reaction_type_check` | Check constraint             | `reaction_type`                                   | A `reaction.created` signal is inside the closed set, or the column is `NULL` (KNOT-ADR-050) |

The composite index mirrors `comments_version_id_created_at_idx` and
`stories_created_at_id_idx`: it is descending on both columns and in the same order as the
inbox's `WHERE (created_at, id) < ($2, $3) ORDER BY created_at DESC, id DESC` seek, so
PostgreSQL seeks to the resume point instead of sorting the table.

### The event and entity sets are closed

`event_type` is one of `version.created`, `comment.created`,
`reaction.created`, `inquiry.answered`, or `inquiry.nearby`; `entity_type` is one of `story`,
`version`, `comment`, or `inquiry`. Both sets are CHECK constraints *and* Go constants
with a `Valid` method, so a value outside them is refused before it reaches the database. The
constraints are widened in the same migration that starts writing a new event — a migration that
adds an event without widening the CHECK would fail at the first write (KNOT-ADR-050,
KNOT-ADR-056).

**Retired event.** The `event_type` CHECK still permits `bridge.created` and
`entity_type` still permits `bridge`, because the constraint was never narrowed and
historical rows use both. No active code path dispatches that event any more, and every
inbox read excludes it at the store query level, so a pre-KNOT-023 row is preserved but
never shown (KNOT-ADR-071).

Each event maps to exactly one entity type — `version.created` → `version`,
`comment.created` → `comment`, and both inquiry events → `inquiry`
— and the service enforces the mapping. A notification therefore always points at something a
client can render, and "an adaptation" can never be filed against a comment.

The one exception is `reaction.created`, whose entity is whatever was reacted to: it accepts any
of the entity kinds rather than one fixed type, because a reaction reuses the entity it points
at so the client opens the same screen a tap on the content would.

Both inquiry events name the **inquiry** rather than the answer, so a tap opens the question,
where the new answer is visible in its thread. That keeps one destination for both events and
avoids an endpoint whose only purpose would be to resolve an answer id.

`inquiry.nearby` is the only event a recipient can receive about content they have no part in:
it fires because of where they are from — the recipient is Rooted in the place the question
names — rather than because they authored something.

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

## `reactions`

One user's one **perspective signal** on one piece of content (KNOT-ADR-050). It is not a
like: the four signals do not compete, a user may hold any combination of them on the same
entity, and nothing orders content by them.

| Column          | Type          | Nullable | Default             | Notes                                                          |
| --------------- | ------------- | -------- | ------------------- | -------------------------------------------------------------- |
| `id`            | `uuid`        | no       | `gen_random_uuid()` | Primary key                                                     |
| `user_id`       | `uuid`        | no       | —                   | References `users(id)`; the person who reacted                    |
| `entity_type`   | `text`        | no       | —                   | One of `story`, `version`, `comment`, `inquiry`; a closed set      |
| `entity_id`     | `uuid`        | no       | —                   | The reacted-to row. **No foreign key** — see below                |
| `reaction_type` | `text`        | no       | —                   | One of the four signals; a closed set                             |
| `created_at`    | `timestamptz` | no       | `now()`             | Insertion time                                                    |

### Indexes and constraints

| Name                                    | Kind             | Columns                                                       | Purpose                                                        |
| --------------------------------------- | ---------------- | ------------------------------------------------------------- | -------------------------------------------------------------- |
| `reactions_pkey`                        | Primary key      | `id`                                                          | Row identity                                                     |
| `reactions_unique_per_user_entity_type` | Unique index     | `(user_id, entity_type, entity_id, reaction_type)`             | One user holds each signal on an entity at most once; also the toggle's `ON CONFLICT` target |
| `reactions_entity_idx`                  | Btree index      | `(entity_type, entity_id)`                                     | Serves the count aggregation for one entity and the batched `= ANY($2)` for a page |
| `reactions_user_idx`                    | Btree index      | `(user_id)`                                                    | Serves the `my_reactions` enrichment                             |
| `reactions_user_id_fkey`                | Foreign key      | `user_id` → `users(id)`                                        | `ON DELETE CASCADE`: deleting an account removes its reactions   |
| `reactions_entity_type_check`           | Check constraint | `entity_type`                                                  | The entity set is closed in the database                         |
| `reactions_reaction_type_check`         | Check constraint | `reaction_type`                                                | The four-signal set is closed in the database                    |

### `entity_id` is polymorphic and has no foreign key

A reaction points at one of four tables, which a single column cannot reference. The
`entity_type` CHECK closes the set of kinds, and the **application** enforces referential
integrity: every toggle route resolves its target through that domain's own service before it
writes. The cost is that a deleted entity leaves its reactions behind — nothing deletes
content today, so a sweep is a future task rather than a live defect (KNOT-ADR-050).

### Toggling is one transaction, and the unique index is the switch

The insert relies on `reactions_unique_per_user_entity_type` as its `ON CONFLICT` target: a row
it does not create already exists, so the same call deletes it. Both writes run in one
transaction, so the entity is never observed with the signal half-applied.

## `inquiries`

A question someone asked about a place (KNOT-016).

| Column | Type | Notes |
| --- | --- | --- |
| `id` | `uuid` | PK, `gen_random_uuid()` |
| `author_id` | `uuid` | NOT NULL, FK `users(id)` `ON DELETE CASCADE` |
| `title` | `text` | NOT NULL, 1–200 characters after trimming |
| `body` | `text` | NOT NULL, 1–5,000 characters after trimming |
| `language` | `text` | NOT NULL, a valid ISO 639-3 code (KNOT-ADR-046) |
| `place` | `text` | NULL, 1–100 characters after trimming, one line, no tabs |
| `place_country` | `text` | NULL, ≤100 characters |
| `latitude` | `double precision` | NULL |
| `longitude` | `double precision` | NULL |
| `answer_count` | `integer` | NOT NULL DEFAULT 0, maintained by the application |
| `created_at` | `timestamptz` | NOT NULL DEFAULT `now()`, the list's sort key |
| `updated_at` | `timestamptz` | NOT NULL DEFAULT `now()` |

### Indexes and constraints

| Index | Serves |
| --- | --- |
| `inquiries_created_at_id_idx (created_at DESC, id DESC)` | `GET /inquiries`, the keyset order |
| `inquiries_place_idx (place)` | the `?place=` filter, and Rooted routing |
| `inquiries_author_id_idx (author_id)` | a user's own inquiries, and the wall's inquiry branch |

There is **no** CHECK on `language`: the canonical ISO 639-3 list is compile-time data the
application owns, and the notifications and reactions migrations take the same position. There
is no CHECK on the coordinate pair either — the pair is enforced by the service, exactly as it
is on `stories` and `rooted_signals` (KNOT-ADR-034).

### `answer_count` is denormalised, and maintained in the insert's transaction

The count is stored on the inquiry so the list and the detail can show "3 answers" without a
correlated `COUNT(*)` per row. It is not maintained by a trigger: the service inserts the
answer, increments the count, and commits both in one transaction, having first locked the
inquiry row with `SELECT … FOR UPDATE`. Two answers arriving at once serialise on that lock, so
neither can observe the other's pre-increment value and write it back (the KNOT-ADR-054
pattern). The lock is on `inquiries.id` — one stable row, and the entity whose count is being
changed — not on `inquiry_answers` rows.

`updated_at` is deliberately **not** touched by an answer. An answer is new activity on the
inquiry rather than an edit of it, and the wall orders by `created_at`, so bumping `updated_at`
would misreport an edit that never happened.

## `inquiry_answers`

A public, attributed reply to one inquiry (KNOT-016).

| Column | Type | Notes |
| --- | --- | --- |
| `id` | `uuid` | PK, `gen_random_uuid()` |
| `inquiry_id` | `uuid` | NOT NULL, FK `inquiries(id)` `ON DELETE CASCADE` |
| `author_id` | `uuid` | NOT NULL, FK `users(id)` `ON DELETE CASCADE` |
| `language` | `text` | NOT NULL, a valid ISO 639-3 code |
| `body` | `text` | NOT NULL, 1–5,000 characters after trimming |
| `created_at` | `timestamptz` | NOT NULL DEFAULT `now()`, the thread's sort key |
| `updated_at` | `timestamptz` | NOT NULL DEFAULT `now()` |

### Indexes and constraints

| Index | Serves |
| --- | --- |
| `inquiry_answers_inquiry_created_idx (inquiry_id, created_at ASC, id ASC)` | `GET /inquiries/{id}/answers`, oldest first |
| `inquiry_answers_author_id_idx (author_id)` | a user's own answers, and the wall's `inquiry_answer` branch |

The index is ascending because an answer thread reads as a conversation: the earliest answer
leads, the opposite of the inquiry list. `ON DELETE CASCADE` on `inquiry_id` means removing a
question takes its answers with it — there is no delete route at MVP, but the schema is honest
when a row is removed by hand.

### Operations that deliberately do not exist

No accepted answer, no closing, no ranking, no editing, no deletion, no anonymity, and no
follow/notify-me. Each is a product decision, not an omission: an inquiry stays open forever
because a place's knowledge is plural (KNOT-ADR-055).

## The profile wall (a read model, no new table)

The profile wall (`GET /users/{id}/profile`) is **not backed by a table**. It is a read
model over the entity tables that already exist — `stories`, `story_versions`,
`comments`, `inquiries`, and `inquiry_answers` — unioned in one query, filtered by
the author, and ordered chronologically:

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

    UNION ALL
    SELECT 'inquiry' AS kind, i.id, i.created_at,
           jsonb_build_object('title', i.title,
                              'place', i.place)
    FROM inquiries i
    WHERE i.author_id = $1

    UNION ALL
    SELECT 'inquiry_answer' AS kind, a.id, a.created_at,
           jsonb_build_object('inquiry_id',    a.inquiry_id,
                              'inquiry_title', i.title,
                              'body_preview',  left(a.body, 200))
    FROM inquiry_answers a
    JOIN inquiries i ON i.id = a.inquiry_id
    WHERE a.author_id = $1
) AS activities
WHERE (created_at, id) < ($2::timestamptz, $3::uuid)   -- when a cursor is sent
ORDER BY created_at DESC, id DESC
LIMIT $4
```

**Why a union and not a materialised `activities` table.** The entities are already
the source of truth; an extra table would have to be written on every create in every
domain, kept in sync on every delete, and backfilled. A read model with one query costs
one scan of each author-indexed table and stays correct by construction (KNOT-ADR-042).

**Why the payload is built in SQL.** `jsonb_build_object` attaches the context a card needs
— a story's title, an adaptation's story title, a comment's preview, an inquiry's place, an
answer's question — to the row, so a whole page is one query rather than
one per activity. The comment and answer previews are `left(body, 200)`, so a full body never
travels to a client through the wall.

**One act is deliberately not double-counted.** A story's root version is excluded from
the `version` branch (the root *is* the story). The inquiry branches
need no such exclusion: asking a question and answering one are different acts by different
people, and an answer always belongs to someone else's inquiry.

**The five wall kinds.** `story`, `version`, `comment`, `inquiry`, and
`inquiry_answer`, with payloads `{title, pillar, language}`, `{story_id, story_title,
language}`, `{version_id, story_id, body_preview}`, `{title, place}`, and `{inquiry_id,
inquiry_title, body_preview}`. Every payload field carries `omitempty`, so a place-less inquiry
has no `place` key at all rather than an explicit null.

**Indexes it relies on.** `stories_author_id_idx`, `story_versions_author_id_idx`,
`comments_author_id_idx`, `inquiries_author_id_idx`, and
`inquiry_answers_author_id_idx` narrow each branch to the author; the
`ORDER BY (created_at DESC, id DESC)` is a sort over the merged rows, which is bounded by the
per-author filters. Each branch's primary-key lookup for the join is an index scan.

## Moderation (migration 0015, KNOT-017a)

The safety foundation: who may moderate, who has blocked whom, and what has been
reported. Only `blocks`, `reports`, `moderation_cases`, and `audit_log` are used in
KNOT-017a; `moderation_actions` is created here so KNOT-017b needs no new
migration.

- **`users.role`** — `TEXT NOT NULL DEFAULT 'user'`, `CHECK (role IN ('user',
  'moderator', 'admin'))`. Assigned manually at MVP; there is no API to change it.
- **`blocks`** — one **directed** block per row (`blocker_id`, `blocked_id`), both
  `REFERENCES users(id) ON DELETE CASCADE`, with `CHECK (blocker_id <> blocked_id)`.
  `blocks_unique_pair (blocker_id, blocked_id)` makes a block idempotent;
  `blocks_blocked_id_idx` makes "who blocked me" cheap. Every read treats the pair
  as mutual, so both directions are consulted (KNOT-ADR-060).
- **`reports`** — one user's report of one entity: `reporter_id`, `entity_type`
  (six kinds), `entity_id` (no foreign key: the six kinds live in six tables),
  `category`, nullable `reason`, `created_at`.
  `reports_one_per_user_per_entity` is unique, so a repeat is a 409;
  `reports_entity_idx (entity_type, entity_id)` groups an entity's reports.
- **`moderation_cases`** — one per entity (`moderation_cases_entity_unique`),
  `status` in `open | actioned | dismissed`, and a denormalised `report_count`
  incremented in the same transaction that inserts a report (the KNOT-ADR-054
  pattern). `moderation_cases_status_updated_idx (status, updated_at DESC)` serves
  the 017b queue.
- **`moderation_actions`** — a moderator's action on a case (017b).
- **`audit_log`** — append-only; one row per block/report event (and, later, per
  moderator action). `metadata JSONB NOT NULL DEFAULT '{}'`.

Every statement is idempotent (`IF NOT EXISTS` / `pg_constraint` guards), so
re-applying `0015_moderation.up.sql` by hand is safe.

## Search (migration 0016, KNOT-018)

Full-text search adds **one generated column and one GIN index to each of two tables** — two, not
three, and not to `stories`.

| Table | Column | Index |
| --- | --- | --- |
| `story_versions` | `search_vector tsvector` | `story_versions_search_vector_idx` (GIN) |
| `inquiries` | `search_vector tsvector` | `inquiries_search_vector_idx` (GIN) |

### Why `stories` has no `search_vector`

A story row carries no title and no body: its content *is* its root version, joined through
`stories.root_version_id` (which is `NOT NULL` and `UNIQUE`, so the join is always one row).
Copying that text onto `stories` would duplicate it, need its own maintenance, and let it drift
from the version it is derived from. A **story** search result therefore comes from
`story_versions WHERE parent_version_id IS NULL` joined to `stories`, and it carries the story id;
a **version** result comes from the non-root rows and carries its own id plus its story's id.

### The column is generated, never written

```sql
search_vector tsvector GENERATED ALWAYS AS (
  setweight(to_tsvector('simple', coalesce(title, '')), 'A') ||
  setweight(to_tsvector('simple', coalesce(body, '')), 'B')
) STORED
```

PostgreSQL maintains it on every insert and update, so it cannot drift from `title` and `body`
and no application code ever writes it. The two-argument `to_tsvector(regconfig, text)` form is
used because only it is immutable, which a generated column requires. The weights are `A` for the
title and `B` for the body, so a title hit outranks a body hit without a second index.

### `simple`, not a language configuration

The `simple` configuration lower-cases and tokenises but does **not** stem. Knot is multilingual:
English stemming would corrupt isiZulu or Sepedi lexemes, and there is no single stemmer for the
languages Knot serves (KNOT-ADR-065). The cost is that "running" does not match "run"; the
query layer's prefix matching recovers most of the practical loss, since "runn" does.

`pg_trgm` is deliberately **not** used. Prefix matching (`mang:*`) covers partial-word search
without a second extension, a second index, or a second query path.

### Idempotency

`ALTER TABLE ... ADD COLUMN` has no `IF NOT EXISTS`, so each column is added behind an
`information_schema` guard and each index uses `CREATE INDEX IF NOT EXISTS`. Applying
`0016_search.up.sql` twice is therefore a no-op, and the `down` file drops the indexes and columns
behind matching guards. Verified in both directions against the local database.

## `user_interactions` (migration `0019`)

The engagement log behind the relevance engine: one row per significant action a user
takes on a story or a version.

| Column               | Type        | Notes |
| -------------------- | ----------- | ----- |
| `id`                 | `UUID`      | Primary key, `gen_random_uuid()`. |
| `user_id`            | `UUID`      | `REFERENCES users(id) ON DELETE CASCADE`. |
| `entity_type`        | `TEXT`      | Closed set: `story`, `version`. |
| `entity_id`          | `UUID`      | No foreign key: one column cannot reference two tables. |
| `interaction_type`   | `TEXT`      | Closed set: `view`, `read_full`, `reaction`, `comment`, `adapt`. |
| `interaction_weight` | `REAL`      | The weight applied at write time (1 / 3 / 5 / 7 / 10). |
| `created_at`         | `TIMESTAMPTZ` | Defaults to `now()`. |

**Indexes and constraints.** `user_interactions_user_created_idx` on
`(user_id, created_at DESC)` serves "this reader's engagement, newest first";
`user_interactions_entity_idx` on `(entity_type, entity_id)` serves the future
entity-side reads. Both closed sets and `interaction_weight > 0` are CHECK constraints, so
an unknown value cannot be written even by a mistake in Go.

**The weight is data, not a formula.** It is resolved in `internal/interactions` from
exported constants and stored on the row, so re-tuning the ladder later does not rewrite
history. A comment's or an adaptation's engagement is recorded against the version it was
acted on, which is the content that engaged the reader.

## `entity_features` (migration `0020`)

The ML-ready read model: one denormalised row per entity, so a ranking pass can join a
candidate's language, pillar and place without touching the content tables.

| Column        | Type          | Notes |
| ------------- | ------------- | ----- |
| `entity_id`   | `UUID`        | Primary key; a story or a version id. |
| `entity_type` | `TEXT`        | Closed set: `story`, `version`. |
| `language`    | `TEXT NULL`   | A story's root-version language, or the version's own. |
| `pillar`      | `TEXT NULL`   | The story's pillar. |
| `place_lower` | `TEXT NULL`   | `lower(stories.approximate_location)`. |
| `updated_at`  | `TIMESTAMPTZ` | Defaults to `now()`; refreshed by a rebuild. |

**Derived, never authoritative.** The migration backfills every existing story and version
(39 rows on the founder's local database: 15 stories and 24 versions), and
`stories.PostgresStore.BackfillEntityFeatures` rebuilds the table with an
`ON CONFLICT (entity_id) DO UPDATE`. A future task will maintain it on write; until then a
newly created story gets its row on the next rebuild.

**Why the columns are copied.** A story's language lives on its root version while its
pillar and place live on the story; a version's language is its own while its pillar and
place belong to the story. The read model collapses that join once instead of on every
ranking read (KNOT-026, KNOT-ADR-075).

