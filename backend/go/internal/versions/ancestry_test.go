package versions

import (
	"context"
	"errors"
	"testing"
)

// TestPostgresStoreListAncestryReturnsTheClosure pins the batched walk the feed
// relies on (KNOT-022): one read returns every version a page of versions needs to
// be attributed -- each requested version, its parent, and so on to the root --
// with shared ancestors appearing once.
//
// It is the property that makes attributing a feed page one query rather than one
// walk per entry.
func TestPostgresStoreListAncestryReturnsTheClosure(t *testing.T) {
	env := integrationSetup(t)
	ctx := context.Background()

	story := newIntegrationStory(t, env, "ancestry")
	root := story.RootVersionID

	adapterB := newIntegrationUser(t, env, "ancestry-b")
	adapterC := newIntegrationUser(t, env, "ancestry-c")

	mid, err := env.store.CreateVersion(ctx, StoryVersion{
		StoryID:         story.ID,
		ParentVersionID: root,
		AuthorID:        adapterB,
		Language:        "zul",
		Title:           "Ukuhumusha",
		Body:            "Umzimba wokuhumusha.",
	})
	if err != nil {
		t.Fatalf("CreateVersion(mid) error = %v, want nil", err)
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
		t.Fatalf("CreateVersion(leaf) error = %v, want nil", err)
	}

	// Both seeds in one call. The closure is the root, the middle, and the leaf --
	// three rows for two seeds, because they share their ancestors.
	rows, err := env.store.ListAncestry(ctx, []string{mid.ID, leaf.ID})
	if err != nil {
		t.Fatalf("ListAncestry() error = %v, want nil", err)
	}

	got := make(map[string]StoryVersion, len(rows))
	for _, row := range rows {
		if _, duplicate := got[row.ID]; duplicate {
			t.Errorf("version %s appeared twice, want a shared ancestor returned once", row.ID)
		}
		got[row.ID] = row
	}

	if len(rows) != 3 {
		t.Fatalf("closure has %d rows, want 3 (root, mid, leaf)", len(rows))
	}
	for _, want := range []string{root, mid.ID, leaf.ID} {
		if _, ok := got[want]; !ok {
			t.Errorf("closure is missing %s", want)
		}
	}
	if got[mid.ID].ParentVersionID != root {
		t.Errorf("mid parent = %q, want the root %q", got[mid.ID].ParentVersionID, root)
	}
	if got[leaf.ID].ParentVersionID != mid.ID {
		t.Errorf("leaf parent = %q, want the middle version %q", got[leaf.ID].ParentVersionID, mid.ID)
	}
	if got[root].ParentVersionID != "" {
		t.Errorf("root parent = %q, want an empty parent id for a root", got[root].ParentVersionID)
	}

	// The rows are enough to rebuild each seed's chain in memory, which is exactly
	// what the feed does with them.
	for _, want := range []struct {
		seed  string
		depth int
	}{{mid.ID, 1}, {leaf.ID, 2}} {
		chain, err := BuildChainFromVersions(want.seed, rows)
		if err != nil {
			t.Fatalf("BuildChainFromVersions(%s) error = %v, want nil", want.seed, err)
		}
		if chain.Depth() != want.depth {
			t.Errorf("depth from the closure for %s = %d, want %d", want.seed, chain.Depth(), want.depth)
		}
	}

	// A single seed needs only its own lineage.
	one, err := env.store.ListAncestry(ctx, []string{mid.ID})
	if err != nil {
		t.Fatalf("ListAncestry(one seed) error = %v, want nil", err)
	}
	if len(one) != 2 {
		t.Errorf("closure for one adaptation = %d rows, want 2 (root and adaptation)", len(one))
	}
}

// TestPostgresStoreListAncestryWithNoSeedsQueriesNothing pins that an empty seed
// list is not an error: the feed calls it only when a page carries adaptations,
// and an id-less `IN ()` is not valid SQL.
func TestPostgresStoreListAncestryWithNoSeedsQueriesNothing(t *testing.T) {
	env := integrationSetup(t)

	rows, err := env.store.ListAncestry(context.Background(), nil)
	if err != nil {
		t.Fatalf("ListAncestry(nil) error = %v, want nil", err)
	}
	if len(rows) != 0 {
		t.Errorf("rows = %d, want none", len(rows))
	}
}

// TestPostgresStoreListAncestryIgnoresUnknownSeeds pins that a seed nobody has
// heard of contributes nothing rather than failing the read: a version deleted
// mid-request must not break a page of other people's content.
func TestPostgresStoreListAncestryIgnoresUnknownSeeds(t *testing.T) {
	env := integrationSetup(t)
	ctx := context.Background()

	story := newIntegrationStory(t, env, "ancestry-unknown")

	rows, err := env.store.ListAncestry(ctx, []string{"00000000-0000-4000-8000-000000000000", story.RootVersionID})
	if err != nil {
		t.Fatalf("ListAncestry() error = %v, want nil", err)
	}
	if len(rows) != 1 || rows[0].ID != story.RootVersionID {
		t.Fatalf("closure = %+v, want only the seed that exists", rows)
	}
}

// TestChainsForResolvesAPageInOneRead pins the batching contract of the resolver
// the feed uses: one store read serves every version on the page, the ids are
// passed through unchanged, and each chain is ordered root-first.
func TestChainsForResolvesAPageInOneRead(t *testing.T) {
	service, store := newTestService(t)
	store.ancestryRows = []StoryVersion{
		rootRow(),
		adaptationRow(attrMid1ID, attrRootID, attrMid1Author, "zul"),
		adaptationRow(attrMid2ID, attrMid1ID, attrMid2Author, "ssw"),
	}

	chains, err := service.ChainsFor(context.Background(), []string{attrMid1ID, attrMid2ID})
	if err != nil {
		t.Fatalf("ChainsFor() error = %v, want nil", err)
	}

	if store.ancestryCalls != 1 {
		t.Errorf("ancestry reads = %d, want exactly 1 for the whole page", store.ancestryCalls)
	}
	if len(store.gotAncestryIDs) != 2 || store.gotAncestryIDs[0] != attrMid1ID || store.gotAncestryIDs[1] != attrMid2ID {
		t.Errorf("requested ids = %v, want both versions in order", store.gotAncestryIDs)
	}

	if len(chains) != 2 {
		t.Fatalf("chains = %d, want one per version", len(chains))
	}
	if got := chains[attrMid1ID].Depth(); got != 1 {
		t.Errorf("depth of the first adaptation = %d, want 1", got)
	}
	if got := chains[attrMid2ID].Depth(); got != 2 {
		t.Errorf("depth of the second adaptation = %d, want 2", got)
	}

	target, ok := chains[attrMid2ID].Target()
	if !ok || target.VersionID != attrMid2ID {
		t.Errorf("chain target = %+v (ok = %v), want the requested version %q", target, ok, attrMid2ID)
	}
}

// TestChainsForSkipsUnresolvableVersions pins that attribution is enrichment: a
// lineage that cannot be built (a cycle, or rows that vanished mid-request) leaves
// that entry out of the map instead of failing the whole page.
func TestChainsForSkipsUnresolvableVersions(t *testing.T) {
	service, store := newTestService(t)

	// The store returns the requested version but not its parent, so no chain can
	// be built from the rows it does have.
	store.ancestryRows = []StoryVersion{
		adaptationRow(attrMid1ID, attrRootID, attrMid1Author, "zul"),
	}

	chains, err := service.ChainsFor(context.Background(), []string{attrMid1ID})
	if err != nil {
		t.Fatalf("ChainsFor() error = %v, want nil", err)
	}
	if len(chains) != 0 {
		t.Errorf("chains = %+v, want none when the lineage cannot be resolved", chains)
	}
}

// TestChainsForEmptyInputQueriesNothing pins that a page with no adaptations costs
// no read at all.
func TestChainsForEmptyInputQueriesNothing(t *testing.T) {
	service, store := newTestService(t)

	chains, err := service.ChainsFor(context.Background(), nil)
	if err != nil {
		t.Fatalf("ChainsFor(nil) error = %v, want nil", err)
	}
	if len(chains) != 0 {
		t.Errorf("chains = %d, want none", len(chains))
	}
	if store.ancestryCalls != 0 {
		t.Errorf("ancestry reads = %d, want 0", store.ancestryCalls)
	}
}

// TestChainsForPropagatesStoreFailure pins that a failing read is reported, not
// swallowed: the caller decides whether an unattributed page is acceptable, and
// the HTTP layer chooses to log and continue.
func TestChainsForPropagatesStoreFailure(t *testing.T) {
	service, store := newTestService(t)
	store.ancestryErr = errors.New("connection reset")

	chains, err := service.ChainsFor(context.Background(), []string{attrMid1ID})
	if err == nil {
		t.Fatal("ChainsFor() error = nil, want the store error")
	}
	if chains != nil {
		t.Errorf("chains = %+v, want nil alongside the error", chains)
	}
}
