package search

import (
	"encoding/base64"
	"errors"
	"testing"
	"time"
)

// encodeRaw base64url-encodes a raw cursor payload, so a test can build a token
// that DecodeCursor must reject.
func encodeRaw(payload string) string {
	return base64.RawURLEncoding.EncodeToString([]byte(payload))
}

// TestKindValid proves the three supported kinds are accepted and everything else
// — including the empty string — is not.
func TestKindValid(t *testing.T) {
	valid := []Kind{KindStory, KindVersion, KindInquiry}
	for _, kind := range valid {
		if !kind.Valid() {
			t.Errorf("Kind(%q).Valid() = false, want true", kind)
		}
	}

	invalid := []Kind{"", "stories", "STORY", "comments", "story "}
	for _, kind := range invalid {
		if kind.Valid() {
			t.Errorf("Kind(%q).Valid() = true, want false", kind)
		}
	}
}

// TestTsQuery proves a phrase becomes a valid, prefix-matching tsquery, and that no
// character with meaning in tsquery syntax can survive from user input.
func TestTsQuery(t *testing.T) {
	tests := []struct {
		name   string
		phrase string
		want   string
	}{
		{name: "single word", phrase: "manguzi", want: "manguzi:*"},
		{name: "single word is lower-cased", phrase: "Manguzi", want: "manguzi:*"},
		{name: "two words", phrase: "cape town", want: "cape:* & town:*"},
		{name: "extra whitespace collapses", phrase: "  Cape   Town  ", want: "cape:* & town:*"},
		{name: "trailing punctuation is dropped", phrase: "why?", want: "why:*"},
		{name: "an apostrophe splits terms", phrase: "cattle's", want: "cattle:* & s:*"},
		{name: "digits are kept", phrase: "route 66", want: "route:* & 66:*"},
		{name: "non-ascii letters are kept", phrase: "café", want: "café:*"},
		{name: "tsquery operators cannot survive", phrase: "a&b|c!d(e)f:g<1>", want: "a:* & b:* & c:* & d:* & e:* & f:* & g:* & 1:*"},
		{name: "empty phrase is unmatchable", phrase: "", want: unmatchableTsQuery},
		{name: "punctuation only is unmatchable", phrase: "!!!", want: unmatchableTsQuery},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if got := tsQuery(test.phrase); got != test.want {
				t.Errorf("tsQuery(%q) = %q, want %q", test.phrase, got, test.want)
			}
		})
	}
}

// TestCursorRoundTrip proves a cursor survives encoding and decoding unchanged.
func TestCursorRoundTrip(t *testing.T) {
	const id = "11111111-1111-4111-8111-111111111111"
	created := time.Date(2026, 10, 3, 8, 30, 0, 0, time.UTC)

	encoded := NewCursor(0.123456789, created, id).Encode()

	decoded, err := DecodeCursor(encoded)
	if err != nil {
		t.Fatalf("DecodeCursor() error = %v, want nil", err)
	}
	if decoded.Rank() != 0.123456789 {
		t.Errorf("Rank() = %v, want 0.123456789", decoded.Rank())
	}
	if !decoded.CreatedAt().Equal(created) {
		t.Errorf("CreatedAt() = %v, want %v", decoded.CreatedAt(), created)
	}
	if decoded.ID() != id {
		t.Errorf("ID() = %q, want %q", decoded.ID(), id)
	}
}

// TestDecodeCursorRejectsMalformedTokens proves every failure mode is a validation
// error on the cursor field, so a bad token is a 400 rather than a 500.
func TestDecodeCursorRejectsMalformedTokens(t *testing.T) {
	const id = "11111111-1111-4111-8111-111111111111"
	timestamp := time.Date(2026, 10, 3, 8, 30, 0, 0, time.UTC).Format(time.RFC3339Nano)

	tests := []struct {
		name string
		raw  string
	}{
		{name: "not base64url", raw: "not a token!!"},
		{name: "two components", raw: encodeRaw("0.5|" + timestamp)},
		{name: "four components", raw: encodeRaw("0.5|" + timestamp + "|" + id + "|extra")},
		{name: "malformed rank", raw: encodeRaw("high|" + timestamp + "|" + id)},
		{name: "malformed timestamp", raw: encodeRaw("0.5|yesterday|" + id)},
		{name: "malformed id", raw: encodeRaw("0.5|" + timestamp + "|not-a-uuid")},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			_, err := DecodeCursor(test.raw)
			if err == nil {
				t.Fatalf("DecodeCursor(%q) error = nil, want an error", test.raw)
			}

			var validation *ValidationError
			if !errors.As(err, &validation) {
				t.Fatalf("DecodeCursor(%q) error = %v, want a *ValidationError", test.raw, err)
			}
			if validation.Field != "cursor" {
				t.Errorf("validation field = %q, want %q", validation.Field, "cursor")
			}
			if !errors.Is(err, ErrValidation) {
				t.Errorf("errors.Is(err, ErrValidation) = false, want true")
			}
		})
	}
}

// TestIsUUID proves the canonical-form check accepts a canonical UUID and rejects
// the shapes that must never reach a SQL cast.
func TestIsUUID(t *testing.T) {
	tests := []struct {
		value string
		want  bool
	}{
		{value: "11111111-1111-4111-8111-111111111111", want: true},
		{value: "aaaaaaaa-aaaa-4aaa-8aaa-aaaaaaaaaaaa", want: true},
		{value: "AAAAAAAA-AAAA-4AAA-8AAA-AAAAAAAAAAAA", want: true},
		{value: "", want: false},
		{value: "1111-1111-4111-8111-111111111111", want: false},
		{value: "11111111111141118111111111111111", want: false},
		{value: "11111111-1111-4111-8111-11111111111z", want: false},
		{value: "11111111_1111_4111_8111_111111111111", want: false},
	}

	for _, test := range tests {
		if got := isUUID(test.value); got != test.want {
			t.Errorf("isUUID(%q) = %v, want %v", test.value, got, test.want)
		}
	}
}
