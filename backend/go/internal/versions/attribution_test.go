package versions

import (
	"context"
	"errors"
	"strings"
	"testing"
)

// Canonical UUID text for the attribution tests. They are deliberately distinct
// from the ids the service tests use, so a chain built here can never be
// confused with a canned fixture there.
const (
	attrStoryID = "77777777-7777-4777-8777-777777777777"
	attrRootID  = "88888888-8888-4888-8888-888888888881"
	attrMid1ID  = "88888888-8888-4888-8888-888888888882"
	attrMid2ID  = "88888888-8888-4888-8888-888888888883"
	attrLeafID  = "88888888-8888-4888-8888-888888888884"
)

// Author ids and names used by the chain fixtures.
const (
	attrRootAuthor = "11111111-1111-4111-8111-111111111111"
	attrMid1Author = "11111111-1111-4111-8111-111111111112"
	attrMid2Author = "11111111-1111-4111-8111-111111111113"
	attrLeafAuthor = "11111111-1111-4111-8111-111111111114"
	attrRootName   = "Nomsa Khan"
	attrMid1Name   = "Sipho Dlamini"
	attrMid2Name   = "Thandi Mokoena"
	attrLeafName   = "Ana Ruiz"
)

// chainStore is a VersionStore whose versions are keyed by id, so a walk can be
// exercised over a real parent chain.
type chainStore struct {
	byID map[string]StoryVersion
}

func newChainStore(rows ...StoryVersion) *chainStore {
	store := &chainStore{byID: make(map[string]StoryVersion, len(rows))}
	for _, row := range rows {
		store.byID[row.ID] = row
	}
	return store
}

func (c *chainStore) CreateVersion(_ context.Context, version StoryVersion) (StoryVersion, error) {
	return version, nil
}

func (c *chainStore) GetVersion(_ context.Context, id string) (StoryVersion, error) {
	row, ok := c.byID[id]
	if !ok {
		return StoryVersion{}, ErrNotFound
	}
	return row, nil
}

func (c *chainStore) ListByStory(_ context.Context, storyID string) ([]StoryVersion, error) {
	out := make([]StoryVersion, 0)
	for _, row := range c.byID {
		if row.StoryID == storyID {
			out = append(out, row)
		}
	}
	return out, nil
}

// callCountingStore wraps a chainStore and counts the reads a walk makes, so a
// test can prove the in-memory resolver never touches the store.
type callCountingStore struct {
	inner    *chainStore
	getCalls int
}

func (c *callCountingStore) CreateVersion(ctx context.Context, version StoryVersion) (StoryVersion, error) {
	return c.inner.CreateVersion(ctx, version)
}

func (c *callCountingStore) GetVersion(ctx context.Context, id string) (StoryVersion, error) {
	c.getCalls++
	return c.inner.GetVersion(ctx, id)
}

func (c *callCountingStore) ListByStory(ctx context.Context, storyID string) ([]StoryVersion, error) {
	return c.inner.ListByStory(ctx, storyID)
}

// rootRow is a story's root version: no parent, so it is the original.
func rootRow() StoryVersion {
	return StoryVersion{
		ID:       attrRootID,
		StoryID:  attrStoryID,
		AuthorID: attrRootAuthor,
		Language: "eng",
	}
}

// adaptationRow is one adaptation in a chain.
func adaptationRow(id, parentID, authorID, language string) StoryVersion {
	return StoryVersion{
		ID:              id,
		StoryID:         attrStoryID,
		ParentVersionID: parentID,
		AuthorID:        authorID,
		Language:        language,
	}
}

// namedChain builds a chain of the given links for the formatting tests.
func namedChain(links ...ChainLink) Chain {
	chain := make(Chain, 0, len(links))
	for i, link := range links {
		link.Depth = i
		chain = append(chain, link)
	}
	return chain
}

func TestFormatAttributionEmptyChainIsEmpty(t *testing.T) {
	if got := FormatAttribution(nil); got != "" {
		t.Errorf("FormatAttribution(nil) = %q, want an empty string", got)
	}
	if got := FormatAttribution(Chain{}); got != "" {
		t.Errorf("FormatAttribution(Chain{}) = %q, want an empty string", got)
	}
}

func TestFormatAttributionRootStory(t *testing.T) {
	chain := namedChain(
		ChainLink{AuthorID: attrRootAuthor, AuthorDisplayName: attrRootName, Language: "eng"},
	)

	want := "Original story by Nomsa Khan in English."
	if got := FormatAttribution(chain); got != want {
		t.Errorf("FormatAttribution(root) = %q, want %q", got, want)
	}
}

func TestFormatAttributionFirstAdaptation(t *testing.T) {
	chain := namedChain(
		ChainLink{AuthorID: attrRootAuthor, AuthorDisplayName: attrRootName, Language: "eng"},
		ChainLink{AuthorID: attrMid1Author, AuthorDisplayName: attrMid1Name, Language: "zul"},
	)

	want := "Adapted from Nomsa Khan's original story in English into Zulu."
	if got := FormatAttribution(chain); got != want {
		t.Errorf("FormatAttribution(first adaptation) = %q, want %q", got, want)
	}
}

func TestFormatAttributionAdaptationOfAdaptation(t *testing.T) {
	chain := namedChain(
		ChainLink{AuthorID: attrRootAuthor, AuthorDisplayName: attrRootName, Language: "eng"},
		ChainLink{AuthorID: attrMid1Author, AuthorDisplayName: attrMid1Name, Language: "zul"},
		ChainLink{AuthorID: attrMid2Author, AuthorDisplayName: attrMid2Name, Language: "ssw"},
	)

	want := "Adapted from Sipho Dlamini's Zulu version of Nomsa Khan's original story in English into Swati."
	if got := FormatAttribution(chain); got != want {
		t.Errorf("FormatAttribution(adaptation of adaptation) = %q, want %q", got, want)
	}
}

// The deep form names the immediate parent and the root, and counts the links in
// between, so a long lineage still reads as one sentence.
func TestFormatAttributionDeepChainCompresses(t *testing.T) {
	chain := namedChain(
		ChainLink{AuthorID: attrRootAuthor, AuthorDisplayName: attrRootName, Language: "eng"},
		ChainLink{AuthorID: attrMid1Author, AuthorDisplayName: attrMid1Name, Language: "zul"},
		ChainLink{AuthorID: attrMid2Author, AuthorDisplayName: attrMid2Name, Language: "xho"},
		ChainLink{AuthorID: attrLeafAuthor, AuthorDisplayName: attrLeafName, Language: "tso"},
	)

	want := "Adapted from Thandi Mokoena's Xhosa version, which traces back through 2 adaptations to Nomsa Khan's original story in English, into Tsonga."
	if got := FormatAttribution(chain); got != want {
		t.Errorf("FormatAttribution(deep chain) = %q, want %q", got, want)
	}
}

func TestFormatAttributionDeepChainCountsMiddleLinks(t *testing.T) {
	chain := namedChain(
		ChainLink{AuthorDisplayName: "A", Language: "eng"},
		ChainLink{AuthorDisplayName: "B", Language: "zul"},
		ChainLink{AuthorDisplayName: "C", Language: "xho"},
		ChainLink{AuthorDisplayName: "D", Language: "tso"},
		ChainLink{AuthorDisplayName: "E", Language: "nso"},
	)

	got := FormatAttribution(chain)
	if !strings.Contains(got, "traces back through 3 adaptations") {
		t.Errorf("FormatAttribution(5 links) = %q, want it to count 3 middle links", got)
	}
}

func TestFormatAttributionFallsBackWhenAuthorIsUnknown(t *testing.T) {
	chain := namedChain(ChainLink{AuthorID: attrRootAuthor, Language: "eng"})

	want := "Original story by another author in English."
	if got := FormatAttribution(chain); got != want {
		t.Errorf("FormatAttribution(missing name) = %q, want %q", got, want)
	}
}

func TestFormatAttributionFallsBackWhenLanguageIsUnknown(t *testing.T) {
	chain := namedChain(ChainLink{AuthorID: attrRootAuthor, AuthorDisplayName: attrRootName})

	want := "Original story by Nomsa Khan in their language."
	if got := FormatAttribution(chain); got != want {
		t.Errorf("FormatAttribution(missing language) = %q, want %q", got, want)
	}
}

func TestFormatAttributionFallsBackWhenLanguageCodeIsUnrecognised(t *testing.T) {
	chain := namedChain(
		ChainLink{AuthorDisplayName: attrRootName, Language: "eng"},
		ChainLink{AuthorDisplayName: attrMid1Name, Language: "zzz"},
	)

	want := "Adapted from Nomsa Khan's original story in English into their language."
	if got := FormatAttribution(chain); got != want {
		t.Errorf("FormatAttribution(unrecognised code) = %q, want %q", got, want)
	}
}

func TestBuildChainForRootVersion(t *testing.T) {
	store := newChainStore(rootRow())

	chain, err := BuildChain(context.Background(), store, attrRootID)
	if err != nil {
		t.Fatalf("BuildChain() error = %v, want nil", err)
	}

	if len(chain) != 1 {
		t.Fatalf("len(chain) = %d, want 1", len(chain))
	}
	if chain[0].VersionID != attrRootID || chain[0].AuthorID != attrRootAuthor {
		t.Errorf("chain[0] = %+v, want the root version", chain[0])
	}
	if chain[0].Depth != 0 || chain.Depth() != 0 {
		t.Errorf("depth = %d / %d, want 0 / 0", chain[0].Depth, chain.Depth())
	}
}

func TestBuildChainForFirstAdaptation(t *testing.T) {
	store := newChainStore(
		rootRow(),
		adaptationRow(attrMid1ID, attrRootID, attrMid1Author, "zul"),
	)

	chain, err := BuildChain(context.Background(), store, attrMid1ID)
	if err != nil {
		t.Fatalf("BuildChain() error = %v, want nil", err)
	}

	if len(chain) != 2 {
		t.Fatalf("len(chain) = %d, want 2", len(chain))
	}
	if chain[0].VersionID != attrRootID {
		t.Errorf("chain[0].VersionID = %q, want the root %q", chain[0].VersionID, attrRootID)
	}
	if chain[1].VersionID != attrMid1ID {
		t.Errorf("chain[1].VersionID = %q, want the adaptation %q", chain[1].VersionID, attrMid1ID)
	}
	if chain.Depth() != 1 {
		t.Errorf("depth = %d, want 1", chain.Depth())
	}
}

func TestBuildChainForAdaptationOfAdaptation(t *testing.T) {
	store := newChainStore(
		rootRow(),
		adaptationRow(attrMid1ID, attrRootID, attrMid1Author, "zul"),
		adaptationRow(attrMid2ID, attrMid1ID, attrMid2Author, "ssw"),
	)

	chain, err := BuildChain(context.Background(), store, attrMid2ID)
	if err != nil {
		t.Fatalf("BuildChain() error = %v, want nil", err)
	}

	wantIDs := []string{attrRootID, attrMid1ID, attrMid2ID}
	if len(chain) != len(wantIDs) {
		t.Fatalf("len(chain) = %d, want %d", len(chain), len(wantIDs))
	}
	for i, want := range wantIDs {
		if chain[i].VersionID != want {
			t.Errorf("chain[%d].VersionID = %q, want %q", i, chain[i].VersionID, want)
		}
		if chain[i].Depth != i {
			t.Errorf("chain[%d].Depth = %d, want %d", i, chain[i].Depth, i)
		}
	}
	if chain.Depth() != 2 {
		t.Errorf("depth = %d, want 2", chain.Depth())
	}
}

func TestBuildChainMissingVersionIsNotFound(t *testing.T) {
	store := newChainStore(rootRow())

	_, err := BuildChain(context.Background(), store, attrLeafID)
	if !errors.Is(err, ErrNotFound) {
		t.Errorf("BuildChain() error = %v, want ErrNotFound", err)
	}
}

func TestBuildChainMalformedIDIsNotFound(t *testing.T) {
	store := &callCountingStore{inner: newChainStore(rootRow())}

	_, err := BuildChain(context.Background(), store, "not-a-uuid")
	if !errors.Is(err, ErrNotFound) {
		t.Errorf("BuildChain() error = %v, want ErrNotFound", err)
	}
	if store.getCalls != 0 {
		t.Errorf("store received %d reads, want 0 — a malformed id cannot name a version", store.getCalls)
	}
}

// A malformed tree must be refused rather than walked forever.
func TestBuildChainRejectsCycle(t *testing.T) {
	store := newChainStore(
		rootRow(),
		adaptationRow(attrMid1ID, attrMid2ID, attrMid1Author, "zul"),
		adaptationRow(attrMid2ID, attrMid1ID, attrMid2Author, "ssw"),
	)

	_, err := BuildChain(context.Background(), store, attrMid1ID)
	if !errors.Is(err, ErrChainCycle) {
		t.Errorf("BuildChain() error = %v, want ErrChainCycle", err)
	}
}

func TestBuildChainRejectsNilStore(t *testing.T) {
	if _, err := BuildChain(context.Background(), nil, attrRootID); err == nil {
		t.Error("BuildChain(nil store) error = nil, want an error")
	}
}

func TestBuildChainFromVersionsResolvesInMemory(t *testing.T) {
	rows := []StoryVersion{
		rootRow(),
		adaptationRow(attrMid1ID, attrRootID, attrMid1Author, "zul"),
		adaptationRow(attrMid2ID, attrMid1ID, attrMid2Author, "ssw"),
	}

	chain, err := BuildChainFromVersions(attrMid2ID, rows)
	if err != nil {
		t.Fatalf("BuildChainFromVersions() error = %v, want nil", err)
	}

	if chain.Depth() != 2 {
		t.Errorf("depth = %d, want 2", chain.Depth())
	}
	if chain[0].VersionID != attrRootID || chain[2].VersionID != attrMid2ID {
		t.Errorf("chain = %+v, want root first and the target last", chain)
	}
}

func TestBuildChainFromVersionsMissingTargetIsNotFound(t *testing.T) {
	rows := []StoryVersion{rootRow()}

	_, err := BuildChainFromVersions(attrMid1ID, rows)
	if !errors.Is(err, ErrNotFound) {
		t.Errorf("BuildChainFromVersions() error = %v, want ErrNotFound", err)
	}
}

func TestBuildChainFromVersionsRejectsCycle(t *testing.T) {
	rows := []StoryVersion{
		rootRow(),
		adaptationRow(attrMid1ID, attrMid2ID, attrMid1Author, "zul"),
		adaptationRow(attrMid2ID, attrMid1ID, attrMid2Author, "ssw"),
	}

	_, err := BuildChainFromVersions(attrMid1ID, rows)
	if !errors.Is(err, ErrChainCycle) {
		t.Errorf("BuildChainFromVersions() error = %v, want ErrChainCycle", err)
	}
}

// The tree endpoint's whole point: a page of versions costs no store reads.
func TestBuildChainFromVersionsNeverReadsTheStore(t *testing.T) {
	store := &callCountingStore{inner: newChainStore(
		rootRow(),
		adaptationRow(attrMid1ID, attrRootID, attrMid1Author, "zul"),
		adaptationRow(attrMid2ID, attrMid1ID, attrMid2Author, "ssw"),
	)}

	rows, err := store.ListByStory(context.Background(), attrStoryID)
	if err != nil {
		t.Fatalf("ListByStory() error = %v, want nil", err)
	}
	readsAfterList := store.getCalls

	for _, row := range rows {
		if _, err := BuildChainFromVersions(row.ID, rows); err != nil {
			t.Fatalf("BuildChainFromVersions(%s) error = %v, want nil", row.ID, err)
		}
	}

	if store.getCalls != readsAfterList {
		t.Errorf("store read count went from %d to %d, want no reads while resolving chains", readsAfterList, store.getCalls)
	}
}

// The service exposes the resolver so a handler depends on one interface.
func TestServiceBuildChain(t *testing.T) {
	store := newChainStore(
		rootRow(),
		adaptationRow(attrMid1ID, attrRootID, attrMid1Author, "zul"),
	)
	service, err := NewService(store, &fakeNotifier{})
	if err != nil {
		t.Fatalf("NewService() error = %v, want nil", err)
	}

	chain, err := service.BuildChain(context.Background(), attrMid1ID)
	if err != nil {
		t.Fatalf("BuildChain() error = %v, want nil", err)
	}
	if chain.Depth() != 1 {
		t.Errorf("depth = %d, want 1", chain.Depth())
	}
	if chain[0].Language != "eng" || chain[1].Language != "zul" {
		t.Errorf("languages = %q, %q, want eng then zul", chain[0].Language, chain[1].Language)
	}
}

func TestChainAccessorsOnEmptyChain(t *testing.T) {
	var empty Chain

	if _, ok := empty.Root(); ok {
		t.Error("Root() on an empty chain reported ok, want false")
	}
	if _, ok := empty.Target(); ok {
		t.Error("Target() on an empty chain reported ok, want false")
	}
	if depth := empty.Depth(); depth != 0 {
		t.Errorf("Depth() on an empty chain = %d, want 0", depth)
	}
}

func TestChainRootAndTargetOnAPopulatedChain(t *testing.T) {
	chain := namedChain(
		ChainLink{VersionID: attrRootID},
		ChainLink{VersionID: attrMid1ID},
	)

	root, ok := chain.Root()
	if !ok || root.VersionID != attrRootID {
		t.Errorf("Root() = (%+v, %v), want the first link", root, ok)
	}
	target, ok := chain.Target()
	if !ok || target.VersionID != attrMid1ID {
		t.Errorf("Target() = (%+v, %v), want the last link", target, ok)
	}
}
