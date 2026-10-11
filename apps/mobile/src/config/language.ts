/**
 * The mobile app's single fallback language.
 *
 * Every language the app sends to the server is an ISO 639-3 code — three
 * lower-case letters, e.g. `eng` (KNOT-ADR-046). When a form has no usable
 * language to offer — an empty profile, a screen opened with nothing selected —
 * it falls back to this constant rather than repeating a literal.
 *
 * Keeping the canonical default in one place makes it a one-line change and lets
 * a regression test assert that no two-letter ISO 639-1 code (such as the old
 * `'en'`) creeps back in as a hard-coded default (KNOT-016-fix).
 */
export const DEFAULT_LANGUAGE_CODE = 'eng';
