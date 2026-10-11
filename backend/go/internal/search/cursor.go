package search

import (
	"encoding/base64"
	"strconv"
	"strings"
	"time"
)

// cursorSeparator divides the three components of the decoded payload. Neither a
// formatted float nor an RFC 3339 timestamp ever contains it, and a canonical UUID
// is hexadecimal digits and hyphens only.
const cursorSeparator = "|"

// Cursor is the position a page resumes from.
//
// Search orders by (rank DESC, created_at DESC, id DESC), so the cursor carries
// all three: the relevance score of the last row delivered, its creation time, and
// its id. The id is what makes the page correct when two rows share a rank and a
// timestamp.
//
// **Rank is data-dependent.** `ts_rank` is a function of the matched document, so
// it is stable for a given phrase against a given corpus but changes when the
// corpus does. A page walked while content is being written can therefore skip or
// repeat a row, exactly as any ranked search does; the trade-off is accepted
// because ordering by recency alone would make search useless (documented in
// docs/DEVELOPMENT.md).
//
// The fields are unexported because a cursor is opaque to clients: it must be
// round-tripped, not constructed.
type Cursor struct {
	rank      float64
	createdAt time.Time
	id        string
}

// NewCursor returns the cursor that resumes immediately after the row identified
// by rank, createdAt, and id.
func NewCursor(rank float64, createdAt time.Time, id string) Cursor {
	return Cursor{rank: rank, createdAt: createdAt.UTC(), id: id}
}

// Rank returns the encoded relevance score.
func (c Cursor) Rank() float64 { return c.rank }

// CreatedAt returns the encoded sort timestamp, always in UTC.
func (c Cursor) CreatedAt() time.Time { return c.createdAt }

// ID returns the encoded row id.
func (c Cursor) ID() string { return c.id }

// Encode renders the cursor as an opaque, URL-safe token: the rank, the RFC 3339
// nanosecond timestamp, and the id joined by cursorSeparator, base64url-encoded
// without padding.
//
// base64url without padding is used because the token travels in a query string
// and must not need escaping.
func (c Cursor) Encode() string {
	payload := strconv.FormatFloat(c.rank, 'g', -1, 64) +
		cursorSeparator + c.createdAt.UTC().Format(time.RFC3339Nano) +
		cursorSeparator + c.id

	return base64.RawURLEncoding.EncodeToString([]byte(payload))
}

// String implements fmt.Stringer so a cursor interpolated into a log line prints
// as the opaque token rather than as a struct with unexported fields.
func (c Cursor) String() string { return c.Encode() }

// DecodeCursor parses a token produced by Encode.
//
// Every failure mode is reported as a *ValidationError on the "cursor" field,
// which also satisfies errors.Is(err, ErrValidation): a client-supplied cursor is
// untrusted input, and a bad one is a bad request rather than a server fault.
// Because the id is validated as canonical UUID text, it is safe to cast in SQL.
func DecodeCursor(raw string) (Cursor, error) {
	decoded, err := base64.RawURLEncoding.DecodeString(raw)
	if err != nil {
		return Cursor{}, invalidCursor("must be a base64url-encoded token issued by this API")
	}

	parts := strings.Split(string(decoded), cursorSeparator)
	if len(parts) != 3 {
		return Cursor{}, invalidCursor("is missing one of its three components")
	}

	rank, err := strconv.ParseFloat(parts[0], 64)
	if err != nil {
		return Cursor{}, invalidCursor("has a malformed rank")
	}

	createdAt, err := time.Parse(time.RFC3339Nano, parts[1])
	if err != nil {
		return Cursor{}, invalidCursor("has a malformed timestamp")
	}

	if !isUUID(parts[2]) {
		return Cursor{}, invalidCursor("has a malformed id")
	}

	return Cursor{rank: rank, createdAt: createdAt.UTC(), id: parts[2]}, nil
}

// invalidCursor builds the cursor validation error.
func invalidCursor(reason string) error {
	return &ValidationError{Field: "cursor", Message: reason}
}

// isUUID reports whether s is canonical UUID text (8-4-4-4-12 hexadecimal
// digits). It mirrors the check every other Knot package applies before casting a
// client-supplied string to uuid in SQL; the value is only ever used as a
// parameter, never interpolated.
func isUUID(s string) bool {
	if len(s) != 36 {
		return false
	}
	for i := 0; i < len(s); i++ {
		switch i {
		case 8, 13, 18, 23:
			if s[i] != '-' {
				return false
			}
		default:
			if !isHexDigit(s[i]) {
				return false
			}
		}
	}
	return true
}

// isHexDigit reports whether b is a hexadecimal digit, in either case.
func isHexDigit(b byte) bool {
	switch {
	case b >= '0' && b <= '9':
		return true
	case b >= 'a' && b <= 'f':
		return true
	case b >= 'A' && b <= 'F':
		return true
	default:
		return false
	}
}
