/**
 * A tiny, polyfill-free query-string builder.
 *
 * React Native ships a `URLSearchParams` polyfill that implements the read methods
 * (`get`, `has`, `toString`) but **not** the mutation methods (`set`, `append`,
 * `delete`). Building a query with `new URLSearchParams()` therefore throws
 * "URLSearchParams.set is not implemented" at runtime on a device — a failure
 * neither TypeScript nor ESLint catches, because the DOM typings declare the
 * mutation methods that the React Native polyfill lacks (KNOT-017a-fix).
 *
 * This helper builds the string by hand instead, so no screen or API module ever
 * needs `URLSearchParams` for writing.
 */

/**
 * Builds a query string from a small set of scalar parameters, or `""` when
 * there is nothing to add.
 *
 * `undefined` and the empty string are omitted, so an absent cursor adds
 * nothing; a numeric `0` is kept, because it is a real value. Keys and values are
 * percent-encoded.
 */
export function buildQueryString(
  params: Readonly<Record<string, string | number | undefined>>,
): string {
  const parts: string[] = [];

  for (const [key, value] of Object.entries(params)) {
    if (value === undefined || value === '') {
      continue;
    }
    parts.push(`${encodeURIComponent(key)}=${encodeURIComponent(String(value))}`);
  }

  return parts.length === 0 ? '' : `?${parts.join('&')}`;
}
