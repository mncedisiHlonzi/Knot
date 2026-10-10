package rooted

import (
	"context"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"
)

// foreignKeyViolation is the PostgreSQL SQLSTATE raised by a foreign key
// violation. It is how a signal for a non-existent user is recognised.
const foreignKeyViolation = "23503"

// rootedUserFK is the foreign key that names rooted_signals.user_id. Matching on
// the constraint name keeps a violation of some other key from being mistaken for
// a missing user.
const rootedUserFK = "rooted_signals_user_id_fkey"

// signalColumns is the canonical SELECT/RETURNING column list. It is a constant so
// every query in this file stays consistent with scanSignal, and its order is the
// order scanSignal reads.
const signalColumns = `id, user_id, place, latitude, longitude, place_country, duration_bucket, is_public, is_primary, created_at, updated_at`

// PostgresStore is the pgx-backed implementation of RootedStore.
//
// It holds an injected pool and no other state, so it is safe for concurrent use.
type PostgresStore struct {
	pool *pgxpool.Pool
}

// Compile-time proof that the store satisfies the domain contract.
var _ RootedStore = (*PostgresStore)(nil)

// NewPostgresStore returns a store backed by pool. The pool is injected rather
// than reached for globally so that callers own its lifecycle.
func NewPostgresStore(pool *pgxpool.Pool) (*PostgresStore, error) {
	if pool == nil {
		return nil, fmt.Errorf("rooted: postgres store requires a non-nil pool")
	}
	return &PostgresStore{pool: pool}, nil
}

// SetPrimary upserts the user's primary signal and returns the stored row.
//
// The insert conflicts with rooted_signals_one_primary_per_user — the partial
// unique index on (user_id) where is_primary — so a second call updates the
// existing primary row instead of creating a second one. The index predicate is
// repeated in ON CONFLICT so PostgreSQL can infer that exact index.
//
// A signal for a user that does not exist is reported as ErrUserNotFound rather
// than as a raw driver error, so the handler can answer 404.
func (s *PostgresStore) SetPrimary(ctx context.Context, userID string, signal Signal) (Signal, error) {
	if !isUUID(userID) {
		return Signal{}, ErrUserNotFound
	}

	const query = `
		INSERT INTO rooted_signals (user_id, place, latitude, longitude, place_country, duration_bucket, is_public, is_primary)
		VALUES ($1, $2, $3, $4, $5, $6, $7, true)
		ON CONFLICT (user_id) WHERE is_primary = true
		DO UPDATE SET
			place = EXCLUDED.place,
			latitude = EXCLUDED.latitude,
			longitude = EXCLUDED.longitude,
			place_country = EXCLUDED.place_country,
			duration_bucket = EXCLUDED.duration_bucket,
			is_public = EXCLUDED.is_public,
			updated_at = now()
		RETURNING ` + signalColumns

	stored, err := scanSignal(s.pool.QueryRow(
		ctx,
		query,
		userID,
		signal.Place,
		signal.Latitude,
		signal.Longitude,
		signal.PlaceCountry,
		string(signal.DurationBucket),
		signal.IsPublic,
	))
	if err != nil {
		if isForeignKeyViolation(err, rootedUserFK) {
			return Signal{}, ErrUserNotFound
		}
		return Signal{}, fmt.Errorf("rooted: set primary signal: %w", err)
	}

	return stored, nil
}

// ListByUser returns every signal for a user, primary first, oldest first within
// that. The primary flag is the ordering key so the owner's active signal leads.
func (s *PostgresStore) ListByUser(ctx context.Context, userID string) ([]Signal, error) {
	if !isUUID(userID) {
		return nil, nil
	}

	const query = `SELECT ` + signalColumns + ` FROM rooted_signals WHERE user_id = $1 ORDER BY is_primary DESC, created_at ASC, id ASC`

	return s.querySignals(ctx, query, userID)
}

// ListPublicByUser returns only the public signals for a user, primary first.
func (s *PostgresStore) ListPublicByUser(ctx context.Context, userID string) ([]Signal, error) {
	if !isUUID(userID) {
		return nil, nil
	}

	const query = `SELECT ` + signalColumns + ` FROM rooted_signals WHERE user_id = $1 AND is_public = true ORDER BY is_primary DESC, created_at ASC, id ASC`

	return s.querySignals(ctx, query, userID)
}

// BatchPrimaryPublic returns the primary public signal for each of the given user
// ids, keyed by user id, in a single query.
//
// The ids are encoded as a real uuid[] rather than a text[] so the parameter type
// matches the column and user_id's index stays usable. Ids that are not canonical
// UUIDs are dropped rather than sent, because a malformed element would make the
// whole array cast fail.
func (s *PostgresStore) BatchPrimaryPublic(ctx context.Context, userIDs []string) (map[string]Signal, error) {
	out := make(map[string]Signal, len(userIDs))

	ids := make([]pgtype.UUID, 0, len(userIDs))
	for _, userID := range userIDs {
		var parsed pgtype.UUID
		if err := parsed.Scan(userID); err != nil {
			continue
		}
		ids = append(ids, parsed)
	}
	if len(ids) == 0 {
		return out, nil
	}

	const query = `
		SELECT ` + signalColumns + `
		FROM rooted_signals
		WHERE is_primary = true AND is_public = true AND user_id = ANY($1)`

	rows, err := s.pool.Query(ctx, query, ids)
	if err != nil {
		return nil, fmt.Errorf("rooted: batch primary public signals: %w", err)
	}
	defer rows.Close()

	for rows.Next() {
		signal, err := scanSignal(rows)
		if err != nil {
			return nil, fmt.Errorf("rooted: batch primary public signals: %w", err)
		}
		out[signal.UserID] = signal
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("rooted: batch primary public signals: %w", err)
	}

	return out, nil
}

// RootedUserIDsByPlace returns the ids of at most limit users whose primary
// public signal is for place, earliest declarer first.
//
// The order is (created_at ASC, id ASC) — "the first N Rooted users of this place"
// read literally: the people who declared the connection earliest. A user has at
// most one primary signal (the partial unique index), so no duplicate user id can
// be returned and no DISTINCT is needed.
//
// The match is exact on the stored place, which is the same free text a signal was
// declared with; it is served by rooted_signals_place_idx. Only primary, public
// signals route: a hidden signal is one its owner chose not to be findable by, and
// routing an unknown person's question to them would breach that (KNOT-ADR-056).
func (s *PostgresStore) RootedUserIDsByPlace(ctx context.Context, place string, limit int) ([]string, error) {
	if place == "" || limit < 1 {
		return []string{}, nil
	}

	const query = `
		SELECT user_id
		FROM rooted_signals
		WHERE is_primary = true AND is_public = true AND place = $1
		ORDER BY created_at ASC, id ASC
		LIMIT $2`

	rows, err := s.pool.Query(ctx, query, place, limit)
	if err != nil {
		return nil, fmt.Errorf("rooted: root user ids by place: %w", err)
	}
	defer rows.Close()

	userIDs := make([]string, 0, limit)
	for rows.Next() {
		var userID string
		if err := rows.Scan(&userID); err != nil {
			return nil, fmt.Errorf("rooted: root user ids by place: %w", err)
		}
		userIDs = append(userIDs, userID)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("rooted: root user ids by place: %w", err)
	}

	return userIDs, nil
}

// UserExists reports whether a user row exists. A malformed id is reported as
// false rather than as a database error.
func (s *PostgresStore) UserExists(ctx context.Context, userID string) (bool, error) {
	if !isUUID(userID) {
		return false, nil
	}

	var exists bool
	if err := s.pool.QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM users WHERE id = $1)`, userID).Scan(&exists); err != nil {
		return false, fmt.Errorf("rooted: check user: %w", err)
	}

	return exists, nil
}

// querySignals runs a signal SELECT and scans every row into a slice.
func (s *PostgresStore) querySignals(ctx context.Context, query string, args ...any) ([]Signal, error) {
	rows, err := s.pool.Query(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("rooted: list signals: %w", err)
	}
	defer rows.Close()

	signals := make([]Signal, 0)
	for rows.Next() {
		signal, err := scanSignal(rows)
		if err != nil {
			return nil, fmt.Errorf("rooted: list signals: %w", err)
		}
		signals = append(signals, signal)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("rooted: list signals: %w", err)
	}

	return signals, nil
}

// rowScanner is the subset of pgx.Row and pgx.Rows that scanSignal needs.
type rowScanner interface {
	Scan(dest ...any) error
}

// scanSignal reads one row of signalColumns into a Signal. It returns the driver
// error unwrapped so callers can classify it (for example as a foreign key
// violation).
func scanSignal(row rowScanner) (Signal, error) {
	var (
		signal  Signal
		bucket  string
		country *string
	)

	err := row.Scan(
		&signal.ID,
		&signal.UserID,
		&signal.Place,
		&signal.Latitude,
		&signal.Longitude,
		&country,
		&bucket,
		&signal.IsPublic,
		&signal.IsPrimary,
		&signal.CreatedAt,
		&signal.UpdatedAt,
	)
	if err != nil {
		return Signal{}, err
	}

	signal.PlaceCountry = country
	signal.DurationBucket = DurationBucket(bucket)

	return signal, nil
}

// isForeignKeyViolation reports whether err is a foreign key violation on the
// named constraint.
func isForeignKeyViolation(err error, constraint string) bool {
	var pgErr *pgconn.PgError
	return errors.As(err, &pgErr) && pgErr.Code == foreignKeyViolation && pgErr.ConstraintName == constraint
}
