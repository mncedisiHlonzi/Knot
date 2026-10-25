package auth

import (
	"context"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
)

// credentialColumns is the canonical SELECT/RETURNING column list. It is a
// constant so every query in this file stays consistent with scanCredential.
const credentialColumns = `id, user_id, credential_type, credential_data, created_at, updated_at`

// PostgresCredentialStore is the pgx-backed implementation of CredentialStore.
//
// It holds an injected pool and no other state, so it is safe for concurrent use.
type PostgresCredentialStore struct {
	pool *pgxpool.Pool
}

// Compile-time proof that the store satisfies the domain contract.
var _ CredentialStore = (*PostgresCredentialStore)(nil)

// NewPostgresCredentialStore returns a store backed by pool. The pool is injected
// rather than reached for globally so that callers own its lifecycle.
func NewPostgresCredentialStore(pool *pgxpool.Pool) (*PostgresCredentialStore, error) {
	if pool == nil {
		return nil, fmt.Errorf("auth: postgres credential store requires a non-nil pool")
	}

	return &PostgresCredentialStore{pool: pool}, nil
}

// Create inserts a credential and returns the stored row.
//
// A second credential of the same type for the same user is ErrCredentialExists
// rather than a raw unique violation: the caller's next move — an Update, or a
// refusal — depends on knowing which of the two happened.
func (s *PostgresCredentialStore) Create(ctx context.Context, credential *Credential) (*Credential, error) {
	if credential == nil {
		return nil, fmt.Errorf("auth: create credential: credential must not be nil")
	}
	if !isUUID(credential.UserID) {
		return nil, wrapUUIDError("auth: create credential", credential.UserID)
	}
	if !IsCredentialType(credential.CredentialType) {
		return nil, fmt.Errorf("auth: create credential: type %q: %w", credential.CredentialType, ErrInvalidCredentialType)
	}

	const query = `
		INSERT INTO credentials (user_id, credential_type, credential_data)
		VALUES ($1, $2, $3)
		RETURNING ` + credentialColumns

	created, err := scanCredential(s.pool.QueryRow(ctx, query,
		credential.UserID,
		credential.CredentialType,
		credential.CredentialData,
	))
	if err != nil {
		var pgErr *pgconn.PgError
		if errors.As(err, &pgErr) && pgErr.Code == uniqueViolation {
			return nil, ErrCredentialExists
		}
		return nil, fmt.Errorf("auth: create credential: %w", err)
	}

	return created, nil
}

// FindByUserIDAndType returns the user's credential of that type, or
// ErrCredentialNotFound.
func (s *PostgresCredentialStore) FindByUserIDAndType(ctx context.Context, userID, credentialType string) (*Credential, error) {
	if !isUUID(userID) {
		return nil, wrapUUIDError("auth: find credential", userID)
	}
	if !IsCredentialType(credentialType) {
		return nil, fmt.Errorf("auth: find credential: type %q: %w", credentialType, ErrInvalidCredentialType)
	}

	const query = `
		SELECT ` + credentialColumns + `
		FROM credentials
		WHERE user_id = $1 AND credential_type = $2`

	credential, err := scanCredential(s.pool.QueryRow(ctx, query, userID, credentialType))
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, ErrCredentialNotFound
		}
		return nil, fmt.Errorf("auth: find credential: %w", err)
	}

	return credential, nil
}

// Update replaces the credential's data and returns the stored row.
//
// Only credential_data moves. updated_at is set by the database so it cannot
// disagree with the row it describes, and a malformed id is not-found for the
// same reason as elsewhere in this package.
func (s *PostgresCredentialStore) Update(ctx context.Context, credential *Credential) (*Credential, error) {
	if credential == nil {
		return nil, fmt.Errorf("auth: update credential: credential must not be nil")
	}
	if !isUUID(credential.ID) {
		return nil, ErrCredentialNotFound
	}

	const query = `
		UPDATE credentials
		SET credential_data = $2, updated_at = now()
		WHERE id = $1
		RETURNING ` + credentialColumns

	updated, err := scanCredential(s.pool.QueryRow(ctx, query, credential.ID, credential.CredentialData))
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, ErrCredentialNotFound
		}
		return nil, fmt.Errorf("auth: update credential: %w", err)
	}

	return updated, nil
}

// scanCredential reads one credential row in credentialColumns order.
func scanCredential(row pgx.Row) (*Credential, error) {
	var credential Credential

	if err := row.Scan(
		&credential.ID,
		&credential.UserID,
		&credential.CredentialType,
		&credential.CredentialData,
		&credential.CreatedAt,
		&credential.UpdatedAt,
	); err != nil {
		return nil, err
	}

	return &credential, nil
}
