package httpapi

import (
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"

	"github.com/knot/backend/internal/auth"
	"github.com/knot/backend/internal/identity"
)

// testSessionID is the session the test access token names, so the protected auth
// routes act on a session the fake service can recognise.
const testSessionID = "22222222-2222-4222-8222-000000000001"

// doAuthedRequest issues a request carrying the accepted test token, so the
// protected auth routes run through the real middleware.
func doAuthedRequest(t *testing.T, handler http.Handler, method, path, body string) *httptest.ResponseRecorder {
	t.Helper()

	var reader io.Reader
	if body != "" {
		reader = strings.NewReader(body)
	}

	request := httptest.NewRequest(method, path, reader)
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("Authorization", "Bearer "+testAccessToken)

	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, request)

	return recorder
}

func TestVerifyEmailEndpoint(t *testing.T) {
	t.Run("success", func(t *testing.T) {
		service := &fakeAuthService{}
		handler := newTestHandler(t, service)

		recorder := doRequest(handler, http.MethodPost, "/auth/verify-email", `{"token":"abc123"}`)

		if recorder.Code != http.StatusOK {
			t.Fatalf("status = %d, want %d (body %s)", recorder.Code, http.StatusOK, recorder.Body.String())
		}

		var body successResponse
		decodeBody(t, recorder, &body)
		if !body.Success {
			t.Error("success = false, want true")
		}
		if service.gotVerifyToken != "abc123" {
			t.Errorf("service received token %q, want %q", service.gotVerifyToken, "abc123")
		}
	})

	t.Run("invalid token", func(t *testing.T) {
		handler := newTestHandler(t, &fakeAuthService{verifyEmailErr: auth.ErrInvalidVerificationToken})

		recorder := doRequest(handler, http.MethodPost, "/auth/verify-email", `{"token":"nope"}`)

		if recorder.Code != http.StatusBadRequest {
			t.Fatalf("status = %d, want %d", recorder.Code, http.StatusBadRequest)
		}
		if code := decodedErrorCode(t, recorder); code != codeInvalidVerificationToken {
			t.Errorf("error code = %q, want %q", code, codeInvalidVerificationToken)
		}
	})
}

// TestLoginUnverifiedEmail proves the verify-before-login rule surfaces as 403 with
// a code the client can act on (KNOT-ADR-083).
func TestLoginUnverifiedEmail(t *testing.T) {
	handler := newTestHandler(t, &fakeAuthService{loginErr: identity.ErrEmailNotVerified})

	recorder := doRequest(handler, http.MethodPost, "/auth/login", `{
		"email": "ada@example.com",
		"password": "correct horse 7"
	}`)

	if recorder.Code != http.StatusForbidden {
		t.Fatalf("status = %d, want %d", recorder.Code, http.StatusForbidden)
	}
	if code := decodedErrorCode(t, recorder); code != codeEmailNotVerified {
		t.Errorf("error code = %q, want %q", code, codeEmailNotVerified)
	}
}

// TestLoginForwardsDeviceInfo proves the client's device detail reaches the
// service, which is what the device list later shows.
func TestLoginForwardsDeviceInfo(t *testing.T) {
	service := &fakeAuthService{loginResult: sampleTokenOutput()}
	handler := newTestHandler(t, service)

	recorder := doRequest(handler, http.MethodPost, "/auth/login", `{
		"email": "ada@example.com",
		"password": "correct horse 7",
		"device_info": {"platform": "ios", "app_version": "0.1.0"}
	}`)

	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d (body %s)", recorder.Code, http.StatusOK, recorder.Body.String())
	}
	if service.gotLogin.DeviceInfo["platform"] != "ios" {
		t.Errorf("device_info = %v, want platform ios", service.gotLogin.DeviceInfo)
	}
	if service.gotLogin.IPAddress == "" {
		t.Error("the service was given an empty ip address")
	}
}

func TestRefreshEndpoint(t *testing.T) {
	t.Run("rotates", func(t *testing.T) {
		service := &fakeAuthService{refreshResult: sampleTokenOutput()}
		handler := newTestHandler(t, service)

		recorder := doRequest(handler, http.MethodPost, "/auth/refresh", `{
			"refresh_token": "11111111-1111-4111-8111-111111111111.secret"
		}`)

		if recorder.Code != http.StatusOK {
			t.Fatalf("status = %d, want %d (body %s)", recorder.Code, http.StatusOK, recorder.Body.String())
		}

		var body tokenResponse
		decodeBody(t, recorder, &body)
		if body.AccessToken == "" || body.RefreshToken == "" {
			t.Error("refresh returned an empty token")
		}
		if service.gotRefresh == "" {
			t.Error("the service was not given the refresh token")
		}
	})

	t.Run("reuse is unauthorized", func(t *testing.T) {
		handler := newTestHandler(t, &fakeAuthService{refreshErr: auth.ErrRefreshTokenReused})

		recorder := doRequest(handler, http.MethodPost, "/auth/refresh", `{
			"refresh_token": "11111111-1111-4111-8111-111111111111.secret"
		}`)

		if recorder.Code != http.StatusUnauthorized {
			t.Fatalf("status = %d, want %d", recorder.Code, http.StatusUnauthorized)
		}
		if code := decodedErrorCode(t, recorder); code != codeInvalidRefreshToken {
			t.Errorf("error code = %q, want %q", code, codeInvalidRefreshToken)
		}
	})
}

// TestLogoutUsesTheTokenSession proves the body cannot choose which session is
// ended: the id comes from the access token's own claim.
func TestLogoutUsesTheTokenSession(t *testing.T) {
	service := &fakeAuthService{}
	handler := newTestHandler(t, service)

	recorder := doAuthedRequest(t, handler, http.MethodPost, "/auth/logout", "")

	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d (body %s)", recorder.Code, http.StatusOK, recorder.Body.String())
	}
	if service.gotLogout[0] != testUserID {
		t.Errorf("user id = %q, want %q", service.gotLogout[0], testUserID)
	}
	if service.gotLogout[1] != testSessionID {
		t.Errorf("session id = %q, want the token's session %q", service.gotLogout[1], testSessionID)
	}
}

func TestListSessionsEndpoint(t *testing.T) {
	service := &fakeAuthService{sessionsResult: []*auth.Session{
		{
			ID:         testSessionID,
			UserID:     testUserID,
			DeviceInfo: map[string]any{"platform": "ios"},
			IPAddress:  "203.0.113.7",
		},
		{
			ID:         "22222222-2222-4222-8222-222222222222",
			UserID:     testUserID,
			DeviceInfo: nil,
			IPAddress:  "",
		},
	}}
	handler := newTestHandler(t, service)

	recorder := doAuthedRequest(t, handler, http.MethodGet, "/auth/sessions", "")

	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d (body %s)", recorder.Code, http.StatusOK, recorder.Body.String())
	}

	var body sessionsResponse
	decodeBody(t, recorder, &body)

	if len(body.Sessions) != 2 {
		t.Fatalf("sessions = %d, want 2", len(body.Sessions))
	}
	if !body.Sessions[0].Current {
		t.Error("the caller's own session is not marked current")
	}
	if body.Sessions[1].Current {
		t.Error("another session is marked current")
	}
	if body.Sessions[1].DeviceInfo == nil {
		t.Error("device_info is null, want an empty object")
	}
	if service.gotListUser != testUserID {
		t.Errorf("user id = %q, want %q", service.gotListUser, testUserID)
	}
}

func TestDeleteSessionEndpoint(t *testing.T) {
	t.Run("revokes", func(t *testing.T) {
		service := &fakeAuthService{}
		handler := newTestHandler(t, service)

		target := "22222222-2222-4222-8222-222222222222"
		recorder := doAuthedRequest(t, handler, http.MethodDelete, "/auth/sessions/"+target, "")

		if recorder.Code != http.StatusOK {
			t.Fatalf("status = %d, want %d (body %s)", recorder.Code, http.StatusOK, recorder.Body.String())
		}
		if service.gotRevoke[1] != target {
			t.Errorf("revoked session = %q, want %q", service.gotRevoke[1], target)
		}
		if service.gotRevoke[0] != testUserID {
			t.Errorf("user id = %q, want %q", service.gotRevoke[0], testUserID)
		}
	})

	t.Run("unknown session", func(t *testing.T) {
		handler := newTestHandler(t, &fakeAuthService{revokeErr: auth.ErrSessionNotFound})

		recorder := doAuthedRequest(t, handler, http.MethodDelete,
			"/auth/sessions/22222222-2222-4222-8222-222222222222", "")

		if recorder.Code != http.StatusNotFound {
			t.Fatalf("status = %d, want %d", recorder.Code, http.StatusNotFound)
		}
		if code := decodedErrorCode(t, recorder); code != codeSessionNotFound {
			t.Errorf("error code = %q, want %q", code, codeSessionNotFound)
		}
	})
}

// TestForgotPasswordAlwaysSucceeds proves the endpoint cannot be used to discover
// which addresses have accounts.
func TestForgotPasswordAlwaysSucceeds(t *testing.T) {
	service := &fakeAuthService{}
	handler := newTestHandler(t, service)

	recorder := doRequest(handler, http.MethodPost, "/auth/forgot-password", `{"email":"nobody@example.com"}`)

	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d (body %s)", recorder.Code, http.StatusOK, recorder.Body.String())
	}

	var body successResponse
	decodeBody(t, recorder, &body)
	if !body.Success {
		t.Error("success = false, want true")
	}
}

func TestResetPasswordEndpoint(t *testing.T) {
	t.Run("resets", func(t *testing.T) {
		service := &fakeAuthService{}
		handler := newTestHandler(t, service)

		recorder := doRequest(handler, http.MethodPost, "/auth/reset-password", `{
			"token": "reset-token",
			"new_password": "a brand new 9"
		}`)

		if recorder.Code != http.StatusOK {
			t.Fatalf("status = %d, want %d (body %s)", recorder.Code, http.StatusOK, recorder.Body.String())
		}
		if service.gotReset[0] != "reset-token" || service.gotReset[1] != "a brand new 9" {
			t.Errorf("service received %v, want the token and the new password", service.gotReset)
		}
	})

	t.Run("invalid token", func(t *testing.T) {
		handler := newTestHandler(t, &fakeAuthService{resetErr: auth.ErrInvalidPasswordResetToken})

		recorder := doRequest(handler, http.MethodPost, "/auth/reset-password", `{
			"token": "nope",
			"new_password": "a brand new 9"
		}`)

		if recorder.Code != http.StatusBadRequest {
			t.Fatalf("status = %d, want %d", recorder.Code, http.StatusBadRequest)
		}
		if code := decodedErrorCode(t, recorder); code != codeInvalidResetToken {
			t.Errorf("error code = %q, want %q", code, codeInvalidResetToken)
		}
	})
}

// TestProtectedAuthRoutesRequireAToken proves the four session routes are not
// reachable anonymously.
func TestProtectedAuthRoutesRequireAToken(t *testing.T) {
	handler := newTestHandler(t, &fakeAuthService{})

	requests := []struct {
		method string
		path   string
	}{
		{method: http.MethodPost, path: "/auth/logout"},
		{method: http.MethodGet, path: "/auth/sessions"},
		{method: http.MethodDelete, path: "/auth/sessions/22222222-2222-4222-8222-222222222222"},
	}

	for _, request := range requests {
		t.Run(request.method+" "+request.path, func(t *testing.T) {
			recorder := doRequest(handler, request.method, request.path, "")

			if recorder.Code != http.StatusUnauthorized {
				t.Fatalf("status = %d, want %d", recorder.Code, http.StatusUnauthorized)
			}
			if code := decodedErrorCode(t, recorder); code != codeUnauthorized {
				t.Errorf("error code = %q, want %q", code, codeUnauthorized)
			}
		})
	}
}

// TestRegisterIsRateLimited proves the register limit is a hard block and that the
// refusal carries the header a client needs.
func TestRegisterIsRateLimited(t *testing.T) {
	handler := newTestHandler(t, &fakeAuthService{registerResult: sampleUser()})

	body := `{"email":"ada@example.com","password":"correct horse 7","display_name":"Ada"}`

	for i := int64(1); i <= auth.LimitRegister.Attempts; i++ {
		recorder := doRequest(handler, http.MethodPost, "/auth/register", body)
		if recorder.Code != http.StatusCreated {
			t.Fatalf("attempt %d status = %d, want %d (body %s)", i, recorder.Code, http.StatusCreated, recorder.Body.String())
		}
	}

	recorder := doRequest(handler, http.MethodPost, "/auth/register", body)

	if recorder.Code != http.StatusTooManyRequests {
		t.Fatalf("status = %d, want %d", recorder.Code, http.StatusTooManyRequests)
	}
	if code := decodedErrorCode(t, recorder); code != codeRateLimited {
		t.Errorf("error code = %q, want %q", code, codeRateLimited)
	}

	retryAfter, err := strconv.Atoi(recorder.Header().Get("Retry-After"))
	if err != nil {
		t.Fatalf("Retry-After = %q, want an integer: %v", recorder.Header().Get("Retry-After"), err)
	}
	if want := int(auth.LimitRegister.Window.Seconds()); retryAfter != want {
		t.Errorf("Retry-After = %d, want %d", retryAfter, want)
	}
}

// TestLoginIsRateLimitedPerAddress proves the login limit blocks a run of guesses.
func TestLoginIsRateLimitedPerAddress(t *testing.T) {
	handler := newTestHandler(t, &fakeAuthService{loginErr: identity.ErrInvalidCredentials})

	body := `{"email":"ada@example.com","password":"wrong horse 7"}`

	// The per-address limit is the looser of the two, so it is the one that decides
	// here: the per-email limit would block first in a real attack on one account.
	var blocked bool
	for i := 0; i < 20; i++ {
		recorder := doRequest(handler, http.MethodPost, "/auth/login", body)
		if recorder.Code == http.StatusTooManyRequests {
			blocked = true
			if recorder.Header().Get("Retry-After") == "" {
				t.Error("a 429 was returned without Retry-After")
			}
			break
		}
	}

	if !blocked {
		t.Fatal("20 sign-in attempts were never rate limited")
	}
}

// TestForgotPasswordIsRateLimited proves an unregistered address still burns the
// limit, so the limiter cannot be used to probe accounts.
func TestForgotPasswordIsRateLimited(t *testing.T) {
	handler := newTestHandler(t, &fakeAuthService{})

	body := `{"email":"nobody@example.com"}`

	for i := int64(1); i <= auth.LimitForgotPassword.Attempts; i++ {
		if recorder := doRequest(handler, http.MethodPost, "/auth/forgot-password", body); recorder.Code != http.StatusOK {
			t.Fatalf("attempt %d status = %d, want %d", i, recorder.Code, http.StatusOK)
		}
	}

	recorder := doRequest(handler, http.MethodPost, "/auth/forgot-password", body)
	if recorder.Code != http.StatusTooManyRequests {
		t.Fatalf("status = %d, want %d", recorder.Code, http.StatusTooManyRequests)
	}
}

// TestServiceFailureIsNotLeaked proves an unexpected error becomes a generic 500
// with no internal detail, on the new endpoints too.
func TestServiceFailureIsNotLeaked(t *testing.T) {
	handler := newTestHandler(t, &fakeAuthService{refreshErr: errors.New("redis on fire")})

	recorder := doRequest(handler, http.MethodPost, "/auth/refresh", `{
		"refresh_token": "11111111-1111-4111-8111-111111111111.secret"
	}`)

	if recorder.Code != http.StatusInternalServerError {
		t.Fatalf("status = %d, want %d", recorder.Code, http.StatusInternalServerError)
	}
	if strings.Contains(recorder.Body.String(), "redis on fire") {
		t.Errorf("response leaked internal detail: %s", recorder.Body.String())
	}
}

// TestSessionRoutesUseTheTokenSessionNotTheBody is a regression guard: the logout
// handler must never read a session id supplied by the caller.
//
// The handler does not read a body at all, so a caller that sends one is ignored
// rather than obeyed — which is exactly the property pinned here.
func TestSessionRoutesUseTheTokenSessionNotTheBody(t *testing.T) {
	service := &fakeAuthService{}
	handler := newTestHandler(t, service)

	const attempted = "33333333-3333-4333-8333-333333333333"

	recorder := doAuthedRequest(t, handler, http.MethodPost, "/auth/logout",
		fmt.Sprintf(`{"session_id":%q}`, attempted))

	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d (body %s)", recorder.Code, http.StatusOK, recorder.Body.String())
	}
	if service.gotLogout[1] != testSessionID {
		t.Errorf("logout used session %q, want the token's session %q", service.gotLogout[1], testSessionID)
	}
	if service.gotLogout[1] == attempted {
		t.Error("logout obeyed a session id taken from the request body")
	}
}
