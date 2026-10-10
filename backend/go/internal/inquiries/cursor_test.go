package inquiries

import (
	"errors"
	"strings"
	"testing"
	"time"
)

const (
	cursorID = "11111111-1111-4111-8111-111111111111"
	otherID  = "22222222-2222-4222-8222-222222222222"
)

func TestCursorRoundTrips(t *testing.T) {
	createdAt := time.Date(2026, 2, 3, 4, 5, 6, 700, time.UTC)
	encoded := NewCursor(createdAt, cursorID).Encode()

	decoded, err := DecodeCursor(encoded)
	if err != nil {
		t.Fatalf("DecodeCursor() error = %v, want nil", err)
	}
	if !decoded.CreatedAt().Equal(createdAt) {
		t.Errorf("CreatedAt() = %v, want %v", decoded.CreatedAt(), createdAt)
	}
	if decoded.ID() != cursorID {
		t.Errorf("ID() = %q, want %q", decoded.ID(), cursorID)
	}
}

// The token is opaque but must survive a query string unchanged, which is why the
// payload is base64url without padding.
func TestCursorEncodingIsURLSafe(t *testing.T) {
	encoded := NewCursor(time.Now().UTC(), cursorID).Encode()

	if strings.ContainsAny(encoded, "+/=") {
		t.Errorf("encoded cursor %q contains characters that need escaping in a query string", encoded)
	}
}

func TestCursorNormalisesToUTC(t *testing.T) {
	location := time.FixedZone("SAST", 2*60*60)
	local := time.Date(2026, 2, 3, 4, 5, 6, 0, location)

	cursor := NewCursor(local, cursorID)
	if cursor.CreatedAt().Location() != time.UTC {
		t.Errorf("CreatedAt() location = %v, want UTC", cursor.CreatedAt().Location())
	}
}

func TestDecodeCursorRejectsBadInput(t *testing.T) {
	valid := NewCursor(time.Now().UTC(), cursorID).Encode()

	cases := []struct {
		name string
		raw  string
	}{
		{name: "not base64", raw: "not-a-cursor!!!"},
		// base64url of "no separator here" has no "|".
		{name: "missing separator", raw: "bm8gc2VwYXJhdG9yIGhlcmU"},
		// base64url of "2026-02-03T04:05:06Z|" — a valid timestamp, empty id.
		{name: "empty id", raw: "MjAyNi0wMi0wM1QwNDowNTowNlp8"},
		// base64url of "not-a-time|11111111-1111-4111-8111-111111111111".
		{name: "malformed timestamp", raw: "bm90LWEtdGltZXwxMTExMTExMS0xMTExLTQxMTEtODExMS0xMTExMTExMTExMTE"},
		// A valid prefix with a truncated id.
		{name: "malformed id", raw: "MjAyNi0wMi0wM1QwNDowNTowNlp8YWJj"},
		{name: "empty", raw: ""},
	}

	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			if _, err := DecodeCursor(testCase.raw); err == nil {
				t.Fatal("DecodeCursor() error = nil, want an error")
			} else if !errors.Is(err, ErrValidation) {
				t.Errorf("DecodeCursor() error = %v, want it to satisfy ErrValidation", err)
			}
		})
	}

	// The control: the valid token still decodes, so the cases above fail for their
	// stated reason rather than because every input is rejected.
	if _, err := DecodeCursor(valid); err != nil {
		t.Fatalf("DecodeCursor(valid) error = %v, want nil", err)
	}
}

func TestDecodeCursorRejectsNonCanonicalUUID(t *testing.T) {
	// A UUID with the wrong separator positions: the id must be canonical UUID text
	// before it can reach a SQL cast.
	raw := NewCursor(time.Now().UTC(), "111111112111141118111111111111111111").Encode()

	if _, err := DecodeCursor(raw); err == nil {
		t.Fatal("DecodeCursor() error = nil, want an error for a non-canonical id")
	} else if !errors.Is(err, ErrValidation) {
		t.Errorf("DecodeCursor() error = %v, want it to satisfy ErrValidation", err)
	}
}

func TestCursorStringIsTheOpaqueToken(t *testing.T) {
	cursor := NewCursor(time.Now().UTC(), cursorID)

	if cursor.String() != cursor.Encode() {
		t.Errorf("String() = %q, want the encoded token %q", cursor.String(), cursor.Encode())
	}
}

func TestCursorDistinguishesSameInstantDifferentRows(t *testing.T) {
	createdAt := time.Date(2026, 2, 3, 4, 5, 6, 0, time.UTC)

	if NewCursor(createdAt, cursorID).Encode() == NewCursor(createdAt, otherID).Encode() {
		t.Error("two cursors on the same instant with different ids encoded identically")
	}
}
