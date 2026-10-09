package httpapi

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/knot/backend/internal/identity"
)

// fakeAuthService records the input it received and returns canned results, so
// handler behaviour can be tested without a database.
type fakeAuthService struct {
	registerResult *identity.AuthResult
	registerErr    error
	loginResult    *identity.AuthResult
	loginErr       error

	gotRegister identity.RegisterInput
	gotLogin    identity.LoginInput
}

func (f *fakeAuthService) Register(_ context.Context, in identity.RegisterInput) (*identity.AuthResult, error) {
	f.gotRegister = in
	return f.registerResult, f.registerErr
}

func (f *fakeAuthService) Login(_ context.Context, in identity.LoginInput) (*identity.AuthResult, error) {
	f.gotLogin = in
	return f.loginResult, f.loginErr
}

// sampleResult is a well-formed successful auth result.
func sampleResult() *identity.AuthResult {
	return &identity.AuthResult{
		User: &identity.User{
			ID:                  "11111111-1111-4111-8111-111111111111",
			Email:               "ada@example.com",
			DisplayName:         "Ada Lovelace",
			PreferredLanguages:  []string{"en", "fr"},
			ApproximateLocation: "Cape Town",
			Phone:               "+27000000000",
			PasswordHash:        "$argon2id$v=19$m=65536,t=1,p=4$c2FsdA$c2VjcmV0",
			CreatedAt:           time.Date(2026, 10, 7, 12, 0, 0, 0, time.UTC),
		},
		Tokens: identity.TokenPair{
			AccessToken:  "access-token-value",
			RefreshToken: "refresh-token-value",
			ExpiresIn:    900,
		},
	}
}

func newTestHandler(t *testing.T, service AuthService) http.Handler {
	t.Helper()

	logger := slog.New(slog.NewTextHandler(io.Discard, nil))

	authHandler, err := NewAuthHandler(service, logger)
	if err != nil {
		t.Fatalf("NewAuthHandler() error = %v, want nil", err)
	}

	testRouter, err := newTestRouter(t, logger, authHandler)
	if err != nil {
		t.Fatalf("NewRouter() error = %v, want nil", err)
	}

	return testRouter
}

// newTestRouter assembles the router the way cmd/knot does, so the identity
// request tests run against the real route table and middleware chain.
func newTestRouter(t *testing.T, logger *slog.Logger, authHandler *AuthHandler) (http.Handler, error) {
	t.Helper()

	storiesHandler, err := NewStoriesHandler(&fakeStoriesService{}, &fakeRootedService{}, &fakeStoryMediaLookup{}, logger)
	if err != nil {
		t.Fatalf("NewStoriesHandler() error = %v, want nil", err)
	}

	versionsHandler, err := NewVersionsHandler(&fakeVersionsService{}, &fakeRootedService{}, logger)
	if err != nil {
		t.Fatalf("NewVersionsHandler() error = %v, want nil", err)
	}

	conversationsHandler, err := NewConversationsHandler(&fakeConversationsService{}, &fakeRootedService{}, logger)
	if err != nil {
		t.Fatalf("NewConversationsHandler() error = %v, want nil", err)
	}

	rootedHandler, err := NewRootedHandler(&fakeRootedService{}, logger)
	if err != nil {
		t.Fatalf("NewRootedHandler() error = %v, want nil", err)
	}

	discoveryHandler, err := NewDiscoveryHandler(fakeDiscoveryService{}, &fakeRootedService{}, logger)
	if err != nil {
		t.Fatalf("NewDiscoveryHandler() error = %v, want nil", err)
	}

	authMiddleware, err := NewAuthMiddleware(&fakeTokenParser{subject: testUserID}, logger)
	if err != nil {
		t.Fatalf("NewAuthMiddleware() error = %v, want nil", err)
	}

	router, err := NewRouter(authHandler, storiesHandler, versionsHandler, conversationsHandler, rootedHandler, discoveryHandler, newTestAvatarHandler(t, logger), newTestStoryMediaHandler(t, logger), authMiddleware, "0.1.0", logger)
	if err != nil {
		return nil, err
	}

	return router.Handler(), nil
}

func doRequest(handler http.Handler, method, path, body string) *httptest.ResponseRecorder {
	var reader io.Reader
	if body != "" {
		reader = strings.NewReader(body)
	}

	request := httptest.NewRequest(method, path, reader)
	request.Header.Set("Content-Type", "application/json")

	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, request)

	return recorder
}

func decodeBody(t *testing.T, recorder *httptest.ResponseRecorder, dst any) {
	t.Helper()
	if err := json.Unmarshal(recorder.Body.Bytes(), dst); err != nil {
		t.Fatalf("decoding response body %q: %v", recorder.Body.String(), err)
	}
}

func decodedErrorCode(t *testing.T, recorder *httptest.ResponseRecorder) string {
	t.Helper()

	var body errorResponse
	decodeBody(t, recorder, &body)
	return body.Error.Code
}

func TestHealth(t *testing.T) {
	handler := newTestHandler(t, &fakeAuthService{})

	recorder := doRequest(handler, http.MethodGet, "/health", "")

	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", recorder.Code, http.StatusOK)
	}
	if contentType := recorder.Header().Get("Content-Type"); !strings.HasPrefix(contentType, "application/json") {
		t.Errorf("content type = %q, want application/json", contentType)
	}

	var body healthResponse
	decodeBody(t, recorder, &body)

	if body.Status != "ok" {
		t.Errorf("status field = %q, want %q", body.Status, "ok")
	}
	if body.Version != "0.1.0" {
		t.Errorf("version = %q, want %q", body.Version, "0.1.0")
	}
	if _, err := time.Parse(time.RFC3339, body.Time); err != nil {
		t.Errorf("time = %q, want RFC3339: %v", body.Time, err)
	}
}

func TestRegisterHappyPath(t *testing.T) {
	service := &fakeAuthService{registerResult: sampleResult()}
	handler := newTestHandler(t, service)

	recorder := doRequest(handler, http.MethodPost, "/auth/register", `{
		"email": "ada@example.com",
		"password": "correct horse battery staple",
		"display_name": "Ada Lovelace",
		"preferred_languages": ["en", "fr"],
		"approximate_location": "Cape Town",
		"phone": "+27000000000"
	}`)

	if recorder.Code != http.StatusCreated {
		t.Fatalf("status = %d, want %d (body %s)", recorder.Code, http.StatusCreated, recorder.Body.String())
	}

	var body authResponse
	decodeBody(t, recorder, &body)

	if body.User.Email != "ada@example.com" {
		t.Errorf("user.email = %q, want %q", body.User.Email, "ada@example.com")
	}
	if body.User.DisplayName != "Ada Lovelace" {
		t.Errorf("user.display_name = %q, want %q", body.User.DisplayName, "Ada Lovelace")
	}
	if body.AccessToken != "access-token-value" {
		t.Errorf("access_token = %q, want the issued token", body.AccessToken)
	}
	if body.RefreshToken != "refresh-token-value" {
		t.Errorf("refresh_token = %q, want the issued token", body.RefreshToken)
	}
	if body.ExpiresIn != 900 {
		t.Errorf("expires_in = %d, want 900", body.ExpiresIn)
	}

	// The service must receive exactly what the client sent.
	if service.gotRegister.Email != "ada@example.com" {
		t.Errorf("service received email %q, want %q", service.gotRegister.Email, "ada@example.com")
	}
	if len(service.gotRegister.PreferredLanguages) != 2 {
		t.Errorf("service received %v, want two languages", service.gotRegister.PreferredLanguages)
	}
}

// TestRegisterResponseNeverLeaksPasswordHash is the critical projection check.
func TestRegisterResponseNeverLeaksPasswordHash(t *testing.T) {
	handler := newTestHandler(t, &fakeAuthService{registerResult: sampleResult()})

	recorder := doRequest(handler, http.MethodPost, "/auth/register", `{
		"email": "ada@example.com",
		"password": "correct horse battery staple",
		"display_name": "Ada Lovelace"
	}`)

	raw := recorder.Body.String()

	if strings.Contains(raw, "password_hash") {
		t.Errorf("response mentions password_hash: %s", raw)
	}
	if strings.Contains(raw, "argon2id") {
		t.Errorf("response contains an argon2id hash: %s", raw)
	}
	if strings.Contains(raw, "correct horse battery staple") {
		t.Errorf("response contains the plaintext password: %s", raw)
	}
}

func TestRegisterEmitsEmptyLanguageArrayNotNull(t *testing.T) {
	result := sampleResult()
	result.User.PreferredLanguages = nil
	handler := newTestHandler(t, &fakeAuthService{registerResult: result})

	recorder := doRequest(handler, http.MethodPost, "/auth/register", `{
		"email": "ada@example.com",
		"password": "correct horse battery staple",
		"display_name": "Ada Lovelace"
	}`)

	if !strings.Contains(recorder.Body.String(), `"preferred_languages":[]`) {
		t.Errorf("body = %s, want an empty array rather than null", recorder.Body.String())
	}
}

func TestRegisterValidationFailure(t *testing.T) {
	service := &fakeAuthService{
		registerErr: &identity.ValidationError{Field: "email", Message: "must be a valid email address"},
	}
	handler := newTestHandler(t, service)

	recorder := doRequest(handler, http.MethodPost, "/auth/register", `{
		"email": "not-an-email",
		"password": "correct horse battery staple",
		"display_name": "Ada Lovelace"
	}`)

	if recorder.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want %d", recorder.Code, http.StatusBadRequest)
	}
	if code := decodedErrorCode(t, recorder); code != codeValidation {
		t.Errorf("error code = %q, want %q", code, codeValidation)
	}
}

func TestRegisterDuplicateEmail(t *testing.T) {
	handler := newTestHandler(t, &fakeAuthService{registerErr: identity.ErrEmailTaken})

	recorder := doRequest(handler, http.MethodPost, "/auth/register", `{
		"email": "ada@example.com",
		"password": "correct horse battery staple",
		"display_name": "Ada Lovelace"
	}`)

	if recorder.Code != http.StatusConflict {
		t.Fatalf("status = %d, want %d", recorder.Code, http.StatusConflict)
	}
	if code := decodedErrorCode(t, recorder); code != codeEmailTaken {
		t.Errorf("error code = %q, want %q", code, codeEmailTaken)
	}
}

func TestRegisterUnexpectedFailureIsInternalError(t *testing.T) {
	handler := newTestHandler(t, &fakeAuthService{registerErr: errors.New("database on fire")})

	recorder := doRequest(handler, http.MethodPost, "/auth/register", `{
		"email": "ada@example.com",
		"password": "correct horse battery staple",
		"display_name": "Ada Lovelace"
	}`)

	if recorder.Code != http.StatusInternalServerError {
		t.Fatalf("status = %d, want %d", recorder.Code, http.StatusInternalServerError)
	}
	if code := decodedErrorCode(t, recorder); code != codeInternal {
		t.Errorf("error code = %q, want %q", code, codeInternal)
	}
	// Internal detail must not reach the client.
	if strings.Contains(recorder.Body.String(), "database on fire") {
		t.Errorf("response leaked internal error detail: %s", recorder.Body.String())
	}
}

func TestRegisterRejectsUnknownJSONField(t *testing.T) {
	handler := newTestHandler(t, &fakeAuthService{registerResult: sampleResult()})

	recorder := doRequest(handler, http.MethodPost, "/auth/register", `{
		"email": "ada@example.com",
		"password": "correct horse battery staple",
		"display_name": "Ada Lovelace",
		"nickname": "ada"
	}`)

	if recorder.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want %d", recorder.Code, http.StatusBadRequest)
	}
	if code := decodedErrorCode(t, recorder); code != codeInvalidRequest {
		t.Errorf("error code = %q, want %q", code, codeInvalidRequest)
	}
}

func TestRegisterRejectsMalformedJSON(t *testing.T) {
	handler := newTestHandler(t, &fakeAuthService{registerResult: sampleResult()})

	recorder := doRequest(handler, http.MethodPost, "/auth/register", `{"email": `)

	if recorder.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want %d", recorder.Code, http.StatusBadRequest)
	}
	if code := decodedErrorCode(t, recorder); code != codeInvalidRequest {
		t.Errorf("error code = %q, want %q", code, codeInvalidRequest)
	}
}

func TestRegisterRejectsOversizedBody(t *testing.T) {
	handler := newTestHandler(t, &fakeAuthService{registerResult: sampleResult()})

	// One byte over the 1 MiB cap.
	oversized := `{"email":"` + strings.Repeat("a", maxRequestBodyBytes) + `"}`

	recorder := doRequest(handler, http.MethodPost, "/auth/register", oversized)

	if recorder.Code != http.StatusRequestEntityTooLarge {
		t.Fatalf("status = %d, want %d", recorder.Code, http.StatusRequestEntityTooLarge)
	}
	if code := decodedErrorCode(t, recorder); code != codeRequestTooLarge {
		t.Errorf("error code = %q, want %q", code, codeRequestTooLarge)
	}
}

func TestRegisterRejectsEmptyBody(t *testing.T) {
	handler := newTestHandler(t, &fakeAuthService{registerResult: sampleResult()})

	recorder := doRequest(handler, http.MethodPost, "/auth/register", "")

	if recorder.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want %d", recorder.Code, http.StatusBadRequest)
	}
}

func TestLoginHappyPath(t *testing.T) {
	service := &fakeAuthService{loginResult: sampleResult()}
	handler := newTestHandler(t, service)

	recorder := doRequest(handler, http.MethodPost, "/auth/login", `{
		"email": "ada@example.com",
		"password": "correct horse battery staple"
	}`)

	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d (body %s)", recorder.Code, http.StatusOK, recorder.Body.String())
	}

	var body authResponse
	decodeBody(t, recorder, &body)

	if body.User.ID != "11111111-1111-4111-8111-111111111111" {
		t.Errorf("user.id = %q, want the sample id", body.User.ID)
	}
	if body.ExpiresIn != 900 {
		t.Errorf("expires_in = %d, want 900", body.ExpiresIn)
	}
	if service.gotLogin.Email != "ada@example.com" {
		t.Errorf("service received email %q, want %q", service.gotLogin.Email, "ada@example.com")
	}
}

func TestLoginInvalidCredentials(t *testing.T) {
	handler := newTestHandler(t, &fakeAuthService{loginErr: identity.ErrInvalidCredentials})

	recorder := doRequest(handler, http.MethodPost, "/auth/login", `{
		"email": "ada@example.com",
		"password": "the-wrong-password"
	}`)

	if recorder.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want %d", recorder.Code, http.StatusUnauthorized)
	}

	var body errorResponse
	decodeBody(t, recorder, &body)

	if body.Error.Code != codeInvalidCreds {
		t.Errorf("error code = %q, want %q", body.Error.Code, codeInvalidCreds)
	}
	// The message must not distinguish "unknown email" from "wrong password".
	if body.Error.Message != "invalid credentials" {
		t.Errorf("error message = %q, want the generic %q", body.Error.Message, "invalid credentials")
	}
}

func TestLoginRejectsUnknownJSONField(t *testing.T) {
	handler := newTestHandler(t, &fakeAuthService{loginResult: sampleResult()})

	recorder := doRequest(handler, http.MethodPost, "/auth/login", `{
		"email": "ada@example.com",
		"password": "correct horse battery staple",
		"remember_me": true
	}`)

	if recorder.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want %d", recorder.Code, http.StatusBadRequest)
	}
	if code := decodedErrorCode(t, recorder); code != codeInvalidRequest {
		t.Errorf("error code = %q, want %q", code, codeInvalidRequest)
	}
}

func TestRequestIDIsEchoed(t *testing.T) {
	handler := newTestHandler(t, &fakeAuthService{})

	request := httptest.NewRequest(http.MethodGet, "/health", nil)
	request.Header.Set("X-Request-ID", "caller-supplied-id")

	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, request)

	if got := recorder.Header().Get("X-Request-ID"); got != "caller-supplied-id" {
		t.Errorf("X-Request-ID = %q, want the caller-supplied value", got)
	}
}

func TestRequestIDIsGeneratedWhenAbsent(t *testing.T) {
	handler := newTestHandler(t, &fakeAuthService{})

	recorder := doRequest(handler, http.MethodGet, "/health", "")

	if got := recorder.Header().Get("X-Request-ID"); got == "" {
		t.Error("X-Request-ID is empty, want a generated id")
	}
}

func TestUnknownRouteIsNotFound(t *testing.T) {
	handler := newTestHandler(t, &fakeAuthService{})

	recorder := doRequest(handler, http.MethodGet, "/nope", "")

	if recorder.Code != http.StatusNotFound {
		t.Errorf("status = %d, want %d", recorder.Code, http.StatusNotFound)
	}
}

func TestWrongMethodIsMethodNotAllowed(t *testing.T) {
	handler := newTestHandler(t, &fakeAuthService{})

	recorder := doRequest(handler, http.MethodGet, "/auth/login", "")

	if recorder.Code != http.StatusMethodNotAllowed {
		t.Errorf("status = %d, want %d", recorder.Code, http.StatusMethodNotAllowed)
	}
}

func TestNewAuthHandlerRejectsMissingDependencies(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))

	if _, err := NewAuthHandler(nil, logger); err == nil {
		t.Error("NewAuthHandler(nil, logger) error = nil, want an error")
	}
	if _, err := NewAuthHandler(&fakeAuthService{}, nil); err == nil {
		t.Error("NewAuthHandler(service, nil) error = nil, want an error")
	}
}

func TestNewRouterRejectsMissingDependencies(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))

	authHandler, err := NewAuthHandler(&fakeAuthService{}, logger)
	if err != nil {
		t.Fatalf("NewAuthHandler() error = %v, want nil", err)
	}
	storiesHandler, err := NewStoriesHandler(&fakeStoriesService{}, &fakeRootedService{}, &fakeStoryMediaLookup{}, logger)
	if err != nil {
		t.Fatalf("NewStoriesHandler() error = %v, want nil", err)
	}
	versionsHandler, err := NewVersionsHandler(&fakeVersionsService{}, &fakeRootedService{}, logger)
	if err != nil {
		t.Fatalf("NewVersionsHandler() error = %v, want nil", err)
	}
	conversationsHandler, err := NewConversationsHandler(&fakeConversationsService{}, &fakeRootedService{}, logger)
	if err != nil {
		t.Fatalf("NewConversationsHandler() error = %v, want nil", err)
	}
	rootedHandler, err := NewRootedHandler(&fakeRootedService{}, logger)
	if err != nil {
		t.Fatalf("NewRootedHandler() error = %v, want nil", err)
	}
	discoveryHandler, err := NewDiscoveryHandler(fakeDiscoveryService{}, &fakeRootedService{}, logger)
	if err != nil {
		t.Fatalf("NewDiscoveryHandler() error = %v, want nil", err)
	}
	avatarHandler := newTestAvatarHandler(t, logger)
	storyMediaHandler := newTestStoryMediaHandler(t, logger)
	authMiddleware, err := NewAuthMiddleware(&fakeTokenParser{subject: testUserID}, logger)
	if err != nil {
		t.Fatalf("NewAuthMiddleware() error = %v, want nil", err)
	}

	if _, err := NewRouter(nil, storiesHandler, versionsHandler, conversationsHandler, rootedHandler, discoveryHandler, avatarHandler, storyMediaHandler, authMiddleware, "0.1.0", logger); err == nil {
		t.Error("NewRouter(nil, ...) error = nil, want an error")
	}
	if _, err := NewRouter(authHandler, nil, versionsHandler, conversationsHandler, rootedHandler, discoveryHandler, avatarHandler, storyMediaHandler, authMiddleware, "0.1.0", logger); err == nil {
		t.Error("NewRouter(_, nil, ...) error = nil, want an error")
	}
	if _, err := NewRouter(authHandler, storiesHandler, nil, conversationsHandler, rootedHandler, discoveryHandler, avatarHandler, storyMediaHandler, authMiddleware, "0.1.0", logger); err == nil {
		t.Error("NewRouter(_, _, nil, ...) error = nil, want an error")
	}
	if _, err := NewRouter(authHandler, storiesHandler, versionsHandler, nil, rootedHandler, discoveryHandler, avatarHandler, storyMediaHandler, authMiddleware, "0.1.0", logger); err == nil {
		t.Error("NewRouter(_, _, _, nil, ...) error = nil, want an error")
	}
	if _, err := NewRouter(authHandler, storiesHandler, versionsHandler, conversationsHandler, nil, discoveryHandler, avatarHandler, storyMediaHandler, authMiddleware, "0.1.0", logger); err == nil {
		t.Error("NewRouter(_, _, _, _, nil, ...) error = nil, want an error")
	}
	if _, err := NewRouter(authHandler, storiesHandler, versionsHandler, conversationsHandler, rootedHandler, nil, avatarHandler, storyMediaHandler, authMiddleware, "0.1.0", logger); err == nil {
		t.Error("NewRouter(_, _, _, _, _, nil, ...) error = nil, want an error")
	}
	if _, err := NewRouter(authHandler, storiesHandler, versionsHandler, conversationsHandler, rootedHandler, discoveryHandler, nil, storyMediaHandler, authMiddleware, "0.1.0", logger); err == nil {
		t.Error("NewRouter(_, _, _, _, _, _, nil, ...) error = nil, want an error")
	}
	if _, err := NewRouter(authHandler, storiesHandler, versionsHandler, conversationsHandler, rootedHandler, discoveryHandler, avatarHandler, nil, authMiddleware, "0.1.0", logger); err == nil {
		t.Error("NewRouter(_, _, _, _, _, _, _, nil, ...) error = nil, want an error")
	}
	if _, err := NewRouter(authHandler, storiesHandler, versionsHandler, conversationsHandler, rootedHandler, discoveryHandler, avatarHandler, storyMediaHandler, nil, "0.1.0", logger); err == nil {
		t.Error("NewRouter(_, _, _, _, _, _, _, _, nil, ...) error = nil, want an error")
	}
	if _, err := NewRouter(authHandler, storiesHandler, versionsHandler, conversationsHandler, rootedHandler, discoveryHandler, avatarHandler, storyMediaHandler, authMiddleware, "0.1.0", nil); err == nil {
		t.Error("NewRouter(..., nil) error = nil, want an error")
	}
}
