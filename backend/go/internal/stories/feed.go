package stories

import "time"

// FeedKind discriminates the two entry types the merged feed carries. It is the
// `kind` field on the wire, so the client can dispatch an entry to the right
// screen without inspecting which fields happen to be present (KNOT-022).
type FeedKind string

// The feed entry kinds.
const (
	// FeedKindStory is a story: the original telling, its root version's content.
	FeedKindStory FeedKind = "story"
	// FeedKindVersion is an adaptation of a story: a `story_versions` row with a
	// parent. Root versions are never feed entries of their own, because the
	// story branch already carries the root's content.
	FeedKindVersion FeedKind = "version"
)

// FeedItem is one entry of the merged feed.
//
// Kind says which entry this is, and exactly one of Story and Version carries its
// data: a story entry fills Story, a version entry fills Version. The other is
// the zero value and must not be read.
//
// Story is embedded rather than named so a story entry reads like the Story it
// is (`item.Title`), which is how every existing caller and test already reads a
// list page.
//
// **That promotion is only valid for a story entry.** On a version entry the
// promoted fields resolve to the embedded Story's zero values, so a caller must
// switch on Kind before reading anything from item.Story or item.Version. The
// entry's own identity and sort key come from SortID and SortTime below, which
// answer correctly for both kinds -- a caller that reads item.CreatedAt directly
// would silently get the zero time on a version entry and corrupt pagination.
type FeedItem struct {
	// Kind is the entry's discriminator.
	Kind FeedKind
	// Story is the entry's story, set when Kind is FeedKindStory.
	Story
	// Version is the entry's adaptation, set when Kind is FeedKindVersion.
	Version VersionFeedItem
}

// SortID returns the entry's own id: the story id for a story entry, the version
// id for a version entry. It is the id half of the feed's sort key.
func (i FeedItem) SortID() string {
	if i.Kind == FeedKindVersion {
		return i.Version.ID
	}
	return i.Story.ID
}

// SortTime returns the entry's feed sort timestamp: a story's own creation time,
// or a version's, so an adaptation appears where it was written rather than where
// its story was. It is the timestamp half of the feed's sort key.
func (i FeedItem) SortTime() time.Time {
	if i.Kind == FeedKindVersion {
		return i.Version.CreatedAt
	}
	return i.Story.CreatedAt
}

// VersionFeedItem is an adaptation as the feed lists it.
//
// It carries the version's own content and author, plus the fields a card needs
// from the **parent story**: its pillar, its place, its media, and its aggregate
// counts. A version has no media of its own and no place of its own, so borrowing
// the story's is what lets a version card read exactly like a story card, told
// apart only by the kind badge and the attribution line (KNOT-022).
type VersionFeedItem struct {
	// ID is the version's own id. StoryID is the parent story's.
	ID      string
	StoryID string
	// ParentVersionID is the version this one adapted.
	ParentVersionID string
	// AuthorID is the adapter, who is also the version's author.
	AuthorID string
	// Language, Title, and Body are the version's own content.
	Language string
	Title    string
	Body     string
	// CreatedAt is the version's insertion time: the entry's feed sort key, so
	// an adaptation appears where it was written, not where the story was.
	CreatedAt time.Time
	// UpdatedAt is maintained by the database.
	UpdatedAt time.Time
	// The parent story's fields.
	Pillar              Pillar
	ApproximateLocation string
	Latitude            *float64
	Longitude           *float64
	PlaceCountry        *string
	MediaURLs           []string
}
