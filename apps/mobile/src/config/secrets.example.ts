/**
 * Mapbox public access token template.
 *
 * To use Mapbox in the app, copy this file to `secrets.local.ts` (same
 * directory) and replace the placeholder with your actual public token.
 *
 * `secrets.local.ts` is gitignored and must NEVER be committed.
 *
 * The public token starts with `pk.`. It is safe for client-side use
 * and is embedded in the built app. It is NOT the secret downloads
 * token (`sk.`), which stays in ~/.gradle/gradle.properties.
 */
export const KNOT_MAPBOX_TOKEN = 'pk.REPLACE_WITH_YOUR_PUBLIC_MAPBOX_TOKEN';
