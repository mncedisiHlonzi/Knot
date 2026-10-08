# Knot — API

**Status: MVP, subject to change.** These endpoints exist to get the first people onto
Knot. Shapes, codes, and paths may change without notice while the product is in
foundation and early Phase 1.

- Base URL: `http://localhost:8080` (see `KNOT_HTTP_PORT` and `KNOT_API_URL`).
- All request and response bodies are JSON, sent with `Content-Type: application/json`.
- Request bodies are limited to **1 MiB**. Unknown fields are **rejected**.
- Every request is logged with a request id. Send `X-Request-ID` to correlate your own
  logs; the server echoes it back, and generates one when it is absent.

## Errors

Every error uses the same envelope:

```json
{ "error": { "code": "invalid_credentials", "message": "invalid credentials" } }
```

`code` is stable and safe to branch on. `message` is human-readable and may change.

| Status | `code`              | Meaning                                              |
| ------ | ------------------- | ---------------------------------------------------- |
| 400    | `validation_error`  | A field failed validation; `message` names the field  |
| 400    | `invalid_request`   | Body is not a single valid JSON object, or has unknown fields |
| 401    | `invalid_credentials` | Login failed. Deliberately identical for a wrong password and an unknown email |
| 401    | `unauthorized`      | A protected route was called without a valid access token. Identical for a missing, malformed, expired, or wrong-type token |
| 404    | `not_found`         | The requested resource (story, version, comment, bridge, or user) does not exist, or its id is not a UUID |
| 405    | *(empty body)*      | Method not allowed for that path                     |
| 409    | `email_taken`       | That email is already registered                     |
| 413    | `request_too_large` | Body exceeded 1 MiB                                  |
| 500    | `internal_error`    | Something failed server-side. No internal detail is returned |

## GET /health

Liveness only. It touches no dependency, so it answers "is this process serving HTTP",
not "is the database healthy".

**200**

```json
{ "status": "ok", "version": "0.1.0", "time": "2026-10-07T16:26:36Z" }
```

`time` is RFC3339 in UTC.

## POST /auth/register

Creates an account and returns a token pair.

**Request**

```json
{
  "email": "ada@example.com",
  "password": "at-least-8-characters",
  "display_name": "Ada Lovelace",
  "preferred_languages": ["en", "fr"],
  "approximate_location": "Cape Town",
  "phone": "+27000000000"
}
```

| Field                  | Required | Rules                                                          |
| ---------------------- | -------- | -------------------------------------------------------------- |
| `email`                | yes      | Valid address, at most 254 characters, stored lower-cased        |
| `password`             | yes      | At least 8 characters, at most 1024 bytes                        |
| `display_name`         | yes      | 1-80 characters after trimming                                   |
| `preferred_languages`  | no       | At most 20 entries, each at most 35 characters; blanks dropped   |
| `approximate_location` | no       | At most 120 characters                                           |
| `phone`                | no       | At most 32 characters                                            |

**201**

```json
{
  "user": {
    "id": "7c0c1bfb-acf5-48ad-a3ba-4ea6617e05d8",
    "email": "ada@example.com",
    "display_name": "Ada Lovelace",
    "preferred_languages": ["en", "fr"],
    "approximate_location": "Cape Town",
    "phone": "+27000000000",
    "created_at": "2026-10-07T18:26:37.134182+02:00"
  },
  "access_token": "<jwt>",
  "refresh_token": "<jwt>",
  "expires_in": 900
}
```

The response never contains a password or a password hash.

**Errors:** `400 validation_error`, `400 invalid_request`, `409 email_taken`,
`413 request_too_large`, `500 internal_error`.

## POST /auth/login

Exchanges credentials for a token pair.

**Request**

```json
{ "email": "ada@example.com", "password": "at-least-8-characters" }
```

**200** — the same shape as register.

**Errors:** `401 invalid_credentials`, `400 invalid_request`, `413 request_too_large`,
`500 internal_error`.

A wrong password and an unknown email both return `401 invalid_credentials` with an
identical message, so the endpoint cannot be used to discover which accounts exist.

## Authentication

`POST /stories`, `POST /stories/{id}/adapt`, `POST /versions/{id}/comments`,
`POST /comments/{id}/bridges`, `POST /users/me/rooted`, and `GET /users/me/rooted` are
protected routes. Every other route is public and needs no credentials.

A protected route requires an access token in the standard header:

```
Authorization: Bearer <access_token>
```

The token is verified for signature, algorithm (HS256 only), expiry, and type, so a
refresh token presented as an access token is rejected. Every failure — no header, the
wrong scheme, an expired token, a foreign signature, a refresh token — returns the same
`401 unauthorized` response, because the client's remedy is identical in each case and a
distinguishable answer would confirm guesses about the token.

## Stories

A story is one cultural artefact: a title, the story itself, the language it is told in,
and the pillar it belongs to.

| Route             | Auth       | Purpose                     |
| ----------------- | ---------- | --------------------------- |
| `POST /stories`   | `Bearer`   | Publish a story             |
| `GET /stories`    | public     | Read the feed, newest first |
| `GET /stories/{id}` | public   | Read one story              |

### The story object

```json
{
  "id": "d6b53a2c-2e2f-4a4d-9b0f-3f6f4e0f1a2b",
  "author_id": "7c0c1bfb-acf5-48ad-a3ba-4ea6617e05d8",
  "root_version_id": "0f6c2b1a-9e2d-4c7b-8a31-6d5e4f3c2b1a",
  "pillar": "wonder",
  "language": "en",
  "title": "The first rain",
  "body": "Grandmother said the first rain remembers every name.",
  "approximate_location": "Cape Town",
  "media_urls": ["https://example.com/rain.jpg"],
  "sensitive": false,
  "created_at": "2026-10-07T18:26:37.134182+02:00",
  "updated_at": "2026-10-07T18:26:37.134182+02:00"
}
```

`media_urls` is always an array, never `null`. `approximate_location` is an empty string
when it was not given.

`GET /stories/{id}` also carries `author_rooted`, the author's inline Rooted summary, or
`null`. See [`author_rooted` on content responses](#author_rooted-on-content-responses).

`language`, `title`, and `body` are the content of the story's **root version**, and
`root_version_id` names that version. A story's content is not stored on the story
itself: every telling of it — the original and every adaptation — is a version. See
[Versions and the Language Tree](#versions-and-the-language-tree).

### POST /stories

Publishes a story as the authenticated user and returns **201** with the stored story,
wrapped as `{ "story": { ... } }`.

The response is what the database stored, not an echo of the request: `id`, `created_at`,
and `updated_at` are generated server-side.

**Request**

```json
{
  "pillar": "wonder",
  "language": "en",
  "title": "The first rain",
  "body": "Grandmother said the first rain remembers every name.",
  "approximate_location": "Cape Town",
  "media_urls": ["https://example.com/rain.jpg"],
  "sensitive": false
}
```

| Field                  | Required | Rules                                                        |
| ---------------------- | -------- | ------------------------------------------------------------ |
| `pillar`               | yes      | Exactly `wonder` or `heritage`                               |
| `language`             | yes      | 2-8 letters; stored lower-cased                              |
| `title`                | yes      | 1-200 characters after trimming                              |
| `body`                 | yes      | 1-10000 characters; stored verbatim, so formatting survives  |
| `approximate_location` | no       | At most 100 characters after trimming                        |
| `media_urls`           | no       | At most 20 links, each at most 2048 characters, none blank    |
| `sensitive`            | no       | Defaults to `false`                                          |

**There is no `author_id` field.** The author is taken from the access token. A request
that includes `author_id` is rejected as an unknown field (400) rather than silently
ignored, so there is never any doubt about which value was used.

**Errors:** `401 unauthorized`, `400 validation_error`, `400 invalid_request`,
`413 request_too_large`, `500 internal_error`.

### GET /stories/{id}

Returns **200** with `{ "story": { ... } }`.

A story that does not exist returns **404** `not_found`. An id that is not a UUID also
returns 404: it cannot name an existing row, and answering 404 keeps the lookup on the
id index instead of casting the column to text.

**Errors:** `404 not_found`, `500 internal_error`.

### GET /stories

Returns **200** with one page of the feed, newest first:

```json
{ "stories": [ { "id": "...", "title": "..." } ], "next_cursor": "MjAyNi0xMC0wN1QxODoyNjo0Mlo..." }
```

| Query    | Required | Rules                                              |
| -------- | -------- | -------------------------------------------------- |
| `cursor` | no       | A `next_cursor` from a previous page. Omit for the first page |
| `limit`  | no       | 1-50. Defaults to 20; a larger value is clamped to 50 |

`next_cursor` is **always present**. An empty string means the feed has no further pages;
any other value is an opaque token to send back as `cursor` to fetch the next page.
Treat it as opaque: it encodes the sort position, and its format may change.

`stories` is always an array, never `null`, so an empty feed is `"stories": []`.

Paging is keyset-based rather than offset-based, so a page is selected by the sort
position rather than by counting rows. A story published between two requests does not
shift the pages, and no story is skipped or repeated. The cursor is exclusive: it resumes
**after** the last story of the previous page.

**Errors:** `400 validation_error` (an unreadable `cursor`, or a `limit` that is not a
positive integer), `500 internal_error`.

## Versions and the Language Tree

A story's content is a **version**, and a human adaptation is a new version that
descends from an older one. Every story has exactly one **root version** (the original,
with no parent), and every adaptation names the version it came from. Together they form
the story's **Language Tree**.

The tree is an adjacency list: the server returns it as a flat list in which each version
carries its `parent_version_id`, and the client assembles the nesting. See
KNOT-ADR-011 in [`docs/DECISIONS.md`](DECISIONS.md).

| Route                      | Auth     | Purpose                                 |
| -------------------------- | -------- | --------------------------------------- |
| `POST /stories/{id}/adapt` | `Bearer` | Add a human adaptation of a version      |
| `GET /stories/{id}/tree`   | public   | Every version of a story, oldest first   |
| `GET /versions/{id}`       | public   | Read one version                         |

### The version object

```json
{
  "id": "0f6c2b1a-9e2d-4c7b-8a31-6d5e4f3c2b1a",
  "story_id": "d6b53a2c-2e2f-4a4d-9b0f-3f6f4e0f1a2b",
  "parent_version_id": null,
  "author_id": "7c0c1bfb-acf5-48ad-a3ba-4ea6617e05d8",
  "language": "en",
  "title": "The first rain",
  "body": "Grandmother said the first rain remembers every name.",
  "adaptation_note": null,
  "created_at": "2026-10-07T18:26:37.134182+02:00",
  "updated_at": "2026-10-07T18:26:37.134182+02:00"
}
```

`parent_version_id` is `null` for a story's root version and a version id for an
adaptation. `adaptation_note` is `null` when the adapter left none. Version responses also
carry `author_rooted`, the author's inline Rooted summary, or `null`. See
[`author_rooted` on content responses](#author_rooted-on-content-responses).

### POST /stories/{id}/adapt

Adds a version adapted from an existing one and returns **201** with the stored version,
wrapped as `{ "version": { ... } }`. The response is what the database stored, not an
echo of the request.

**Request**

```json
{
  "parent_version_id": "0f6c2b1a-9e2d-4c7b-8a31-6d5e4f3c2b1a",
  "language": "fr",
  "title": "La première pluie",
  "body": "Grand-mère disait que la première pluie se souvient de chaque nom.",
  "adaptation_note": "Rendered for French-speaking listeners."
}
```

| Field               | Required | Rules                                                    |
| ------------------- | -------- | -------------------------------------------------------- |
| `parent_version_id` | yes      | UUID. Must belong to the story named in the path          |
| `language`          | yes      | 2-8 letters; stored lower-cased                           |
| `title`             | yes      | 1-200 characters after trimming                           |
| `body`              | yes      | 1-10000 characters; stored verbatim                        |
| `adaptation_note`   | no       | At most 1000 characters after trimming                     |

**There is no `author_id` field.** The adapter is taken from the access token. A request
that includes `author_id` is rejected as an unknown field (400).

**Errors:** `401 unauthorized`, `400 validation_error` (including a `parent_version_id`
that belongs to a different story, or is not a UUID), `400 invalid_request`,
`404 not_found` (the story does not exist, or the parent version does not exist),
`413 request_too_large`, `500 internal_error`.

### GET /stories/{id}/tree

Returns **200** with every version of the story, oldest first:

```json
{
  "story_id": "d6b53a2c-2e2f-4a4d-9b0f-3f6f4e0f1a2b",
  "versions": [
    { "id": "0f6c2b1a-...", "parent_version_id": null },
    { "id": "1a2b3c4d-...", "parent_version_id": "0f6c2b1a-..." }
  ]
}
```

`versions` is always an array, never `null`. The list is flat, not nested: assemble the
tree from `parent_version_id`. The first version is the root, whose `parent_version_id`
is `null`.

**Errors:** `404 not_found` (the story does not exist, or its id is not a UUID),
`500 internal_error`.

### GET /versions/{id}

Returns **200** with `{ "version": { ... } }`.

**Errors:** `404 not_found` (no such version, or the id is not a UUID),
`500 internal_error`.

## Conversations and Bridges

A conversation is the flat list of comments on one story **version**, newest first.
Comments are how people talk about a telling; a **bridge** is how that talk crosses a
language boundary.

A bridge does not translate in place. It creates a **new comment** in the target
language, on the **story's version written in that language**, and records a bridge that
references both. The source comment is left untouched, so both conversations stay intact,
and the bridge is a first-class object with its own id. A bridge therefore connects two
conversations rather than adding to one. See KNOT-ADR-014 in
[`docs/DECISIONS.md`](DECISIONS.md).

| Route                          | Auth     | Purpose                                  |
| ------------------------------ | -------- | ---------------------------------------- |
| `POST /versions/{id}/comments` | `Bearer` | Comment on a version                      |
| `GET /versions/{id}/comments`  | public   | Read a version's comments, newest first    |
| `POST /comments/{id}/bridges`  | `Bearer` | Bridge a comment into another language     |
| `GET /comments/{id}/bridges`   | public   | Bridges touching a comment                 |
| `GET /bridges/{id}`            | public   | Read one bridge                            |

### The comment object

```json
{
  "id": "66666666-6666-4666-8666-666666666666",
  "version_id": "44444444-4444-4444-8444-444444444444",
  "author_id": "7c0c1bfb-acf5-48ad-a3ba-4ea6617e05d8",
  "language": "en",
  "body": "The first rain remembers every name.",
  "created_at": "2026-10-08T18:26:37.134182+02:00",
  "updated_at": "2026-10-08T18:26:37.134182+02:00"
}
```

### The bridge object

```json
{
  "id": "77777777-7777-4777-8777-777777777777",
  "source_comment_id": "66666666-6666-4666-8666-666666666666",
  "target_comment_id": "88888888-8888-4888-8888-888888888888",
  "author_id": "7c0c1bfb-acf5-48ad-a3ba-4ea6617e05d8",
  "target_language": "fr",
  "adaptation_note": "Rendered for French-speaking listeners.",
  "created_at": "2026-10-08T18:30:00.000000+02:00"
}
```

`adaptation_note` is `null` when the bridger left none. Comment and bridge responses also
carry `author_rooted`, the author's inline Rooted summary, or `null`. See
[`author_rooted` on content responses](#author_rooted-on-content-responses).

### POST /versions/{id}/comments

Comments on a version as the authenticated user and returns **201** with the stored
comment, wrapped as `{ "comment": { ... } }`.

**Request**

```json
{ "body": "The first rain remembers every name.", "language": "en" }
```

| Field      | Required | Rules                            |
| ---------- | -------- | -------------------------------- |
| `body`     | yes      | 1-5000 characters; stored verbatim |
| `language` | yes      | 2-8 letters; stored lower-cased   |

**There is no `author_id` field.** The commenter is taken from the access token.

**Errors:** `401 unauthorized`, `400 validation_error`, `400 invalid_request`,
`404 not_found` (the version does not exist), `413 request_too_large`,
`500 internal_error`.

### GET /versions/{id}/comments

Returns **200** with one page of the version's comments, newest first:

```json
{ "comments": [ { "id": "...", "body": "..." } ], "next_cursor": "MjAyNi0xMC0wOFQxODo..." }
```

| Query    | Required | Rules                                                        |
| -------- | -------- | ------------------------------------------------------------ |
| `cursor` | no       | A `next_cursor` from a previous page. Omit for the first page  |
| `limit`  | no       | 1-50. Defaults to 20; a larger value is clamped to 50          |

`comments` is always an array, never `null`, and `next_cursor` is always present. Paging
is keyset, exactly as for the feed: a page is a range of the sort order, so a comment
posted between two requests neither shifts nor repeats a page.

**Errors:** `400 validation_error` (an unreadable `cursor`, or a `limit` that is not a
positive integer), `404 not_found` (the version does not exist, or its id is not a UUID),
`500 internal_error`.

### POST /comments/{id}/bridges

Bridges the comment into another language and returns **201** with the bridge plus both
comments, so a client does not have to fetch them:

```json
{
  "bridge": { "id": "...", "source_comment_id": "...", "target_comment_id": "...", "target_language": "fr" },
  "source_comment": { "id": "...", "language": "en" },
  "target_comment": { "id": "...", "language": "fr" }
}
```

**Request**

```json
{
  "target_language": "fr",
  "body": "La première pluie se souvient de chaque nom.",
  "adaptation_note": "Rendered for French-speaking listeners."
}
```

| Field             | Required | Rules                                                                         |
| ----------------- | -------- | ----------------------------------------------------------------------------- |
| `target_language` | yes      | 2-8 letters; stored lower-cased; must differ from the source comment's language |
| `body`            | yes      | The target comment's body, 1-5000 characters                                   |
| `adaptation_note` | no       | At most 1000 characters after trimming                                          |

**There is no `author_id` field.** The bridger is taken from the access token. The server
resolves the target comment's version from the source comment's story and `target_language`;
when the story has several versions in that language the oldest is used. A source comment
may be bridged into any one given language once; a second attempt returns
`400 validation_error`.

**Errors:** `401 unauthorized`, `400 validation_error` (including a target language equal
to the source's, a story with no version in that language, or a language the comment has
already been bridged into), `400 invalid_request`, `404 not_found` (the source comment
does not exist), `413 request_too_large`, `500 internal_error`.

### GET /comments/{id}/bridges

Returns **200** with `{ "bridges": [ ... ] }`: every bridge in which the comment is the
source or the target, newest first. `bridges` is always an array, never `null`.

**Errors:** `404 not_found` (the comment does not exist, or its id is not a UUID),
`500 internal_error`.

### GET /bridges/{id}

Returns **200** with `{ "bridge": { ... } }`.

**Errors:** `404 not_found` (no such bridge, or the id is not a UUID),
`500 internal_error`.

## Rooted

Rooted is a person's self-declared connection to a place: a place name at **city or region
precision only**, and a duration bucket. It is self-declared and never verified — there is
no vouching, no score, and no gating. See KNOT-ADR-016 in
[`docs/DECISIONS.md`](DECISIONS.md).

| Route                    | Auth     | Purpose                                       |
| ------------------------ | -------- | --------------------------------------------- |
| `POST /users/me/rooted`  | `Bearer` | Set (or replace) your own Rooted signal        |
| `GET /users/me/rooted`   | `Bearer` | Read your own signals, private ones included   |
| `GET /users/{id}/rooted` | public   | Read a user's public signals                   |

### The signal object

```json
{
  "id": "99999999-9999-4999-8999-999999999999",
  "user_id": "7c0c1bfb-acf5-48ad-a3ba-4ea6617e05d8",
  "place": "Cape Town",
  "duration_bucket": "lifelong",
  "is_public": true,
  "is_primary": true,
  "created_at": "2026-10-08T12:00:00Z",
  "updated_at": "2026-10-08T12:00:00Z"
}
```

`duration_bucket` is one of `lifelong`, `many_years`, `several_years`, `a_few_years`, or
`recently`. A person has exactly one **primary** signal, so `is_primary` is always `true`
for a stored signal in this MVP: setting a signal replaces the previous one rather than
adding another.

### POST /users/me/rooted

Sets the authenticated user's primary Rooted signal and returns **200** with
`{ "signal": { ... } }`. A second call **replaces** the first.

**Request**

```json
{ "place": "Cape Town", "duration_bucket": "lifelong", "is_public": true }
```

| Field             | Required | Rules                                                                    |
| ----------------- | -------- | ------------------------------------------------------------------------ |
| `place`           | yes      | 1-80 characters after trimming; a single line (no line breaks or tabs)    |
| `duration_bucket` | yes      | Exactly one of the five values above                                      |
| `is_public`       | no       | Defaults to `true`; `false` hides the signal from public read             |

**There is no `user_id` field.** The owner is taken from the access token. A request that
includes `user_id` is rejected as an unknown field (400). A signal is public unless
`is_public` is explicitly `false`, and that default is applied by the server rather than
assumed from the client.

**Errors:** `401 unauthorized`, `400 validation_error` (including a `place` that is empty,
too long, or contains a line break or a tab, and a `duration_bucket` outside the five
values), `400 invalid_request`, `413 request_too_large`, `500 internal_error`.

### GET /users/me/rooted

Returns **200** with `{ "signals": [ ... ] }`: the authenticated user's own signals,
**including** any hidden from public read. `signals` is always an array, never `null`.

**Errors:** `401 unauthorized`, `500 internal_error`.

### GET /users/{id}/rooted

Returns **200** with `{ "signals": [ ... ] }`: the user's **public** signals only.

An unknown user returns **404** `not_found`, so an empty array means "this user has
declared nothing public" rather than "no such user". An id that is not a UUID also returns
404.

**Errors:** `404 not_found`, `500 internal_error`.

### `author_rooted` on content responses

The story detail, version, comment, and bridge responses carry an `author_rooted` field:

```json
{ "author_rooted": { "place": "Cape Town", "duration_bucket": "lifelong" } }
```

It is the author's primary **public** Rooted signal, or `null` when they have none (or have
hidden it). It carries only `place` and `duration_bucket` — no `id`, no `user_id`, and no
timestamps — because it is an inline display summary, not a lookup of the full signal
(KNOT-ADR-017). The server attaches it with a single batched read of every distinct author
in a response, never one query per row.

| Response                                                                             | `author_rooted` describes            |
| ------------------------------------------------------------------------------------ | ------------------------------------- |
| `GET /stories/{id}`                                                                    | the story's author                     |
| `POST /stories/{id}/adapt`, `GET /stories/{id}/tree`, `GET /versions/{id}`             | each version's author                  |
| `POST /versions/{id}/comments`, `GET /versions/{id}/comments`                           | each comment's author                  |
| `POST /comments/{id}/bridges`, `GET /comments/{id}/bridges`, `GET /bridges/{id}`       | each bridge's author (and, on create, both comments' authors) |

`GET /stories` (the feed) and `POST /stories` do not populate the field: it is present
there but always `null`.

## Tokens

Two HS256 JWTs are issued. Both are signed with `KNOT_JWT_SECRET`.

| Token           | Lifetime | Claims                                    |
| --------------- | -------- | ----------------------------------------- |
| `access_token`  | 15 min   | `typ=access`, `sub=<user id>`, `iat`, `exp` |
| `refresh_token` | 30 days  | `typ=refresh`, `sub`, `iat`, `exp`, `jti` (random) |

Verified claims are checked for signature, algorithm (HS256 only), expiry, and token
type, so a refresh token cannot be used as an access token. Send the access token in an
`Authorization: Bearer` header to reach a protected route.

**Not yet implemented:** a refresh endpoint, logout, and token revocation. Only issuance
exists today. Removing `refresh_token`/`jti` rotation and revocation from scope was
deliberate — see KNOT-ADR-005. Access tokens are not stored server-side, so a protected
route trusts the signature and expiry and nothing else.
