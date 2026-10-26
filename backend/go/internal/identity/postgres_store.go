package identity

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
)

// uniqueViolation is the PostgreSQL SQLSTATE raised by a unique constraint
// violation. It is how a duplicate email is recognised.
const uniqueViolation = "23505"

// userColumns is the canonical SELECT/RETURNING column list. It is a constant so
// every query in this file stays consistent with scanUser.
const userColumns = `id, email, phone, password_hash, display_name, role, preferred_languages, approximate_location, avatar_url, created_at, updated_at, email_verified_at, deleted_at, deletion_scheduled_at, purged_at`

// PostgresStore is the pgx-backed implementation of UserStore.
//
// It holds an injected pool and no other state, so it is safe for concurrent use.
type PostgresStore struct {
	pool *pgxpool.Pool
}

// Compile-time proof that the store satisfies the domain contract.
var _ UserStore = (*PostgresStore)(nil)

// NewPostgresStore returns a store backed by pool. The pool is injected rather
// than reached for globally so that callers own its lifecycle.
func NewPostgresStore(pool *pgxpool.Pool) (*PostgresStore, error) {
	if pool == nil {
		return nil, fmt.Errorf("identity: postgres store requires a non-nil pool")
	}
	return &PostgresStore{pool: pool}, nil
}

// CreateUser inserts a user and returns the stored row. A duplicate email is
// reported as ErrEmailTaken rather than as a raw driver error.
func (s *PostgresStore) CreateUser(ctx context.Context, user *User) (*User, error) {
	const query = `
		INSERT INTO users (
			email, phone, password_hash, display_name, preferred_languages, approximate_location
		)
		VALUES ($1, $2, $3, $4, $5, $6)
		RETURNING ` + userColumns

	row := s.pool.QueryRow(
		ctx,
		query,
		NormalizeEmail(user.Email),
		nullIfEmpty(user.Phone),
		user.PasswordHash,
		user.DisplayName,
		nonNilLanguages(user.PreferredLanguages),
		nullIfEmpty(user.ApproximateLocation),
	)

	created, err := scanUser(row)
	if err != nil {
		var pgErr *pgconn.PgError
		if errors.As(err, &pgErr) && pgErr.Code == uniqueViolation {
			return nil, ErrEmailTaken
		}
		return nil, fmt.Errorf("identity: create user: %w", err)
	}

	return created, nil
}

// FindUserByEmail returns the user with the given email, matched
// case-insensitively, or ErrUserNotFound.
func (s *PostgresStore) FindUserByEmail(ctx context.Context, email string) (*User, error) {
	const query = `SELECT ` + userColumns + ` FROM users WHERE lower(email) = lower($1)`

	user, err := scanUser(s.pool.QueryRow(ctx, query, NormalizeEmail(email)))
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, ErrUserNotFound
		}
		return nil, fmt.Errorf("identity: find user by email: %w", err)
	}

	return user, nil
}

// FindUserByID returns the user with the given id, or ErrUserNotFound.
//
// A malformed id is treated as not-found rather than as a database error, which
// keeps the id column's index usable instead of casting it to text in SQL.
func (s *PostgresStore) FindUserByID(ctx context.Context, id string) (*User, error) {
	if !isUUID(id) {
		return nil, ErrUserNotFound
	}

	const query = `SELECT ` + userColumns + ` FROM users WHERE id = $1`

	user, err := scanUser(s.pool.QueryRow(ctx, query, id))
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, ErrUserNotFound
		}
		return nil, fmt.Errorf("identity: find user by id: %w", err)
	}

	return user, nil
}

// FindActiveUserByEmail returns a user that is not inside its deletion window.
//
// The deletion filter lives in SQL rather than in Go so a deleted account is
// indistinguishable from an absent one at every layer above the store, which is
// what keeps sign-in from confirming that an account exists but is closing
// (KNOT-ADR-081).
func (s *PostgresStore) FindActiveUserByEmail(ctx context.Context, email string) (*User, error) {
	const query = `SELECT ` + userColumns + ` FROM users
		WHERE lower(email) = lower($1) AND deleted_at IS NULL`

	user, err := scanUser(s.pool.QueryRow(ctx, query, NormalizeEmail(email)))
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, ErrUserNotFound
		}
		return nil, fmt.Errorf("identity: find active user by email: %w", err)
	}

	return user, nil
}

// FindActiveUserByID returns a user that is not inside its deletion window.
func (s *PostgresStore) FindActiveUserByID(ctx context.Context, id string) (*User, error) {
	if !isUUID(id) {
		return nil, ErrUserNotFound
	}

	const query = `SELECT ` + userColumns + ` FROM users WHERE id = $1 AND deleted_at IS NULL`

	user, err := scanUser(s.pool.QueryRow(ctx, query, id))
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, ErrUserNotFound
		}
		return nil, fmt.Errorf("identity: find active user by id: %w", err)
	}

	return user, nil
}

// UpdateAvatarURL stores the object key of the user's current avatar and returns
// the stored row, with updated_at refreshed by the database.
//
// Passing an empty key clears the avatar. A malformed id and an id with no row
// are both reported as ErrUserNotFound, for the same reason as FindUserByID.
func (s *PostgresStore) UpdateAvatarURL(ctx context.Context, userID, avatarURL string) (*User, error) {
	if !isUUID(userID) {
		return nil, ErrUserNotFound
	}

	const query = `
		UPDATE users
		SET avatar_url = $2, updated_at = now()
		WHERE id = $1
		RETURNING ` + userColumns

	updated, err := scanUser(s.pool.QueryRow(ctx, query, userID, nullIfEmpty(avatarURL)))
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, ErrUserNotFound
		}
		return nil, fmt.Errorf("identity: update avatar url: %w", err)
	}

	return updated, nil
}

// FindUsersByID returns the users with the given ids. Malformed ids and ids that
// match no row are simply absent from the result, and the order is not
// guaranteed.//
// It exists so a page of notifications can resolve every distinct actor with one
// query instead of one query per notification. Duplicates are removed here as
// well as in the service so the parameter list can never grow past the caller's
// distinct ids.
func (s *PostgresStore) FindUsersByID(ctx context.Context, ids []string) ([]*User, error) {
	unique := make([]string, 0, len(ids))
	seen := make(map[string]struct{}, len(ids))
	for _, id := range ids {
		if !isUUID(id) {
			continue
		}
		if _, ok := seen[id]; ok {
			continue
		}
		seen[id] = struct{}{}
		unique = append(unique, id)
	}
	if len(unique) == 0 {
		return []*User{}, nil
	}

	placeholders := make([]string, len(unique))
	args := make([]any, len(unique))
	for i, id := range unique {
		placeholders[i] = fmt.Sprintf("$%d", i+1)
		args[i] = id
	}

	query := `SELECT ` + userColumns + ` FROM users WHERE id IN (` + strings.Join(placeholders, ", ") + `)`

	rows, err := s.pool.Query(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("identity: find users by id: %w", err)
	}
	defer rows.Close()

	users := make([]*User, 0, len(unique))
	for rows.Next() {
		user, err := scanUser(rows)
		if err != nil {
			return nil, fmt.Errorf("identity: find users by id: %w", err)
		}
		users = append(users, user)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("identity: find users by id: %w", err)
	}

	return users, nil
}

// MarkEmailVerified records that the user's address was proven and returns the
// stored row.
//
// The original timestamp is kept on a second verification: `COALESCE` makes the
// write idempotent, so a replayed verification link cannot move the moment the
// account became trustworthy. A malformed id and an id with no row are both
// ErrUserNotFound, for the same reason as FindUserByID.
func (s *PostgresStore) MarkEmailVerified(ctx context.Context, userID string) (*User, error) {
	if !isUUID(userID) {
		return nil, ErrUserNotFound
	}

	const query = `
		UPDATE users
		SET email_verified_at = COALESCE(email_verified_at, now()), updated_at = now()
		WHERE id = $1
		RETURNING ` + userColumns

	verified, err := scanUser(s.pool.QueryRow(ctx, query, userID))
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, ErrUserNotFound
		}
		return nil, fmt.Errorf("identity: mark email verified: %w", err)
	}

	return verified, nil
}

// UpdatePasswordHash replaces the stored password credential and returns the
// updated row.
//
// `users.password_hash` is the deprecated home of the password secret
// (KNOT-ADR-079); a reset writes it here and the auth service refreshes the
// matching `credentials` row, so both stay in step until the column is retired.
func (s *PostgresStore) UpdatePasswordHash(ctx context.Context, userID, passwordHash string) (*User, error) {
	if !isUUID(userID) {
		return nil, ErrUserNotFound
	}

	const query = `
		UPDATE users
		SET password_hash = $2, updated_at = now()
		WHERE id = $1
		RETURNING ` + userColumns

	updated, err := scanUser(s.pool.QueryRow(ctx, query, userID, passwordHash))
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, ErrUserNotFound
		}
		return nil, fmt.Errorf("identity: update password hash: %w", err)
	}

	return updated, nil
}

// MarkDeleted soft-deletes the account and returns the stored row.
//
// The timestamps are written once: `COALESCE` keeps an earlier `deleted_at` and
// `deletion_scheduled_at`, so a second request cannot extend a recovery window that
// is already running down. The window is computed in SQL from the same `now()` that
// stamps the deletion, so the two can never disagree by a clock tick.
func (s *PostgresStore) MarkDeleted(ctx context.Context, userID string) (*User, error) {
	if !isUUID(userID) {
		return nil, ErrUserNotFound
	}

	const query = `
		UPDATE users
		SET deleted_at = COALESCE(deleted_at, now()),
			deletion_scheduled_at = COALESCE(deletion_scheduled_at, now() + make_interval(secs => $2)),
			updated_at = now()
		WHERE id = $1
		RETURNING ` + userColumns

	deleted, err := scanUser(s.pool.QueryRow(ctx, query, userID, int64(DeletionRecoveryWindow.Seconds())))
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, ErrUserNotFound
		}
		return nil, fmt.Errorf("identity: mark deleted: %w", err)
	}

	return deleted, nil
}

// ClearDeletion cancels a pending deletion and returns the stored row.
//
// Clearing an account that was never deleted is a successful no-op, which is what
// lets a login call it unconditionally once it has seen a deletion timestamp: the
// race between two devices signing in at once then has one harmless loser instead of
// an error.
func (s *PostgresStore) ClearDeletion(ctx context.Context, userID string) (*User, error) {
	if !isUUID(userID) {
		return nil, ErrUserNotFound
	}

	const query = `
		UPDATE users
		SET deleted_at = NULL, deletion_scheduled_at = NULL, updated_at = now()
		WHERE id = $1 AND deleted_at IS NOT NULL
		RETURNING ` + userColumns

	restored, err := scanUser(s.pool.QueryRow(ctx, query, userID))
	if err != nil {
		if !errors.Is(err, pgx.ErrNoRows) {
			return nil, fmt.Errorf("identity: clear deletion: %w", err)
		}

		// Nothing to clear: either the account is live (fine) or it does not exist.
		return s.FindUserByID(ctx, userID)
	}

	return restored, nil
}

// PurgeDueUsers anonymises every account whose recovery window has closed.
//
// Everything happens in one transaction, so a crash cannot leave an account with a
// rewritten email and a live password. The due ids are selected `FOR UPDATE SKIP
// LOCKED`: two purge runs at once therefore split the work instead of colliding, and
// a row locked by another worker is simply left for it.
//
// The purge **anonymises, it does not erase** (ADR-089). The row and every story,
// comment and signal it authored stay; what goes is anything that could identify the
// person or log in as them: the address (replaced with an unroutable placeholder),
// the display name, the contact fields, the avatar, the password hash, and every
// credential, session and verification token.
func (s *PostgresStore) PurgeDueUsers(ctx context.Context, now time.Time) (int, error) {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return 0, fmt.Errorf("identity: purge: begin: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	const dueQuery = `
		SELECT id
		FROM users
		WHERE deleted_at IS NOT NULL
			AND purged_at IS NULL
			AND deletion_scheduled_at IS NOT NULL
			AND deletion_scheduled_at <= $1
		FOR UPDATE SKIP LOCKED`

	rows, err := tx.Query(ctx, dueQuery, now)
	if err != nil {
		return 0, fmt.Errorf("identity: purge: select due users: %w", err)
	}

	due := make([]string, 0, 8)
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			rows.Close()
			return 0, fmt.Errorf("identity: purge: scan due user: %w", err)
		}
		due = append(due, id)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return 0, fmt.Errorf("identity: purge: iterate due users: %w", err)
	}

	for _, id := range due {
		if err := purgeUser(ctx, tx, id); err != nil {
			return 0, err
		}
	}

	if err := tx.Commit(ctx); err != nil {
		return 0, fmt.Errorf("identity: purge: commit: %w", err)
	}

	return len(due), nil
}

// purgeAnonymisedEmailDomain is the reserved TLD the anonymised address uses.
// RFC 2606 reserves `.invalid`, so an address there can never be delivered and can
// never collide with a real person's address.
const purgeAnonymisedEmailDomain = "@deleted.invalid"

// purgeUser anonymises one account and removes everything that could log in as it.
func purgeUser(ctx context.Context, tx pgx.Tx, userID string) error {
	// `password_hash` is NOT NULL, so it is replaced rather than nulled. The value
	// cannot be parsed as an argon2id PHC string, which means no password verifies
	// against it even if the row were reachable — a fail-closed placeholder rather
	// than an empty hash that a lenient verifier might accept.
	const anonymise = `
		UPDATE users
		SET email = 'purged+' || id::text || $2,
			display_name = 'Deleted User',
			phone = NULL,
			approximate_location = NULL,
			avatar_url = NULL,
			preferred_languages = '{}',
			password_hash = '!purged',
			purged_at = now(),
			updated_at = now()
		WHERE id = $1 AND purged_at IS NULL`

	if _, err := tx.Exec(ctx, anonymise, userID, purgeAnonymisedEmailDomain); err != nil {
		return fmt.Errorf("identity: purge: anonymise %s: %w", userID, err)
	}

	for _, table := range []string{"credentials", "sessions", "verification_tokens"} {
		if _, err := tx.Exec(ctx, `DELETE FROM `+table+` WHERE user_id = $1`, userID); err != nil {
			return fmt.Errorf("identity: purge: delete %s for %s: %w", table, userID, err)
		}
	}

	return nil
}

// rowScanner is the subset of pgx.Row and pgx.Rows that scanUser needs.
type rowScanner interface {
	Scan(dest ...any) error
}

// scanUser reads one row of userColumns into a User. It returns the driver error
// unwrapped so callers can classify it (for example as a unique violation).
func scanUser(row rowScanner) (*User, error) {
	var (
		user              User
		phone             *string
		location          *string
		avatarURL         *string
		preferredLanguage []string
	)

	err := row.Scan(
		&user.ID,
		&user.Email,
		&phone,
		&user.PasswordHash,
		&user.DisplayName,
		&user.Role,
		&preferredLanguage,
		&location,
		&avatarURL,
		&user.CreatedAt,
		&user.UpdatedAt,
		&user.EmailVerifiedAt,
		&user.DeletedAt,
		&user.DeletionScheduledAt,
		&user.PurgedAt,
	)
	if err != nil {
		return nil, err
	}

	if phone != nil {
		user.Phone = *phone
	}
	if location != nil {
		user.ApproximateLocation = *location
	}
	if avatarURL != nil {
		user.AvatarURL = *avatarURL
	}
	user.PreferredLanguages = nonNilLanguages(preferredLanguage)

	return &user, nil
}

// nullIfEmpty maps "" onto SQL NULL, which is what the nullable columns expect.
func nullIfEmpty(value string) *string {
	if value == "" {
		return nil
	}
	return &value
}

// nonNilLanguages guarantees a non-nil slice so a TEXT[] NOT NULL column never
// receives NULL.
func nonNilLanguages(languages []string) []string {
	if languages == nil {
		return []string{}
	}
	return languages
}

// isUUID reports whether s is a canonical 8-4-4-4-12 hexadecimal UUID string.
func isUUID(s string) bool {
	if len(s) != 36 {
		return false
	}
	for i := 0; i < len(s); i++ {
		switch i {
		case 8, 13, 18, 23:
			if s[i] != '-' {
				return false
			}
		default:
			if !isHexDigit(s[i]) {
				return false
			}
		}
	}
	return true
}

// isHexDigit reports whether b is an ASCII hexadecimal digit.
func isHexDigit(b byte) bool {
	switch {
	case b >= '0' && b <= '9':
		return true
	case b >= 'a' && b <= 'f':
		return true
	case b >= 'A' && b <= 'F':
		return true
	default:
		return false
	}
}
