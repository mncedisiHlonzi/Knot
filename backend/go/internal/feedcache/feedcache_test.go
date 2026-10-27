package feedcache

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/alicebob/miniredis/v2"

	"github.com/knot/backend/internal/cache"
)

// The clock every fixture is stamped with. Fixed so an assertion about scores is
// readable and so nothing depends on the wall clock.
var testNow = time.Date(2026, 10, 27, 12, 0, 0, 0, time.UTC)

// Canonical UUID text, because the real callers only ever pass ids PostgreSQL
// generated.
const (
	authorID   = "11111111-1111-4111-8111-111111111111"
	readerA    = "22222222-2222-4222-8222-222222222222"
	readerB    = "33333333-3333-4333-8333-333333333333"
	outsiderID = "44444444-4444-4444-8444-444444444444"
)

// fakeAudience returns a canned target list, so a fan-out test asserts the rules
// the feed applies to an audience rather than how SQL found it.
type fakeAudience struct {
	targets []string
	err     error

	calls int
	got   AudienceQuery
}

func (f *fakeAudience) TargetUserIDs(_ context.Context, q AudienceQuery) ([]string, error) {
	f.calls++
	f.got = q
	if f.err != nil {
		return nil, f.err
	}
	return f.targets, nil
}

// newTestFeed returns a feed over an in-process Redis, so the round trip through
// ZADD/ZREVRANGE/ZREMRANGEBYRANK is exercised for real without Docker.
func newTestFeed(t *testing.T, opts Options, audience *fakeAudience) (*Feed, *miniredis.Miniredis) {
	t.Helper()

	server := miniredis.RunT(t)
	redisCache, err := cache.NewRedisCache(server.Addr(), "", 0)
	if err != nil {
		t.Fatalf("NewRedisCache() error = %v, want nil", err)
	}
	t.Cleanup(func() { _ = redisCache.Close() })

	sets, err := cache.NewSortedSet(redisCache)
	if err != nil {
		t.Fatalf("NewSortedSet() error = %v, want nil", err)
	}

	if audience == nil {
		audience = &fakeAudience{}
	}

	feed, err := New(sets, audience, opts)
	if err != nil {
		t.Fatalf("New() error = %v, want nil", err)
	}

	return feed, server
}

// defaultOptions is the shape a deployment gets out of the box.
func defaultOptions() Options {
	return Options{Cap: FeedCap, FanoutThreshold: FanoutThreshold}
}

// readItems is a terse Read that fails the test on error.
func readItems(t *testing.T, feed *Feed, userID string, limit int) []Item {
	t.Helper()

	items, err := feed.Read(context.Background(), userID, limit)
	if err != nil {
		t.Fatalf("Read(%s) error = %v, want nil", userID, err)
	}
	return items
}

// readIDs is readItems projected onto the ids, for the assertions that only care
// about which entries came back.
func readIDs(t *testing.T, feed *Feed, userID string, limit int) []string {
	t.Helper()

	items := readItems(t, feed, userID, limit)
	ids := make([]string, 0, len(items))
	for _, item := range items {
		ids = append(ids, item.ID)
	}
	return ids
}

func TestFanoutStoryWritesEachRecipientsFeed(t *testing.T) {
	audience := &fakeAudience{targets: []string{readerA, readerB}}
	feed, server := newTestFeed(t, defaultOptions(), audience)

	const storyID = "aaaaaaaa-aaaa-4aaa-8aaa-aaaaaaaaaaaa"

	result, err := feed.FanoutStory(context.Background(), storyID, authorID, "eng", testNow)
	if err != nil {
		t.Fatalf("FanoutStory() error = %v, want nil", err)
	}
	if result.Recipients != 2 {
		t.Errorf("Recipients = %d, want 2", result.Recipients)
	}
	if result.HighFanout {
		t.Error("HighFanout = true, want false for an audience below the threshold")
	}

	// The audience question itself: the author's id, the story's language, and one
	// extra id so an over-large audience can be detected without being counted.
	if audience.got.AuthorID != authorID || audience.got.Language != "eng" {
		t.Errorf("audience query = %+v, want author %s in eng", audience.got, authorID)
	}
	if want := FanoutThreshold + 1; audience.got.Limit != want {
		t.Errorf("audience limit = %d, want %d", audience.got.Limit, want)
	}

	// The round trip: written by ZADD, read back by ZREVRANGE, with created_at as
	// the score.
	for _, userID := range []string{readerA, readerB} {
		ids := readIDs(t, feed, userID, 10)
		if len(ids) != 1 || ids[0] != storyID {
			t.Fatalf("feed %s = %v, want [%s]", userID, ids, storyID)
		}

		score, err := server.ZScore(FeedKey(userID), storyID)
		if err != nil {
			t.Fatalf("ZScore(%s) error = %v, want nil", userID, err)
		}
		if want := float64(testNow.Unix()); score != want {
			t.Errorf("score in %s = %v, want %v (created_at in unix seconds)", userID, score, want)
		}
	}

	// A reader who was not in the audience gets nothing.
	if ids := readIDs(t, feed, outsiderID, 10); len(ids) != 0 {
		t.Errorf("outsider feed = %v, want empty", ids)
	}
}

func TestFanoutStoryExcludesTheAuthor(t *testing.T) {
	audience := &fakeAudience{targets: []string{authorID, readerA}}
	feed, _ := newTestFeed(t, defaultOptions(), audience)

	const storyID = "bbbbbbbb-bbbb-4bbb-8bbb-bbbbbbbbbbbb"

	if _, err := feed.FanoutStory(context.Background(), storyID, authorID, "zul", testNow); err != nil {
		t.Fatalf("FanoutStory() error = %v, want nil", err)
	}

	if ids := readIDs(t, feed, authorID, 10); len(ids) != 0 {
		t.Errorf("author feed = %v, want empty (an author is not told about their own story)", ids)
	}
	if ids := readIDs(t, feed, readerA, 10); len(ids) != 1 {
		t.Errorf("reader feed = %v, want the story", ids)
	}
}

func TestFanoutStoryTrimsToCap(t *testing.T) {
	const cap = 3

	audience := &fakeAudience{targets: []string{readerA}}
	feed, server := newTestFeed(t, Options{Cap: cap, FanoutThreshold: FanoutThreshold}, audience)

	// Six stories, one second apart, so the newest is unambiguous.
	created := make([]time.Time, 0, 6)
	for i := 0; i < 6; i++ {
		created = append(created, testNow.Add(time.Duration(i)*time.Second))
	}

	for i, at := range created {
		storyID := storyIDFor(i)
		if _, err := feed.FanoutStory(context.Background(), storyID, authorID, "eng", at); err != nil {
			t.Fatalf("FanoutStory(%d) error = %v, want nil", i, err)
		}
	}

	if got := zcard(t, server, FeedKey(readerA)); got != cap {
		t.Fatalf("feed cardinality = %d, want %d (trimmed on every insert)", got, cap)
	}

	// The survivors are the newest `cap`, newest first.
	ids := readIDs(t, feed, readerA, 10)
	want := []string{storyIDFor(5), storyIDFor(4), storyIDFor(3)}
	if len(ids) != len(want) {
		t.Fatalf("feed = %v, want %v", ids, want)
	}
	for i := range want {
		if ids[i] != want[i] {
			t.Fatalf("feed = %v, want %v", ids, want)
		}
	}
}

func TestFanoutStoryHighFanoutStoresTheAuthorFeed(t *testing.T) {
	// A threshold of two, and three readers: the audience is over the limit.
	audience := &fakeAudience{targets: []string{readerA, readerB, outsiderID}}
	feed, server := newTestFeed(t, Options{Cap: FeedCap, FanoutThreshold: 2}, audience)

	const storyID = "cccccccc-cccc-4ccc-8ccc-cccccccccccc"

	result, err := feed.FanoutStory(context.Background(), storyID, authorID, "eng", testNow)
	if err != nil {
		t.Fatalf("FanoutStory() error = %v, want nil", err)
	}
	if !result.HighFanout {
		t.Error("HighFanout = false, want true when the audience exceeds the threshold")
	}
	if result.Recipients != 0 {
		t.Errorf("Recipients = %d, want 0 (no per-reader write)", result.Recipients)
	}

	// One write, to the author's own feed.
	if got := zcard(t, server, AuthorFeedKey(authorID)); got != 1 {
		t.Errorf("author feed cardinality = %d, want 1", got)
	}
	for _, userID := range []string{readerA, readerB, outsiderID} {
		if got := zcard(t, server, FeedKey(userID)); got != 0 {
			t.Errorf("feed %s cardinality = %d, want 0", userID, got)
		}
	}

	// The read path pulls it: a reader who asks for this author's feed gets the
	// story, and the plain Read does not (nothing was fanned out).
	merged, err := feed.ReadMerged(context.Background(), readerA, 10, []string{authorID})
	if err != nil {
		t.Fatalf("ReadMerged() error = %v, want nil", err)
	}
	if len(merged) != 1 || merged[0].ID != storyID || merged[0].Kind != KindStory {
		t.Errorf("merged feed = %v, want one story entry %s", merged, storyID)
	}
	if ids := readIDs(t, feed, readerA, 10); len(ids) != 0 {
		t.Errorf("unmerged feed = %v, want empty", ids)
	}
}

func TestFanoutStoryAudienceFailureIsReturned(t *testing.T) {
	wantErr := errors.New("rooted signals unavailable")
	audience := &fakeAudience{err: wantErr}
	feed, _ := newTestFeed(t, defaultOptions(), audience)

	_, err := feed.FanoutStory(context.Background(), "dddddddd-dddd-4ddd-8ddd-dddddddddddd", authorID, "eng", testNow)
	if !errors.Is(err, wantErr) {
		t.Fatalf("FanoutStory() error = %v, want it to wrap %v", err, wantErr)
	}
}

func TestFanoutStoryRejectsMissingIDs(t *testing.T) {
	feed, _ := newTestFeed(t, defaultOptions(), nil)

	if _, err := feed.FanoutStory(context.Background(), "", authorID, "eng", testNow); err == nil {
		t.Error("FanoutStory() with no story id error = nil, want an error")
	}
	if _, err := feed.FanoutStory(context.Background(), "eeeeeeee-eeee-4eee-8eee-eeeeeeeeeeee", "", "eng", testNow); err == nil {
		t.Error("FanoutStory() with no author id error = nil, want an error")
	}
}

func TestSeedFillsAnEmptyFeed(t *testing.T) {
	feed, _ := newTestFeed(t, defaultOptions(), nil)
	ctx := context.Background()

	entries := []Entry{
		{ID: storyIDFor(1), CreatedAt: testNow},
		{ID: storyIDFor(2), CreatedAt: testNow.Add(time.Minute)},
		// A duplicate is harmless: ZADD replaces the score.
		{ID: storyIDFor(1), CreatedAt: testNow},
	}

	if err := feed.Seed(ctx, readerA, entries); err != nil {
		t.Fatalf("Seed() error = %v, want nil", err)
	}

	ids := readIDs(t, feed, readerA, 10)
	want := []string{storyIDFor(2), storyIDFor(1)}
	if len(ids) != len(want) {
		t.Fatalf("seeded feed = %v, want %v", ids, want)
	}
	for i := range want {
		if ids[i] != want[i] {
			t.Fatalf("seeded feed = %v, want %v", ids, want)
		}
	}

	// Seeding nobody, or with nothing, is a no-op.
	if err := feed.Seed(ctx, "", entries); err != nil {
		t.Errorf("Seed(\"\") error = %v, want nil", err)
	}
	if err := feed.Seed(ctx, readerB, nil); err != nil {
		t.Errorf("Seed(nil) error = %v, want nil", err)
	}
}

func TestReadMergedOrdersBothSourcesAndCollapsesDuplicates(t *testing.T) {
	audience := &fakeAudience{targets: []string{readerA}}
	feed, _ := newTestFeed(t, defaultOptions(), audience)
	ctx := context.Background()

	const (
		older      = "ffffffff-ffff-4fff-8fff-ffffffffffff"
		newer      = "99999999-9999-4999-8999-999999999999"
		newest     = "88888888-8888-4888-8888-888888888888"
		highFanout = "77777777-7777-4777-8777-777777777777"
	)

	// The reader already has `older`, and `newer` arrives later from the same
	// author's stored feed; `newer` also appears in the author feed, so the merge
	// must not repeat it.
	if _, err := feed.FanoutStory(ctx, older, authorID, "eng", testNow); err != nil {
		t.Fatalf("FanoutStory(older) error = %v, want nil", err)
	}
	if err := feed.Seed(ctx, readerA, []Entry{{ID: newer, CreatedAt: testNow.Add(time.Hour)}}); err != nil {
		t.Fatalf("Seed() error = %v, want nil", err)
	}
	for _, entry := range []Entry{
		{ID: newer, CreatedAt: testNow.Add(time.Hour)},
		{ID: newest, CreatedAt: testNow.Add(2 * time.Hour)},
	} {
		if err := feed.Seed(ctx, highFanout, []Entry{entry}); err != nil {
			t.Fatalf("Seed(author) error = %v, want nil", err)
		}
	}

	// The author feed is read at AuthorFeedKey, so seed it there.
	if err := feed.sets.Prepend(ctx, AuthorFeedKey(highFanout), float64(testNow.Add(2*time.Hour).Unix()), newest, FeedCap); err != nil {
		t.Fatalf("prepend author feed: %v", err)
	}

	items, err := feed.ReadMerged(ctx, readerA, 10, []string{highFanout})
	if err != nil {
		t.Fatalf("ReadMerged() error = %v, want nil", err)
	}

	want := []string{newest, newer, older}
	if len(items) != len(want) {
		t.Fatalf("merged feed = %v, want %v", items, want)
	}
	for i := range want {
		if items[i].ID != want[i] {
			t.Fatalf("merged feed = %v, want %v", items, want)
		}
		if items[i].Kind != KindStory {
			t.Fatalf("entry %d kind = %q, want %q", i, items[i].Kind, KindStory)
		}
	}
}

func TestReadHonoursTheLimit(t *testing.T) {
	audience := &fakeAudience{targets: []string{readerA}}
	feed, _ := newTestFeed(t, defaultOptions(), audience)
	ctx := context.Background()

	for i := 0; i < 5; i++ {
		if _, err := feed.FanoutStory(ctx, storyIDFor(i), authorID, "eng", testNow.Add(time.Duration(i)*time.Second)); err != nil {
			t.Fatalf("FanoutStory(%d) error = %v, want nil", i, err)
		}
	}

	items, err := feed.Read(ctx, readerA, 2)
	if err != nil {
		t.Fatalf("Read() error = %v, want nil", err)
	}
	if len(items) != 2 || items[0].ID != storyIDFor(4) || items[1].ID != storyIDFor(3) {
		t.Errorf("Read(limit 2) = %v, want the two newest", items)
	}
}

func TestReadOfAnUnknownReaderIsEmptyNotAnError(t *testing.T) {
	feed, _ := newTestFeed(t, defaultOptions(), nil)

	ids, err := feed.Read(context.Background(), outsiderID, 10)
	if err != nil {
		t.Fatalf("Read() error = %v, want nil", err)
	}
	if ids == nil {
		t.Fatal("Read() = nil, want an empty slice (a miss is not an error)")
	}
	if len(ids) != 0 {
		t.Errorf("Read() = %v, want empty", ids)
	}
}

func TestNewValidatesItsInputs(t *testing.T) {
	server := miniredis.RunT(t)
	redisCache, err := cache.NewRedisCache(server.Addr(), "", 0)
	if err != nil {
		t.Fatalf("NewRedisCache() error = %v, want nil", err)
	}
	t.Cleanup(func() { _ = redisCache.Close() })

	sets, err := cache.NewSortedSet(redisCache)
	if err != nil {
		t.Fatalf("NewSortedSet() error = %v, want nil", err)
	}

	if _, err := New(nil, &fakeAudience{}, defaultOptions()); err == nil {
		t.Error("New(nil sets) error = nil, want an error")
	}
	if _, err := New(sets, nil, defaultOptions()); err == nil {
		t.Error("New(nil audience) error = nil, want an error")
	}
	if _, err := New(sets, &fakeAudience{}, Options{Cap: 0, FanoutThreshold: 10}); err == nil {
		t.Error("New(cap 0) error = nil, want an error")
	}
	if _, err := New(sets, &fakeAudience{}, Options{Cap: 10, FanoutThreshold: 0}); err == nil {
		t.Error("New(threshold 0) error = nil, want an error")
	}
}

func TestNewSortedSetRejectsAForeignCache(t *testing.T) {
	if _, err := cache.NewSortedSet(foreignCache{}); err == nil {
		t.Error("NewSortedSet(foreign cache) error = nil, want an error")
	}
}

// foreignCache is a Cache that is not the Redis implementation, which is the only
// thing NewSortedSet can serve.
type foreignCache struct{}

func (foreignCache) Get(context.Context, string) (string, error)              { return "", cache.ErrMiss }
func (foreignCache) Set(context.Context, string, string, time.Duration) error { return nil }
func (foreignCache) SetEX(context.Context, string, string, time.Duration) error {
	return nil
}
func (foreignCache) Delete(context.Context, string) error { return nil }
func (foreignCache) Incr(context.Context, string, time.Duration) (int64, error) {
	return 0, nil
}
func (foreignCache) Ping(context.Context) error { return nil }
func (foreignCache) Close() error               { return nil }

// zcard reports how many members a sorted set holds, through miniredis's direct
// API (which has no ZCARD helper). A key that was never written holds zero members.
func zcard(t *testing.T, server *miniredis.Miniredis, key string) int {
	t.Helper()

	if !server.Exists(key) {
		return 0
	}

	members, err := server.ZMembers(key)
	if err != nil {
		t.Fatalf("ZMembers(%q) error = %v, want nil", key, err)
	}
	return len(members)
}

// storyIDFor returns a distinct canonical UUID for an index, so a test can talk
// about "story 3" without spelling out a UUID.
func storyIDFor(index int) string {
	const digits = "0123456789abcdef"

	suffix := make([]byte, 12)
	for i := len(suffix) - 1; i >= 0; i-- {
		suffix[i] = digits[index%16]
		index /= 16
	}

	return "00000000-0000-4000-8000-" + string(suffix)
}

func TestFanoutVersionWritesTheVersionFeed(t *testing.T) {
	audience := &fakeAudience{targets: []string{readerA, readerB}}
	feed, server := newTestFeed(t, defaultOptions(), audience)

	const versionID = "aaaaaaaa-aaaa-4aaa-9aaa-aaaaaaaaaaaa"

	result, err := feed.FanoutVersion(context.Background(), versionID, authorID, "zul", testNow)
	if err != nil {
		t.Fatalf("FanoutVersion() error = %v, want nil", err)
	}
	if result.Recipients != 2 {
		t.Errorf("Recipients = %d, want 2", result.Recipients)
	}

	for _, userID := range []string{readerA, readerB} {
		items := readItems(t, feed, userID, 10)
		if len(items) != 1 || items[0].ID != versionID || items[0].Kind != KindVersion {
			t.Fatalf("feed %s = %v, want one version entry %s", userID, items, versionID)
		}

		// It lands in the VERSION set, not the story set: a reader that only knows
		// the story set must never be handed a version id as a story.
		if got := zcard(t, server, FeedVersionsKey(userID)); got != 1 {
			t.Errorf("version set %s cardinality = %d, want 1", userID, got)
		}
		if got := zcard(t, server, FeedKey(userID)); got != 0 {
			t.Errorf("story set %s cardinality = %d, want 0", userID, got)
		}

		score, err := server.ZScore(FeedVersionsKey(userID), versionID)
		if err != nil {
			t.Fatalf("ZScore(%s) error = %v, want nil", userID, err)
		}
		if want := float64(testNow.Unix()); score != want {
			t.Errorf("score in %s = %v, want %v (the version's own created_at)", userID, score, want)
		}
	}

	// The author is never a recipient of their own adaptation.
	if items := readItems(t, feed, authorID, 10); len(items) != 0 {
		t.Errorf("author feed = %v, want empty", items)
	}
}

func TestFanoutVersionTrimsToCap(t *testing.T) {
	const cap = 2

	audience := &fakeAudience{targets: []string{readerA}}
	feed, server := newTestFeed(t, Options{Cap: cap, FanoutThreshold: FanoutThreshold}, audience)

	for i := 0; i < 4; i++ {
		if _, err := feed.FanoutVersion(context.Background(), storyIDFor(i), authorID, "eng", testNow.Add(time.Duration(i)*time.Second)); err != nil {
			t.Fatalf("FanoutVersion(%d) error = %v, want nil", i, err)
		}
	}

	if got := zcard(t, server, FeedVersionsKey(readerA)); got != cap {
		t.Fatalf("version set cardinality = %d, want %d", got, cap)
	}

	items := readItems(t, feed, readerA, 10)
	if len(items) != cap || items[0].ID != storyIDFor(3) || items[1].ID != storyIDFor(2) {
		t.Fatalf("version feed = %v, want the two newest", items)
	}
}

func TestFanoutVersionHighFanoutStoresTheAuthorVersionFeed(t *testing.T) {
	audience := &fakeAudience{targets: []string{readerA, readerB, outsiderID}}
	feed, server := newTestFeed(t, Options{Cap: FeedCap, FanoutThreshold: 2}, audience)

	const versionID = "bbbbbbbb-bbbb-4bbb-9bbb-bbbbbbbbbbbb"

	result, err := feed.FanoutVersion(context.Background(), versionID, authorID, "eng", testNow)
	if err != nil {
		t.Fatalf("FanoutVersion() error = %v, want nil", err)
	}
	if !result.HighFanout || result.Recipients != 0 {
		t.Errorf("result = %+v, want HighFanout with no per-reader writes", result)
	}

	if got := zcard(t, server, AuthorFeedVersionsKey(authorID)); got != 1 {
		t.Errorf("author version feed cardinality = %d, want 1", got)
	}
	for _, userID := range []string{readerA, readerB, outsiderID} {
		if got := zcard(t, server, FeedVersionsKey(userID)); got != 0 {
			t.Errorf("version set %s cardinality = %d, want 0", userID, got)
		}
	}

	merged, err := feed.ReadMerged(context.Background(), readerA, 10, []string{authorID})
	if err != nil {
		t.Fatalf("ReadMerged() error = %v, want nil", err)
	}
	if len(merged) != 1 || merged[0].ID != versionID || merged[0].Kind != KindVersion {
		t.Errorf("merged feed = %v, want the version entry %s", merged, versionID)
	}
}

func TestReadMergedInterleavesStoriesAndVersions(t *testing.T) {
	audience := &fakeAudience{targets: []string{readerA}}
	feed, _ := newTestFeed(t, defaultOptions(), audience)
	ctx := context.Background()

	// Written out of order on purpose: the merge is by score, not by arrival.
	if _, err := feed.FanoutStory(ctx, storyIDFor(2), authorID, "eng", testNow.Add(3*time.Second)); err != nil {
		t.Fatalf("FanoutStory(2) error = %v, want nil", err)
	}
	if _, err := feed.FanoutVersion(ctx, storyIDFor(1), authorID, "eng", testNow.Add(2*time.Second)); err != nil {
		t.Fatalf("FanoutVersion(1) error = %v, want nil", err)
	}
	if _, err := feed.FanoutStory(ctx, storyIDFor(0), authorID, "eng", testNow.Add(time.Second)); err != nil {
		t.Fatalf("FanoutStory(0) error = %v, want nil", err)
	}

	items := readItems(t, feed, readerA, 10)
	if len(items) != 3 {
		t.Fatalf("merged feed = %v, want 3 entries", items)
	}

	want := []struct {
		id   string
		kind Kind
	}{
		{storyIDFor(2), KindStory},
		{storyIDFor(1), KindVersion},
		{storyIDFor(0), KindStory},
	}
	for i, entry := range want {
		if items[i].ID != entry.id || items[i].Kind != entry.kind {
			t.Fatalf("merged feed = %v, want %v", items, want)
		}
	}

	// The page is truncated AFTER the interleave, so a limit of 2 keeps the two
	// newest across both kinds rather than two of one.
	if limited := readItems(t, feed, readerA, 2); len(limited) != 2 || limited[0].ID != storyIDFor(2) || limited[1].ID != storyIDFor(1) {
		t.Errorf("merged feed (limit 2) = %v, want the two newest of both kinds", limited)
	}
}

func TestSeedWritesBothKinds(t *testing.T) {
	feed, server := newTestFeed(t, defaultOptions(), nil)
	ctx := context.Background()

	entries := []Entry{
		{ID: storyIDFor(0), Kind: KindStory, CreatedAt: testNow},
		{ID: storyIDFor(1), Kind: KindVersion, CreatedAt: testNow.Add(time.Minute)},
	}

	if err := feed.Seed(ctx, readerA, entries); err != nil {
		t.Fatalf("Seed() error = %v, want nil", err)
	}

	if got := zcard(t, server, FeedKey(readerA)); got != 1 {
		t.Errorf("story set cardinality = %d, want 1", got)
	}
	if got := zcard(t, server, FeedVersionsKey(readerA)); got != 1 {
		t.Errorf("version set cardinality = %d, want 1", got)
	}

	items := readItems(t, feed, readerA, 10)
	if len(items) != 2 || items[0].Kind != KindVersion || items[1].Kind != KindStory {
		t.Fatalf("seeded feed = %v, want the version then the story", items)
	}
}
