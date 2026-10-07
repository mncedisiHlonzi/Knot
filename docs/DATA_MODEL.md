# Knot — Data Model

**Status: MVP, subject to change.** Five tables exist today: `users`, `stories`,
`story_versions`, `comments`, and `bridges`.

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

Applied versions are recorded in the `schema_migrations` table, which the runner creates
on first use.

## `users`

| Column                 | Type                     | Nullable | Default            | Notes                                            |
| ---------------------- | ------------------------ | -------- | ------------------ | ------------------------------------------------ |
| `id`                   | `uuid`                   | no       | `gen_random_uuid()` | Primary key. `gen_random_uuid()` is built into PostgreSQL 13+; Knot targets 16 |
| `email`                | `text`                   | no       | —                  | Unique. Stored lower-cased by the application and matched case-insensitively |
| `phone`                | `text`                   | yes      | `NULL`             | Optional contact number                          |
| `password_hash`        | `text`                   | no       | —                  | argon2id PHC string. Never returned by the API   |
| `display_name`         | `text`                   | no       | —                  | 1-80 characters                                  |
| `preferred_languages`  | `text[]`                 | no       | `'{}'`             | Language codes/tags. The application always writes an array, never `NULL` |
| `approximate_location` | `text`                   | yes      | `NULL`             | Coarse location only; precise location is out of scope |
| `created_at`           | `timestamptz`            | no       | `now()`            |                                                  |
| `updated_at`           | `timestamptz`            | no       | `now()`            | Set on insert. No trigger yet: updates are a later task |

### Indexes and constraints

| Name              | Kind                       | Columns | Purpose                                   |
| ----------------- | -------------------------- | ------- | ----------------------------------------- |
| `users_pkey`      | Primary key                | `id`    | Row identity                              |
| `users_email_key` | Unique constraint (btree)  | `email` | Prevents duplicate accounts; named so operational tooling can refer to it |

The uniqueness rule is enforced in the database, not only in application code: a
duplicate insert surfaces to the service as `identity.ErrEmailTaken`, which the API
reports as `409 email_taken`.

### Deliberate omissions

- **No `updated_at` trigger.** Nothing updates a user yet; the column exists so the first
  profile-editing task does not need a migration for it.
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
| `language`           | `text`        | no       | —                   | 2-8 letters, stored lower-cased                                       |
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
| `language`   | `text`        | no       | —                   | 2-8 letters, stored lower-cased                     |
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
| `target_language`   | `text`        | no       | —                   | 2-8 letters, stored lower-cased                                      |
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
