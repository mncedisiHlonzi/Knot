package versions

import (
	"errors"
	"strings"
	"testing"
)

// A sentinel error and a *ValidationError must be distinguishable from each
// other, and a ValidationError must satisfy errors.Is(err, ErrValidation) while
// still being recoverable as the concrete type.
func TestValidationErrorContracts(t *testing.T) {
	err := &ValidationError{Field: "title", Message: "is required"}

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
	if validation.Field != "title" {
		t.Errorf("field = %q, want %q", validation.Field, "title")
	}
	if got := err.Error(); !strings.Contains(got, "title") || !strings.Contains(got, "is required") {
		t.Errorf("Error() = %q, want it to name the field and the reason", got)
	}
}

func TestSentinelErrorsAreDistinct(t *testing.T) {
	if errors.Is(ErrNotFound, ErrValidation) {
		t.Error("ErrNotFound satisfies ErrValidation, want the two to be independent")
	}
	if errors.Is(ErrValidation, ErrNotFound) {
		t.Error("ErrValidation satisfies ErrNotFound, want the two to be independent")
	}
}

func TestStoryVersionIsRoot(t *testing.T) {
	root := StoryVersion{ParentVersionID: ""}
	if !root.IsRoot() {
		t.Error("a version with no parent is not reported as the root")
	}

	child := StoryVersion{ParentVersionID: "44444444-4444-4444-8444-444444444444"}
	if child.IsRoot() {
		t.Error("a version with a parent is reported as the root")
	}
}
