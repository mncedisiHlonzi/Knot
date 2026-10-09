package notifications

import (
	"encoding/base64"
	"errors"
	"strings"
	"testing"
	"time"
)

func TestCursorRoundTrip(t *testing.T) {
	cursor := NewCursor(testNow, testID)

	decoded, err := DecodeCursor(cursor.Encode())
	if err != nil {
		t.Fatalf("DecodeCursor() error = %v, want nil", err)
	}

	if !decoded.CreatedAt().Equal(testNow) {
		t.Errorf("created at = %s, want %s", decoded.CreatedAt(), testNow)
	}
	if decoded.ID() != testID {
		t.Errorf("id = %q, want %q", decoded.ID(), testID)
	}
}

func TestCursorIsUTC(t *testing.T) {
	// A cursor built from a non-UTC clock must still encode the same instant, so
	// two clients in different zones page through the same inbox identically.
	zone := time.FixedZone("SAST", 2*60*60)
	cursor := NewCursor(testNow.In(zone), testID)

	if cursor.CreatedAt().Location() != time.UTC {
		t.Errorf("created at location = %s, want UTC", cursor.CreatedAt().Location())
	}
	if !cursor.CreatedAt().Equal(testNow) {
		t.Errorf("created at = %s, want the same instant as %s", cursor.CreatedAt(), testNow)
	}
}

func TestCursorEncodeIsURLSafe(t *testing.T) {
	encoded := NewCursor(testNow, testID).Encode()

	if strings.ContainsAny(encoded, "+/=") {
		t.Errorf("encoded cursor = %q, want base64url without padding", encoded)
	}
}

func TestCursorStringMatchesEncode(t *testing.T) {
	cursor := NewCursor(testNow, testID)

	if cursor.String() != cursor.Encode() {
		t.Errorf("String() = %q, want %q", cursor.String(), cursor.Encode())
	}
}

func TestDecodeCursorRejectsMalformedTokens(t *testing.T) {
	tests := []struct {
		name    string
		token   string
		message string
	}{
		{
			name:    "not base64",
			token:   "!!!not-base64!!!",
			message: "base64url",
		},
		{
			name:    "empty",
			token:   "",
			message: "separator",
		},
		{
			name:    "no separator",
			token:   raw(base64.RawURLEncoding, "2026-10-09T12:00:00Z"),
			message: "separator",
		},
		{
			name:    "malformed timestamp",
			token:   raw(base64.RawURLEncoding, "yesterday|"+testID),
			message: "timestamp",
		},
		{
			name:    "malformed id",
			token:   raw(base64.RawURLEncoding, testNow.Format(time.RFC3339Nano)+"|not-a-uuid"),
			message: "id",
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			_, err := DecodeCursor(test.token)
			if err == nil {
				t.Fatal("DecodeCursor() error = nil, want an error")
			}
			if !errors.Is(err, ErrValidation) {
				t.Errorf("DecodeCursor() error = %v, want ErrValidation", err)
			}

			var validation *ValidationError
			if !errors.As(err, &validation) {
				t.Fatalf("DecodeCursor() error = %v, want a *ValidationError", err)
			}
			if validation.Field != "cursor" {
				t.Errorf("validation field = %q, want %q", validation.Field, "cursor")
			}
			if !strings.Contains(validation.Message, test.message) {
				t.Errorf("validation message = %q, want it to mention %q", validation.Message, test.message)
			}
		})
	}
}

func TestDecodeCursorNormalisesToUTC(t *testing.T) {
	// A token produced elsewhere in the world decodes to the same instant, in UTC.
	zone := time.FixedZone("NZST", 12*60*60)
	token := raw(base64.RawURLEncoding, testNow.In(zone).Format(time.RFC3339Nano)+"|"+testID)

	decoded, err := DecodeCursor(token)
	if err != nil {
		t.Fatalf("DecodeCursor() error = %v, want nil", err)
	}
	if decoded.CreatedAt().Location() != time.UTC {
		t.Errorf("created at location = %s, want UTC", decoded.CreatedAt().Location())
	}
	if !decoded.CreatedAt().Equal(testNow) {
		t.Errorf("created at = %s, want the same instant as %s", decoded.CreatedAt(), testNow)
	}
}

// raw encodes payload with the given base64 encoding, so a test can build a token
// that is decodable but not a valid cursor.
func raw(encoding *base64.Encoding, payload string) string {
	return encoding.EncodeToString([]byte(payload))
}
