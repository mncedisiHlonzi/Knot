/**
 * Jest-only placeholder for the gitignored secrets.local module.
 *
 * Production continues to use the real secrets.local.ts on the
 * developer's machine. Jest maps any import of `./secrets.local`
 * to this file so tests run on a fresh clone (CI) without the
 * gitignored file present.
 *
 * The token is a public placeholder for tests only; it is not a
 * real Mapbox token and is safe to commit.
 */
export const KNOT_MAPBOX_TOKEN = 'pk.test_placeholder_token_not_a_real_mapbox_token';
