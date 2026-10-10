package reactions

import (
	"errors"
	"testing"
)

func TestReactionTypeValid(t *testing.T) {
	for _, reactionType := range ReactionTypes {
		if !reactionType.Valid() {
			t.Errorf("%q.Valid() = false, want true", reactionType)
		}
	}

	for _, invalid := range []ReactionType{"", "like", "RINGS_TRUE", "rings-true"} {
		if invalid.Valid() {
			t.Errorf("%q.Valid() = true, want false", invalid)
		}
	}
}

func TestReactionTypesAreTheFourSignals(t *testing.T) {
	want := []ReactionType{RingsTrue, KnowItDifferently, AddsSomethingNew, NeedsASource}
	if len(ReactionTypes) != len(want) {
		t.Fatalf("ReactionTypes has %d entries, want %d", len(ReactionTypes), len(want))
	}
	for i, reactionType := range want {
		if ReactionTypes[i] != reactionType {
			t.Errorf("ReactionTypes[%d] = %q, want %q", i, ReactionTypes[i], reactionType)
		}
	}
}

func TestEntityTypeValid(t *testing.T) {
	for _, entityType := range []EntityType{EntityStory, EntityVersion, EntityComment, EntityBridge} {
		if !entityType.Valid() {
			t.Errorf("%q.Valid() = false, want true", entityType)
		}
	}

	for _, invalid := range []EntityType{"", "profile", "Story", "notification"} {
		if invalid.Valid() {
			t.Errorf("%q.Valid() = true, want false", invalid)
		}
	}
}

func TestSummaryZeroValue(t *testing.T) {
	var summary Summary
	if summary.Total() != 0 {
		t.Errorf("zero Summary.Total() = %d, want 0", summary.Total())
	}
	for _, reactionType := range ReactionTypes {
		if count := summary.Count(reactionType); count != 0 {
			t.Errorf("zero Summary.Count(%q) = %d, want 0", reactionType, count)
		}
	}
}

func TestSummaryAddAndCount(t *testing.T) {
	var summary Summary
	summary.Add(RingsTrue)
	summary.Add(RingsTrue)
	summary.Add(NeedsASource)

	if got := summary.Count(RingsTrue); got != 2 {
		t.Errorf("Count(rings_true) = %d, want 2", got)
	}
	if got := summary.Count(NeedsASource); got != 1 {
		t.Errorf("Count(needs_a_source) = %d, want 1", got)
	}
	if got := summary.Count(KnowItDifferently); got != 0 {
		t.Errorf("Count(know_it_differently) = %d, want 0", got)
	}
	if got := summary.Total(); got != 3 {
		t.Errorf("Total() = %d, want 3", got)
	}
}

func TestSummaryAddIgnoresUnknownType(t *testing.T) {
	var summary Summary
	summary.Add(ReactionType("like"))
	if summary.Total() != 0 {
		t.Errorf("Total() = %d, want 0 after adding an unknown type", summary.Total())
	}
}

func TestSummaryCountUnknownTypeIsZero(t *testing.T) {
	summary := Summary{RingsTrue: 5}
	if got := summary.Count(ReactionType("like")); got != 0 {
		t.Errorf("Count(unknown) = %d, want 0", got)
	}
}

func TestValidationErrorUnwrapsToErrValidation(t *testing.T) {
	err := &ValidationError{Field: "reaction_type", Message: "must be one of the four"}
	if !errors.Is(err, ErrValidation) {
		t.Error("errors.Is(ValidationError, ErrValidation) = false, want true")
	}

	var validation *ValidationError
	if !errors.As(err, &validation) {
		t.Error("errors.As(ValidationError) = false, want true")
	}
	if validation.Field != "reaction_type" {
		t.Errorf("Field = %q, want reaction_type", validation.Field)
	}
}
