package identity

import (
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/golang-jwt/jwt/v5"
)

// newTestIssuer returns an issuer with a fixed, sufficiently long secret.
func newTestIssuer(t *testing.T) *TokenIssuer {
	t.Helper()

	issuer, err := NewTokenIssuer("test-secret-that-is-at-least-32-bytes-long")
	if err != nil {
		t.Fatalf("NewTokenIssuer() error = %v, want nil", err)
	}
	return issuer
}

func TestNewTokenIssuerRejectsEmptySecret(t *testing.T) {
	if _, err := NewTokenIssuer(""); err == nil {
		t.Fatal("NewTokenIssuer(\"\") error = nil, want an error")
	}
}

func TestIssueAndParseAccessToken(t *testing.T) {
	issuer := newTestIssuer(t)

	token, err := issuer.IssueAccessToken("user-123")
	if err != nil {
		t.Fatalf("IssueAccessToken() error = %v, want nil", err)
	}

	claims, err := issuer.ParseAccessToken(token)
	if err != nil {
		t.Fatalf("ParseAccessToken() error = %v, want nil", err)
	}

	if claims.Subject != "user-123" {
		t.Errorf("subject = %q, want %q", claims.Subject, "user-123")
	}
	if claims.TokenType != TokenTypeAccess {
		t.Errorf("token type = %q, want %q", claims.TokenType, TokenTypeAccess)
	}
	if claims.ExpiresAt == nil {
		t.Fatal("expiry is nil, want it set")
	}

	lifetime := claims.ExpiresAt.Sub(claims.IssuedAt.Time)
	if lifetime != AccessTokenTTL {
		t.Errorf("lifetime = %s, want %s", lifetime, AccessTokenTTL)
	}
}

func TestAccessTokenHasNoTokenID(t *testing.T) {
	issuer := newTestIssuer(t)

	token, err := issuer.IssueAccessToken("user-123")
	if err != nil {
		t.Fatalf("IssueAccessToken() error = %v, want nil", err)
	}

	claims, err := issuer.ParseAccessToken(token)
	if err != nil {
		t.Fatalf("ParseAccessToken() error = %v, want nil", err)
	}
	if claims.ID != "" {
		t.Errorf("access token id = %q, want empty", claims.ID)
	}
}

func TestIssueAndParseRefreshToken(t *testing.T) {
	issuer := newTestIssuer(t)

	token, err := issuer.IssueRefreshToken("user-123")
	if err != nil {
		t.Fatalf("IssueRefreshToken() error = %v, want nil", err)
	}

	claims, err := issuer.ParseRefreshToken(token)
	if err != nil {
		t.Fatalf("ParseRefreshToken() error = %v, want nil", err)
	}

	if claims.Subject != "user-123" {
		t.Errorf("subject = %q, want %q", claims.Subject, "user-123")
	}
	if claims.TokenType != TokenTypeRefresh {
		t.Errorf("token type = %q, want %q", claims.TokenType, TokenTypeRefresh)
	}
	if claims.ID == "" {
		t.Error("refresh token id is empty, want a random token id")
	}

	lifetime := claims.ExpiresAt.Sub(claims.IssuedAt.Time)
	if lifetime != RefreshTokenTTL {
		t.Errorf("lifetime = %s, want %s", lifetime, RefreshTokenTTL)
	}
}

func TestRefreshTokenIDsAreUnique(t *testing.T) {
	issuer := newTestIssuer(t)

	first, err := issuer.IssueRefreshToken("user-123")
	if err != nil {
		t.Fatalf("first IssueRefreshToken() error = %v, want nil", err)
	}
	second, err := issuer.IssueRefreshToken("user-123")
	if err != nil {
		t.Fatalf("second IssueRefreshToken() error = %v, want nil", err)
	}

	if first == second {
		t.Error("two refresh tokens are identical, want distinct token ids")
	}
}

func TestExpiredTokenIsRejected(t *testing.T) {
	issuer := newTestIssuer(t)

	// Issue the token two hours in the past so its 15 minute lifetime is over.
	issuer.now = func() time.Time { return time.Now().Add(-2 * time.Hour) }

	token, err := issuer.IssueAccessToken("user-123")
	if err != nil {
		t.Fatalf("IssueAccessToken() error = %v, want nil", err)
	}

	// Restore the real clock before verifying.
	issuer.now = time.Now

	if _, err := issuer.ParseAccessToken(token); !errors.Is(err, ErrInvalidToken) {
		t.Errorf("ParseAccessToken() error = %v, want ErrInvalidToken", err)
	}
}

func TestTamperedTokenIsRejected(t *testing.T) {
	issuer := newTestIssuer(t)

	token, err := issuer.IssueAccessToken("user-123")
	if err != nil {
		t.Fatalf("IssueAccessToken() error = %v, want nil", err)
	}

	// Flip the final character of the signature.
	last := token[len(token)-1]
	replacement := byte('A')
	if last == 'A' {
		replacement = 'B'
	}
	tampered := token[:len(token)-1] + string(replacement)

	if _, err := issuer.ParseAccessToken(tampered); !errors.Is(err, ErrInvalidToken) {
		t.Errorf("ParseAccessToken() error = %v, want ErrInvalidToken for a tampered token", err)
	}
}

func TestTokenSignedWithAnotherSecretIsRejected(t *testing.T) {
	issuer := newTestIssuer(t)
	other, err := NewTokenIssuer("a-completely-different-secret-of-32-bytes")
	if err != nil {
		t.Fatalf("NewTokenIssuer() error = %v, want nil", err)
	}

	token, err := other.IssueAccessToken("user-123")
	if err != nil {
		t.Fatalf("IssueAccessToken() error = %v, want nil", err)
	}

	if _, err := issuer.ParseAccessToken(token); !errors.Is(err, ErrInvalidToken) {
		t.Errorf("ParseAccessToken() error = %v, want ErrInvalidToken for a foreign signature", err)
	}
}

func TestRefreshTokenIsRejectedAsAccessToken(t *testing.T) {
	issuer := newTestIssuer(t)

	refresh, err := issuer.IssueRefreshToken("user-123")
	if err != nil {
		t.Fatalf("IssueRefreshToken() error = %v, want nil", err)
	}

	if _, err := issuer.ParseAccessToken(refresh); !errors.Is(err, ErrInvalidToken) {
		t.Errorf("ParseAccessToken() error = %v, want ErrInvalidToken for a refresh token", err)
	}

	access, err := issuer.IssueAccessToken("user-123")
	if err != nil {
		t.Fatalf("IssueAccessToken() error = %v, want nil", err)
	}
	if _, err := issuer.ParseRefreshToken(access); !errors.Is(err, ErrInvalidToken) {
		t.Errorf("ParseRefreshToken() error = %v, want ErrInvalidToken for an access token", err)
	}
}

func TestTokenWithUnexpectedAlgorithmIsRejected(t *testing.T) {
	const secret = "test-secret-that-is-at-least-32-bytes-long"
	issuer := newTestIssuer(t)

	// Sign with HS512 using the same key. The issuer only accepts HS256, so this
	// must be rejected rather than silently accepted.
	claims := Claims{
		TokenType: TokenTypeAccess,
		RegisteredClaims: jwt.RegisteredClaims{
			Subject:   "user-123",
			IssuedAt:  jwt.NewNumericDate(time.Now()),
			ExpiresAt: jwt.NewNumericDate(time.Now().Add(time.Hour)),
		},
	}
	foreign, err := jwt.NewWithClaims(jwt.SigningMethodHS512, claims).SignedString([]byte(secret))
	if err != nil {
		t.Fatalf("signing HS512 token: %v", err)
	}

	if _, err := issuer.ParseAccessToken(foreign); !errors.Is(err, ErrInvalidToken) {
		t.Errorf("ParseAccessToken() error = %v, want ErrInvalidToken for an HS512 token", err)
	}
}

func TestEmptyTokenIsRejected(t *testing.T) {
	issuer := newTestIssuer(t)

	if _, err := issuer.ParseAccessToken(""); !errors.Is(err, ErrInvalidToken) {
		t.Errorf("ParseAccessToken(\"\") error = %v, want ErrInvalidToken", err)
	}
}

func TestJWTIsNotSignedWithTheNoneAlgorithm(t *testing.T) {
	issuer := newTestIssuer(t)

	token, err := issuer.IssueAccessToken("user-123")
	if err != nil {
		t.Fatalf("IssueAccessToken() error = %v, want nil", err)
	}

	// The header must name a real HMAC algorithm, never "none".
	header := strings.SplitN(token, ".", 2)[0]
	if strings.Contains(strings.ToLower(header), "none") {
		t.Errorf("token header %q mentions the none algorithm", header)
	}
}
