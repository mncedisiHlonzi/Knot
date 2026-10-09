package language

import (
	"strings"
	"testing"
)

// southAfricanCodes are the ISO 639-1 codes for South African languages. Sepedi
// (Northern Sotho, "nso") is deliberately absent: it has no 2-letter ISO 639-1
// code, so it is outside this contract (KNOT-ADR-045).
var southAfricanCodes = []string{"af", "en", "nr", "ss", "st", "tn", "ts", "ve", "xh", "zu"}

func TestAllIsWellFormed(t *testing.T) {
	all := All()

	if len(all) < 80 {
		t.Fatalf("len(All()) = %d, want at least 80 languages", len(all))
	}

	codes := make(map[string]bool, len(all))
	names := make(map[string]bool, len(all))

	for _, item := range all {
		if len(item.Code) != 2 {
			t.Errorf("code %q is %d characters, want exactly 2", item.Code, len(item.Code))
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

	for i := 1; i < len(all); i++ {
		if all[i-1].Name >= all[i].Name {
			t.Errorf("languages are not sorted by name: %q before %q", all[i-1].Name, all[i].Name)
		}
	}
}

func TestAllIncludesTheSouthAfricanCodes(t *testing.T) {
	for _, code := range southAfricanCodes {
		if !IsValid(code) {
			t.Errorf("IsValid(%q) = false, want true", code)
		}
	}
}

func TestIsValid(t *testing.T) {
	valid := []string{"en", "zu", "af", "fr", "pt", "ar", "zh", "ja", "sw"}
	for _, code := range valid {
		if !IsValid(code) {
			t.Errorf("IsValid(%q) = false, want true", code)
		}
	}

	invalid := map[string]string{
		"empty":               "",
		"upper case":          "EN",
		"mixed case":          "En",
		"three letters":       "eng",
		"english name":        "English",
		"one letter":          "e",
		"nso (not ISO 639-1)": "nso",
		"unknown code":        "zz",
		"padded":              " en",
		"region variant":      "pt-BR",
	}
	for name, code := range invalid {
		if IsValid(code) {
			t.Errorf("%s: IsValid(%q) = true, want false", name, code)
		}
	}
}

func TestName(t *testing.T) {
	if got, ok := Name("en"); !ok || got != "English" {
		t.Errorf("Name(%q) = %q, %v; want %q, true", "en", got, ok, "English")
	}
	if got, ok := Name("zu"); !ok || got != "Zulu" {
		t.Errorf("Name(%q) = %q, %v; want %q, true", "zu", got, ok, "Zulu")
	}

	for _, code := range []string{"", "EN", "eng", "English", "zz"} {
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

	first[0] = Language{Code: "xx", Name: "Mutated"}

	if All()[0].Code == "xx" {
		t.Error("mutating the slice returned by All() changed the package's list")
	}
}
