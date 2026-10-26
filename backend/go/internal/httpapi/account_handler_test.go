package httpapi

import (
	"errors"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/knot/backend/internal/identity"
)

/**
 * The account-lifecycle endpoints.
 *
 * They are tested through the real router, so the protection on DELETE /users/me and
 * the admin-secret check on the purge are exercised as they are wired rather than as
 * they are described.
 */

// newAccountHandlerRouter builds the router around a specific account fake.
func newAccountHandlerRouter(t *testing.T, service *fakeAccountService) http.Handler {
	t.Helper()

	logger := slog.New(slog.NewTextHandler(io.Discard, nil))

	authHandler, err := NewAuthHandler(&fakeAuthService{}, newTestLimiter(t), logger)
	if err != nil {
		t.Fatalf("NewAuthHandler() error = %v, want nil", err)
	}

	testRouter, err := newTestRouterWithAccount(t, logger, authHandler, newTestAccountHandlerWith(t, service, logger))
	if err != nil {
		t.Fatalf("NewRouter() error = %v, want nil", err)
	}

	return testRouter
}

func TestDeleteAccountEndpoint(t *testing.T) {
	t.Run("deletes", func(t *testing.T) {
		service := &fakeAccountService{}
		handler := newAccountHandlerRouter(t, service)

		recorder := doAuthedRequest(t, handler, http.MethodDelete, "/users/me",
			`{"password":"correct horse 7"}`)

		if recorder.Code != http.StatusOK {
			t.Fatalf("status = %d, want %d (body %s)", recorder.Code, http.StatusOK, recorder.Body.String())
		}

		var body successResponse
		decodeBody(t, recorder, &body)
		if !body.Success {
			t.Error("success = false, want true")
		}
		if service.gotUserID != testUserID {
			t.Errorf("service received user %q, want the token's subject %q", service.gotUserID, testUserID)
		}
		if service.gotPassword != "correct horse 7" {
			t.Errorf("service received password %q, want the one from the body", service.gotPassword)
		}
	})

	t.Run("wrong password", func(t *testing.T) {
		handler := newAccountHandlerRouter(t, &fakeAccountService{requestDeletionErr: identity.ErrInvalidCredentials})

		recorder := doAuthedRequest(t, handler, http.MethodDelete, "/users/me", `{"password":"nope"}`)

		if recorder.Code != http.StatusUnauthorized {
			t.Fatalf("status = %d, want %d", recorder.Code, http.StatusUnauthorized)
		}
		if code := decodedErrorCode(t, recorder); code != codeInvalidCreds {
			t.Errorf("error code = %q, want %q", code, codeInvalidCreds)
		}
	})

	t.Run("requires authentication", func(t *testing.T) {
		handler := newAccountHandlerRouter(t, &fakeAccountService{})

		recorder := doRequest(handler, http.MethodDelete, "/users/me", `{"password":"correct horse 7"}`)

		if recorder.Code != http.StatusUnauthorized {
			t.Fatalf("status = %d, want %d", recorder.Code, http.StatusUnauthorized)
		}
		if code := decodedErrorCode(t, recorder); code != codeUnauthorized {
			t.Errorf("error code = %q, want %q", code, codeUnauthorized)
		}
	})

	t.Run("rejects an unknown field", func(t *testing.T) {
		handler := newAccountHandlerRouter(t, &fakeAccountService{})

		recorder := doAuthedRequest(t, handler, http.MethodDelete, "/users/me",
			`{"password":"correct horse 7","reason":"because"}`)

		if recorder.Code != http.StatusBadRequest {
			t.Fatalf("status = %d, want %d", recorder.Code, http.StatusBadRequest)
		}
		if code := decodedErrorCode(t, recorder); code != codeInvalidRequest {
			t.Errorf("error code = %q, want %q", code, codeInvalidRequest)
		}
	})
}

// TestRecoverAccountEndpoint proves recovery needs no session but does need the
// credential — it is the path for somebody who deleted the account from elsewhere.
func TestRecoverAccountEndpoint(t *testing.T) {
	t.Run("recovers", func(t *testing.T) {
		service := &fakeAccountService{}
		handler := newAccountHandlerRouter(t, service)

		recorder := doRequest(handler, http.MethodPost, "/users/me/recover",
			`{"email":"ada@example.com","password":"correct horse 7"}`)

		if recorder.Code != http.StatusOK {
			t.Fatalf("status = %d, want %d (body %s)", recorder.Code, http.StatusOK, recorder.Body.String())
		}
		if service.gotEmail != "ada@example.com" {
			t.Errorf("service received email %q, want the one from the body", service.gotEmail)
		}
	})

	t.Run("wrong password", func(t *testing.T) {
		handler := newAccountHandlerRouter(t, &fakeAccountService{recoverErr: identity.ErrInvalidCredentials})

		recorder := doRequest(handler, http.MethodPost, "/users/me/recover",
			`{"email":"ada@example.com","password":"nope"}`)

		if recorder.Code != http.StatusUnauthorized {
			t.Fatalf("status = %d, want %d", recorder.Code, http.StatusUnauthorized)
		}
	})

	t.Run("past the window is gone", func(t *testing.T) {
		handler := newAccountHandlerRouter(t, &fakeAccountService{recoverErr: identity.ErrAccountPendingPurge})

		recorder := doRequest(handler, http.MethodPost, "/users/me/recover",
			`{"email":"ada@example.com","password":"correct horse 7"}`)

		if recorder.Code != http.StatusGone {
			t.Fatalf("status = %d, want %d", recorder.Code, http.StatusGone)
		}
		if code := decodedErrorCode(t, recorder); code != codeAccountPendingPurge {
			t.Errorf("error code = %q, want %q", code, codeAccountPendingPurge)
		}
	})
}

// TestPurgeDeletedEndpoint proves the operator endpoint is unreachable without the
// secret and reports what it purged with it.
func TestPurgeDeletedEndpoint(t *testing.T) {
	post := func(t *testing.T, handler http.Handler, secret string) *httptest.ResponseRecorder {
		t.Helper()

		request := httptest.NewRequest(http.MethodPost, "/admin/purge-deleted", nil)
		request.Header.Set("Content-Type", "application/json")
		if secret != "" {
			request.Header.Set(adminSecretHeader, secret)
		}

		recorder := httptest.NewRecorder()
		handler.ServeHTTP(recorder, request)

		return recorder
	}

	t.Run("purges with the secret", func(t *testing.T) {
		service := &fakeAccountService{purged: 3}
		handler := newAccountHandlerRouter(t, service)

		result := post(t, handler, testAdminSecret)

		if result.Code != http.StatusOK {
			t.Fatalf("status = %d, want %d (body %s)", result.Code, http.StatusOK, result.Body.String())
		}
		if !strings.Contains(result.Body.String(), `"purged":3`) {
			t.Errorf("body = %s, want purged 3", result.Body.String())
		}
		if service.purgeCalls != 1 {
			t.Errorf("purge calls = %d, want 1", service.purgeCalls)
		}
	})

	t.Run("refuses a missing secret", func(t *testing.T) {
		service := &fakeAccountService{purged: 3}
		handler := newAccountHandlerRouter(t, service)

		result := post(t, handler, "")

		if result.Code != http.StatusUnauthorized {
			t.Fatalf("status = %d, want %d", result.Code, http.StatusUnauthorized)
		}
		if service.purgeCalls != 0 {
			t.Error("the purge ran without a secret")
		}
	})

	t.Run("refuses a wrong secret", func(t *testing.T) {
		service := &fakeAccountService{purged: 3}
		handler := newAccountHandlerRouter(t, service)

		result := post(t, handler, testAdminSecret+"-wrong")

		if result.Code != http.StatusUnauthorized {
			t.Fatalf("status = %d, want %d", result.Code, http.StatusUnauthorized)
		}
		if service.purgeCalls != 0 {
			t.Error("the purge ran with a wrong secret")
		}
	})

	t.Run("does not leak the secret", func(t *testing.T) {
		handler := newAccountHandlerRouter(t, &fakeAccountService{})

		result := post(t, handler, "guess")

		if strings.Contains(result.Body.String(), testAdminSecret) || strings.Contains(result.Body.String(), "guess") {
			t.Errorf("the refusal echoed a secret: %s", result.Body.String())
		}
	})

	t.Run("a failed purge is a server error", func(t *testing.T) {
		handler := newAccountHandlerRouter(t, &fakeAccountService{purgeErr: errors.New("database on fire")})

		result := post(t, handler, testAdminSecret)

		if result.Code != http.StatusInternalServerError {
			t.Fatalf("status = %d, want %d", result.Code, http.StatusInternalServerError)
		}
		if strings.Contains(result.Body.String(), "database on fire") {
			t.Errorf("response leaked internal detail: %s", result.Body.String())
		}
	})
}

// TestNewAccountHandlerRejectsMissingDependencies covers the constructor's guards,
// including the empty secret that would make the purge endpoint unguarded.
func TestNewAccountHandlerRejectsMissingDependencies(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	limiter := newTestLimiter(t)

	if _, err := NewAccountHandler(nil, limiter, testAdminSecret, logger); err == nil {
		t.Error("NewAccountHandler(nil, ...) error = nil, want an error")
	}
	if _, err := NewAccountHandler(&fakeAccountService{}, nil, testAdminSecret, logger); err == nil {
		t.Error("NewAccountHandler(_, nil limiter, ...) error = nil, want an error")
	}
	if _, err := NewAccountHandler(&fakeAccountService{}, limiter, "", logger); err == nil {
		t.Error("NewAccountHandler(_, _, \"\", ...) error = nil, want an error")
	}
	if _, err := NewAccountHandler(&fakeAccountService{}, limiter, testAdminSecret, nil); err == nil {
		t.Error("NewAccountHandler(_, _, _, nil) error = nil, want an error")
	}
}

// TestRecoverAccountIsRateLimited proves recovery cannot be used as an unthrottled
// password oracle: it is a public route that checks a credential, so it is counted
// against the same limits as sign-in.
func TestRecoverAccountIsRateLimited(t *testing.T) {
	handler := newAccountHandlerRouter(t, &fakeAccountService{recoverErr: identity.ErrInvalidCredentials})

	var last *httptest.ResponseRecorder
	for attempt := 1; attempt <= 11; attempt++ {
		last = doRequest(handler, http.MethodPost, "/users/me/recover",
			`{"email":"ada@example.com","password":"wrong"}`)

		if attempt <= 10 && last.Code != http.StatusUnauthorized {
			t.Fatalf("attempt %d status = %d, want %d (body %s)",
				attempt, last.Code, http.StatusUnauthorized, last.Body.String())
		}
	}

	if last.Code != http.StatusTooManyRequests {
		t.Fatalf("eleventh attempt status = %d, want %d", last.Code, http.StatusTooManyRequests)
	}
	if last.Header().Get("Retry-After") == "" {
		t.Error("Retry-After is empty, want the window in seconds")
	}

	var body errorResponse
	decodeBody(t, last, &body)
	if body.Error.Code != codeRateLimited {
		t.Errorf("code = %q, want %q", body.Error.Code, codeRateLimited)
	}
}
