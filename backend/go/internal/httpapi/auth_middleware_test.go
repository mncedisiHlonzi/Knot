package httpapi

import (
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/golang-jwt/jwt/v5"

	"github.com/knot/backend/internal/identity"
)

// fakeTokenParser stands in for identity.TokenIssuer so the middleware can be
// tested without real signing material.
type fakeTokenParser struct {
	// subject is the user id reported for any accepted token.
	subject string
	// err, when set, is returned instead of claims.
	err error

	gotToken string
	calls    int
}

func (f *fakeTokenParser) ParseAccessToken(raw string) (*identity.Claims, error) {
	f.calls++
	f.gotToken = raw
	if f.err != nil {
		return nil, f.err
	}
	if raw != testAccessToken {
		return nil, identity.ErrInvalidToken
	}

	return &identity.Claims{RegisteredClaims: jwt.RegisteredClaims{Subject: f.subject}}, nil
}

// capturedUser records what a protected handler saw on the context.
type capturedUser struct {
	id string
	ok bool
}

// protectedHandler wires the middleware in front of a handler that reports the
// user id it was given, which is what the middleware's whole job is.
func protectedHandler(t *testing.T, parser AccessTokenParser) (http.HandlerFunc, *capturedUser, *int) {
	t.Helper()

	logger := slog.New(slog.NewTextHandler(io.Discard, nil))

	middleware, err := NewAuthMiddleware(parser, logger)
	if err != nil {
		t.Fatalf("NewAuthMiddleware() error = %v, want nil", err)
	}

	seen := &capturedUser{}
	innerCalls := 0

	handler := middleware.Require(func(w http.ResponseWriter, r *http.Request) {
		innerCalls++
		seen.id, seen.ok = UserIDFromContext(r.Context())
		writeJSON(w, http.StatusOK, map[string]string{"status": "reached"})
	})

	return handler, seen, &innerCalls
}

func TestRequireRejectsMalformedAuthorizationHeaders(t *testing.T) {
	tests := []struct {
		name   string
		header string
	}{
		{name: "absent", header: ""},
		{name: "wrong scheme", header: "Basic " + testAccessToken},
		{name: "scheme only", header: "Bearer"},
		{name: "empty credentials", header: "Bearer "},
		{name: "extra fields", header: "Bearer one two"},
		{name: "tab separated", header: "Bearer\ttoken"},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			parser := &fakeTokenParser{subject: testUserID}
			inner, _, innerCalls := protectedHandler(t, parser)

			request := httptest.NewRequest(http.MethodPost, "/stories", nil)
			if test.header != "" {
				request.Header.Set("Authorization", test.header)
			}

			recorder := httptest.NewRecorder()
			inner.ServeHTTP(recorder, request)

			if recorder.Code != http.StatusUnauthorized {
				t.Fatalf("status = %d, want %d", recorder.Code, http.StatusUnauthorized)
			}
			if code := decodedErrorCode(t, recorder); code != codeUnauthorized {
				t.Errorf("error code = %q, want %q", code, codeUnauthorized)
			}
			if *innerCalls != 0 {
				t.Error("the protected handler ran, want the request stopped at the middleware")
			}
			if parser.calls != 0 {
				t.Errorf("parser received %d calls, want 0 — an unreadable header needs no verification", parser.calls)
			}
		})
	}
}

func TestRequireRejectsTokenThatFailsVerification(t *testing.T) {
	parser := &fakeTokenParser{err: identity.ErrInvalidToken}
	inner, _, innerCalls := protectedHandler(t, parser)

	recorder := httptest.NewRecorder()
	inner.ServeHTTP(recorder, newBearerRequest(http.MethodPost, "/stories", testAccessToken))

	if recorder.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want %d", recorder.Code, http.StatusUnauthorized)
	}
	if *innerCalls != 0 {
		t.Error("the protected handler ran, want the request stopped at the middleware")
	}
}

func TestRequireRejectsTokenWithoutASubject(t *testing.T) {
	parser := &fakeTokenParser{subject: ""}
	inner, _, innerCalls := protectedHandler(t, parser)

	recorder := httptest.NewRecorder()
	inner.ServeHTTP(recorder, newBearerRequest(http.MethodPost, "/stories", testAccessToken))

	if recorder.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want %d", recorder.Code, http.StatusUnauthorized)
	}
	if *innerCalls != 0 {
		t.Error("the protected handler ran, want a token with no subject to be rejected")
	}
}

func TestRequirePassesTheSubjectToTheHandler(t *testing.T) {
	parser := &fakeTokenParser{subject: testUserID}
	inner, seen, innerCalls := protectedHandler(t, parser)

	recorder := httptest.NewRecorder()
	inner.ServeHTTP(recorder, newBearerRequest(http.MethodPost, "/stories", testAccessToken))

	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", recorder.Code, http.StatusOK)
	}
	if *innerCalls != 1 {
		t.Errorf("handler ran %d times, want exactly 1", *innerCalls)
	}
	if parser.gotToken != testAccessToken {
		t.Errorf("parser received %q, want the raw token with the scheme stripped", parser.gotToken)
	}
	if !seen.ok {
		t.Fatal("UserIDFromContext returned ok = false, want true")
	}
	if seen.id != testUserID {
		t.Errorf("user id = %q, want %q", seen.id, testUserID)
	}
}

func TestRequireIsCaseInsensitiveAboutTheScheme(t *testing.T) {
	parser := &fakeTokenParser{subject: testUserID}
	inner, _, innerCalls := protectedHandler(t, parser)

	request := httptest.NewRequest(http.MethodPost, "/stories", nil)
	request.Header.Set("Authorization", "bearer "+testAccessToken)

	recorder := httptest.NewRecorder()
	inner.ServeHTTP(recorder, request)

	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d — RFC 7235 defines the scheme as case-insensitive", recorder.Code, http.StatusOK)
	}
	if *innerCalls != 1 {
		t.Errorf("handler ran %d times, want exactly 1", *innerCalls)
	}
}

func TestRequireAcceptsARealAccessToken(t *testing.T) {
	// One case runs against the genuine issuer, so the middleware is proven to
	// work with the tokens the server actually mints rather than only with a
	// fake parser that agrees with it.
	const secret = "knot-test-signing-key-at-least-32-bytes-long"

	issuer, err := identity.NewTokenIssuer(secret)
	if err != nil {
		t.Fatalf("NewTokenIssuer() error = %v, want nil", err)
	}

	accessToken, err := issuer.IssueAccessToken(testUserID)
	if err != nil {
		t.Fatalf("IssueAccessToken() error = %v, want nil", err)
	}

	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	middleware, err := NewAuthMiddleware(issuer, logger)
	if err != nil {
		t.Fatalf("NewAuthMiddleware() error = %v, want nil", err)
	}

	var seenUser string
	handler := middleware.Require(func(w http.ResponseWriter, r *http.Request) {
		seenUser, _ = UserIDFromContext(r.Context())
		w.WriteHeader(http.StatusOK)
	})

	recorder := httptest.NewRecorder()
	handler(recorder, newBearerRequest(http.MethodPost, "/stories", accessToken))

	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", recorder.Code, http.StatusOK)
	}
	if seenUser != testUserID {
		t.Errorf("user id = %q, want %q taken from the token subject", seenUser, testUserID)
	}
}

func TestRequireRejectsRefreshTokensAndForeignSecrets(t *testing.T) {
	const secret = "knot-test-signing-key-at-least-32-bytes-long"

	issuer, err := identity.NewTokenIssuer(secret)
	if err != nil {
		t.Fatalf("NewTokenIssuer() error = %v, want nil", err)
	}

	refreshToken, err := issuer.IssueRefreshToken(testUserID)
	if err != nil {
		t.Fatalf("IssueRefreshToken() error = %v, want nil", err)
	}

	foreign, err := identity.NewTokenIssuer("a completely different signing key at least 32 bytes")
	if err != nil {
		t.Fatalf("NewTokenIssuer() error = %v, want nil", err)
	}
	foreignToken, err := foreign.IssueAccessToken(testUserID)
	if err != nil {
		t.Fatalf("IssueAccessToken() error = %v, want nil", err)
	}

	tests := []struct {
		name  string
		token string
	}{
		{name: "refresh token presented as access token", token: refreshToken},
		{name: "access token signed with another secret", token: foreignToken},
		{name: "not a token at all", token: "garbage"},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			logger := slog.New(slog.NewTextHandler(io.Discard, nil))
			middleware, err := NewAuthMiddleware(issuer, logger)
			if err != nil {
				t.Fatalf("NewAuthMiddleware() error = %v, want nil", err)
			}

			innerCalls := 0
			handler := middleware.Require(func(w http.ResponseWriter, _ *http.Request) {
				innerCalls++
				w.WriteHeader(http.StatusOK)
			})

			recorder := httptest.NewRecorder()
			handler(recorder, newBearerRequest(http.MethodPost, "/stories", test.token))

			if recorder.Code != http.StatusUnauthorized {
				t.Fatalf("status = %d, want %d", recorder.Code, http.StatusUnauthorized)
			}
			if innerCalls != 0 {
				t.Error("the protected handler ran, want the request stopped at the middleware")
			}
		})
	}
}

func TestUserIDFromContextOnABareContext(t *testing.T) {
	request := httptest.NewRequest(http.MethodGet, "/stories", nil)

	userID, ok := UserIDFromContext(request.Context())
	if ok {
		t.Error("ok = true, want false when no user is on the context")
	}
	if userID != "" {
		t.Errorf("user id = %q, want an empty string", userID)
	}
}

func TestNewAuthMiddlewareRejectsMissingDependencies(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))

	if _, err := NewAuthMiddleware(nil, logger); err == nil {
		t.Error("NewAuthMiddleware(nil, logger) error = nil, want an error")
	}
	if _, err := NewAuthMiddleware(&fakeTokenParser{subject: testUserID}, nil); err == nil {
		t.Error("NewAuthMiddleware(parser, nil) error = nil, want an error")
	}
}

// newBearerRequest builds a request carrying the given bearer token.
func newBearerRequest(method, path, token string) *http.Request {
	request := httptest.NewRequest(method, path, nil)
	request.Header.Set("Authorization", "Bearer "+token)
	return request
}
