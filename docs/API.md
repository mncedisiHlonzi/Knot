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
| 404    | `not_found`         | The requested story does not exist, or its id is not a UUID |
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

`POST /stories` is a protected route. Every other route is public and needs no
credentials.

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
