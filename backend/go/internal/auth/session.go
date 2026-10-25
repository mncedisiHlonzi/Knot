package auth

import (
	"context"
	"errors"
	"time"
)

// ErrSessionNotFound is returned when a session id or refresh token hash matches
// no row.
var ErrSessionNotFound = errors.New("auth: session not found")

// Session is one signed-in device.
//
// It is the row, not the token: the client holds an opaque refresh token whose
// SHA-256 the server stores in RefreshTokenHash. Nothing here can be replayed,
// which is the whole point of storing the hash (KNOT-ADR-080).
type Session struct {
	// ID is the session's own identity, and what Revoke takes.
	ID string
	// UserID is the account the session belongs to.
	UserID string
	// RefreshTokenHash is the SHA-256 of the opaque refresh token, hex encoded.
	// The plaintext token exists only in the client's hands.
	RefreshTokenHash string
	// DeviceInfo is whatever the client told us about itself. It is free-form
	// JSONB in the database, so this is a map rather than a struct: the server
	// stores what it was given and a device screen renders what it finds.
	DeviceInfo map[string]any
	// IPAddress is the address the session was created from, or "" when it is
	// unknown. The column is INET; this field is the address without a netmask.
	IPAddress string
	// LastUsedAt is the last time the session was refreshed.
	LastUsedAt time.Time
	// RevokedAt is when the session was ended, or nil while it is live.
	RevokedAt *time.Time
	// CreatedAt is when the session was created.
	CreatedAt time.Time
}

// Active reports whether the session has not been revoked.
//
// It says nothing about whether the session has expired: expiry belongs to the
// token, and a session row outlives it so the device stays visible in a list of
// sign-ins.
func (s *Session) Active() bool {
	return s.RevokedAt == nil
}

// SessionStore is the storage contract for sessions.
//
// It stores and revokes; it does not mint tokens, hash them, or decide how long
// a session lives. Those are policy, and policy lives in the service that
// KNOT-031 adds.
type SessionStore interface {
	// Create stores a new session and returns the stored row.
	Create(ctx context.Context, session *Session) (*Session, error)

	// FindByRefreshTokenHash returns the session for a refresh token hash,
	// whether or not it has been revoked: a caller that must reject a revoked
	// session has to see it to say so.
	FindByRefreshTokenHash(ctx context.Context, refreshTokenHash string) (*Session, error)

	// Revoke ends a session. Revoking an already revoked session is not an
	// error and does not move its revoked_at; an id that matches no row is
	// ErrSessionNotFound.
	Revoke(ctx context.Context, sessionID string) error

	// ListForUser returns the user's live sessions, newest first. Revoked
	// sessions are omitted: this is what a "your devices" screen shows.
	ListForUser(ctx context.Context, userID string) ([]*Session, error)

	// RevokeAllForUser ends every live session the user has. It is idempotent
	// and reports no error when the user has none.
	RevokeAllForUser(ctx context.Context, userID string) error
}
