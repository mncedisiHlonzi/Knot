package httpapi

import (
	"fmt"
	"log/slog"
	"net/http"
	"time"
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
	authMiddleware *AuthMiddleware
	version        string
	logger         *slog.Logger
}

// NewRouter returns the root handler for the API.
func NewRouter(auth *AuthHandler, storiesHandler *StoriesHandler, versionsHandler *VersionsHandler, conversationsHandler *ConversationsHandler, authMiddleware *AuthMiddleware, version string, logger *slog.Logger) (*Router, error) {
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
	mux.HandleFunc("POST /auth/register", r.auth.Register)
	mux.HandleFunc("POST /auth/login", r.auth.Login)

	// Stories. Publishing requires an access token; reading is open.
	mux.HandleFunc("POST /stories", r.authMiddleware.Require(r.stories.Create))
	mux.HandleFunc("GET /stories", r.stories.List)
	mux.HandleFunc("GET /stories/{id}", r.stories.Get)

	// Tell My People: story versions and the Language Tree. Adapting requires an
	// access token; reading a version or a tree is open.
	mux.HandleFunc("POST /stories/{id}/adapt", r.authMiddleware.Require(r.versions.Adapt))
	mux.HandleFunc("GET /stories/{id}/tree", r.versions.Tree)
	mux.HandleFunc("GET /versions/{id}", r.versions.Get)

	// Conversations: comments on a version, and the bridges between comments.
	// Commenting and bridging require an access token; reading is open.
	mux.HandleFunc("POST /versions/{id}/comments", r.authMiddleware.Require(r.conversations.CreateComment))
	mux.HandleFunc("GET /versions/{id}/comments", r.conversations.ListComments)
	mux.HandleFunc("POST /comments/{id}/bridges", r.authMiddleware.Require(r.conversations.CreateBridge))
	mux.HandleFunc("GET /comments/{id}/bridges", r.conversations.ListBridges)
	mux.HandleFunc("GET /bridges/{id}", r.conversations.GetBridge)

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
