import { MIN_SEARCH_LENGTH, canSearch } from '../search';

describe('canSearch', () => {
  it('accepts a phrase of at least the minimum length', () => {
    expect(canSearch({ q: 'Manguzi' })).toBe(true);
    expect(canSearch({ q: 'ab' })).toBe(true);
  });

  it('rejects a phrase shorter than the minimum length', () => {
    expect(canSearch({ q: 'a' })).toBe(false);
    expect(canSearch({ q: '' })).toBe(false);
    expect(canSearch({})).toBe(false);
  });

  it('ignores surrounding whitespace when measuring the phrase', () => {
    expect(canSearch({ q: '  ab  ' })).toBe(true);
    expect(canSearch({ q: '   a   ' })).toBe(false);
    expect(canSearch({ q: '      ' })).toBe(false);
  });

  it('counts characters, not UTF-16 code units', () => {
    // A multi-byte letter is one character for the server, so one is too short.
    expect(canSearch({ q: 'é' })).toBe(false);
    expect(canSearch({ q: 'éé' })).toBe(true);

    // An emoji is one code point but two code units: `.length` would say 2 and
    // send a request the server rejects with a 400.
    expect('👍'.length).toBeGreaterThan([...'👍'].length);
    expect(canSearch({ q: '👍' })).toBe(false);
  });

  it('accepts a filter with no phrase', () => {
    expect(canSearch({ type: 'inquiry' })).toBe(true);
    expect(canSearch({ pillar: 'wonder' })).toBe(true);
    expect(canSearch({ language: 'eng' })).toBe(true);
    expect(canSearch({ authorId: '11111111-1111-4111-8111-111111111111' })).toBe(true);
    expect(canSearch({ place: 'Manguzi' })).toBe(true);
  });

  it('accepts a short phrase when a filter is also present', () => {
    expect(canSearch({ q: 'a', type: 'story' })).toBe(true);
    expect(canSearch({ q: '', language: 'zul' })).toBe(true);
  });

  it('rejects empty strings posing as filters', () => {
    expect(canSearch({ q: 'a', type: undefined, pillar: undefined, language: '', place: '' })).toBe(
      false,
    );
  });

  it('exposes the same minimum the server enforces', () => {
    expect(MIN_SEARCH_LENGTH).toBe(2);
  });
});
