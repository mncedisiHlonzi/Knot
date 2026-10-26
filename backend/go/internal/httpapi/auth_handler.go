package httpapi

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/knot/backend/internal/auth"
	"github.com/knot/backend/internal/identity"
)

// maxRequestBodyBytes bounds every request body. Anything larger is rejected
// before it can be buffered or parsed.
const maxRequestBodyBytes = 1 << 20 // 1 MiB

// Machine-readable error codes returned in the "error.code" field.
const (
	codeInvalidRequest  = "invalid_request"
	codeRequestTooLarge = "request_too_large"
	codeValidation      = "validation_error"
	codeEmailTaken      = "email_taken"
	codeInvalidCreds    = "invalid_credentials"
	codeInternal        = "internal_error"

	// codeEmailNotVerified is 403: the credentials were right, the account is not
	// usable yet (KNOT-ADR-083).
	codeEmailNotVerified = "email_not_verified"
	// codeInvalidRefreshToken covers both an unusable token and a replayed one.
	// They are the same remedy for the client — sign in again — and telling a
	// thief which of the two it was would tell them whether reuse was detected.
	codeInvalidRefreshToken = "invalid_refresh_token"
	// codeInvalidVerificationToken is a verification link that is unknown, used,
	// or expired.
	codeInvalidVerificationToken = "invalid_verification_token"
	// codeInvalidResetToken is a password reset link that is unknown or expired.
	codeInvalidResetToken = "invalid_reset_token"
	// codeSessionNotFound is a session id that does not exist or does not belong
	// to the caller.
	codeSessionNotFound = "session_not_found"
	// codeRateLimited is 429: the caller must wait Retry-After seconds.
	codeRateLimited = "rate_limited"
)

// AuthService is the slice of the authentication service that the HTTP layer
// needs. Depending on an interface (rather than the concrete service) keeps
// handler tests free of a database, a cache and a clock.
type AuthService interface {
	Register(ctx context.Context, in auth.RegisterInput) (*identity.User, error)
	VerifyEmail(ctx context.Context, token string) error
	Login(ctx context.Context, in auth.LoginInput) (*auth.TokenOutput, error)
	Refresh(ctx context.Context, refreshToken string) (*auth.TokenOutput, error)
	Logout(ctx context.Context, userID, sessionID string) error
	ListSessions(ctx context.Context, userID string) ([]*auth.Session, error)
	RevokeSession(ctx context.Context, userID, sessionID string) error
	ForgotPassword(ctx context.Context, email string) error
	ResetPassword(ctx context.Context, token, newPassword string) error
}

// AuthHandler serves the authentication endpoints: registration, address
// verification, sign-in, refresh, sign-out, the device list, and password reset.
type AuthHandler struct {
	service AuthService
	limiter *auth.Limiter
	logger  *slog.Logger
}

// NewAuthHandler returns a handler backed by service, counting attempts with
// limiter.
//
// The limiter is injected rather than built here so the handler has no opinion
// about where the counters live, and so a test can pass one backed by an
// in-process cache.
func NewAuthHandler(service AuthService, limiter *auth.Limiter, logger *slog.Logger) (*AuthHandler, error) {
	if service == nil {
		return nil, fmt.Errorf("httpapi: auth handler requires a service")
	}
	if limiter == nil {
		return nil, fmt.Errorf("httpapi: auth handler requires a rate limiter")
	}
	if logger == nil {
		return nil, fmt.Errorf("httpapi: auth handler requires a logger")
	}
	return &AuthHandler{service: service, limiter: limiter, logger: logger}, nil
}

// registerRequest is the POST /auth/register body.
type registerRequest struct {
	Email               string   `json:"email"`
	Password            string   `json:"password"`
	DisplayName         string   `json:"display_name"`
	PreferredLanguages  []string `json:"preferred_languages"`
	ApproximateLocation string   `json:"approximate_location"`
	Phone               string   `json:"phone"`
}

// verifyEmailRequest is the POST /auth/verify-email body.
type verifyEmailRequest struct {
	Token string `json:"token"`
}

// loginRequest is the POST /auth/login body.
type loginRequest struct {
	Email    string `json:"email"`
	Password string `json:"password"`
	// DeviceInfo is optional free-form client detail, shown back in the device
	// list. It is never used for an authorisation decision.
	DeviceInfo map[string]any `json:"device_info"`
}

// refreshRequest is the POST /auth/refresh body.
type refreshRequest struct {
	RefreshToken string `json:"refresh_token"`
}

// forgotPasswordRequest is the POST /auth/forgot-password body.
type forgotPasswordRequest struct {
	Email string `json:"email"`
}

// resetPasswordRequest is the POST /auth/reset-password body.
type resetPasswordRequest struct {
	Token       string `json:"token"`
	NewPassword string `json:"new_password"`
}

// successResponse is the body of every endpoint whose only outcome is "done".
type successResponse struct {
	Success bool `json:"success"`
}

// userResponse is the public projection of a user. It is an explicit type, not
// the domain User, so the password hash can never be serialised by accident.
type userResponse struct {
	ID                  string    `json:"id"`
	Email               string    `json:"email"`
	DisplayName         string    `json:"display_name"`
	PreferredLanguages  []string  `json:"preferred_languages"`
	ApproximateLocation string    `json:"approximate_location"`
	Phone               string    `json:"phone"`
	CreatedAt           time.Time `json:"created_at"`
	// EmailVerifiedAt is null until the address is proven (KNOT-ADR-083). It is
	// exposed so the client can explain why sign-in is refused without having to
	// guess from an error code.
	EmailVerifiedAt *time.Time `json:"email_verified_at"`
	// AvatarURL is the path on this API that serves the user's avatar, or "" when
	// the user has none. It is never a link to object storage: the bucket is
	// private and every byte reaches the client through this backend
	// (KNOT-ADR-029).
	AvatarURL string `json:"avatar_url"`
}

// registerResponse is the POST /auth/register body: the account, and no tokens.
type registerResponse struct {
	User userResponse `json:"user"`
}

// tokenResponse is returned by login and refresh.
type tokenResponse struct {
	User         userResponse `json:"user"`
	AccessToken  string       `json:"access_token"`
	RefreshToken string       `json:"refresh_token"`
	ExpiresIn    int          `json:"expires_in"`
	// Recovered is true when this sign-in cancelled a pending account deletion, so a
	// client can welcome the person back instead of pretending nothing happened
	// (KNOT-ADR-081). It is false on refresh, which never recovers anything.
	Recovered bool `json:"recovered"`
}

// sessionResponse is one entry of GET /auth/sessions.
type sessionResponse struct {
	ID         string         `json:"id"`
	DeviceInfo map[string]any `json:"device_info"`
	IPAddress  string         `json:"ip_address"`
	LastUsedAt time.Time      `json:"last_used_at"`
	CreatedAt  time.Time      `json:"created_at"`
	// Current marks the session this request was made from, so a device list can
	// label it instead of offering to revoke the row the user is reading it on.
	Current bool `json:"current"`
}

// sessionsResponse is the GET /auth/sessions body. Sessions is always an array.
type sessionsResponse struct {
	Sessions []sessionResponse `json:"sessions"`
}

// errorBody is the machine-readable half of an error response.
type errorBody struct {
	Code    string `json:"code"`
	Message string `json:"message"`
}

// errorResponse is the shape of every error the API returns.
type errorResponse struct {
	Error errorBody `json:"error"`
}

// Register handles POST /auth/register.
//
// It returns 201 with the account and **no tokens**: an account cannot sign in
// until its address is verified, so issuing a session here would contradict the
// rule the next request would enforce (KNOT-ADR-083).
func (h *AuthHandler) Register(w http.ResponseWriter, r *http.Request) {
	if !h.allow(w, r, auth.LimitRegister, clientIP(r)) {
		return
	}

	var body registerRequest
	if err := decodeJSON(w, r, &body); err != nil {
		h.writeDecodeError(w, r, err)
		return
	}

	user, err := h.service.Register(r.Context(), auth.RegisterInput{
		Email:               body.Email,
		Password:            body.Password,
		DisplayName:         body.DisplayName,
		PreferredLanguages:  body.PreferredLanguages,
		ApproximateLocation: body.ApproximateLocation,
		Phone:               body.Phone,
	})
	if err != nil {
		h.writeServiceError(w, r, err)
		return
	}

	writeJSON(w, http.StatusCreated, registerResponse{User: newUserResponse(user)})
}

// VerifyEmail handles POST /auth/verify-email.
func (h *AuthHandler) VerifyEmail(w http.ResponseWriter, r *http.Request) {
	var body verifyEmailRequest
	if err := decodeJSON(w, r, &body); err != nil {
		h.writeDecodeError(w, r, err)
		return
	}

	if err := h.service.VerifyEmail(r.Context(), body.Token); err != nil {
		h.writeServiceError(w, r, err)
		return
	}

	writeJSON(w, http.StatusOK, successResponse{Success: true})
}

// Login handles POST /auth/login.
//
// Two limits are applied: one per client address, and one per account. The
// per-address limit stops a credential-stuffing run from a single origin; the
// per-account limit stops a distributed one from concentrating on one person.
func (h *AuthHandler) Login(w http.ResponseWriter, r *http.Request) {
	var body loginRequest
	if err := decodeJSON(w, r, &body); err != nil {
		// An unreadable sign-in attempt still counts against the address.
		h.allow(w, r, auth.LimitLoginByIP, clientIP(r))
		h.writeDecodeError(w, r, err)
		return
	}

	if !h.allow(w, r, auth.LimitLoginByIP, clientIP(r)) {
		return
	}
	if !h.allow(w, r, auth.LimitLoginByEmail, identity.NormalizeEmail(body.Email)) {
		return
	}

	result, err := h.service.Login(r.Context(), auth.LoginInput{
		Email:      body.Email,
		Password:   body.Password,
		DeviceInfo: body.DeviceInfo,
		IPAddress:  clientIP(r),
	})
	if err != nil {
		h.writeServiceError(w, r, err)
		return
	}

	writeJSON(w, http.StatusOK, newTokenResponse(result))
}

// Refresh handles POST /auth/refresh.
//
// The limit is keyed by the session the token names, so it is applied before the
// token is validated: a client hammering the endpoint is counted whether or not
// its tokens are real.
func (h *AuthHandler) Refresh(w http.ResponseWriter, r *http.Request) {
	var body refreshRequest
	if err := decodeJSON(w, r, &body); err != nil {
		h.writeDecodeError(w, r, err)
		return
	}

	if sessionID, ok := auth.SplitRefreshToken(strings.TrimSpace(body.RefreshToken)); ok {
		if !h.allow(w, r, auth.LimitRefresh, sessionID) {
			return
		}
	}

	result, err := h.service.Refresh(r.Context(), body.RefreshToken)
	if err != nil {
		h.writeServiceError(w, r, err)
		return
	}

	writeJSON(w, http.StatusOK, newTokenResponse(result))
}

// Logout handles POST /auth/logout.
//
// The session is taken from the access token's own claim, not from the body: a
// caller cannot ask to sign out somebody else's device, because the only session
// id it can name is the one inside the token it presented.
func (h *AuthHandler) Logout(w http.ResponseWriter, r *http.Request) {
	userID, ok := UserIDFromContext(r.Context())
	if !ok {
		writeError(w, http.StatusUnauthorized, codeUnauthorized, "authentication required")
		return
	}

	sessionID, _ := SessionIDFromContext(r.Context())

	if err := h.service.Logout(r.Context(), userID, sessionID); err != nil {
		h.writeServiceError(w, r, err)
		return
	}

	writeJSON(w, http.StatusOK, successResponse{Success: true})
}

// ListSessions handles GET /auth/sessions.
func (h *AuthHandler) ListSessions(w http.ResponseWriter, r *http.Request) {
	userID, ok := UserIDFromContext(r.Context())
	if !ok {
		writeError(w, http.StatusUnauthorized, codeUnauthorized, "authentication required")
		return
	}

	currentSessionID, _ := SessionIDFromContext(r.Context())

	sessions, err := h.service.ListSessions(r.Context(), userID)
	if err != nil {
		h.writeServiceError(w, r, err)
		return
	}

	items := make([]sessionResponse, 0, len(sessions))
	for _, session := range sessions {
		if session == nil {
			continue
		}
		items = append(items, newSessionResponse(session, currentSessionID))
	}

	writeJSON(w, http.StatusOK, sessionsResponse{Sessions: items})
}

// DeleteSession handles DELETE /auth/sessions/{id}.
func (h *AuthHandler) DeleteSession(w http.ResponseWriter, r *http.Request) {
	userID, ok := UserIDFromContext(r.Context())
	if !ok {
		writeError(w, http.StatusUnauthorized, codeUnauthorized, "authentication required")
		return
	}

	if err := h.service.RevokeSession(r.Context(), userID, r.PathValue("id")); err != nil {
		h.writeServiceError(w, r, err)
		return
	}

	writeJSON(w, http.StatusOK, successResponse{Success: true})
}

// ForgotPassword handles POST /auth/forgot-password.
//
// It answers 200 for every input — registered, unregistered, malformed — because
// a different answer for an unregistered address would turn the endpoint into an
// account enumerator (KNOT-ADR-087).
func (h *AuthHandler) ForgotPassword(w http.ResponseWriter, r *http.Request) {
	var body forgotPasswordRequest
	if err := decodeJSON(w, r, &body); err != nil {
		h.writeDecodeError(w, r, err)
		return
	}

	if !h.allow(w, r, auth.LimitForgotPassword, identity.NormalizeEmail(body.Email)) {
		return
	}

	if err := h.service.ForgotPassword(r.Context(), body.Email); err != nil {
		h.writeServiceError(w, r, err)
		return
	}

	writeJSON(w, http.StatusOK, successResponse{Success: true})
}

// ResetPassword handles POST /auth/reset-password.
func (h *AuthHandler) ResetPassword(w http.ResponseWriter, r *http.Request) {
	var body resetPasswordRequest
	if err := decodeJSON(w, r, &body); err != nil {
		h.writeDecodeError(w, r, err)
		return
	}

	if err := h.service.ResetPassword(r.Context(), body.Token, body.NewPassword); err != nil {
		h.writeServiceError(w, r, err)
		return
	}

	writeJSON(w, http.StatusOK, successResponse{Success: true})
}

// allow counts one attempt against limit and, when the limit is exceeded, writes
// the 429 response with Retry-After and reports false.
func (h *AuthHandler) allow(w http.ResponseWriter, r *http.Request, limit auth.Limit, key string) bool {
	return allowAttempt(w, r, h.limiter, h.logger, limit, key)
}

// allowAttempt is the shared body of every rate-limit check in the API. It lives at
// package level because the account handler needs the same behaviour on recovery,
// which verifies a password and so must be counted like sign-in.
//
// Retry-After is the whole window: it is the longest the caller can possibly need
// to wait, so a client that honours it is always correct.
func allowAttempt(w http.ResponseWriter, r *http.Request, limiter *auth.Limiter, logger *slog.Logger, limit auth.Limit, key string) bool {
	result := limiter.Allow(r.Context(), limit, key)
	if result.Allowed {
		return true
	}

	retryAfter := int(result.RetryAfter.Seconds())
	if retryAfter < 1 {
		retryAfter = 1
	}
	w.Header().Set("Retry-After", strconv.Itoa(retryAfter))

	logger.WarnContext(r.Context(),
		"rate limit exceeded",
		slog.String("request_id", RequestIDFromContext(r.Context())),
		slog.String("path", r.URL.Path),
		slog.String("limit", limit.Name),
		slog.Int64("count", result.Count),
	)

	writeError(w, http.StatusTooManyRequests, codeRateLimited,
		fmt.Sprintf("too many attempts; try again in %d seconds", retryAfter))

	return false
}

// writeServiceError maps domain errors onto HTTP status codes. Only errors we
// recognise as safe are described to the client; everything else becomes a
// generic 500 and is logged with the request id.
func (h *AuthHandler) writeServiceError(w http.ResponseWriter, r *http.Request, err error) {
	var validation *identity.ValidationError

	switch {
	case errors.As(err, &validation):
		writeError(w, http.StatusBadRequest, codeValidation, fmt.Sprintf("%s %s", validation.Field, validation.Message))
	case errors.Is(err, identity.ErrEmailTaken):
		writeError(w, http.StatusConflict, codeEmailTaken, "that email is already registered")
	case errors.Is(err, identity.ErrEmailNotVerified):
		writeError(w, http.StatusForbidden, codeEmailNotVerified,
			"verify your email address before signing in")
	case errors.Is(err, identity.ErrInvalidCredentials):
		writeError(w, http.StatusUnauthorized, codeInvalidCreds, "invalid credentials")
	case errors.Is(err, identity.ErrAccountDeleted):
		writeError(w, http.StatusForbidden, codeAccountDeleted, "this account has been deleted")
	case errors.Is(err, identity.ErrAccountPendingPurge):
		writeError(w, http.StatusGone, codeAccountPendingPurge,
			"this account has been deleted and can no longer be recovered")
	case errors.Is(err, auth.ErrInvalidRefreshToken), errors.Is(err, auth.ErrRefreshTokenReused):
		writeError(w, http.StatusUnauthorized, codeInvalidRefreshToken, "sign in again")
	case errors.Is(err, auth.ErrInvalidVerificationToken):
		writeError(w, http.StatusBadRequest, codeInvalidVerificationToken,
			"this verification link is invalid or has expired")
	case errors.Is(err, auth.ErrInvalidPasswordResetToken):
		writeError(w, http.StatusBadRequest, codeInvalidResetToken,
			"this password reset link is invalid or has expired")
	case errors.Is(err, auth.ErrSessionNotFound):
		writeError(w, http.StatusNotFound, codeSessionNotFound, "session not found")
	default:
		h.logger.ErrorContext(
			r.Context(),
			"auth request failed",
			slog.String("request_id", RequestIDFromContext(r.Context())),
			slog.String("path", r.URL.Path),
			slog.String("error", err.Error()),
		)
		writeError(w, http.StatusInternalServerError, codeInternal, "internal server error")
	}
}

// writeDecodeError reports a body that could not be read or parsed. The shared
// helper does the work; the method keeps existing call sites unchanged.
func (h *AuthHandler) writeDecodeError(w http.ResponseWriter, r *http.Request, err error) {
	writeDecodeError(w, r, h.logger, err)
}

// clientIP returns the address the request came from, without its port.
//
// It reads the connection rather than a forwarding header on purpose: the API is
// not behind a proxy today, so nothing sets X-Forwarded-For, and trusting that
// header would let any caller pick its own rate-limit key by inventing one. When
// a proxy is introduced, this is the single place that has to learn which header
// is trustworthy (see docs/SECURITY.md).
func clientIP(r *http.Request) string {
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		return r.RemoteAddr
	}

	return host
}

// newTokenResponse projects a service result onto the wire format.
func newTokenResponse(result *auth.TokenOutput) tokenResponse {
	return tokenResponse{
		User:         newUserResponse(result.User),
		AccessToken:  result.AccessToken,
		RefreshToken: result.RefreshToken,
		ExpiresIn:    result.ExpiresIn,
		Recovered:    result.Recovered,
	}
}

// newSessionResponse projects one session onto the wire format.
func newSessionResponse(session *auth.Session, currentSessionID string) sessionResponse {
	deviceInfo := session.DeviceInfo
	if deviceInfo == nil {
		// Emit {} rather than null so a client can read keys without a guard.
		deviceInfo = map[string]any{}
	}

	return sessionResponse{
		ID:         session.ID,
		DeviceInfo: deviceInfo,
		IPAddress:  session.IPAddress,
		LastUsedAt: session.LastUsedAt,
		CreatedAt:  session.CreatedAt,
		Current:    currentSessionID != "" && session.ID == currentSessionID,
	}
}

// newUserResponse projects a domain user onto the wire format.
//
// PreferredLanguages is always an array, never null. AvatarURL is translated
// from the stored object key into the path this API serves it from, so the key
// never leaves the server.
func newUserResponse(user *identity.User) userResponse {
	if user == nil {
		return userResponse{PreferredLanguages: []string{}}
	}

	languages := user.PreferredLanguages
	if languages == nil {
		// Emit [] rather than null for an empty list.
		languages = []string{}
	}

	return userResponse{
		ID:                  user.ID,
		Email:               user.Email,
		DisplayName:         user.DisplayName,
		PreferredLanguages:  languages,
		ApproximateLocation: user.ApproximateLocation,
		Phone:               user.Phone,
		CreatedAt:           user.CreatedAt,
		EmailVerifiedAt:     user.EmailVerifiedAt,
		AvatarURL:           avatarPathFor(user),
	}
}

// decodeJSON reads exactly one JSON object into dst, with a 1 MiB body cap and
// unknown fields rejected.
func decodeJSON(w http.ResponseWriter, r *http.Request, dst any) error {
	r.Body = http.MaxBytesReader(w, r.Body, maxRequestBodyBytes)

	decoder := json.NewDecoder(r.Body)
	decoder.DisallowUnknownFields()

	if err := decoder.Decode(dst); err != nil {
		return err
	}
	if decoder.More() {
		return errors.New("request body must contain exactly one JSON object")
	}

	return nil
}

// writeJSON writes v as a JSON response with the given status.
func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	// The response has already been given a status code, so a failure here can
	// only be a broken connection: there is nothing useful left to send.
	_ = json.NewEncoder(w).Encode(v)
}

// writeError writes the standard error envelope.
func writeError(w http.ResponseWriter, status int, code, message string) {
	writeJSON(w, status, errorResponse{Error: errorBody{Code: code, Message: message}})
}
