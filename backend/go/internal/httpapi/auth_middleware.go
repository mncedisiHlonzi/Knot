package httpapi

import (
	"context"
	"fmt"
	"log/slog"
	"net/http"
	"strings"

	"github.com/knot/backend/internal/identity"
)

// userIDContextKey is the context key under which the authenticated user id is
// stored. It is a private struct type so no other package can collide with it,
// matching the requestIDKey convention in middleware.go.
type userIDContextKey struct{}

// UserIDFromContext returns the authenticated user's id, which is canonical UUID
// text.
//
// The second result is false when no authenticated user is on the context — for
// example on a public route, or if a handler is reached without the auth
// middleware. Handlers must treat false as unauthenticated rather than as an
// empty user, so a missing credential can never be mistaken for a valid one.
func UserIDFromContext(ctx context.Context) (string, bool) {
	userID, ok := ctx.Value(userIDContextKey{}).(string)
	if !ok || userID == "" {
		return "", false
	}
	return userID, true
}

// withUserID returns ctx carrying userID as the authenticated user.
func withUserID(ctx context.Context, userID string) context.Context {
	return context.WithValue(ctx, userIDContextKey{}, userID)
}

// AccessTokenParser is the slice of identity.TokenIssuer that the middleware
// needs. Depending on an interface (rather than the concrete issuer) keeps
// middleware tests free of real JWT signing material.
type AccessTokenParser interface {
	// ParseAccessToken validates a bearer access token and returns its claims.
	ParseAccessToken(raw string) (*identity.Claims, error)
}

// AuthMiddleware authenticates requests that must carry a bearer access token.
//
// It is applied per route rather than globally: public routes such as
// GET /stories must stay reachable without credentials. Only the routes wrapped
// with Require see a user id on the context.
type AuthMiddleware struct {
	tokens AccessTokenParser
	logger *slog.Logger
}

// NewAuthMiddleware returns middleware backed by tokens.
func NewAuthMiddleware(tokens AccessTokenParser, logger *slog.Logger) (*AuthMiddleware, error) {
	if tokens == nil {
		return nil, fmt.Errorf("httpapi: auth middleware requires a token parser")
	}
	if logger == nil {
		return nil, fmt.Errorf("httpapi: auth middleware requires a logger")
	}
	return &AuthMiddleware{tokens: tokens, logger: logger}, nil
}

// Require wraps next so that it only runs for a request carrying a valid bearer
// access token. On success the user id from the token's subject claim is placed
// on the request context, where handlers read it with UserIDFromContext.
//
// Every failure — absent header, wrong scheme, empty token, malformed token,
// expired token, refresh token presented as an access token — produces the same
// 401 body. Distinguishing them would tell an attacker which part of a guess was
// right, and the client's remedy is identical in all cases.
func (m *AuthMiddleware) Require(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		raw, ok := bearerToken(r)
		if !ok {
			m.reject(w, r, "missing or malformed Authorization header")
			return
		}

		claims, err := m.tokens.ParseAccessToken(raw)
		if err != nil {
			m.reject(w, r, err.Error())
			return
		}

		// A correctly signed token always carries a subject, so an empty one
		// means the token was not issued by this service's issuer.
		if claims == nil || claims.Subject == "" {
			m.reject(w, r, "access token has no subject")
			return
		}

		next(w, r.WithContext(withUserID(r.Context(), claims.Subject)))
	}
}

// Optional authenticates a public route without requiring a credential.
//
// A request that carries a valid bearer token gains the user id on its context,
// exactly as Require would set it; a request with no token, or with a token that
// does not validate, proceeds anonymously (UserIDFromContext then reports false).
// It exists so a public read can still be personalised — for example, so
// GET /stories can include the caller's own reactions — without turning the route
// into a protected one (KNOT-ADR-051).
func (m *AuthMiddleware) Optional(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		raw, ok := bearerToken(r)
		if !ok {
			next(w, r)
			return
		}

		claims, err := m.tokens.ParseAccessToken(raw)
		if err != nil || claims == nil || claims.Subject == "" {
			// A public route does not refuse a bad credential; it serves the
			// anonymous view rather than a 401.
			next(w, r)
			return
		}

		next(w, r.WithContext(withUserID(r.Context(), claims.Subject)))
	}
}

// reject logs the reason and writes the uniform 401 response. The reason is
// logged, never returned: the response body is identical for every cause.
func (m *AuthMiddleware) reject(w http.ResponseWriter, r *http.Request, reason string) {
	m.logger.WarnContext(
		r.Context(),
		"rejected unauthenticated request",
		slog.String("request_id", RequestIDFromContext(r.Context())),
		slog.String("path", r.URL.Path),
		slog.String("reason", reason),
	)
	writeError(w, http.StatusUnauthorized, codeUnauthorized, "authentication required")
}

// bearerToken extracts the token from an "Authorization: Bearer <token>" header.
//
// The scheme is compared case-insensitively because RFC 7235 defines it that
// way. Exactly one token is expected: "Bearer a b" is rejected rather than
// silently truncated.
func bearerToken(r *http.Request) (string, bool) {
	header := r.Header.Get("Authorization")
	if header == "" {
		return "", false
	}

	scheme, token, found := strings.Cut(header, " ")
	if !found || !strings.EqualFold(scheme, "Bearer") {
		return "", false
	}

	token = strings.TrimSpace(token)
	if token == "" || strings.ContainsAny(token, " \t") {
		return "", false
	}

	return token, true
}
