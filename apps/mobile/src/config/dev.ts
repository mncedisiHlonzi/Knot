import { KNOT_MAPBOX_TOKEN as LOCAL_MAPBOX_TOKEN } from './secrets.local';

/**
 * The backend URL for local development.
 *
 * The Knot API runs on the developer's own machine on port 8080, but the host
 * the app must dial depends on where the app is running:
 *
 *   - iOS Simulator     → http://localhost:8080    (shares the Mac's loopback)
 *   - Android emulator  → http://10.0.2.2:8080      (the emulator's host-loopback alias)
 *   - Physical device   → the Mac's Wi-Fi address   (e.g. http://10.27.80.173:8080)
 *
 * The value below is the founder's Mac Wi-Fi address at the time of writing. It
 * is not a secret and not production configuration — it is a per-developer,
 * per-network convenience default, tied to one machine on one Wi-Fi. A physical
 * phone can only reach the Mac over the LAN, and `localhost` on the phone means
 * the phone itself, so the LAN address is the only thing that works there.
 *
 * Because that address changes whenever the Mac joins a different network, this
 * file is the single place to update: change the constant and reload the app.
 * The phone and the Mac must be on the same Wi-Fi network.
 *
 * To read the Mac's current Wi-Fi address:  ipconfig getifaddr en0
 */
export const DEV_API_URL = 'http://10.27.80.173:8080';

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
