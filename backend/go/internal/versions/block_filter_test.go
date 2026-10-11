package versions

import (
	"context"
	"errors"
	"testing"

	"github.com/knot/backend/internal/moderation"
)

// blockedVersionID is a distinct version id for the block-filter detail test.
const blockedVersionID = "bbbbbbbb-bbbb-4bbb-8bbb-bbbbbbbbbbbb"

// TestGetVersionBlockedAuthorIsNotFound proves a blocked author's version is
// reported as not found on the detail route (KNOT-ADR-064).
func TestGetVersionBlockedAuthorIsNotFound(t *testing.T) {
	service, store := newTestService(t)
	store.getResult = StoryVersion{
		ID:       blockedVersionID,
		StoryID:  serviceStoryID,
		AuthorID: serviceAuthorID,
	}

	ctx := moderation.WithExcludedAuthors(context.Background(), []string{serviceAuthorID})
	if _, err := service.GetVersion(ctx, blockedVersionID); !errors.Is(err, ErrNotFound) {
		t.Fatalf("GetVersion() error = %v, want ErrNotFound", err)
	}

	if _, err := service.GetVersion(context.Background(), blockedVersionID); err != nil {
		t.Fatalf("GetVersion() with no block set error = %v, want nil", err)
	}
}

// blockedAdapterID is a distinct user for the write-prevention test.
const blockedAdapterID = "cccccccc-cccc-4ccc-8ccc-cccccccccccc"

// TestCreateAdaptationRejectsBlockedParentAuthor proves a blocked user cannot
// adapt a version someone on either side of the block authored (KNOT-ADR-060).
func TestCreateAdaptationRejectsBlockedParentAuthor(t *testing.T) {
	service, store := newTestService(t)
	store.getResult = StoryVersion{
		ID:       serviceParentID,
		StoryID:  serviceStoryID,
		AuthorID: serviceAuthorID,
	}

	ctx := moderation.WithExcludedAuthors(context.Background(), []string{serviceAuthorID})
	_, err := service.CreateAdaptation(ctx, CreateAdaptationInput{
		StoryID:         serviceStoryID,
		ParentVersionID: serviceParentID,
		AuthorID:        blockedAdapterID,
		Language:        "zul",
		Title:           "t",
		Body:            "b",
	})
	if !errors.Is(err, moderation.ErrBlocked) {
		t.Fatalf("CreateAdaptation() error = %v, want ErrBlocked", err)
	}
	if store.createCalls != 0 {
		t.Errorf("CreateVersion called %d times, want 0", store.createCalls)
	}
}
