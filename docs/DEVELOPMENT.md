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
- **Go:** local development runs **Go 1.27**; `backend/go/go.mod` declares `go 1.22` and
  CI installs **Go 1.22**.

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
| Object layout       | `avatars/<user id>/<uuid>.jpg`                                     |
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

Because the data directory is on a named volume, `scripts/dev-down.sh` keeps every
uploaded avatar and `scripts/dev-reset.sh` deletes it along with the databases.

Backend settings: `KNOT_S3_ENDPOINT`, `KNOT_S3_REGION`, `KNOT_S3_ACCESS_KEY`,
`KNOT_S3_SECRET_KEY`, and `KNOT_S3_BUCKET` — see "Backend configuration" below. In
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

### Mapbox setup

The Discovery Map renders with Mapbox (KNOT-ADR-026). Two different tokens are involved, and
they are easy to confuse:

- **Public access token (`pk.…`)** — the token the app uses at runtime to draw tiles. Public
  tokens are safe to ship inside a client app, but this one is **not committed**: it lives in
  the gitignored local files `apps/mobile/src/config/secrets.local.ts` and
  `apps/mobile/android/app/src/main/res/values/mapbox.xml` (KNOT-ADR-027). See
  [Local secrets setup (after cloning)](#local-secrets-setup-after-cloning) below. If it is ever
  abused, rotate it in the Mapbox console and update both local files.

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
  -d '{"email":"you@example.com","password":"at-least-8-chars","display_name":"You"}'
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
| `POST /comments/{id}/bridges`  | `Bearer` | `internal/httpapi/conversations_handler.go` |
| `GET /comments/{id}/bridges`   | public   | `internal/httpapi/conversations_handler.go` |
| `GET /bridges/{id}`            | public   | `internal/httpapi/conversations_handler.go` |
| `POST /users/me/rooted`        | `Bearer` | `internal/httpapi/rooted_handler.go` |
| `GET /users/me/rooted`         | `Bearer` | `internal/httpapi/rooted_handler.go` |
| `GET /users/{id}/rooted`       | public   | `internal/httpapi/rooted_handler.go` |
| `GET /discovery/clusters`      | public   | `internal/httpapi/discovery_handler.go` |
| `GET /discovery/places/{place}` | public  | `internal/httpapi/discovery_handler.go` |
| `POST /users/me/avatar`        | `Bearer` | `internal/httpapi/avatar_handler.go` |
| `GET /users/{id}/avatar`       | public   | `internal/httpapi/avatar_handler.go` |

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

### Conversations and bridges

`POST /versions/{id}/comments`, `GET /versions/{id}/comments`,
`POST /comments/{id}/bridges`, `GET /comments/{id}/bridges`, and `GET /bridges/{id}` are
served by `internal/httpapi/conversations_handler.go`, backed by the
`internal/conversations` domain package. A comment belongs to one story version; a bridge
records that a comment is an adaptation of another comment.

Comment pagination reuses the feed's keyset pattern. The cursor is duplicated in
`internal/conversations/cursor.go` rather than shared, following KNOT-ADR-010, and the
query matches `comments_version_id_created_at_idx`:

```sql
SELECT id, version_id, author_id, language, body, created_at, updated_at
FROM comments
WHERE version_id = $1 AND (created_at, id) < ($2::timestamptz, $3::uuid)
ORDER BY created_at DESC, id DESC
LIMIT $4
```

Creating a bridge resolves the target version first — the source comment's story, plus the
version of that story written in `target_language` (the oldest, when several share it) —
then writes a target comment and a bridge in one transaction: the target comment is
inserted first, the bridge second, and a failure on either rolls both back. There is no
circular foreign key here, so a plain `Begin`/`Commit` is enough — contrast the
single-statement CTE the story plus its root version needs, above.

Bridging is open to any authenticated user. The bridger comes from the token, and the only
structural checks are that the target language differs from the source comment's language
and that the comment has not already been bridged into that language (see
KNOT-ADR-014/015).

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
unreadable — and a user with no public signal simply carries `null`.

```sql
SELECT id, user_id, place, duration_bucket, is_public, is_primary, created_at, updated_at
FROM rooted_signals
WHERE is_primary = true AND is_public = true AND user_id = ANY($1)
```

That is the single batch query behind the enrichment; `$1` is a `uuid[]`, so `user_id`'s
index stays usable. The `author_rooted` projection carries only `place` and
`duration_bucket`.

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
  - `KNOT_ENV`, `KNOT_POSTGRES_DSN`, `KNOT_REDIS_ADDR`, `KNOT_HTTP_PORT`,
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
