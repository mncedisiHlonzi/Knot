package httpapi

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"time"

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
)

// AuthService is the slice of the identity service that the HTTP layer needs.
// Depending on an interface (rather than the concrete service) keeps handler
// tests free of a database.
type AuthService interface {
	Register(ctx context.Context, in identity.RegisterInput) (*identity.AuthResult, error)
	Login(ctx context.Context, in identity.LoginInput) (*identity.AuthResult, error)
}

// AuthHandler serves the registration and login endpoints.
type AuthHandler struct {
	service AuthService
	logger  *slog.Logger
}

// NewAuthHandler returns a handler backed by service.
func NewAuthHandler(service AuthService, logger *slog.Logger) (*AuthHandler, error) {
	if service == nil {
		return nil, fmt.Errorf("httpapi: auth handler requires a service")
	}
	if logger == nil {
		return nil, fmt.Errorf("httpapi: auth handler requires a logger")
	}
	return &AuthHandler{service: service, logger: logger}, nil
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

// loginRequest is the POST /auth/login body.
type loginRequest struct {
	Email    string `json:"email"`
	Password string `json:"password"`
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
	// AvatarURL is the path on this API that serves the user's avatar, or "" when
	// the user has none. It is never a link to object storage: the bucket is
	// private and every byte reaches the client through this backend
	// (KNOT-ADR-029).
	AvatarURL string `json:"avatar_url"`
}

// authResponse is returned by both register and login.
type authResponse struct {
	User         userResponse `json:"user"`
	AccessToken  string       `json:"access_token"`
	RefreshToken string       `json:"refresh_token"`
	ExpiresIn    int          `json:"expires_in"`
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
func (h *AuthHandler) Register(w http.ResponseWriter, r *http.Request) {
	var body registerRequest
	if err := decodeJSON(w, r, &body); err != nil {
		h.writeDecodeError(w, r, err)
		return
	}

	result, err := h.service.Register(r.Context(), identity.RegisterInput{
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

	writeJSON(w, http.StatusCreated, newAuthResponse(result))
}

// Login handles POST /auth/login.
func (h *AuthHandler) Login(w http.ResponseWriter, r *http.Request) {
	var body loginRequest
	if err := decodeJSON(w, r, &body); err != nil {
		h.writeDecodeError(w, r, err)
		return
	}

	result, err := h.service.Login(r.Context(), identity.LoginInput{
		Email:    body.Email,
		Password: body.Password,
	})
	if err != nil {
		h.writeServiceError(w, r, err)
		return
	}

	writeJSON(w, http.StatusOK, newAuthResponse(result))
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
	case errors.Is(err, identity.ErrInvalidCredentials):
		writeError(w, http.StatusUnauthorized, codeInvalidCreds, "invalid credentials")
	default:
		h.logger.ErrorContext(
			r.Context(),
			"identity request failed",
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

// newAuthResponse projects a domain result onto the wire format.
func newAuthResponse(result *identity.AuthResult) authResponse {
	return authResponse{
		User:         newUserResponse(result.User),
		AccessToken:  result.Tokens.AccessToken,
		RefreshToken: result.Tokens.RefreshToken,
		ExpiresIn:    result.Tokens.ExpiresIn,
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
