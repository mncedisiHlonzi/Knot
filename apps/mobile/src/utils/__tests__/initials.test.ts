import { getInitials } from '../initials';

describe('getInitials', () => {
  it('uses the first and last words of a multi-word name', () => {
    expect(getInitials('Ada Lovelace')).toBe('AL');
    expect(getInitials('Mary Jane Watson')).toBe('MW');
  });

  it('uses up to two letters of a single-word name', () => {
    expect(getInitials('Prince')).toBe('PR');
  });

  it('returns one letter when the name has one letter', () => {
    expect(getInitials('A')).toBe('A');
  });

  it('uppercases the result', () => {
    expect(getInitials('ada lovelace')).toBe('AL');
  });

  it('ignores surrounding and repeated whitespace', () => {
    expect(getInitials('  Ada   Lovelace  ')).toBe('AL');
  });

  it('falls back to "?" when the name has no letters', () => {
    expect(getInitials('')).toBe('?');
    expect(getInitials('   ')).toBe('?');
  });

  it('never throws and falls back to "?" for a missing name', () => {
    expect(getInitials(null)).toBe('?');
    expect(getInitials(undefined)).toBe('?');
  });

  it('never throws for a non-string name', () => {
    // The API types describe the wire format optimistically; a malformed response
    // can deliver anything, so the guard must be a typeof check.
    expect(getInitials(42 as unknown as string)).toBe('?');
    expect(getInitials({} as unknown as string)).toBe('?');
    expect(getInitials(['Ada'] as unknown as string)).toBe('?');
  });
});
