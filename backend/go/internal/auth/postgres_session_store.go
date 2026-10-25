package auth

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// sessionColumns is the canonical SELECT/RETURNING list. It is a constant so
// every query in this file stays consistent with scanSession.
//
// host(ip_address) returns the address without its netmask and NULL when the
// column is NULL, which keeps the Go side a plain string instead of making every
// caller handle an INET type.
const sessionColumns = `id, user_id, refresh_token_hash, device_info, host(ip_address), last_used_at, revoked_at, created_at`

// PostgresSessionStore is the pgx-backed implementation of SessionStore.
//
// It holds an injected pool and no other state, so it is safe for concurrent use.
type PostgresSessionStore struct {
	pool *pgxpool.Pool
}

// Compile-time proof that the store satisfies the domain contract.
var _ SessionStore = (*PostgresSessionStore)(nil)

// NewPostgresSessionStore returns a store backed by pool. The pool is injected
// rather than reached for globally so that callers own its lifecycle.
func NewPostgresSessionStore(pool *pgxpool.Pool) (*PostgresSessionStore, error) {
	if pool == nil {
		return nil, fmt.Errorf("auth: postgres session store requires a non-nil pool")
	}

	return &PostgresSessionStore{pool: pool}, nil
}

// Create inserts a session and returns the stored row.
//
// A session id supplied by the caller is used as given, and an empty one is left
// to the database. The service mints its own id because the id has to appear
// inside the refresh token that is handed to the client at the same moment the
// row is written (see newSessionID); the column's default remains for anything
// that inserts a session directly.
func (s *PostgresSessionStore) Create(ctx context.Context, session *Session) (*Session, error) {
	if session == nil {
		return nil, fmt.Errorf("auth: create session: session must not be nil")
	}
	if !isUUID(session.UserID) {
		return nil, wrapUUIDError("auth: create session", session.UserID)
	}
	if session.ID != "" && !isUUID(session.ID) {
		return nil, fmt.Errorf("auth: create session: session id %q is not a UUID", session.ID)
	}

	deviceInfo, err := encodeDeviceInfo(session.DeviceInfo)
	if err != nil {
		return nil, fmt.Errorf("auth: create session: %w", err)
	}

	const query = `
		INSERT INTO sessions (id, user_id, refresh_token_hash, device_info, ip_address)
		VALUES (COALESCE(NULLIF($1, '')::uuid, gen_random_uuid()), $2, $3, $4, NULLIF($5, '')::inet)
		RETURNING ` + sessionColumns

	created, err := scanSession(s.pool.QueryRow(ctx, query,
		session.ID,
		session.UserID,
		session.RefreshTokenHash,
		deviceInfo,
		session.IPAddress,
	))
	if err != nil {
		return nil, fmt.Errorf("auth: create session: %w", err)
	}

	return created, nil
}

// FindByRefreshTokenHash returns the session for a refresh token hash, revoked
// or not, or ErrSessionNotFound.
func (s *PostgresSessionStore) FindByRefreshTokenHash(ctx context.Context, refreshTokenHash string) (*Session, error) {
	const query = `SELECT ` + sessionColumns + ` FROM sessions WHERE refresh_token_hash = $1`

	session, err := scanSession(s.pool.QueryRow(ctx, query, refreshTokenHash))
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, ErrSessionNotFound
		}
		return nil, fmt.Errorf("auth: find session by refresh token hash: %w", err)
	}

	return session, nil
}

// Revoke ends a session, leaving an earlier revoked_at in place.
//
// A malformed id is treated as not-found rather than as a database error, for
// the same reason as the identity store: it keeps the id column's index usable
// instead of casting it to text in SQL.
func (s *PostgresSessionStore) Revoke(ctx context.Context, sessionID string) error {
	if !isUUID(sessionID) {
		return ErrSessionNotFound
	}

	const query = `
		UPDATE sessions
		SET revoked_at = COALESCE(revoked_at, now())
		WHERE id = $1
		RETURNING revoked_at`

	var revokedAt time.Time
	if err := s.pool.QueryRow(ctx, query, sessionID).Scan(&revokedAt); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return ErrSessionNotFound
		}
		return fmt.Errorf("auth: revoke session: %w", err)
	}

	return nil
}

// FindByID returns the session with the given id, revoked or not.
func (s *PostgresSessionStore) FindByID(ctx context.Context, sessionID string) (*Session, error) {
	if !isUUID(sessionID) {
		return nil, ErrSessionNotFound
	}

	const query = `SELECT ` + sessionColumns + ` FROM sessions WHERE id = $1`

	session, err := scanSession(s.pool.QueryRow(ctx, query, sessionID))
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, ErrSessionNotFound
		}
		return nil, fmt.Errorf("auth: find session by id: %w", err)
	}

	return session, nil
}

// RotateRefreshToken swaps the session's refresh token hash in one statement.
//
// The WHERE clause carries all three conditions — live, owned by this id, and
// holding the expected hash — so the check and the write cannot be separated by a
// concurrent refresh. When it matches nothing, the follow-up read says which
// condition failed, because "the session is gone" and "this token was already
// rotated" call for very different responses (KNOT-ADR-085).
func (s *PostgresSessionStore) RotateRefreshToken(ctx context.Context, sessionID, expectedHash, newHash string) (*Session, error) {
	if !isUUID(sessionID) {
		return nil, ErrSessionNotFound
	}
	if expectedHash == "" || newHash == "" {
		return nil, fmt.Errorf("auth: rotate refresh token: both hashes must be set")
	}

	const query = `
		UPDATE sessions
		SET refresh_token_hash = $3, last_used_at = now()
		WHERE id = $1 AND refresh_token_hash = $2 AND revoked_at IS NULL
		RETURNING ` + sessionColumns

	rotated, err := scanSession(s.pool.QueryRow(ctx, query, sessionID, expectedHash, newHash))
	if err == nil {
		return rotated, nil
	}
	if !errors.Is(err, pgx.ErrNoRows) {
		return nil, fmt.Errorf("auth: rotate refresh token: %w", err)
	}

	// Nothing matched. Distinguish a dead session from a stale token, so the
	// service can contain a leaked token without signing the user out for the
	// ordinary case of a request that lost a race with its own retry.
	existing, findErr := s.FindByID(ctx, sessionID)
	if findErr != nil {
		return nil, findErr
	}
	if !existing.Active() {
		return nil, ErrSessionNotFound
	}

	return nil, ErrSessionRefreshTokenMismatch
}

// ListForUser returns the user's live sessions, newest first.
//
// A malformed user id is an error rather than an empty list: it can only be a
// bug in the caller, and an empty list would hide it.
func (s *PostgresSessionStore) ListForUser(ctx context.Context, userID string) ([]*Session, error) {
	if !isUUID(userID) {
		return nil, wrapUUIDError("auth: list sessions", userID)
	}

	const query = `
		SELECT ` + sessionColumns + `
		FROM sessions
		WHERE user_id = $1 AND revoked_at IS NULL
		ORDER BY created_at DESC, id DESC`

	rows, err := s.pool.Query(ctx, query, userID)
	if err != nil {
		return nil, fmt.Errorf("auth: list sessions: %w", err)
	}
	defer rows.Close()

	sessions := make([]*Session, 0, 4)
	for rows.Next() {
		session, scanErr := scanSession(rows)
		if scanErr != nil {
			return nil, fmt.Errorf("auth: list sessions: scan: %w", scanErr)
		}
		sessions = append(sessions, session)
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("auth: list sessions: %w", err)
	}

	return sessions, nil
}

// RevokeAllForUser ends every live session the user has.
//
// It reports no error for a user with no sessions, so signing out everywhere is
// safe to call twice and safe to call after a password change has already done
// the work.
func (s *PostgresSessionStore) RevokeAllForUser(ctx context.Context, userID string) error {
	if !isUUID(userID) {
		return wrapUUIDError("auth: revoke all sessions", userID)
	}

	const query = `
		UPDATE sessions
		SET revoked_at = COALESCE(revoked_at, now())
		WHERE user_id = $1 AND revoked_at IS NULL`

	if _, err := s.pool.Exec(ctx, query, userID); err != nil {
		return fmt.Errorf("auth: revoke all sessions: %w", err)
	}

	return nil
}

// scanSession reads one session row in sessionColumns order.
func scanSession(row pgx.Row) (*Session, error) {
	var (
		session   Session
		deviceRaw []byte
		ipAddress *string
	)

	if err := row.Scan(
		&session.ID,
		&session.UserID,
		&session.RefreshTokenHash,
		&deviceRaw,
		&ipAddress,
		&session.LastUsedAt,
		&session.RevokedAt,
		&session.CreatedAt,
	); err != nil {
		return nil, err
	}

	if ipAddress != nil {
		session.IPAddress = *ipAddress
	}

	if len(deviceRaw) > 0 {
		if err := json.Unmarshal(deviceRaw, &session.DeviceInfo); err != nil {
			return nil, fmt.Errorf("decode device info: %w", err)
		}
	}
	if session.DeviceInfo == nil {
		session.DeviceInfo = map[string]any{}
	}

	return &session, nil
}

// encodeDeviceInfo renders device info for a JSONB column.
//
// A nil or empty map is stored as {}, never as SQL NULL: the column is NOT NULL,
// and an empty object is the honest representation of "the client said nothing".
func encodeDeviceInfo(info map[string]any) ([]byte, error) {
	if len(info) == 0 {
		return []byte("{}"), nil
	}

	encoded, err := json.Marshal(info)
	if err != nil {
		return nil, fmt.Errorf("encode device info: %w", err)
	}

	return encoded, nil
}
