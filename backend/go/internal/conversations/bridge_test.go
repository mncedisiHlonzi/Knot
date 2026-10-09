package conversations

import (
	"testing"
	"time"
)

func TestBridgeCarriesItsFields(t *testing.T) {
	created := time.Date(2026, 10, 8, 12, 0, 0, 0, time.UTC)
	bridge := Bridge{
		ID:              "77777777-7777-4777-8777-777777777777",
		SourceCommentID: "66666666-6666-4666-8666-666666666666",
		TargetCommentID: "88888888-8888-4888-8888-888888888888",
		AuthorID:        "11111111-1111-4111-8111-111111111111",
		TargetLanguage:  "fra",
		AdaptationNote:  "Rendered for French-speaking listeners.",
		CreatedAt:       created,
	}

	if bridge.SourceCommentID == bridge.TargetCommentID {
		t.Error("source and target comment ids are equal, want two distinct comments")
	}
	if bridge.TargetLanguage != "fra" {
		t.Errorf("target language = %q, want %q", bridge.TargetLanguage, "fra")
	}
	if bridge.AdaptationNote == "" {
		t.Error("adaptation note is empty, want the stored note")
	}
	if !bridge.CreatedAt.Equal(created) {
		t.Errorf("created at = %v, want %v", bridge.CreatedAt, created)
	}
}

// An empty AdaptationNote is the representation of "no note"; it must be usable
// as the zero value rather than requiring a pointer.
func TestBridgeAdaptationNoteIsOptional(t *testing.T) {
	bridge := Bridge{
		SourceCommentID: "66666666-6666-4666-8666-666666666666",
		TargetCommentID: "88888888-8888-4888-8888-888888888888",
		TargetLanguage:  "fra",
	}

	if bridge.AdaptationNote != "" {
		t.Errorf("adaptation note = %q, want the empty string for no note", bridge.AdaptationNote)
	}
}
