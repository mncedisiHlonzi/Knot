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
type Router struct {
	auth    *AuthHandler
	version string
	logger  *slog.Logger
}

// NewRouter returns the root handler for the API.
func NewRouter(auth *AuthHandler, version string, logger *slog.Logger) (*Router, error) {
	if auth == nil {
		return nil, errNilHandler("auth")
	}
	if logger == nil {
		return nil, errNilHandler("logger")
	}
	return &Router{auth: auth, version: version, logger: logger}, nil
}

// Handler returns the composed http.Handler, middleware included.
func (r *Router) Handler() http.Handler {
	mux := http.NewServeMux()

	// Method-qualified patterns are stdlib ServeMux features as of Go 1.22.
	mux.HandleFunc("GET /health", r.handleHealth)
	mux.HandleFunc("POST /auth/register", r.auth.Register)
	mux.HandleFunc("POST /auth/login", r.auth.Login)

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
