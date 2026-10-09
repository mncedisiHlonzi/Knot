package language

import (
	"strings"
	"testing"
)

// southAfricanCodes are the ISO 639-3 codes for the languages the product
// promises to offer. It includes Sepedi ("nso"), which is the point of the move
// to ISO 639-3: Sepedi has no two-letter code, so the ISO 639-1 contract could not
// name it at all (KNOT-ADR-046).
var southAfricanCodes = map[string]string{
	"afr": "Afrikaans",
	"eng": "English",
	"nbl": "South Ndebele",
	"nso": "Pedi",
	"sot": "Southern Sotho",
	"ssw": "Swati",
	"tsn": "Tswana",
	"tso": "Tsonga",
	"ven": "Venda",
	"xho": "Xhosa",
	"zul": "Zulu",
}

func TestAllIsWellFormed(t *testing.T) {
	all := All()

	if len(all) < 7000 {
		t.Fatalf("len(All()) = %d, want at least 7000 languages", len(all))
	}

	codes := make(map[string]bool, len(all))
	names := make(map[string]bool, len(all))

	for _, item := range all {
		if len(item.Code) != 3 {
			t.Errorf("code %q is %d characters, want exactly 3", item.Code, len(item.Code))
		}
		for i := 0; i < len(item.Code); i++ {
			if item.Code[i] < 'a' || item.Code[i] > 'z' {
				t.Errorf("code %q contains %q, want only lower-case a-z", item.Code, string(item.Code[i]))
				break
			}
		}
		if strings.TrimSpace(item.Name) == "" {
			t.Errorf("code %q has an empty name", item.Code)
		}
		if codes[item.Code] {
			t.Errorf("code %q appears twice", item.Code)
		}
		if names[item.Name] {
			t.Errorf("name %q appears twice", item.Name)
		}
		codes[item.Code] = true
		names[item.Name] = true
	}

	// Names are sorted by code point, which is also the order the mobile list is
	// generated in, so the two lists can be compared element by element.
	for i := 1; i < len(all); i++ {
		if all[i-1].Name >= all[i].Name {
			t.Errorf("languages are not sorted by name: %q before %q", all[i-1].Name, all[i].Name)
		}
	}
}

func TestAllIncludesTheSouthAfricanLanguages(t *testing.T) {
	for code, name := range southAfricanCodes {
		if !IsValid(code) {
			t.Errorf("IsValid(%q) = false, want true", code)
			continue
		}
		if got, ok := Name(code); !ok || got != name {
			t.Errorf("Name(%q) = %q, %v; want %q, true", code, got, ok, name)
		}
	}
}

func TestIsValidAcceptsISO6393Codes(t *testing.T) {
	valid := []string{"eng", "zul", "afr", "fra", "por", "ara", "zho", "jpn", "swa", "nso", "tso"}
	for _, code := range valid {
		if !IsValid(code) {
			t.Errorf("IsValid(%q) = false, want true", code)
		}
	}
}

func TestIsValidRejectsEverythingElse(t *testing.T) {
	invalid := map[string]string{
		"empty":               "",
		"upper case":          "ENG",
		"mixed case":          "Eng",
		"two letters":         "en",
		"ISO 639-1 Zulu":      "zu",
		"english name":        "English",
		"one letter":          "e",
		"unknown code":        "zzz",
		"padded":              " eng",
		"two letter and dash": "en-ZA",
		"ISO 639-2/B German":  "ger",
	}
	for name, code := range invalid {
		if IsValid(code) {
			t.Errorf("%s: IsValid(%q) = true, want false", name, code)
		}
	}
}

func TestName(t *testing.T) {
	if got, ok := Name("eng"); !ok || got != "English" {
		t.Errorf("Name(%q) = %q, %v; want %q, true", "eng", got, ok, "English")
	}
	if got, ok := Name("zul"); !ok || got != "Zulu" {
		t.Errorf("Name(%q) = %q, %v; want %q, true", "zul", got, ok, "Zulu")
	}
	// The registry's own reference name, kept verbatim rather than translated to
	// "Northern Sotho" (KNOT-ADR-046).
	if got, ok := Name("nso"); !ok || got != "Pedi" {
		t.Errorf("Name(%q) = %q, %v; want %q, true", "nso", got, ok, "Pedi")
	}

	for _, code := range []string{"", "ENG", "en", "English", "zzz"} {
		if got, ok := Name(code); ok || got != "" {
			t.Errorf("Name(%q) = %q, %v; want \"\", false", code, got, ok)
		}
	}
}

func TestAllReturnsACopy(t *testing.T) {
	first := All()
	if len(first) == 0 {
		t.Fatal("All() returned nothing")
	}

	first[0] = Language{Code: "xxx", Name: "Mutated"}

	if All()[0].Code == "xxx" {
		t.Error("mutating the slice returned by All() changed the package's list")
	}
}
