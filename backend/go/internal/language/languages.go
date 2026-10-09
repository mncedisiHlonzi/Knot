package language

// Package language is the canonical language list Knot accepts for user data.
//
// A user, story, version, comment, or bridge names its language with an ISO
// 639-1 code — two lowercase letters, such as "en" or "zu" — and this package is
// the single source of truth for which codes exist. The service layer of every
// domain that stores a language validates against it, so free text ("English",
// "eng", "EN") is rejected at the edge rather than becoming inconsistent rows
// (KNOT-ADR-045).
//
// The list is a compile-time constant, not a database table: it changes rarely,
// and a lookup that cannot fail at runtime is one less thing to keep available.
// It is deliberately duplicated in the mobile app at
// apps/mobile/src/data/languages.ts — the two lists MUST stay identical; a change
// to one is a change to the other.

// Language is one canonical language: its two-letter ISO 639-1 code and its
// English display name.
type Language struct {
	// Code is the canonical 2-letter ISO 639-1 code, lower case.
	Code string
	// Name is the English display name, e.g. "Zulu".
	Name string
}

// languages is the canonical list, sorted alphabetically by Name. It is
// unexported so callers cannot mutate it; All returns a copy.
//
// Region variants (pt-BR, en-GB) and three-letter codes that have no ISO 639-1
// code (for example Northern Sotho's "nso") are deliberately out of scope: the
// contract is exactly the 2-letter ISO 639-1 set (KNOT-ADR-045).
var languages = []Language{
	{"af", "Afrikaans"},
	{"sq", "Albanian"},
	{"am", "Amharic"},
	{"ar", "Arabic"},
	{"hy", "Armenian"},
	{"az", "Azerbaijani"},
	{"eu", "Basque"},
	{"be", "Belarusian"},
	{"bn", "Bengali"},
	{"bs", "Bosnian"},
	{"bg", "Bulgarian"},
	{"my", "Burmese"},
	{"ca", "Catalan"},
	{"zh", "Chinese"},
	{"hr", "Croatian"},
	{"cs", "Czech"},
	{"da", "Danish"},
	{"nl", "Dutch"},
	{"en", "English"},
	{"et", "Estonian"},
	{"fi", "Finnish"},
	{"fr", "French"},
	{"gl", "Galician"},
	{"ka", "Georgian"},
	{"de", "German"},
	{"el", "Greek"},
	{"gu", "Gujarati"},
	{"ht", "Haitian Creole"},
	{"ha", "Hausa"},
	{"he", "Hebrew"},
	{"hi", "Hindi"},
	{"hu", "Hungarian"},
	{"is", "Icelandic"},
	{"ig", "Igbo"},
	{"id", "Indonesian"},
	{"ga", "Irish"},
	{"it", "Italian"},
	{"ja", "Japanese"},
	{"kn", "Kannada"},
	{"kk", "Kazakh"},
	{"km", "Khmer"},
	{"rw", "Kinyarwanda"},
	{"ko", "Korean"},
	{"ky", "Kyrgyz"},
	{"lo", "Lao"},
	{"lv", "Latvian"},
	{"lt", "Lithuanian"},
	{"lb", "Luxembourgish"},
	{"mk", "Macedonian"},
	{"mg", "Malagasy"},
	{"ms", "Malay"},
	{"ml", "Malayalam"},
	{"mt", "Maltese"},
	{"mi", "Maori"},
	{"mr", "Marathi"},
	{"mn", "Mongolian"},
	{"ne", "Nepali"},
	{"nb", "Norwegian Bokmål"},
	{"nn", "Norwegian Nynorsk"},
	{"ps", "Pashto"},
	{"fa", "Persian"},
	{"pl", "Polish"},
	{"pt", "Portuguese"},
	{"pa", "Punjabi"},
	{"ro", "Romanian"},
	{"ru", "Russian"},
	{"sm", "Samoan"},
	{"gd", "Scottish Gaelic"},
	{"sr", "Serbian"},
	{"st", "Sesotho"},
	{"sn", "Shona"},
	{"sd", "Sindhi"},
	{"si", "Sinhala"},
	{"sk", "Slovak"},
	{"sl", "Slovenian"},
	{"so", "Somali"},
	{"nr", "South Ndebele"},
	{"es", "Spanish"},
	{"su", "Sundanese"},
	{"sw", "Swahili"},
	{"ss", "Swati"},
	{"sv", "Swedish"},
	{"tl", "Tagalog"},
	{"tg", "Tajik"},
	{"ta", "Tamil"},
	{"tt", "Tatar"},
	{"te", "Telugu"},
	{"th", "Thai"},
	{"ts", "Tsonga"},
	{"tn", "Tswana"},
	{"tr", "Turkish"},
	{"tk", "Turkmen"},
	{"uk", "Ukrainian"},
	{"ur", "Urdu"},
	{"ug", "Uyghur"},
	{"uz", "Uzbek"},
	{"ve", "Venda"},
	{"vi", "Vietnamese"},
	{"cy", "Welsh"},
	{"xh", "Xhosa"},
	{"yi", "Yiddish"},
	{"yo", "Yoruba"},
	{"zu", "Zulu"},
}

// byCode indexes the canonical list by code, so IsValid and Name are one map
// lookup rather than a scan.
var byCode = func() map[string]Language {
	index := make(map[string]Language, len(languages))
	for _, item := range languages {
		index[item.Code] = item
	}
	return index
}()

// All returns the canonical list, sorted alphabetically by Name.
//
// It returns a copy, so a caller that sorts or truncates the result cannot alter
// the package's own list.
func All() []Language {
	out := make([]Language, len(languages))
	copy(out, languages)
	return out
}

// IsValid reports whether code is a canonical ISO 639-1 code.
//
// The code must match exactly: "en" is valid, while "EN", "eng", and "English"
// are not. Normalising case here would make "EN" a second spelling of "en" and
// re-introduce the inconsistency this list exists to remove (KNOT-ADR-045).
func IsValid(code string) bool {
	_, ok := byCode[code]
	return ok
}

// Name returns the English display name for code, and whether the code is
// canonical.
func Name(code string) (string, bool) {
	item, ok := byCode[code]
	if !ok {
		return "", false
	}
	return item.Name, true
}
