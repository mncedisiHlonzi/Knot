package stories

import (
	"context"
	"errors"
	"testing"
)

// ---------------------------------------------------------------------------
// Editing and permanent removal against PostgreSQL (KNOT-054)
// ---------------------------------------------------------------------------

// Editing rewrites the ROOT VERSION's content and touches the story row, which is
// what a client reads back; ownership is enforced by the same statement.
func TestIntegrationUpdateStoryRewritesTheRootVersion(t *testing.T) {
	env := integrationSetup(t)
	ctx := context.Background()

	created, err := env.store.CreateStory(ctx, newIntegrationStory(env.author, "edit"))
	if err != nil {
		t.Fatalf("CreateStory() error = %v, want nil", err)
	}

	updated, err := env.store.UpdateStory(ctx, created.ID, env.author, UpdateStoryInput{
		Title: "A revised title",
		Body:  "A revised body.",
	})
	if err != nil {
		t.Fatalf("UpdateStory() error = %v, want nil", err)
	}
	if updated.Title != "A revised title" || updated.Body != "A revised body." {
		t.Errorf("story = (%q, %q), want the revised text", updated.Title, updated.Body)
	}

	// The root version is where the content lives.
	var title, body string
	if err := env.pool.QueryRow(ctx, `SELECT title, body FROM story_versions WHERE id = $1`, created.RootVersionID).Scan(&title, &body); err != nil {
		t.Fatalf("could not read the root version: %v", err)
	}
	if title != "A revised title" || body != "A revised body." {
		t.Errorf("root version = (%q, %q), want the revised text", title, body)
	}

	// The story row's own timestamp moved, so a listing that reads only stories
	// can tell it changed.
	if !updated.UpdatedAt.After(created.UpdatedAt) {
		t.Errorf("updated_at = %v, want it after %v", updated.UpdatedAt, created.UpdatedAt)
	}

	// A stranger cannot edit it, and nothing is written.
	const stranger = "55555555-5555-4555-8555-555555555555"
	if _, err := env.store.UpdateStory(ctx, created.ID, stranger, UpdateStoryInput{Title: "Hijacked", Body: "Body."}); !errors.Is(err, ErrNotAuthor) {
		t.Errorf("UpdateStory() as a stranger error = %v, want ErrNotAuthor", err)
	}
	if _, err := env.store.GetStory(ctx, created.ID); err != nil {
		t.Fatalf("GetStory() error = %v, want the story still readable", err)
	}
}

// Permanent removal is only reachable from the bin, and it takes every version
// with the story.
func TestIntegrationPurgeStoryOnlyRemovesADeletedStory(t *testing.T) {
	env := integrationSetup(t)
	ctx := context.Background()

	created, err := env.store.CreateStory(ctx, newIntegrationStory(env.author, "purge"))
	if err != nil {
		t.Fatalf("CreateStory() error = %v, want nil", err)
	}

	// A live story is refused, and the row is untouched.
	if err := env.store.PurgeStory(ctx, created.ID, env.author); !errors.Is(err, ErrNotDeleted) {
		t.Errorf("PurgeStory() on a live story error = %v, want ErrNotDeleted", err)
	}
	if _, err := env.store.GetStory(ctx, created.ID); err != nil {
		t.Errorf("GetStory() error = %v, want the story untouched", err)
	}

	if err := env.store.DeleteStory(ctx, created.ID, env.author); err != nil {
		t.Fatalf("DeleteStory() error = %v, want nil", err)
	}

	// The bin serves it, and names only the caller's rows.
	bin, err := env.store.ListDeletedStories(ctx, env.author, MaxDeletedListLimit)
	if err != nil {
		t.Fatalf("ListDeletedStories() error = %v, want nil", err)
	}
	found := false
	for _, story := range bin {
		if story.ID == created.ID {
			found = true
		}
	}
	if !found {
		t.Error("the deleted story is not in its author's bin")
	}
	if _, err := env.store.GetDeletedStory(ctx, created.ID, env.author); err != nil {
		t.Fatalf("GetDeletedStory() error = %v, want nil", err)
	}

	// A stranger still cannot purge it.
	const stranger = "55555555-5555-4555-8555-555555555555"
	if err := env.store.PurgeStory(ctx, created.ID, stranger); !errors.Is(err, ErrNotAuthor) {
		t.Errorf("PurgeStory() as a stranger error = %v, want ErrNotAuthor", err)
	}

	if err := env.store.PurgeStory(ctx, created.ID, env.author); err != nil {
		t.Fatalf("PurgeStory() error = %v, want nil", err)
	}

	// The row and its root version are gone; the cascade did the rest.
	var stories, versions int
	if err := env.pool.QueryRow(ctx, `SELECT count(*) FROM stories WHERE id = $1`, created.ID).Scan(&stories); err != nil {
		t.Fatalf("could not count stories: %v", err)
	}
	if err := env.pool.QueryRow(ctx, `SELECT count(*) FROM story_versions WHERE id = $1`, created.RootVersionID).Scan(&versions); err != nil {
		t.Fatalf("could not count versions: %v", err)
	}
	if stories != 0 || versions != 0 {
		t.Errorf("remaining rows = (%d stories, %d versions), want 0 and 0", stories, versions)
	}

	// The derived read model has no foreign key, so the purge clears it explicitly.
	var features int
	if err := env.pool.QueryRow(
		ctx,
		`SELECT count(*) FROM entity_features WHERE entity_id = ANY($1::uuid[])`,
		[]string{created.ID, created.RootVersionID},
	).Scan(&features); err != nil {
		t.Fatalf("could not count entity features: %v", err)
	}
	if features != 0 {
		t.Errorf("entity_features rows = %d, want 0 for a purged story", features)
	}

	if _, err := env.store.GetDeletedStory(ctx, created.ID, env.author); !errors.Is(err, ErrNotFound) {
		t.Errorf("GetDeletedStory() after purge error = %v, want ErrNotFound", err)
	}
}
