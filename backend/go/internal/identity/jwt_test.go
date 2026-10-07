package identity

import (
	"bytes"
	"encoding/base64"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/golang-jwt/jwt/v5"

	"github.com/knot/backend/internal/testutil"
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

// splitToken splits a compact JWS into its header, payload, and signature segments.
func splitToken(t *testing.T, token string) []string {
	t.Helper()

	segments := strings.Split(token, ".")
	if len(segments) != 3 {
		t.Fatalf("token has %d segments, want 3", len(segments))
	}

	return segments
}

// decodeSegment base64url-decodes one JWT segment. It fails the test rather than
// returning an error, because a segment that does not decode makes the assertion
// that follows meaningless.
func decodeSegment(t *testing.T, segment string) []byte {
	t.Helper()

	decoded, err := base64.RawURLEncoding.DecodeString(segment)
	if err != nil {
		t.Fatalf("decoding JWT segment %q: %v", segment, err)
	}

	return decoded
}

// tamperSignature returns the token with a mutated signature segment.
//
// It exists so the three-part token structure stays out of the shared
// testutil.TamperBase64Body helper, which only knows about a single base64 body. The
// mutation flips a bit in the leading byte of the MAC, which is guaranteed to change
// the signature. See testutil.TamperBase64Body for why the mutation is expressed in
// decoded bytes rather than as an edit to the encoded text.
func tamperSignature(t *testing.T, token string) string {
	t.Helper()

	segments := splitToken(t, token)
	segments[2], _, _ = testutil.TamperBase64Body(t, base64.RawURLEncoding, segments[2], func(signature []byte) {
		signature[0] ^= 0x01
	})

	return strings.Join(segments, ".")
}

func TestTamperedTokenIsRejected(t *testing.T) {
	issuer := newTestIssuer(t)

	token, err := issuer.IssueAccessToken("user-123")
	if err != nil {
		t.Fatalf("IssueAccessToken() error = %v, want nil", err)
	}

	tampered := tamperSignature(t, token)

	if _, err := issuer.ParseAccessToken(tampered); !errors.Is(err, ErrInvalidToken) {
		t.Errorf("ParseAccessToken() error = %v, want ErrInvalidToken for a tampered signature", err)
	}
}

// TestForgedPayloadIsRejected tampers at a different point from
// TestTamperedTokenIsRejected: it rewrites the payload segment without re-signing.
// This is the forged-claim attack the signature exists to prevent.
func TestForgedPayloadIsRejected(t *testing.T) {
	issuer := newTestIssuer(t)

	token, err := issuer.IssueAccessToken("user-123")
	if err != nil {
		t.Fatalf("IssueAccessToken() error = %v, want nil", err)
	}

	segments := splitToken(t, token)

	claims := decodeSegment(t, segments[1])
	forged := bytes.Replace(claims, []byte("user-123"), []byte("user-999"), 1)
	if bytes.Equal(forged, claims) {
		t.Fatal("forged payload is identical to the original; nothing was tampered with")
	}

	segments[1] = base64.RawURLEncoding.EncodeToString(forged)
	tampered := strings.Join(segments, ".")

	if _, err := issuer.ParseAccessToken(tampered); !errors.Is(err, ErrInvalidToken) {
		t.Errorf("ParseAccessToken() error = %v, want ErrInvalidToken for a re-encoded payload", err)
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
