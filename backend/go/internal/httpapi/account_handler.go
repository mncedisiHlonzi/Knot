package httpapi

import (
	"context"
	"crypto/subtle"
	"errors"
	"fmt"
	"log/slog"
	"net/http"

	"github.com/knot/backend/internal/auth"
	"github.com/knot/backend/internal/identity"
)

// Machine-readable codes for the account lifecycle.
const (
	// codeAccountDeleted is 403: the account has been deleted and is not usable.
	codeAccountDeleted = "account_deleted"
	// codeAccountPendingPurge is 410: the recovery window has closed, so the account
	// is gone whether or not the purge job has reached it yet.
	codeAccountPendingPurge = "account_pending_purge"
	// codeUnauthorizedAdmin is the uniform answer to a missing or wrong admin secret.
	codeUnauthorizedAdmin = "unauthorized"
)

// adminSecretHeader carries the operator secret for the admin endpoints.
const adminSecretHeader = "X-Knot-Admin-Secret"

// AccountService is the slice of the identity service the account endpoints need.
//
// Depending on an interface (rather than the concrete service) keeps handler tests
// free of a database, and states exactly which three operations the lifecycle
// exposes over HTTP.
type AccountService interface {
	// RequestDeletion soft-deletes the account once the owner has proved the request
	// with their password.
	RequestDeletion(ctx context.Context, userID, password string) error
	// RecoverAccount cancels a pending deletion for the account owning email.
	RecoverAccount(ctx context.Context, email, password string) error
	// PurgeDeletedUsers anonymises every account whose recovery window has closed.
	PurgeDeletedUsers(ctx context.Context) (int, error)
}

// AccountHandler serves the account-lifecycle endpoints: deletion, the explicit
// recovery, and the operator-only purge trigger.
type AccountHandler struct {
	service     AccountService
	limiter     *auth.Limiter
	adminSecret string
	logger      *slog.Logger
}

// NewAccountHandler returns a handler backed by service.
//
// adminSecret is required: the purge endpoint anonymises accounts, and a handler
// without a secret to compare against would have to either refuse every request or
// accept every request. Failing at construction makes that impossible.
//
// limiter is required because recovery verifies a password on a public route: without
// a limiter it would be an unthrottled oracle for guessing one.
func NewAccountHandler(service AccountService, limiter *auth.Limiter, adminSecret string, logger *slog.Logger) (*AccountHandler, error) {
	if service == nil {
		return nil, fmt.Errorf("httpapi: account handler requires a service")
	}
	if limiter == nil {
		return nil, fmt.Errorf("httpapi: account handler requires a rate limiter")
	}
	if adminSecret == "" {
		return nil, fmt.Errorf("httpapi: account handler requires a non-empty admin secret")
	}
	if logger == nil {
		return nil, fmt.Errorf("httpapi: account handler requires a logger")
	}

	return &AccountHandler{service: service, limiter: limiter, adminSecret: adminSecret, logger: logger}, nil
}

// deleteAccountRequest is the DELETE /users/me body.
type deleteAccountRequest struct {
	Password string `json:"password"`
}

// recoverAccountRequest is the POST /users/me/recover body.
type recoverAccountRequest struct {
	Email    string `json:"email"`
	Password string `json:"password"`
}

// purgeResponse is the POST /admin/purge-deleted body.
type purgeResponse struct {
	Purged int `json:"purged"`
}

// DeleteAccount handles DELETE /users/me.
//
// The account is soft-deleted, not erased: its content stays readable and
// attributable, and the person can come back for the recovery window by signing in
// (KNOT-ADR-081). The password is required because this is a destructive action one
// tap away in a settings list.
func (h *AccountHandler) DeleteAccount(w http.ResponseWriter, r *http.Request) {
	userID, ok := UserIDFromContext(r.Context())
	if !ok {
		writeError(w, http.StatusUnauthorized, codeUnauthorized, "authentication required")
		return
	}

	var body deleteAccountRequest
	if err := decodeJSON(w, r, &body); err != nil {
		writeDecodeError(w, r, h.logger, err)
		return
	}

	if err := h.service.RequestDeletion(r.Context(), userID, body.Password); err != nil {
		h.writeAccountError(w, r, err)
		return
	}

	// The other devices are signed out by the caller: this response cannot reach a
	// session whose access token is still valid unless the client revokes its own.
	writeJSON(w, http.StatusOK, successResponse{Success: true})
}

// RecoverAccount handles POST /users/me/recover.
//
// It is deliberately public — no access token — because the person using it may not
// have a session on any device: the whole point is to undo a deletion from somewhere
// else. The email and password are the credential, and both are checked before
// anything is cancelled.
func (h *AccountHandler) RecoverAccount(w http.ResponseWriter, r *http.Request) {
	var body recoverAccountRequest
	if err := decodeJSON(w, r, &body); err != nil {
		writeDecodeError(w, r, h.logger, err)
		return
	}

	// Recovery checks a password on a public route, so it is counted against the same
	// two limits as sign-in and shares their counters: an attacker's budget across
	// login and recovery is one budget, not two.
	if !allowAttempt(w, r, h.limiter, h.logger, auth.LimitLoginByIP, clientIP(r)) {
		return
	}
	if !allowAttempt(w, r, h.limiter, h.logger, auth.LimitLoginByEmail, identity.NormalizeEmail(body.Email)) {
		return
	}

	if err := h.service.RecoverAccount(r.Context(), body.Email, body.Password); err != nil {
		h.writeAccountError(w, r, err)
		return
	}

	writeJSON(w, http.StatusOK, successResponse{Success: true})
}

// PurgeDeletedUsers handles POST /admin/purge-deleted.
//
// It is the MVP trigger for the purge job: nothing runs on a timer yet (that is
// KNOT-034a), so an operator runs this by hand. The secret is compared in constant
// time and is never logged, not even on failure, because the failure path of a
// shared secret is exactly where one leaks.
func (h *AccountHandler) PurgeDeletedUsers(w http.ResponseWriter, r *http.Request) {
	if !h.authorised(r) {
		h.logger.WarnContext(r.Context(),
			"rejected an unauthorised purge request",
			slog.String("request_id", RequestIDFromContext(r.Context())),
		)
		writeError(w, http.StatusUnauthorized, codeUnauthorizedAdmin, "invalid admin secret")
		return
	}

	purged, err := h.service.PurgeDeletedUsers(r.Context())
	if err != nil {
		h.writeAccountError(w, r, err)
		return
	}

	h.logger.InfoContext(r.Context(),
		"purged deleted accounts",
		slog.Int("purged", purged),
		slog.String("request_id", RequestIDFromContext(r.Context())),
	)

	writeJSON(w, http.StatusOK, purgeResponse{Purged: purged})
}

// authorised reports whether the request carries the configured admin secret.
//
// An absent header, an empty one, and a wrong one are all refusals, and
// `subtle.ConstantTimeCompare` is used so the comparison itself does not reveal how
// much of a guess was right.
func (h *AccountHandler) authorised(r *http.Request) bool {
	provided := r.Header.Get(adminSecretHeader)
	if provided == "" {
		return false
	}

	return subtle.ConstantTimeCompare([]byte(provided), []byte(h.adminSecret)) == 1
}

// writeAccountError maps the account-lifecycle errors onto statuses.
func (h *AccountHandler) writeAccountError(w http.ResponseWriter, r *http.Request, err error) {
	var validation *identity.ValidationError

	switch {
	case errors.As(err, &validation):
		writeError(w, http.StatusBadRequest, codeValidation, fmt.Sprintf("%s %s", validation.Field, validation.Message))
	case errors.Is(err, identity.ErrInvalidCredentials):
		writeError(w, http.StatusUnauthorized, codeInvalidCreds, "invalid credentials")
	case errors.Is(err, identity.ErrUserNotFound):
		// The caller is authenticated but the account is gone: the same answer as a
		// credential failure, so nothing is confirmed about an id.
		writeError(w, http.StatusUnauthorized, codeInvalidCreds, "invalid credentials")
	case errors.Is(err, identity.ErrAccountDeleted):
		writeError(w, http.StatusForbidden, codeAccountDeleted, "this account has been deleted")
	case errors.Is(err, identity.ErrAccountPendingPurge):
		writeError(w, http.StatusGone, codeAccountPendingPurge,
			"this account has been deleted and can no longer be recovered")
	default:
		h.logger.ErrorContext(r.Context(),
			"account request failed",
			slog.String("request_id", RequestIDFromContext(r.Context())),
			slog.String("path", r.URL.Path),
			slog.String("error", err.Error()),
		)
		writeError(w, http.StatusInternalServerError, codeInternal, "internal server error")
	}
}
