package httpapi

// The Redis-backed read caches the content handlers consult before the database
// (KNOT-036, KNOT-036-fix).
//
// They travel together in one value because they are wired together, switched
// together and absent together: a deployment with Redis trouble disables all of
// them, and a handler test that cares about one almost always has an opinion about
// the other. Either field may be nil, and a nil cache means "take the database
// path", never "fail".

// Caches are the optional precomputed read caches.
type Caches struct {
	// Feed is the precomputed chronological feed: the first page of GET /stories.
	Feed FeedCache
	// Details is the story and version detail cache: GET /stories/{id} and
	// GET /versions/{id}.
	Details DetailCache
}
