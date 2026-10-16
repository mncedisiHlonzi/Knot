package versions

import (
	"context"
	"testing"
)

// newIntegrationUser inserts an extra account for a lineage test, so an
// adaptation can be authored by somebody other than the story's original author.
// It uses the environment's email prefix, so the teardown that deletes every
// prefixed account removes this one too.
func newIntegrationUser(t *testing.T, env integrationEnv, name string) string {
	t.Helper()

	var id string
	err := env.pool.QueryRow(
		context.Background(),
		`INSERT INTO users (email, password_hash, display_name, preferred_languages)
		 VALUES ($1, $2, $3, $4)
		 RETURNING id`,
		env.prefix+name+"@example.test",
		"$argon2id$v=19$m=65536,t=1,p=4$c2FsdA$a2V5",
		"Integration "+name,
		[]string{"eng"},
	).Scan(&id)
	if err != nil {
		t.Fatalf("could not create the integration user %q: %v", name, err)
	}

	return id
}

// TestPostgresStoreBuildChainFollowsAPersistedLineage pins the resolver against
// real rows: a story, an adaptation of it, and an adaptation of that adaptation
// produce chains of depth 0, 1 and 2, each ordered root-first and carrying the
// right author and language at every hop (KNOT-021).
func TestPostgresStoreBuildChainFollowsAPersistedLineage(t *testing.T) {
	env := integrationSetup(t)
	ctx := context.Background()

	story := newIntegrationStory(t, env, "attribution")
	root := story.RootVersionID

	// Two adapters, so every hop of the chain has a distinct author to assert.
	adapterB := newIntegrationUser(t, env, "adapter-b")
	adapterC := newIntegrationUser(t, env, "adapter-c")

	mid, err := env.store.CreateVersion(ctx, StoryVersion{
		StoryID:         story.ID,
		ParentVersionID: root,
		AuthorID:        adapterB,
		Language:        "zul",
		Title:           "Ukuhumusha",
		Body:            "Umzimba wokuhumusha.",
	})
	if err != nil {
		t.Fatalf("CreateVersion(adaptation) error = %v, want nil", err)
	}

	leaf, err := env.store.CreateVersion(ctx, StoryVersion{
		StoryID:         story.ID,
		ParentVersionID: mid.ID,
		AuthorID:        adapterC,
		Language:        "ssw",
		Title:           "Kuhumusha",
		Body:            "Ummongo wekuhumusha.",
	})
	if err != nil {
		t.Fatalf("CreateVersion(adaptation of adaptation) error = %v, want nil", err)
	}

	// The root: one link, depth 0, authored by the story's original author.
	rootChain, err := BuildChain(ctx, env.store, root)
	if err != nil {
		t.Fatalf("BuildChain(root) error = %v, want nil", err)
	}
	if len(rootChain) != 1 {
		t.Fatalf("len(root chain) = %d, want 1", len(rootChain))
	}
	if rootChain.Depth() != 0 {
		t.Errorf("root depth = %d, want 0", rootChain.Depth())
	}
	if rootChain[0].AuthorID != env.author || rootChain[0].Language != "eng" {
		t.Errorf("root link = %+v, want the story's author in eng", rootChain[0])
	}

	// The first adaptation: depth 1, root first.
	midChain, err := BuildChain(ctx, env.store, mid.ID)
	if err != nil {
		t.Fatalf("BuildChain(adaptation) error = %v, want nil", err)
	}
	if midChain.Depth() != 1 {
		t.Errorf("adaptation depth = %d, want 1", midChain.Depth())
	}
	assertChainShape(t, midChain, []chainHop{
		{versionID: root, authorID: env.author, language: "eng"},
		{versionID: mid.ID, authorID: adapterB, language: "zul"},
	})

	// The adaptation of the adaptation: depth 2, and the whole lineage present.
	leafChain, err := BuildChain(ctx, env.store, leaf.ID)
	if err != nil {
		t.Fatalf("BuildChain(adaptation of adaptation) error = %v, want nil", err)
	}
	if leafChain.Depth() != 2 {
		t.Errorf("deep depth = %d, want 2", leafChain.Depth())
	}
	assertChainShape(t, leafChain, []chainHop{
		{versionID: root, authorID: env.author, language: "eng"},
		{versionID: mid.ID, authorID: adapterB, language: "zul"},
		{versionID: leaf.ID, authorID: adapterC, language: "ssw"},
	})

	// The in-memory resolver -- what the tree endpoint uses -- must agree with
	// the store walk for every version on the page.
	rows, err := env.store.ListByStory(ctx, story.ID)
	if err != nil {
		t.Fatalf("ListByStory() error = %v, want nil", err)
	}
	for _, want := range []Chain{rootChain, midChain, leafChain} {
		target, ok := want.Target()
		if !ok {
			t.Fatal("fixture chain has no target")
		}

		got, err := BuildChainFromVersions(target.VersionID, rows)
		if err != nil {
			t.Fatalf("BuildChainFromVersions(%s) error = %v, want nil", target.VersionID, err)
		}
		if len(got) != len(want) {
			t.Fatalf("len(in-memory chain for %s) = %d, want %d", target.VersionID, len(got), len(want))
		}
		for i := range want {
			if got[i].VersionID != want[i].VersionID || got[i].AuthorID != want[i].AuthorID || got[i].Language != want[i].Language {
				t.Errorf("in-memory chain for %s at depth %d = %+v, want %+v", target.VersionID, i, got[i], want[i])
			}
		}
	}
}

// chainHop is one expected link of a fixture chain.
type chainHop struct {
	versionID string
	authorID  string
	language  string
}

// assertChainShape checks a chain hop by hop, including that each link's depth is
// its position.
func assertChainShape(t *testing.T, chain Chain, want []chainHop) {
	t.Helper()

	if len(chain) != len(want) {
		t.Fatalf("len(chain) = %d, want %d", len(chain), len(want))
	}

	for i, hop := range want {
		if chain[i].VersionID != hop.versionID {
			t.Errorf("chain[%d].VersionID = %q, want %q", i, chain[i].VersionID, hop.versionID)
		}
		if chain[i].AuthorID != hop.authorID {
			t.Errorf("chain[%d].AuthorID = %q, want %q", i, chain[i].AuthorID, hop.authorID)
		}
		if chain[i].Language != hop.language {
			t.Errorf("chain[%d].Language = %q, want %q", i, chain[i].Language, hop.language)
		}
		if chain[i].Depth != i {
			t.Errorf("chain[%d].Depth = %d, want %d", i, chain[i].Depth, i)
		}
	}
}
