package conversations

import (
	"context"
	"errors"
	"testing"

	"github.com/knot/backend/internal/moderation"
)

// blockCommenterID is a distinct UUID for the user attempting the blocked write.
const blockCommenterID = "99999999-9999-4999-8999-999999999999"

func TestCreateCommentRejectsBlockedVersionAuthor(t *testing.T) {
	service, comments, _ := newTestService(t)
	comments.versionAuthorResult = authorID

	ctx := moderation.WithExcludedAuthors(context.Background(), []string{authorID})
	_, err := service.CreateComment(ctx, CreateCommentInput{
		VersionID: versionID,
		AuthorID:  blockCommenterID,
		Language:  "eng",
		Body:      "hello",
	})

	if !errors.Is(err, moderation.ErrBlocked) {
		t.Fatalf("CreateComment() error = %v, want ErrBlocked", err)
	}
	if comments.createCalls != 0 {
		t.Errorf("CreateComment() wrote %d comments, want 0", comments.createCalls)
	}
}

func TestCreateCommentAllowsWhenTheAuthorIsNotBlocked(t *testing.T) {
	service, comments, _ := newTestService(t)
	comments.versionAuthorResult = authorID

	// The version author is not in the block set, so the comment is written.
	ctx := moderation.WithExcludedAuthors(context.Background(), []string{blockCommenterID})
	if _, err := service.CreateComment(ctx, CreateCommentInput{
		VersionID: versionID,
		AuthorID:  blockCommenterID,
		Language:  "eng",
		Body:      "hello",
	}); err != nil {
		t.Fatalf("CreateComment() error = %v, want nil", err)
	}
	if comments.createCalls != 1 {
		t.Errorf("CreateComment() wrote %d comments, want 1", comments.createCalls)
	}
}

func TestCreateBridgeRejectsBlockedSourceAuthor(t *testing.T) {
	service, comments, _ := newTestService(t)
	comments.getResult = Comment{
		ID:        sourceCommentID,
		VersionID: versionID,
		AuthorID:  authorID,
		Language:  "eng",
	}

	ctx := moderation.WithExcludedAuthors(context.Background(), []string{authorID})
	_, _, _, err := service.CreateBridge(ctx, CreateBridgeInput{
		SourceCommentID: sourceCommentID,
		AuthorID:        blockCommenterID,
		TargetLanguage:  "zul",
		Body:            "a bridge",
	})

	if !errors.Is(err, moderation.ErrBlocked) {
		t.Fatalf("CreateBridge() error = %v, want ErrBlocked", err)
	}
}
