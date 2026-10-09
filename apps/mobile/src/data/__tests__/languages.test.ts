import { existsSync, readFileSync } from 'fs';
import { resolve } from 'path';

import {
  LANGUAGES,
  MAX_PREFERRED_LANGUAGES,
  findLanguage,
  isLanguageCode,
  languageName,
  searchLanguages,
  toggleLanguage,
} from '../languages';

const CODE = /^[a-z]{3}$/;

/**
 * The South African codes the product promises to offer, with the name the SIL
 * reference gives them. Sepedi (nso) is here because ISO 639-3 names it; the
 * previous two-letter contract could not (KNOT-ADR-046).
 */
const SOUTH_AFRICAN_LANGUAGES: Readonly<Record<string, string>> = {
  afr: 'Afrikaans',
  eng: 'English',
  nbl: 'South Ndebele',
  nso: 'Pedi',
  sot: 'Southern Sotho',
  ssw: 'Swati',
  tsn: 'Tswana',
  tso: 'Tsonga',
  ven: 'Venda',
  xho: 'Xhosa',
  zul: 'Zulu',
};

describe('the canonical language list', () => {
  it('is large enough to be the real list, not a placeholder', () => {
    expect(LANGUAGES.length).toBeGreaterThanOrEqual(7000);
  });

  it('uses a three-letter lower-case code for every entry', () => {
    for (const language of LANGUAGES) {
      expect(language.code).toMatch(CODE);
    }
  });

  it('gives every entry a name', () => {
    for (const language of LANGUAGES) {
      expect(language.name.trim()).not.toBe('');
    }
  });

  it('has no duplicate code and no duplicate name', () => {
    const codes = LANGUAGES.map((language) => language.code);
    const names = LANGUAGES.map((language) => language.name);

    expect(new Set(codes).size).toBe(codes.length);
    expect(new Set(names).size).toBe(names.length);
  });

  it('is sorted by name, by code point', () => {
    // Code-point order, not `localeCompare`: the server's test asserts the same
    // strict order, and the two lists have to match entry for entry.
    const names = LANGUAGES.map((language) => language.name);
    const sorted = [...names].sort((a, b) => (a < b ? -1 : a > b ? 1 : 0));

    expect(names).toEqual(sorted);
  });

  it('offers every South African language with the name a person would expect', () => {
    for (const [code, name] of Object.entries(SOUTH_AFRICAN_LANGUAGES)) {
      expect(languageName(code)).toBe(name);
    }
  });

  it('offers Sepedi, which the old two-letter contract could not', () => {
    expect(findLanguage('nso')).toEqual({ code: 'nso', name: 'Pedi' });
  });
});

describe('findLanguage and isLanguageCode', () => {
  it('matches a code exactly', () => {
    expect(findLanguage('eng')?.name).toBe('English');
    expect(isLanguageCode('eng')).toBe(true);
  });

  it('rejects a code that is not canonical', () => {
    for (const value of [
      '',
      'EN',
      'ENG',
      'Eng',
      'en',
      'zu',
      'English',
      'e',
      'zzz',
      ' eng',
      'en-ZA',
    ]) {
      expect(isLanguageCode(value)).toBe(false);
      expect(findLanguage(value)).toBeUndefined();
    }
  });
});

describe('languageName', () => {
  it('returns the display name for a canonical code', () => {
    expect(languageName('afr')).toBe('Afrikaans');
    expect(languageName('zul')).toBe('Zulu');
  });

  it('falls back to the code itself when the list does not know it', () => {
    expect(languageName('en')).toBe('en');
    expect(languageName('zzz')).toBe('zzz');
  });
});

describe('searchLanguages', () => {
  it('returns the whole list for an empty query', () => {
    expect(searchLanguages('')).toEqual(LANGUAGES);
    expect(searchLanguages('   ')).toEqual(LANGUAGES);
  });

  it('matches the name and the code, ignoring case and surrounding space', () => {
    const zulu = { code: 'zul', name: 'Zulu' };

    expect(searchLanguages('zulu')).toEqual([zulu]);
    // A three-letter query is a substring of other names too — "Zulgo-Gemzek" —
    // so the assertion is that Zulu is among the matches, not that it is alone.
    expect(searchLanguages('ZUL')).toContainEqual(zulu);
    expect(searchLanguages(' zul ')).toContainEqual(zulu);
  });

  it('returns nothing when nothing matches', () => {
    expect(searchLanguages('not-a-language')).toEqual([]);
  });
});

describe('toggleLanguage', () => {
  it('adds a code that was not chosen', () => {
    expect(toggleLanguage([], 'eng')).toEqual(['eng']);
    expect(toggleLanguage(['eng'], 'zul')).toEqual(['eng', 'zul']);
  });

  it('removes a code that was chosen', () => {
    expect(toggleLanguage(['eng', 'zul'], 'eng')).toEqual(['zul']);
    expect(toggleLanguage(['eng'], 'eng')).toEqual([]);
  });

  it('keeps the order the codes were chosen in and never mutates the input', () => {
    const chosen = ['zul', 'eng'];

    expect(toggleLanguage(chosen, 'afr')).toEqual(['zul', 'eng', 'afr']);
    expect(chosen).toEqual(['zul', 'eng']);
  });
});

describe('MAX_PREFERRED_LANGUAGES', () => {
  it('matches the server\u2019s own cap on preferred languages', () => {
    expect(MAX_PREFERRED_LANGUAGES).toBe(20);
  });
});

/**
 * The Go and TypeScript lists are separate files that must stay identical, so
 * this reads the Go source and compares it entry for entry. It is skipped when
 * only the mobile folder is present, so the app's suite still runs on its own.
 */
const SERVER_LIST_PATH = resolve(
  __dirname,
  '../../../../../backend/go/internal/language/languages.go',
);
const serverListAvailable = existsSync(SERVER_LIST_PATH);

const describeServer = serverListAvailable ? describe : describe.skip;

describeServer('the canonical list matches the server', () => {
  const source = serverListAvailable ? readFileSync(SERVER_LIST_PATH, 'utf8') : '';

  it('lists exactly the same codes and names', () => {
    const entries = [...source.matchAll(/\{"([a-z]{3})", "([^"]+)"\}/g)].map((match) => ({
      code: match[1],
      name: match[2],
    }));

    expect(entries.length).toBeGreaterThanOrEqual(7000);
    expect(entries).toEqual(LANGUAGES.map((language) => ({ ...language })));
  });
});
