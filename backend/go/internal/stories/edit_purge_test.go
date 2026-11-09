package stories

import (
	"context"
	"errors"
	"strings"
	"testing"
)

// ---------------------------------------------------------------------------
// Editing and permanent removal (KNOT-054)
// ---------------------------------------------------------------------------

func TestUpdateStoryTrimsTheTitleAndKeepsTheBodyVerbatim(t *testing.T) {
	service, store := newTestService(t)
	store.updateResult = Story{ID: storyID, AuthorID: authorID, Title: "A new title"}

	updated, err := service.UpdateStory(context.Background(), storyID, authorID, UpdateStoryInput{
		Title: "  A new title  ",
		Body:  "  She kept a string of knots.\n",
	})
	if err != nil {
		t.Fatalf("UpdateStory() error = %v, want nil", err)
	}
	if updated.Title != "A new title" {
		t.Errorf("title = %q, want %q", updated.Title, "A new title")
	}
	if store.updateCalls != 1 {
		t.Errorf("store update calls = %d, want 1", store.updateCalls)
	}
	if store.gotUpdateID != storyID || store.gotUpdateAuthor != authorID {
		t.Errorf("store call = (%q, %q), want (%q, %q)", store.gotUpdateID, store.gotUpdateAuthor, storyID, authorID)
	}
	if store.gotUpdateInput.Title != "A new title" {
		t.Errorf("stored title = %q, want it trimmed", store.gotUpdateInput.Title)
	}
	// The body is the author's formatting: it must reach the store untouched.
	if store.gotUpdateInput.Body != "  She kept a string of knots.\n" {
		t.Errorf("stored body = %q, want it verbatim", store.gotUpdateInput.Body)
	}
}

func TestUpdateStoryValidation(t *testing.T) {
	cases := []struct {
		name  string
		input UpdateStoryInput
		field string
	}{
		{
			name:  "blank title",
			input: UpdateStoryInput{Title: "   ", Body: "Body."},
			field: "title",
		},
		{
			name:  "title too long",
			input: UpdateStoryInput{Title: strings.Repeat("a", MaxTitleLen+1), Body: "Body."},
			field: "title",
		},
		{
			name:  "blank body",
			input: UpdateStoryInput{Title: "Title", Body: "  \n "},
			field: "body",
		},
		{
			name:  "body too long",
			input: UpdateStoryInput{Title: "Title", Body: strings.Repeat("a", MaxBodyLen+1)},
			field: "body",
		},
	}

	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			service, store := newTestService(t)

			_, err := service.UpdateStory(context.Background(), storyID, authorID, testCase.input)

			if !errors.Is(err, ErrValidation) {
				t.Fatalf("error = %v, want ErrValidation", err)
			}
			var validation *ValidationError
			if !errors.As(err, &validation) || validation.Field != testCase.field {
				t.Errorf("validation = %+v, want field %q", validation, testCase.field)
			}
			if store.updateCalls != 0 {
				t.Errorf("store calls = %d, want 0", store.updateCalls)
			}
		})
	}
}

func TestUpdateStoryRejectsAMalformedIDWithoutTouchingTheStore(t *testing.T) {
	service, store := newTestService(t)

	_, err := service.UpdateStory(context.Background(), "not-a-uuid", authorID, UpdateStoryInput{Title: "T", Body: "B"})

	if !errors.Is(err, ErrNotFound) {
		t.Errorf("error = %v, want ErrNotFound", err)
	}
	if store.updateCalls != 0 {
		t.Errorf("store calls = %d, want 0", store.updateCalls)
	}
}

func TestUpdateStoryPassesThroughAnOwnershipRefusal(t *testing.T) {
	service, store := newTestService(t)
	store.updateErr = ErrNotAuthor

	_, err := service.UpdateStory(context.Background(), storyID, authorID, UpdateStoryInput{Title: "T", Body: "B"})

	if !errors.Is(err, ErrNotAuthor) {
		t.Errorf("error = %v, want ErrNotAuthor", err)
	}
}

func TestUpdateStoryWrapsAStoreFailure(t *testing.T) {
	service, store := newTestService(t)
	store.updateErr = errors.New("connection reset")

	_, err := service.UpdateStory(context.Background(), storyID, authorID, UpdateStoryInput{Title: "T", Body: "B"})

	if err == nil || !strings.Contains(err.Error(), "update story") {
		t.Errorf("error = %v, want a wrapped update failure", err)
	}
}

func TestGetDeletedStoryReturnsTheStoredStory(t *testing.T) {
	service, store := newTestService(t)
	store.deletedResult = Story{ID: storyID, AuthorID: authorID, Title: "In the bin"}

	story, err := service.GetDeletedStory(context.Background(), storyID, authorID)

	if err != nil {
		t.Fatalf("GetDeletedStory() error = %v, want nil", err)
	}
	if story.Title != "In the bin" {
		t.Errorf("title = %q, want %q", story.Title, "In the bin")
	}
	if store.deletedCalls != 1 || store.gotDeletedID != storyID || store.gotDeletedAuthor != authorID {
		t.Errorf("store call = (%d, %q, %q), want (1, %q, %q)",
			store.deletedCalls, store.gotDeletedID, store.gotDeletedAuthor, storyID, authorID)
	}
}

func TestGetDeletedStoryPassesThroughEveryRefusal(t *testing.T) {
	for _, sentinel := range []error{ErrNotFound, ErrNotAuthor, ErrNotDeleted} {
		service, store := newTestService(t)
		store.deletedErr = sentinel

		_, err := service.GetDeletedStory(context.Background(), storyID, authorID)

		if !errors.Is(err, sentinel) {
			t.Errorf("error = %v, want %v", err, sentinel)
		}
	}
}

func TestGetDeletedStoryWrapsAStoreFailure(t *testing.T) {
	service, store := newTestService(t)
	store.deletedErr = errors.New("connection reset")

	_, err := service.GetDeletedStory(context.Background(), storyID, authorID)

	if err == nil || !strings.Contains(err.Error(), "get deleted story") {
		t.Errorf("error = %v, want a wrapped read failure", err)
	}
}

func TestPurgeStoryPassesTheAuthorToTheStore(t *testing.T) {
	service, store := newTestService(t)

	if err := service.PurgeStory(context.Background(), storyID, authorID); err != nil {
		t.Fatalf("PurgeStory() error = %v, want nil", err)
	}
	if store.purgeCalls != 1 || store.gotPurgeID != storyID || store.gotPurgeAuthor != authorID {
		t.Errorf("store call = (%d, %q, %q), want (1, %q, %q)",
			store.purgeCalls, store.gotPurgeID, store.gotPurgeAuthor, storyID, authorID)
	}
}

func TestPurgeStoryPassesThroughTheStateRefusal(t *testing.T) {
	service, store := newTestService(t)
	store.purgeErr = ErrNotDeleted

	err := service.PurgeStory(context.Background(), storyID, authorID)

	if !errors.Is(err, ErrNotDeleted) {
		t.Errorf("error = %v, want ErrNotDeleted", err)
	}
}

func TestPurgeStoryRejectsAMalformedIDWithoutTouchingTheStore(t *testing.T) {
	service, store := newTestService(t)

	err := service.PurgeStory(context.Background(), storyID, "not-a-uuid")

	if !errors.Is(err, ErrNotFound) {
		t.Errorf("error = %v, want ErrNotFound", err)
	}
	if store.purgeCalls != 0 {
		t.Errorf("store calls = %d, want 0", store.purgeCalls)
	}
}

func TestPurgeStoryWrapsAStoreFailure(t *testing.T) {
	service, store := newTestService(t)
	store.purgeErr = errors.New("connection reset")

	err := service.PurgeStory(context.Background(), storyID, authorID)

	if err == nil || !strings.Contains(err.Error(), "purge story") {
		t.Errorf("error = %v, want a wrapped purge failure", err)
	}
}

func TestListDeletedStoriesReturnsTheBin(t *testing.T) {
	service, store := newTestService(t)
	store.listDeletedResult = []Story{{ID: storyID, AuthorID: authorID}}

	deleted, err := service.ListDeletedStories(context.Background(), authorID)

	if err != nil {
		t.Fatalf("ListDeletedStories() error = %v, want nil", err)
	}
	if len(deleted) != 1 || deleted[0].ID != storyID {
		t.Errorf("deleted = %+v, want the store's row", deleted)
	}
	if store.listDelCalls != 1 || store.gotDeletedListBy != authorID {
		t.Errorf("store call = (%d, %q), want (1, %q)", store.listDelCalls, store.gotDeletedListBy, authorID)
	}
	// The bin is read whole, but bounded.
	if store.gotDeletedLimit != MaxDeletedListLimit {
		t.Errorf("limit = %d, want %d", store.gotDeletedLimit, MaxDeletedListLimit)
	}
}

func TestListDeletedStoriesEmptyBinIsAnEmptySlice(t *testing.T) {
	service, _ := newTestService(t)

	deleted, err := service.ListDeletedStories(context.Background(), authorID)

	if err != nil {
		t.Fatalf("ListDeletedStories() error = %v, want nil", err)
	}
	if deleted == nil {
		t.Fatal("deleted = nil, want []")
	}
	if len(deleted) != 0 {
		t.Errorf("deleted = %d, want 0", len(deleted))
	}
}

func TestListDeletedStoriesRejectsAMalformedAuthor(t *testing.T) {
	service, store := newTestService(t)

	_, err := service.ListDeletedStories(context.Background(), "")

	if !errors.Is(err, ErrNotFound) {
		t.Errorf("error = %v, want ErrNotFound", err)
	}
	if store.listDelCalls != 0 {
		t.Errorf("store calls = %d, want 0", store.listDelCalls)
	}
}

func TestListDeletedStoriesWrapsAStoreFailure(t *testing.T) {
	service, store := newTestService(t)
	store.listDeletedErr = errors.New("connection reset")

	_, err := service.ListDeletedStories(context.Background(), authorID)

	if err == nil || !strings.Contains(err.Error(), "list deleted stories") {
		t.Errorf("error = %v, want a wrapped read failure", err)
	}
}
