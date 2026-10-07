# Knot — Data Model

**Status: MVP, subject to change.** Only the identity table exists today.

Migrations live in `backend/go/migrations/` and are applied with
`go run ./cmd/knot migrate up`. See the "Migrations" section of
[`docs/DEVELOPMENT.md`](DEVELOPMENT.md).

## Migration history

| Version | Name    | What it creates                        |
| ------- | ------- | -------------------------------------- |
| `0001`  | `users` | The `users` table and its email index  |

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
- **No other tables.** Stories, adaptations, and conversations are Phase 1+ work and each
  gets its own migration and its own review.
