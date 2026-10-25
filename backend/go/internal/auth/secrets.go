package auth

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"strings"
)

// Length of the random material behind a refresh token or a verification token.
//
// Thirty-two bytes is 256 bits of entropy, which is far past the point where
// guessing is a threat model: an attacker cannot enumerate a space that size even
// with the whole planet's compute. It is the same length as the SHA-256 digest it
// is hashed into, which keeps the stored value exactly as large as the search
// space it summarises.
const secretBytes = 32

// refreshTokenSeparator divides the session id from the secret in a refresh token.
//
// The refresh token is opaque to the client — a single string it stores and sends
// back — but it is deliberately not opaque to the server: the prefix names the
// session the token belongs to, which lets a refresh detect *reuse* (a token that
// was rotated away being presented again) instead of only detecting "not found"
// (KNOT-ADR-085). Without the session id the old and the new token would look
// identical to the server: both would simply be unknown hashes.
const refreshTokenSeparator = "."

// newSecret returns 32 random bytes, hex encoded.
func newSecret() (string, error) {
	buf := make([]byte, secretBytes)
	if _, err := rand.Read(buf); err != nil {
		return "", fmt.Errorf("auth: generate secret: %w", err)
	}

	return hex.EncodeToString(buf), nil
}

// hashSecret returns the hex-encoded SHA-256 of a secret.
//
// Every token Knot stores is stored this way: the plaintext exists in the
// client's hands (or in an email that was never sent) and nowhere else, so a
// database dump or a Redis dump is not a set of usable credentials.
func hashSecret(secret string) string {
	sum := sha256.Sum256([]byte(secret))

	return hex.EncodeToString(sum[:])
}

// newSessionID returns a random RFC 4122 version 4 UUID.
//
// Sessions are given their id by Go rather than by the database because the id
// has to appear inside the refresh token, and the token has to be handed to the
// client at the same moment the row is written. It is a UUIDv4 rather than a
// sequence so an id carries no information about when it was created or how many
// sessions exist, and so two database shards can never mint the same one. The
// cost is twenty lines instead of a dependency.
func newSessionID() (string, error) {
	buf := make([]byte, 16)
	if _, err := rand.Read(buf); err != nil {
		return "", fmt.Errorf("auth: generate session id: %w", err)
	}

	// Set the version (4) and the RFC 4122 variant bits.
	buf[6] = (buf[6] & 0x0f) | 0x40
	buf[8] = (buf[8] & 0x3f) | 0x80

	return fmt.Sprintf(
		"%x-%x-%x-%x-%x",
		buf[0:4], buf[4:6], buf[6:8], buf[8:10], buf[10:16],
	), nil
}

// newRefreshToken mints a token for sessionID and returns the token to hand to
// the client and the hash to store against the session.
func newRefreshToken(sessionID string) (token, hash string, err error) {
	secret, err := newSecret()
	if err != nil {
		return "", "", err
	}

	return sessionID + refreshTokenSeparator + secret, hashSecret(secret), nil
}

// SplitRefreshToken returns the session id a refresh token names, without
// verifying anything.
//
// It is exported because rate limiting has to happen before the token is
// validated: a caller hammering the refresh endpoint should be counted whether or
// not its tokens are real, and counting by session id needs the prefix. It
// deliberately does not check the secret — that is the store's job, in one
// conditional UPDATE — so it can never be used as an authentication check.
func SplitRefreshToken(token string) (sessionID string, ok bool) {
	prefix, _, found := strings.Cut(token, refreshTokenSeparator)
	if !found || prefix == "" {
		return "", false
	}

	return prefix, true
}

// parseRefreshToken splits a refresh token into the session it names and the
// secret it carries.
func parseRefreshToken(token string) (sessionID, secret string, err error) {
	sessionID, secret, found := strings.Cut(token, refreshTokenSeparator)
	if !found || sessionID == "" || secret == "" {
		return "", "", fmt.Errorf("auth: malformed refresh token")
	}

	return sessionID, secret, nil
}
