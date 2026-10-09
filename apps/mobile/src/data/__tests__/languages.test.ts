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

const CODE = /^[a-z]{2}$/;

/** The South African codes the product promises to offer. */
const SOUTH_AFRICAN_CODES = ['af', 'en', 'nr', 'ss', 'st', 'tn', 'ts', 've', 'xh', 'zu'];

describe('the canonical language list', () => {
  it('is large enough to be the real list, not a placeholder', () => {
    expect(LANGUAGES.length).toBeGreaterThanOrEqual(80);
  });

  it('uses a two-letter lower-case code for every entry', () => {
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

  it('is sorted alphabetically by name', () => {
    const names = LANGUAGES.map((language) => language.name);
    const sorted = [...names].sort((a, b) => a.localeCompare(b, 'en'));

    expect(names).toEqual(sorted);
  });

  it('offers every South African code with the name a person would expect', () => {
    for (const code of SOUTH_AFRICAN_CODES) {
      expect(findLanguage(code)).toBeDefined();
    }
    expect(languageName('zu')).toBe('Zulu');
    expect(languageName('xh')).toBe('Xhosa');
  });

  it('does not offer nso, which has no ISO 639-1 code', () => {
    expect(findLanguage('nso')).toBeUndefined();
  });
});

describe('findLanguage and isLanguageCode', () => {
  it('matches a code exactly', () => {
    expect(findLanguage('en')?.name).toBe('English');
    expect(isLanguageCode('en')).toBe(true);
  });

  it('rejects a code that is not canonical', () => {
    for (const value of ['', 'EN', 'En', 'eng', 'English', 'e', 'zz', ' en', 'en-ZA']) {
      expect(isLanguageCode(value)).toBe(false);
      expect(findLanguage(value)).toBeUndefined();
    }
  });
});

describe('languageName', () => {
  it('returns the display name for a canonical code', () => {
    expect(languageName('af')).toBe('Afrikaans');
  });

  it('falls back to the code itself when the list does not know it', () => {
    expect(languageName('zz')).toBe('zz');
  });
});

describe('searchLanguages', () => {
  it('returns the whole list for an empty query', () => {
    expect(searchLanguages('')).toEqual(LANGUAGES);
    expect(searchLanguages('   ')).toEqual(LANGUAGES);
  });

  it('matches the name and the code, ignoring case and surrounding space', () => {
    expect(searchLanguages('zulu')).toEqual([{ code: 'zu', name: 'Zulu' }]);
    expect(searchLanguages('ZUL')).toEqual([{ code: 'zu', name: 'Zulu' }]);
    expect(searchLanguages(' zu ')).toEqual([{ code: 'zu', name: 'Zulu' }]);
  });

  it('returns nothing when nothing matches', () => {
    expect(searchLanguages('not-a-language')).toEqual([]);
  });
});

describe('toggleLanguage', () => {
  it('adds a code that was not chosen', () => {
    expect(toggleLanguage([], 'en')).toEqual(['en']);
    expect(toggleLanguage(['en'], 'zu')).toEqual(['en', 'zu']);
  });

  it('removes a code that was chosen', () => {
    expect(toggleLanguage(['en', 'zu'], 'en')).toEqual(['zu']);
    expect(toggleLanguage(['en'], 'en')).toEqual([]);
  });

  it('keeps the order the codes were chosen in and never mutates the input', () => {
    const chosen = ['zu', 'en'];

    expect(toggleLanguage(chosen, 'af')).toEqual(['zu', 'en', 'af']);
    expect(chosen).toEqual(['zu', 'en']);
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
    const entries = [...source.matchAll(/\{"([a-z]{2})", "([^"]+)"\}/g)].map((match) => ({
      code: match[1],
      name: match[2],
    }));

    expect(entries.length).toBeGreaterThanOrEqual(80);
    expect(entries).toEqual(LANGUAGES.map((language) => ({ ...language })));
  });
});
