package httpapi

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"time"

	"github.com/knot/backend/internal/feedcache"
	"github.com/knot/backend/internal/moderation"
	"github.com/knot/backend/internal/reactions"
	"github.com/knot/backend/internal/versions"
)

// VersionsService is the slice of the versions service that the HTTP layer
// needs. Depending on an interface (rather than the concrete service) keeps
// handler tests free of a database.
type VersionsService interface {
	CreateAdaptation(ctx context.Context, in versions.CreateAdaptationInput) (versions.StoryVersion, error)
	GetVersion(ctx context.Context, id string) (versions.StoryVersion, error)
	GetTree(ctx context.Context, storyID string) ([]versions.StoryVersion, error)
	// BuildChain returns one version's attribution lineage, ordered root-first,
	// so the handler can state who an adaptation was adapted from (KNOT-021).
	BuildChain(ctx context.Context, versionID string) (versions.Chain, error)
}

// VersionsHandler serves the Tell My People endpoints.
type VersionsHandler struct {
	service   VersionsService
	authors   AuthorLookup
	rooted    RootedLookup
	reactions ReactionsLookup
	recorder  InteractionRecorder
	// caches are the optional Redis read caches: the version feed sets an
	// adaptation is fanned out into, and the version detail store (KNOT-036-fix).
	// Nil fields mean "take the database path".
	caches Caches
	// media reads the story's media for a version detail. A version has no media
	// of its own: it shows the media of the story it adapts, with the same
	// derivative URLs the story detail carries (KNOT-036-fix3). Nil means "answer
	// an empty media list".
	media  StoryMediaLookup
	logger *slog.Logger
}

// NewVersionsHandler returns a handler backed by service. The author lookup names
// and pictures each version's author (one batched read per response), the rooted
// lookup attaches that author's inline Rooted summary, and the reactions lookup
// attaches the perspective-reaction counts and the reader's own signals.
//
// Without a recorder the handler writes no interaction log;
// NewVersionsHandlerWithRecorder is the composition root's entry point
// (KNOT-026).
func NewVersionsHandler(service VersionsService, authors AuthorLookup, rooted RootedLookup, reactions ReactionsLookup, logger *slog.Logger) (*VersionsHandler, error) {
	return NewVersionsHandlerWithRecorder(service, authors, rooted, reactions, logger, nil)
}

// NewVersionsHandlerWithRecorder is NewVersionsHandler plus the engagement
// recorder. A nil recorder disables the log.
func NewVersionsHandlerWithRecorder(service VersionsService, authors AuthorLookup, rooted RootedLookup, reactions ReactionsLookup, logger *slog.Logger, recorder InteractionRecorder) (*VersionsHandler, error) {
	return NewVersionsHandlerWithCaches(service, authors, rooted, reactions, logger, recorder, Caches{})
}

// NewVersionsHandlerWithCaches is NewVersionsHandlerWithRecorder plus the Redis
// read caches (KNOT-036-fix).
//
// A nil cache leaves every behaviour unchanged: the version detail is the database
// read it has always been, and a new adaptation is not fanned out.
func NewVersionsHandlerWithCaches(service VersionsService, authors AuthorLookup, rooted RootedLookup, reactions ReactionsLookup, logger *slog.Logger, recorder InteractionRecorder, caches Caches) (*VersionsHandler, error) {
	return NewVersionsHandlerWithMedia(service, authors, rooted, reactions, logger, recorder, caches, nil)
}

// NewVersionsHandlerWithMedia is NewVersionsHandlerWithCaches plus the story-media
// lookup (KNOT-036-fix3).
//
// A version detail carries the media of the story it adapts, because a version has
// none of its own. A nil lookup answers an empty media array rather than failing.
func NewVersionsHandlerWithMedia(service VersionsService, authors AuthorLookup, rooted RootedLookup, reactions ReactionsLookup, logger *slog.Logger, recorder InteractionRecorder, caches Caches, media StoryMediaLookup) (*VersionsHandler, error) {
	if service == nil {
		return nil, fmt.Errorf("httpapi: versions handler requires a service")
	}
	if authors == nil {
		return nil, fmt.Errorf("httpapi: versions handler requires an author lookup")
	}
	if rooted == nil {
		return nil, fmt.Errorf("httpapi: versions handler requires a rooted lookup")
	}
	if reactions == nil {
		return nil, fmt.Errorf("httpapi: versions handler requires a reactions lookup")
	}
	if logger == nil {
		return nil, fmt.Errorf("httpapi: versions handler requires a logger")
	}
	return &VersionsHandler{
		service:   service,
		authors:   authors,
		rooted:    rooted,
		reactions: reactions,
		recorder:  recorder,
		caches:    caches,
		media:     media,
		logger:    logger,
	}, nil
}

// createAdaptationRequest is the POST /stories/{id}/adapt body.
//
// There is deliberately no author_id field. The adapter is taken from the
// authenticated request, so a client cannot adapt as somebody else — and because
// decodeJSON rejects unknown fields, sending author_id is a 400 rather than a
// silently ignored field.
type createAdaptationRequest struct {
	ParentVersionID string `json:"parent_version_id"`
	Language        string `json:"language"`
	Title           string `json:"title"`
	Body            string `json:"body"`
	AdaptationNote  string `json:"adaptation_note"`
}

// versionResponse is the public projection of a story version. It is an explicit
// type, not the domain StoryVersion, so the wire format is a deliberate choice.
//
// ParentVersionID and AdaptationNote are pointers so an absent value is JSON
// null: the root version has no parent, and an adapter may leave no note.
type versionResponse struct {
	ID              string  `json:"id"`
	StoryID         string  `json:"story_id"`
	ParentVersionID *string `json:"parent_version_id"`
	AuthorID        string  `json:"author_id"`
	// AuthorDisplayName and AuthorAvatarURL are the author's inline attribution,
	// filled by one batched lookup per response. AuthorAvatarURL is the backend
	// path to fetch an avatar from, or null when the author has none, so a client
	// renders initials (KNOT-ADR-041).
	AuthorDisplayName string    `json:"author_display_name"`
	AuthorAvatarURL   *string   `json:"author_avatar_url"`
	Language          string    `json:"language"`
	Title             string    `json:"title"`
	Body              string    `json:"body"`
	AdaptationNote    *string   `json:"adaptation_note"`
	CreatedAt         time.Time `json:"created_at"`
	UpdatedAt         time.Time `json:"updated_at"`
	// AuthorRooted is the version author's primary public Rooted signal, or null
	// when they have none. It is a summary (place and duration only), attached at
	// the HTTP layer; see KNOT-ADR-017.
	AuthorRooted *rootedSummary `json:"author_rooted"`
	// Reactions is the count of each of the four perspective signals on the
	// version (KNOT-ADR-051). MyReactions is the signals the reader holds.
	Reactions   reactionCountsResponse `json:"reactions"`
	MyReactions []string               `json:"my_reactions"`
	// AttributionLine is the human sentence tracing this version's lineage back
	// to the story's original, and VersionDepth is how many adaptations separate
	// the two: 0 for a root version, 1 for a first adaptation, and so on. Both
	// are derived at read time from the parent pointers rather than stored on the
	// row, so the tree stays the single source of truth (KNOT-ADR-069). The line
	// is rendered server-side and the client shows it verbatim.
	AttributionLine string `json:"attribution_line"`
	VersionDepth    int    `json:"version_depth"`
}

// versionEnvelope wraps a single version, so the response shape can gain sibling
// fields later without breaking clients.
type versionEnvelope struct {
	Version versionResponse `json:"version"`
}

// versionDetailResponse is GET /versions/{id}: the version plus the media of the
// story it adapts, in display order (KNOT-036-fix3).
//
// It is a separate type from versionResponse so the ADAPT and TREE responses keep
// exactly the shape they had: adding a field to versionResponse would have put an
// empty `media` key on every version in a tree. The embedded struct's fields are
// promoted, so the JSON is the version's own fields plus `media`.
type versionDetailResponse struct {
	versionResponse
	// Media mirrors the story detail's media array, including thumbnail_url,
	// medium_url, and poster_url for every item. It is always an array, never null.
	Media []storyMediaResponse `json:"media"`
}

// versionDetailEnvelope wraps a single version detail, and is also the shape the
// detail cache stores.
type versionDetailEnvelope struct {
	Version versionDetailResponse `json:"version"`
}

// treeResponse is the GET /stories/{id}/tree body. Versions is a flat adjacency
// list: every version carries its parent pointer and the client assembles the
// tree. Versions is always an array, never null.
type treeResponse struct {
	StoryID  string            `json:"story_id"`
	Versions []versionResponse `json:"versions"`
}

// Adapt handles POST /stories/{id}/adapt. The route is protected, so the adapter
// id comes from the context rather than from the body.
func (h *VersionsHandler) Adapt(w http.ResponseWriter, r *http.Request) {
	authorID, ok := UserIDFromContext(r.Context())
	if !ok {
		// Unreachable while the route is wrapped by AuthMiddleware. It is kept
		// as a fail-closed guard so that a future wiring mistake cannot create a
		// version with no author.
		writeError(w, http.StatusUnauthorized, codeUnauthorized, "authentication required")
		return
	}

	var body createAdaptationRequest
	if err := decodeJSON(w, r, &body); err != nil {
		writeDecodeError(w, r, h.logger, err)
		return
	}

	created, err := h.service.CreateAdaptation(r.Context(), versions.CreateAdaptationInput{
		StoryID:         r.PathValue("id"),
		ParentVersionID: body.ParentVersionID,
		AuthorID:        authorID,
		Language:        body.Language,
		Title:           body.Title,
		Body:            body.Body,
		AdaptationNote:  body.AdaptationNote,
	})
	if err != nil {
		h.writeServiceError(w, r, err, "story or parent version not found")
		return
	}

	response := newVersionResponse(created)
	response.AuthorRooted = authorRootedSummaries(r.Context(), h.rooted, h.logger, []string{created.AuthorID})[created.AuthorID]
	response.AuthorDisplayName, response.AuthorAvatarURL = authorFields(r.Context(), h.authors, h.logger, created.AuthorID)
	// The new version's depth is its parent's plus one, which walking its own
	// chain produces without a separate count.
	response.AttributionLine, response.VersionDepth = h.attribution(r.Context(), created.ID)

	// The adaptation is credited to the telling it was written from: that is the
	// content whose pillar the reader engaged with (KNOT-026).
	recordInteraction(r.Context(), h.logger, h.recorder, authorID, interactionEntityVersion, body.ParentVersionID, interactionAdapt)

	// The parent story's cached detail is stale from this moment: its version_count
	// and language_count changed. Deleting it is the invalidation the read cache
	// needs, and it happens before the response so the next reader cannot see the
	// old counts (KNOT-036-fix).
	invalidateStoryDetail(r.Context(), h.logger, h.caches.Details, created.StoryID)
	// A brand-new version cannot be cached yet, so this delete is hygiene rather
	// than repair, exactly like the one a story creation performs.
	invalidateVersionDetail(r.Context(), h.logger, h.caches.Details, created.ID)

	// The adaptation is fanned out on write like a story, into the version feed
	// sets, so it appears in readers' precomputed feeds where it was written
	// (KNOT-036-fix).
	recordFeedFanout(r.Context(), h.logger, h.caches.Feed, feedcache.KindVersion, created.ID, created.AuthorID, created.Language, created.CreatedAt)

	writeJSON(w, http.StatusCreated, versionEnvelope{Version: response})
}

// Tree handles GET /stories/{id}/tree. The route is public.
func (h *VersionsHandler) Tree(w http.ResponseWriter, r *http.Request) {
	storyID := r.PathValue("id")

	list, err := h.service.GetTree(r.Context(), storyID)
	if err != nil {
		h.writeServiceError(w, r, err, "story not found")
		return
	}

	authorIDs := make([]string, 0, len(list))
	for _, version := range list {
		authorIDs = append(authorIDs, version.AuthorID)
	}
	// One batched read per lookup names and pictures every author in the tree.
	summaries := authorRootedSummaries(r.Context(), h.rooted, h.logger, authorIDs)
	attributions := authorAttributions(r.Context(), h.authors, h.logger, authorIDs)

	items := make([]versionResponse, 0, len(list))
	for _, version := range list {
		item := newVersionResponse(version)
		item.AuthorRooted = summaries[version.AuthorID]
		attribution := attributions[version.AuthorID]
		item.AuthorDisplayName = attribution.DisplayName
		item.AuthorAvatarURL = optionalString(attribution.AvatarURL)

		// Every version's chain is resolved **in memory** from the rows already
		// loaded, so a tree of N versions costs no attribution query at all: the
		// names come from the page-wide attribution read above, and the structure
		// from the parent pointers already in hand (KNOT-021).
		chain, chainErr := versions.BuildChainFromVersions(version.ID, list)
		if chainErr != nil {
			// A chain that cannot be resolved -- parent pointers that loop -- is
			// logged and left empty rather than failing the page: the tree is
			// still readable without its attribution sentence.
			h.logger.WarnContext(
				r.Context(),
				"attribution chain resolution failed",
				slog.String("request_id", RequestIDFromContext(r.Context())),
				slog.String("version_id", version.ID),
				slog.String("error", chainErr.Error()),
			)
		} else {
			item.AttributionLine = versions.FormatAttribution(chainWithNames(chain, attributions))
			item.VersionDepth = chain.Depth()
		}

		items = append(items, item)
	}

	// One batched pair of reads decorates the whole tree with reaction counts and
	// the reader's own signals (KNOT-ADR-051).
	readerID, _ := UserIDFromContext(r.Context())
	versionIDs := make([]string, 0, len(items))
	for i := range items {
		versionIDs = append(versionIDs, items[i].ID)
	}
	reactionData := loadReactions(r.Context(), h.reactions, h.logger, reactions.EntityVersion, readerID, versionIDs)
	for i := range items {
		items[i].Reactions = reactionData.Counts[items[i].ID]
		items[i].MyReactions = orEmptyStrings(reactionData.Mine[items[i].ID])
	}

	writeJSON(w, http.StatusOK, treeResponse{StoryID: storyID, Versions: items})
}

// Get handles GET /versions/{id}. The route is public.
func (h *VersionsHandler) Get(w http.ResponseWriter, r *http.Request) {
	versionID := r.PathValue("id")

	// A repeat tap on the same adaptation is answered from Redis, with the reader's
	// own signals and the block filter re-applied (KNOT-036-fix).
	if cached, ok := h.cachedVersionDetail(r.Context(), versionID); ok {
		writeJSON(w, http.StatusOK, versionDetailEnvelope{Version: *cached})
		return
	}

	version, err := h.service.GetVersion(r.Context(), versionID)
	if err != nil {
		h.writeServiceError(w, r, err, "version not found")
		return
	}

	response := newVersionResponse(version)
	response.AuthorRooted = authorRootedSummaries(r.Context(), h.rooted, h.logger, []string{version.AuthorID})[version.AuthorID]
	response.AuthorDisplayName, response.AuthorAvatarURL = authorFields(r.Context(), h.authors, h.logger, version.AuthorID)

	userID, _ := UserIDFromContext(r.Context())
	response.Reactions, response.MyReactions = reactionsForEntity(r.Context(), h.reactions, h.logger, reactions.EntityVersion, userID, version.ID)
	response.AttributionLine, response.VersionDepth = h.attribution(r.Context(), version.ID)

	// A version has no media of its own: the detail shows the media of the story it
	// adapts, projected exactly as the story detail projects it, so an adaptation's
	// page renders the same thumbnails and posters (KNOT-036-fix3).
	detail := versionDetailResponse{
		versionResponse: response,
		Media:           storyMediaForDetail(r.Context(), h.media, h.logger, version.StoryID),
	}

	h.storeVersionDetail(r.Context(), detail)

	writeJSON(w, http.StatusOK, versionDetailEnvelope{Version: detail})
}

// attribution resolves one version's chain and renders its sentence, returning
// the line and the version's depth.
//
// Resolution is enrichment: a chain that cannot be built (a cycle, or a version
// that vanished mid-request) is logged and reported as an empty line at depth 0
// rather than failing a response the reader can still use.
func (h *VersionsHandler) attribution(ctx context.Context, versionID string) (string, int) {
	chain, err := h.service.BuildChain(ctx, versionID)
	if err != nil {
		h.logger.WarnContext(
			ctx,
			"attribution chain resolution failed",
			slog.String("request_id", RequestIDFromContext(ctx)),
			slog.String("version_id", versionID),
			slog.String("error", err.Error()),
		)
		return "", 0
	}

	return attributionText(ctx, h.authors, h.logger, chain), chain.Depth()
}

// writeServiceError maps domain errors onto HTTP status codes. Only errors we
// recognise as safe are described to the client; everything else becomes a
// generic 500 and is logged with the request id. notFoundMessage lets each route
// name what was not found.
func (h *VersionsHandler) writeServiceError(w http.ResponseWriter, r *http.Request, err error, notFoundMessage string) {
	var validation *versions.ValidationError

	switch {
	case errors.As(err, &validation):
		writeError(w, http.StatusBadRequest, codeValidation, fmt.Sprintf("%s %s", validation.Field, validation.Message))
	case errors.Is(err, versions.ErrNotFound):
		writeError(w, http.StatusNotFound, codeNotFound, notFoundMessage)
	case errors.Is(err, moderation.ErrBlocked):
		writeError(w, http.StatusForbidden, codeBlocked, "you cannot interact with this content")
	default:
		h.logger.ErrorContext(
			r.Context(),
			"versions request failed",
			slog.String("request_id", RequestIDFromContext(r.Context())),
			slog.String("path", r.URL.Path),
			slog.String("error", err.Error()),
		)
		writeError(w, http.StatusInternalServerError, codeInternal, "internal server error")
	}
}

// newVersionResponse projects a domain version onto the wire format.
func newVersionResponse(version versions.StoryVersion) versionResponse {
	return versionResponse{
		ID:              version.ID,
		StoryID:         version.StoryID,
		ParentVersionID: optionalString(version.ParentVersionID),
		AuthorID:        version.AuthorID,
		Language:        version.Language,
		Title:           version.Title,
		Body:            version.Body,
		AdaptationNote:  optionalString(version.AdaptationNote),
		CreatedAt:       version.CreatedAt,
		UpdatedAt:       version.UpdatedAt,
		// Emit [] rather than null until the enrichment fills it in.
		MyReactions: []string{},
	}
}

// optionalString maps "" onto nil so an absent value is serialised as JSON null
// rather than as an empty string that a client might mistake for a real value.
func optionalString(value string) *string {
	if value == "" {
		return nil
	}
	return &value
}

// optionalInt wraps an int that one response shape carries and another does not.
// Paired with `omitempty` it drops the whole key rather than sending a zero the
// reader might act on -- which is how the feed stays free of the attribution
// fields this task adds only to the story detail (KNOT-021).
func optionalInt(value int) *int {
	return &value
}
