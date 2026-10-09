package conversations

import (
	"errors"
	"strings"
	"testing"
	"time"
)

// A sentinel error and a *ValidationError must be distinguishable from each
// other, and a ValidationError must satisfy errors.Is(err, ErrValidation) while
// still being recoverable as the concrete type.
func TestValidationErrorContracts(t *testing.T) {
	err := &ValidationError{Field: "body", Message: "is required"}

	if !errors.Is(err, ErrValidation) {
		t.Error("errors.Is(err, ErrValidation) = false, want true")
	}
	if errors.Is(err, ErrNotFound) {
		t.Error("errors.Is(err, ErrNotFound) = true, want false — the sentinels must stay distinct")
	}

	var validation *ValidationError
	if !errors.As(err, &validation) {
		t.Fatalf("errors.As(err, &ValidationError) = false, want true")
	}
	if validation.Field != "body" {
		t.Errorf("field = %q, want %q", validation.Field, "body")
	}
	if got := err.Error(); !strings.Contains(got, "body") || !strings.Contains(got, "is required") {
		t.Errorf("Error() = %q, want it to name the field and the reason", got)
	}
}

func TestSentinelErrorsAreDistinct(t *testing.T) {
	sentinels := map[string]error{
		"ErrNotFound":       ErrNotFound,
		"ErrValidation":     ErrValidation,
		"ErrAlreadyBridged": ErrAlreadyBridged,
	}

	for name, sentinel := range sentinels {
		for otherName, other := range sentinels {
			if name == otherName {
				continue
			}
			if errors.Is(sentinel, other) {
				t.Errorf("%s satisfies %s, want the sentinels to be independent", name, otherName)
			}
		}
	}
}

func TestCommentCarriesItsFields(t *testing.T) {
	created := time.Date(2026, 10, 8, 12, 0, 0, 0, time.UTC)
	comment := Comment{
		ID:        "66666666-6666-4666-8666-666666666666",
		VersionID: "44444444-4444-4444-8444-444444444444",
		AuthorID:  "11111111-1111-4111-8111-111111111111",
		Language:  "eng",
		Body:      "The first rain remembers every name.",
		CreatedAt: created,
		UpdatedAt: created,
	}

	if comment.ID == "" || comment.VersionID == "" || comment.AuthorID == "" {
		t.Error("a comment id, version id, or author id is empty, want all three set")
	}
	if comment.Language != "eng" {
		t.Errorf("language = %q, want %q", comment.Language, "eng")
	}
	if !comment.CreatedAt.Equal(created) {
		t.Errorf("created at = %v, want %v", comment.CreatedAt, created)
	}
}
