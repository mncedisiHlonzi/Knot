/**
 * Pure validation for the auth forms.
 *
 * The server is the source of truth — every rule here is enforced again in Go —
 * so these helpers exist only to catch a mistake before it costs a round trip. They
 * are kept out of the screens so they can be unit-tested without a renderer, and
 * they return a message rather than throwing so a screen can show it beside the
 * field.
 */

/** The shortest password the server accepts (`identity.MinPasswordLength`). */
export const MIN_PASSWORD_LENGTH = 8;

/** The longest password the server accepts, in bytes (it hashes them; this bounds work). */
export const MAX_PASSWORD_BYTES = 1024;

/** A pragmatic email shape check: one @, something on both sides, a dot after it. */
const EMAIL_PATTERN = /^[^\s@]+@[^\s@]+\.[^\s@]+$/;

/**
 * Returns the first problem with an email address, or undefined when it is
 * plausible. It does not try to be RFC 5322: the server parses the address for
 * real, and the only thing worth catching here is a typo.
 */
export function validateEmail(email: string): string | undefined {
  if (email.trim() === '') {
    return 'Email is required.';
  }
  if (!EMAIL_PATTERN.test(email.trim())) {
    return 'Email must look like name@example.com.';
  }
  return undefined;
}

/**
 * Returns the first problem with a password, or undefined when it satisfies the
 * server's policy (KNOT-ADR-086): at least 8 characters, at most 1024 bytes, and at
 * least one letter and one digit.
 *
 * The letter-and-digit rule is checked per rune, exactly as Go's
 * `unicode.IsLetter` / `unicode.IsDigit` do, so "café1" and "пароль1" pass on both
 * sides.
 */
export function validatePassword(password: string): string | undefined {
  // Byte length, not character count: the server's cap bounds hashing work.
  if (utf8Length(password) > MAX_PASSWORD_BYTES) {
    return `Password must be at most ${MAX_PASSWORD_BYTES} bytes.`;
  }
  if ([...password].length < MIN_PASSWORD_LENGTH) {
    return `Password must be at least ${MIN_PASSWORD_LENGTH} characters.`;
  }

  let hasLetter = false;
  let hasDigit = false;
  for (const character of password) {
    if (/\p{L}/u.test(character)) {
      hasLetter = true;
    } else if (/\p{Nd}/u.test(character)) {
      hasDigit = true;
    }
    if (hasLetter && hasDigit) {
      break;
    }
  }

  if (!hasLetter || !hasDigit) {
    return 'Password must contain at least one letter and one number.';
  }

  return undefined;
}

/**
 * Returns the UTF-8 length of a string, in bytes.
 *
 * `TextEncoder` is not guaranteed on Hermes, so the count is done directly: one
 * byte below U+0080, two below U+0800, three below U+10000, four above. That is
 * the exact UTF-8 encoding rule, and it is enough for a length cap — it never
 * needs to produce the bytes themselves.
 */
export function utf8Length(value: string): number {
  let bytes = 0;

  for (const character of value) {
    const codePoint = character.codePointAt(0) ?? 0;
    if (codePoint < 0x80) {
      bytes += 1;
    } else if (codePoint < 0x800) {
      bytes += 2;
    } else if (codePoint < 0x10000) {
      bytes += 3;
    } else {
      bytes += 4;
    }
  }

  return bytes;
}
