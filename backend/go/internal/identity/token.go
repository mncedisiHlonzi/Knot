package identity

import (
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"time"

	"github.com/golang-jwt/jwt/v5"
)

// Token lifetimes. The access token is deliberately short-lived because it
// cannot be revoked; the refresh token is long-lived and is rotated by the
// (not yet implemented) refresh endpoint.
const (
	// AccessTokenTTL is how long an access token remains valid.
	AccessTokenTTL = 15 * time.Minute
	// RefreshTokenTTL is how long a refresh token remains valid.
	RefreshTokenTTL = 30 * 24 * time.Hour

	// MinJWTSecretBytes is the shortest signing key accepted outside local
	// development.
	MinJWTSecretBytes = 32
)

// Claims is the claim set Knot issues in both access and refresh tokens.
type Claims struct {
	// TokenType is "access" or "refresh". Checking it stops a refresh token from
	// being replayed as an access token.
	TokenType string `json:"typ"`
	jwt.RegisteredClaims
}

// TokenIssuer signs and verifies Knot's HS256 JWTs.
//
// It holds no state beyond the signing key, so it is safe for concurrent use.
type TokenIssuer struct {
	secret []byte
	// now is injectable so tests can produce tokens with a controlled clock.
	now func() time.Time
}

// NewTokenIssuer returns an issuer that signs with secret. It fails when the
// secret is empty, because an empty HS256 key would be trivially forgeable.
func NewTokenIssuer(secret string) (*TokenIssuer, error) {
	if secret == "" {
		return nil, fmt.Errorf("identity: JWT secret must not be empty")
	}
	return &TokenIssuer{secret: []byte(secret), now: time.Now}, nil
}

// IssueAccessToken returns a signed access token for userID.
func (i *TokenIssuer) IssueAccessToken(userID string) (string, error) {
	return i.issue(userID, TokenTypeAccess, AccessTokenTTL)
}

// IssueRefreshToken returns a signed refresh token for userID. It carries a
// random token id (the "jti" claim) so a future refresh endpoint can rotate and
// revoke individual tokens.
func (i *TokenIssuer) IssueRefreshToken(userID string) (string, error) {
	return i.issue(userID, TokenTypeRefresh, RefreshTokenTTL)
}

// ParseAccessToken verifies raw and returns its claims. It returns
// ErrInvalidToken for a malformed, expired, mis-signed, or wrong-type token.
func (i *TokenIssuer) ParseAccessToken(raw string) (*Claims, error) {
	return i.parse(raw, TokenTypeAccess)
}

// ParseRefreshToken verifies raw and returns its claims.
func (i *TokenIssuer) ParseRefreshToken(raw string) (*Claims, error) {
	return i.parse(raw, TokenTypeRefresh)
}

// issue builds, signs, and returns a token of the given type.
func (i *TokenIssuer) issue(userID, tokenType string, ttl time.Duration) (string, error) {
	if userID == "" {
		return "", fmt.Errorf("identity: cannot issue %s token without a user id", tokenType)
	}

	now := i.now()
	claims := Claims{
		TokenType: tokenType,
		RegisteredClaims: jwt.RegisteredClaims{
			Subject:   userID,
			IssuedAt:  jwt.NewNumericDate(now),
			ExpiresAt: jwt.NewNumericDate(now.Add(ttl)),
		},
	}

	if tokenType == TokenTypeRefresh {
		id, err := randomTokenID()
		if err != nil {
			return "", err
		}
		claims.ID = id
	}

	signed, err := jwt.NewWithClaims(jwt.SigningMethodHS256, claims).SignedString(i.secret)
	if err != nil {
		return "", fmt.Errorf("identity: sign %s token: %w", tokenType, err)
	}

	return signed, nil
}

// parse verifies raw and asserts that it is a token of the expected type.
func (i *TokenIssuer) parse(raw, wantType string) (*Claims, error) {
	if raw == "" {
		return nil, fmt.Errorf("%w: empty token", ErrInvalidToken)
	}

	claims := &Claims{}
	parsed, err := jwt.ParseWithClaims(
		raw,
		claims,
		func(*jwt.Token) (any, error) { return i.secret, nil },
		// Pinning the accepted algorithm is what prevents algorithm-confusion
		// attacks (for example, a token signed with "none" or with the public
		// half of an asymmetric key).
		jwt.WithValidMethods([]string{jwt.SigningMethodHS256.Alg()}),
	)
	if err != nil {
		return nil, fmt.Errorf("%w: %s", ErrInvalidToken, err)
	}
	if !parsed.Valid {
		return nil, fmt.Errorf("%w: token is not valid", ErrInvalidToken)
	}
	if claims.TokenType != wantType {
		return nil, fmt.Errorf("%w: token type %q, want %q", ErrInvalidToken, claims.TokenType, wantType)
	}
	if claims.Subject == "" {
		return nil, fmt.Errorf("%w: missing subject", ErrInvalidToken)
	}

	return claims, nil
}

// randomTokenID returns a 128-bit random identifier, hex encoded.
func randomTokenID() (string, error) {
	buf := make([]byte, 16)
	if _, err := rand.Read(buf); err != nil {
		return "", fmt.Errorf("identity: generate token id: %w", err)
	}
	return hex.EncodeToString(buf), nil
}
