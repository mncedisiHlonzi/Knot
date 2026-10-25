# Knot — API

**Status: MVP, subject to change.** These endpoints exist to get the first people onto
Knot. Shapes, codes, and paths may change without notice while the product is in
foundation and early Phase 1.

- Base URL: `http://localhost:8080` (see `KNOT_HTTP_PORT` and `KNOT_API_URL`).
- All request and response bodies are JSON, sent with `Content-Type: application/json`.
- Request bodies are limited to **1 MiB**. Unknown fields are **rejected**.
- The one exception is `POST /users/me/avatar`, which takes `multipart/form-data` and is
  limited to **5 MiB** (see [Avatars](#avatars)).
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
| 400    | `invalid_request`   | Body is not a single valid JSON object, or has unknown fields; or an avatar upload is not `multipart/form-data` with a `file` field |
| 401    | `invalid_credentials` | Login failed. Deliberately identical for a wrong password and an unknown email |
| 401    | `unauthorized`      | A protected route was called without a valid access token. Identical for a missing, malformed, expired, or wrong-type token |
| 403    | `forbidden`         | You are not allowed to change this resource: only a story's author may attach media, and only the author or the uploader may delete it |
| 404    | `not_found`         | The requested resource (story, version, comment, user, avatar, or media) does not exist, or its id is not a UUID |
| 405    | *(empty body)*      | Method not allowed for that path                     |
| 409    | `email_taken`       | That email is already registered                     |
| 413    | `request_too_large` | Body exceeded 1 MiB; an avatar exceeded 5 MiB; or an image exceeded 10 MiB, or a video 100 MiB |
| 415    | `unsupported_media_type` | The uploaded avatar is not a JPEG, PNG, or WebP, or the uploaded story media is not a JPEG, PNG, WebP, MP4, or MOV |
| 500    | `internal_error`    | Something failed server-side. No internal detail is returned |

## GET /health

Liveness only. It touches no dependency, so it answers "is this process serving HTTP",
not "is the database healthy".

**200**

```json
{ "status": "ok", "version": "0.1.0", "time": "2026-10-07T16:26:36Z" }
```

`time` is RFC3339 in UTC.

## GET /languages

Every language the API accepts, for a client to offer in a picker. Public: no token
is needed, because a picker is needed before anyone has registered.

**200**

```json
{
  "languages": [
    { "code": "eng", "name": "English" },
    { "code": "zul", "name": "Zulu" }
  ]
}
```

`languages` is always an array, never `null`, sorted alphabetically by `name`. The
response is the whole list — **7,927 entries, about 270 KB** — and it is not
paginated: it is a compile-time constant on the server that a client caches for the
life of the process. `code` is an ISO 639-3 code, three lower-case letters.

**Every language field below accepts exactly these codes, matched exactly.** See
[Languages](#languages).

## Languages

A user, story, version, or comment names its language with an
**ISO 639-3 code**: three lower-case ASCII letters, such as `eng` or `zul`.
`GET /languages` is the authoritative list, and it is duplicated on the device so
the picker cannot offer a value the server rejects (KNOT-ADR-046).

ISO 639-3 rather than ISO 639-1: the two-letter set names only the 184 languages
that happen to have a two-letter code, which excluded Sepedi (`nso`) and thousands
of other languages people actually tell stories in. ISO 639-3 names every language
with one consistent three-letter code.

Three rules follow from that, and apply to `language`, `preferred_languages`, and
`target_language` alike:

- The code must be in the list. `English`, `eng`, `nso` (a two-letter code), `zzz`,
  and `en-ZA` are all rejected with `400 validation_error`.
- **Case matters.** `ENG` is rejected; a client sends `eng`. The server does not
  case-fold, so one language has one stored spelling.
- Surrounding whitespace is trimmed, and an empty entry in `preferred_languages` is
  dropped rather than rejected.

A code is three letters, never two, so a client that still holds a two-letter value
from an earlier release must map it before sending it. Region variants such as
`pt-BR` remain out of scope: a language is a code, not a locale.

Names are the ISO 639-3 reference names (SIL), in English and verbatim — Sepedi is
listed as `Pedi` — so the list can be diffed against a future revision of the
registry.

## POST /auth/register

Creates an account. It returns **no tokens**: an account cannot sign in until its
address is verified (KNOT-ADR-083), so a session is only obtainable from
[`POST /auth/login`](#post-authlogin).

A verification link is written to the server console instead of being emailed
(KNOT-ADR-088); see [Testing Auth Locally](DEVELOPMENT.md#testing-auth-locally).

**Request**

```json
{
  "email": "ada@example.com",
  "password": "at-least-8-characters-1",
  "display_name": "Ada Lovelace",
  "preferred_languages": ["eng", "fra"],
  "approximate_location": "Cape Town",
  "phone": "+27000000000"
}
```

| Field                  | Required | Rules                                                          |
| ---------------------- | -------- | -------------------------------------------------------------- |
| `email`                | yes      | Valid address, at most 254 characters, stored lower-cased        |
| `password`             | yes      | At least 8 characters, at most 1024 bytes, and at least one letter and one number (KNOT-ADR-086) |
| `display_name`         | yes      | 1-80 characters after trimming                                   |
| `preferred_languages`  | no       | An array of at most 20 ISO 639-3 codes; blanks dropped. See [Languages](#languages) |
| `approximate_location` | no       | At most 120 characters                                           |
| `phone`                | no       | At most 32 characters                                            |

**201**

```json
{
  "user": {
    "id": "7c0c1bfb-acf5-48ad-a3ba-4ea6617e05d8",
    "email": "ada@example.com",
    "display_name": "Ada Lovelace",
    "preferred_languages": ["eng", "fra"],
    "approximate_location": "Cape Town",
    "phone": "+27000000000",
    "created_at": "2026-10-07T18:26:37.134182+02:00",
    "email_verified_at": null,
    "avatar_url": ""
  }
}
```

The response never contains a password or a password hash.

**Errors:** `400 validation_error`, `400 invalid_request`, `409 email_taken`,
`413 request_too_large`, `429 rate_limited`, `500 internal_error`.

The endpoint is limited to **5 attempts per hour per client address**.

### The user object

Register, login, and the avatar upload all return the same projection, so a client has
one profile shape to parse.

| Field                  | Type            | Notes                                                                    |
| ---------------------- | --------------- | ------------------------------------------------------------------------ |
| `id`                   | string (UUID)   |                                                                          |
| `email`                | string          | Stored lower-cased                                                       |
| `display_name`         | string          |                                                                          |
| `preferred_languages`  | array of string | Always an array, never `null`                                            |
| `approximate_location` | string          | `""` when unset                                                          |
| `phone`                | string          | `""` when unset                                                          |
| `created_at`           | string          | RFC3339                                                                  |
| `email_verified_at`    | string \| null  | RFC3339 once the address is proven, `null` until then (KNOT-ADR-083)     |
| `avatar_url`           | string          | Path on this API, or `""` when the user has no avatar. Never a link to object storage — see [Avatars](#avatars) |

**Errors:** `400 validation_error`, `400 invalid_request`, `409 email_taken`,
`413 request_too_large`, `500 internal_error`.

## POST /auth/login

Exchanges credentials for a session.

**Request**

```json
{
  "email": "ada@example.com",
  "password": "at-least-8-characters-1",
  "device_info": { "platform": "ios", "app_version": "0.1.0" }
}
```

| Field         | Required | Rules                                                                 |
| ------------- | -------- | --------------------------------------------------------------------- |
| `email`       | yes      |                                                                       |
| `password`    | yes      |                                                                       |
| `device_info` | no       | Free-form JSON object, stored with the session and shown in the device list. Never used for an authorisation decision |

**200**

```json
{
  "user": { "id": "7c0c1bfb-…", "email_verified_at": "2026-10-07T18:30:00Z" },
  "access_token": "<jwt>",
  "refresh_token": "<session-uuid>.<secret>",
  "expires_in": 900
}
```

`access_token` is a JWT valid for 15 minutes and carries the session id in its `sid`
claim. `refresh_token` is an opaque string, valid for 30 days, and is **rotated on every
use** (KNOT-ADR-085). A user may hold at most 5 live sessions; a sixth sign-in revokes the
oldest (KNOT-ADR-084).

**Errors:** `401 invalid_credentials`, `403 email_not_verified`, `400 invalid_request`,
`413 request_too_large`, `429 rate_limited`, `500 internal_error`.

A wrong password and an unknown email both return `401 invalid_credentials` with an
identical message, so the endpoint cannot be used to discover which accounts exist.
`403 email_not_verified` is only ever returned **after** the password has been checked.

Limited to **10 attempts per 15 minutes per client address** and **10 per 15 minutes per
account**.

## POST /auth/verify-email

Redeems the token from a verification link and unlocks sign-in.

**Request**

```json
{ "token": "<token from the console-logged link>" }
```

**200** `{ "success": true }`

**Errors:** `400 invalid_verification_token` (unknown, already used, expired, or of the
wrong type), `400 invalid_request`, `429 rate_limited`.

## POST /auth/refresh

Rotates the refresh token and mints a new pair. The token that was sent stops working
immediately.

**Request**

```json
{ "refresh_token": "<session-uuid>.<secret>" }
```

**200** — the same shape as login.

**Errors:** `401 invalid_refresh_token` (malformed, unknown, revoked — or reused, in
which case **every session on the account is revoked**), `400 invalid_request`,
`429 rate_limited`.

Limited to **30 refreshes per 15 minutes per session**.

## POST /auth/logout

Revokes the session the presented access token belongs to. Protected.

The session is read from the token's own `sid` claim, never from the request body, so a
caller cannot sign out another device.

**200** `{ "success": true }`

**Errors:** `401 unauthorized`, `500 internal_error`. Signing out twice is not an error.

## GET /auth/sessions

Lists the caller's live sessions, newest first. Protected.

**200**

```json
{
  "sessions": [
    {
      "id": "7f9d4d3c-…",
      "device_info": { "platform": "ios", "app_version": "0.1.0" },
      "ip_address": "203.0.113.7",
      "last_used_at": "2026-10-26T09:12:44Z",
      "created_at": "2026-10-26T08:00:00Z",
      "current": true
    }
  ]
}
```

`device_info` is `{}` rather than `null` when the client sent none. `current` marks the
session the request was made from.

**Errors:** `401 unauthorized`, `500 internal_error`.

## DELETE /auth/sessions/{id}

Revokes one of the caller's own sessions. Protected.

**200** `{ "success": true }`

**Errors:** `404 session_not_found` (unknown id **or** another account's session — the
API never confirms that an id exists on someone else's account), `401 unauthorized`.

## POST /auth/forgot-password

Starts a password reset. A reset link is written to the server console instead of being
emailed (KNOT-ADR-088), and is valid for 30 minutes.

**Request**

```json
{ "email": "ada@example.com" }
```

**200** `{ "success": true }` — **always**, whether or not the address has an account, so
the endpoint cannot be used to enumerate accounts (KNOT-ADR-087).

**Errors:** `400 invalid_request`, `429 rate_limited`.

Limited to **3 requests per hour per address**.

## POST /auth/reset-password

Sets a new password from a reset token, then revokes **every** session on the account
(KNOT-ADR-087).

**Request**

```json
{ "token": "<token from the console-logged link>", "new_password": "another strong 1" }
```

**200** `{ "success": true }`

The token is single use. The new password must satisfy the same policy as registration.

**Errors:** `400 invalid_reset_token` (unknown or expired), `400 validation_error`,
`429 rate_limited`.

## Authentication

`POST /stories`, `POST /stories/{id}/adapt`, `POST /versions/{id}/comments`,
`POST /users/me/rooted`, `GET /users/me/rooted`,
`POST /users/me/avatar`, `POST /stories/{id}/media`, `DELETE /stories/{id}/media/{mid}`,
`POST /auth/logout`, `GET /auth/sessions` and `DELETE /auth/sessions/{id}`
are protected routes. Every other route is public and needs no credentials.

A protected route requires an access token in the standard header:

```
Authorization: Bearer <access_token>
```

The token is verified for signature, algorithm (HS256 only), expiry, and type, so a
refresh token presented as an access token is rejected. Every failure — no header, the
wrong scheme, an expired token, a foreign signature, a refresh token — returns the same
`401 unauthorized` response, because the client's remedy is identical in each case and a
distinguishable answer would confirm guesses about the token.

## Avatars

A user's profile image. The bytes live in object storage, but they are **only ever read
and written by the backend**: no presigned URL is issued, no bucket is public, and
`avatar_url` is a path on this API rather than a link to a store (KNOT-ADR-028,
KNOT-ADR-029).

| Route                    | Auth     | Purpose                            |
| ------------------------ | -------- | ---------------------------------- |
| `POST /users/me/avatar`  | `Bearer` | Upload or replace your own avatar   |
| `GET /users/{id}/avatar` | public   | Fetch a user's avatar image         |

### POST /users/me/avatar

`Content-Type: multipart/form-data`, with the image in a field named **`file`**. The
owner comes from the access token, never from the request, so one account can never write
into another account's namespace.

| Constraint | Rule                                                                        |
| ---------- | --------------------------------------------------------------------------- |
| Size       | At most **5 MiB**                                                            |
| Format     | JPEG, PNG, or WebP. The type is decided by inspecting the bytes, not by the `Content-Type` you declare, so a mislabelled file is rejected rather than reflected back |
| Body       | Must be `multipart/form-data` carrying a `file` field                        |

Replacing an avatar stores a new object and deletes the previous one, so a user never
accumulates avatars. The path is stable and only the `v` parameter changes: a client that
strips the query string still reaches the current avatar, and a cache keyed on the old URL
is simply not reused.

**200**

```json
{
  "user": {
    "id": "7c0c1bfb-acf5-48ad-a3ba-4ea6617e05d8",
    "email": "ada@example.com",
    "display_name": "Ada Lovelace",
    "preferred_languages": ["eng"],
    "approximate_location": "Cape Town",
    "phone": "+27000000000",
    "created_at": "2026-10-07T18:26:37.134182+02:00",
    "avatar_url": "/users/7c0c1bfb-acf5-48ad-a3ba-4ea6617e05d8/avatar?v=0f8b1c2d-9e34-4a7b-8c5d-1a2b3c4d5e6f.png"
  }
}
```

The body is the same user projection that register and login return, so a client does not
need a second request to learn the new URL.

**Errors:** `400 invalid_request` (not multipart, or no `file` field), `401 unauthorized`,
`404 not_found` (the token's subject is not a user), `413 request_too_large`,
`415 unsupported_media_type`, `500 internal_error`.

### GET /users/{id}/avatar

Serves the image bytes with the stored `Content-Type` and
`Cache-Control: public, max-age=3600`. The query string is ignored: it exists so a
replaced avatar gets a fresh URL and caches keyed on the old one are not reused.

A user with no avatar, a user that does not exist, and a row whose object is missing all
return the same `404 not_found`. They are deliberately indistinguishable, so this endpoint
cannot be used to test whether an account exists.

**Errors:** `404 not_found`, `500 internal_error`.

## Stories

A story is one cultural artefact: a title, the story itself, the language it is told in,
and the pillar it belongs to.

| Route             | Auth       | Purpose                     |
| ----------------- | ---------- | --------------------------- |
| `POST /stories`   | `Bearer`   | Publish a story             |
| `GET /stories`    | public     | Read the feed: stories and adaptations of them, newest first |
| `GET /stories/{id}` | public   | Read one story              |

### The story object

```json
{
  "id": "d6b53a2c-2e2f-4a4d-9b0f-3f6f4e0f1a2b",
  "author_id": "7c0c1bfb-acf5-48ad-a3ba-4ea6617e05d8",
  "author_display_name": "Ada Lovelace",
  "author_avatar_url": "/users/7c0c1bfb-acf5-48ad-a3ba-4ea6617e05d8/avatar?v=ada.png",
  "root_version_id": "0f6c2b1a-9e2d-4c7b-8a31-6d5e4f3c2b1a",
  "pillar": "wonder",
  "language": "eng",
  "title": "The first rain",
  "body": "Grandmother said the first rain remembers every name.",
  "approximate_location": "Cape Town",
  "latitude": -33.9249,
  "longitude": 18.4241,
  "place_country": "South Africa",
  "media_urls": ["https://example.com/rain.jpg"],
  "media": [
    {
      "id": "9b2c7d1e-...-4a6f",
      "media_type": "image",
      "source": "camera",
      "mime_type": "image/jpeg",
      "width": 1024,
      "height": 768,
      "duration_ms": null,
      "size_bytes": 204800,
      "display_order": 0,
      "created_at": "2026-10-09T18:26:37.134182+02:00",
      "url": "/stories/d6b53a2c-.../media/9b2c7d1e-.../content"
    }
  ],
  "created_at": "2026-10-07T18:26:37.134182+02:00",
  "updated_at": "2026-10-07T18:26:37.134182+02:00",
  "version_count": 4,
  "language_count": 3,
  "comment_count": 14,
  "media_count": 5,
  "first_media_type": "image",
  "first_media_url": "/stories/d6b53a2c-.../media/9b2c7d1e-.../content",
  "first_media_width": 1080,
  "first_media_height": 1920,
  "attribution_line": "Original story by Ada Lovelace in English.",
  "version_depth": 0
}
```

`media_urls` is always an array, never `null`. `approximate_location` is an empty string
when it was not given.

There is **no `sensitive` field**. The flag was removed from the platform in KNOT-023c
(KNOT-ADR-072): the story shape no longer carries it and `POST /stories` rejects it as an
unknown field, so a client must neither send nor expect it.

`latitude`, `longitude`, and `place_country` are the story's **structured place data**, or
`null` when the author attached no place. The coordinate pair is `null` together or set
together: a story either has a point the map can plot or it has none (KNOT-ADR-034). The
Discovery Map plots `latitude`/`longitude` directly, so a small town appears even though it
is not in the client's fallback lookup table.

`attribution_line` and `version_depth` are the story's own attribution, and they appear on
the **detail** (`GET /stories/{id}`) only. A story IS its root version, so the line always
reads `Original story by {author} in {language}.` and the depth is always `0` (KNOT-021).
The feed (`GET /stories`) and the place page **omit both keys entirely**: they share this
object's shape, so the two fields are pointers carrying `omitempty`, which keeps their JSON
byte-for-byte what it was before this feature. A client that consumes the feed must
therefore treat the fields as absent. An **adaptation entry** in the feed carries its own
`attribution_line` instead, because a version is not a root and its lineage is a sentence
rather than a constant (KNOT-022).

`media` is the story's attached files (see [Story media](#story-media)). On
`GET /stories/{id}` it is the full list in `display_order`. On the feed (`GET /stories`) it
is a **single-item preview** — the first item only, or an empty array — so a card can show
one thumbnail without downloading every item's metadata. It is always an array, never
`null`.

`version_count`, `language_count`, `comment_count`, and `media_count` are the story's
aggregate counts, attached to both the feed and the detail by one batched read per count
type (never a query per story). `version_count` is every version in the story's tree,
including the root; `language_count` is the distinct languages across those versions;
`comment_count` is every comment on any version, replies included; `media_count` is the
number of attached media items. Zero is a real answer, so the fields are always present as
integers (KNOT-020c-fix).

`first_media_type` is `"image"` or `"video"`, and `first_media_url` is the path on this
API that serves the first media item's bytes — or both are `null` when the story has no
media. They let the feed draw a preview (an image, or a placeholder for a video, since no
poster frame is stored yet) without reading the `media` array (KNOT-020c-fix).

`first_media_width` and `first_media_height` are that item's pixel dimensions, or `null`
when the uploader's client did not report them (a story uploaded before the columns
existed, or from a source that could not measure it — a video asset often reports none).
They let a list frame the media at its **natural width:height ratio** instead of a fixed
one, so a portrait photo is shown whole rather than cropped into a 16:9 window; a client
falls back to its own default ratio when either is `null`, and fills that frame rather than
letterboxing inside it (KNOT-020g-fix5, KNOT-020g-fix6). The same pair is on
[`GET /users/{id}/profile`](#get-usersidprofile) as `first_media_width` /
`first_media_height` (omitted rather than `null` when unknown), and on
`GET /discovery/places/{place}` by the same enrichment.

`preview_truncated` is `true` when the story's `body` is longer than the **150**-character
preview window a list draws, and `false` otherwise. The feed still carries the whole
`body` — clamping it to a few lines is the client's job — so this answers the only question
the server can: is there more here than a preview shows? A body of exactly 150 characters
is **not** marked, because the window is exceeded, not met. It is always present as a
boolean (never `null`), so a client can render a "read more" link without inferring
truncation from the text (KNOT-020g-fix12).

Both the feed (`GET /stories`, on every story entry in the page) and the detail
(`GET /stories/{id}`) carry `author_rooted`, the author's inline Rooted summary, or `null`.
On a **version** entry in the feed it is the adapter's summary, because the entry's author
is the adapter (KNOT-022).
See [`author_rooted` on content responses](#author_rooted-on-content-responses).

`author_display_name` is the author's name, and `author_avatar_url` is a path on this API
(`/users/{id}/avatar`) that resolves to their avatar — or `null` when they have no avatar,
in which case a client renders their initials. Both are attached by one batched lookup per
response, so a card can attribute a story without a request per row (KNOT-ADR-041).
See [Author attribution](#author-attribution) for the shared contract.

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
  "language": "eng",
  "title": "The first rain",
  "body": "Grandmother said the first rain remembers every name.",
  "approximate_location": "Cape Town",
  "latitude": -33.9249,
  "longitude": 18.4241,
  "place_country": "South Africa",
  "media_urls": ["https://example.com/rain.jpg"]
}
```

| Field                  | Required | Rules                                                        |
| ---------------------- | -------- | ------------------------------------------------------------ |
| `pillar`               | yes      | Exactly `wonder` or `heritage`                               |
| `language`             | yes      | A canonical ISO 639-3 code, such as `eng` or `zul`. See [Languages](#languages) |
| `title`                | yes      | 1-200 characters after trimming                              |
| `body`                 | yes      | 1-10000 characters; stored verbatim, so formatting survives  |
| `approximate_location` | no       | At most 100 characters after trimming                        |
| `latitude`             | no       | Between -90 and 90. Must be sent with `longitude`             |
| `longitude`            | no       | Between -180 and 180. Must be sent with `latitude`            |
| `place_country`        | no       | At most 100 characters after trimming                        |
| `media_urls`           | no       | At most 20 links, each at most 2048 characters, none blank    |

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

Returns **200** with one page of the feed. The page is a **merged stream of
two entry kinds** — a story and somebody's adaptation of one — and every entry carries the
`kind` that says which it is (KNOT-022). Its order is chosen by `feed_type`: `recent`
(the default) is newest first; `for_you` is the reader's ranked order (KNOT-027).

```json
{
  "items": [
    {
      "kind": "version",
      "id": "…",
      "story_id": "…",
      "parent_version_id": "…",
      "author_id": "…",
      "language": "zul",
      "title": "Imvula yokuqala",
      "version_number": 2,
      "attribution_line": "Adapted from Nomsa Khan's original story in English.",
      "recent_interactions": [
        {
          "type": "reaction",
          "actor_display_name": "Tycoon",
          "actor_avatar_url": "/users/…/avatar?v=t.png",
          "detail": "rings_true"
        }
      ],
      "parent_author_id": "…",
      "parent_author_display_name": "Nomsa Khan",
      "parent_author_avatar_url": "/users/…/avatar?v=b.png",
      "root_author_id": "…",
      "root_author_display_name": "Nomsa Khan",
      "root_author_avatar_url": "/users/…/avatar?v=a.png",
      "created_at": "…"
    },
    { "kind": "story", "id": "…", "title": "The first rain", "created_at": "…" }
  ],
  "next_cursor": "MjAyNi0xMC0wN1QxODoyNjo0Mlo..."
}
```

| Query       | Required | Rules                                              |
| ----------- | -------- | -------------------------------------------------- |
| `cursor`    | no       | A `next_cursor` from a previous page. Omit for the first page |
| `limit`     | no       | 1-50. Defaults to 20; a larger value is clamped to 50 |
| `feed_type` | no       | `recent` (default) or `for_you`. `for_you` requires authentication |

`next_cursor` is **always present**. An empty string means the feed has no further pages;
any other value is an opaque token to send back as `cursor` to fetch the next page.
Treat it as opaque: it encodes the sort position, and its format may change. The
`for_you` cursor is **specific to the reader and to the ordering** — it is a position in
that reader's ranked list, so it must never be reused across `feed_type` values or users.

`items` is always an array, never `null`, so an empty feed is `"items": []`. The key is
`items` rather than `stories` because a page is no longer only stories (KNOT-ADR-070).
The place page ([`GET /discovery/places/{place}`](#get-discoveryplacesplace)) still answers
with `stories`, because it lists stories only.

#### The ranked feed (`feed_type=for_you`)

`for_you` orders the same merged entries with the deterministic relevance engine
(KNOT-026, KNOT-ADR-076): base score 1, +100 when the reader wrote the entry, +50 when its
language is one of the reader's preferred languages, +20 when it shares the pillar the
reader has engaged with most, and −1 per hour since it was written. Ties break by recency
and then id, so the order is reproducible.

The ranking runs over a bounded window of the newest 200 entries, so the ranked feed
describes that window rather than the whole table; the window is a deliberate limit until
the ranker moves into the store. A page is sliced from the ranked window, and the cursor
records the last entry's rank, creation time and id. While the window, the reader's
profile and their engagement are unchanged, paging never repeats or skips an entry. A
change in between (new content, new engagement, or time moving the recency term) can
reorder entries around the cursor, because the cursor is a position in a recomputed
ranking, not a snapshot of it.

#### The two entry kinds

A client switches on `kind` and then reads that shape. Never infer the kind from which
fields happen to be present: both entries are flat objects, so an unknown field on one kind
is a bug rather than a signal.

| `kind`      | What it is                                            | Identity                                   |
| ----------- | ----------------------------------------------------- | ------------------------------------------ |
| `"story"`   | Somebody's own story, exactly the [story object](#the-story-object) | `id` is the story; open `GET /stories/{id}` |
| `"version"` | One adaptation of another story                       | `id` is the **version**; open `GET /versions/{id}` |

A **root version is never an entry of its own**. A story IS its root version: the story
entry carries that content, so listing the root separately would show every story twice.

An adaptation entry is the version-side counterpart of the story object, with the same
card-shaped fields (author line, framing, media preview, aggregate counts, reactions) so
one card component can draw both. What differs:

| Field | On a `story` entry | On a `version` entry |
| ----- | ------------------ | -------------------- |
| `id` | the story | the **version** — the thing a reader opens, reacts to, and adapts from |
| `story_id` | absent | the story being adapted |
| `parent_version_id` | absent | the version this one adapts. Never `null`: a root is not a feed entry |
| `author_id` | the story's author | the **adapter**. The original author is named in `attribution_line` |
| `language`, `title`, `body` | the root version's content | the adapter's own text |
| `pillar`, `approximate_location`, `latitude`, `longitude`, `place_country`, `media_urls`, `media_count`, `first_media_*` | the story's own | **inherited from the story**: a version row carries no pillar, place, or media of its own (KNOT-ADR-030, KNOT-ADR-034) |
| `version_count`, `language_count`, `comment_count` | the story's aggregates | the **story's** aggregates, so the entry can say how much adaptation it has |
| `reactions`, `my_reactions` | the story's | the **version's**: a reader reacts to the adaptation they are reading (KNOT-021) |
| `recent_interactions` | the story's social proof: reactions on the story and comments on its **root version** | the adaptation's social proof: reactions and comments on the **version** itself |
| `version_number` | absent | the entry's position in its lineage, counting the original as **1**, so a first adaptation is `2` |
| `attribution_line` | absent | the server-rendered sentence tracing this version back to the original, shown **verbatim**; empty when the lineage could not be resolved |
| `parent_author_id`, `parent_author_display_name`, `parent_author_avatar_url` | absent | the author of the version this one **directly adapts**: the lineage actor a card credits first. For a first adaptation this is the same person as the root author, and a client renders one avatar (KNOT-022-fix2) |
| `root_author_id`, `root_author_display_name`, `root_author_avatar_url` | absent | the **original** author — the first link of the lineage — as structured data, so a card can head the entry with "{name}'s original story" without parsing `attribution_line`. The name is empty when the lineage could not be resolved; the avatar is `null` when the original author has none (KNOT-023a) |
| `root_version_id`, `updated_at`, `version_depth`, `media` | present (`version_depth` is `0`) | absent |

`attribution_line` is the same wording the version endpoints use (see
[the attribution line](#the-attribution-line)). There is no `version_depth` on a feed
entry because a feed entry is never a root, so its depth is at least 1; `version_number` is
what a reader says out loud.

`version_number` is `0` and `attribution_line` is empty when the lineage could not be
resolved. Attribution is enrichment: a page that cannot resolve a lineage is still served,
and the entry simply shows no sentence.

The three `root_author_*` and three `parent_author_*` fields are filled by the **same**
batched enrichment as `attribution_line` — one ancestry read plus the page's single author
read — so they add no query. They are present on every version entry: an unresolvable
lineage yields an empty name and a null avatar, which a client reads as "do not draw the
header" (KNOT-023a).

The entry's own `author_*` fields are the **poster** (the adapter who wrote this version),
while `parent_author_*` and `root_author_*` are the **lineage**. A card should draw the
lineage avatars in the attribution row and the poster in the author line, so the two
credits are distinct (KNOT-022-fix2).

#### Social proof (`recent_interactions`)

Every feed entry carries `recent_interactions`, always an array, never `null`. It is the
social-proof header a card draws above its author line: at most the **newest reaction** and
the **newest comment** on the entry, merged by `created_at` descending, so the list holds
zero, one, or two items (KNOT-028).

```json
"recent_interactions": [
  {
    "type": "reaction",
    "actor_display_name": "Tycoon",
    "actor_avatar_url": "/users/…/avatar?v=t.png",
    "detail": "rings_true"
  },
  {
    "type": "comment",
    "actor_display_name": "Yantuntu",
    "actor_avatar_url": null,
    "detail": null
  }
]
```

| Field | Rules |
| --- | --- |
| `type` | `"reaction"` or `"comment"` |
| `actor_display_name` | the actor's name; empty when the account could not be resolved |
| `actor_avatar_url` | the backend path, or `null` when the actor has no avatar |
| `detail` | the reaction type (for example `"rings_true"`) for a reaction; **`null`** for a comment |

The read is **batched**: one statement over `reactions` and one over `comments` for the whole
page, and the actors ride in the page's single author read, so it adds no query per entry. It
is enrichment: a failed read leaves `[]` and the page still serves. A story entry's comments
are its **root version's** — the conversation the card opens — while an adaptation entry's are
its own.

The client draws the interactions when the list is non-empty; when it is empty it falls back
to the version's `attribution_line`, and draws nothing when that is empty too. Names are bold;
the rest of the sentence is secondary ink (KNOT-028, KNOT-ADR-073).

Ordering is `(created_at DESC, id DESC)` over **both kinds**, keyed by each entry's own
`created_at`: an adaptation appears where it was written, not where its story was published.

The block filter applies to **each entry's own author**. A viewer who blocks A stops seeing
A's story *and* stops seeing any version A wrote; a version B wrote of A's story stays
visible. A block hides an author, not a lineage — the same rule the version endpoints
follow (KNOT-ADR-060).

Paging is keyset-based rather than offset-based, so a page is selected by the sort
position rather than by counting rows. An entry published between two requests does not
shift the pages, and no entry is skipped or repeated. The cursor is exclusive: it resumes
**after** the last entry of the previous page.

**Errors:** `400 validation_error` (an unreadable `cursor`, or a `limit` that is not a
positive integer), `500 internal_error`.

## Story media

Images and videos attached to a story. The bytes live in object storage, but they are
**only ever read and written by the backend**: no presigned URL is issued, no bucket is
public, and a media item's `url` is a path on this API rather than a link to a store
(KNOT-ADR-028, KNOT-ADR-029). See [the story object](#the-story-object) for the `media`
array they appear in.

Each uploaded file records how it was obtained: `source` is `camera` when it was captured
with the device camera and `gallery` when it was chosen from the photo library. The mobile
client draws a capture badge from it (KNOT-ADR-030, KNOT-ADR-031).

| Route                                       | Auth     | Purpose                                |
| ------------------------------------------- | -------- | -------------------------------------- |
| `POST /stories/{id}/media`                  | `Bearer` | Attach an image or video to a story     |
| `GET /stories/{id}/media`                   | public   | List a story's media, in display order  |
| `GET /stories/{id}/media/{mid}/content`     | public   | Stream the bytes (supports `Range`)     |
| `DELETE /stories/{id}/media/{mid}`          | `Bearer` | Remove a media item                     |

### The media object

```json
{
  "id": "9b2c7d1e-...-4a6f",
  "media_type": "image",
  "source": "camera",
  "mime_type": "image/jpeg",
  "width": 1024,
  "height": 768,
  "duration_ms": null,
  "size_bytes": 204800,
  "display_order": 0,
  "created_at": "2026-10-09T18:26:37.134182+02:00",
  "url": "/stories/d6b53a2c-.../media/9b2c7d1e-.../content"
}
```

`media_type` is `image` or `video`. `source` is `camera` or `gallery`. `width`, `height`,
and `duration_ms` are `null` when the client did not report them. `url` is a **relative**
path: resolve it against the API base URL. The object's storage key is never returned.

### POST /stories/{id}/media

`Content-Type: multipart/form-data`, with the file in a field named **`file`** and the
origin in a field named **`source`**. Optional fields `width`, `height`, and `duration_ms`
may carry the picker's metadata. Only the story's **author** may attach media in the MVP;
any other authenticated user is `403 forbidden`.

Send **`width` and `height`** whenever the picker can measure the file. They are what a list
frames the item with: the feed, a place page, and a profile wall all set the frame's aspect
ratio from this pair, so a portrait photo renders tall instead of being cropped into a fixed
window. Omitting them is legal and stores `NULL` — the client then falls back to a 16:9 frame
and fills it, cropping rather than leaving a bar. A video asset often reports no dimensions,
so a video uploaded without them is the common case that takes the fallback
(KNOT-020g-fix6).

| Constraint | Rule                                                                         |
| ---------- | ---------------------------------------------------------------------------- |
| Images     | JPEG, PNG, or WebP, at most **10 MiB**                                        |
| Videos     | MP4 or MOV, at most **100 MiB**                                               |
| Type       | Decided by inspecting the bytes, not by the `Content-Type` you declare        |
| Body       | Must be `multipart/form-data` carrying a `file` field                         |
| Count      | At most **10** items per story, images and videos together (KNOT-ADR-054)     |

`display_order` is assigned by the server: each upload appends after the story's current
last item, so the order matches the upload order.

**The 10-item cap.** A story may hold at most ten media items, images and videos counted
together. An attempt to attach an eleventh returns **400 `validation_error`** with the
message `a story may have at most 10 media items`, and nothing is stored. The cap is
enforced inside the insert, under a lock on the story row, so two uploads arriving at once
cannot both take the last slot (KNOT-ADR-054). Deleting an item frees a slot immediately.

**201** — `{ "media": { ... } }`, the stored item.

**Errors:** `400 invalid_request` (not multipart, or no `file` field), `400 validation_error`
(a bad `source`, or the story already holds 10 items), `401 unauthorized`, `403 forbidden`
(not the story's author), `404 not_found` (the story does not exist), `413
request_too_large`, `415 unsupported_media_type`, `500 internal_error`.

### GET /stories/{id}/media

Returns **200** with the story's media, in display order:

```json
{ "media": [ { "id": "...", "media_type": "image", "source": "camera", "url": "..." } ] }
```

`media` is always an array, never `null`. A story that does not exist is `404 not_found`.

**Errors:** `404 not_found`, `500 internal_error`.

### GET /stories/{id}/media/{mid}/content

Streams the bytes with the stored `Content-Type`, `Cache-Control: public, max-age=3600`,
`Accept-Ranges: bytes`, and `X-Content-Type-Options: nosniff`.

**Range support.** A valid single-range `Range: bytes=start-end` header (also `bytes=start-`
and `bytes=-suffix`) is answered **206 Partial Content** with a `Content-Range` header and
exactly the requested bytes. With no header, or an unusable one, the whole object is
answered **200** (KNOT-ADR-032). Multi-range requests are treated as unusable.

A media id that does not exist — or a row whose object is missing — returns `404 not_found`.

**Errors:** `404 not_found`, `500 internal_error`.

### DELETE /stories/{id}/media/{mid}

Removes the object from storage (best-effort) and deletes the row. Only the story's author
or the item's uploader may delete it; anyone else is `403 forbidden`. Returns **204** with
no body.

**Errors:** `401 unauthorized`, `403 forbidden`, `404 not_found`, `500 internal_error`.

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
  "author_display_name": "Ada Lovelace",
  "author_avatar_url": "/users/7c0c1bfb-acf5-48ad-a3ba-4ea6617e05d8/avatar?v=ada.png",
  "language": "eng",
  "title": "The first rain",
  "body": "Grandmother said the first rain remembers every name.",
  "adaptation_note": null,
  "created_at": "2026-10-07T18:26:37.134182+02:00",
  "updated_at": "2026-10-07T18:26:37.134182+02:00",
  "attribution_line": "Original story by Ada Lovelace in English.",
  "version_depth": 0
}
```

`parent_version_id` is `null` for a story's root version and a version id for an
adaptation. `adaptation_note` is `null` when the adapter left none. Version responses also
carry `author_rooted`, the author's inline Rooted summary, or `null`. Every version
response also carries `author_display_name` and `author_avatar_url` — see
[Author attribution](#author-attribution). See
[`author_rooted` on content responses](#author_rooted-on-content-responses)
and [The attribution line](#the-attribution-line).

### The attribution line

Every version response — `GET /versions/{id}`, `POST /stories/{id}/adapt`, and each entry
of `GET /stories/{id}/tree` — carries two derived fields:

| Field              | Type   | Meaning                                                                                       |
| ------------------ | ------ | --------------------------------------------------------------------------------------------- |
| `attribution_line` | string | The human sentence tracing this version's lineage back to the story's original                  |
| `version_depth`    | int    | How many adaptations separate this version from the original: `0` for the root, `1` for a first adaptation, and so on |

Neither is stored. Both are resolved at read time by walking `parent_version_id` up to the
root (KNOT-ADR-069), so the tree stays the single source of truth and the sentence can never
drift from it. **The server renders the sentence and a client shows it verbatim** — a client
must not re-format it, because the wording is one decision in one place. An author whose
account cannot be resolved is named `another author`, and a language that is missing or
unrecognised reads `their language`, so the sentence never has a hole in it.

The wording depends on how many versions the lineage holds (write `N` for that count;
`version_depth` is `N - 1`):

| N  | `attribution_line` |
| -- | ------------------ |
| 1  | `Original story by {author} in {language}.` |
| 2  | `Adapted from {root author}'s original story in {root language}.` |
| 3  | `Adapted from {middle author}'s {middle language} version of {root author}'s original story.` |
| 4+ | `Adapted from {parent author}'s {parent language} version, tracing back to {root author}'s original story.` |

An adaptation names the people it was adapted **from**, never the adapter: the adapter is
already the author line on the card, so repeating their name made the credit long and said
the same thing twice (KNOT-030). The target language is deliberately absent: the reader is
already looking at that version, and repeating "into {language}" made the sentence read like
a data log (KNOT-022-fix2). From four links on, the middle of the lineage is compressed into
"tracing back to", so a long chain still reads as one sentence rather than a list.

**The tree resolves every chain in memory.** The tree endpoint loads a story's versions
once and then walks the parent pointers it already holds, so a page of N versions costs the
same number of queries as a page of one.

### POST /stories/{id}/adapt

Adds a version adapted from an existing one and returns **201** with the stored version,
wrapped as `{ "version": { ... } }`. The response is what the database stored, not an
echo of the request.

**Request**

```json
{
  "parent_version_id": "0f6c2b1a-9e2d-4c7b-8a31-6d5e4f3c2b1a",
  "language": "fra",
  "title": "La première pluie",
  "body": "Grand-mère disait que la première pluie se souvient de chaque nom.",
  "adaptation_note": "Rendered for French-speaking listeners."
}
```

| Field               | Required | Rules                                                    |
| ------------------- | -------- | -------------------------------------------------------- |
| `parent_version_id` | yes      | UUID. Must belong to the story named in the path          |
| `language`          | yes      | A canonical ISO 639-3 code, such as `fra` or `zul`. See [Languages](#languages) |
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

## Conversations

A conversation is the list of comments on one story **version**, newest thread first.
Comments are how people talk about a telling.

A comment can be a **reply** to another comment. Threading is exactly one level deep: a
reply to a reply is stored against the same top-level comment its parent answers, so a
conversation is always a top-level comment followed by its replies, and never nests
further. See KNOT-ADR-047 in [`docs/DECISIONS.md`](DECISIONS.md).

| Route                          | Auth     | Purpose                                  |
| ------------------------------ | -------- | ---------------------------------------- |
| `POST /versions/{id}/comments` | `Bearer` | Comment on a version, or reply to one     |
| `GET /versions/{id}/comments`  | public   | Read a version's comments, newest first    |
| `GET /comments/{id}`           | public   | Resolve one comment, with its story        |

### Comment bridging is retired

Comment bridging is no longer part of the API. The routes `POST /comments/{id}/bridges`,
`GET /comments/{id}/bridges`, and `GET /bridges/{id}` were removed in KNOT-023, together
with the bridge reaction routes (`POST|GET /bridges/{id}/reactions`). A request to any of
them is a plain **404**: the routes do not exist. The `bridges` table is left dormant in
the database — its rows are preserved but no active code path reads it — and historical
`bridge.created` notifications are hidden from the inbox. See KNOT-ADR-071.

### The comment object

```json
{
  "id": "66666666-6666-4666-8666-666666666666",
  "version_id": "44444444-4444-4444-8444-444444444444",
  "author_id": "7c0c1bfb-acf5-48ad-a3ba-4ea6617e05d8",
  "author_display_name": "Ada Lovelace",
  "author_avatar_url": null,
  "language": "eng",
  "body": "The first rain remembers every name.",
  "parent_comment_id": null,
  "created_at": "2026-10-08T18:26:37.134182+02:00",
  "updated_at": "2026-10-08T18:26:37.134182+02:00"
}
```

`parent_comment_id` is `null` for a top-level comment, and is always present, so a client
can tell "top level" from "the server did not answer".

Comment responses also carry `author_rooted`, the author's inline Rooted summary, or
`null`, and the attribution pair `author_display_name` / `author_avatar_url` — see
[Author attribution](#author-attribution). See
[`author_rooted` on content responses](#author_rooted-on-content-responses).

### POST /versions/{id}/comments

Comments on a version as the authenticated user and returns **201** with the stored
comment, wrapped as `{ "comment": { ... } }`. With `parent_comment_id` it writes a
**reply** instead.

**Request**

```json
{
  "body": "The first rain remembers every name.",
  "language": "eng",
  "parent_comment_id": "66666666-6666-4666-8666-666666666666"
}
```

| Field               | Required | Rules                            |
| ------------------- | -------- | -------------------------------- |
| `body`              | yes      | 1-5000 characters; stored verbatim |
| `language`          | yes      | A canonical ISO 639-3 code, such as `eng`. See [Languages](#languages) |
| `parent_comment_id` | no       | The comment being replied to. Omit it, or send `null`, for a top-level comment |

The parent must be a comment **on the same version**. Naming a comment from another
version — or one that does not exist, or is not a UUID — is a `400 validation_error` with
`field` = `parent_comment_id`, not a `404`: the reply would otherwise land in a
conversation the author did not write in.

**Replying to a reply attaches to the reply's top-level comment.** If `parent_comment_id`
names a reply, the stored `parent_comment_id` is that reply's own parent, and the request
still succeeds (**201**). A client therefore never needs to resolve the thread itself.

The **notification** for a reply goes to the author of the comment that was replied to,
not to the version's author. Replying to your own comment notifies nobody. The event is
still `comment.created`.

**There is no `author_id` field.** The commenter is taken from the access token.

**Errors:** `401 unauthorized`, `400 validation_error`, `400 invalid_request`,
`404 not_found` (the version does not exist), `413 request_too_large`,
`500 internal_error`.

### GET /versions/{id}/comments

Returns **200** with one page of the version's comments, newest first:

```json
{ "comments": [ { "id": "...", "body": "...", "parent_comment_id": null } ], "next_cursor": "MjAyNi0xMC0wOFQxODo..." }
```

| Query    | Required | Rules                                                        |
| -------- | -------- | ------------------------------------------------------------ |
| `cursor` | no       | A `next_cursor` from a previous page. Omit for the first page  |
| `limit`  | no       | 1-50. Defaults to 20; a larger value is clamped to 50          |

`comments` is always an array, never `null`, and `next_cursor` is always present. Paging
is keyset, exactly as for the feed: a page is a range of the sort order, so a comment
posted between two requests neither shifts nor repeats a page.

**Each reply follows the comment it answers**, so the array is a flat, renderable list:
`[parent, its replies oldest first, next parent, its replies, ...]`. Replies are not
independently pageable and never appear as items of their own, which is why the ordering
survives the one-level depth limit.

**`limit` counts top-level comments, not rows.** A page can therefore return more than
`limit` items — up to `limit` top-level comments plus all of their replies. The cursor
advances by top-level comment, so paging never splits a thread.

**Errors:** `400 validation_error` (an unreadable `cursor`, or a `limit` that is not a
positive integer), `404 not_found` (the version does not exist, or its id is not a UUID),
`500 internal_error`.

### GET /comments/{id}

Resolves a comment id to the comment, its version, and its story. It exists so a
notification that points at a comment can open that comment's thread without a
second lookup.

Returns **200** with:

```json
{
  "comment": {
    "id": "66666666-6666-4666-8666-666666666666",
    "version_id": "44444444-4444-4444-8444-444444444444",
    "story_id": "33333333-3333-4333-8333-333333333333",
    "author_id": "7c0c1bfb-acf5-48ad-a3ba-4ea6617e05d8",
    "author_display_name": "Ada Lovelace",
    "author_avatar_url": null,
    "language": "eng",
    "body": "The first rain remembers every name.",
    "parent_comment_id": null,
    "created_at": "2026-10-08T18:26:37.134182+02:00",
    "updated_at": "2026-10-08T18:26:37.134182+02:00"
  }
}
```

`story_id` is the story the comment's version belongs to. A comment row does not store it,
so the server resolves it from the version. The response carries the same author attribution
as every other comment response — `author_display_name`, `author_avatar_url`, and
`author_rooted` — see [Author attribution](#author-attribution).

**Errors:** `404 not_found` (no such comment, or the id is not a UUID),
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
  "latitude": -33.9249,
  "longitude": 18.4241,
  "place_country": "South Africa",
  "duration_bucket": "lifelong",
  "is_public": true,
  "is_primary": true,
  "created_at": "2026-10-08T12:00:00Z",
  "updated_at": "2026-10-08T12:00:00Z"
}
```

`latitude`, `longitude`, and `place_country` are the signal's structured place data, or
`null`; the coordinate pair goes together (KNOT-ADR-034). `place` is the display name.

`duration_bucket` is one of `lifelong`, `many_years`, `several_years`, `a_few_years`, or
`recently`. A person has exactly one **primary** signal, so `is_primary` is always `true`
for a stored signal in this MVP: setting a signal replaces the previous one rather than
adding another.

### POST /users/me/rooted

Sets the authenticated user's primary Rooted signal and returns **200** with
`{ "signal": { ... } }`. A second call **replaces** the first.

**Request**

```json
{
  "place": "Cape Town",
  "latitude": -33.9249,
  "longitude": 18.4241,
  "place_country": "South Africa",
  "duration_bucket": "lifelong",
  "is_public": true
}
```

| Field             | Required | Rules                                                                    |
| ----------------- | -------- | ------------------------------------------------------------------------ |
| `place`           | yes      | 1-80 characters after trimming; a single line (no line breaks or tabs)    |
| `latitude`        | no       | Between -90 and 90. Must be sent with `longitude`                         |
| `longitude`       | no       | Between -180 and 180. Must be sent with `latitude`                        |
| `place_country`   | no       | At most 100 characters after trimming                                      |
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

The feed, story detail, version, and comment responses carry an `author_rooted` field:

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
| `GET /stories`, `GET /stories/{id}`                                                    | the story's author                     |
| `POST /stories/{id}/adapt`, `GET /stories/{id}/tree`, `GET /versions/{id}`             | each version's author                  |
| `POST /versions/{id}/comments`, `GET /versions/{id}/comments`, `GET /comments/{id}`     | each comment's author                  |

The feed attaches it to every story on the page in one batched read (KNOT-020c-fix4,
reversing the earlier decision to leave the feed unenriched: KNOT-015b). `POST /stories`
still does not populate the field — the client reads the story detail after creating it —
so it is present there but always `null`.

### Author attribution

Every response that names an author carries the author's display name and avatar, so a
client can attribute content without a request per row:

```json
{
  "author_id": "7c0c1bfb-acf5-48ad-a3ba-4ea6617e05d8",
  "author_display_name": "Ada Lovelace",
  "author_avatar_url": "/users/7c0c1bfb-acf5-48ad-a3ba-4ea6617e05d8/avatar?v=ada.png"
}
```

- `author_display_name` is the account's display name, or `""` when the account can no
  longer be resolved (for example, a deleted user).
- `author_avatar_url` is a path on this API (`/users/{id}/avatar`, never a link to object
  storage), or `null` when the author has no avatar. A client resolves it against the API
  base URL and renders the author's initials when it is `null`.

`author_id` alone is not enough for a client to render attribution: there is no public
"read a user" route. These fields close that gap. They are attached by **one batched
lookup per response** — every distinct author id in a response is resolved in a single
call — never one query per row, and never by widening an entity's stored columns
(KNOT-ADR-041).

| Response                                                                                          | Attribution describes            |
| ------------------------------------------------------------------------------------------------- | -------------------------------- |
| `POST /stories`, `GET /stories`, `GET /stories/{id}`                                              | the story's author                |
| `POST /stories/{id}/adapt`, `GET /stories/{id}/tree`, `GET /versions/{id}`                         | each version's author             |
| `POST /versions/{id}/comments`, `GET /versions/{id}/comments`, `GET /comments/{id}`                | each comment's author             |

Attribution is supplementary: if the lookup fails, the response still returns, with
`author_display_name` empty and `author_avatar_url` `null`.

## Profiles

Every user has a public **wall**: one chronological stream of everything they have
authored — stories, adaptations, comments, and inquiries. It is how the app answers
"who is this person, and what have they made?" from any author mention.

| Route                     | Auth   | Purpose                                    |
| ------------------------- | ------ | ------------------------------------------ |
| `GET /users/{id}/profile` | public | One page of a user's public activity wall  |

### The profile object

```json
{
  "user": {
    "id": "7c0c1bfb-acf5-48ad-a3ba-4ea6617e05d8",
    "display_name": "Ada Lovelace",
    "avatar_url": "/users/7c0c1bfb-acf5-48ad-a3ba-4ea6617e05d8/avatar?v=ada.png",
    "rooted": { "place": "Cape Town", "duration_bucket": "lifelong" },
    "joined_at": "2026-10-07T18:26:37.134182+02:00"
  },
  "activities": [
    {
      "kind": "story",
      "id": "d6b53a2c-2e2f-4a4d-9b0f-3f6f4e0f1a2b",
      "created_at": "2026-10-09T18:26:37.134182+02:00",
      "payload": {
        "title": "The first rain",
        "pillar": "wonder",
        "language": "eng",
        "place": "Manguzi",
        "version_count": 3,
        "preview": "When the first rain came, the herd turned for home before anyone…",
        "preview_truncated": true,
        "media_preview_url": "/stories/d6b53a2c-2e2f-4a4d-9b0f-3f6f4e0f1a2b/media/9f1c/content",
        "first_media_type": "image",
        "first_media_width": 1080,
        "first_media_height": 1920
      }
    }
  ],
  "next_cursor": ""
}
```

`avatar_url` is a path on this API (`/users/{id}/avatar`), or `null` when the user
has no avatar. `rooted` is the same inline summary content responses carry (see
[`author_rooted` on content responses](#author_rooted-on-content-responses)), or
`null`.

### GET /users/{id}/profile

Returns **200** with the user's public identity header and one page of their
activity, newest first.

| Query    | Required | Rules                                                    |
| -------- | -------- | -------------------------------------------------------- |
| `cursor` | no       | A `next_cursor` from a previous page. Omit for the first  |
| `limit`  | no       | 1-50. Defaults to 20; a larger value is clamped to 50      |

`activities` is always an array, never `null`. `next_cursor` is the empty string on
the last page. Paging is keyset over `(created_at, id)`, exactly as the feed and a
thread are.

Each activity is discriminated by `kind`; `payload` carries the context for that
kind:

| `kind`           | `id` names     | `payload`                                                                                          |
| ---------------- | -------------- | -------------------------------------------------------------------------------------------------- |
| `story`          | the story      | `{ title, pillar, language, place, version_count, preview, preview_truncated, media_preview_url, first_media_type, first_media_width, first_media_height }`             |
| `version`        | the adaptation | `{ story_id, story_title, language, place, version_number, preview, preview_truncated, media_preview_url, first_media_type, first_media_width, first_media_height }`    |
| `comment`        | the comment    | `{ version_id, story_id, body_preview }`                                                             |
| `inquiry`        | the question   | `{ title, place, answer_count, preview, preview_truncated }`                                          |
| `inquiry_answer` | the answer     | `{ inquiry_id, inquiry_title, body_preview }`                                                        |

`place` is the content's coarse place, **omitted** (not `null`) when it names
none. `version_count` counts every version in the story's tree, its root
included, so it is always at least 1. `version_number` is the adaptation's ordinal
among its story's versions, counting the root as 1, so a story's first adaptation
is 2. `answer_count` counts a question's public answers. Every one of them is
resolved in the same single query that builds the page — never a query per
activity (KNOT-020g-fix2).

`preview` is the card's body excerpt: the first **150** characters of the story's
body, the adaptation's body, or the question's body, with a `…` appended when the
body is longer than that, and no ellipsis when it fits. It is **omitted** (not
`null`) only when the body is empty, and it is truncated in SQL — `left(body, 150)`
— so a page never ships a full body to draw a two-line excerpt (KNOT-020g-fix4).
`body_preview` on a comment or an answer is the separate 200-character excerpt
used by a thread, and is unchanged.

`preview_truncated` is `true` when `preview` is an excerpt rather than the whole
body. It is computed in the same SQL expression that cuts the excerpt
(`length(body) > 150`), so it can never disagree with the text, and it is what a
card's "read more" link keys off. It is **omitted** (not `false`) when false,
because of `omitempty` — a client reads an absent flag as "no more to read", and
falls back to the excerpt's own trailing `…` when the field is missing altogether
from an older server (KNOT-020g-fix12).

`media_preview_url` is the path to the story's first media item's content and
`first_media_type` is that item's kind (`image` or `video`); both are **omitted**
when the story carries no media, so a client draws a preview only when it has one.
A version has no media of its own, so it carries its story's. Unlike the fields
above, the preview is attached from one batched media read per page rather than
being projected in SQL (KNOT-020g-fix3).

`first_media_width` and `first_media_height` are that item's pixel dimensions, so
a card frames the media at its natural ratio rather than a fixed one. They are
**omitted** when the media carries no dimensions or when the activity has no media
at all — note the difference from `GET /stories`, where the pair is `null` — and a
client treats an absent pair exactly as it treats `null`: fall back to 16:9
(KNOT-020g-fix5).

Every activity is authored by the wall's owner, so there is **no per-activity
author**: the name and avatar are on the `user` header. `body_preview` is the first
200 characters of a comment.

One act is one activity: a story's **root version** is not listed again as a
`version`. See KNOT-ADR-042.

**Errors:** `400 validation_error` (an unreadable `cursor`, or a `limit` that is not
a positive integer), `404 not_found` (no such user, or the id is not a UUID),
`500 internal_error`.

## Discovery

Discovery answers "where are stories told?" It groups stories into **place clusters** — one
entry per place, with how many stories are there, which pillars they belong to, which
languages they are told in, and when the newest one was published — and lists the stories at a
single place, with the same keyset pagination the feed uses.

A place is identified by the normalised `lower(trim(...))` form of its name together with the
country the geocoder reported (KNOT-ADR-020, KNOT-020e-fix). Two stories about "Beijing" are one
cluster however far apart the coordinates the authors chose from the geocoding picker
(KNOT-ADR-034, KNOT-ADR-035), and a story with no country is its own cluster rather than merging
with a same-named place that has one. The cluster's coordinate is the **average** of its members'
points; the map client plots that point directly, so a small town appears even though it is not
in the client's fallback lookup table.

| Route                           | Auth   | Purpose                                    |
| ------------------------------- | ------ | ------------------------------------------ |
| `GET /discovery/clusters`       | public | Places with stories, most stories first     |
| `GET /discovery/places/{place}` | public | One page of the stories at a place, newest first |

### The cluster object

```json
{
  "place": "Manguzi",
  "place_country": "South Africa",
  "latitude": -26.9998,
  "longitude": 32.7489,
  "story_count": 42,
  "pillar_counts": { "wonder": 30, "heritage": 12 },
  "languages": ["afr", "eng", "xho"],
  "latest_story_at": "2026-10-08T12:00:00Z"
}
```

`latitude` and `longitude` are the **average** of the cluster's member coordinates, or `null`
together for a **legacy cluster** — one whose stories carry no coordinate (created before
migration `0009`) — in which case the client falls back to its own lookup table. `place_country`
is the country the geocoder reported, or `null` when the stories name none. The cluster is keyed
by `(place, place_country)`: the place name normalised to lower case, and the country treated as
a distinct bucket when absent. Two stories at the same place with slightly different coordinates
are therefore one cluster, not two; `place` keeps the earliest member's original spelling.

`pillar_counts` always carries **both** supported pillars, so a place with only wonder stories
still has `"heritage": 0`. `languages` is the distinct, sorted set of the stories' **root
version** languages — always an array, never `null`. `place` is an author's original spelling.
The cluster query orders by `story_count` descending; a language *filter* matches any version of
a story, while the aggregated `languages` list comes from root versions only (KNOT-ADR-020).

### GET /discovery/clusters

Returns **200** with `{ "clusters": [ ... ] }`, ordered by `story_count` descending.

| Query      | Required | Rules                                                           |
| ---------- | -------- | --------------------------------------------------------------- |
| `pillar`   | no       | `wonder` or `heritage`; keeps only places with a story under it  |
| `language` | no       | A canonical ISO 639-3 code; keeps only places with a version in that language |
| `limit`    | no       | 1-500. Defaults to 100; a larger value is clamped to 500         |

`clusters` is always an array, never `null`.

**Errors:** `400 validation_error` (an unknown `pillar`, a malformed `language`, or a `limit`
that is not a positive integer), `500 internal_error`.

### GET /discovery/places/{place}

Returns **200** with one page of the stories at a place, newest first:

```json
{ "stories": [ { "id": "...", "title": "..." } ], "next_cursor": "MjAyNi0xMC0wOFQxMjowMDowMFo..." }
```

The `{place}` path segment is URL-encoded by the client and decoded by the server, and the
match is case-insensitive: `GET /discovery/places/Cape%20Town` and
`/discovery/places/cape%20town` are the same place.

Each story is the [story object](#the-story-object), carrying the **same enrichment the feed
carries** (KNOT-020e-fix2): `author_display_name` and `author_avatar_url` from one batched
identity read, `author_rooted`, the single-item `media` preview with `media_count` /
`first_media_type` / `first_media_url`, the `version_count` / `language_count` / `comment_count`
aggregates, and the `reactions` / `my_reactions` pair. A story read here and the same story read
from `GET /stories` carry identical fields with identical values.

| Query     | Required | Rules                                                           |
| --------- | -------- | --------------------------------------------------------------- |
| `country` | no       | Narrows the page to one country. The literal `null` selects the stories that record **no** country |
| `pillar`  | no       | `wonder` or `heritage`; keeps only stories filed under that lens. Anything else is a `400` |
| `cursor`  | no       | A `next_cursor` from a previous page. Omit for the first page  |
| `limit`   | no       | 1-50. Defaults to 20; a larger value is clamped to 50          |

`country` is how a country-scoped cluster is opened: a cluster is keyed by `(place, country)`
(KNOT-020e-fix), so passing its `place_country` back lists exactly the stories its `story_count`
counts. Omitting the parameter — the behaviour before KNOT-020e-fix2 — lists every story with that
place name, including the same-named places in other countries. The value is matched verbatim
against the stored `place_country`, because a client passes back exactly what a cluster reported.
The word `null` is **reserved** to mean "records no country", so a story whose `place_country` is
`null` is reachable only through it. An unrecognised country is not an error: it matches nothing
and returns an empty page, exactly as an unknown place does.

`pillar` is a **closed set**, not free text, so it behaves differently from `country`: an unknown
value is a `400 validation_error` rather than an empty page. Omitting it lists every pillar. It is
what the place screen's All / Heritage / Wonder tabs send, and it composes with `country` — the
filters are ANDed (KNOT-020e-fix6). The column is indexed (`stories_pillar_idx`, migration `0002`),
so the predicate is a plain indexed equality test.

Changing any filter changes the query, so clients must start paging again from the first page: a
`cursor` issued under one filter is not meaningful under another.

`stories` is always an array, never `null`, and `next_cursor` is always present. Paging is
keyset, exactly as for the feed. A place with no stories is **200** with `"stories": []`, not
a 404: it is a valid place with an empty result, not a missing resource.

**Errors:** `400 validation_error` (an unreadable `cursor`, or a `limit` that is not a
positive integer), `500 internal_error`.

## Notifications

The signed-in user's **in-app inbox**: one row per time another person acted on their
content. Every route below is protected, and every route is scoped to the caller by the
token — there is no user id in any request, and no way to read or mark another person's
inbox. Delivery is in-app only; there is no push channel (KNOT-ADR-039).

The content events exist, and each one fires only when the recipient and the actor are
different people:

| `event_type`       | Fires when                                             | `entity_type`           | `entity_id` names  |
| ------------------ | ------------------------------------------------------ | ----------------------- | ------------------ |
| `version.created`  | someone adapts a version the recipient authored         | `version`               | the new version     |
| `comment.created`  | someone comments on a version the recipient authored, **or replies to a comment the recipient authored** | `comment` | the new comment |
| `reaction.created` | someone leaves a perspective reaction on content the recipient authored | the target's own kind | the reacted-to entity |

For `comment.created`, the recipient is the **version's author** for a top-level comment and
the **replied-to comment's author** for a reply (KNOT-ADR-047). A reply therefore notifies
the person being answered even when a third person wrote the version, and the two cases
never notify the same person twice: only one notification is written per comment.

Acting on your own content never notifies you, and the database enforces the same rule
(KNOT-ADR-038). A `reaction.created` notification fires only when a signal is **created**
(toggling one off is silent) and only when the reactor is not the content's author; it
carries `reaction_type` (one of the four signals), and no other event does. Its
`entity_type` is the reacted-to content's own kind, so the client opens the same screen a tap
on the content would.

### The notification object

```json
{
  "id": "7f1d6b1e-6a0d-4a1f-9a1f-2a5b0d9c4e10",
  "event_type": "version.created",
  "entity_type": "version",
  "entity_id": "3d5c9f24-1b8e-4c7a-9f0b-6e2d8a1c4b77",
  "reaction_type": "",
  "read": false,
  "created_at": "2026-10-09T12:00:00Z",
  "actor": {
    "id": "22222222-2222-4222-8222-222222222222",
    "display_name": "Ada Lovelace",
    "avatar_url": "/users/22222222-2222-4222-8222-222222222222/avatar?v=ada.png",
    "author_rooted": { "place": "Cape Town", "duration_bucket": "lifelong" }
  }
}
```

`read` is derived from the stored `read_at`; the timestamp itself is not part of the
contract, because the client only draws an unread marker. `reaction_type` names the signal a
`reaction.created` event carries (`"rings_true"`, `"know_it_differently"`,
`"adds_something_new"`, or `"needs_a_source"`) and is `""` for every other event. `actor` is
`null` when the acting account can no longer be resolved, so a row never disappears just
because its actor was deleted. `avatar_url` is a **path on this API** (`""` when the actor has no avatar), never a
link to object storage, and `author_rooted` is the same
[inline Rooted summary](#author_rooted-on-content-responses) that content responses carry.

### GET /notifications

Returns **200** with one page of the caller's notifications, newest first:

```json
{ "notifications": [ { "id": "..." } ], "next_cursor": "MjAyNi0xMC0wOVQxMjowMDowMFo..." }
```

| Query    | Required | Rules                                                       |
| -------- | -------- | ----------------------------------------------------------- |
| `cursor` | no       | A `next_cursor` from a previous page. Omit for the first page |
| `limit`  | no       | 1-50. Defaults to 20; a larger value is clamped to 50         |

`notifications` is always an array, never `null`, and `next_cursor` is `""` on the last page.
Paging is keyset on `(created_at, id)`, exactly as the feed and a thread are. An empty inbox
is **200** with `"notifications": []`.

**Errors:** `400 validation_error` (an unreadable `cursor`, or a `limit` that is not a
positive integer), `401 unauthorized`, `500 internal_error`.

### GET /notifications/unread_count

Returns **200** with `{ "count": 3 }`. It is a route of its own so the feed's bell can show a
number without downloading a page of notifications.

**Errors:** `401 unauthorized`, `500 internal_error`.

### POST /notifications/{id}/read

Marks one notification read. Returns **204** with no body — the caller asked for a state, not
for a representation. Marking an already-read notification is not an error.

A notification that does not exist **or that belongs to another user** is `404
not_found`: the same answer for both, so the route cannot be used to probe whether an id
exists.

**Errors:** `401 unauthorized`, `404 not_found`, `500 internal_error`.

### POST /notifications/read_all

Marks every unread notification for the caller read. Returns **200** with
`{ "updated": 3 }`, which is how many rows the write actually changed — so a client can tell
"nothing was unread" from "the call did not land".

**Errors:** `401 unauthorized`, `500 internal_error`.

**Where notifications come from:** a notification is a **non-critical side effect** of the
write that caused it. Adapting a version, commenting, or bridging stores the notification
after the primary row is committed, and a failure to store it is logged and swallowed rather
than failing the request — the adaptation or comment must not be lost because an inbox write
failed (KNOT-ADR-038).

## Reactions

The four **perspective signals** a reader can leave on a story, a version, a comment, or an
inquiry (KNOT-ADR-050; comments are read-only, KNOT-ADR-053). They are not likes: the four do not
compete, a reader may hold any combination of them on the same entity, and none of them cancels
another. Nothing sorts or ranks content by them.

| `reaction_type`        | Meaning                     | Emoji |
| ---------------------- | --------------------------- | ----- |
| `rings_true`           | "Rings true to me"          | ✅     |
| `know_it_differently`  | "I know it differently"     | 🔄     |
| `adds_something_new`   | "Adds something new"        | ➕     |
| `needs_a_source`       | "Needs a source"            | 📎     |

Every content response carries two reaction fields (KNOT-ADR-051):

```json
"reactions": { "rings_true": 3, "know_it_differently": 1, "adds_something_new": 0, "needs_a_source": 2 },
"my_reactions": ["rings_true", "adds_something_new"]
```

`reactions` is the count of each signal, always all four keys. `my_reactions` is the signals
the **authenticated caller** holds, as an array of type strings — `[]` when the caller is
signed out or holds none. These fields are attached to the story detail and feed, the version
detail and tree, the comment list and detail, and the inquiry list and detail.

### POST /{entity}/{id}/reactions

Toggles one signal on one entity, as the authenticated user. **Protected.** The request body
is `{ "reaction_type": "rings_true" }`. Toggling a signal the caller already holds removes
it; otherwise it is added, so a caller never has to know the current state.

Returns **200** with the entity's updated counts:

```json
{ "reactions": { "rings_true": 1, "know_it_differently": 0, "adds_something_new": 0, "needs_a_source": 0 } }
```

- `POST /stories/{id}/reactions`
- `POST /versions/{id}/reactions`

**Not supported on comments.** `POST /comments/{id}/reactions` always answers **400**
`validation_error` with `"reactions are not supported on comments"`, and writes nothing
(KNOT-ADR-053). The route stays registered so the answer is an explicit 400 rather than a 404,
which tells a client the operation is not supported instead of that the route does not exist. A
comment's existing reaction rows are kept — it is not a migration — and the GET below still
returns them.

Errors: **400** `validation_error` for an unknown `reaction_type`, or for any comment (see
above); **401** `unauthorized`; **404** `not_found` when the entity does not exist.

### GET /{entity}/{id}/reactions

Returns **200** with every reaction on the entity, newest first, each reactor resolved.
**Public.** An id that names nothing returns an empty array rather than a 404.

```json
{
  "reactions": [
    {
      "id": "9c1e4a77-2f3b-4d5e-8a6b-1c2d3e4f5a6b",
      "user": { "id": "22222222-2222-4222-8222-222222222222", "display_name": "Ada Lovelace", "avatar_url": "/users/22222222-2222-4222-8222-222222222222/avatar?v=ada.png" },
      "reaction_type": "rings_true",
      "created_at": "2026-10-10T09:00:00Z"
    }
  ]
}
```

- `GET /stories/{id}/reactions`
- `GET /versions/{id}/reactions`
- `GET /comments/{id}/reactions`

`GET /comments/{id}/reactions` remains available even though a comment reaction can no longer
be created: it is a read-only view of the rows that already exist, which is useful for an audit
and harmless to serve (KNOT-ADR-053).

## Curious Inquiries

A question someone asks about a place, and the public answers to it.

Three rules shape every response in this section (KNOT-ADR-055):

- **everything is public and attributed.** There is no anonymous inquiry and no anonymous
  answer, so an inquiry always names its asker and an answer always names its answerer.
- **an inquiry stays open forever.** There is no accepted answer, no closing, and no ranking,
  because knowledge of a place is plural and an early answer is not the final word.
- **nothing is editable or deletable.** There is no update or delete route in this section.

Naming a place is what makes an inquiry more than a post: it routes the question to the first
few people Rooted in that place (KNOT-ADR-056). Leaving the place out is allowed; the question
is still public, it is simply not announced to anyone.

| Method | Path                      | Auth      |
| ------ | ------------------------- | --------- |
| POST   | `/inquiries`              | Protected |
| GET    | `/inquiries`              | Public (optional auth) |
| GET    | `/inquiries/{id}`         | Public (optional auth) |
| POST   | `/inquiries/{id}/answers` | Protected |
| GET    | `/inquiries/{id}/answers` | Public (optional auth) |
| GET    | `/answers/{id}`           | Public (optional auth) |

The two public reads use optional authentication: a request carrying a valid access token also
receives the caller's own reaction highlights, and a request with no token (or a bad one) is
served anonymously rather than refused (KNOT-ADR-051).

### The inquiry object

```json
{
  "id": "d3f1c8a2-5b64-4e19-9f0a-7c2b8e4d1a35",
  "author_id": "22222222-2222-4222-8222-222222222222",
  "title": "Why do the cattle come home at the same hour?",
  "body": "Every evening, without anyone calling them.",
  "language": "eng",
  "place": "Manguzi",
  "place_country": "South Africa",
  "latitude": -26.9998,
  "longitude": 32.7489,
  "answer_count": 3,
  "created_at": "2026-10-10T09:00:00Z",
  "updated_at": "2026-10-10T09:00:00Z",
  "author_display_name": "Ada Lovelace",
  "author_avatar_url": "/users/22222222-2222-4222-8222-222222222222/avatar?v=ada.png",
  "author_rooted": { "place": "Manguzi", "duration_bucket": "lifelong" },
  "reactions": {
    "rings_true": 2,
    "know_it_differently": 0,
    "adds_something_new": 1,
    "needs_a_source": 0
  },
  "my_reactions": ["rings_true"]
}
```

- `place` is null when the question names no place, and `place_country`, `latitude`, and
  `longitude` are null together when the location picker was not used (KNOT-ADR-034).
- `answer_count` is maintained by the server inside the same transaction that inserts an
  answer, so it is never stale for a committed answer.
- `author_rooted` and the attribution fields follow every other content response
  (KNOT-ADR-017, KNOT-ADR-041). `my_reactions` is `[]` for an anonymous reader.
- `reactions` is present because an inquiry is content someone authored and others weigh in on;
  `adds_something_new` and `needs_a_source` are how someone comments on a question without
  answering it (KNOT-ADR-057).

### The answer object

```json
{
  "id": "8b2a4f31-6c07-4a5d-b1e9-3f8c2d7a6b40",
  "inquiry_id": "d3f1c8a2-5b64-4e19-9f0a-7c2b8e4d1a35",
  "author_id": "55555555-5555-4555-8555-555555555555",
  "language": "eng",
  "body": "They follow the river, and the river has a tide.",
  "created_at": "2026-10-10T11:30:00Z",
  "updated_at": "2026-10-10T11:30:00Z",
  "author_display_name": "Grace Hopper",
  "author_avatar_url": null,
  "author_rooted": null
}
```

An answer carries **no** `reactions`: an answer is a reply, and replies carry no reactions at
MVP (KNOT-ADR-052). It is the one difference from the inquiry object's shape.

### POST /inquiries

Asks a question. **Protected.** Returns **201**.

The asker is taken from the access token, never the body — there is no `author_id` field and no
anonymity flag, so a client cannot ask as somebody else.

```json
{
  "title": "Why do the cattle come home at the same hour?",
  "body": "Every evening, without anyone calling them.",
  "language": "eng",
  "place": "Manguzi",
  "latitude": -26.9998,
  "longitude": 32.7489,
  "place_country": "South Africa"
}
```

| Field | Required | Rule |
| --- | --- | --- |
| `title` | Yes | 1–200 characters after trimming |
| `body` | Yes | 1–5,000 characters after trimming |
| `language` | Yes | A valid ISO 639-3 code (KNOT-ADR-046) |
| `place` | No | 1–100 characters after trimming, on one line with no tabs |
| `latitude` / `longitude` | No | Supplied together or not at all; −90..90 and −180..180 |
| `place_country` | No | At most 100 characters after trimming |

A bad field is **400** `validation_error`, rendered as `"<field> <message>"`.

**Routing.** When `place` is present, the question is announced to the first 5 users whose
primary **public** Rooted signal is for that place, earliest declarer first
(`inquiry.nearby`). The asker is never notified of their own question. Routing happens after
the inquiry is committed and never fails the request: a Rooted lookup that is unavailable, or a
notification that cannot be written, is logged and swallowed. A place-less inquiry is stored and
routed nowhere.

### GET /inquiries

One page of open inquiries, **newest first**. **Public.**

| Query | Default | Notes |
| --- | --- | --- |
| `place` | absent | Exact match on the stored spelling. Absent means every place, including questions that name none. |
| `limit` | 20 | Clamped to 50. Not a positive integer is a 400. |
| `cursor` | absent | The `next_cursor` from the previous page. |

```json
{ "inquiries": [ /* inquiry objects */ ], "next_cursor": "MjAyNi0xMC0xMFQwOTowMDowMFo|d3f1c8a2-…" }
```

`next_cursor` is `""` on the last page.

### GET /inquiries/{id}

One inquiry. **Public.** An unknown id is **404** `not_found`.

### POST /inquiries/{id}/answers

Answers a question. **Protected.** Returns **201**.

```json
{ "body": "They follow the river.", "language": "eng" }
```

`body` is 1–5,000 characters after trimming and `language` must be a valid ISO 639-3 code.

The answerer is taken from the access token. The answer is committed with the inquiry's
`answer_count` increment in one transaction, under a row lock on the inquiry, so two answers
arriving at once cannot write the same count. The asker is then notified with
`inquiry.answered`; answering your own question never notifies you (KNOT-ADR-038).

An unknown inquiry is **404**; an unknown author is **404**.

### GET /inquiries/{id}/answers

One page of a question's answers, **oldest first**, so the thread reads as a conversation.
**Public.** `limit` defaults to 50 and is clamped to 100; `cursor` behaves as above.

An unknown inquiry is **404** rather than an empty thread, so a client can tell "no such
question" from "nobody has answered yet".

```json
{ "answers": [ /* answer objects */ ], "next_cursor": "" }
```

### GET /answers/{id}

One answer. **Public.** The top-level path matches how a single comment (`GET /comments/{id}`)
is already fetched. An unknown id is **404**.

### Reactions on an inquiry

`POST /inquiries/{id}/reactions` and `GET /inquiries/{id}/reactions` follow the per-entity
reaction routes exactly (`POST /{entity}/{id}/reactions`), with a body of
`{"reaction_type": "rings_true"}`. Reactions are **not** supported on answers.

### Notification events

Two events are added to the inbox by this feature. Both name an inquiry, so a tap opens the
question in either case.

| Event | Recipient | Fires when |
| --- | --- | --- |
| `inquiry.answered` | The asker | Someone else answers their question |
| `inquiry.nearby` | A user Rooted in the named place | Someone asks a question about that place |

`inquiry.nearby` is the only event a recipient can receive about content they have no part in:
it fires because of where they are from, not because of anything they authored. At most 5
recipients are notified per inquiry; everyone else reaches the question through the public list
(KNOT-ADR-056).

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

## Moderation (KNOT-017a)

Reports and blocks: the collection half of the moderation system. The moderator
queue and its actions (hide/warn/suspend) ship in KNOT-017b, so there is no
moderator route here.

### POST /reports

Protected. Body: `{ "entity_type", "entity_id", "category", "reason"? }`.
`entity_type` is one of `story | version | comment | inquiry |
inquiry_answer`. `category` is one of `harassment | hate_speech | misinformation |
spam | sensitive_content | other`; `reason` is **required** when the category is
`other` and optional otherwise. Returns **201** with the report object. A repeat by
the same user on the same entity is **409**; an entity that does not exist is
**404**. Reports are private to the reporter and the moderator queue.

### GET /reports/mine

Protected, cursor-paginated (`cursor`, `limit`). Returns
`{ "reports": [ ... ], "next_cursor": "" }`, newest first.

### POST /blocks/{user_id} · DELETE /blocks/{user_id}

Protected. Both are idempotent and answer **204**. Blocking yourself is **400**.

### GET /blocks/mine

Protected, cursor-paginated. Returns
`{ "blocks": [ { "user": { "id", "display_name", "avatar_url", "role" },
"created_at" } ], "next_cursor": "" }`.

### Block-based filtering and write prevention

Blocking is **mutual hiding plus write prevention** (KNOT-ADR-060). The server
resolves a viewer's block set once per authenticated request and filters it out of
every content list: the feed, a version's comments and their replies, a comment's
thread, the inquiry list and its answers, and a user's profile wall (which answers
**404** when its owner is on either side of a block). Anonymous readers carry no
block set and see everything.

A write that targets content authored by either side of a block is refused with
**403** and the machine code **`blocked`**:
`{"error":{"code":"blocked","message":"you cannot interact with this content"}}`.
This covers commenting, reacting, answering an inquiry, and adapting a
story.

## Search (KNOT-018)

`GET /search` — full-text search across stories, versions, and inquiries in **one** ranked list.
Public with optional auth: a signed-in reader's blocked authors are hidden (KNOT-ADR-064), an
anonymous reader sees everything.

### Query parameters

| Name | Type | Notes |
| --- | --- | --- |
| `q` | string | The phrase to match. At least 2 characters when present. Optional **only if** another filter is given. |
| `type` | `story` \| `version` \| `inquiry` | Restrict to one corpus. Omit for all three. |
| `pillar` | `wonder` \| `heritage` | Restrict to one pillar. **Excludes inquiries** — they have no pillar. |
| `language` | ISO 639-3 | e.g. `eng`, validated against the canonical list. |
| `author_id` | uuid | Restrict to one author. |
| `place` | string | Exact match on the stored spelling (a story's `approximate_location`, an inquiry's `place`). |
| `cursor` | opaque | A `next_cursor` from a previous page. |
| `limit` | integer | Default 20, maximum 50. A larger value is clamped, not refused. |

Matching is word-based and case-insensitive. The phrase is reduced to letters and digits and each
term is matched as a **prefix**, so `mang` finds `Manguzi` and `MANGUZI` finds it too. There is no
stemming and no fuzzy matching.

### Response

200:

```json
{
  "results": [
    {
      "kind": "story",
      "id": "0b1c…",
      "story_id": "0b1c…",
      "title": "Cattle come home at dusk",
      "preview": "The herd walks back from the river as the light goes…",
      "language": "eng",
      "pillar": "wonder",
      "place": "Manguzi",
      "author_id": "7f30…",
      "author_display_name": "Aisha Nkosi",
      "author_avatar_url": "/users/7f30…/avatar",
      "author_rooted": { "place": "Manguzi", "duration_bucket": "years" },
      "created_at": "2026-10-01T09:00:00Z"
    }
  ],
  "next_cursor": ""
}
```

- `kind` is `story`, `version`, or `inquiry`.
- `id` is the story id, the version id, or the inquiry id — whatever a client opens.
- `story_id` is the story a version belongs to. It equals `id` for a story and is `null` for an
  inquiry.
- `pillar` is `null` for an inquiry. `place` is `null` when no place was recorded.
- `preview` is the body truncated to 200 characters. Matched terms are **not** highlighted.
- `author_display_name`, `author_avatar_url`, and `author_rooted` are the same inline attribution
  every other content response carries, filled by one batched lookup per response.
- `next_cursor` is `""` on the last page.

Ordering is `rank DESC, created_at DESC, id DESC`, so the best match is first and equal ranks fall
back to newest. Pages are walked with `next_cursor` exactly as the feed is walked.

### Errors

| Status | Code | When |
| --- | --- | --- |
| 400 | `validation_error` | A phrase shorter than 2 characters or longer than 200, an unknown `type` / `pillar` / `language`, a malformed `author_id` or `cursor`, a `place` over 100 characters, a `limit` below 1, or **no phrase and no filter at all**. |
| 500 | `internal_error` | The search could not be served. |

A request with neither a phrase nor a filter is refused rather than answered with the whole corpus,
and a 1-character phrase is refused rather than served — a client should not send one.
