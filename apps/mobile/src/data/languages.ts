/**
 * The canonical language list Knot accepts for user data.
 *
 * A user, story, version, comment, or bridge names its language with an ISO
 * 639-1 code — two lowercase letters, such as "en" or "zu" — and the server
 * rejects anything that is not on this list (KNOT-ADR-045). The picker offers
 * exactly these entries, so a value the app can produce is a value the API
 * accepts, and free text like "English" or "EN" never reaches the server.
 *
 * This list is a deliberate duplicate of the server's canonical list at
 * backend/go/internal/language/languages.go. The two MUST stay identical: a
 * change to one is a change to the other. The test alongside this file reads the
 * Go source and fails when they drift apart.
 */

/** A canonical language: its two-letter ISO 639-1 code and its English name. */
export type Language = {
  /** The canonical 2-letter ISO 639-1 code, lower case. */
  readonly code: string;
  /** The English display name, e.g. "Zulu". */
  readonly name: string;
};

/**
 * The canonical list, sorted alphabetically by name.
 *
 * Region variants (pt-BR, en-GB) and three-letter codes that have no ISO 639-1
 * code (for example Northern Sotho's "nso") are deliberately out of scope: the
 * contract is exactly the 2-letter ISO 639-1 set.
 */
export const LANGUAGES: readonly Language[] = [
  { code: 'af', name: 'Afrikaans' },
  { code: 'sq', name: 'Albanian' },
  { code: 'am', name: 'Amharic' },
  { code: 'ar', name: 'Arabic' },
  { code: 'hy', name: 'Armenian' },
  { code: 'az', name: 'Azerbaijani' },
  { code: 'eu', name: 'Basque' },
  { code: 'be', name: 'Belarusian' },
  { code: 'bn', name: 'Bengali' },
  { code: 'bs', name: 'Bosnian' },
  { code: 'bg', name: 'Bulgarian' },
  { code: 'my', name: 'Burmese' },
  { code: 'ca', name: 'Catalan' },
  { code: 'zh', name: 'Chinese' },
  { code: 'hr', name: 'Croatian' },
  { code: 'cs', name: 'Czech' },
  { code: 'da', name: 'Danish' },
  { code: 'nl', name: 'Dutch' },
  { code: 'en', name: 'English' },
  { code: 'et', name: 'Estonian' },
  { code: 'fi', name: 'Finnish' },
  { code: 'fr', name: 'French' },
  { code: 'gl', name: 'Galician' },
  { code: 'ka', name: 'Georgian' },
  { code: 'de', name: 'German' },
  { code: 'el', name: 'Greek' },
  { code: 'gu', name: 'Gujarati' },
  { code: 'ht', name: 'Haitian Creole' },
  { code: 'ha', name: 'Hausa' },
  { code: 'he', name: 'Hebrew' },
  { code: 'hi', name: 'Hindi' },
  { code: 'hu', name: 'Hungarian' },
  { code: 'is', name: 'Icelandic' },
  { code: 'ig', name: 'Igbo' },
  { code: 'id', name: 'Indonesian' },
  { code: 'ga', name: 'Irish' },
  { code: 'it', name: 'Italian' },
  { code: 'ja', name: 'Japanese' },
  { code: 'kn', name: 'Kannada' },
  { code: 'kk', name: 'Kazakh' },
  { code: 'km', name: 'Khmer' },
  { code: 'rw', name: 'Kinyarwanda' },
  { code: 'ko', name: 'Korean' },
  { code: 'ky', name: 'Kyrgyz' },
  { code: 'lo', name: 'Lao' },
  { code: 'lv', name: 'Latvian' },
  { code: 'lt', name: 'Lithuanian' },
  { code: 'lb', name: 'Luxembourgish' },
  { code: 'mk', name: 'Macedonian' },
  { code: 'mg', name: 'Malagasy' },
  { code: 'ms', name: 'Malay' },
  { code: 'ml', name: 'Malayalam' },
  { code: 'mt', name: 'Maltese' },
  { code: 'mi', name: 'Maori' },
  { code: 'mr', name: 'Marathi' },
  { code: 'mn', name: 'Mongolian' },
  { code: 'ne', name: 'Nepali' },
  { code: 'nb', name: 'Norwegian Bokmål' },
  { code: 'nn', name: 'Norwegian Nynorsk' },
  { code: 'ps', name: 'Pashto' },
  { code: 'fa', name: 'Persian' },
  { code: 'pl', name: 'Polish' },
  { code: 'pt', name: 'Portuguese' },
  { code: 'pa', name: 'Punjabi' },
  { code: 'ro', name: 'Romanian' },
  { code: 'ru', name: 'Russian' },
  { code: 'sm', name: 'Samoan' },
  { code: 'gd', name: 'Scottish Gaelic' },
  { code: 'sr', name: 'Serbian' },
  { code: 'st', name: 'Sesotho' },
  { code: 'sn', name: 'Shona' },
  { code: 'sd', name: 'Sindhi' },
  { code: 'si', name: 'Sinhala' },
  { code: 'sk', name: 'Slovak' },
  { code: 'sl', name: 'Slovenian' },
  { code: 'so', name: 'Somali' },
  { code: 'nr', name: 'South Ndebele' },
  { code: 'es', name: 'Spanish' },
  { code: 'su', name: 'Sundanese' },
  { code: 'sw', name: 'Swahili' },
  { code: 'ss', name: 'Swati' },
  { code: 'sv', name: 'Swedish' },
  { code: 'tl', name: 'Tagalog' },
  { code: 'tg', name: 'Tajik' },
  { code: 'ta', name: 'Tamil' },
  { code: 'tt', name: 'Tatar' },
  { code: 'te', name: 'Telugu' },
  { code: 'th', name: 'Thai' },
  { code: 'ts', name: 'Tsonga' },
  { code: 'tn', name: 'Tswana' },
  { code: 'tr', name: 'Turkish' },
  { code: 'tk', name: 'Turkmen' },
  { code: 'uk', name: 'Ukrainian' },
  { code: 'ur', name: 'Urdu' },
  { code: 'ug', name: 'Uyghur' },
  { code: 'uz', name: 'Uzbek' },
  { code: 've', name: 'Venda' },
  { code: 'vi', name: 'Vietnamese' },
  { code: 'cy', name: 'Welsh' },
  { code: 'xh', name: 'Xhosa' },
  { code: 'yi', name: 'Yiddish' },
  { code: 'yo', name: 'Yoruba' },
  { code: 'zu', name: 'Zulu' },
];

/**
 * The most languages one person may claim to speak.
 *
 * It mirrors the server's own cap (`identity.maxLanguages`): sending more is a
 * validation error, so the picker stops the user before the request is made.
 */
export const MAX_PREFERRED_LANGUAGES = 20;

/** Looks up a language by its code, or undefined when the code is not canonical. */
export function findLanguage(code: string): Language | undefined {
  return LANGUAGES.find((language) => language.code === code);
}

/** Reports whether code is a canonical ISO 639-1 code, matched exactly. */
export function isLanguageCode(code: string): boolean {
  return findLanguage(code) !== undefined;
}

/**
 * Returns the English name for a code, falling back to the code itself.
 *
 * The fallback keeps a stored value readable when it is something the canonical
 * list does not know — an older row, or a code added to the server first.
 */
export function languageName(code: string): string {
  return findLanguage(code)?.name ?? code;
}

/**
 * Returns the selection a multi-select picker should hold after code is tapped:
 * code is added when it was absent, and removed when it was present.
 *
 * The order codes were chosen in is preserved, so the list reads the way the user
 * built it, and the input array is never mutated.
 */
export function toggleLanguage(codes: readonly string[], code: string): readonly string[] {
  if (codes.includes(code)) {
    return codes.filter((entry) => entry !== code);
  }

  return [...codes, code];
}

/**
 * Filters the canonical list by a free-text query.
 *
 * The query is matched case-insensitively against both the name and the code, so
 * "zu", "zulu", and "ZUL" all find Zulu. An empty query returns the whole list in
 * its canonical order.
 */
export function searchLanguages(query: string): readonly Language[] {
  const needle = query.trim().toLowerCase();

  if (needle === '') {
    return LANGUAGES;
  }

  return LANGUAGES.filter(
    (language) =>
      language.name.toLowerCase().includes(needle) || language.code.toLowerCase().includes(needle),
  );
}
