package conversations

import (
	"bytes"
	"encoding/base64"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/knot/backend/internal/testutil"
)

// cursorTime is a timestamp with nanoseconds set, so a round trip proves the
// RFC 3339 nanosecond format does not silently lose precision.
var cursorTime = time.Date(2026, 10, 8, 12, 30, 45, 123456789, time.UTC)

// cursorID is canonical UUID text, which is the only id shape a cursor may carry.
const cursorID = "66666666-6666-4666-8666-666666666666"

func TestCursorRoundTrip(t *testing.T) {
	encoded := NewCursor(cursorTime, cursorID).Encode()

	decoded, err := DecodeCursor(encoded)
	if err != nil {
		t.Fatalf("DecodeCursor(%q) error = %v, want nil", encoded, err)
	}

	if !decoded.CreatedAt().Equal(cursorTime) {
		t.Errorf("created at = %v, want %v", decoded.CreatedAt(), cursorTime)
	}
	if decoded.ID() != cursorID {
		t.Errorf("id = %q, want %q", decoded.ID(), cursorID)
	}
}

func TestCursorRoundTripPreservesInstantsNotOffsets(t *testing.T) {
	// A cursor built from a non-UTC location must decode to the same instant
	// expressed in UTC, because only the instant matters to the SQL comparison.
	zone := time.FixedZone("UTC+2", 2*60*60)
	local := cursorTime.In(zone)

	decoded, err := DecodeCursor(NewCursor(local, cursorID).Encode())
	if err != nil {
		t.Fatalf("DecodeCursor() error = %v, want nil", err)
	}

	if !decoded.CreatedAt().Equal(local) {
		t.Errorf("created at = %v, want the same instant as %v", decoded.CreatedAt(), local)
	}
	if location := decoded.CreatedAt().Location(); location != time.UTC {
		t.Errorf("location = %v, want UTC", location)
	}
}

func TestCursorEncodingIsURLSafeAndStable(t *testing.T) {
	first := NewCursor(cursorTime, cursorID).Encode()
	second := NewCursor(cursorTime, cursorID).Encode()

	if first != second {
		t.Errorf("Encode() is not deterministic: %q != %q", first, second)
	}
	if strings.ContainsAny(first, "+/=") {
		t.Errorf("encoded cursor %q contains base64 characters that need escaping in a query string", first)
	}
}

func TestCursorStringDoesNotLeakStructure(t *testing.T) {
	cursor := NewCursor(cursorTime, cursorID)
	if got, want := cursor.String(), cursor.Encode(); got != want {
		t.Errorf("String() = %q, want the opaque token %q", got, want)
	}
}

func TestDecodeCursorRejectsMalformedTokens(t *testing.T) {
	tests := []struct {
		name  string
		token string
	}{
		{name: "empty", token: ""},
		{name: "not base64", token: "not a cursor!!!"},
		{name: "valid base64 but no separator", token: base64.RawURLEncoding.EncodeToString([]byte("nothing-here"))},
		{name: "malformed timestamp", token: encodeCursorPayload("not-a-timestamp|" + cursorID)},
		{name: "non-uuid id", token: encodeCursorPayload(cursorTime.Format(time.RFC3339Nano) + "|not-a-uuid")},
		{name: "uuid with invalid hex", token: encodeCursorPayload(cursorTime.Format(time.RFC3339Nano) + "|zzzzzzzz-1111-4111-8111-111111111111")},
		{name: "uuid of the wrong length", token: encodeCursorPayload(cursorTime.Format(time.RFC3339Nano) + "|11111111-1111-4111-8111-11111111111")},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			cursor, err := DecodeCursor(test.token)
			if err == nil {
				t.Fatalf("DecodeCursor(%q) error = nil, want an error", test.token)
			}
			if !errors.Is(err, ErrValidation) {
				t.Errorf("errors.Is(err, ErrValidation) = false, want true (err = %v)", err)
			}

			var validation *ValidationError
			if !errors.As(err, &validation) {
				t.Fatalf("errors.As(err, &ValidationError) = false, want true (err = %v)", err)
			}
			if validation.Field != "cursor" {
				t.Errorf("field = %q, want %q", validation.Field, "cursor")
			}
			if cursor != (Cursor{}) {
				t.Errorf("cursor = %+v, want the zero cursor", cursor)
			}
		})
	}
}

func TestDecodeCursorRejectsTamperedToken(t *testing.T) {
	// A cursor is the only client-supplied value that is decoded rather than
	// validated field by field, so a tampered one must be rejected outright.
	//
	// The mutation is applied to decoded bytes rather than to the encoded text,
	// and never to the final character: base64's last character carries only the
	// padding bits, so changing it often decodes to the identical payload and the
	// test would silently prove nothing. See testutil.TamperBase64Body.
	tests := []struct {
		name   string
		mutate func(payload []byte)
	}{
		{
			name: "id is no longer hexadecimal",
			mutate: func(payload []byte) {
				// Overwrite the id's first character with a letter that is not a
				// hex digit, which no UUID text can contain.
				payload[idOffset(payload)] = 'z'
			},
		},
		{
			name: "separator is destroyed",
			mutate: func(payload []byte) {
				index := strings.IndexByte(string(payload), cursorSeparator[0])
				if index >= 0 {
					payload[index] = '#'
				}
			},
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			original := NewCursor(cursorTime, cursorID).Encode()

			tampered, before, after := testutil.TamperBase64Body(t, base64.RawURLEncoding, original, test.mutate)

			if tampered == original {
				t.Fatal("tamper produced an identical token; the test would prove nothing")
			}
			// Guard the guard: the decoded payload really did change, so the
			// mutation was not a silent no-op.
			if bytes.Equal(before, after) {
				t.Fatal("tamper left the decoded payload unchanged")
			}

			if _, err := DecodeCursor(tampered); err == nil {
				t.Fatalf("DecodeCursor(%q) error = nil, want an error", tampered)
			} else if !errors.Is(err, ErrValidation) {
				t.Errorf("errors.Is(err, ErrValidation) = false, want true (err = %v)", err)
			}
		})
	}
}

func TestNewCursorPopulatesFields(t *testing.T) {
	cursor := NewCursor(cursorTime, cursorID)

	if !cursor.CreatedAt().Equal(cursorTime) {
		t.Errorf("CreatedAt() = %v, want %v", cursor.CreatedAt(), cursorTime)
	}
	if cursor.ID() != cursorID {
		t.Errorf("ID() = %q, want %q", cursor.ID(), cursorID)
	}
}

// idOffset returns the index at which the id begins in an encoded payload.
func idOffset(payload []byte) int {
	index := strings.IndexByte(string(payload), cursorSeparator[0])
	if index < 0 {
		return 0
	}
	return index + 1
}

// encodeCursorPayload builds a token from a raw payload, bypassing Encode so
// malformed payloads can be constructed.
func encodeCursorPayload(payload string) string {
	return base64.RawURLEncoding.EncodeToString([]byte(payload))
}
