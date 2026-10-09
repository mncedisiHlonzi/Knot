package storymedia

import (
	"errors"
	"testing"
	"time"
)

func TestMediaTypeValid(t *testing.T) {
	for _, valid := range []MediaType{MediaTypeImage, MediaTypeVideo} {
		if !valid.Valid() {
			t.Errorf("%q.Valid() = false, want true", valid)
		}
	}
	for _, invalid := range []MediaType{"", "audio", "IMAGE"} {
		if invalid.Valid() {
			t.Errorf("%q.Valid() = true, want false", invalid)
		}
	}
}

func TestMediaSourceValid(t *testing.T) {
	for _, valid := range []MediaSource{MediaSourceCamera, MediaSourceGallery} {
		if !valid.Valid() {
			t.Errorf("%q.Valid() = false, want true", valid)
		}
	}
	for _, invalid := range []MediaSource{"", "screenshot", "Camera"} {
		if invalid.Valid() {
			t.Errorf("%q.Valid() = true, want false", invalid)
		}
	}
}

func TestAllowedMediaType(t *testing.T) {
	tests := []struct {
		mimeType  string
		mediaType MediaType
		extension string
		ok        bool
	}{
		{"image/jpeg", MediaTypeImage, ".jpg", true},
		{"image/png", MediaTypeImage, ".png", true},
		{"image/webp", MediaTypeImage, ".webp", true},
		{"video/mp4", MediaTypeVideo, ".mp4", true},
		{"video/quicktime", MediaTypeVideo, ".mov", true},
		{"image/gif", "", "", false},
		{"application/pdf", "", "", false},
		{"", "", "", false},
	}

	for _, test := range tests {
		mediaType, extension, ok := AllowedMediaType(test.mimeType)
		if ok != test.ok {
			t.Errorf("AllowedMediaType(%q) ok = %v, want %v", test.mimeType, ok, test.ok)
			continue
		}
		if !ok {
			continue
		}
		if mediaType != test.mediaType || extension != test.extension {
			t.Errorf("AllowedMediaType(%q) = (%q, %q), want (%q, %q)", test.mimeType, mediaType, extension, test.mediaType, test.extension)
		}
	}
}

func TestMaxBytesFor(t *testing.T) {
	if got := MaxBytesFor(MediaTypeImage); got != MaxImageBytes {
		t.Errorf("MaxBytesFor(image) = %d, want %d", got, MaxImageBytes)
	}
	if got := MaxBytesFor(MediaTypeVideo); got != MaxVideoBytes {
		t.Errorf("MaxBytesFor(video) = %d, want %d", got, MaxVideoBytes)
	}
	if got := MaxBytesFor("audio"); got != 0 {
		t.Errorf("MaxBytesFor(unknown) = %d, want 0", got)
	}
}

func TestStoryMediaKeyPrefix(t *testing.T) {
	const storyID = "55555555-5555-4555-8555-555555555555"

	prefix := StoryMediaKeyPrefix(storyID)
	if want := "story-media/" + storyID + "/"; prefix != want {
		t.Errorf("StoryMediaKeyPrefix() = %q, want %q", prefix, want)
	}
}

func TestTrimMimeType(t *testing.T) {
	tests := map[string]string{
		"image/png":                  "image/png",
		"image/png; charset=binary":  "image/png",
		"  video/mp4  ":              "video/mp4",
		"":                           "",
		"image/jpeg;charset=utf-8;a": "image/jpeg",
	}

	for input, want := range tests {
		if got := TrimMimeType(input); got != want {
			t.Errorf("TrimMimeType(%q) = %q, want %q", input, got, want)
		}
	}
}

func TestValidationErrorSatisfiesErrValidation(t *testing.T) {
	err := &ValidationError{Field: "source", Message: "must be camera or gallery"}

	if !errors.Is(err, ErrValidation) {
		t.Error("errors.Is(err, ErrValidation) = false, want true")
	}

	var validation *ValidationError
	if !errors.As(err, &validation) {
		t.Fatal("errors.As(err, &ValidationError) = false, want true")
	}
	if validation.Field != "source" {
		t.Errorf("field = %q, want source", validation.Field)
	}
	if got, want := err.Error(), "storymedia: invalid source: must be camera or gallery"; got != want {
		t.Errorf("Error() = %q, want %q", got, want)
	}
}

// cursorTime is a timestamp with nanoseconds set, so a round trip proves the
// RFC 3339 nanosecond format does not silently lose precision.
var cursorTime = time.Date(2026, 10, 9, 12, 30, 45, 123456789, time.UTC)

// cursorID is canonical UUID text, the only id shape a cursor may carry.
const cursorID = "55555555-5555-4555-8555-555555555555"

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

func TestCursorEncodingIsURLSafeAndStable(t *testing.T) {
	first := NewCursor(cursorTime, cursorID).Encode()
	second := NewCursor(cursorTime, cursorID).Encode()

	if first != second {
		t.Errorf("Encode() is not deterministic: %q != %q", first, second)
	}
	if got, want := NewCursor(cursorTime, cursorID).String(), first; got != want {
		t.Errorf("String() = %q, want %q", got, want)
	}
}

func TestDecodeCursorRejectsMalformedTokens(t *testing.T) {
	for _, token := range []string{"", "not a cursor!!!", "bm90LWEtY3Vyc29y"} {
		cursor, err := DecodeCursor(token)
		if err == nil {
			t.Fatalf("DecodeCursor(%q) error = nil, want an error", token)
		}
		if !errors.Is(err, ErrValidation) {
			t.Errorf("errors.Is(err, ErrValidation) = false, want true (err = %v)", err)
		}
		if cursor != (Cursor{}) {
			t.Errorf("cursor = %+v, want the zero cursor", cursor)
		}
	}
}
