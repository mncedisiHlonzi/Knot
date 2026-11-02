import { KNOT_MAPBOX_TOKEN as LOCAL_MAPBOX_TOKEN } from './secrets.local';

/**
 * The backend URL the app talks to.
 *
 * LOCAL DEVELOPMENT (the default): the API runs on the founder's Mac, and a
 * physical phone reaches it over the same Wi-Fi network. `localhost` on the phone
 * means the phone itself, so the Mac's LAN address is the only host that works
 * from a device — and it changes whenever the Mac joins a different network, which
 * is why this constant is the single place to update. To read the current
 * address: `ipconfig getifaddr en0`. Start the server with
 * `cd backend/go && go run ./cmd/knot` (it listens on port 8080).
 *
 * The value below is the Mac's Wi-Fi address on the office network
 * (KNOT-037-fix20). Host rules while pointing at a local server:
 *
 *   - iOS Simulator     → http://localhost:8080    (shares the Mac's loopback)
 *   - Android emulator  → http://10.0.2.2:8080     (the emulator's host-loopback alias)
 *   - Physical device   → the Mac's Wi-Fi address  (http://10.92.86.173:8080)
 *
 * A debug build permits cleartext for that local server
 * (android/app/src/debug/res/xml/network_security_config.xml, KNOT-037-fix11).
 *
 * CLOUD ALTERNATIVE (Render): the API was deployed at
 * https://knot-backend-lsx0.onrender.com (KNOT-037-fix14). To use it, replace the
 * constant with that URL — it is HTTPS, so it also works in a release build. The
 * free tier could not run the AVIF media pipeline fast enough, which is why local
 * is the default again (KNOT-037-fix20); switching back is this one line.
 *
 * `API_BASE_URL` in `./api.ts` derives from this constant and strips a trailing
 * slash, so a URL may be written either way.
 */
export const DEV_API_URL = 'http://10.92.86.173:8080';

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
