import { KNOT_MAPBOX_TOKEN as LOCAL_MAPBOX_TOKEN } from './secrets.local';

/**
 * The backend URL the app talks to.
 *
 * LIVE BACKEND (the default): the Go API, the Rust media worker, PostgreSQL and
 * Redis run on Railway (KNOT-039). The URL is HTTPS, so requests work from any
 * network and in a release build, and a physical phone does not have to be on the
 * same Wi-Fi network as the Mac.
 *
 * LOCAL DEVELOPMENT (commented out below): the API runs on the founder's Mac and
 * a physical phone reaches it over the same Wi-Fi network. `localhost` on the
 * phone means the phone itself, so the Mac's LAN address is the only host that
 * works from a device — and it changes whenever the Mac joins a different
 * network. Read the current address with `ipconfig getifaddr en0` and start the
 * server with `cd backend/go && go run ./cmd/knot` (it listens on port 8080).
 * This is for office-only development; switch to it by swapping the comment on
 * the two lines below and rebuilding (see "Switching Between Railway and Local
 * Backend" in docs/DEVELOPMENT.md).
 *
 * Host rules while pointing at a local server:
 *
 *   - iOS Simulator     → http://localhost:8080    (shares the Mac's loopback)
 *   - Android emulator  → http://10.0.2.2:8080     (the emulator's host-loopback alias)
 *   - Physical device   → the Mac's Wi-Fi address  (http://10.92.86.173:8080)
 *
 * A debug build permits cleartext to a local server
 * (android/app/src/debug/res/xml/network_security_config.xml, KNOT-037-fix11); a
 * release build permits it only for the on-device video-cache proxy, which is a
 * further reason the HTTPS Railway URL is the default.
 *
 * Before Railway the API ran on Render (https://knot-backend-lsx0.onrender.com,
 * KNOT-037-fix14); Railway replaced it in KNOT-039.
 *
 * `API_BASE_URL` in `./api.ts` derives from this constant and strips a trailing
 * slash, so a URL may be written either way.
 */
export const DEV_API_URL = 'https://go-api-production-ee4e.up.railway.app';
// export const DEV_API_URL = 'http://10.92.86.173:8080';

/**
 * The Mapbox public access token (starts with `pk.`), used by the Discovery Map.
 *
 * The real value lives in `secrets.local.ts`, which is **gitignored** and must
 * never be committed (KNOT-ADR-027); `secrets.example.ts` is the committed
 * template to copy from. The committed `secrets.local.d.ts` declares the module
 * so `npm run typecheck` still succeeds before the local file has been created —
 * in that case Mapbox renders an invalid-token tile rather than crashing. See
 * docs/DEVELOPMENT.md, "Local secrets setup (after cloning)".
 *
 * This is the *public* token, safe for client-side use. It is NOT the *secret*
 * downloads token (`sk.`) that Gradle uses to fetch the Mapbox native SDK; that
 * one lives in `~/.gradle/gradle.properties` and must never be committed.
 */
export const KNOT_MAPBOX_TOKEN = LOCAL_MAPBOX_TOKEN;

/**
 * Upload size limits for story media, mirroring the backend's rules so the client
 * refuses an oversized file before spending the user's data on the upload
 * (KNOT-ADR-032).
 *
 *   - Images (JPEG, PNG, WebP): at most 10 MiB.
 *   - Videos (MP4, MOV):        at most 100 MiB.
 *
 * Avatars have their own, smaller 5 MiB limit, enforced by the backend alone.
 */
export const MAX_IMAGE_BYTES = 10 * 1024 * 1024;
export const MAX_VIDEO_BYTES = 100 * 1024 * 1024;
