package stories

import (
	"encoding/base64"
	"strings"
	"time"
)

// cursorSeparator divides the timestamp from the id inside the decoded payload.
// Neither component can contain it: RFC 3339 never uses "|", and a canonical
// UUID is hexadecimal digits and hyphens only.
const cursorSeparator = "|"

// Cursor is the position a feed page resumes from. It encodes the sort key of
// the last story already delivered — the (created_at, id) pair the feed orders
// by — so the next page can be selected with a single strict "less than"
// comparison.
//
// The fields are unexported because a cursor is opaque to clients: it must be
// round-tripped, not constructed. Use NewCursor to build one and DecodeCursor to
// parse one back from the wire.
type Cursor struct {
	createdAt time.Time
	id        string
}

// NewCursor returns the cursor that resumes immediately after the story
// identified by createdAt and id.
func NewCursor(createdAt time.Time, id string) Cursor {
	return Cursor{createdAt: createdAt.UTC(), id: id}
}

// CreatedAt returns the encoded sort timestamp, always in UTC.
func (c Cursor) CreatedAt() time.Time { return c.createdAt }

// ID returns the encoded story id.
func (c Cursor) ID() string { return c.id }

// Encode renders the cursor as an opaque, URL-safe token: the RFC 3339
// nanosecond timestamp and the id joined by cursorSeparator, base64url-encoded
// without padding.
//
// base64url without padding is used because the token travels in a query string
// and must not need escaping.
func (c Cursor) Encode() string {
	payload := c.createdAt.UTC().Format(time.RFC3339Nano) + cursorSeparator + c.id
	return base64.RawURLEncoding.EncodeToString([]byte(payload))
}

// String implements fmt.Stringer so a cursor interpolated into a log line or an
// error message prints as the opaque token rather than as a struct with an
// unexported field.
func (c Cursor) String() string { return c.Encode() }

// DecodeCursor parses a token produced by Encode.
//
// Every failure mode is reported as a *ValidationError on the "cursor" field,
// which also satisfies errors.Is(err, ErrValidation): a client-supplied cursor
// is untrusted input, and a bad one is a bad request rather than a server fault.
// Because the timestamp and id are both validated, the id is guaranteed to be
// canonical UUID text before it ever reaches a SQL cast.
func DecodeCursor(raw string) (Cursor, error) {
	decoded, err := base64.RawURLEncoding.DecodeString(raw)
	if err != nil {
		return Cursor{}, invalidCursor("must be a base64url-encoded token issued by this API")
	}

	timestamp, id, found := strings.Cut(string(decoded), cursorSeparator)
	if !found {
		return Cursor{}, invalidCursor("is missing its separator")
	}

	createdAt, err := time.Parse(time.RFC3339Nano, timestamp)
	if err != nil {
		return Cursor{}, invalidCursor("has a malformed timestamp")
	}

	if !isUUID(id) {
		return Cursor{}, invalidCursor("has a malformed id")
	}

	return Cursor{createdAt: createdAt.UTC(), id: id}, nil
}

// invalidCursor builds the cursor validation error.
func invalidCursor(reason string) error {
	return &ValidationError{Field: "cursor", Message: reason}
}
