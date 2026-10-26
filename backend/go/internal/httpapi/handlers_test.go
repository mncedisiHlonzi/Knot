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

	"github.com/alicebob/miniredis/v2"

	"github.com/knot/backend/internal/auth"
	"github.com/knot/backend/internal/cache"
	"github.com/knot/backend/internal/identity"
)

// fakeAuthService records the input it received and returns canned results, so
// handler behaviour can be tested without a database, a cache, or a clock.
type fakeAuthService struct {
	registerResult *identity.User
	registerErr    error
	verifyEmailErr error
	loginResult    *auth.TokenOutput
	loginErr       error
	refreshResult  *auth.TokenOutput
	refreshErr     error
	logoutErr      error
	sessionsResult []*auth.Session
	sessionsErr    error
	revokeErr      error
	forgotErr      error
	resetErr       error

	gotRegister    auth.RegisterInput
	gotVerifyToken string
	gotLogin       auth.LoginInput
	gotRefresh     string
	gotLogout      [2]string
	gotListUser    string
	gotRevoke      [2]string
	gotForgot      string
	gotReset       [2]string
}

func (f *fakeAuthService) Register(_ context.Context, in auth.RegisterInput) (*identity.User, error) {
	f.gotRegister = in
	return f.registerResult, f.registerErr
}

func (f *fakeAuthService) VerifyEmail(_ context.Context, token string) error {
	f.gotVerifyToken = token
	return f.verifyEmailErr
}

func (f *fakeAuthService) Login(_ context.Context, in auth.LoginInput) (*auth.TokenOutput, error) {
	f.gotLogin = in
	return f.loginResult, f.loginErr
}

func (f *fakeAuthService) Refresh(_ context.Context, refreshToken string) (*auth.TokenOutput, error) {
	f.gotRefresh = refreshToken
	return f.refreshResult, f.refreshErr
}

func (f *fakeAuthService) Logout(_ context.Context, userID, sessionID string) error {
	f.gotLogout = [2]string{userID, sessionID}
	return f.logoutErr
}

func (f *fakeAuthService) ListSessions(_ context.Context, userID string) ([]*auth.Session, error) {
	f.gotListUser = userID
	return f.sessionsResult, f.sessionsErr
}

func (f *fakeAuthService) RevokeSession(_ context.Context, userID, sessionID string) error {
	f.gotRevoke = [2]string{userID, sessionID}
	return f.revokeErr
}

func (f *fakeAuthService) ForgotPassword(_ context.Context, email string) error {
	f.gotForgot = email
	return f.forgotErr
}

func (f *fakeAuthService) ResetPassword(_ context.Context, token, newPassword string) error {
	f.gotReset = [2]string{token, newPassword}
	return f.resetErr
}

// sampleUser is a well-formed account.
func sampleUser() *identity.User {
	verifiedAt := time.Date(2026, 10, 7, 12, 5, 0, 0, time.UTC)

	return &identity.User{
		ID:                  "11111111-1111-4111-8111-111111111111",
		Email:               "ada@example.com",
		DisplayName:         "Ada Lovelace",
		PreferredLanguages:  []string{"eng", "fra"},
		ApproximateLocation: "Cape Town",
		Phone:               "+27000000000",
		PasswordHash:        "$argon2id$v=19$m=65536,t=1,p=4$c2FsdA$c2VjcmV0",
		CreatedAt:           time.Date(2026, 10, 7, 12, 0, 0, 0, time.UTC),
		EmailVerifiedAt:     &verifiedAt,
	}
}

// sampleTokenOutput is a well-formed successful sign-in.
func sampleTokenOutput() *auth.TokenOutput {
	return &auth.TokenOutput{
		User:         sampleUser(),
		AccessToken:  "access-token-value",
		RefreshToken: "11111111-1111-4111-8111-111111111111.refresh-secret",
		ExpiresIn:    900,
		SessionID:    "11111111-1111-4111-8111-111111111111",
	}
}

// fakeAccountService records the calls it received and returns canned errors, so the
// account endpoints can be tested without a database.
type fakeAccountService struct {
	requestDeletionErr error
	recoverErr         error
	purgeErr           error
	purged             int

	gotUserID   string
	gotPassword string
	gotEmail    string
	purgeCalls  int
}

func (f *fakeAccountService) RequestDeletion(_ context.Context, userID, password string) error {
	f.gotUserID = userID
	f.gotPassword = password
	return f.requestDeletionErr
}

func (f *fakeAccountService) RecoverAccount(_ context.Context, email, password string) error {
	f.gotEmail = email
	f.gotPassword = password
	return f.recoverErr
}

func (f *fakeAccountService) PurgeDeletedUsers(_ context.Context) (int, error) {
	f.purgeCalls++
	return f.purged, f.purgeErr
}

// testAdminSecret is the secret the test account handler is built with.
const testAdminSecret = "test-admin-secret"

// newTestAccountHandler builds an account handler with the test secret.
func newTestAccountHandler(t *testing.T, logger *slog.Logger) *AccountHandler {
	t.Helper()

	return newTestAccountHandlerWith(t, &fakeAccountService{}, logger)
}

// newTestAccountHandlerWith builds an account handler around a specific fake.
func newTestAccountHandlerWith(t *testing.T, service AccountService, logger *slog.Logger) *AccountHandler {
	t.Helper()

	handler, err := NewAccountHandler(service, newTestLimiter(t), testAdminSecret, logger)
	if err != nil {
		t.Fatalf("NewAccountHandler() error = %v, want nil", err)
	}

	return handler
}

// newTestLimiter returns a rate limiter backed by an in-process Redis, so handler
// tests exercise the real counting path without a container.
func newTestLimiter(t *testing.T) *auth.Limiter {
	t.Helper()

	server := miniredis.RunT(t)

	redisCache, err := cache.NewRedisCache(server.Addr(), "", 0)
	if err != nil {
		t.Fatalf("NewRedisCache() error = %v, want nil", err)
	}
	t.Cleanup(func() { _ = redisCache.Close() })

	limiter, err := auth.NewLimiter(redisCache, slog.New(slog.NewTextHandler(io.Discard, nil)))
	if err != nil {
		t.Fatalf("NewLimiter() error = %v, want nil", err)
	}

	return limiter
}

func newTestHandler(t *testing.T, service AuthService) http.Handler {
	t.Helper()

	logger := slog.New(slog.NewTextHandler(io.Discard, nil))

	authHandler, err := NewAuthHandler(service, newTestLimiter(t), logger)
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

	return newTestRouterWithAccount(t, logger, authHandler, newTestAccountHandler(t, logger))
}

// newTestRouterWithAccount assembles the router with a specific account handler, so a
// test can inject a fake before the route table is built.
func newTestRouterWithAccount(t *testing.T, logger *slog.Logger, authHandler *AuthHandler, accountHandler *AccountHandler) (http.Handler, error) {
	t.Helper()

	storiesHandler, err := NewStoriesHandler(&fakeStoriesService{}, &fakeAuthorService{}, &fakeRootedService{}, &fakeStoryMediaLookup{}, &fakeReactionsLookup{}, &fakeStoryStatsLookup{}, &fakeVersionChainLookup{}, &fakeInteractionsLookup{}, logger)
	if err != nil {
		t.Fatalf("NewStoriesHandler() error = %v, want nil", err)
	}

	versionsHandler, err := NewVersionsHandler(&fakeVersionsService{}, &fakeAuthorService{}, &fakeRootedService{}, &fakeReactionsLookup{}, logger)
	if err != nil {
		t.Fatalf("NewVersionsHandler() error = %v, want nil", err)
	}

	conversationsHandler, err := NewConversationsHandler(&fakeConversationsService{}, &fakeAuthorService{}, &fakeRootedService{}, &fakeReactionsLookup{}, logger)
	if err != nil {
		t.Fatalf("NewConversationsHandler() error = %v, want nil", err)
	}

	rootedHandler, err := NewRootedHandler(&fakeRootedService{}, logger)
	if err != nil {
		t.Fatalf("NewRootedHandler() error = %v, want nil", err)
	}

	discoveryHandler := newTestDiscoveryHandler(t, logger, fakeDiscoveryService{})

	authMiddleware, err := NewAuthMiddleware(&fakeTokenParser{subject: testUserID, sessionID: testSessionID}, logger)
	if err != nil {
		t.Fatalf("NewAuthMiddleware() error = %v, want nil", err)
	}

	router, err := NewRouter(authHandler, storiesHandler, versionsHandler, conversationsHandler, rootedHandler, discoveryHandler, newTestAvatarHandler(t, logger), newTestStoryMediaHandler(t, logger), newTestNotificationsHandler(t, logger), newTestProfileHandler(t, logger), newTestReactionsRouterHandler(t, logger), newTestInquiriesHandler(t, logger), newTestModerationHandler(t, logger), newTestSearchHandler(t, logger), accountHandler, authMiddleware, "0.1.0", logger)
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

func TestLanguagesServesTheCanonicalList(t *testing.T) {
	handler := newTestHandler(t, &fakeAuthService{})

	// The route is deliberately public, so no Authorization header is sent: a
	// client needs the list to render a picker before anyone has registered.
	recorder := doRequest(handler, http.MethodGet, "/languages", "")

	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", recorder.Code, http.StatusOK)
	}
	if contentType := recorder.Header().Get("Content-Type"); !strings.HasPrefix(contentType, "application/json") {
		t.Errorf("content type = %q, want application/json", contentType)
	}

	var body languagesResponse
	decodeBody(t, recorder, &body)

	if len(body.Languages) < 7000 {
		t.Fatalf("len(languages) = %d, want at least 7000", len(body.Languages))
	}

	// The list is the contract every language field is validated against, so the
	// shape is checked here too: three lower-case letters, a name, no duplicates.
	seen := make(map[string]string, len(body.Languages))
	previous := ""
	for _, item := range body.Languages {
		if len(item.Code) != 3 {
			t.Errorf("code %q is not three characters", item.Code)
		}
		if item.Code != strings.ToLower(item.Code) {
			t.Errorf("code %q is not lower case", item.Code)
		}
		if item.Name == "" {
			t.Errorf("code %q has an empty name", item.Code)
		}
		if _, duplicate := seen[item.Code]; duplicate {
			t.Errorf("code %q appears more than once", item.Code)
		}
		seen[item.Code] = item.Name

		if previous != "" && item.Name <= previous {
			t.Errorf("names are not sorted: %q came after %q", item.Name, previous)
		}
		previous = item.Name
	}

	// Spot-check the names a client shows for codes the product promises.
	want := map[string]string{
		"eng": "English",
		"zul": "Zulu",
		"afr": "Afrikaans",
		"xho": "Xhosa",
		"fra": "French",
		"por": "Portuguese",
	}
	for code, name := range want {
		if seen[code] != name {
			t.Errorf("languages[%q] = %q, want %q", code, seen[code], name)
		}
	}

	// nso is present now: ISO 639-3 names every language, which is the whole point
	// of moving off ISO 639-1 (KNOT-ADR-046).
	if name, offered := seen["nso"]; !offered {
		t.Error("nso is not offered, want it present: ISO 639-3 names Sepedi")
	} else if name != "Pedi" {
		t.Errorf("languages[%q] = %q, want %q", "nso", name, "Pedi")
	}
}

func TestRegisterHappyPath(t *testing.T) {
	// A fresh account has never proven its address (KNOT-ADR-083).
	registered := sampleUser()
	registered.EmailVerifiedAt = nil

	service := &fakeAuthService{registerResult: registered}
	handler := newTestHandler(t, service)

	recorder := doRequest(handler, http.MethodPost, "/auth/register", `{
		"email": "ada@example.com",
		"password": "correct horse battery staple",
		"display_name": "Ada Lovelace",
		"preferred_languages": ["eng", "fra"],
		"approximate_location": "Cape Town",
		"phone": "+27000000000"
	}`)

	if recorder.Code != http.StatusCreated {
		t.Fatalf("status = %d, want %d (body %s)", recorder.Code, http.StatusCreated, recorder.Body.String())
	}

	var body registerResponse
	decodeBody(t, recorder, &body)

	if body.User.Email != "ada@example.com" {
		t.Errorf("user.email = %q, want %q", body.User.Email, "ada@example.com")
	}
	if body.User.DisplayName != "Ada Lovelace" {
		t.Errorf("user.display_name = %q, want %q", body.User.DisplayName, "Ada Lovelace")
	}
	if body.User.EmailVerifiedAt != nil {
		t.Errorf("user.email_verified_at = %v, want null for a registration", body.User.EmailVerifiedAt)
	}

	// Registration must not hand out a session: an unverified account cannot sign
	// in, so issuing tokens here would contradict the rule (KNOT-ADR-083).
	raw := recorder.Body.String()
	if strings.Contains(raw, "access_token") || strings.Contains(raw, "refresh_token") {
		t.Errorf("registration response contains tokens: %s", raw)
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
	handler := newTestHandler(t, &fakeAuthService{registerResult: sampleUser()})

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
	user := sampleUser()
	user.PreferredLanguages = nil
	handler := newTestHandler(t, &fakeAuthService{registerResult: user})

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
	handler := newTestHandler(t, &fakeAuthService{registerResult: sampleUser()})

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
	handler := newTestHandler(t, &fakeAuthService{registerResult: sampleUser()})

	recorder := doRequest(handler, http.MethodPost, "/auth/register", `{"email": `)

	if recorder.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want %d", recorder.Code, http.StatusBadRequest)
	}
	if code := decodedErrorCode(t, recorder); code != codeInvalidRequest {
		t.Errorf("error code = %q, want %q", code, codeInvalidRequest)
	}
}

func TestRegisterRejectsOversizedBody(t *testing.T) {
	handler := newTestHandler(t, &fakeAuthService{registerResult: sampleUser()})

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
	handler := newTestHandler(t, &fakeAuthService{registerResult: sampleUser()})

	recorder := doRequest(handler, http.MethodPost, "/auth/register", "")

	if recorder.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want %d", recorder.Code, http.StatusBadRequest)
	}
}

func TestLoginHappyPath(t *testing.T) {
	service := &fakeAuthService{loginResult: sampleTokenOutput()}
	handler := newTestHandler(t, service)

	recorder := doRequest(handler, http.MethodPost, "/auth/login", `{
		"email": "ada@example.com",
		"password": "correct horse battery staple"
	}`)

	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d (body %s)", recorder.Code, http.StatusOK, recorder.Body.String())
	}

	var body tokenResponse
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
	handler := newTestHandler(t, &fakeAuthService{loginResult: sampleTokenOutput()})

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
	limiter := newTestLimiter(t)

	if _, err := NewAuthHandler(nil, limiter, logger); err == nil {
		t.Error("NewAuthHandler(nil, limiter, logger) error = nil, want an error")
	}
	if _, err := NewAuthHandler(&fakeAuthService{}, nil, logger); err == nil {
		t.Error("NewAuthHandler(service, nil, logger) error = nil, want an error")
	}
	if _, err := NewAuthHandler(&fakeAuthService{}, limiter, nil); err == nil {
		t.Error("NewAuthHandler(service, limiter, nil) error = nil, want an error")
	}
}

func TestNewRouterRejectsMissingDependencies(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))

	accountHandler := newTestAccountHandler(t, logger)

	authHandler, err := NewAuthHandler(&fakeAuthService{}, newTestLimiter(t), logger)
	if err != nil {
		t.Fatalf("NewAuthHandler() error = %v, want nil", err)
	}
	storiesHandler, err := NewStoriesHandler(&fakeStoriesService{}, &fakeAuthorService{}, &fakeRootedService{}, &fakeStoryMediaLookup{}, &fakeReactionsLookup{}, &fakeStoryStatsLookup{}, &fakeVersionChainLookup{}, &fakeInteractionsLookup{}, logger)
	if err != nil {
		t.Fatalf("NewStoriesHandler() error = %v, want nil", err)
	}
	versionsHandler, err := NewVersionsHandler(&fakeVersionsService{}, &fakeAuthorService{}, &fakeRootedService{}, &fakeReactionsLookup{}, logger)
	if err != nil {
		t.Fatalf("NewVersionsHandler() error = %v, want nil", err)
	}
	conversationsHandler, err := NewConversationsHandler(&fakeConversationsService{}, &fakeAuthorService{}, &fakeRootedService{}, &fakeReactionsLookup{}, logger)
	if err != nil {
		t.Fatalf("NewConversationsHandler() error = %v, want nil", err)
	}
	rootedHandler, err := NewRootedHandler(&fakeRootedService{}, logger)
	if err != nil {
		t.Fatalf("NewRootedHandler() error = %v, want nil", err)
	}
	discoveryHandler := newTestDiscoveryHandler(t, logger, fakeDiscoveryService{})
	avatarHandler := newTestAvatarHandler(t, logger)
	storyMediaHandler := newTestStoryMediaHandler(t, logger)
	notificationsHandler := newTestNotificationsHandler(t, logger)
	profileHandler := newTestProfileHandler(t, logger)
	reactionsHandler := newTestReactionsRouterHandler(t, logger)
	inquiriesHandler := newTestInquiriesHandler(t, logger)
	moderationHandler := newTestModerationHandler(t, logger)
	authMiddleware, err := NewAuthMiddleware(&fakeTokenParser{subject: testUserID, sessionID: testSessionID}, logger)
	if err != nil {
		t.Fatalf("NewAuthMiddleware() error = %v, want nil", err)
	}

	if _, err := NewRouter(nil, storiesHandler, versionsHandler, conversationsHandler, rootedHandler, discoveryHandler, avatarHandler, storyMediaHandler, notificationsHandler, profileHandler, reactionsHandler, inquiriesHandler, moderationHandler, newTestSearchHandler(t, logger), accountHandler, authMiddleware, "0.1.0", logger); err == nil {
		t.Error("NewRouter(nil, ...) error = nil, want an error")
	}
	if _, err := NewRouter(authHandler, nil, versionsHandler, conversationsHandler, rootedHandler, discoveryHandler, avatarHandler, storyMediaHandler, notificationsHandler, profileHandler, reactionsHandler, inquiriesHandler, moderationHandler, newTestSearchHandler(t, logger), accountHandler, authMiddleware, "0.1.0", logger); err == nil {
		t.Error("NewRouter(_, nil, ...) error = nil, want an error")
	}
	if _, err := NewRouter(authHandler, storiesHandler, nil, conversationsHandler, rootedHandler, discoveryHandler, avatarHandler, storyMediaHandler, notificationsHandler, profileHandler, reactionsHandler, inquiriesHandler, moderationHandler, newTestSearchHandler(t, logger), accountHandler, authMiddleware, "0.1.0", logger); err == nil {
		t.Error("NewRouter(_, _, nil, ...) error = nil, want an error")
	}
	if _, err := NewRouter(authHandler, storiesHandler, versionsHandler, nil, rootedHandler, discoveryHandler, avatarHandler, storyMediaHandler, notificationsHandler, profileHandler, reactionsHandler, inquiriesHandler, moderationHandler, newTestSearchHandler(t, logger), accountHandler, authMiddleware, "0.1.0", logger); err == nil {
		t.Error("NewRouter(_, _, _, nil, ...) error = nil, want an error")
	}
	if _, err := NewRouter(authHandler, storiesHandler, versionsHandler, conversationsHandler, nil, discoveryHandler, avatarHandler, storyMediaHandler, notificationsHandler, profileHandler, reactionsHandler, inquiriesHandler, moderationHandler, newTestSearchHandler(t, logger), accountHandler, authMiddleware, "0.1.0", logger); err == nil {
		t.Error("NewRouter(_, _, _, _, nil, ...) error = nil, want an error")
	}
	if _, err := NewRouter(authHandler, storiesHandler, versionsHandler, conversationsHandler, rootedHandler, nil, avatarHandler, storyMediaHandler, notificationsHandler, profileHandler, reactionsHandler, inquiriesHandler, moderationHandler, newTestSearchHandler(t, logger), accountHandler, authMiddleware, "0.1.0", logger); err == nil {
		t.Error("NewRouter(_, _, _, _, _, nil, ...) error = nil, want an error")
	}
	if _, err := NewRouter(authHandler, storiesHandler, versionsHandler, conversationsHandler, rootedHandler, discoveryHandler, nil, storyMediaHandler, notificationsHandler, profileHandler, reactionsHandler, inquiriesHandler, moderationHandler, newTestSearchHandler(t, logger), accountHandler, authMiddleware, "0.1.0", logger); err == nil {
		t.Error("NewRouter(_, _, _, _, _, _, nil, ...) error = nil, want an error")
	}
	if _, err := NewRouter(authHandler, storiesHandler, versionsHandler, conversationsHandler, rootedHandler, discoveryHandler, avatarHandler, nil, notificationsHandler, profileHandler, reactionsHandler, inquiriesHandler, moderationHandler, newTestSearchHandler(t, logger), accountHandler, authMiddleware, "0.1.0", logger); err == nil {
		t.Error("NewRouter(_, _, _, _, _, _, _, nil, ...) error = nil, want an error")
	}
	if _, err := NewRouter(authHandler, storiesHandler, versionsHandler, conversationsHandler, rootedHandler, discoveryHandler, avatarHandler, storyMediaHandler, nil, profileHandler, reactionsHandler, inquiriesHandler, moderationHandler, newTestSearchHandler(t, logger), accountHandler, authMiddleware, "0.1.0", logger); err == nil {
		t.Error("NewRouter(_, _, _, _, _, _, _, _, nil, ...) error = nil, want an error")
	}
	if _, err := NewRouter(authHandler, storiesHandler, versionsHandler, conversationsHandler, rootedHandler, discoveryHandler, avatarHandler, storyMediaHandler, notificationsHandler, nil, reactionsHandler, inquiriesHandler, moderationHandler, newTestSearchHandler(t, logger), accountHandler, authMiddleware, "0.1.0", logger); err == nil {
		t.Error("NewRouter(_, _, _, _, _, _, _, _, _, nil profile, ...) error = nil, want an error")
	}
	if _, err := NewRouter(authHandler, storiesHandler, versionsHandler, conversationsHandler, rootedHandler, discoveryHandler, avatarHandler, storyMediaHandler, notificationsHandler, profileHandler, nil, inquiriesHandler, moderationHandler, newTestSearchHandler(t, logger), accountHandler, authMiddleware, "0.1.0", logger); err == nil {
		t.Error("NewRouter(_, _, _, _, _, _, _, _, _, _, nil reactions, ...) error = nil, want an error")
	}
	if _, err := NewRouter(authHandler, storiesHandler, versionsHandler, conversationsHandler, rootedHandler, discoveryHandler, avatarHandler, storyMediaHandler, notificationsHandler, profileHandler, reactionsHandler, nil, moderationHandler, newTestSearchHandler(t, logger), accountHandler, authMiddleware, "0.1.0", logger); err == nil {
		t.Error("NewRouter(_, _, _, _, _, _, _, _, _, _, _, nil inquiries, ...) error = nil, want an error")
	}
	if _, err := NewRouter(authHandler, storiesHandler, versionsHandler, conversationsHandler, rootedHandler, discoveryHandler, avatarHandler, storyMediaHandler, notificationsHandler, profileHandler, reactionsHandler, inquiriesHandler, nil, newTestSearchHandler(t, logger), accountHandler, authMiddleware, "0.1.0", logger); err == nil {
		t.Error("NewRouter(_, _, _, _, _, _, _, _, _, _, _, _, nil moderation, ...) error = nil, want an error")
	}
	if _, err := NewRouter(authHandler, storiesHandler, versionsHandler, conversationsHandler, rootedHandler, discoveryHandler, avatarHandler, storyMediaHandler, notificationsHandler, profileHandler, reactionsHandler, inquiriesHandler, moderationHandler, nil, newTestAccountHandler(t, logger), authMiddleware, "0.1.0", logger); err == nil {
		t.Error("NewRouter(_, _, _, _, _, _, _, _, _, _, _, _, _, nil search, ...) error = nil, want an error")
	}
	if _, err := NewRouter(authHandler, storiesHandler, versionsHandler, conversationsHandler, rootedHandler, discoveryHandler, avatarHandler, storyMediaHandler, notificationsHandler, profileHandler, reactionsHandler, inquiriesHandler, moderationHandler, newTestSearchHandler(t, logger), nil, authMiddleware, "0.1.0", logger); err == nil {
		t.Error("NewRouter(..., nil account, ...) error = nil, want an error")
	}
	if _, err := NewRouter(authHandler, storiesHandler, versionsHandler, conversationsHandler, rootedHandler, discoveryHandler, avatarHandler, storyMediaHandler, notificationsHandler, profileHandler, reactionsHandler, inquiriesHandler, moderationHandler, newTestSearchHandler(t, logger), newTestAccountHandler(t, logger), nil, "0.1.0", logger); err == nil {
		t.Error("NewRouter(_, _, _, _, _, _, _, _, _, _, _, _, _, _, _, nil auth middleware, ...) error = nil, want an error")
	}
	if _, err := NewRouter(authHandler, storiesHandler, versionsHandler, conversationsHandler, rootedHandler, discoveryHandler, avatarHandler, storyMediaHandler, notificationsHandler, profileHandler, reactionsHandler, inquiriesHandler, moderationHandler, newTestSearchHandler(t, logger), newTestAccountHandler(t, logger), authMiddleware, "0.1.0", nil); err == nil {
		t.Error("NewRouter(..., nil) error = nil, want an error")
	}
}
