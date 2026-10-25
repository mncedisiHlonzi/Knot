import { MIN_PASSWORD_LENGTH, utf8Length, validateEmail, validatePassword } from '../authForm';

/**
 * The client-side half of the password policy (KNOT-ADR-086).
 *
 * The server is the authority; these tests pin the mirror to the same rules so a
 * hint can never tell a person a password is fine when the API will reject it.
 */
describe('validatePassword', () => {
  it('accepts a password that meets the policy', () => {
    expect(validatePassword('correct horse 7')).toBeUndefined();
    expect(validatePassword('abcdefg1')).toBeUndefined();
  });

  it('rejects a password shorter than the minimum', () => {
    expect(validatePassword('abc1234')).toBe(
      `Password must be at least ${MIN_PASSWORD_LENGTH} characters.`,
    );
  });

  it('rejects a password with no digit', () => {
    expect(validatePassword('nodigitsatall')).toBe(
      'Password must contain at least one letter and one number.',
    );
  });

  it('rejects a password with no letter', () => {
    expect(validatePassword('12345678')).toBe(
      'Password must contain at least one letter and one number.',
    );
  });

  it('accepts a letter and a digit from any script', () => {
    expect(validatePassword('пароль12')).toBeUndefined();
    expect(validatePassword('café2026')).toBeUndefined();
  });

  it('measures the cap in UTF-8 bytes, as the server does', () => {
    // 1024 bytes of "é" (two bytes each) is 512 characters; one more character
    // pushes it over the server's cap.
    const atLimit = 'a'.repeat(1022) + '11';
    const overLimit = 'a'.repeat(1023) + '11';

    expect(utf8Length(atLimit)).toBe(1024);
    expect(validatePassword(atLimit)).toBeUndefined();
    expect(validatePassword(overLimit)).toBe('Password must be at most 1024 bytes.');
  });
});

describe('utf8Length', () => {
  it('counts one byte per ASCII character', () => {
    expect(utf8Length('abc')).toBe(3);
  });

  it('counts two bytes for a Latin-1 supplement character', () => {
    expect(utf8Length('é')).toBe(2);
  });

  it('counts three bytes for a BMP character outside Latin-1', () => {
    expect(utf8Length('あ')).toBe(3);
  });

  it('counts four bytes for an astral character', () => {
    expect(utf8Length('😀')).toBe(4);
  });
});

describe('validateEmail', () => {
  it('accepts a plausible address', () => {
    expect(validateEmail('ada@example.com')).toBeUndefined();
  });

  it('rejects an empty value', () => {
    expect(validateEmail('   ')).toBe('Email is required.');
  });

  it('rejects a value that is not an address', () => {
    expect(validateEmail('not-an-email')).toBe('Email must look like name@example.com.');
    expect(validateEmail('ada@example')).toBe('Email must look like name@example.com.');
    expect(validateEmail('ada @example.com')).toBe('Email must look like name@example.com.');
  });
});
