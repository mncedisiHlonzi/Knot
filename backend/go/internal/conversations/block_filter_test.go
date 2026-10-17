package conversations

import (
	"context"
	"errors"
	"testing"

	"github.com/knot/backend/internal/moderation"
)

// createFilterUser inserts a second user, scoped to the run's prefix so cleanup
// removes it, and returns its id. It is the "blocked author" in these tests.
func createFilterUser(t *testing.T, env integrationEnv) string {
	t.Helper()

	var id string
	err := env.pool.QueryRow(
		context.Background(),
		`INSERT INTO users (email, password_hash, display_name, preferred_languages)
		 VALUES ($1, $2, $3, $4)
		 RETURNING id`,
		env.prefix+"blocked@example.test",
		"$argon2id$v=19$m=65536,t=1,p=4$c2FsdA$a2V5",
		"Blocked Author",
		[]string{"eng"},
	).Scan(&id)
	if err != nil {
		t.Fatalf("createFilterUser() error = %v, want nil", err)
	}
	return id
}

// insertFilterComment writes one comment and returns the stored row.
func insertFilterComment(t *testing.T, env integrationEnv, versionID, authorID, body string, parentID *string) Comment {
	t.Helper()

	comment, err := env.store.CreateComment(context.Background(), Comment{
		VersionID:       versionID,
		AuthorID:        authorID,
		Language:        "eng",
		Body:            body,
		ParentCommentID: parentID,
	})
	if err != nil {
		t.Fatalf("CreateComment(%q) error = %v, want nil", body, err)
	}
	return comment
}

// commentPageHasID reports whether a page of comments contains the id.
func commentPageHasID(page []Comment, id string) bool {
	for _, comment := range page {
		if comment.ID == id {
			return true
		}
	}
	return false
}

// TestPostgresStoreBlockFilterHidesCommentsAndReplies proves the comment thread
// hides a blocked author's top-level comment and their reply, while another
// author's comment and the thread remain.
func TestPostgresStoreBlockFilterHidesCommentsAndReplies(t *testing.T) {
	env := integrationSetup(t)
	ctx := context.Background()

	_, versionID := newIntegrationStory(t, env, "block-comments")
	blocked := createFilterUser(t, env)

	visible := insertFilterComment(t, env, versionID, env.author, "visible comment", nil)
	hiddenTop := insertFilterComment(t, env, versionID, blocked, "hidden top", nil)
	hiddenReply := insertFilterComment(t, env, versionID, blocked, "hidden reply", &visible.ID)

	filtered := moderation.WithExcludedAuthors(ctx, []string{blocked})

	page, _, err := env.store.ListComments(filtered, versionID, nil, 20)
	if err != nil {
		t.Fatalf("ListComments() error = %v, want nil", err)
	}
	if commentPageHasID(page, hiddenTop.ID) {
		t.Error("blocked author's top-level comment is visible")
	}
	if !commentPageHasID(page, visible.ID) {
		t.Error("unblocked author's comment is missing")
	}

	replies, err := env.store.ListReplies(filtered, []string{visible.ID})
	if err != nil {
		t.Fatalf("ListReplies() error = %v, want nil", err)
	}
	if commentPageHasID(replies, hiddenReply.ID) {
		t.Error("blocked author's reply is visible")
	}

	// Without a block set the hidden rows are present: the filter, not a missing row.
	unfiltered, _, err := env.store.ListComments(ctx, versionID, nil, 20)
	if err != nil {
		t.Fatalf("ListComments() error = %v, want nil", err)
	}
	if !commentPageHasID(unfiltered, hiddenTop.ID) {
		t.Error("unfiltered page is missing the blocked author's comment")
	}
}

// TestGetCommentBlockedAuthorIsNotFound proves a blocked author's comment is
// reported as not found on the detail route (KNOT-ADR-064).
func TestGetCommentBlockedAuthorIsNotFound(t *testing.T) {
	service, comments, _ := newTestService(t)
	comments.getResult = Comment{ID: sourceCommentID, VersionID: versionID, AuthorID: authorID}

	ctx := moderation.WithExcludedAuthors(context.Background(), []string{authorID})
	if _, err := service.GetComment(ctx, sourceCommentID); !errors.Is(err, ErrNotFound) {
		t.Fatalf("GetComment() error = %v, want ErrNotFound", err)
	}

	if _, err := service.GetComment(context.Background(), sourceCommentID); err != nil {
		t.Fatalf("GetComment() with no block set error = %v, want nil", err)
	}
}
