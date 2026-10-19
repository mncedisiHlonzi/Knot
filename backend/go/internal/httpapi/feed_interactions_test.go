package httpapi

import (
	"encoding/json"
	"errors"
	"net/http"
	"testing"
	"time"

	"github.com/knot/backend/internal/identity"
	"github.com/knot/backend/internal/interactions"
	"github.com/knot/backend/internal/reactions"
)

// The interaction actors are distinct from every other fixture id in this
// package, so a failure names the row it is about.
const (
	feedReactorID   = "90000000-0000-4000-8000-000000000007"
	feedCommenterID = "90000000-0000-4000-8000-000000000008"

	feedReactorName   = "Tycoon"
	feedCommenterName = "Yantuntu"
)

// feedStoryItems decodes the story entries of a page into their wire type, which
// carries recent_interactions; feedStories deliberately decodes the bare story
// shape and would drop it.
func feedStoryItems(t *testing.T, page feedPage) []feedStoryItem {
	t.Helper()

	out := make([]feedStoryItem, 0, len(page.Items))
	for i, raw := range page.Items {
		if kind := feedKindOf(t, raw); kind != feedKindStory {
			continue
		}
		var story feedStoryItem
		if err := json.Unmarshal(raw, &story); err != nil {
			t.Fatalf("decode item %d as a feed story: %v", i, err)
		}
		out = append(out, story)
	}

	return out
}

// TestFeedAttachesLatestInteractionsInOneBatch is the brief's scenario: a mixed
// page carries the newest reaction and the newest comment for each entry, merged
// newest-first and named from the page's single author read (KNOT-028).
func TestFeedAttachesLatestInteractionsInOneBatch(t *testing.T) {
	handler, _, lookups := feedFixture(t)

	// The fixture's fake author lookup already knows the entry authors; the two
	// interaction actors ride in the same batched read.
	authors := lookups.authors.(*fakeAuthorService)
	authors.users[feedReactorID] = &identity.User{
		ID:          feedReactorID,
		DisplayName: feedReactorName,
		AvatarURL:   "avatars/tycoon.png",
	}
	authors.users[feedCommenterID] = &identity.User{
		ID:          feedCommenterID,
		DisplayName: feedCommenterName,
	}

	lookup := lookups.interactions.(*fakeInteractionsLookup)
	lookup.latest = map[string][]interactions.Interaction{
		feedVersionID: {
			// The comment is newer, so it leads the two.
			{
				Kind:      interactions.KindComment,
				ActorID:   feedCommenterID,
				CreatedAt: testNow.Add(-time.Minute),
				ID:        "comment",
			},
			{
				Kind:      interactions.KindReaction,
				ActorID:   feedReactorID,
				Detail:    string(reactions.KnowItDifferently),
				CreatedAt: testNow.Add(-2 * time.Minute),
				ID:        "reaction",
			},
		},
		feedStoryID: {
			{
				Kind:      interactions.KindReaction,
				ActorID:   feedReactorID,
				Detail:    string(reactions.RingsTrue),
				CreatedAt: testNow.Add(-30 * time.Second),
				ID:        "story-reaction",
			},
		},
	}

	recorder := doStoryRequest(handler, http.MethodGet, "/stories", "", "")
	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d (body %s)", recorder.Code, http.StatusOK, recorder.Body.String())
	}

	var page feedPage
	decodeBody(t, recorder, &page)

	// ONE batched read for the whole page, including both kinds of entry.
	if lookup.calls != 1 {
		t.Errorf("interaction lookup calls = %d, want exactly 1 for the page", lookup.calls)
	}
	if len(lookup.gotTargets) != 2 {
		t.Fatalf("targets = %d, want 2 (the story and the adaptation)", len(lookup.gotTargets))
	}
	for _, target := range lookup.gotTargets {
		switch target.Key {
		case feedStoryID:
			if target.ReactionEntityType != reactions.EntityStory || target.ReactionEntityID != feedStoryID {
				t.Errorf("story target = %+v, want the story's own reactions", target)
			}
			if target.CommentVersionID != feedRootVersionID {
				t.Errorf("story comment version = %s, want the root version %s", target.CommentVersionID, feedRootVersionID)
			}
		case feedVersionID:
			if target.ReactionEntityType != reactions.EntityVersion || target.ReactionEntityID != feedVersionID {
				t.Errorf("version target = %+v, want the adaptation's own reactions", target)
			}
			if target.CommentVersionID != feedVersionID {
				t.Errorf("version comment version = %s, want the version itself %s", target.CommentVersionID, feedVersionID)
			}
		default:
			t.Errorf("unexpected target key %q", target.Key)
		}
	}

	adaptations := feedVersions(t, page)
	if len(adaptations) != 1 {
		t.Fatalf("adaptations = %d, want 1", len(adaptations))
	}
	versionInteractions := adaptations[0].RecentInteractions
	if len(versionInteractions) != 2 {
		t.Fatalf("version interactions = %d, want 2", len(versionInteractions))
	}
	if versionInteractions[0].Type != string(interactions.KindComment) {
		t.Errorf("version interactions[0].Type = %q, want comment (the newer action)", versionInteractions[0].Type)
	}
	if versionInteractions[0].ActorDisplayName != feedCommenterName {
		t.Errorf("comment actor = %q, want %q", versionInteractions[0].ActorDisplayName, feedCommenterName)
	}
	if versionInteractions[0].ActorAvatarURL != nil {
		t.Errorf("comment actor avatar = %v, want null (the actor has none)", *versionInteractions[0].ActorAvatarURL)
	}
	if versionInteractions[0].Detail != nil {
		t.Errorf("comment detail = %q, want null", *versionInteractions[0].Detail)
	}
	if versionInteractions[1].Type != string(interactions.KindReaction) {
		t.Errorf("version interactions[1].Type = %q, want reaction", versionInteractions[1].Type)
	}
	if versionInteractions[1].Detail == nil || *versionInteractions[1].Detail != string(reactions.KnowItDifferently) {
		t.Errorf("reaction detail = %v, want %q", versionInteractions[1].Detail, reactions.KnowItDifferently)
	}
	if versionInteractions[1].ActorAvatarURL == nil || *versionInteractions[1].ActorAvatarURL != avatarPathFor(authors.users[feedReactorID]) {
		t.Errorf("reaction actor avatar = %v, want the actor's avatar path", versionInteractions[1].ActorAvatarURL)
	}

	storiesPage := feedStoryItems(t, page)
	if len(storiesPage) != 1 {
		t.Fatalf("story entries = %d, want 1", len(storiesPage))
	}
	storyInteractions := storiesPage[0].RecentInteractions
	if len(storyInteractions) != 1 {
		t.Fatalf("story interactions = %d, want 1", len(storyInteractions))
	}
	if storyInteractions[0].Type != string(interactions.KindReaction) {
		t.Errorf("story interaction type = %q, want reaction", storyInteractions[0].Type)
	}
	if storyInteractions[0].ActorDisplayName != feedReactorName {
		t.Errorf("story interaction actor = %q, want %q", storyInteractions[0].ActorDisplayName, feedReactorName)
	}

	// The interaction actors were named by the SAME batched read as the page's
	// own authors, not by a read per interaction.
	if authors.calls != 1 {
		t.Errorf("author lookup calls = %d, want exactly 1 for the page", authors.calls)
	}
	if !containsID(authors.gotIDs, feedReactorID) || !containsID(authors.gotIDs, feedCommenterID) {
		t.Errorf("author ids = %v, want both interaction actors in the batched read", authors.gotIDs)
	}
}

// TestFeedSwallowsInteractionLookupFailure proves social proof is enrichment: a
// failed read leaves the field empty (never null) and the page still serves
// (KNOT-028).
func TestFeedSwallowsInteractionLookupFailure(t *testing.T) {
	handler, _, lookups := feedFixture(t)
	lookups.interactions.(*fakeInteractionsLookup).err = errors.New("reactions unavailable")

	recorder := doStoryRequest(handler, http.MethodGet, "/stories", "", "")
	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d (enrichment failure must not fail the read)", recorder.Code, http.StatusOK)
	}

	var page feedPage
	decodeBody(t, recorder, &page)

	for _, story := range feedStoryItems(t, page) {
		if story.RecentInteractions == nil || len(story.RecentInteractions) != 0 {
			t.Errorf("story interactions = %v, want an empty array", story.RecentInteractions)
		}
	}
	for _, version := range feedVersions(t, page) {
		if version.RecentInteractions == nil || len(version.RecentInteractions) != 0 {
			t.Errorf("version interactions = %v, want an empty array", version.RecentInteractions)
		}
	}
}

// containsID reports whether ids holds want.
func containsID(ids []string, want string) bool {
	for _, id := range ids {
		if id == want {
			return true
		}
	}
	return false
}
