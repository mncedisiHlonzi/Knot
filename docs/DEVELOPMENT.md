# Knot — Development

How to set up and run the Knot foundation locally.

## Prerequisites

| Tool           | Local machine      | CI      | Notes                                           |
| -------------- | ------------------ | ------- | ----------------------------------------------- |
| Node.js        | 24                 | 20      | Mobile tooling and package manager              |
| npm            | Bundled with Node.js | (from Node.js) | The only package manager used          |
| Go             | 1.27               | 1.22    | Backend; `backend/go/go.mod` declares `go 1.22`  |
| Docker Desktop | Any recent version | n/a     | Local PostgreSQL + Redis + MinIO only (Docker Compose) |
| Git            | Any recent version | n/a     | Version control                                 |
| actionlint     | Optional           | 1.7.7   | Lints `.github/workflows/`; pinned in CI        |

Docker is required **only** for local PostgreSQL, Redis, and MinIO. The mobile app and the Go
backend always run on the host — there is deliberately no application container.

### Version drift — read this before trusting the pins

Local tool versions and CI tool versions are **not** identical today:

- **Node:** local development runs **Node 24**; CI pins **Node 20**.
- **Go:** local development runs **Go 1.27**; `backend/go/go.mod` declares `go 1.23` and
  CI installs **Go 1.23**. The directive moved from 1.22 to 1.23 with KNOT-031a because the
  Resend Go SDK declares `go 1.23` in its own `go.mod`; `go get` raised ours automatically.

This drift is accepted for the foundation phase, but it means a green local run is **not**
proof that CI will be green.

> **The first CI run is the true test of the pins.** If a pin is wrong, CI is what fails —
> correct the pin rather than lowering the local toolchain.

## Repository layout

```
knot/
├── apps/mobile/            React Native + TypeScript app
├── backend/go/             Go modular monolith
├── docs/                   Documentation
├── infrastructure/docker/  Local Docker Compose definitions (Postgres + Redis + MinIO)
├── scripts/                Developer scripts (dev-up / dev-down / dev-reset)
├── tools/                  Generators (generate_languages.py regenerates the language list)
└── ...                     Placeholder directories (see README.md)
```

## Local infrastructure (PostgreSQL + Redis + MinIO)

Local PostgreSQL, Redis, and MinIO run in Docker via
`infrastructure/docker/docker-compose.yml`. That file contains **no application
service** — the mobile app and the Go backend run on the host and reach the containers
over `localhost`.

- Images are pinned to specific minor versions (`postgres:16.4-alpine`,
  `redis:7.4.0-alpine`, and a pinned MinIO tag — see below). `latest` is never used.
- Ports bind to `127.0.0.1` only, so nothing is exposed to your network.
- **PostgreSQL is published on host port `5433`, not the default `5432`.** This avoids a
  clash with any Postgres already installed on the developer's machine. Only the *host*
  mapping changed — the container's internal port is still `5432`. Redis stays on `6379`,
  and MinIO stays on `9000`/`9001`.
  Override the host port with `KNOT_POSTGRES_PORT` if `5433` is also taken.
- Data lives in the named volumes `knot_postgres_data`, `knot_redis_data`, and
  `knot_minio_data`.
- The CI "Services smoke" job uses the **same pinned versions**, so local and CI do not
  drift apart.

### Developer scripts

| Script                 | What it does                                                       |
| ---------------------- | ------------------------------------------------------------------ |
| `scripts/dev-up.sh`    | Start PostgreSQL + Redis + MinIO. Idempotent — safe to re-run.      |
| `scripts/dev-down.sh`  | Stop them. **Named volumes are preserved**, so data survives.       |
| `scripts/dev-reset.sh` | Stop them and **delete** the named volumes. Destructive.            |

```bash
scripts/dev-up.sh             # start Postgres + Redis + MinIO
scripts/dev-down.sh           # stop, keep data
scripts/dev-reset.sh          # wipe all local data — prompts for confirmation
scripts/dev-reset.sh --yes    # wipe all local data without a prompt
```

`dev-reset.sh` is destructive: it requires the literal word `yes`, or the `--yes` flag.
All three scripts drive Docker Compose only — they never touch mobile or Go source, and
they exit with a clear message if the Docker CLI is unavailable.

To drive Compose directly instead:

```bash
docker compose -f infrastructure/docker/docker-compose.yml --env-file .env up -d
docker compose -f infrastructure/docker/docker-compose.yml --env-file .env down
docker compose -f infrastructure/docker/docker-compose.yml config   # syntax check only
```

### MinIO (media storage)

MinIO is the local object store, and it is where avatars live. The bucket is private and
no client ever talks to MinIO directly: the Go backend is the only party that reads or
writes objects (KNOT-ADR-028, KNOT-ADR-029).

| What                | Where                                                              |
| ------------------- | ------------------------------------------------------------------ |
| S3 API              | `http://127.0.0.1:9000`                                            |
| Web console         | `http://127.0.0.1:9001`                                            |
| Bucket              | `knot-media` (`KNOT_S3_BUCKET`)                                     |
| Object layout       | Avatars: `avatars/<user id>/<uuid>.{jpg,png,webp}`. Story media: `story-media/<story id>/<uuid>.{jpg,png,webp,mp4,mov}`, with the renditions beside it as `<uuid>_thumb.jpg`, `<uuid>_medium.jpg` and `<uuid>_poster.jpg` (KNOT-037) |
| Data directory      | `/bitnami/minio/data` in the container, on the `knot_minio_data` named volume |
| Credentials         | `KNOT_S3_ACCESS_KEY` / `KNOT_S3_SECRET_KEY`, which are the same values the container is given as `KNOT_MINIO_ROOT_USER` / `KNOT_MINIO_ROOT_PASSWORD` |
| Health              | `curl -f http://127.0.0.1:9000/minio/health/live`                   |

The data directory is **image-specific**, and mounting the wrong one fails quietly: the
bucket is still created and uploads still succeed, but objects land in the container's
writable layer and disappear the next time the container is recreated. This image runs
`minio server ... /bitnami/minio/data`, so that is where the volume is mounted; upstream's
`minio/minio` uses `/data`. The compose file comments on this at the mount.

Open the console in a browser and sign in with the `.env` values (or the compose
defaults), or use the bundled `mc` client. `mc` is **not on `PATH`**, so call it by full
path:

```bash
# What is in the bucket?
docker compose -f infrastructure/docker/docker-compose.yml exec minio \
  /opt/bitnami/minio-client/bin/mc ls --recursive local/knot-media/

# Upload an avatar the way the API does, then read it back
curl -X POST http://localhost:8080/users/me/avatar \
  -H "Authorization: Bearer $KNOT_ACCESS_TOKEN" \
  -F file=@me.png

# Prove that an unknown user is a plain 404, not a store error
curl -i http://localhost:8080/users/00000000-0000-4000-8000-000000000000/avatar
```

Story media (KNOT-013) lands under the `story-media/` prefix, one folder per story. In the
console (`http://127.0.0.1:9001`, sign in with the `.env` values) open the `knot-media`
bucket and browse that prefix; with `mc`:

```bash
# Every story-media object, newest first
docker compose -f infrastructure/docker/docker-compose.yml exec minio \
  /opt/bitnami/minio-client/bin/mc ls --recursive local/knot-media/story-media/

# Attach a small image to a story, then stream it back
curl -X POST http://localhost:8080/stories/$KNOT_STORY_ID/media \
  -H "Authorization: Bearer $KNOT_ACCESS_TOKEN" \
  -F file=@photo.png -F source=camera

# Range request: expect 206 with a Content-Range header
curl -i -H 'Range: bytes=0-99' \
  http://localhost:8080/stories/$KNOT_STORY_ID/media/$KNOT_MEDIA_ID/content
```

**A story holds at most 10 media items** (KNOT-ADR-054, `storymedia.MaxMediaPerStory`). The cap
is enforced in the insert transaction, which locks the `stories` row and then counts
`story_media`, so two uploads arriving together cannot both take the last slot; an eleventh
upload is a `400 validation_error` carrying `storymedia.MediaLimitMessage`. There is no
database constraint — a CHECK cannot count rows. The mobile `CreateStoryScreen` mirrors the same
number through `src/utils/mediaLimit.ts` (`MAX_MEDIA_PER_STORY`), shows an `N / 10` counter,
disables **+ Add media** at the cap, and passes the free slots to `MediaPickerSheet` as
`maxSelection` (`selectionLimit` of 0 means "unlimited" in `react-native-image-picker`, so the
sheet refuses to launch at 0 rather than allowing an unbounded pick).

Deleting a media row does **not** remove the object from the bucket in the console view
until the `DELETE` endpoint is called; a row and its object are removed together by
`DELETE /stories/{id}/media/{mid}`, which also removes that item's renditions.

#### Media renditions (KNOT-037)

Every image upload is also rendered as a feed thumbnail (JPEG, max 400px wide, quality 80)
and a story-detail rendition (JPEG, max 1200px wide, quality 85); every video upload gets a
poster frame (JPEG, max 400px wide) taken a fifth of the way in. The objects are named
`<uuid>_thumb.jpg`, `<uuid>_medium.jpg` and `<uuid>_poster.jpg` inside the same
`story-media/<story id>/` folder, and the row records their keys in `thumbnail_key`,
`medium_key` and `poster_key` (migration `0024`).

Renditions are served by the same content endpoint with `?variant=thumb|medium|poster`:

```bash
# The thumbnail, the medium rendition, and the fallback behaviour
curl -i "http://localhost:8080/stories/$KNOT_STORY_ID/media/$KNOT_MEDIA_ID/content?variant=thumb"
curl -i "http://localhost:8080/stories/$KNOT_STORY_ID/media/$KNOT_MEDIA_ID/content?variant=medium"
# A poster on an image has none, so this answers with the ORIGINAL (200, image/png)
curl -i "http://localhost:8080/stories/$KNOT_STORY_ID/media/$KNOT_MEDIA_ID/content?variant=poster"
```

A failed rendition is a `WARN` in the log and nothing else: the original is already stored
and every reader falls back to it, so a missing decoder or a missing FFmpeg never fails an
upload.

**FFmpeg is required for video poster frames**, and only for those. Image renditions are
pure Go and need nothing installed:

```bash
# macOS
brew install ffmpeg
# Debian/Ubuntu
sudo apt-get install ffmpeg
# Verify
ffmpeg -version
```

The backend calls `ffmpeg` from `PATH` only when a video is uploaded; if it is missing, the
upload still succeeds with a `WARN` ("could not generate a video poster frame: … ffmpeg is
not installed") and the item simply has no `poster_url`. A production image that wants video
posters must install FFmpeg in the container. FFmpeg is **not** a Go dependency and is not
vendored: it is a system prerequisite, like Docker.

#### Cloudflare R2 (production storage)

Both local and production speak the S3 API through `internal/storage`, so moving to R2 is
configuration only — no code, no bucket-management calls (KNOT-ADR-091). `KNOT_STORAGE_PROVIDER`
names which store the settings point at (`minio`, the default, or `r2`); the backend logs an
INFO line at startup when it is `r2`.

```bash
KNOT_STORAGE_PROVIDER=r2
KNOT_S3_ENDPOINT=https://<account-id>.r2.cloudflarestorage.com
KNOT_S3_REGION=auto
KNOT_S3_ACCESS_KEY=<r2-access-key-id>
KNOT_S3_SECRET_KEY=<r2-secret-access-key>
KNOT_S3_BUCKET=knot-media
```

The bucket must already exist, and the credentials are an R2 API token's key id and secret.
R2's region is `auto`; MinIO ignores the value, so one default serves both.

#### Large uploads use the multipart uploader

`storage.Put` goes through the transfer manager from
`github.com/aws/aws-sdk-go-v2/feature/s3/transfermanager`, not a bare `PutObject`
(KNOT-037-fix2, KNOT-037-fix3). Below the part size it issues exactly one `PutObject`, so a
thumbnail or a poster behaves as it always did; above it the body is split into parts sent
with bounded concurrency, and a failed part is retried with the SDK's exponential backoff
instead of the whole file being restarted.

The tuning is three numbers in `internal/storage/s3.go` — `uploadPartSize` (8 MiB),
`uploadConcurrency` (3) and, set to the part size, `MultipartUploadThreshold` — and the values
were **measured on a slow link, not copied from the package's defaults**. The 5 MiB / five
parts default was slower on the same connection: parallel parts only split a saturated uplink
into smaller pieces. What the uploader buys is not throughput, it is that a large upload no
longer has to survive as one long-lived request.

Two things to know before changing those numbers. A part size below S3's 5 MiB minimum is
rejected by the store — MinIO enforces it too — so `TestS3StorageMultipartUploadRoundTrips`
(a real 9 MiB upload read back byte for byte) is the test that catches it. And
`MultipartUploadThreshold` **defaults to 16 MiB independently of the part size**, unlike the
old `manager` package which compared the body against the part size; it is set explicitly
here so an 8-16 MiB upload does not quietly revert to a single request.

The dependency is pinned to `transfermanager v0.1.6` rather than `@latest` on purpose: v0.1.7
and later require **Go 1.24** and CI pins **1.23**. Bumping the toolchain is a separate
decision; until then, do not "helpfully" upgrade this module. `S3Storage.uploader` is a
one-method interface, so the next migration touches one field, one constructor line and the
test double.

#### The request log reports what the client received (KNOT-ADR-092)

One line is logged per request. A response that was **delivered** logs the usual
`INFO msg="http request" status=<handler status>`.

When the response body could not be written to the client, that same slot becomes an `ERROR`
and the `status` is **503** — what the client experienced — not the status the handler
intended. The handler's own status is kept beside it:

```
ERROR write deadline exceeded; client did not receive response
     status=503 intended_status=201 deadline_exceeded=true duration_ms=256611 …
```

This exists because a large upload that outlives `WriteTimeout` used to log `status=201` for
a response nobody received (KNOT-037-fix3). Two details matter if you touch the middleware:
`WriteHeader` commits a status and returns no error, so a write *error* is the only honest
signal; and a response small enough to fit the server's 2 KiB buffer is written to the socket
in `finishRequest`, whose error `net/http` discards — which is why the middleware **flushes
explicitly** before deciding what to log. Remove that flush and the failure becomes invisible
again; `TestRequestLoggingCatchesARealWriteTimeout` is the test that proves it.

Because the data directory is on a named volume, `scripts/dev-down.sh` keeps every
uploaded avatar and `scripts/dev-reset.sh` deletes it along with the databases.

Backend settings: `KNOT_STORAGE_PROVIDER`, `KNOT_S3_ENDPOINT`, `KNOT_S3_REGION`,
`KNOT_S3_ACCESS_KEY`, `KNOT_S3_SECRET_KEY`, and `KNOT_S3_BUCKET` — see "Backend
configuration" below and "Cloudflare R2" above. In
`local` every one of them has a working default, so a fresh clone needs no `.env` edit to
upload an avatar. Outside `local`, the endpoint and both credentials are required and the
server refuses to start without them.

> **Pinned image.** The compose file uses
> `bitnamilegacy/minio:2025.7.23-debian-12-r5`. MinIO no longer publishes `minio/minio`
> to Docker Hub (the Hub repository was withdrawn), `quay.io/minio/minio` requires
> authentication, and `bitnami/minio` has no tags, so the last published Bitnami build of
> genuine MinIO is pinned instead. It is a different image from the one upstream
> documents, so two details differ and both are handled in the compose file: the data
> directory (above) and the health check. The reason is repeated in a comment at the
> service, and KNOT-ADR-028 records the decision.

## Git hooks

The repository tracks its git hooks in `.githooks/`, activated with `core.hooksPath`.
The local `.git/hooks/` directory is untracked and cannot be shared, so it is not used.

- **`pre-commit`** scans every Go file under `backend/` and **blocks the commit** if any
  file declares two or more `package` clauses. This catches the recurring corruption where
  a tool prepends a stray `package` line — which breaks `gofmt` and `go vet`, and has
  reached CI before.

Install the hooks once after cloning:

```bash
scripts/install-hooks.sh
```

`install-hooks.sh` sets `git config core.hooksPath .githooks` and marks the hook
executable. If a commit is blocked, remove the stray line and retry — do not work around it.

Bypass the hook only in a genuine emergency with `git commit --no-verify`. This is
**discouraged**: it defeats the guard whose whole purpose is to keep the corruption out of
CI.
## Mobile (`apps/mobile/`)

```bash
cd apps/mobile
npm install
npm run lint        # ESLint
npm run typecheck   # tsc --noEmit (strict)
npm test            # Jest
npm run format:check # Prettier
```

There is intentionally no runnable product app yet — the scaffold has no navigation and
no product screens.

### Design tokens

The mobile theme lives in `apps/mobile/src/theme/` and encodes every token from
[`BRAND.md`](BRAND.md) as plain TypeScript objects: `colors`, `fonts`, `fontWeights`,
`fontSizes`, `lineHeights`, `typography`, `spacing`, `radius`, and `elevation`.

Import the tokens from the theme barrel and use them in place of literal values:

```ts
import { colors, radius, spacing } from '../../theme';

const styles = StyleSheet.create({
  card: {
    backgroundColor: colors.bg.inverse,
    borderColor: colors.border.default,
    borderRadius: radius.md,
    padding: spacing.lg,
  },
});
```

There is no theme provider, context, or styling library — the tokens are plain objects
imported directly. Values that fall between the documented scale steps stay numeric (for
example a `paddingVertical: 10`), and `BRAND.md` remains the source of truth for every
token value.

#### The component library and the redesigned screens (KNOT-020b / KNOT-020c)

The design pass added a component library in `apps/mobile/src/components/` — `Button`,
`Chip`, `Avatar`, `PillarTag`, `RootedBadge`, `AuthorLine`, `ReactionBar`, `FeedEntry`,
`Input`, `Header`, `EmptyState`, and `TabBar` — plus the brand SVG components (`Logo`,
`Logomark`, `Wordmark`, `SvgImage`) from KNOT-020b-fix. The five priority screens (Splash,
Onboarding, Feed, Story Detail, and Profile) were rebuilt on it in KNOT-020c: they compose
those components and style with the `typeScale` composites and the semantic colour roles
(`colors.text.primary`, `colors.bg.canvas`, …), so no redesigned screen sets a raw hex or a
manual `fontSize`/`lineHeight`/`fontWeight`. The three content screens — Adapt Story, Language
Tree, and Comment Thread — were migrated the same way in **KNOT-020d**: each
now opens with the shared `Header`, uses `Button`/`Input`/`EmptyState` instead of hand-rolled
controls, and reads `typeScale.*` and the semantic tokens throughout. The remaining screens
still read the deprecated legacy aliases until their own tasks migrate them.

**KNOT-020d-fix** added `StoryVersionScreen` — the per-version detail view a Language Tree row
opens onto — and refined the two list surfaces. The Language Tree row and a Comment Thread row
both drop the inline `RootedBadge`; the version's reactions moved off the tree row and onto the
version's own screen. `StoryVersionScreen` mirrors `StoryDetailScreen`'s reading column and
fetches through the same `versionsApi.getVersion` client, and it is reached from a Language Tree
row (an `onOpenVersion` callback) through a `storyVersion` overlay in `App.tsx`.

**KNOT-020e** migrated the two discovery screens the same way. `DiscoveryMapScreen` follows the
reference's layout: a static `Header`, the pillar filter as underline tabs, the Mapbox map across
the screen behind a rounded bottom sheet, and the sheet carrying the aggregate totals over a
scrollable place list with pull-to-refresh and a count-scaled marker per cluster.
`PlaceStoriesScreen` renders the place as an `h1` with its story and pillar totals beneath it and
lists its stories through the same `FeedEntry` the Feed uses (paused — the Feed is the only screen
that auto-plays, KNOT-ADR-068), with pull-to-refresh and cursor paging on scroll-to-end. Both read
`typeScale.*` and the semantic colour roles only, and the wording they share lives in the pure
`screens/discovery/discoveryFormat.ts`.

**KNOT-020e-fix** corrected that pass after on-device review: the map uses Mapbox's **Light** style
(the reference's light canvas, KNOT-ADR-026), the place list is the reference's **bottom sheet**
(`radius.xxl` top corners, `colors.bg.canvas`, `elevation[3]`'s upward shadow, a decorative drag
handle — no drag gesture), and discovery now clusters by **place and country** rather than by
coordinate, averaging a cluster's coordinates to place its marker. The rationale is recorded as a
scope note in [DECISIONS.md](DECISIONS.md); the API field notes are in [API.md](API.md).

**KNOT-020e-fix2** closed the three gaps that pass left on device. `GET /discovery/places/{place}`
now takes an optional `country` parameter (the reserved literal `null` meaning "records no country"),
so a country-scoped cluster's page lists exactly the stories its header counts. The place page is
enriched through the same `storyPageLookups.enrich` helper the feed uses, so a story carries
identical fields — author, avatar, media preview, counts, reactions — in both places. And the map
marker's ring is the navy ink rather than the canvas white, which disappeared into Mapbox's Light
basemap. The `?country=` contract is in [API.md](API.md) and the rationale in
[DECISIONS.md](DECISIONS.md).

**KNOT-020e-fix3** added the reference's compact map header to Place Stories: a 140-point,
edge-to-edge, fully non-interactive Mapbox strip (`StyleURL.Light`, city-level camera, one indigo
dot ringed in the navy ink) between the place block and the story list. It is positioned from the
cluster's averaged coordinate, which the `placeStories` overlay now carries, so it costs no extra
request; `placeMapPoint()` (in `screens/discovery/discoveryFormat.ts`) returns `undefined` for a
missing or nonsensical pair, and the strip is then not rendered at all. The strip is a sibling of
the list, not a list header, so it never scrolls with the stories.

**KNOT-020e-fix5** corrected the strip's sizing after on-device review, where the first story's
media bled into the strip. The wrapper is a normal-flow box with an explicit height; the MapView is
now given explicit `width: '100%'` / `height: '100%'` instead of `flex: 1`, and the wrapper clips it
with `overflow: 'hidden'` plus a canvas fill. A native map view is not a plain Yoga-measured View,
and React Native does not clip by default, so both bounds and clipping are required for a strip of
this size; without them the map laid out and drew beyond the strip and over the list.

**KNOT-020e-fix6** closed the bleed properly and added the place screen's pillar tabs. The strip's
`MapView` now sets `surfaceView={false}`, so Android renders it into a **texture view** rather than
the default `GLSurfaceView` — a surface view is punched through the RN view hierarchy, ignoring its
parent's clipping and the sibling z-order, which is why bounding it was not enough. The strip, the
tab row, and the list are stacked `zIndex` 2 > 1 > 0 with an opaque canvas background on the list,
so a story scrolled under the bands is hidden rather than drawn across them. The All / Heritage /
Wonder tabs mirror the Feed's equal-width tab row and filter **server-side** through the place
endpoint's new `?pillar=` parameter (a closed set: an unknown pillar is a `400`, unlike `country`);
changing a tab restarts the list from the first page, so a cursor is never reused across filters.

**KNOT-020f** migrated the last two screens that predate the design pass. `NotificationsScreen` now
opens on the shared `Header` — a 44-point back button, the title, and "Mark all as read" as the
trailing action in the brand ink — and each row is a full-bleed hairline entry rather than a card:
the actor's `Avatar`, one paragraph of body text with the actor's name in the stronger ink, the
relative time in the tertiary ink, and an 8-point indigo dot on the paper tint when the row is
unread. `SearchScreen` reads the same `Header`, two `Input` fields (the phrase and the place), a row
of `Chip`s for each filter, and flat result rows carrying an overline kind label, the title, a
two-line preview, the `AuthorLine`, and the `PillarTag`, each closed by a full-bleed hairline. Both
screens keep their fetching, keyset paging, debounce, and tap-through routing unchanged, and both
now read `typeScale.*` and the semantic colour roles only: no `fontSizes`, `lineHeights`, or
`typography.*` remains in either screen, and neither reads a deprecated colour alias. The inbox's
sentence wording moved into `screens/notifications/notificationFormat.ts`, a React-Native-free
module, so the rules that turn an event into its sentence — and the avatar URL resolver — are
unit-tested the way the discovery screens' labels are.

**KNOT-020g** migrated the three Curious Inquiries screens and audited the profile. `Input` gained
five optional pass-throughs — `autoCapitalize`, `autoCorrect`, `returnKeyType`,
`accessibilityLabel`, and a `leadingIcon` slot — so the Search field's magnifier now rides inside the
field and its keyboard behaviour (no autocapitalisation or autocorrection, a search return key, and
a "Search phrase" label) is restored (the KNOT-020f-fix1 item). `InquiryListScreen` opens on the
shared `Header` with an "Ask a question" trailing action and renders each question as a flat
hairline entry — an `INQUIRY · PLACE` overline, the title, a two-line body, the asker's `AuthorLine`,
and the answer tally with the question's language — with the place filter moved onto an `Input` and
an `EmptyState` for an empty list. `InquiryDetailScreen` reads the `Header` (back, and a report
action behind a `DotsThree` overflow), the question block (overline, `h1`, `AuthorLine`, body,
`ReactionBar`), a hairlined count bar, each answer as a hairline entry, and a composer pairing a
language `Chip` with an `Input` and a `Button`. `AskInquiryScreen` reads the `Header` with a close
button, two `Input` fields, the language and place pickers, the routing note, and a primary
`Button`. All three screens now read `typeScale.*` and the semantic colour roles only; the profile
screens were already there. The screens the design pass has not reached yet (auth, Rooted setup,
Create Story) still read the deprecated legacy aliases.

The `typeScale` **weights** are one step heavier than the reference snapshot for every token but
the three body styles (`reading`, `body`, `bodyS`), which stay at 400 so prose keeps its
readability — a deliberate divergence from `docs/design-reference/tokens.json` requested by the
founder so the app reads with more dignity (KNOT-020c-fix6). See
[DESIGN_SYSTEM.md](DESIGN_SYSTEM.md) for the reference-vs-mobile table.

The Feed's video preview is the one stateful piece of that library: `FeedVideo` (KNOT-020c-fix3)
plays a clip automatically, muted, once its entry is the one the list judges to be at least 60%
visible, and pauses every other entry's. A single feed-wide mute setting, held in `FeedScreen`,
governs all of them; `FeedEntry` itself stays hookless and only passes the visibility and mute
state down. The Feed is the only screen that auto-plays. See KNOT-ADR-068.

#### Motion and The Thread (KNOT-020h)

The motion layer lands last, on top of the design system: `src/theme/motion.ts`
holds the easing tuples and the `micro`/`small`/`medium`/`screen`/`max` durations,
and every animation added here reads from it (the springs are the one exception —
React Native's spring takes `speed`/`bounciness`, not a duration; the file says
so). Everything else runs on the **native driver**; the only JS-driven animation
is `Chip`'s fill, because `backgroundColor` cannot be animated natively.

- **`components/TheThread.tsx`** is the signature 1.5pt indigo line that draws
  itself: a fixed `length` × 1.5 frame (so nothing reflows while it draws) holding
  a line that grows through a `scaleX`/`scaleY` transform offset by half the
  length — the trick that keeps the leading edge at the start of the line — with
  `opacity` interpolated from the same value so `Animated.loop`'s reset is
  invisible. It runs `standard` at `durations.max`, the longest token
  (the brief's `durations.long` does not exist; `max` is the ceiling).
- **Where it draws:** under the splash logo; under the feed's tab row while a
  pull-to-refresh is in flight (the `RefreshControl` keeps the gesture and its own
  spinner is made transparent, because that prop cannot host a custom indicator);
  inside the adapt form's primary button via `Button`'s new optional
  `loadingIndicator`; and **horizontally** above the language tree while it
  loads, replacing its `ActivityIndicator` (`testID` `tree-thread`).
- **Overlay transitions, in `App.tsx`:** one `Animated.View` around the rendered
  screen carries a `translateX`. A **push** is detected by the stack getting
  deeper (every push already goes through `pushOverlay`, so no call site changed):
  a `useLayoutEffect` jumps the offset to the window width and runs `enter` at
  `durations.screen` to zero, before the frame paints. A **pop** is run *before*
  the state changes — `exit` at `durations.screen` slides the screen out, and the
  stack is popped when it finishes, so the leaving screen keeps its content
  instead of remounting into its loading state. `popping` makes a second back tap
  during those 280ms a no-op. The tab bar stays outside the animated view: it is
  still hidden whenever the stack is non-empty.
- **Disclosed:** the app renders exactly ONE screen at a time (that is what pauses
  the Feed's video), so the transition slides over the canvas rather than over a
  static underlying screen. Keeping the lower screen mounted would restart hidden
  video playback, so it was not done; a true parallax needs a navigator that keeps
  the stack mounted.
- **Micro-interactions:** `Button` scales to `0.97` on press-in and back on
  press-out (`timing` at `micro`); `Chip` cross-fades `bg.sunken`→`brand.tint`
  (`timing` at `small`) and springs its leading icon in when selected; a tapped
  `ReactionBar` pill pulses to `1.06` and back (`micro` twice) — the reaction
  glyphs themselves are long gone (KNOT-025-fix3); and the active `TabBar` glyph
  runs a two-step `micro` bounce when its tab becomes selected.

#### The theme system (KNOT-020i)

There are two themes and one place that decides which is active:
`src/theme/ThemeContext.tsx`. `ThemeProvider` sits at the root of `App.tsx` (which
renders a `ThemedApp` inside it, so App's own stylesheet follows the theme too).
`useTheme()` returns `{ theme, colors, setTheme, toggleTheme }`.

**Writing a themed screen.** Colours must not be captured at module scope — a
`StyleSheet.create({ … colors.x … })` evaluated at import time freezes whichever
palette loaded first — so every screen and component now builds its stylesheet
from a factory:

```ts
const createStyles = (colors: ThemeColors) => StyleSheet.create({ … });

export default function Screen() {
  const { colors } = useTheme();               // only where a colour is used inline
  const styles = useThemedStyles(createStyles);
}
```

`useThemedStyles` caches one stylesheet per theme (keyed by the factory), so the
object keeps its identity between renders and style arrays do not churn. The
static `colors` export still exists as the LIGHT set — it is the documented
fallback for anything rendered without a provider (a unit test, a preview) — but
no screen imports it any more.

**Rules.**

- Never hard-code a colour: add or reuse a role in `colors.ts` instead. `palette.ts`
  is the only file with hex values, plus the two `border.divider` values the role
  owns because the reference has no swatch for them.
- A new role must be defined for BOTH themes in `activePalette`. Do not spread the
  light palette over the dark one: the dark palette deliberately renames the accent
  roles (`indigoText`/`indigoFill`/`indigoOnTint`).
- A sheet uses `colors.bg.sheet`, not `colors.bg.canvas` — they differ in dark.
- Chrome that JS owns must follow the theme explicitly: the status-bar `barStyle`
  (`light-content` in dark), the Mapbox `styleURL` on the discovery screens, and
  the Android **navigation bar**, which `src/native/systemBars.ts` repaints
  through the app-local `SystemBarsModule` whenever the active theme changes
  (KNOT-020j). Modals need the same treatment: RN's `Modal` is an Android dialog
  with its own window and RN's own container theme, so the module also repaints
  every open `ReactModalHostView` dialog, on a theme change and on each
  window-focus change (`MainActivity.onWindowFocusChanged`, KNOT-020j-fix). The
  native theme's `navigationBarColor` is only the value shown before JavaScript
  mounts.

**Persistence.** `src/theme/themeStorage.ts` mirrors `session.ts`: best-effort
AsyncStorage under `knot.theme.v1`, `saveTheme`/`loadTheme`/`clearTheme`, every
failure logged and swallowed. On launch the stored choice wins; with none, the
provider follows `Appearance.getColorScheme()`; with neither, light.

**Testing.** Component suites call components directly with no renderer, so
`jest.setup.js` replaces `React.useContext` for every suite with the light theme
(the same value the context defaults to outside a provider). A suite that
provides its own `jest.mock('react', …)` must therefore supply `useContext`
itself — see the comment in the setup file.

#### Navigation and media overlays (KNOT-020j)

**The tab bar** (`src/components/TabBar.tsx`) is five destinations: Home, Map,
Create (the elevated action), Inquiries, and Notifications. The notifications tab
shows the short label **Notice** so it fits an equal fifth of the bar on a narrow
device; its accessibility label stays "Notifications". Inquiries draws a
`ChatsCircle` glyph rather than a magnifier, which is the Feed header's search
control (KNOT-020j-fix). Notifications took the slot Profile used to hold, so the
inbox has a permanent home; the bell left the
Feed header, and the header's right side is now the search glyph and the
signed-in person's avatar, which opens their profile overlay. A tab renders as a
screen in `App.tsx`'s `renderTab` (like Inquiries), so the bar stays visible and
the destination reads as selected. Adding or renaming a destination means
changing `TabName` in `TabBar.tsx` and the `switch` in `App.tsx` together.

The unread badge is data, not tab state: `App.tsx` reads
`notificationsApi.getUnreadCount` whenever the signed-in tab changes and passes
`unreadCount` to `TabBar`, which stays stateless.

**Media text overlays** — a video timestamp, the "Video" tag, a media count, the
carousel counter — always sit on `colors.scrim.overlay`. Their ink is
`colors.text.onMedia`, which is white in BOTH themes because the scrim is,
and their type is `typeScale.label` (13pt bold) or heavier. Do not use
`text.onBrand` over media: it is the canvas colour and flips to near-black in the
dark theme. `mediaOverlayText(colors)` (`src/theme/mediaOverlay.ts`) supplies the
ink and the legibility shadow in one object.

### The media viewer (KNOT-020k; definitive pass KNOT-020k-fix4; final polish KNOT-020k-fix5)

Tapping media anywhere in the app opens one full-screen viewer. It is a React Native
`Modal` owned by `App.tsx` (`mediaViewer: { items, index, story } | null`), so it sits
above the tab bar and every overlay without being a route. A screen receives
`onOpenMedia(items, index, story?)` and calls it.

`story` is a `ViewerStory` — a projection (`storyId`, `title`, `body`, `media`, and the
author's name/avatar/time). It carries only what the overlay draws, so a screen hands
over its own record without the viewer knowing any API type.

**Every surface opens the whole gallery.** `ViewerStory.media` holds the story's full
array, and `App.openMediaViewer` guarantees it:

- **Story Detail and Version Detail** already hold the array, so they open at once on
  the tapped index.
- **Feed, Place Stories and the profile wall** hold a single preview (`first_media_*` /
  `media_preview_url`) with no ids for the rest, so they hand over `media: []`; `App`
  reads `GET /stories/{id}`, maps it with `toViewerMedia`, and opens on the page whose
  url matches the tapped preview (`viewerIndexByUrl`). A failed read falls back to the
  preview so the tap is never dead.

The pieces:

- **`MediaGalleryModal.tsx`** — the black theatre (`colors.bg.theater`) with the media
  edge to edge behind the status bar; a floating top bar (page counter on the left,
  close on the right); and, when it was given a story, a bottom overlay of **one bold
  author line** (`Avatar size="sm"` + bold name + `·` compact time, no Rooted badge),
  the title at `typeScale.h3`, three lines of the body, and one bold **"Read more"**
  link. The overlay is fully **transparent**: there is no fill, no radius, no card and
  **no scrim or gradient**. Legibility comes from a heavy `textShadow` on every text
  node (radius 5, `colors.bg.theater`) — the TikTok/Instagram treatment — so white type
  reads over a white photograph while the picture is never dimmed. The shadow must be
  applied to the nested name `Text` too: RN does not inherit `textShadow*`. The link
  closes the viewer and opens the story. There are deliberately **no reactions and no
  CTA** here.
- **Mounting and the autoplay rule.** The modal is mounted for the session, but the
  interactive `ViewerStage` is mounted **per open**, and its pager state is seeded with
  `useState(() => clampViewerIndex(initialIndex, media.length))`. `FlatList` gets
  `initialScrollIndex={initialPage}`, `initialNumToRender={initialPage + 1}` and a
  post-mount `scrollToIndex({ index: initialPage, animated: false })`, so the tapped
  page is the one rendered, the one on screen and the one whose `VideoPlayer` receives
  `isActive` — even if the list ignores `initialScrollIndex` on its first layout.
  `onViewableItemsChanged` keeps the page in step with swipes (ignored until the pager
  has moved once, so a mount-time report cannot override the seeded page);
  `onMomentumScrollEnd` is the exact arithmetic fallback.
- **Why a Feed video "would not open" (KNOT-020k-fix5) — the player ate the tap.**
  None of the pager seeding above was ever the cause. On Android the native video
  surface is the topmost thing over the picture, and it consumed the press before it
  reached the `Pressable` the card wraps its media in, so `onOpenMedia` never fired and
  no modal window was created — while tapping an image in the same card opened the
  viewer perfectly. `FeedVideo` therefore wraps `<Video>` in a `View` with
  `pointerEvents="none"`; `box-none` is not enough, the surface itself has to be
  skipped. This is also what makes the profile wall's video cards work. **Any new
  component that draws a video under a tap target must do the same** — and the mute
  control stays a *sibling* above the anchor so it remains tappable.
- **`VideoPlayer.tsx`** — `react-native-video` with a **transparent** transport strip
  anchored to the bottom: a thin scrub line that swells into a handle under the finger,
  the running `0:12 / 0:45`, and mute. There is **no play/pause button**: a tap on the
  picture toggles playback *and* the chrome. `VIDEO_CONTROLS_HEIGHT` is exported so the
  viewer can keep the story text clear of the strip; the overlay adds a whole `lg` on top
  of it, because the scrub bar's 32pt touch band is invisible — text that merely abutted
  the strip would look clear while still swallowing taps aimed at **"Read more"**.
  Videos **loop** (`repeat`), so there is no ended state to recover from. Videos start
  with **sound on** (`START_MUTED = false`), because the viewer was opened deliberately;
  the Feed's in-place previews keep their own always-muted rule (`FeedVideo`). The
  overlay and the transport fade with the chrome on a tap.
- **`mediaViewer.ts`** — the pure helpers (index clamping, progress, scrub seconds, the
  overlay's `viewerSnippet` window, `viewerTimeLabel`'s "0:00" clock, and
  `viewerIndexForId`'s key→page resolution).
- **`storyGallery.ts`** — the pure gallery mapping (`toViewerMedia`, `viewerIndexByUrl`)
  used by `App` when a caller only held a preview.

Gestures use `Animated` and `PanResponder` only: the app has no gesture library and this
viewer adds none (KNOT-ADR-037, KNOT-ADR-078). The navigation bar is painted black while
the viewer is open and restored to the theme's canvas when it closes.

**The status bar inside a modal (KNOT-020k-fix8).** `<StatusBar barStyle="light-content"`
`backgroundColor="transparent" translucent />` is declared inside `MediaGalleryModal`, but
that declaration alone cannot turn the icons white on Android: a `Modal` is a real
`Dialog` with its own window, and `ReactModalHostView` copies the **Activity's**
status-bar appearance into that window once, when the dialog is shown — before a
component inside the modal can run, and carrying the light theme's dark icons. So
`src/native/systemBars.ts` grew `setStatusBarIcons(lightContent)`, which the existing
app-local `SystemBarsModule.kt` applies to the Activity **and to every open
`ReactModalHostView` dialog** (the same window walk it already used for the navigation
bar). The viewer forces `true` on open and passes `null` on close; `null` means “stop
managing”, which hands the bar back to React Native's own status-bar stack so a screen
that declares its own `StatusBar` (Story Detail's edge-to-edge media) gets its value
back. **Do not replace this with a declarative `StatusBar` alone** — it cannot reach a
dialog window. The manifest-mandated iOS path is unchanged: the modal is a view
controller there and `StatusBar` already applies app-wide.

**The feed video's timestamp (KNOT-020k-fix8).** `FeedVideo`'s badge used to be the clip
length, learned on `onLoad`. It is now `formatProgress(current, duration)` from
`feedVideoMeta.ts` — `0:15 / 1:16` — driven by the player's `onProgress` event. The
subscription is handed to `<Video>` **only while `isVisible`**, so a paused or off-screen
preview keeps the last position it reached instead of ticking unseen, and
`formatProgress` returns `''` until the length is known (there is no badge to count
against). The same badge is drawn by the Story Detail carousel, which passes
`durationMs` and `durationPosition="bottom-left"`.

**The story overlay is transparent — the black is the system chrome
(KNOT-020k-fix10).** `styles.context` paints **no surface**: no band, no scrim, no
gradient. Each line is media-white ink with a heavy black shadow, so the picture
runs to the bottom edge of the screen and the black a viewer sees under the caption
is the **system navigation bar** plus the room, never a panel drawn by the viewer.
An earlier pass (fix9) put an opaque `bg.theater` band behind the text and had
`VideoPlayer` paint a matching strip under its transport; that was a misreading of
"black at the bottom" and both are gone. The overlay is rendered *after* the pager,
so anything opaque there would cover a video's controls — keep it transparent and let
the bars do the black.

**Only a video reserves the bottom (`KNOT-020k-fix11`).** The inset is conditional:
an image page keeps the base `spacing.lg` and its caption hugs the bottom edge,
while a video page adds `VIDEO_CONTROLS_HEIGHT` so the text clears the scrub bar's
invisible 32pt touch band (`contextAboveControls`, applied only when the current page
is a video). The two page types therefore no longer share a caption height — that
shared height was fix9's compensation for a band that no longer exists, and it left
an empty strip under every photo.

**Every entry point opens the viewer the same way (`KNOT-020k-fix11`).**
`FeedEntry` hands `onOpenMedia` a complete `ViewerStory` — `storyId` plus the tapped
item in `media` — matching what Story Detail passes. `App.openMediaViewer` opens at
once on what the caller holds, so a tap on a Feed (or place/profile) image lands in
the fixed, full-window layout on the first frame instead of waiting for
`GET /stories/{id}`.

**Instant open, then the whole gallery (`KNOT-020k-fix16`).** Opening instantly with
one item used to leave the Feed viewer single-page. `App.openMediaViewer` now sets the
viewer state synchronously **and** starts a background read when the context held at
most one item; `withHydratedGallery` (`components/mediaViewer.ts`) installs the answer
only if the same story is still open, keeping the opening index. Two rules make it
seamless:

- **The stage's `key` is the opening page and nothing else.** Including the media
  length — which the key used to do — remounts the pager when the array grows and
  throws the reader back to the start.
- **`ViewerStage` re-anchors on the item, not the index.** It tracks the active item's
  url (a preview and the gallery share only the url — the ids differ), and when the
  media array changes it finds that url, sets the index and `scrollToIndex` without
  animation. Every index has shifted under the reader, so the item is the only stable
  identity. Its viewability callback reads the items through a ref for the same reason:
  the callback must keep its identity while the gallery is replaced underneath it.

The system-bar paint from `KNOT-020k-fix15` is untouched by this: the chrome is applied
when the modal opens, not when the media changes.

**Releasing the native video player (`KNOT-020k-fix17`).** A pager keeps its
neighbouring pages mounted, so before this every visited clip held a live
`ExoPlayer` — surface, decoders, buffers and audio track — until the whole viewer
closed; a long session of opening, swiping and closing ran the process out of
memory (`OutOfMemoryError` in `ExoPlayer:Playback`). `react-native-video` has **no
`release()`**. What it does have is `setSource(source?)`: called with no argument it
reaches the native side as an empty source, and `ReactExoplayerView.setSrc` answers
that with `clearSrc()`, which stops the player and drops its media items. So
`VideoPlayer` runs an effect on `[isActive, uri]`: when the page stops being the one
on screen it calls `pause()` + `setSource()` and forgets the loaded state; when the
reader swipes back it re-seats `setSource({ uri })` and the clip loads again. A
`released` ref keeps a first mount from reloading its own source. The native **view**
is still freed by unmounting — the viewer renders `ViewerStage` only while `visible`,
so closing releases the rest. Do not "optimise" the clear away: an idle-but-mounted
player is exactly what accumulates.

**The viewer paints its own system bars — both of them, on every window
(`KNOT-020k-fix15`).** Opening the viewer from the Feed used to leave the status and
navigation bars in the app's canvas while every other entry point looked right.
Two gaps caused it, and both are closed:

1. **Only the status bar's *icons* were ever forced.** `setStatusBarIcons` now takes
   the fill colour as well, and the native module applies it (plus contrast
   enforcement off on API 29+) to the Activity **and** to every open
   `ReactModalHostView` dialog, whose window does not inherit the Activity's
   status-bar colour. The `<StatusBar backgroundColor="transparent" translucent />`
   inside the modal reaches the Activity only — it can never colour the dialog that
   is actually on top. The screens that looked right (Story Detail, Version Detail,
   Place Stories, Discovery Map) all declare their own transparent `<StatusBar>`, so
   the Activity bar was already transparent behind the dialog; **the Feed declares
   none**, so it inherited App's opaque canvas bar and stayed white.
2. **The paint was a one-shot passive effect fired in the commit that *builds* the
   dialog.** The Feed's commit is the heaviest in the app (it mounts the whole
   `ViewerStage` + pager + media on top of a live list), so the call could be
   delivered before the dialog was attached and `applyToOpenDialogs` then found
   nothing. The same paint now runs again from the `Modal`'s **`onShow`**, which the
   native dialog emits (`ReactModalHostView` sets it as the dialog's
   `OnShowListener`) after the window exists and after React Native has copied the
   Activity's appearance into it. Treat `onShow` as the authoritative moment for any
   future system-bar work in a modal.

On close the module repaints the Activity's status bar in the chrome colour
JavaScript has already restored (the canvas) before handing the icons back to
`StatusBar`, so no black survives the viewer.

### Author attribution (`AuthorLine`)

Every screen that shows user-generated content attributes it through one component,
`apps/mobile/src/components/AuthorLine.tsx`: an avatar, the author's display name, an
optional `RootedBadge`, and the content's timestamp as a relative phrase. It takes
`displayName`, `avatarUrl`, `createdAt`, `rooted`, and an optional `size`
(`small` / `medium` / `large`), and omits any segment whose input is missing.

- The avatar is an `<Image>` when `avatarUrl` is set, resolved against `API_BASE_URL`. The
  server sends a relative path (`/users/{id}/avatar`), or `null` for an author with none.
- With no avatar it falls back to initials from `getInitials` in
  `apps/mobile/src/utils/initials.ts` (first and last word, up to two letters, `?` when the
  name is blank).
- The timestamp is rendered with `formatRelativeTime` from
  `apps/mobile/src/utils/time.ts`, never as a raw ISO string.

The fields come from the API — `author_display_name` and `author_avatar_url` on stories,
versions, and comments (KNOT-ADR-041) — so a screen never has to call the identity
service for a name. Render attribution by passing an entity's fields to `AuthorLine`; do not
hand-roll the markup on a new screen.

**Where the Rooted badge shows.** The badge belongs to **detail views** — Feed, Story Detail,
Profile, and the version detail (`StoryVersionScreen`) — where attribution is the point. **List
rows drop it**: a Language Tree row, a Comment Thread row, a Search result, and an Inquiry list row
pass `rooted={null}`, and the Inquiry detail drops it too, because a row is about the version, the
discussion, the result, or the question, and not the author's rootedness (KNOT-020d-fix, extended by
KNOT-020g; see the scope notes at the end of [`docs/DECISIONS.md`](DECISIONS.md)).

### The profile wall (`UserProfileScreen`)

`apps/mobile/src/screens/profile/UserProfileScreen.tsx` shows a user's public wall from
`GET /users/{id}/profile` (`src/api/profile.ts`): a centred header (avatar, name, Rooted badge,
"Joined …", and a stories/versions/answers stats line) above three activity tabs — Stories,
Versions, and Inquiries — and a list of rich cards, with pull-to-refresh and cursor paging. Each
card carries an overline (a story leads with its plain pillar tag) and the title over a meta line,
closed by a full-bleed hairline, and opens the entity it names — a story card opens the story, a
version card opens the story it belongs to, and an inquiry card opens the question. The wall shows
only what a person authored themselves: comments and answers still arrive on the
activity stream but are filtered out of the display and given no tab (KNOT-020g-fix). The stats
line, the per-tab filtering, and the card copy are reduced from the activities loaded so far
(`src/screens/profile/profileActivity.ts`), because the endpoint returns one page and no totals;
both grow as the wall is paged (KNOT-020c).

The header's overflow control opens a **bottom-sheet options menu** (`Modal`, rounded top corners,
a drag handle, and a hairline between rows): Edit avatar, Set Rooted, Blocked users, and a
destructive Sign out for the owner, and Block user for a visitor. It dismisses on a backdrop tap,
the Android back button, or a downward drag of its handle (KNOT-020g-fix).

**KNOT-020g-fix2** filled the two gaps that pass left. The profile endpoint now enriches each
activity in the same query that builds the page (`internal/profile/postgres_store.go`): a story
carries `place` and `version_count`, an adaptation `place` and `version_number` (its ordinal among
its story's versions, counting the root as 1), and a question `answer_count`. A card therefore
reads `HERITAGE · Manguzi` over the title with `English · 4 versions · 2 days ago` beneath it,
`Version 2 · Zulu` with `Manguzi · 3 weeks ago`, and `Inquiry · Manguzi` with
`6 answers · 2 days ago`; a segment whose field is absent is omitted, never faked. The profile's
options sheet and the media picker (`MediaPickerSheet`, shared with the story composer) now draw
the **same sheet**: a canvas fill with `radius.xxl` top corners, `elevation[3]`, a draggable 32×4
handle, icon rows with bold labels and hairline separators at a 44-point minimum height, and a
text-only Cancel. The three tabs gained the screen's gutter, so the first underline is no longer
clipped. Finally, the blocked-users list left the wall for its own overlay screen
(`screens/profile/BlockedUsersScreen.tsx`), reached from the options sheet: an avatar, a name, and
an Unblock action per row, with pull-to-refresh and an empty state.

**KNOT-020g-fix3** gave the cards their media. `GET /users/{id}/profile` now attaches the story's
first media item to every story activity — and to every version activity, which previews its story's
media — as `media_preview_url` plus `first_media_type`, from **one batched media read per page**,
reusing the feed's `storyMediaPreviews` and `firstMediaFields` helpers (`internal/httpapi/enrich.go`).
A story card therefore draws a 16:9 rounded image under its title, or a plain placeholder with a play
glyph for a video, and draws nothing when the story has no media; a version card does the same. The
profile's stats line now reads `N stories · N versions · N inquiries` (each noun pluralised against
its own count) instead of counting answers, and the bottom sheets lost their row dividers — rows are
separated by spacing alone. The three tabs already met the reference (natural width, left-aligned,
a 24-point gap, a 16-point gutter, and a full-bleed hairline), so that item needed no change.

**KNOT-020g-fix4** gave the cards their body and their frame. Each `story`, `version`, and `inquiry`
activity now carries `preview` — the first 150 characters of the story's, the adaptation's, or the
question's body, truncated **in SQL** (`left(body, 150) || '…'`) so a page never ships a whole body
to draw a two-line excerpt — and a card draws it as `typeScale.bodyS` under its title, in the feed's
reading order: pillar or overline, title, excerpt, media, meta. A video card no longer shows a bare
play glyph: it renders `FeedVideo` in a new `previewOnly` mode, which draws the clip's first frame
as a still (with its own fallback layer while it loads) behind a scrim-backed play badge, with no
duration chrome and no mute control — the wall is a browse surface, and playback stays in the Feed.
`FeedVideo`'s `isVisible`, `isMuted`, and `onToggleMute` became optional with the defaults the feed
already passed, so its one call site is unchanged. The three tabs are now centred as a group
(`tabsContent` gained `flexGrow: 1` and `justifyContent: 'center'`; `flexGrow` is what lets the
content container fill the viewport so the justification has space to act in), the picker sheet's
Cancel reads in `colors.state.danger`, and `Header` gained an opt-in `centered` prop that pins a
title to the bar's own middle rather than to the space left between the two side slots. Only the
profile passes it, so every other header is untouched.

**KNOT-020g-fix5** gave list media its own shape. Every payload that already carried a story's first
media item — `GET /stories`, `GET /stories/{id}`, `GET /discovery/places/{place}`, and
`GET /users/{id}/profile` — now also carries `first_media_width` and `first_media_height`. **No
query changed:** `storymedia.FirstMedia` already selected and scanned the columns (KNOT-013); only
the HTTP projection was discarding them, so `firstMediaFields` now returns the whole description
of the first item (kind, URL, dimensions) as one small struct and the three call sites assign all of
it. On the client, `src/utils/mediaFrame.ts` turns a nullable pair into a ratio —
`FALLBACK_MEDIA_ASPECT_RATIO` (16:9) when either dimension is missing, zero, or negative — and both
`FeedEntry` and the profile card frame their media with it, so a portrait photo renders tall instead
of being cropped into a fixed window. Media is fitted with `contain` rather than `cover`, so an
unknown shape is letterboxed rather than cut. The profile card's media also went **full-bleed**: it
escapes the card's 16-point gutter (`marginHorizontal: -layout.screenPaddingFeed`) and lost its
`radius.lg` corners, which is the inset the founder saw — the feed's media frame was already
full-bleed and unrounded (KNOT-020c-fix2). The profile's video preview badge went from a 64-point
`scrim.overlay` disc around a 48-point `PlayCircle` to a 36-point disc around a 20-point white
`Play` triangle, so the badge reads over arbitrary media without dominating the card.

**KNOT-020g-fix6** changed how a list *fills* the frame it builds. Frames are still shaped by
`mediaFrame.ts` (the media's own `width / height`, or 16:9 when the payload reports neither), but
the media inside them is now fitted with **`cover`** everywhere a list draws it — `FeedEntry`, the
profile card, and `FeedVideo`, which lost the `resizeMode` knob it had briefly carried for a
`contain` preview, because no caller wants one. `cover` fills the frame, so a frame whose ratio does
not quite match the media crops the overflow instead of leaving a `bg.sunken` bar; and media whose
shape the payload never reported — a legacy row, or a video whose picker reported only its duration,
which `react-native-image-picker` routinely does — is cropped into the 16:9 fallback rather than
letterboxed inside it. **Nothing changed in the upload path:** the picker already forwarded the
asset's width and height, `CreateStoryScreen` already passed them, `uploadStoryMedia` already sent
them as form fields, and the server already parsed and stored them — the bars came from `contain`
meeting a payload with no dimensions, not from a missing field. Avatar uploads carry no dimensions
and are unchanged: an avatar renders in a fixed circle and the `users` table has no columns for
them, so sending them would be write-only.

**KNOT-020g-fix7** gave list media its own height. A list frame was already full width, but its
*shape* was guessed whenever the payload could not describe the media — which is most videos, since
`react-native-image-picker` reports only their duration — so a fixed 16:9 frame cropped portrait
media into it. Now the frame is the media's own shape, resolved in two stages: the payload's
`first_media_width` / `first_media_height` when the server knows them (so the frame is right on the
first paint and the media never settles), and otherwise the natural size the media reports on load —
`Image`'s `onLoad` for a photo, and `FeedVideo`'s new `onLoadSize` pass-through, which forwards
`naturalSize` from the player in **both** of its modes, for a video. Until one of them answers, the
frame is a full-width square of `bg.sunken`; the media is fitted with `cover`, so it fills whatever
frame it is given and a placeholder is never a bar. `resolveMediaAspectRatio` is the whole rule and
is pure, so it is tested without a renderer; it deliberately returns `null` rather than a fixed
fallback, and `useMediaAspectRatio` (a new module beside `mediaFrame.ts`, which stays import-free)
holds the loaded size across renders.

**This is where `FeedEntry` stopped being hookless.** It held no state on purpose so the tests could
call it as a plain function, and the media frame is the first thing in it that genuinely needs state.
Its tests moved to `components/__tests__/FeedEntry.test.ts`, which stubs React's `useState` and
`useCallback` so the direct-call pattern survives; the decision itself is covered exhaustively as a
pure rule. The profile card's media follows the same rule and is now a file-local `CardMedia`
component, because the hook cannot live in a helper a parent calls mid-render.

**KNOT-020g-fix8** closed a right-edge gap on the profile wall and coloured the two system bars.
The gap was arithmetic, not styling taste: a profile card sits inside the wall's 16-point list
gutter, so its content box is `screen − 32`, and `cardMedia` asked for `width: '100%'` **and** a
`-16` horizontal margin — the width resolved against the padded box and the margin then shifted it
left, leaving the media flush at `x = 0` and 32 points short at the right. A negative margin and a
percentage width cannot both be right. The card now has **no horizontal padding of its own**: the
two text blocks around the media carry the gutter (`cardPadded`), which leaves the media a plain
full-width child with no margin at all — the structure `FeedEntry` has, and the reason the feed's
media was already correct. The hairline still spans the screen, because the card keeps the negative
margin that cancels the list's gutter.

The system bars are painted by the **Android window**, so they are set in the native theme
(`android/app/src/main/res/values/styles.xml`) with a `knot_canvas` colour resource alongside
`knot_splash_background`. They take effect before React Native mounts and survive every screen.
`windowLightStatusBar` and `windowLightNavigationBar` supply the dark icons a light bar needs; the
navigation-bar attribute is API 27+, so on API 24-26 the bar is white with its default light icons.
`App.tsx` also renders `<StatusBar backgroundColor={colors.bg.canvas} barStyle="dark-content"
translucent={false} />` in each of its four roots so the JavaScript layer agrees with the theme once
it runs — the value is declared in both layers deliberately, because each can only read its own.
**`StatusBar` cannot reach the navigation bar**; only the theme can, and only while the app targets
below API 35 (see the note below).

**KNOT-020g-fix9** gave the two Discovery screens the modern map treatment. On the Discovery Map the
map is no longer a band between the tabs and the sheet: it is the **screen**, positioned
`absoluteFillObject`, and the header and the pillar tabs float over it in one opaque canvas card. On
Place Stories the 140-point map strip left the flow and is pinned to the top of the screen with a
drawing-order `zIndex` of 2, the back header floats over it at 3, and a spacer of the strip's height
reserves its band so the place block, the tabs and the list all start below it. The strip's rendered
height is `topInset + STRIP_HEIGHT`, so the map grows upward into the status bar area.

Both screens make the status bar **translucent** with the canvas colour and `dark-content`, which is
how the map reaches the physical top edge: with `translucent: false` (the app-wide default from
KNOT-020g-fix8) Android reserves the bar's space and nothing can sit behind it. A translucent window
means the app is laid out from `y = 0`, so the floating bands pad themselves by
`mapTopInset(Platform.OS, StatusBar.currentHeight)` — the height on Android, nothing on iOS, whose
root `SafeAreaView` has already inset the screen. The bar is still painted opaque white over the map,
so the icons stay dark on white rather than over the map.

Mapbox's scale bar and its attribution button are off on both maps (`scaleBarEnabled`,
`attributionEnabled`); the **logo stays**, as Mapbox's terms require, and is positioned with
`logoPosition` so it cannot be hidden — just above the sheet on the Discovery Map, whose every lower
corner is behind the sheet, and in the strip's lower left on Place Stories.

**KNOT-020g-fix10** made the Discovery Map **map-first**, the shape Bolt's and Uber's home screens use, and
undid the floating bands KNOT-020g-fix9 had put over it. The screen is now the map plus a sheet: the
`MapView` keeps `StyleSheet.absoluteFillObject` and **nothing floats above it** — the title bar, the
pillar tab row, `styles.bands` and the `mapTopInset` read are all gone from this screen. Everything the
screen offers moved into the bottom sheet, which grew from 44% to **50%** of the screen height: a
decorative drag handle (now `radius.pill`, with the reference's roomier `spacing.md`/`spacing.lg`
margins around it), a `typeScale.h1` headline ("Discover places."), a `typeScale.body` subtitle,
the pillar filter as a **`Chip` row** (All / Wonder / Heritage, `selected` marking the active one),
the `placeTotalsLabel` aggregate line, and then the place list. The filter is still the same
server-side `?pillar=` query — only the control that sets it moved, so chips replaced the underline
tabs without touching the fetch.

The status bar on this screen is now **transparent** (`backgroundColor="transparent"`, still
`translucent` and `dark-content`), so the map shows through it. Nothing needs to clear the bar, which
is why the `mapTopInset` call was removed here; Place Stories keeps its own. Mapbox's
**attribution button is restored** (`attributionEnabled`), settling the compliance question fix9
left open: the logo sits bottom-left and the attribution bottom-right, both on the one line of map
the sheet does not cover (`{ bottom: sheetHeight + spacing.sm }`), and the scale bar stays off.

**KNOT-020g-fix11** gave Place Stories the map-to-top treatment the Discovery Map already had, and polished
the Discovery Map's place rows. On Place Stories the strip **already** ran to `y = 0` and behind the
status bar — what the founder saw as "the map strip does not reach the top — there is a white band"
was chrome painting over it: `styles.floatingHeader` was a full-width box with a `colors.bg.canvas`
fill, and the status bar itself was still the opaque canvas from KNOT-020g-fix9. Both are gone: the
bar is now `backgroundColor="transparent"` (matching the Discovery Map), the overlay paints nothing
and starts at `top: mapTopInset(...)`, and the back control carries its own 44-point **round canvas
pill** with `elevation[2]` instead of sitting on a band. That is the same trade the Discovery Map
makes — a small opaque control rather than a full-width strip, so the map is visible right up to the
top edge while the arrow stays legible over any tile.

In the Discovery Map's sheet the place rows lost their `borderTopColor`/`borderTopWidth` hairline; the
rows are told apart by their own padding, which is the sheet's pattern. The name moved from
`typeScale.button` to **`typeScale.h3`**, the count from `bodyS`/`text.secondary` to
`caption`/`text.tertiary` so it recedes behind the name, and each row gained a leading
**`MapPin`** (20pt, `weight="fill"`, `text.tertiary`) in a 36-point `bg.sunken` disc — the first map
pin in the app, so there was no existing treatment to follow. With the row's `spacing.md` vertical
padding that makes a row about 68 points when it carries a subtitle and about 60 when it does not, so
the 44-point touch target is met by the row's own height rather than by a hit-slop.

**KNOT-020g-fix12** finished the Discovery Map sheet, capped list media, and polished the Create Story
screen. In the sheet's place rows the text column went from `flexShrink: 1` to **`flex: 1`** — that was
the whole bug: a content-hugging column let `justifyContent: 'space-between'` push the name into the
middle of the row, so rows read as centred. The column now takes every point between the pin and the
count, which is what left-aligns it. The pin lost both its `bg.sunken` disc and its fill: it is a
**`MapPinLine`** outline glyph at 20pt in `text.tertiary`, sitting straight on the row. The sheet also
gained a **place search** above the list — an `Input` with a `leadingIcon` magnifier and a *sibling*
clear control, because `Input` has one leading slot and no trailing one — which filters the
**already-loaded** clusters by name, client-side, and never re-queries the server. The search narrows the
list, the map markers and the totals line together, so the three can never disagree; a search that
matches nothing is told apart from "no places yet" by its own `No places match` line. The markers became
**filled indigo pins** (30pt `MapPin`, `weight="fill"`) centred in a transparent 44-point box: the count
left the marker, and with it the "size by busiest cluster" rule, so `markerSize`/`markerCountLabel` and
their tests were **deleted** rather than left dead.

List media gained a **maximum height**. `mediaFrame.ts` exports **`MAX_PORTRAIT_ASPECT_RATIO = 4 / 5`** and
`mediaAspectRatio` now returns `Math.max(width / height, 4 / 5)`: a portrait media taller than 4:5 is framed
at 4:5 (width × 1.25) and cropped top and bottom by its `cover` fill, while landscape media — every
landscape ratio is already wider than 4:5 — passes through untouched. It applies to the payload ratio *and*
to the ratio learned from a loaded image or video, because both go through `mediaAspectRatio`.

Story previews can now say **"Read more"**. The server marks a body as truncated: `profile.Payload` gained
`preview_truncated`, computed in the *same* SQL expression that cuts the excerpt (`length(body) > 150`), and
the feed's `storyResponse` gained one computed in Go (`bodyExceedsPreviewWindow`, by characters, matching
SQL's `length`). `FeedEntry` renders a brand-ink "Read more" link under a truncated preview, wired to the
entry's own handler — so the link and the card open the same story, and a tap that misses the link still
opens the card. The server's flag wins when it sends one; when it is absent the link is driven by the
excerpt's own trailing `…` (`isPreviewTruncated`), which is what the profile wall's SQL excerpt already
carries.

The Create Story screen was the last unmigrated form. It now uses the design system: the shared `Header`
(a Cancel, because the screen is a tab), a `typeScale.h1` headline with a `body` subtitle, an `overline`
label above each control, the underline `Input`s, `Chip`s for Wonder/Heritage, the existing language, place
and media pickers, and a full-width primary `Button` "Publish story". The media strip's tiles are
`bg.sunken` with a `radius.md` frame, the "add" slot is a dashed hairline with an indigo `Plus`, and a
picked video shows a `FilmStrip` glyph instead of a text arrow. That file's **21** deprecated-typography
references and **16** deprecated colour aliases went to **0** (so the 35 such references left across
`src/screens` are now 21, in Login, Register and Rooted Setup — parts 8-10). `Input`'s `keyboardType` union
gained `'url'` so the media-link field keeps its keyboard.

**KNOT-020g-fix13** made video auto-play **app-wide** and centred the shared `Header`'s title.
Auto-play used to be the Feed's alone (KNOT-ADR-068); the rule is now shared. A new
`src/utils/viewability.ts` exports `VIEWABILITY_CONFIG` (`itemVisiblePercentThreshold: 60`), a
structural `ViewableItemsInfo` type, and `activeViewableKey(info)` — the first viewable item's key as
a string, or `undefined` when nothing is visible. Every screen that draws a story's video drives
`FeedVideo` from it: the Feed, **Place Stories**, and the **profile wall**, each holding one
`activeItemId`, one screen-wide `isMuted` (default `true`) and one `toggleMute`. So at most one video
plays per screen, a video pauses when it scrolls out, and tapping it opens the story the card already
opens — the mute control is a nested `Pressable`, so it does not navigate. The profile card's
display-only preview mode is gone: `FeedVideo` lost its `previewOnly` prop along with its last caller,
so the wall plays video the way the Feed does. The screens the brief listed that do **not** draw
video — Search, Inquiries, the language tree and version detail — carry no wiring, because their
endpoints return no media, and the Story Detail's media tiles keep their explicit tap-to-play control
inside a horizontal carousel rather than a viewability list. In the shared `Header` the title is now
drawn **out of flow**, centred on the bar rather than on the space left between the two side slots, so
it sits on the screen's centre whatever those slots hold; the `centered` prop, which only the Profile
tab passed, is gone with it. Place Stories' map strip plots the brand's filled indigo **`MapPin`**
(24pt, centred in a transparent 44-point box) instead of a bare dot; the Feed header nudges the
logomark 8 points right, onto the feed's 16-point content column; Search gained a funnel-labelled
filter group, the `no-results` artwork on both of its empty states, and a quoted
`No results for '<phrase>'.` headline; and Inquiries moved "Ask a question" out of the header into a
full-width primary `Button` beneath it.

**KNOT-020g-fix14** closed the Discovery screens' remaining parity gaps and migrated the last three
screens. On **Place Stories** the map strip now paints **no marker at all**: the `MapPin` added in
fix13 was removed on the founder's instruction, along with its constants and the `PointAnnotation`
import, so the strip is the map's own place label over the coordinate the camera is centred on.
Mapbox's **attribution button is back on** (`attributionEnabled`, bottom-right; the logo keeps the
bottom-left), which closes the compliance gap this screen had carried since fix10/fix11 — the two
marks sit in the strip's lower corners, clear of the floating back control. The screen's pillar
control is now the **Discovery Map's chip row** instead of underline tabs, and both screens offer the
pillars in the same order (**All / Wonder / Heritage**): `PILLAR_TABS` in `discoveryFormat.ts` is the
one list, so the two screens cannot drift. The summary *line* still reads Heritage before Wonder,
which is the wording the design reference prints — a different thing from the order of the controls.

The last three screens left on deprecated tokens are migrated: **Login**, **Register** and **Rooted
Setup**. All typing now goes through `typeScale`, every colour through a semantic role, and the
controls are the design system's — `Input` for the fields (the one text field of the design system,
so the auth screens now read like every other form in the app), a primary `Button` whose `loading`
state replaces the hand-rolled "Signing in…"/"Creating account…"/"Saving…" labels, `Chip` for Rooted
Setup's duration buckets, and a brand-coloured switch track. That is **21** deprecated-typography
references removed (Login 6, Register 6, Rooted Setup 9 — three of the last, in a dead
`styles.input` that was never referenced, deleted with them) and **21** deprecated colour aliases
replaced (6 / 6 / 9), so **`src/screens` is now clean of both**. The shared *components* are not:
`ReportModal`, `LocationPicker`, `LanguagePicker`, `CameraCaptureBadge`, `ReportAction` and
`MediaGalleryModal` still read the deprecated tokens, and two of them (`LanguagePicker` on Register,
`LocationPicker` on Rooted Setup) are embedded in the screens migrated here — migrating the
components is a separate task. Two things were deliberately *not* swapped: no `Header`
was added (it would move each screen's title into a bar, which is a layout change rather than a token
migration), and `Input` gained no `maxLength` — Register's two capped fields clamp inside their
change handlers instead, which is what `maxLength` enforced. `KNOT-ADR-068`'s stale "the Feed is the
only screen that auto-plays" sentence is corrected in place (the decision itself still holds, so it is
not marked superseded).

**KNOT-020g-fix15** finished the token migration. The six shared components that still read the
deprecated tokens — `ReportModal`, `LanguagePicker`, `LocationPicker`, `MediaGalleryModal`,
`CameraCaptureBadge` and `ReportAction` — are now on `typeScale` composites and the semantic colour
roles, so **both `src/screens` and `src/components` are clean** of `typography.*` / `fontSizes.*` /
`lineHeights.*`, of every deprecated colour alias, and of the mis-cased `fontWeights.semiBold` /
`extraBold` spellings. The two pickers matter most: they are drawn inside the already-migrated Register
and Rooted Setup screens, so those screens were clean in source but half-migrated on screen. The change
is **token-level only** — no prop, no behaviour, no structural change — but it does align three things
the design system fixes: a primary fill now takes `colors.text.onBrand` (the report modal's Submit
label, which had been dark-on-indigo), the report modal's scrim is `colors.scrim.overlay` rather than a
literal `rgba(0, 0, 0, 0.6)`, and `MediaGalleryModal`'s `spacing['3xl']` reads are the canonical
`spacing.xxxl`. The pickers deliberately keep their own boxed fields instead of adopting the design
system's underline `Input`, because that would be a visual redesign rather than a token change.

The same screen serves the owner: `isOwnProfile` adds the avatar upload, the Set Rooted
shortcut, and sign-out, so tapping your own author line opens your wall. `AuthorLine`'s
`onPress` is how the app reaches it — a nested `Pressable` handles the author tap without
firing the parent card's (KNOT-ADR-043).

**It is also the Profile tab** (KNOT-ADR-044): `App.tsx` renders it for `case 'profile'` with
`isOwnProfile` and `showBackButton={false}`, because a tab is a destination rather than a
pushed screen, and pushes a `userProfile` overlay for every author tap. The former
`ProfileScreen` (the Rooted-centric view) is deleted, so there is one definition of "profile".
The wall shows the current Rooted badge and a Set Rooted action, but not the list of Rooted
signals the old screen had; that list is not reachable in the app today.

### Languages (`src/data/languages.ts`, `LanguagePicker`)

A language is an ISO 639-3 code — three lower-case letters — and the canonical list of them is
the single source of truth for what the API accepts (KNOT-ADR-046). It exists twice,
deliberately:

- `backend/go/internal/language/languages.go` — 7,927 entries, sorted by name, served
  publicly by `GET /languages`. Every domain validates with `language.IsValid(code)`.
- `apps/mobile/src/data/languages.ts` — the same 7,927 entries, for the picker.

**Both are generated, and the two must stay identical.** Do not edit the entries by hand.
They come from the SIL International ISO 639-3 reference table, and
`tools/generate_languages.py` rewrites the block between the `BEGIN GENERATED` and
`END GENERATED` markers in each file, leaving the prose and the hand-written helpers around
them alone. To regenerate:

```bash
curl -sSL -o /tmp/iso-639-3.tab https://iso639-3.sil.org/sites/iso639-3/files/downloads/iso-639-3.tab
python3 tools/generate_languages.py --source /tmp/iso-639-3.tab
```

The script refuses a table that does not look right — a code that is not three lower-case
letters, a duplicated code or name, fewer than 7,000 entries — so a truncated download fails
loudly instead of quietly shrinking the list. It keeps only `Id` and `Ref_Name`: no scope, no
language type, no ISO 639-2 aliases. Names are the registry's reference names, verbatim,
including `Pedi` for `nso`.

`src/data/__tests__/languages.test.ts` reads the Go source and compares the two lists entry by
entry, so a change to one without the other is a red test rather than a subtle production bug.
That check skips itself when only the mobile folder is checked out (a mobile-only CI job still
runs the rest of the suite).

Because the list is generated, **adding a language is not a code change at all**: it is a new
revision of the registry table plus a regeneration. Do not hand-add an entry, do not add a
region variant (`pt-BR`), and do not invent a code — if the registry does not name it, the API
will not accept it.

`apps/mobile/src/components/LanguagePicker.tsx` is the one way a form asks for a language. It
takes `mode: 'single' | 'multiple'` and searches the list **in memory** — no request, and no
free text that could become a stored value. Use it rather than a `TextInput`: Register uses
`multiple`, and Create Story and Adapt Story use `single`. Display a stored code with
`languageName(code)`, which falls back to the raw code for a value the list does not know; do
not render `"zul"` where a person expects "Zulu".

A search over 7,927 names returns several matches for a short query — `zul` also matches
`Zulgo-Gemzek` — which is why the picker caps the result list and why `findLanguage` and
`languageName` go through a `Map` index rather than scanning.

The comment composer on `CommentThreadScreen` now uses the same picker. A fixed-height
composer bar has no room for a results list, so the composer shows the language as a **chip**
and tapping it opens `LanguagePicker` in `single` mode inside a modal sheet
(KNOT-ADR-048). The chip reads `languageName(code)` when that name is at most 12 characters
and the code itself otherwise — 1,630 of the 7,927 names are longer than the chip — which is
`languageChipLabel` in `apps/mobile/src/screens/conversations/commentThread.ts`. That module
holds the thread's pure rules — the chip label, the reply target, the starting language, how a
flat page is grouped into parents and replies, and what the replies link reads — so they are
tested without rendering React Native.

The composer starts in the user's first preferred language. A user who has none has no
preferred tag to send, so the composer fetches the version (`GET /versions/{id}`) and uses
the version's language, showing `…` in the chip while the request is in flight and falling
back to the app-wide default if it fails — `initialComposeLanguage` encodes the order. The
choice is **not** persisted: it lasts for the visit, and the profile keeps its own screen
(KNOT-ADR-048).

**Every language default in the app is a three-letter ISO 639-3 code.** The single canonical
fallback lives in `apps/mobile/src/config/language.ts` as `DEFAULT_LANGUAGE_CODE = 'eng'`.
Import it rather than writing a literal, and never seed or fall back to a two-letter ISO
639-1 code such as `'en'` — the server validates with `language.IsValid` and rejects
anything that is not a canonical three-letter code. A stored preference that is not a
canonical code is ignored (the code paths check `isLanguageCode`) rather than forwarded.
`src/config/__tests__/language.test.ts` scans the source tree and fails if a two-letter code
reappears as a default (KNOT-016-fix).

The thread renders **top-level comments only**. A comment's replies are collapsed under a link
above its action buttons and reveal **in place** when tapped (KNOT-ADR-049): the first tap shows
the first three, a second tap shows the rest, and the next tap hides them again. `repliesLinkLabel`
computes the link — `View 1 reply`, `View N replies`, `View all N replies`, or `Hide replies` —
and `groupComments` splits the flat page the API returns into `topLevel` plus `repliesByParent`. The
expansion is held in the screen's `useState` (`expandedIds` and `fullyExpandedIds`, two
`Set<string>`s updated immutably) and is session-only: leaving the thread forgets it. A reply is
rendered compact — a small `AuthorLine`, a smaller body, and the same Reply action,
indented inside its parent's card. Tapping **Reply** focuses the composer
(`composerRef.current?.focus()`) so the keyboard opens ready to type, and the payload still names
the top-level comment through `replyTargetId`.

### Perspective reactions

`internal/reactions` owns the four perspective signals (KNOT-ADR-050). It is layered like the
other content domains — `reaction.go` (the closed sets, `Summary`, the store contract),
`service.go` (Toggle, ListForEntity, SummaryForEntity, BatchSummaries, BatchMyReactions), and
`postgres_store.go` — and it knows nothing about HTTP, JSON, or any content domain. Entity
existence is the caller's job: `entity_id` is polymorphic and has no foreign key, so the HTTP
layer resolves the target through its own domain service first.

**Endpoints.** One toggle and one list per entity kind, registered in `router.go`:

```
POST /stories/{id}/reactions      POST /versions/{id}/reactions
POST /comments/{id}/reactions     POST /inquiries/{id}/reactions
GET  /stories/{id}/reactions      GET  /versions/{id}/reactions
GET  /comments/{id}/reactions     GET  /inquiries/{id}/reactions
```

The toggle takes `{"reaction_type":"rings_true"}` and answers with the entity's updated
`{"reactions":{…}}`; it is protected. The list is public and returns the actors. The toggle
resolves the entity through `stories`/`versions`/`conversations` (a `404` for a missing entity),
refuses a reply with a `400` (KNOT-ADR-052), and fires `NotifyReactionCreated` only when the
signal was **created** on **someone else's** content.

**Enrichment.** Every content response carries `reactions` (all four counts) and `my_reactions`
(the reader's own signals) — see `ReactionsLookup` and `loadReactions` in `internal/httpapi/enrich.go`.
A page costs **two batched reads** regardless of size, so a feed, tree, thread, or inquiry list
never fans out into a query per row. The public content reads are wrapped in
`AuthMiddleware.Optional`, so a valid token fills `my_reactions` and an absent or invalid one
yields `[]`: the routes stay public (KNOT-ADR-051). `notifications` gained a nullable
`reaction_type` column so an inbox row can name the signal.

**Comments are read-only for reactions.** `POST /comments/{id}/reactions` answers **400**
`validation_error` ("reactions are not supported on comments") and writes nothing, because the
comment thread already carries a replies link and Reply and a bar overloaded it
(KNOT-ADR-053). `GET /comments/{id}/reactions` still answers, and comment responses still carry
`reactions` and `my_reactions`, so the existing rows stay readable and auditable; the mobile
`Comment` type simply no longer declares those fields because nothing renders them.

**Mobile.** `src/api/reactions.ts` wraps the eight routes and owns the labels;
`src/components/ReactionBar.tsx` renders the four perspective signals — Rings true, Know it
differently, Adds something new, and Needs a source — as equal columns drawn with the brand's
own Reaction glyphs (`src/assets/brand/reaction-*.svg`, tinted through `currentColor`), toggles
optimistically, reverts on error, and invites a signed-out reader to sign in. Its `entityType`
accepts `story`, `version`, and `inquiry`. It is applied to the story detail (below the body) and
each version row in the language tree (compact: glyph and count only). It is **not** applied to
comments: the comment thread already carries a replies link and Reply, so a bar
overloaded it and the comment toggle route answers **400** (KNOT-015g / KNOT-ADR-053). The inbox
renders `reaction.created` as "{actor} ✅ Rings true on your comment." and opens the target — a
story, version, comment, or inquiry — through the existing tap-through routing.

### Native dependencies (Mapbox, AsyncStorage)

The Discovery Map uses **`@rnmapbox/maps`** (KNOT-011a, KNOT-ADR-026) — the app's first native
**map** dependency. The JavaScript layer (lint, typecheck, and Jest) runs with no native
build, so day-to-day work and CI are unaffected. The native projects are generated (KNOT-008a);
these are the setup steps a native build needs:

- **Android:** the native Mapbox SDK comes from Mapbox's own Maven repository and needs a
  **secret** downloads token on the machine — see [Mapbox setup](#mapbox-setup) below. The map's
  own access token is the **public** one and needs no extra step.
- **iOS:** after `npm install`, run `cd apps/mobile/ios && pod install` before opening the
  project in Xcode. Pods must be reinstalled whenever `@rnmapbox/maps` or React Native
  itself changes.
- The **native projects exist** (`apps/mobile/ios/` and `apps/mobile/android/`, added in
  KNOT-008a), so the map screen has a native host; see [Running the mobile app](#running-the-mobile-app)
  below.
- The map resolves place names with a **local lookup table**
  (`apps/mobile/src/data/placeCoordinates.ts`), not a geocoding service, so no geocoding key is
  needed — only the Mapbox map token, which is a public client token.

**`@react-native-async-storage/async-storage`** (KNOT-009) is the second native module. It is
installed by `npm install` and needs no key or configuration, but because it is native it
changed the native app: after pulling this change, run `npm install` and then **rebuild** —
`npx react-native run-android` on Android (run `pod install` in `ios/` first on iOS). A Metro
reload alone is not enough.

**`react-native-image-picker`** and **`react-native-video`** (KNOT-013, KNOT-ADR-031) are the
media modules: the picker covers camera and gallery for photos and videos, and the player
streams story videos. Both are native, so after `npm install` the Android and iOS apps must be
**rebuilt**, exactly like AsyncStorage. There are no keys to configure.

Android camera note: the picker deliberately requires **no** `CAMERA` permission — declaring it
would force a runtime permission request and a `SecurityException` if it were denied, which is
why the app does not declare it. The only manifest change is a `<queries>` block for the
Android 11+ capture intents (`IMAGE_CAPTURE`, `VIDEO_CAPTURE`), so the camera app stays
resolvable. The gallery picker uses the Android Photo Picker and needs no storage permission.
On iOS the picker needs `NSCameraUsageDescription`, `NSPhotoLibraryUsageDescription`, and
`NSMicrophoneUsageDescription`, all declared in `ios/Knot/Info.plist`.

Camera capture and video playback can only be exercised on a physical device — a simulator has
no camera, and video playback needs an object the backend is actually serving.

### Mapbox setup

The Discovery Map renders with Mapbox (KNOT-ADR-026). Two different tokens are involved, and
they are easy to confuse:

- **Public access token (`pk.…`)** — the token the app uses at runtime to draw tiles. Public
  tokens are safe to ship inside a client app, but this one is **not committed**: it lives in
  the gitignored local files `apps/mobile/src/config/secrets.local.ts` and
  `apps/mobile/android/app/src/main/res/values/mapbox.xml` (KNOT-ADR-027). See
  [Local secrets setup (after cloning)](#local-secrets-setup-after-cloning) below. If it is ever
  abused, rotate it in the Mapbox console and update both local files. The token needs the
  **`mapbox.places`** scope in addition to the default styles scope: the location picker calls
  the Mapbox Geocoding API directly from the client to turn a typed place into a coordinate
  (KNOT-ADR-035). Without that scope the picker shows "Could not search" and a story cannot gain
  a location.

- **Secret downloads token (`sk.…`)** — used **only by Gradle** to download the native Mapbox
  SDK from Mapbox's Maven repository. It is a credential and **must never be committed**. It
  lives in the developer's user-level `~/.gradle/gradle.properties`:

  ```properties
  MAPBOX_DOWNLOADS_TOKEN=sk.xxxxxxxx.yyyyyyyy
  ```

  Create it in the Mapbox console with the **`downloads:read`** scope. `android/build.gradle`
  reads it through `project.properties['MAPBOX_DOWNLOADS_TOKEN']` and uses it as the HTTP Basic
  password for `https://api.mapbox.com/downloads/v2/releases/maven`.

Without that secret token Gradle cannot resolve `com.mapbox.maps:android` and the Android build
fails with a `Could not find com.mapbox.maps:android` error. The secret is a one-time,
per-machine setup — it is not needed for lint, typecheck, Jest, or Metro. After this change, a
first native build is `npm install` (to fetch `@rnmapbox/maps`) followed by
`npx react-native run-android`.

### Local secrets setup (after cloning)

Two client-side config files hold the Mapbox **public** token. Neither is committed; each is
created locally by copying its committed `.example` template and filling in the real value
(KNOT-ADR-027). Run this once after cloning.

| Create this local file (gitignored) | By copying this template (committed) |
| --- | --- |
| `apps/mobile/src/config/secrets.local.ts` | `apps/mobile/src/config/secrets.example.ts` |
| `apps/mobile/android/app/src/main/res/values/mapbox.xml` | `apps/mobile/android/app/mapbox.example.xml` |

```bash
cd apps/mobile
cp src/config/secrets.example.ts src/config/secrets.local.ts
cp android/app/mapbox.example.xml android/app/src/main/res/values/mapbox.xml
```

Then replace `pk.REPLACE_WITH_YOUR_PUBLIC_MAPBOX_TOKEN` in **both** files with the public token
from the Mapbox console (Account → Tokens). The two files must stay in sync.

- **Never commit** either local file — both are in `.gitignore`. Only the `.example` templates
  are tracked.
- `apps/mobile/src/config/secrets.local.d.ts` **is** committed: it declares the local module's
  shape so `npm run typecheck` still succeeds on a fresh clone before `secrets.local.ts` exists.
  Metro (and the Android resource merger) still need the real files, so create them before
  running `npm start` or `npx react-native run-android`.
- **Jest uses a module mock** so the test suite also runs on a fresh clone where
  `secrets.local.ts` does not exist (KNOT-018-fix). The committed
  `apps/mobile/src/config/__mocks__/secrets.local.ts` supplies a placeholder token, and
  `jest.config.js` maps every import of `./secrets.local` to it via `moduleNameMapper`. The mock
  wins even when the real local file is present, so tests behave identically on every machine.
  **Keep the mock in sync with the shape of `secrets.local.ts`**: any new exported constant must
  be added to the mock too, or a test that imports it will see `undefined`.
- These are deliberately plain TypeScript and Android XML rather than `.env` files: the bundler
  and Android's resource merger already understand them, so no extra tooling or dependency is
  needed.
- The Android template lives at `apps/mobile/android/app/mapbox.example.xml`, **outside** the
  `res/` tree on purpose: Android compiles every XML file under `res/values/`, so an `.example`
  file placed there would define `mapbox_access_token` a second time and the build would fail
  with `Duplicate resources`. Only the gitignored copy in `res/values/` is a real resource.

### Session persistence

The signed-in session is **persisted across restarts** (KNOT-009, KNOT-ADR-024). `App.tsx`
loads it from AsyncStorage under the key `knot.session.v1` on launch and restores it before
rendering, so a returning user goes straight to the feed instead of the login screen. A
successful login or register saves the session; signing out clears it.

- The stored value is `{ accessToken, refreshToken, user }` as JSON, written and read by
  `apps/mobile/src/session/session.ts`. Storage is best-effort: a missing or corrupted value is
  logged and treated as "not signed in" rather than crashing the app.
- The session lives in **AsyncStorage, which is unencrypted**. This is acceptable only because
  there is no sensitive data yet; moving to encrypted storage before there is any is tracked in
  KNOT-ADR-024.
- **Login is the default auth screen**; a "Don't have an account? Create one" link switches to
  Register, which links back to Login.
- There is **no token refresh** yet: when the access token expires the user must sign in again.
  Auto-refresh is a future task.

### Running the mobile app

The app is **bare React Native**, not Expo (KNOT-ADR-002): it has real `ios/` and `android/`
projects and native modules (`@rnmapbox/maps`, `@react-native-async-storage/async-storage`), so **Expo Go cannot run it**. A native build
is required to see the app on a device or simulator.

Prerequisites:

| Target  | Requires                                                                                     |
| ------- | -------------------------------------------------------------------------------------------- |
| iOS     | macOS, **Xcode** (the full app, not just Command Line Tools), and **CocoaPods**               |
| Android | **Android Studio** (SDK + platform-tools), a JDK, and a device or emulator                    |
| Both    | Node.js 20+ and the project dependencies (`npm install`)                                       |

Install dependencies once after cloning — this also installs the CLI and Metro packages the
native build uses:

```bash
cd apps/mobile
npm install
```

**Start Metro** (the JS bundler) in its own terminal; both platforms need it running:

```bash
cd apps/mobile
npm start          # npx react-native start
```

**iOS:**

```bash
cd apps/mobile/ios
pod install                    # once after cloning, and after native deps change
cd ..
npx react-native run-ios        # or: npm run ios
# or open the workspace in Xcode and press Run:
# open ios/Knot.xcworkspace
```

`@rnmapbox/maps` is picked up by CocoaPods autolinking (`use_native_modules!` in
`ios/Podfile`), so no extra Podfile entry is needed. `ios/Knot.xcworkspace` and
`ios/Podfile.lock` are both generated by `pod install` and are git-ignored until the first
successful build, after which the founder commits `Podfile.lock` deliberately.

**Android:**

```bash
cd apps/mobile
npx react-native run-android    # or: npm run android
```

The map renders with **Mapbox** (KNOT-ADR-026), so no **Google Maps API key** is needed; the
committed `google_maps_api_key` placeholder and its `AndroidManifest.xml` meta-data are left in
place but unused. Mapbox's **secret** downloads token must be present in
`~/.gradle/gradle.properties` for the Android build to resolve the native SDK — see
[Mapbox setup](#mapbox-setup). `android/local.properties` (which holds the SDK path) is
git-ignored.

For a **physical device**, see
[Running the mobile app on Android (physical device)](#running-the-mobile-app-on-android-physical-device)
below.

On both platforms the app is named **Knot** and the bundle identifier / application id is
**`com.knot.app`**.

Finally, point the app at the backend. The API base URL comes from
`apps/mobile/src/config/dev.ts` (read by `apps/mobile/src/config/api.ts`), a single constant
`DEV_API_URL` that is the one place to change. The correct value depends on where the app runs:

| Where the app runs | Set `DEV_API_URL` to        | Why                                         |
| ------------------ | --------------------------- | ------------------------------------------- |
| iOS Simulator      | `http://localhost:8080`     | The simulator shares the Mac's loopback     |
| Android emulator   | `http://10.0.2.2:8080`      | The emulator's alias for the Mac's loopback |
| Physical device    | `http://<Mac Wi-Fi IP>:8080` | The phone reaches the Mac over the LAN     |

The address is **not a secret and not production configuration** — it is a per-developer,
per-network value that changes with the Wi-Fi network, which is why it lives in one small file and
is committed as a convenience default. `apps/mobile/src/config/api.ts` still lets a build-time
`KNOT_API_URL` (if a future build inlines one) take precedence without editing source.

> **On-device verification of the map is expected after this task.** The map is a native module,
> so it can only be confirmed by a real build; lint, typecheck, and Jest exercise the JS layer
> only.

### Running the mobile app on Android (physical device)

This is the full, verified procedure for running the app on a physical Android phone (it was
confirmed on a Samsung Galaxy A13, Android 14). The emulator path is identical except for the
backend URL.

**Prerequisites**

- **JDK 17** (Temurin or equivalent). Confirm the JVM Gradle will use:

  ```bash
  /usr/libexec/java_home -v 17
  ```

- **Android SDK** with **API 35** (`compileSdk 35`), **build-tools 34/35**, `platform-tools`,
  `emulator`, and `cmdline-tools`. Install via Android Studio, or with `sdkmanager`, e.g.
  `sdkmanager "platforms;android-35" "build-tools;35.0.0"`.
- **Environment variables** so the CLI and Gradle can find the toolchains:

  ```bash
  export ANDROID_HOME="$HOME/Library/Android/sdk"
  export JAVA_HOME="$(/usr/libexec/java_home -v 17)"
  export PATH="$ANDROID_HOME/platform-tools:$ANDROID_HOME/emulator:$ANDROID_HOME/cmdline-tools/latest/bin:$PATH"
  ```

  Add these to your shell profile to make them persistent. `apps/mobile/android/local.properties`
  (if present) records `sdk.dir` and is git-ignored.

**Steps**

1. **Enable USB debugging** on the phone: Settings → About phone → tap *Build number* seven times,
   then Settings → Developer options → turn **USB debugging** on.
2. **Connect the phone by USB** and accept the *Allow USB debugging?* prompt on the device.
3. **Confirm the device is visible:**

   ```bash
   adb devices
   ```

   The phone should be listed with state `device` (not `unauthorized` or `offline`). If it shows
   `unauthorized`, re-accept the prompt on the phone.

4. **Start Metro** in its own terminal (both platforms need it running):

   ```bash
   cd apps/mobile
   npm start
   ```

5. **Build and install onto the device:**

   ```bash
   cd apps/mobile
   npx react-native run-android    # or: npm run android
   ```

   The first build downloads Gradle and the Android dependencies and is slow; later builds are
   fast. If the Gradle wrapper download times out,
   `android/gradle/wrapper/gradle-wrapper.properties` sets `networkTimeout=120000` for exactly
   this reason.

**Point the app at the backend**

The app reads its base URL from the single constant `DEV_API_URL` in
`apps/mobile/src/config/dev.ts`:

- **iOS Simulator:** `http://localhost:8080`
- **Android emulator:** `http://10.0.2.2:8080`
- **Physical Android device:** your Mac's Wi-Fi address, e.g. `http://10.27.80.173:8080`

Find the Mac's current Wi-Fi address with:

```bash
ipconfig getifaddr en0
```

The phone and the Mac **must be on the same Wi-Fi network** — the phone reaches the Mac over the
LAN, and `localhost` on the phone means the phone itself. The address **changes whenever the Mac
joins a different network**, so update `DEV_API_URL` in `apps/mobile/src/config/dev.ts` and reload
the app when it does; it is the only place to change.

**Notes**

- **No Google Maps API key is needed.** The map renders with **Mapbox** (KNOT-ADR-026); the
  Google Maps SDK and its billing requirement are not used. Mapbox's **secret** downloads token
  must be in `~/.gradle/gradle.properties` for the build to resolve the native SDK.
- **The New Architecture is disabled** (`newArchEnabled=false` in `android/gradle.properties`,
  KNOT-ADR-021). This is deliberate: it avoids the ~1.5 GB NDK download the New Architecture
  requires on Android. See ADR-021 for when to revisit it.
- Confirming the map renders is an **on-device** step — it is a native module and cannot be
  verified by lint, typecheck, or Jest.

## Backend (`backend/go/`)

```bash
cd backend/go
gofmt -l .          # should print nothing
go vet ./...
go test ./...
go test ./... -race
```

### Running the backend locally

The backend needs a reachable PostgreSQL. Start the local infrastructure first:

```bash
scripts/dev-up.sh                # Postgres on host port 5433, Redis on 6379
cd backend/go
go run ./cmd/knot migrate up     # apply pending migrations
go run ./cmd/knot                # serve the API on :8080
```

`knot` with no arguments starts the HTTP API. `knot migrate up` applies pending
migrations and exits. Migrations are **never** applied automatically when the server
starts, because schema changes should stay an explicit action.

The server logs its startup line, then one line per request containing `request_id`,
`method`, `path`, `status`, and `duration_ms`. Tokens, passwords, and DSNs are never
logged. It shuts down gracefully on SIGINT or SIGTERM, draining in-flight requests for
up to 10 seconds.

To try it by hand:

```bash
curl -s http://localhost:8080/health
curl -s -X POST http://localhost:8080/auth/register \
  -H 'Content-Type: application/json' \
  -d '{"email":"you@example.com","password":"at-least-8-chars","display_name":"You","agreed_to_terms":true}'
```

### Routes

| Route                 | Auth     | Handler file                  |
| --------------------- | -------- | ----------------------------- |
| `GET /health`         | public   | `internal/httpapi/router.go`  |
| `POST /auth/register` | public   | `internal/httpapi/auth_handler.go` |
| `POST /auth/login`    | public   | `internal/httpapi/auth_handler.go` |
| `POST /stories`       | `Bearer` | `internal/httpapi/stories_handler.go` |
| `GET /stories`        | public   | `internal/httpapi/stories_handler.go` |
| `GET /stories/{id}`   | public   | `internal/httpapi/stories_handler.go` |
| `POST /stories/{id}/adapt` | `Bearer` | `internal/httpapi/versions_handler.go` |
| `GET /stories/{id}/tree`   | public   | `internal/httpapi/versions_handler.go` |
| `GET /versions/{id}`       | public   | `internal/httpapi/versions_handler.go` |
| `POST /versions/{id}/comments` | `Bearer` | `internal/httpapi/conversations_handler.go` |
| `GET /versions/{id}/comments`  | public   | `internal/httpapi/conversations_handler.go` |
| `GET /comments/{id}`           | public   | `internal/httpapi/conversations_handler.go` |
| `POST /users/me/rooted`        | `Bearer` | `internal/httpapi/rooted_handler.go` |
| `GET /users/me/rooted`         | `Bearer` | `internal/httpapi/rooted_handler.go` |
| `GET /users/{id}/rooted`       | public   | `internal/httpapi/rooted_handler.go` |
| `GET /discovery/clusters`      | public (optional auth) | `internal/httpapi/discovery_handler.go` |
| `GET /discovery/places/{place}` | public (optional auth) | `internal/httpapi/discovery_handler.go` |
| `POST /users/me/avatar`        | `Bearer` | `internal/httpapi/avatar_handler.go` |
| `GET /users/{id}/avatar`       | public   | `internal/httpapi/avatar_handler.go` |
| `GET /notifications`           | `Bearer` | `internal/httpapi/notifications_handler.go` |
| `GET /notifications/unread_count` | `Bearer` | `internal/httpapi/notifications_handler.go` |
| `POST /notifications/{id}/read` | `Bearer` | `internal/httpapi/notifications_handler.go` |
| `POST /notifications/read_all` | `Bearer` | `internal/httpapi/notifications_handler.go` |
| `POST /inquiries` | `Bearer` | `internal/httpapi/inquiries_handler.go` |
| `GET /inquiries` | public (optional auth) | `internal/httpapi/inquiries_handler.go` |
| `GET /inquiries/{id}` | public (optional auth) | `internal/httpapi/inquiries_handler.go` |
| `POST /inquiries/{id}/answers` | `Bearer` | `internal/httpapi/inquiries_handler.go` |
| `GET /inquiries/{id}/answers` | public (optional auth) | `internal/httpapi/inquiries_handler.go` |
| `GET /answers/{id}` | public (optional auth) | `internal/httpapi/inquiries_handler.go` |
| `POST`/`GET /inquiries/{id}/reactions` | Bearer / public | `internal/httpapi/reactions_handler.go` |
| `GET /search` | public (optional auth) | `internal/httpapi/search_handler.go` |

Routes are declared in one place, `NewRouter` in `internal/httpapi/router.go`, using
Go 1.22 method-qualified `ServeMux` patterns. There is no router dependency.

Authentication is **per route**, not global: `POST /stories` is wrapped in
`AuthMiddleware.Require`, and the public routes are not. A handler that needs the caller
reads `UserIDFromContext(ctx)`; the second return value is `false` when no authenticated
user is on the context, which handlers must treat as unauthenticated. Because protection
travels with the route, adding a protected route means wrapping it explicitly.

`GET /stories/{id}` uses a wildcard segment, so be aware that `/stories/` (with a
trailing slash) does **not** match it — it is a 404, and the handler is never reached.
There is a test for exactly that, because the behaviour is easy to assume wrongly.

The composition root is `cmd/knot/main.go`: it builds the pool, the stores, the services,
the handlers, and the middleware, then hands them to `NewRouter`. That is the only place
that knows how the layers are wired together.

### Tell My People: versions and the Language Tree

`POST /stories/{id}/adapt`, `GET /stories/{id}/tree`, and `GET /versions/{id}` are served
by `internal/httpapi/versions_handler.go`, backed by the `internal/versions` domain
package. A story's content is a version, and every story has a root version; an adaptation
descends from a version through `parent_version_id`.

The tree is an adjacency list, so the tree query is deliberately flat and uses no
recursion:

```sql
SELECT id, story_id, parent_version_id, author_id, language, title, body, adaptation_note, created_at, updated_at
FROM story_versions
WHERE story_id = $1
ORDER BY created_at ASC, id ASC
```

Because a parent is always created before its children, `created_at ASC` returns a version
before every adaptation of it, which is a valid depth-first presentation. The client
assembles the nesting from `parent_version_id`; the server neither sorts for a display nor
returns a nested shape. `ListByStory` returns `ErrNotFound` when the story does not exist,
which is how `GET /stories/{id}/tree` tells "no such story" from "a story with no
versions".

A story and its root version are written together in one statement (two data-modifying
CTEs) because `stories.root_version_id` and `story_versions.story_id` reference each other
and neither row can be inserted first under an immediate foreign key. That single statement
is the transaction: a failure while writing the root version leaves no story behind.

Adapting is open to any authenticated user. The adapter comes from the token, and the only
structural check is that `parent_version_id` belongs to the same story (see KNOT-ADR-013).

#### Hereditary attribution (`internal/versions/attribution.go`)

Every version response carries an `attribution_line` and a `version_depth`, and neither is a
column: they are resolved on read from the parent pointers (KNOT-ADR-069). Attribution is
what makes Tell My People a human-adaptation mechanic rather than a translation tool — it
states who said what, adapted from whom, in what language — so the sentence is rendered on
the server and the client (the Story Version screen) shows it **verbatim**.

The module holds the whole resolver, and it is deliberately pure where it can be:

- `BuildChain(ctx, store, versionID)` walks up `parent_version_id` to the root, one store
  read per link, returning a `Chain` ordered root-first. It returns `ErrNotFound` for a
  missing version and `ErrChainCycle` for parent pointers that loop, and it bounds the walk
  so a malformed tree can never hang a request.
- `BuildChainFromVersions(targetID, rows)` resolves the same chain **entirely in memory**
  from rows a caller already holds. It is what the tree endpoint uses, so a page of N
  versions issues no attribution query at all.
- `FormatAttribution(chain)` turns a chain into the sentence. It is a pure function of the
  chain, with published wording for lineages of 1, 2, 3, and 4+ links (the 4+ form compresses
  the middle into a count). It degrades rather than gaping: a link with no resolved author
  reads "another author", and a missing or unrecognised language reads "their language".

The names are the HTTP layer's job, not the domain's: `chainWithNames` in
`internal/httpapi/enrich.go` fills each link's `AuthorDisplayName` from one batched author
read (`attributionText`, used by the version and adapt endpoints; the tree reuses the
page-wide attribution map it has already loaded). `version_depth` is the chain's length
minus one, so the root is 0 and a first adaptation is 1.

The story detail is the root version, so it needs no walk at all: `StoriesHandler.Get`
builds a one-link chain from the story's own author and root language — reusing the display
name the response has already resolved, so the attribution and the author line can never
disagree — and reports depth 0. The **feed** deliberately does not carry either field, and
the two fields are pointers with `omitempty` on `storyResponse` precisely so the feed's JSON
is byte-for-byte what it was before (attribution on the feed is KNOT-022's work).

### Conversations

`POST /versions/{id}/comments`, `GET /versions/{id}/comments`, and `GET /comments/{id}`
are served by `internal/httpapi/conversations_handler.go`, backed by the
`internal/conversations` domain package. A comment belongs to one story version.

Comment pagination reuses the feed's keyset pattern. The cursor is duplicated in
`internal/conversations/cursor.go` rather than shared, following KNOT-ADR-010, and the
query matches `comments_version_id_created_at_idx`:

```sql
SELECT id, version_id, author_id, language, body, parent_comment_id, created_at, updated_at
FROM comments
WHERE version_id = $1 AND parent_comment_id IS NULL
  AND (created_at, id) < ($2::timestamptz, $3::uuid)
ORDER BY created_at DESC, id DESC
LIMIT $4
```

`AND parent_comment_id IS NULL` is what makes the cursor still work: replies sort by their
own `created_at`, so interleaving them into the stream could place a reply on a page
before the comment it answers. **`limit` therefore counts top-level comments**, and a page
can return more rows than `limit`.

The page's replies come from one batched statement in `PostgresStore.ListReplies`, keyed
by the whole page (`comments_parent_comment_id_idx` serves it):

```sql
SELECT id, version_id, author_id, language, body, parent_comment_id, created_at, updated_at
FROM comments
WHERE parent_comment_id = ANY($1::uuid[])
ORDER BY created_at ASC, id ASC
```

`Service.attachReplies` groups that result by parent and emits each top-level comment
followed by its replies, so the wire format is a flat list a client renders in order and
never re-sorts. Replies are oldest first within a parent, matching how a thread reads.

Creating a **reply** passes `parent_comment_id`. `Service.CreateComment` resolves the
parent before writing and rejects one that does not exist or belongs to another version
with a `400 validation_error` on `parent_comment_id` — a `404` would say the version is
missing, which it is not. Threading is one level deep (KNOT-ADR-047): when the named
parent is itself a reply, the stored parent is rewritten to that reply's own parent, so a
reply is always attached to a top-level comment. The comment being **addressed** is kept
separately, because that is who gets the notification:

- top-level comment → notify the **version's author**
- reply → notify the **author of the comment replied to**

The recipient is resolved before the depth rewrite for exactly that reason. Acting on your
own comment still notifies nobody, and only one notification is written per comment.

### KNOT-023 — bridging retired, and three feed refinements

**Comment bridging is gone** (KNOT-ADR-071): the `bridge.go` domain file, the `BridgeStore`
contract, the bridge routes, the `bridge.created` event dispatch, the `BridgeScreen`, and
the mobile bridge API are all deleted. The `bridges` table is left **dormant** — no
migration, no data deletion — and `notifications.PostgresStore` hides historical
`bridge.created` rows in `List`, `UnreadCount`, and `MarkAllRead`, so the inbox and its
badge agree even though the schema still permits the value.

**Feed entry (`src/components/FeedEntry.tsx`).**

- A **sensitive** item once held its media behind a scrim (blurred image, covered video,
  and a badge that toggled the reveal). **The feature was removed in KNOT-023c** — the
  column is dropped and the card has no reveal state (KNOT-ADR-072, see the 023c note
  below).
- A version card shows its **inherited pillar** as a plain `PillarTag` beside the
  `Version N · Language` overline (the same Wonder/Heritage tokens a story card uses),
  because the pillar tells a reader which kind of story they are opening.
- The **attribution sentence** was moved into the card's header by KNOT-023a–c; it no
  longer sits under the title.

### KNOT-023a — the feed card's attribution header

A version card now **opens with who it descends from**: a small avatar and
"{name}'s original story" — the name in `typeScale.label` at `colors.text.primary`, the
possessive in `typeScale.caption` at `colors.text.tertiary` — then a one-pixel
`colors.border.divider` rule, then the poster's author block. A story card has no header:
its author block starts the card directly.

- The values come from **structured fields on the version feed item** —
  `root_author_id`, `root_author_display_name`, `root_author_avatar_url` — not from parsing
  `attribution_line`. The backend fills them from the first link of the lineage it already
  resolves for the sentence, so the fields cost no query. See `docs/API.md`.
- `colors.border.divider` is new and is **#D5D9E0**, one step darker than
  `border.hairline` (#E8EAEE). It marks a boundary *inside* an entry; hairlines still
  separate entries (and the Feed still draws one at the bottom of each).
- The header is inline in `FeedEntry`, not a mode of `AuthorLine`: it names a **second**
  person (the original author), and `AuthorLine`'s contract is one author's byline. The
  poster's line below it is still `AuthorLine`, unchanged for the other seven screens.
- The feed uses a **compact** relative time, `formatRelativeTimeShort` in
  `src/utils/time.ts`: `now`, `5m`, `3h`, `2d`, `2w`, `3mo`, `1y`. `m` is **minutes** and
  `mo` is **months**, so the two can never be confused. `AuthorLine` gained an opt-in
  `timeFormat` prop (default `long`); only `FeedEntry` passes `"short"`, so every other
  screen keeps its prose time.

### KNOT-023b — the feed card structure

The feed now reads as **white cards on a grey canvas**, separated by a thick gap rather
than a rule. The header and tab row stay on the white canvas above the list.

- **The gap.** `FeedScreen` sets its container to `colors.bg.sunken` and wraps every entry
  in a `styles.card` (`backgroundColor: colors.bg.canvas`, `marginBottom: spacing.sm`).
  `FeedEntry` gained `showBottomHairline?: boolean` (default `true`): the Feed passes
  `false`, and a `paddingBottom` keeps the meta line off the card's edge. Place Stories
  keeps the default, so its hairline-separated column is untouched.
- **The stacked author.** `AuthorLine` gained `layout?: 'inline' | 'stacked'` (default
  `'inline'`). Stacked puts the avatar on the left and a column on the right: name
  (`typeScale.label`), the quiet Rooted line (when present), then the compact time on its
  own line. `FeedEntry` passes `layout="stacked"` for both entry kinds; the other seven
  screens keep the inline byline.
- **The single-line header.** A version card opens with one caption line: an inline
  20pt root avatar (`Avatar size="xs"`) and the server's `attribution_line`, clamped to
  two lines. The sentence is drawn **verbatim** apart from the author span, which KNOT-023c
  splits out for the bold voice (see below). If the sentence is missing but the
  structured root author is present, the line falls back to
  `Adapted from {name}'s original story.`; with neither, the header is omitted. The old
  two-line header, its divider, and the attribution sentence under the title are gone.

### KNOT-023c — feed polish, and the sensitive flag is removed

- **The gap is 8pt.** `FeedScreen`'s `styles.card` now uses `marginBottom: spacing.sm`
  (was `spacing.md`), so the white cards sit closer while the grey canvas still separates
  them.
- **A hairline under the attribution.** On a version card, a 1px `colors.border.hairline`
  rule (`marginTop`/`marginBottom: spacing.sm`) now separates the attribution line from the
  poster's stacked author block.
- **The attribution name is bold.** `feedEntryMeta.attributionParts(line)` splits the
  server sentence around its first name — `Adapted from {name}'s …` or
  `Original story by {name} in …` — using non-greedy patterns so a deep lineage
  (`Adapted from Bongani's Zulu version of Ada's …`) bolds the person the sentence opens
  with, not a later one. `FeedEntry` renders the name in `typeScale.label` /
  `colors.text.primary` and the rest in `typeScale.caption` / `colors.text.tertiary`. An
  unexpected shape falls back to the plain sentence. The split is **display-only**.
- **The author stack is tighter.** `AuthorLine`'s `stackedDetails` dropped its
  `gap: spacing.xs`; the three tokens' own 16pt line heights now set the rhythm, so the
  name / Rooted / time block reads as one unit.
- **The sensitive content feature is gone** (KNOT-ADR-072). The `stories.sensitive`
  column is dropped by migration `0018`; `POST /stories` no longer accepts the field;
  the story and version responses no longer carry it; the composer lost its switch; and
  `FeedEntry` lost the blur, the reveal scrim, and the badge. The moderation report
  category `sensitive_content` is a **different** concept (a user reports content) and
  stays.

### KNOT-022-fix1/fix2 — the attribution row

A version card opens with an **attribution row**: a small stack of the LINEAGE authors'
avatars beside a two-line social credit, then a hairline, then the poster's `AuthorLine`.
The stack shows the author of the version this one directly adapts (`parent_author_*`) and
the story's original author (`root_author_*`) — never the poster, who is the entry's own
`author_*` and is already the `AuthorLine` below. A first adaptation's parent IS the root,
so the client draws one avatar (it compares the ids first).

- The sentence is rendered by `versions.FormatAttribution` and shown verbatim:
  `Adapted from {root}'s original story in {language}.` (N=2),
  `Adapted from {middle}'s {language} version of {root}'s original story.` (N=3), and
  `Adapted from {parent}'s {language} version, tracing back to {root}'s original story.`
  (N=4+). The adapter is deliberately not named: their avatar and name are the author line
  below, so repeating them made the credit long (KNOT-030). The target language is likewise
  not repeated: the reader is already looking at that telling (KNOT-022-fix2).
- `feedEntryMeta.attributionSegments` splits **every** name the sentence mentions, so all
  of them take the bold label voice; `attributionParts` remains as a first-name view. The
  older `Adapted from …` shapes are still parsed, so a newer client styles a sentence from
  an older server.
- Both `parent_author_*` and `root_author_*` come from the chain the feed already resolves
  (the second-to-last and first links) and the same batched author read, so the fields cost
  no extra query. `FeedScreen` passes them into `FeedEntry`; the card renders them at
  `Avatar size="sm"` in lineage order with a canvas ring on the overlap.

### KNOT-029 — universal social proof and the adapt badge

`SocialProofHeader` is now rendered by **every** feed card, not only a version. The
hierarchy is unchanged — interactions, then hereditary attribution, then nothing — but the
inputs differ by kind: a story passes only its `recent_interactions` (it has no
`attribution_line`), so an untouched story draws no header, while a version falls back to
its sentence and lineage. `FeedEntry` therefore renders the header unconditionally and
passes the attribution props only for `variant="version"`.

The copy is a sentence rather than a verb phrase with a dangling "this": one actor reads
"Tycoon finds this true", "Tycoon knows this differently", "Tycoon adds something new",
"Tycoon needs a source", or "Tycoon commented on this". Two actors sharing a signal take
the plural verb ("Tycoon & Yantuntu know this differently"); mixed actions read "Tycoon
finds this true and Yantuntu commented on this". `socialProof.ts` owns both grammatical
numbers, so a conjugation is a data change rather than a new string template.

`AuthorLine` gained an optional `badge` slot, drawn beside the name in the stacked block.
`FeedEntry` passes a 16pt `ShareNetwork` in `brand.primary`, labelled "Adapted story", for
every version card. It is independent of the header, so the adaptation remains visible
when interactions have replaced the attribution sentence (KNOT-029).

KNOT-030 fixes the two phrasings that still read poorly on device. The header now groups its
latest two interactions **by actor first**, so one person who both reacted and commented is
named once and drawn once — "Yantuntu finds this true and commented." — instead of
"Yantuntu finds this true and Yantuntu commented on this."; a reaction leads within that
pair, because its phrase carries "this". `versions.FormatAttribution` also dropped the
adapter from every adaptation sentence: the credit now names only the people the telling was
adapted **from**, since the adapter is already the author line. The client's structured
fallback uses the same concise wording.

### KNOT-031 — the feed card menu and its polish

Every feed card now carries an overflow control, and the card's typography and separation were
tightened to match the reference.

- **The control** is `FeedEntry`'s new optional `onOpenOptions`, drawn as a 44pt `DotsThree`
  in `text.tertiary` at the top-right of the author row. Omitting the prop draws nothing, so
  Place Stories — which reuses the card — keeps a card with no menu rather than a dead one.
  The control is a **nested** `Pressable` inside the card's own, so it takes the touch itself
  and a menu tap never also opens the story; that is the isolation `AuthorLine`'s author
  target already relies on.
- **The menu** (`FeedScreen`'s file-local `PostOptionsSheet`) is a bottom sheet: handle,
  "Hide this post", "Report post", then Cancel, each row at the 44pt minimum. It is local to
  the screen rather than a shared component because the feed is the only place it is drawn —
  the same call the profile's options sheet makes.
- **Hiding is local to the session.** There is no hide endpoint, so the entry leaves the list
  and returns on the next refresh or page load: it is a "not now", and the app does not
  pretend it is a mute. Reporting hands the entry to the existing `ReportModal` with
  `entityType` taken from the entry's `kind` (`version` or `story`) and the entry's own id,
  so a report names the thing that was on screen. The entry is **not** removed after a
  report — moderation is asynchronous, and hiding it would claim an outcome the app cannot
  promise.
- **The header got quieter.** `SocialProofHeader` is a caption, not a paragraph: the sentence
  is `typeScale.caption` (12/16) and the faces are `Avatar size="xs"` (20pt) overlapping by one
  spacing step. The hairline that closes it still separates two blocks **inside** one card, so
  it stays `border.hairline` and hugs the author block below it.
- **Cards were ruled.** `styles.card` briefly gained a full-bleed bottom border in
  `border.divider`. KNOT-032 replaced that rule with a background gap — see below.
- **The stats row** moved from a 12pt caption in `text.tertiary` to `typeScale.label` (13/16
  bold) in `text.secondary`, because it carries real counts rather than metadata about them.

### KNOT-032 — the card structure, and its correction

The feed's cards gained two quick actions. On-device review then found the layout and the
hide interaction wrong, and KNOT-032-fix1 corrected them; the notes below describe the
shipped behaviour.

- **Two quick actions, side by side.** `FeedEntry`'s optional `onOpenOptions` and `onHide`
  draw a 22pt `DotsThree` and a 20pt `X` in `text.tertiary`, in one `cardActions` row at the
  top-right of the author block. Each is its own 44pt `Pressable` nested inside the card's,
  so a quick action never opens the story and never opens the other action. Either control
  disappears when its handler is omitted, which is what keeps Place Stories — the card's
  other caller — free of dead targets.
- **Hiding asks first.** Neither quick action removes anything. The card's `✕` and the
  options sheet's "Hide this post" row both call `FeedScreen.requestHide`, which arms
  `ConfirmSheet` — "Hide this story?", then `Keep story` or `Hide story`. `hideEntry(id)` is
  still the only function that removes an entry, and it runs only on confirmation. The copy
  says the entry is gone "for now", because hiding is session-local: it returns on the next
  refresh, and the app must not imply a mute it cannot deliver.
- **`ConfirmSheet` is the app's first reusable confirmation.** A question, one sentence
  saying what confirming will do, and two `Button`s — the safe choice outlined, the
  destructive one in the danger ink. The backdrop and Android's back button both mean
  "keep", and the confirming action runs at most once per showing, so a double tap cannot
  apply the effect twice.
- **The separation is a band of surface, not a rule and not a gutter.** `styles.card` reads
  `backgroundColor: colors.bg.canvas` and `marginBottom: spacing.sm` — full width, no
  horizontal inset, no border. The page behind it is `colors.bg.separator`, a new surface
  role that paints the same neutral as `border.hairline`: `bg.sunken` (#F5F5F7) was too
  close to white to read as a separator at 8pt, and a *line* token could not be used as a
  large fill without pretending it is a surface. 8pt is the compact step inside the 6–8pt
  target. There is deliberately **no corner radius**: the media inside a card is
  full-bleed, so rounding the card would need `overflow: 'hidden'` and would clip the
  media's own edges.
- **The header band is compact and centred.** `SocialProofHeader`'s container is the
  band's box and the rule is its last child, so padding the container did not work: the
  inset landed **below** the rule, which left the sentence sitting on the rule and the rule
  floating above the author block. The band's step now sits between the row and the rule
  (`divider.marginTop: spacing.sm`), and `FeedEntry` opens a card that has a header at
  `spacing.sm` instead of `spacing.lg` — a card with nothing for the header to say keeps the
  taller resting inset. The container keeps `spacing.sm` under the rule, its clearance from
  the author block, so the three gaps are all 8pt and the band stays content-driven: 45pt for
  a one-line credit, 57pt for two.

### KNOT-025 — the Story Detail redesign

The Story Detail follows the Claude mockup v2 (`docs/design-reference/story-detail-v2/`).
It is a column of its own: the media carousel runs edge-to-edge behind a **floating** 56pt
`Header` (Back + the report overflow) on dark discs over a soft scrim, so the controls stay
legible over any media (KNOT-025-fix4).

- **The carousel** is a horizontal `pagingEnabled` ScrollView of full-width pages. Each
  page is a file-local `CarouselPage`: it resolves the media's own shape through
  `useMediaAspectRatio` (payload dimensions, then the media's load, 4:5 cap — the KNOT-020g
  -fix7 rule) and reports it up, because the pager's **height follows the active page**. A
  video is handed to `FeedVideo` (autoplay only while its page is active, one shared mute);
  an image draws directly. A `n / N` counter pill sits top-right when there is more than one
  item. The pure offset/counter arithmetic lives in `screens/stories/detailCarousel.ts`
  (`carouselCounter`, `carouselIndex`, `carouselPositionLabel`) and is tested without a
  renderer.
- **Video chrome for the detail:** `FeedVideo` gained `durationMs` (seed the badge from the
  payload), `showTag={false}` (the carousel's counter and caption replace the "Video" tag),
  and `durationPosition="bottom-left"` (the Feed keeps the default top-right). The mute
  control stays bottom-right at 44pt, the design system's minimum touch target.
- **Scrolling past the media pauses it:** the screen measures where the carousel ends
  (`onLayout`) and flips one state flag when the vertical scroll crosses it, rather than
  updating state on every scroll frame. Opening the viewer or the report sheet pauses it
  too.
- **Text column:** plain `PillarTag`, `typeScale.h1` title, `AuthorLine size="large"`
  `layout="stacked"` (the Feed's block: name, quiet Rooted line, compact time), the bordered language-tree row (GitBranch + language +
  `N versions`, the whole row tappable), `typeScale.reading` body in `text.secondary`, the
  `ReactionBar` between hairlines, a brand-ink conversation link (it replaces the old ghost
  button but keeps the route), and the full-width primary CTA at the end of the scroll.
- **Deliberate omissions (brief vs mockup):** no Save/bookmark control (no save feature), no
  centre play button (the video autoplays), no caption/credit row (`StoryMedia` carries
  none of those fields), no scroll-linked header title, and no second "N versions · N
  languages" row. The block flow still lives on the author's profile, not the header.
- **KNOT-025-fix1/fix2:** the thumbnail strip was added (48pt squares, 8pt gap, the
  active one ringed in `brand.primary`; tapping scrolls the pager, and the strip centres
  the active item itself). Video tiles now render a **real first frame** through
  `VideoThumbnail` — a paused, muted `react-native-video` player, because the library
  cannot export a frame as an image and the API stores no poster; `onLoad` /
  `onReadyForDisplay` retire a sunken placeholder, which stays if the source fails. The
  author block uses the **Feed's `layout="stacked"`** (with the compact time), not the
  short-lived `detail` layout. The rhythm is 8pt pillar→title, 12pt title→author, 16pt
  author→language row, with 16pt above the text column. The bottom is one hairline-
  separated action area: `ReactionBar layout="grid"` (two-by-two text pills, labels on
  one line), then a full-width secondary conversation button and the primary CTA with
  24pt of air around the stack.
- **KNOT-025-fix3:** the custom reaction SVGs are gone from the app. `ReactionBar` is now
  a set of **text pills** — label plus count on a `bg.sunken` surface, brand tint when
  held — in both its row and grid layouts, so no surface still draws the old glyphs. The
  story strip's active video tile is now a **muted looping preview** (`VideoThumbnail`
  gained `isVisible` and `durationMs`): it plays only for the carousel's active item while
  the strip is on screen and no viewer/report sheet is open, and it pauses otherwise, so
  one decoder runs at a time. The play disc is replaced by a duration badge; `useTextureView`
  still keeps the Android surface inside the rounded tile.
- **KNOT-025-fix4:** the strip is **static again** — `VideoThumbnail` lost `isVisible` and
  `durationMs`, so it mounts a paused, muted player per video tile and shows its first
  frame with a small play disc; only the main carousel autoplays. The carousel now runs
  **edge-to-edge behind a floating header**: this screen alone mounts a `StatusBar` with
  `translucent` and a transparent background, the ScrollView starts at y=0, and a
  `HEADER_BAND`-sized scrim (`scrim.overlay`) plus a `HEADER_TOP`-offset overlay put
  `Header` above the media with the Back/More controls on 44pt dark discs. `HEADER_TOP` is
  `StatusBar.currentHeight` on Android and 0 on iOS, where the app root's `SafeAreaView`
  already insets the screen — so iOS starts the carousel below the notch rather than
  behind it (no safe-area-inset dependency is installed). The band and the light status
  icons are Android-only and drawn only when the story has media; a media-less story
  keeps the canvas and dark icons (the band itself was removed again in fix5, below). The
  counter pill and the loading/error block clear the band, and a no-media story gets a
  band-sized spacer. Reaction pills are refined: a
  hairline border on `bg.paper`, `typeScale.label`, 44pt in the row layout and 36pt in
  the grid, brand tint when held. The Feed's adaptation credit ends after the language
  — the `· Version N` tail is gone (the number still lives on the Story Version screen).
- **KNOT-025-fix5:** the scrim band is **gone** — the review found it dimmed the media's
  top, so nothing is painted behind the status bar now and the carousel is bright to the
  top edge. Legibility rests on Android's `light-content` icons plus the header's own dark
  discs; React Native cannot give the system icons a shadow, so a very bright photo is
  still a weaker case (disclosed). The strip tiles went 48pt → **64pt with a 12pt gap**
  (`THUMBNAIL_GAP`, which also sets the snap stride and the auto-centre offset), and the
  active/hairline ring is now an **overlay View** rather than the tile's border, so the
  media keeps its full square. `VideoThumbnail` takes a `size` and draws an **explicit**
  square frame with the player at `100%`/`cover` — a bare flex player let the native
  surface measure itself and the tiles read stretched/growing. The viewer is out
  of the tap flow: `CarouselPage` is a plain `View` (no `onPress`, no page label) and the
  strip tiles are non-interactive `View`s, so `viewerIndex`, the `MediaGalleryModal`
  render, `carouselRef` and `handleThumbnailPress` are all gone from this screen. The
  modal component and its wiring are untouched; the strip now only *follows* the pager and
  no longer jumps it. Both removals are marked `TODO: Re-enable viewer in a future task`.
- **KNOT-025-fix6:** an **active video tile bled upward out of its square into the
  carousel** on Android — an image never did. The fix is structural: inside
  `VideoThumbnail`'s fixed `size`×`size` frame (`overflow: 'hidden'`, `radius.md`) the
  `<Video>` now sits in a **`StyleSheet.absoluteFillObject` view**. That gives the native
  surface a box with all four insets resolved, so it cannot take its own size from the
  clip; the frame's `overflow: 'hidden'` then clips it. The player keeps
  `width: '100%' / height: '100%'`, `cover`, `useTextureView` and `paused`, and the
  placeholder and play disc remain **siblings** of the anchor inside the clipped frame.
  ⚠️ Strip tiles take the SAME rules already — `StoryDetailScreen`'s `styles.thumbnail`
  is `THUMBNAIL_SIZE`² with `overflow: 'hidden'` and `radius.md` — so the image tile needed
  no change (and an `Image` has no native surface to escape). Tests 384→**385/36** (`+1`
  asserting the anchor exists, holds the player, and leaves the overlays outside it).

### KNOT-026 — the Version Detail shares the Story Detail's layout

The two detail screens are now deliberately the same reading experience.
`StoryVersionScreen` renders the Story Detail's carousel (file-local `CarouselPage`, the
floating `Header` on dark discs, the static 64pt thumbnail strip), its reading column
(plain `PillarTag`, `h1` title, the Feed's stacked `AuthorLine`, the bordered language-tree
row, `typeScale.reading` body), and its action area (one hairline, a two-by-two
`ReactionBar` grid, then the full-width actions). The adapter is the `AuthorLine`; the
server's `attribution_line` is a quiet `caption` / `text.tertiary` line directly beneath it.

A version has **no media, pillar, place, or version count of its own** — they are the
parent story's. `GET /versions/{id}` does **not** repeat them today (its response is the
version's own fields plus attribution), so the screen reads `GET /stories/{id}` once as
enrichment and uses the story's media, pillar, place, and count. `StoryVersion` types those
fields as optional, so a future enriched version response needs no client change. If the
parent read fails, the version itself still renders — without inherited presentation.

The CTAs are version-specific: a ghost **View original story** (the story's root detail)
and the primary **Adapt this version** (the adapt flow with this version as the parent).
The adapter's optional note is kept as a quiet note block rather than the old bordered card,
so it does not disturb the Story Detail's rhythm. The Language Tree row pushes the story's
tree, so it opens the tree from a version that was reached another way.

KNOT-027 polishes that credit and action area after on-device review. The attribution
sentence now lives in its own block with `marginTop: spacing.md` below the **whole** author
line — avatar, name, Rooted line, and time — because at 4pt it read as cramped against the
Rooted badge. It remains a two-line `caption` in `text.tertiary`. The action area also gained
the Story Detail's secondary **Join the conversation** button, which pushes the existing
`comments` overlay with the version's story id and version id. The version payload has no
comment count, so the label is unnumbered.

### Rooted, and the enrichment pattern

`POST /users/me/rooted`, `GET /users/me/rooted`, and `GET /users/{id}/rooted` are served by
`internal/httpapi/rooted_handler.go`, backed by the `internal/rooted` domain package.
Rooted is a self-declared connection to a place (city or region precision only) plus a
duration bucket; a user has exactly one primary signal, so setting one replaces the last.
Writing a signal and reading your own require an access token; reading another user's
public signals is open. See KNOT-ADR-016/017.

The content handlers share one enrichment step: the stories, versions, and conversations
handlers each gather the **distinct author ids** in a response, call
`BatchGetPrimaryPublicSignals` **once**, and attach the resulting inline summary. The helper
is `authorRootedSummaries` in `internal/httpapi/enrich.go`, and the dependency is the narrow
`RootedLookup` interface (one method) rather than the whole Rooted service, so a handler
test can substitute a lookup that counts its calls. A failure to read Rooted is logged and
swallowed — enrichment is supplementary, so a Rooted problem must not make stories
unreadable — and a user with no public signal simply carries `null`. The stories handler's
`List` route (the feed) attaches it to every story on a page in that same single batch,
reversing the earlier decision to leave the feed unenriched (KNOT-020c-fix4).

```sql
SELECT id, user_id, place, duration_bucket, is_public, is_primary, created_at, updated_at
FROM rooted_signals
WHERE is_primary = true AND is_public = true AND user_id = ANY($1)
```

That is the single batch query behind the enrichment; `$1` is a `uuid[]`, so `user_id`'s
index stays usable. The `author_rooted` projection carries only `place` and
`duration_bucket`.

**Author attribution** uses the same shape for a different lookup. The helper is
`authorAttributions` in the same file, and the dependency is the narrow `AuthorLookup`
interface (one method, `UsersByIDs`). Every response that names an author is decorated with
`author_display_name` and `author_avatar_url` in **one batched read per response**; a
single-entity route goes through `authorFields`, which is the one-author case of the same
helper. `author_avatar_url` is the backend path from `avatarPathFor` — or `null` when the
author has no avatar — and a failed lookup is logged and swallowed, leaving an empty name
and a `null` avatar rather than failing the response (KNOT-ADR-041). The notifications
handler consumes the same contract: its `NotificationActors` is an alias of `AuthorLookup`.

**Story counts** extend the same pattern. A feed entry needs four aggregates that span three
tables — `story_versions`, `comments`, and `story_media` — so they are a narrow read model of
their own, `internal/storystats`, exposed to the handler as the one-method `StoryStatsLookup`
interface and attached by `storyStats` in `internal/httpapi/enrich.go`. Its store runs **one
grouped query per count type** for the whole page (never a query per story), a failure is
logged and swallowed so a story stays readable, and a story absent from the result reads as
the zero `Counts`. The `first_media_type`/`first_media_url` fields are derived from the media
preview the handler has already attached, so they cost no extra query (KNOT-020c-fix).

### Notifications

`GET /notifications`, `GET /notifications/unread_count`, `POST /notifications/{id}/read`,
and `POST /notifications/read_all` are served by
`internal/httpapi/notifications_handler.go`, backed by the `internal/notifications` domain
package. Every route is protected and every route is scoped to the caller by the token, so
there is no user id in any request and no way to reach another person's inbox.

A notification records that **another user acted on your content**: someone adapted a
version you wrote, commented on it, or reacted to it. Two further events go
beyond that shape — `reaction.created` names whichever entity was reacted to, and
`inquiry.nearby` reaches a recipient who authored nothing, because they are Rooted in the
place a question names. Five events exist in total and the set is closed by a CHECK constraint
and by Go constants. The package is deliberately **domain-agnostic** — it imports no content
domain and stores primitive ids — and each producing domain declares its own narrow hook
instead, so the dependency graph stays acyclic (KNOT-ADR-040):

```go
// internal/versions
type Notifier interface {
    NotifyVersionCreated(ctx context.Context, recipientID, actorID, versionID string) error
}
```

`versions.Service.CreateAdaptation` and `conversations.Service.CreateComment`
call their hook **after** the primary row is committed, compare recipient and actor first so
your own action never notifies you, and ignore the returned error: a notification is a
non-critical side effect, and an inbox write must not fail the adaptation or comment that
caused it (KNOT-ADR-038). The recipient is resolved inside the producing domain —
`conversations` gained `VersionAuthor(ctx, versionID)` for this rather than importing
`versions` and loading a whole `StoryVersion`.

The inbox is keyset-paginated on `(created_at, id)`, exactly as the feed and a thread are,
and the query matches `notifications_user_created_idx`:

```sql
SELECT id, user_id, actor_id, event_type, entity_type, entity_id, read_at, created_at
FROM notifications
WHERE user_id = $1 AND (created_at, id) < ($2::timestamptz, $3::uuid)
ORDER BY created_at DESC, id DESC
LIMIT $4
```

Marking one read is a single `UPDATE ... SET read_at = COALESCE(read_at, now())`, so a
second tap is not an error and does not move the timestamp; a row that does not exist — or
that belongs to someone else — is `ErrNotFound`, the same answer for both, so the route
cannot be used to probe for ids.

A page of notifications is enriched with its actors in **one batched lookup**,
`identity.UsersByIDs`, and with the same `authorRootedSummaries` helper the content handlers
use, so twenty notifications cost two extra queries rather than twenty. Both enrichments are
supplementary: a lookup failure is logged and the row carries `null` for the actor rather
than failing the inbox. `avatar_url` on a resolved actor reuses `avatarPathFor`, the helper
the avatar handler already had.

Delivery is **in-app only** (KNOT-ADR-039): there is no device-token table, no push
provider, and no delivery state. The mobile app reads its own inbox and the unread count,
and shows a bell with a badge on the feed.

### Profiles (a read model)

`GET /users/{id}/profile` is served by `internal/httpapi/profile_handler.go`, backed by
`internal/profile`. The wall is a **read model**, not a table:
`internal/profile/postgres_store.go` runs one `UNION ALL` over `stories`, `story_versions`,
`comments`, `inquiries`, and `inquiry_answers`, filtered by `author_id` and ordered by
`(created_at DESC, id DESC)`. Each branch builds its payload with `jsonb_build_object`, so a
page is one query, never N+1, and a comment preview is `left(body, 200)` so a full body
never leaves through the wall. One act is not double-counted: a story's root version is
excluded from the version branch (KNOT-ADR-042).

The service resolves the owner through `identity.UserByID` — so an unknown id is a 404 even
with no activity — and decodes the same opaque `(created_at, id)` cursor the feed uses. The
handler attaches the owner's Rooted summary with the shared `authorRootedSummaries` helper
and projects the avatar with `avatarPathFor`, exactly as the content handlers do. The route
is public and does not collide with `GET /users/{id}/rooted` or `GET /users/{id}/avatar`.

### Languages (the canonical list)

`backend/go/internal/language` is the canonical ISO 639-3 list and the only place a language
code is defined (KNOT-ADR-046). It is a compile-time constant — no table, no cache, and a
lookup that cannot fail:

```go
if !language.IsValid(code) {
    return "", &ValidationError{Field: "language", Message: "must be a valid ISO 639-3 language code, such as eng or zul"}
}
```

Validation **matches exactly**, so callers must not lower-case first: `eng` is accepted, and
`ENG`, `en`, and `English` are rejected. That is what keeps one language to one stored
spelling. `strings` trimming still happens, and a blank entry in `preferred_languages` is
dropped rather than rejected.

Every domain that holds a language delegates to it — `stories`, `versions`, `conversations`,
`identity`, and the discovery cluster filter — so there is one rule, not five. There is no
`CHECK` constraint: a language list in PostgreSQL would need a migration per registry update
and would live in two places.

`GET /languages` is served by `handleLanguages` in `internal/httpapi/router.go` — a `Router`
method like `handleHealth`, not a handler type, so `NewRouter`'s signature is unchanged. It
is public, needs no service, and returns `{"languages": [{"code", "name"}, ...]}` built from
`language.All()`, which returns a copy so a caller cannot mutate the list. The response is
about 270 KB and is deliberately not paginated: the client caches it.

### Migration `0011`: ISO 639-1 to ISO 639-3

KNOT-015d stored two-letter codes; KNOT-015d-fix made the code three letters, so
`migrations/0011_iso_639_3.up.sql` rewrites the values already in the database. It touches
the four columns that hold a language — `story_versions.language`, `comments.language`,
`bridges.target_language`, `users.preferred_languages` — and nothing else. `stories` is not
among them: migration `0003` dropped `stories.language` when content moved into a root
version.

Three properties are worth knowing before changing it:

- **It is idempotent.** Every statement matches only values that are exactly two characters,
  so a second run changes nothing.
- **It does not guess.** The 184 ISO 639-1 codes and their counterparts are carried in a
  temporary table taken from the same SIL table the list is generated from, and a two-letter
  value with no counterpart is left as it is. `DROP TABLE` at the end means no permanent
  object is added.
- **Its `down` is best effort.** It reverses three-letter codes that have a two-letter
  ancestor and leaves the rest alone, so `nso` survives a round trip. A database that has been
  through `up` and then `down` holds a mix of widths, because a stored code records no
  history.

`migrations/iso6393_test.go` exercises the real files against the real schema: it seeds
`en`, `zu`, `fr`, an `af` bridge, mixed and empty `preferred_languages`, and an unmapped `zz`,
runs `up`, asserts every mapping, runs `up` again to prove idempotency, runs `down`, and
asserts the best-effort reversal. It all happens inside a transaction that is always rolled
back, so the test leaves the database untouched. It skips when `KNOT_POSTGRES_DSN` is unset or
the tables are missing.

### Curious Inquiries

`POST /inquiries`, `GET /inquiries`, `GET /inquiries/{id}`, `POST /inquiries/{id}/answers`,
`GET /inquiries/{id}/answers`, and `GET /answers/{id}` are served by
`internal/httpapi/inquiries_handler.go`, backed by the `internal/inquiries` domain
(`inquiry.go`, `cursor.go`, `service.go`, `postgres_store.go`).

Two mechanical points are worth knowing before changing them:

- **`GET /answers/{id}` is a top-level path, not `/inquiries/answers/{id}`.** The latter would
  overlap `/inquiries/{id}/answers` at `/inquiries/answers/answers`, and Go's `ServeMux` refuses
  conflicting patterns at registration — it panics rather than picking one. `TestInquiryRoutesAreRegistered`
  builds the real router, so a future pattern change that collides fails in the test suite rather
  than at start-up. The top-level shape also matches `GET /comments/{id}`.
- **Routing is a lookup on Rooted, not a query in this package.** `internal/inquiries` depends on
  a one-method `RootedRouting` interface (`RootedUserIDsByPlace`), which `rooted.Service`
  satisfies. Rooted owns `rooted_signals`, so the SQL lives in
  `internal/rooted/postgres_store.go` beside the other signal reads, and the inquiry domain stays
  free of another domain's schema (KNOT-ADR-056).

The announce-after-commit shape is the one every content domain uses: `CreateInquiry` commits the
row and then tells the place's Rooted users, logging and swallowing any failure, and
`CreateAnswer` commits the answer and its `answer_count` increment in one transaction before
notifying the asker (KNOT-ADR-038).

### Migration `0014`: inquiries, and two widened CHECKs

`migrations/0014_inquiries.{up,down}.sql` adds `inquiries` and `inquiry_answers`, three indexes
on the first and two on the second, `rooted_signals_place_idx`, and widens the notifications
`event_type` and `entity_type` CHECKs plus the reactions `entity_type` CHECK.

Three properties matter:

- **Everything is idempotent.** Tables and indexes use `IF NOT EXISTS`; each CHECK is dropped by
  name first and re-added behind a `pg_constraint` guard, so the file can be re-applied by hand.
  Verified by applying it twice to the local database.
- **The CHECKs must be widened in the same migration that starts writing the new values.** The
  event and entity sets are closed by those constraints, so a migration that added an event
  without widening its CHECK would fail at the first write rather than at migration time.
- **`down` deletes rows the narrowed CHECK could not hold** (`entity_type = 'inquiry'`, and the
  two inquiry events) before re-adding the narrower constraints. That makes the rollback total
  rather than a partial failure, and the affected rows can only exist if this migration ran.

`rooted_signals_place_idx` is added because every earlier Rooted read was by user; finding "the
users Rooted in this place" is the first read keyed by place.

### Search (KNOT-018)

`internal/search` answers `GET /search`: one ranked list across stories, versions, and inquiries.
It is layered like the other domains (`search.go` types + store contract, `cursor.go`,
`service.go`, `postgres_store.go`) and owns its store rather than widening three domain
interfaces, because it is one different read shape over the same data.

**Text lives on two tables, not three.** A story row carries no title or body — its content is its
root version — so a *story* result comes from `story_versions WHERE parent_version_id IS NULL`
joined to `stories`, and a *version* result from the non-root rows. `stories` has no
`search_vector` (KNOT-ADR-065, `docs/DATA_MODEL.md`).

**The SQL is static.** One statement is assembled from exactly two variables: which of the three
subqueries to union (the `type` filter) and whether the keyset clause is appended (a cursor).
**No value is ever interpolated into the statement** — the twelve parameters are bound, and the
same placeholder is reused inside all three subqueries, which PostgreSQL permits. The block filter
needs no dynamic SQL at all: `author_id <> ALL('{}'::uuid[])` is `true`, so an empty set is a
no-op and the anonymous case disappears.

**Phrases become prefix terms.** `tsQuery` reduces a phrase to letters and digits and emits
`term:*` for each, joined by `&`. That is what makes `mang` find `Manguzi`, and it also means no
character that is meaningful in tsquery syntax (`&`, `|`, `!`, `(`, `)`, `:*`, quotes) can reach
the parser from user input — the reason the endpoint cannot be made to 500 with a crafted phrase.
A phrase with no letters or digits at all becomes an unmatchable term rather than an error.

**The cursor carries the rank.** Pagination is a keyset over `(search_rank DESC, created_at DESC,
id DESC)` and the cursor encodes all three. `ts_rank` is a property of the matched document, so a
page walk that spans a write can skip or repeat a row. That is the accepted trade-off: ordering by
recency alone would make a search result list useless.

**Tuning knobs.** Both are one-line changes:

| Knob | Where | Current value |
| --- | --- | --- |
| Term weights | the generated column in `0016_search.up.sql` (`setweight(…, 'A'/'B')`) | title `A`, body `B` |
| Ranking function | `ts_rank` in `internal/search/postgres_store.go` | `ts_rank(search_vector, to_tsquery('simple', $8))` |

Swapping `ts_rank` for `ts_rank_cd` (cover density, which rewards adjacent terms) changes only
the score's scale, because the rank is only ever compared to itself — but it does mean an
existing `next_cursor` from before the change may skip or repeat rows, so it is a deploy-time
decision rather than a hot toggle.

**Tests.** `internal/search` has unit tests for the query builder, the cursor, and every
validation rule, and integration tests (skipped without `KNOT_POSTGRES_DSN`) for full-text search
across all three types, each filter, combined filters, the block filter, case-insensitive and
prefix matching, exact-once pagination, and a filter-only search. Integration tests use a per-run
token so they are unaffected by the real content in the developer's database (KNOT-ADR-063).

### Migrations

- SQL lives in `backend/go/migrations/`, named `<version>_<name>.up.sql` and
  `<version>_<name>.down.sql`, and is embedded into the binary with `go:embed`.
- Applied versions are recorded in the `schema_migrations` table, so re-running
  `migrate up` is safe and reports that the schema is already up to date.
- The runner only goes **up**. The `.down.sql` files exist so that a rollback is
  explicit and reviewable; there is no `migrate down` command.
- There is deliberately no third-party migration library.

## Backend configuration

`backend/go/internal/config` reads configuration from the environment. It uses the Go
standard library only, and it stores values without connecting to anything.

| Variable            | Required                        | Notes                                          |
| ------------------- | ------------------------------- | ---------------------------------------------- |
| `KNOT_ENV`          | No — defaults to `local`        | `local`, `ci`, or `test`                       |
| `KNOT_POSTGRES_DSN` | Only when `ci` / `test`         | Has a safe local default                       |
| `KNOT_REDIS_ADDR`   | Only when `ci` / `test`         | Has a safe local default                       |
| `KNOT_REDIS_PASSWORD`| No                            | Read verbatim; empty means the server requires none (`local` does) |
| `KNOT_REDIS_DB`     | No — defaults to `0`            | Must be a non-negative integer when set        |
| `KNOT_HTTP_PORT`    | No — defaults to `8080`         | Must be 1-65535 when set                       |
| `KNOT_LOG_LEVEL`    | No — defaults to `info`         | `debug`, `info`, `warn`, or `error`            |
| `KNOT_JWT_SECRET`   | Only when `ci` / `test`         | Must be at least 32 bytes outside `local`      |
| `KNOT_S3_ENDPOINT`  | Only when `ci` / `test`         | S3-compatible endpoint; has a local default    |
| `KNOT_S3_ACCESS_KEY`| Only when `ci` / `test`         | Has a local default matching MinIO            |
| `KNOT_S3_SECRET_KEY`| Only when `ci` / `test`         | Has a local default matching MinIO            |
| `KNOT_S3_REGION`    | No — defaults to `us-east-1`    | MinIO ignores it; real S3 does not            |
| `KNOT_S3_BUCKET`    | No — defaults to `knot-media`   | The bucket must already exist                   |

- `local` (or unset): missing values fall back to the safe local defaults listed in
  `.env.example`. A missing JWT secret becomes the documented placeholder and the server
  logs a prominent warning.
- `ci` / `test`: the Postgres DSN, Redis address, object store endpoint, object store
  credentials, and JWT secret are all **required**, the secret must be at least 32 bytes,
  and the backend fails fast naming every problem. Region and bucket keep their defaults
  everywhere, because they are not secrets and a wrong one is caught by the first request
  rather than by a name that has to be repeated in every environment.
- Any other value: treated as required too, so an unrecognised environment never
  silently receives local defaults.

```bash
cd backend/go
KNOT_ENV=local go run ./cmd/knot                 # warns about the placeholder secret
KNOT_ENV=ci go run ./cmd/knot                    # fails fast naming the missing variables
```

Only the environment name and the port are ever logged. The DSN and the JWT secret may
carry credentials and are never logged.

## Environment configuration

- All expected environment variable names live in `.env.example` at the repository root.
- **Convention:** names and placeholder values only. Real secrets are **never** committed.
- Copy `.env.example` to `.env` locally and fill in real values.
- **`.env` is git-ignored; `.env.example` is tracked.** `.gitignore` ignores `.env` and
  `.env.*`, then re-allows `!.env.example`. You can confirm both facts at any time:

```bash
git check-ignore -v .env          # should report a matching ignore rule
git ls-files --error-unmatch .env.example   # should succeed (file is tracked)
```

- The active variables are the `KNOT_*` ones:
  - `KNOT_ENV`, `KNOT_POSTGRES_DSN`, `KNOT_REDIS_ADDR`, `KNOT_REDIS_PASSWORD`,
    `KNOT_REDIS_DB`, `KNOT_HTTP_PORT`,
    `KNOT_LOG_LEVEL`, `KNOT_JWT_SECRET`, `KNOT_S3_ENDPOINT`, `KNOT_S3_REGION`,
    `KNOT_S3_ACCESS_KEY`, `KNOT_S3_SECRET_KEY`, `KNOT_S3_BUCKET` — read by the Go
    backend.
  - `KNOT_POSTGRES_USER`, `KNOT_POSTGRES_PASSWORD`, `KNOT_POSTGRES_DB`,
    `KNOT_POSTGRES_PORT`, `KNOT_REDIS_PORT`, `KNOT_MINIO_ROOT_USER`,
    `KNOT_MINIO_ROOT_PASSWORD`, `KNOT_MINIO_PORT`, `KNOT_MINIO_CONSOLE_PORT` — consumed
    by `infrastructure/docker/docker-compose.yml`.
  - `KNOT_API_URL` — the API base URL for the mobile app, read by
    `apps/mobile/src/config/api.ts` when the bundler inlines `process.env`.
- The "reserved for later phases" block (`REDIS_URL`) is placeholder-only and **not used
  yet**. `DATABASE_URL` was removed in KNOT-002b and `JWT_SECRET` in KNOT-003, in favour
  of `KNOT_POSTGRES_DSN` and `KNOT_JWT_SECRET`.

```bash
cp .env.example .env
```

## npm install scripts (recorded from KNOT-001)

During the KNOT-001 verification run, `npm ci` in `apps/mobile/` **skipped the install
scripts** of some transitive dependencies. Two were observed explicitly:

- `@parcel/watcher` (2.6.0)
- `unrs-resolver` (1.12.2)

Both are present in `apps/mobile/package-lock.json`. They ship optional native binaries,
so a skipped install script can leave a missing or stale native binding on some machines.
In KNOT-001 the skips did **not** affect `lint`, `typecheck`, or `test`.

Practical guidance:

- A skipped install script is not automatically a failure — confirm the behaviour still
  works before changing anything.
- If a later task starts depending on file-watching or module-resolution performance,
  re-check whether these scripts need to run, and whether npm's install-script policy
  (its allow-list / `allowScripts` behaviour) is what is blocking them.
- Do not add `--ignore-scripts`, or change the install-script policy, without an approved
  task.

## Formatting and linting conventions

- **Prettier** formats the mobile app (see `apps/mobile/.prettierrc.json`).
- **ESLint** (flat config) lints the mobile app; TypeScript strict mode is on.
- **gofmt** is the single source of truth for Go formatting.
- `.editorconfig` defines shared whitespace rules (2 spaces; tabs for Go files).

## Testing — tampering with encoded values

**Never tamper with the final character of a base64-encoded value in a test.** The last
character of an encoding whose raw bytes are not a multiple of three carries fewer than
six significant bits, and Go's non-strict decoders ignore those padding bits. An edit that
changes only them decodes to byte-identical output, so the "tamper" is a no-op.

Instead, **decode the value, mutate the decoded bytes, and re-encode** — use
`testutil.TamperBase64Body` in `backend/go/internal/testutil/tamper.go`, which does that,
handles both base64 alphabets, and fails the test if the mutation changed nothing.

It lives in its own package rather than in a `_test.go` file because a helper in a
`_test.go` file is only visible to tests in that same package. `internal/testutil` is
importable by every test that needs it — the JWT and password tests in
`internal/identity` and the cursor test in `internal/stories` all use it today.

This is not hypothetical: it has already caused two flaky tests (a JWT signature's final
character, an argon2id key's final character), each failing about one run in sixteen.

## Testing — integration tests and the local database

**No integration test may assume a clean database.** Every test that runs against
Postgres (`KNOT_POSTGRES_DSN` set) shares the developer's database with real
accounts and content, so a test that counts rows globally, filters by a well-known
place name, or inserts a fixed email/name will read someone else's data and fail on
a machine where CI (a clean database) is green.

Every integration test must instead **scope its assertions to the rows it
created**, by one of:

- a **per-run unique marker** in the values it inserts — a timestamp or UUID in the
  email, name, slug, or place (`knot-inquiries-it-<nanos>-…`, `Cape Town <token>`),
  so a filtered query returns only the test's own rows; or
- a filter on the test's own rows when counting, rather than a global `COUNT(*)` or
  a bare `len(page)` over an unfiltered list.

`t.Cleanup` deletes what the test wrote, but cleanup is not a substitute for
scoping: a test must pass even when a *previous* run's rows (or a real user's) are
still present. Assertions are not weakened to `>=` a count when an exact count is
what the test is about — the data is scoped, not the assertion.

## The merged feed, and how to touch it (KNOT-022)

`GET /stories` is not a list of stories any more: it is a **merged stream** of stories and
human adaptations of them, one newest-first page, each entry carrying a `kind`
(`"story"` or `"version"`). The page's key is `items`. See KNOT-ADR-070 and
[the endpoint](API.md#get-stories) before changing anything here.

Four things are easy to get wrong, and each is pinned by a test:

1. **The two kinds key their enrichment differently.** In `internal/httpapi/enrich.go`, a
   story entry is keyed by its own id for everything; a version entry is keyed by its
   **version id** for reactions and by its **parent story id** for media and the aggregate
   counts (`feedVersionLookups`). Mixing them silently attaches another entity's numbers to
   a card. `feed_versions_test.go` asserts both.
2. **A root version is never an entry.** The version branch filters
   `parent_version_id IS NOT NULL`; without it every story appears twice.
3. **Ordering uses each entry's own `created_at`.** `FeedItem.SortTime()` and `SortID()`
   exist because `item.CreatedAt` on a version entry is the zero time — the embedded story
   value is empty for that kind. Read the sort key through those accessors.
4. **Attribution is enrichment, never a failure.** One batched ancestry read attributes the
   whole page (`versions.Service.ChainsFor` → `ListAncestry`); a chain that cannot be
   resolved leaves that entry's `version_number` at `0` and its `attribution_line` empty
   while the page is still served.

Migration `0017` adds `story_versions_created_at_id_idx`, the partial index the version
branch is ordered by. It changes no data, so `migrate up` on an existing database is a
no-op apart from the index.

To smoke it by hand, register two accounts, publish a story, adapt it once or twice, then
read `GET /stories`: the adaptations appear as `kind: "version"` entries above the story,
each with its own `version_number` and `attribution_line`, and the story appears exactly
once. A page with an adaptation costs one query more than a stories-only page (the ancestry
read) plus a second reaction read, because adaptations and stories react as different
entities.

## Continuous integration

`.github/workflows/ci.yml` runs on every push and pull request. It has four jobs:

- **Mobile job:** install → lint → type-check → test.
- **Backend job:** gofmt check → `go vet` → `go test`.
- **Workflows job:** `actionlint`, pinned to `v1.7.7`, lints `.github/workflows/`. It is
  installed from source with `go install`, so no third-party action has to be trusted and
  the version never floats.
- **Services smoke job:** starts PostgreSQL and Redis with GitHub Actions `services:`
  using the **same pinned image versions as** `infrastructure/docker/docker-compose.yml`,
  then proves reachability with `psql` (`SELECT 1;`) and `redis-cli ping`. It creates
  **no schema, no migrations, and no product tables**.

There are **no deployment steps**. CI verifies the foundation only.

> The first CI run is the real test of the version pins — see "Version drift" above.

## Moderation: reports and blocks (KNOT-017a)

`internal/moderation` is the safety foundation: `role.go` (the `Role` type and its
helpers), `block.go` (`Block`, `BlockStore`, `BlockService`), `report.go`
(`Report`, categories, entity kinds, `ReportStore`, `EntityLookup`,
`ReportService`), `audit.go` (the append-only trail), `cursor.go`, `context.go`
(the request-scoped excluded-author set), `store.go` / `postgres_store.go` (pgx),
and `service.go` (the composed `Service` the composition root wires).

**Routes** (all protected): `POST /reports`, `GET /reports/mine`,
`POST /blocks/{user_id}`, `DELETE /blocks/{user_id}`, `GET /blocks/mine`. There is
no moderator route in this task; they ship in KNOT-017b.

**The block filter.** The auth middleware resolves the caller's mutual block set
once per request and attaches it to the request context. The content stores read it
with `moderation.ExcludedAuthors(ctx)` and append `author_id <> ALL($n::uuid[])` to
their list queries; when the set is empty the predicate is absent, so an anonymous
reader and a user with no blocks pay nothing. The content services refuse a write
whose target author is in the set and return `moderation.ErrBlocked`, which the
handlers map to **403 `blocked`**.

**The entity lookup.** `httpapi.EntityExistence` implements
`moderation.EntityLookup` over the four content services, so a report can confirm
its target exists without the moderation package importing a content domain.

Run the moderation integration tests against Postgres (migration 0015 applied) by
setting `KNOT_POSTGRES_DSN`; they skip otherwise.

### Block filtering is a cross-cutting concern — the audit checklist

Every content **list** endpoint and every content **detail** read must apply the
block filter, and every content **write** must refuse a blocked interaction. The
filter is the viewer's mutual block set, carried on the request context
(`moderation.ExcludedAuthors(ctx)`) and resolved once per authenticated request by
the auth middleware.

Two rules:

- **Lists** add `author_id <> ALL($n::uuid[])` (or `s.author_id` / `b.author_id`
  where the row is aliased) from `moderation.ExcludedAuthors(ctx)`. The predicate
  is absent when the set is empty, so anonymous readers pay nothing.
- **Detail** reads return **404** for a blocked author's resource
  (`moderation.IsExcludedAuthor(ctx, entity.AuthorID)` → the domain's not-found),
  because an invisible resource must be indistinguishable from one that does not
  exist. The profile wall follows the same rule.

The canonical checklist — every content read and its current state:

| Endpoint | Filter |
| --- | --- |
| `GET /stories` (feed) | list predicate (`stories.ListStories`) |
| `GET /stories/{id}` | detail 404 (`stories.GetStory`) |
| `GET /stories/{id}/tree` | structural — not filtered (KNOT-ADR-060) |
| `GET /versions/{id}` | detail 404 (`versions.GetVersion`) |
| `GET /versions/{id}/comments` | list predicate (`conversations.ListComments` + `ListReplies`) |
| `GET /comments/{id}` | detail 404 (`conversations.GetComment`) |
| `GET /inquiries` | list predicate (`inquiries.ListInquiries`) |
| `GET /inquiries/{id}` | detail 404 (`inquiries.GetInquiry`) |
| `GET /inquiries/{id}/answers` | list predicate (`inquiries.ListAnswers`) |
| `GET /answers/{id}` | detail 404 (`inquiries.GetAnswer`) |
| `GET /users/{id}/profile` | 404 when the owner is blocked (`profile.GetProfile`) |
| `GET /discovery/clusters` | list predicate (`discovery.ListClusters`) |
| `GET /discovery/places/{place}` | list predicate (`discovery.ListStoriesAtPlace`) |
| `GET /search` | list predicate (`search.Search`) |
| `GET /notifications` (+ `unread_count`) | list predicate on `actor_id` |
| `GET /reports/mine`, `GET /blocks/mine` | own data — no filter (N/A) |

A **write** (`POST`) is refused with `moderation.ErrBlocked` (→ 403 `blocked`) when
it targets content authored by anyone on either side of a block: commenting,
bridging, reacting, answering, and adapting.

**The client must send its token on these reads.** The filter only applies to an
authenticated request; a public read made without an `Authorization` header is
anonymous and sees everything. The mobile API modules (`stories.ts`,
`conversations.ts`, `versions.ts`, `profile.ts`, `discovery.ts`, `search.ts`)
therefore take an optional `token` and the screens pass the signed-in user's
token. A public route that must still personalise its response — such as the two
Discovery endpoints and Search — is wrapped in `authMiddleware.Optional`, which
resolves the block set for a signed-in caller and serves the anonymous view
otherwise (KNOT-017a-fix3, KNOT-018).

## KNOT-027 — the For You feed

`GET /stories` now takes `feed_type=recent|for_you`. `recent` is the merged chronological
feed unchanged (and the default, so older clients are untouched). `for_you` requires auth,
loads the reader's profile and `PillarEngagement`, fetches a bounded window of the newest
200 entries, enriches the candidates from `entity_features` in **one** batched `FeaturesFor`
read, and calls `relevance.NewSimpleRanker(...).Rank`. The handler slices a page out of the
ranked window and returns a ranked cursor (base64url JSON: rank, created_at, id). The
window is independent of the cursor, so page two re-ranks the same window and resumes by
**entry id** rather than by score: the recency term moves with the clock, and matching on
the score would fail on every page turn. `httpapi.FeedRanking` carries the three optional
dependencies (`Profiles`, `Engagement`, `Features`); without them `for_you` answers 500
instead of quietly serving an unranked feed. The mobile `FeedScreen` derives the ordering
from the selected tab (`For you` → `for_you`, `Latest` → `recent`); Wonder/Heritage/Nearby
stay visual-only, marked with a TODO.

## KNOT-027a — entity_features is maintained on write

The read model is no longer migration-only. `stories.PostgresStore.CreateStory` now runs
in an explicit transaction: the existing two-CTE insert, then an `entity_features` upsert
for **both** the story and its root version (the feed looks stories up by story id and
adaptations by version id, so one without the other leaves the model partial).
`versions.PostgresStore.CreateVersion` does the same for the new version only. The upsert
is a single `INSERT … SELECT … ON CONFLICT (entity_id) DO UPDATE` that derives language,
pillar and `lower(approximate_location)` **from the source tables**, so the row always
matches the projection migration 0020 and `BackfillEntityFeatures` use. A failed upsert
returns the create's error and the transaction rolls back — the stores expose the writer
as a field (`featureUpsert`) so a test can simulate that failure and prove no story or
version survives it. `UpsertEntityFeature` is also available on the stories store for a
standalone rebuild of one entity. No schema change: `entity_features` still has no foreign
key, so content deletion (no such path today) would leave a derived row behind.

## Testing Auth Locally

Knot's authentication verifies addresses and resets passwords with links that are **emailed
through Resend** (KNOT-ADR-090). How you read the link depends on whether
`KNOT_RESEND_API_KEY` is set:

| `KNOT_RESEND_API_KEY` | What happens to the link |
| --- | --- |
| **empty** (the default) | Nothing is sent. The no-op sender logs the whole message, link included, at `warn` level. This is the local round trip. |
| **set** (`re_…`) | The message is delivered by Resend to the real mailbox. Nothing is written to stdout. |

`KNOT_EMAIL_FROM` defaults to `Knot <onboarding@resend.dev>`, the Resend sandbox sender
that needs no verified domain. A deployment with its own domain sets it to that domain,
because the sandbox address only delivers to the Resend account's own mailbox.

Every message carries three views of the same secret (KNOT-031a-fix): an **https link**
behind the button (`{KNOT_WEB_URL}/auth/verify-email?token=…`), the **raw token** printed
underneath as `Or copy this code: …`, and the `knot://` **deep link** as a secondary note for
a client that opens custom schemes. The button is https because many mail clients do not
treat `knot://` as tappable. `KNOT_WEB_URL` defaults to `https://knot.local`, a placeholder
that resolves nowhere — there is no web app yet, so the visible token is the reliable path.

Running `go run ./cmd/knot` from `backend/go` loads the repository-root `.env` automatically,
so a value placed there needs no export. The file only fills in what is unset: a variable
exported in the shell always wins, and a missing `.env` is not an error.

### The full round trip (no Resend account needed)

```bash
# 1. Start the stack and the API, keeping the API's output.
scripts/dev-up.sh
cd backend/go && go run ./cmd/knot 2>&1 | tee /tmp/knot-api.log
#    The startup warning "KNOT_RESEND_API_KEY is empty" is expected.

# 2. Register. The response has NO tokens: a new account cannot sign in yet.
curl -sS -X POST localhost:8080/auth/register \
  -H 'Content-Type: application/json' \
  -d '{"email":"ada@example.com","password":"correct horse 7","display_name":"Ada Lovelace","agreed_to_terms":true}'

# 3. Read the message out of the log (the no-op sender writes it).
grep 'email delivery is disabled' -A 1 /tmp/knot-api.log
#   … body="Welcome to Knot. … https://knot.local/auth/verify-email?token=<TOKEN>
#      … Or copy this code: <TOKEN> … knot://auth/verify-email?token=<TOKEN> …

# 4. Verify. Sign-in is unlocked from this moment.
curl -sS -X POST localhost:8080/auth/verify-email \
  -H 'Content-Type: application/json' -d '{"token":"<TOKEN>"}'

# 5. Sign in. This is the only way to obtain a session.
curl -sS -X POST localhost:8080/auth/login \
  -H 'Content-Type: application/json' \
  -d '{"email":"ada@example.com","password":"correct horse 7","device_info":{"platform":"curl"}}'

# 6. The rest of the session surface.
curl -sS localhost:8080/auth/sessions -H "Authorization: Bearer <ACCESS_TOKEN>"
curl -sS -X POST localhost:8080/auth/logout -H "Authorization: Bearer <ACCESS_TOKEN>"

# 7. Password reset, same pattern.
curl -sS -X POST localhost:8080/auth/forgot-password \
  -H 'Content-Type: application/json' -d '{"email":"ada@example.com"}'
grep 'password reset' /tmp/knot-api.log
#   … knot://auth/reset-password?token=<TOKEN>
curl -sS -X POST localhost:8080/auth/reset-password \
  -H 'Content-Type: application/json' \
  -d '{"token":"<TOKEN>","new_password":"another strong 1"}'

# 8. Lost the verification link? Ask for another one. It always answers 200.
curl -sS -X POST localhost:8080/auth/resend-verification \
  -H 'Content-Type: application/json' -d '{"email":"ada@example.com"}'
```

With a real `KNOT_RESEND_API_KEY` in the environment, steps 3 and 7 change to "open the
mailbox". Everything else is identical.

### Deleting an account, recovering it, and purging it (KNOT-034)

Deletion is a soft delete with a 30-day window (KNOT-ADR-081, KNOT-ADR-089). The purge
that ends the window has **no scheduler yet** — KNOT-034a adds it — so locally you run it
by hand.

```bash
# 1. Delete. The password is required, and every session dies as this returns —
#    so keep the access token from step 5 in case you want to see that it is dead.
curl -sS -X DELETE localhost:8080/users/me \
  -H "Authorization: Bearer <ACCESS_TOKEN>" \
  -H 'Content-Type: application/json' -d '{"password":"at least 8 characters 1"}'

# The old token still verifies cryptographically for up to 15 minutes, but the
# session behind it is gone: the next refresh answers 401 invalid_refresh_token.

# 2. Sign in again inside the window. This *is* the recovery; the response says so.
curl -sS -X POST localhost:8080/auth/login \
  -H 'Content-Type: application/json' \
  -d '{"email":"ada@example.com","password":"at least 8 characters 1"}'
#   … "recovered":true

# 3. Or cancel it explicitly, without a token, from anywhere.
curl -sS -X POST localhost:8080/users/me/recover \
  -H 'Content-Type: application/json' \
  -d '{"email":"ada@example.com","password":"at least 8 characters 1"}'

# 4. To watch the purge without waiting 30 days, move an account's window into the past.
#    (Delete it first with step 1.)
docker exec -i knot-postgres psql -U knot -d knot -c \
  "UPDATE users SET deletion_scheduled_at = now() - interval '1 day'
     WHERE email = 'ada@example.com';"

# 5. Sign-in now answers 410 account_pending_purge, with or without the right password.

# 6. Purge. The secret is the local default from .env.example; without it the route
#    answers 401 and the handler logs the refusal (not the secret).
curl -sS -X POST localhost:8080/admin/purge-deleted \
  -H "X-Knot-Admin-Secret: knot_local_admin_secret_change_me"
#   … {"purged":1}

# 7. Run it again: purged_at makes the job idempotent.
#   … {"purged":0}

# 8. Check the row. Content is untouched, the person is not.
docker exec -i knot-postgres psql -U knot -d knot -c \
  "SELECT id, email, display_name, phone, avatar_url, password_hash, purged_at
     FROM users WHERE display_name = 'Deleted User';"
```

The API prints a warning at startup when `KNOT_ADMIN_SECRET` is still the local
placeholder; outside `local` the service refuses to start without a real one, because
that secret is the only thing standing between an operator and every due account.

### The same round trip in the app

The mobile client follows the identical path, and since KNOT-031-mobile it is the only
way a person signs in:

1. **Register** on the Create account screen. The app does **not** sign you in: an account
   cannot log in until its address is verified (KNOT-ADR-083).
2. **Read the email** (or the console, when `KNOT_RESEND_API_KEY` is empty). The screen says
   "Check your email", and the message carries an https link, the raw token under
   `Or copy this code:`, and the `knot://auth/verify-email?token=…` deep link.
3. **Paste the token** into the Verify screen (reachable from the register confirmation and
   from the login screen's "Enter verification token" button).
4. **Sign in.** The app stores only the refresh token (AsyncStorage, `knot.session.v2`) and
   keeps the 15-minute access token in memory. On the next launch it spends the refresh
   token to restore the session, so a revoked session cannot come back.
5. **Forgot password?** On the login screen. The reset link is emailed the same way, and
   the Reset screen takes the token plus a new password; resetting signs out every device.

`Settings → Sessions` lists the devices the account is signed in on, marks the current one,
and revokes the others.

Because the app refreshes the access token silently, a revoked session surfaces as a return
to the login screen rather than as a stream of failed requests: the client refreshes once on
a `401`, and signs out when the refresh itself is refused.

### Things that will surprise you

- **Every account created before KNOT-031 is unverified**, so it cannot sign in. Register
  the address again (it is taken) — instead, verify it by inserting a token, or simply
  create a new account.
- **Signing in six times revokes the first session.** The cap is five (KNOT-ADR-084), so a
  device list that keeps disappearing is working as designed.
- **Replaying a refresh token signs you out everywhere.** Rotation is per use, and a replayed
  token is treated as theft (KNOT-ADR-085). If you are testing refresh by hand, use the
  token from the *latest* response.
- **Rate limits are real and they are short.** Six rapid registrations from one address
  answer `429` with `Retry-After: 3600`. To clear the counters without waiting, flush Redis:
  ```bash
  docker exec knot-redis redis-cli --scan --pattern 'knot:auth:*' | xargs -r docker exec -i knot-redis redis-cli del
  ```
- **The log is a credential store.** During development, anyone who can read
  `/tmp/knot-api.log` can verify an address or reset a password. Do not post it in a review.

### What the tests cover

The backend tests use an in-process Redis (miniredis), so `go test ./...` needs Docker only
for the PostgreSQL integration tests:

```bash
cd backend/go
KNOT_POSTGRES_DSN='postgres://knot:knot_only_change_me@localhost:5433/knot?sslmode=disable' \
  go test ./... -count=1
```

`internal/auth/service_test.go` exercises every policy decision — the session cap, rotation,
reuse detection, the verification window — against fake stores and a real in-process cache.
