/**
 * Author initials for an avatar fallback.
 *
 * It lives in one place so every fallback — an author line, a profile, an inbox
 * row — draws the same letters for the same name, and so the behaviour can be
 * tested without rendering a screen.
 */

/**
 * Returns the 1-2 uppercase letters to show when a person has no avatar image.
 *
 * The first and last words of the name are used, so "Ada Lovelace" becomes "AL"
 * and "Mary Jane Watson" becomes "MW". A single word contributes up to its first
 * two letters ("Prince" -> "PR"), and a one-letter name contributes one letter.
 *
 * A value that carries no letters — empty, whitespace-only, `null`, `undefined`,
 * or anything that is not a string — has nothing to take, so "?" is returned
 * rather than an empty fallback that would render as a blank circle. The input is
 * typed as nullable on purpose: the API response types describe the wire format
 * optimistically, and a response from an older server can omit the field
 * entirely, so a value may be `undefined` at runtime (KNOT-015b-fix).
 */
export function getInitials(displayName: string | null | undefined): string {
  // A typeof guard, not a truthiness check: a malformed response can deliver a
  // non-string (a number, an object), and only a string has `.trim`.
  if (typeof displayName !== 'string') {
    return '?';
  }

  const words = displayName
    .trim()
    .split(/\s+/)
    .filter((word) => word !== '');

  if (words.length === 0) {
    return '?';
  }

  const first = words[0].charAt(0);
  const last = words.length > 1 ? words[words.length - 1].charAt(0) : (words[0].charAt(1) ?? '');

  return (first + last).toUpperCase();
}
