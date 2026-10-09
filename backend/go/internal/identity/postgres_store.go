package identity

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
)

// uniqueViolation is the PostgreSQL SQLSTATE raised by a unique constraint
// violation. It is how a duplicate email is recognised.
const uniqueViolation = "23505"

// userColumns is the canonical SELECT/RETURNING column list. It is a constant so
// every query in this file stays consistent with scanUser.
const userColumns = `id, email, phone, password_hash, display_name, preferred_languages, approximate_location, avatar_url, created_at, updated_at`

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
		normalizeEmail(user.Email),
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

	user, err := scanUser(s.pool.QueryRow(ctx, query, normalizeEmail(email)))
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
// guaranteed.
//
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
		&preferredLanguage,
		&location,
		&avatarURL,
		&user.CreatedAt,
		&user.UpdatedAt,
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
