package stories

import (
	"context"
	"errors"
	"testing"

	"github.com/knot/backend/internal/moderation"
)

// blockedStoryID is a distinct story id for the block-filter detail test.
const blockedStoryID = "aaaaaaaa-aaaa-4aaa-8aaa-aaaaaaaaaaaa"

// TestGetStoryBlockedAuthorIsNotFound proves a blocked author's story is reported
// as not found on the detail route, matching the list filter (KNOT-ADR-064).
func TestGetStoryBlockedAuthorIsNotFound(t *testing.T) {
	service, store := newTestService(t)
	store.getResult = Story{ID: blockedStoryID, AuthorID: authorID}

	ctx := moderation.WithExcludedAuthors(context.Background(), []string{authorID})
	if _, err := service.GetStory(ctx, blockedStoryID); !errors.Is(err, ErrNotFound) {
		t.Fatalf("GetStory() error = %v, want ErrNotFound", err)
	}

	// With no block set the same story is readable.
	if _, err := service.GetStory(context.Background(), blockedStoryID); err != nil {
		t.Fatalf("GetStory() with no block set error = %v, want nil", err)
	}
}
