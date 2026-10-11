package httpapi

import (
	"fmt"
	"log/slog"
	"net/http"
	"time"

	"github.com/knot/backend/internal/language"
)

// errNilHandler reports a missing dependency at construction time, which is a
// programming error rather than a runtime condition.
func errNilHandler(what string) error {
	return fmt.Errorf("httpapi: router requires a non-nil %s", what)
}

// healthResponse is the GET /health body.
type healthResponse struct {
	Status  string `json:"status"`
	Version string `json:"version"`
	Time    string `json:"time"`
}

// Router builds the Knot HTTP handler.
//
// Middleware order, outermost first:
//
//	request id  ->  logging  ->  recover  ->  routes
//
// Recover sits inside logging so that a recovered panic is still reported with
// its status and duration.
//
// Authentication is not part of that chain: it is applied per route by
// AuthMiddleware.Require, because GET /stories and GET /stories/{id} are public
// while POST /stories is not. The protection therefore travels with the route
// that needs it, and a public route cannot be exposed by a mistake in the
// composition order.
type Router struct {
	auth           *AuthHandler
	stories        *StoriesHandler
	versions       *VersionsHandler
	conversations  *ConversationsHandler
	rooted         *RootedHandler
	discovery      *DiscoveryHandler
	avatar         *AvatarHandler
	storyMedia     *StoryMediaHandler
	notifications  *NotificationsHandler
	profile        *ProfileHandler
	reactions      *ReactionsHandler
	inquiries      *InquiriesHandler
	moderation     *ModerationHandler
	authMiddleware *AuthMiddleware
	version        string
	logger         *slog.Logger
}

// NewRouter returns the root handler for the API.
func NewRouter(auth *AuthHandler, storiesHandler *StoriesHandler, versionsHandler *VersionsHandler, conversationsHandler *ConversationsHandler, rootedHandler *RootedHandler, discoveryHandler *DiscoveryHandler, avatarHandler *AvatarHandler, storyMediaHandler *StoryMediaHandler, notificationsHandler *NotificationsHandler, profileHandler *ProfileHandler, reactionsHandler *ReactionsHandler, inquiriesHandler *InquiriesHandler, moderationHandler *ModerationHandler, authMiddleware *AuthMiddleware, version string, logger *slog.Logger) (*Router, error) {
	if auth == nil {
		return nil, errNilHandler("auth")
	}
	if storiesHandler == nil {
		return nil, errNilHandler("stories")
	}
	if versionsHandler == nil {
		return nil, errNilHandler("versions")
	}
	if conversationsHandler == nil {
		return nil, errNilHandler("conversations")
	}
	if rootedHandler == nil {
		return nil, errNilHandler("rooted")
	}
	if discoveryHandler == nil {
		return nil, errNilHandler("discovery")
	}
	if avatarHandler == nil {
		return nil, errNilHandler("avatar")
	}
	if storyMediaHandler == nil {
		return nil, errNilHandler("story media")
	}
	if notificationsHandler == nil {
		return nil, errNilHandler("notifications")
	}
	if profileHandler == nil {
		return nil, errNilHandler("profile")
	}
	if reactionsHandler == nil {
		return nil, errNilHandler("reactions")
	}
	if inquiriesHandler == nil {
		return nil, errNilHandler("inquiries")
	}
	if moderationHandler == nil {
		return nil, errNilHandler("moderation")
	}
	if authMiddleware == nil {
		return nil, errNilHandler("auth middleware")
	}
	if logger == nil {
		return nil, errNilHandler("logger")
	}
	return &Router{
		auth:           auth,
		stories:        storiesHandler,
		versions:       versionsHandler,
		conversations:  conversationsHandler,
		rooted:         rootedHandler,
		discovery:      discoveryHandler,
		avatar:         avatarHandler,
		storyMedia:     storyMediaHandler,
		notifications:  notificationsHandler,
		profile:        profileHandler,
		reactions:      reactionsHandler,
		inquiries:      inquiriesHandler,
		moderation:     moderationHandler,
		authMiddleware: authMiddleware,
		version:        version,
		logger:         logger,
	}, nil
}

// Handler returns the composed http.Handler, middleware included.
func (r *Router) Handler() http.Handler {
	mux := http.NewServeMux()

	// Method-qualified patterns are stdlib ServeMux features as of Go 1.22.
	mux.HandleFunc("GET /health", r.handleHealth)
	mux.HandleFunc("GET /languages", r.handleLanguages)
	mux.HandleFunc("POST /auth/register", r.auth.Register)
	mux.HandleFunc("POST /auth/login", r.auth.Login)

	// Stories. Publishing requires an access token; reading is open. The reads use
	// optional auth so a signed-in caller's reaction highlights can be attached.
	mux.HandleFunc("POST /stories", r.authMiddleware.Require(r.stories.Create))
	mux.HandleFunc("GET /stories", r.authMiddleware.Optional(r.stories.List))
	mux.HandleFunc("GET /stories/{id}", r.authMiddleware.Optional(r.stories.Get))

	// Tell My People: story versions and the Language Tree. Adapting requires an
	// access token; reading a version or a tree is open.
	mux.HandleFunc("POST /stories/{id}/adapt", r.authMiddleware.Require(r.versions.Adapt))
	mux.HandleFunc("GET /stories/{id}/tree", r.authMiddleware.Optional(r.versions.Tree))
	mux.HandleFunc("GET /versions/{id}", r.authMiddleware.Optional(r.versions.Get))

	// Conversations: comments on a version, and the bridges between comments.
	// Commenting and bridging require an access token; reading is open.
	mux.HandleFunc("POST /versions/{id}/comments", r.authMiddleware.Require(r.conversations.CreateComment))
	mux.HandleFunc("GET /versions/{id}/comments", r.authMiddleware.Optional(r.conversations.ListComments))
	mux.HandleFunc("GET /comments/{id}", r.authMiddleware.Optional(r.conversations.GetComment))
	mux.HandleFunc("POST /comments/{id}/bridges", r.authMiddleware.Require(r.conversations.CreateBridge))
	mux.HandleFunc("GET /comments/{id}/bridges", r.authMiddleware.Optional(r.conversations.ListBridges))
	mux.HandleFunc("GET /bridges/{id}", r.authMiddleware.Optional(r.conversations.GetBridge))

	// Reactions: the four perspective signals, per entity kind. Toggling requires
	// an access token; listing is public. The path names the entity kind, so each
	// toggle resolves its target through that domain's own service (KNOT-ADR-050).
	// Comments are read-only here: POST /comments/{id}/reactions answers 400
	// (KNOT-ADR-053), while its GET stays available for auditing existing rows.
	mux.HandleFunc("POST /stories/{id}/reactions", r.authMiddleware.Require(r.reactions.ToggleStory))
	mux.HandleFunc("GET /stories/{id}/reactions", r.reactions.ListStory)
	mux.HandleFunc("POST /versions/{id}/reactions", r.authMiddleware.Require(r.reactions.ToggleVersion))
	mux.HandleFunc("GET /versions/{id}/reactions", r.reactions.ListVersion)
	mux.HandleFunc("POST /comments/{id}/reactions", r.authMiddleware.Require(r.reactions.ToggleComment))
	mux.HandleFunc("GET /comments/{id}/reactions", r.reactions.ListComment)
	mux.HandleFunc("POST /bridges/{id}/reactions", r.authMiddleware.Require(r.reactions.ToggleBridge))
	mux.HandleFunc("GET /bridges/{id}/reactions", r.reactions.ListBridge)
	mux.HandleFunc("POST /inquiries/{id}/reactions", r.authMiddleware.Require(r.reactions.ToggleInquiry))
	mux.HandleFunc("GET /inquiries/{id}/reactions", r.reactions.ListInquiry)

	// Curious Inquiries: a question about a place, and the public answers to it.
	// Asking and answering require an access token; reading is open, and the two
	// reads use optional auth so a signed-in reader's own reaction highlights travel
	// with the question (KNOT-ADR-051). Every inquiry and every answer is public and
	// attributed: there is no anonymous variant (KNOT-ADR-055).
	//
	// "GET /answers/{id}" is a top-level path rather than "/inquiries/answers/{id}"
	// because the latter would overlap "/inquiries/{id}/answers" at
	// /inquiries/answers/answers, which Go's ServeMux refuses as a conflict. The
	// top-level shape also matches how a single comment and a single bridge are
	// already fetched.
	mux.HandleFunc("POST /inquiries", r.authMiddleware.Require(r.inquiries.Create))
	mux.HandleFunc("GET /inquiries", r.authMiddleware.Optional(r.inquiries.List))
	mux.HandleFunc("GET /inquiries/{id}", r.authMiddleware.Optional(r.inquiries.Get))
	mux.HandleFunc("POST /inquiries/{id}/answers", r.authMiddleware.Require(r.inquiries.CreateAnswer))
	mux.HandleFunc("GET /inquiries/{id}/answers", r.authMiddleware.Optional(r.inquiries.ListAnswers))
	mux.HandleFunc("GET /answers/{id}", r.authMiddleware.Optional(r.inquiries.GetAnswer))

	// Rooted: a user's self-declared connection to a place. Writing a signal and
	// reading your own signals require an access token; reading another user's
	// public signals is open. The literal "me" pattern is more specific than the
	// "{id}" wildcard, so the two routes do not collide.
	mux.HandleFunc("POST /users/me/rooted", r.authMiddleware.Require(r.rooted.SetSignal))
	mux.HandleFunc("GET /users/me/rooted", r.authMiddleware.Require(r.rooted.GetMySignals))
	mux.HandleFunc("GET /users/{id}/rooted", r.rooted.GetUserSignals)

	// Profiles: a user's public wall of everything they have authored. The route is
	// public, and the literal "/profile" segment does not collide with "/rooted" or
	// "/avatar" (KNOT-ADR-042).
	mux.HandleFunc("GET /users/{id}/profile", r.profile.Get)

	// Discovery: finding stories by place. Both routes are public. The place is a
	// path segment and is URL-decoded by the router, so a client may send
	// "Cape%20Town" and reach the handler with "Cape Town".
	mux.HandleFunc("GET /discovery/clusters", r.discovery.Clusters)
	mux.HandleFunc("GET /discovery/places/{place}", r.discovery.PlaceStories)

	// Avatars: a user's profile image. Uploading requires an access token;
	// reading is open, because an avatar is part of a public profile. The bytes
	// are proxied by this backend, so the object store is never reachable from a
	// client and its bucket can stay private.
	mux.HandleFunc("POST /users/me/avatar", r.authMiddleware.Require(r.avatar.Upload))
	mux.HandleFunc("GET /users/{id}/avatar", r.avatar.Get)

	// Story media: images and videos attached to a story. Attaching and deleting
	// require an access token; listing and streaming are open, because a story's
	// media is part of a public story. The bytes are proxied by this backend, so
	// the object store is never reachable from a client (KNOT-ADR-029).
	mux.HandleFunc("POST /stories/{id}/media", r.authMiddleware.Require(r.storyMedia.Create))
	mux.HandleFunc("GET /stories/{id}/media", r.storyMedia.List)
	mux.HandleFunc("DELETE /stories/{id}/media/{mid}", r.authMiddleware.Require(r.storyMedia.Delete))
	mux.HandleFunc("GET /stories/{id}/media/{mid}/content", r.storyMedia.Content)

	// Notifications: the authenticated user's own in-app inbox. Every route is
	// protected, and every route is scoped to the caller's own notifications, so
	// there is no way to read or mark another user's inbox (KNOT-ADR-039).
	//	GET    /notifications                one page, newest first, cursor-paginated
	//	GET    /notifications/unread_count   the bell's badge number
	//	POST   /notifications/{id}/read      mark one read
	//	POST   /notifications/read_all       mark every unread notification read
	mux.HandleFunc("GET /notifications", r.authMiddleware.Require(r.notifications.List))
	mux.HandleFunc("GET /notifications/unread_count", r.authMiddleware.Require(r.notifications.UnreadCount))
	mux.HandleFunc("POST /notifications/read_all", r.authMiddleware.Require(r.notifications.MarkAllRead))
	mux.HandleFunc("POST /notifications/{id}/read", r.authMiddleware.Require(r.notifications.MarkRead))

	// Moderation foundation (KNOT-017a): content reports and user blocks. Every
	// route is protected. Reports are private to the reporter and the moderator
	// queue; blocks are silent. The moderator queue and its actions ship in
	// KNOT-017b, so this task registers no moderator route.
	mux.HandleFunc("POST /reports", r.authMiddleware.Require(r.moderation.CreateReport))
	mux.HandleFunc("GET /reports/mine", r.authMiddleware.Require(r.moderation.ListMyReports))
	mux.HandleFunc("POST /blocks/{user_id}", r.authMiddleware.Require(r.moderation.CreateBlock))
	mux.HandleFunc("DELETE /blocks/{user_id}", r.authMiddleware.Require(r.moderation.DeleteBlock))
	mux.HandleFunc("GET /blocks/mine", r.authMiddleware.Require(r.moderation.ListBlocks))

	return withRequestID(withRequestLogging(r.logger, withRecover(r.logger, mux)))
}

// handleHealth reports liveness. It deliberately touches no dependencies: it
// answers "is this process serving HTTP", not "is every dependency healthy".
func (r *Router) handleHealth(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, healthResponse{
		Status:  "ok",
		Version: r.version,
		Time:    time.Now().UTC().Format(time.RFC3339),
	})
}

// languageResponse is one entry of GET /languages.
type languageResponse struct {
	Code string `json:"code"`
	Name string `json:"name"`
}

// languagesResponse is the GET /languages body. Languages is always an array,
// never null.
type languagesResponse struct {
	Languages []languageResponse `json:"languages"`
}

// handleLanguages serves GET /languages: the canonical ISO 639-3 list the API
// accepts for every language field (KNOT-ADR-046).
//
// It needs no service: the list is a compile-time constant. The route is public,
// because a client needs the list to offer a picker before anyone has registered.
func (r *Router) handleLanguages(w http.ResponseWriter, _ *http.Request) {
	all := language.All()

	items := make([]languageResponse, 0, len(all))
	for _, item := range all {
		items = append(items, languageResponse{Code: item.Code, Name: item.Name})
	}

	writeJSON(w, http.StatusOK, languagesResponse{Languages: items})
}
