package auth

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// verificationTokenColumns is the canonical SELECT/RETURNING column list. It is a
// constant so every query in this file stays consistent with scanVerificationToken.
const verificationTokenColumns = `id, user_id, token_hash, token_type, expires_at, consumed_at, created_at`

// PostgresVerificationTokenStore is the pgx-backed implementation of
// VerificationTokenStore.
//
// It holds an injected pool and no other state, so it is safe for concurrent use.
type PostgresVerificationTokenStore struct {
	pool *pgxpool.Pool
}

// Compile-time proof that the store satisfies the domain contract.
var _ VerificationTokenStore = (*PostgresVerificationTokenStore)(nil)

// NewPostgresVerificationTokenStore returns a store backed by pool. The pool is
// injected rather than reached for globally so that callers own its lifecycle.
func NewPostgresVerificationTokenStore(pool *pgxpool.Pool) (*PostgresVerificationTokenStore, error) {
	if pool == nil {
		return nil, fmt.Errorf("auth: postgres verification token store requires a non-nil pool")
	}

	return &PostgresVerificationTokenStore{pool: pool}, nil
}

// Create inserts a token and returns the stored row.
//
// The expiry is the caller's, not a default: how long an address-verification
// link lives is policy, and this layer has none.
func (s *PostgresVerificationTokenStore) Create(ctx context.Context, token *VerificationToken) (*VerificationToken, error) {
	if token == nil {
		return nil, fmt.Errorf("auth: create verification token: token must not be nil")
	}
	if !isUUID(token.UserID) {
		return nil, wrapUUIDError("auth: create verification token", token.UserID)
	}
	if !IsVerificationTokenType(token.TokenType) {
		return nil, fmt.Errorf(
			"auth: create verification token: type %q: %w",
			token.TokenType, ErrInvalidVerificationTokenType,
		)
	}
	if token.ExpiresAt.IsZero() {
		return nil, fmt.Errorf("auth: create verification token: expires_at must be set")
	}

	const query = `
		INSERT INTO verification_tokens (user_id, token_hash, token_type, expires_at)
		VALUES ($1, $2, $3, $4)
		RETURNING ` + verificationTokenColumns

	created, err := scanVerificationToken(s.pool.QueryRow(ctx, query,
		token.UserID,
		token.TokenHash,
		token.TokenType,
		token.ExpiresAt,
	))
	if err != nil {
		return nil, fmt.Errorf("auth: create verification token: %w", err)
	}

	return created, nil
}

// FindByTokenHash returns the token for a hash, redeemed or not.
func (s *PostgresVerificationTokenStore) FindByTokenHash(ctx context.Context, tokenHash string) (*VerificationToken, error) {
	const query = `SELECT ` + verificationTokenColumns + ` FROM verification_tokens WHERE token_hash = $1`

	token, err := scanVerificationToken(s.pool.QueryRow(ctx, query, tokenHash))
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, ErrVerificationTokenNotFound
		}
		return nil, fmt.Errorf("auth: find verification token: %w", err)
	}

	return token, nil
}

// Consume marks a token as redeemed, exactly once.
//
// The conditional update is the whole mechanism: two concurrent redemptions of
// the same link are serialised by the row lock, and only the first one finds
// consumed_at NULL. When the update matches nothing, a second query says whether
// the token never existed or was already used, because those are different
// answers to give a person.
func (s *PostgresVerificationTokenStore) Consume(ctx context.Context, tokenID string) error {
	if !isUUID(tokenID) {
		return ErrVerificationTokenNotFound
	}

	const consumeQuery = `
		UPDATE verification_tokens
		SET consumed_at = now()
		WHERE id = $1 AND consumed_at IS NULL
		RETURNING consumed_at`

	var consumedAt time.Time
	if err := s.pool.QueryRow(ctx, consumeQuery, tokenID).Scan(&consumedAt); err != nil {
		if !errors.Is(err, pgx.ErrNoRows) {
			return fmt.Errorf("auth: consume verification token: %w", err)
		}

		return s.classifyConsumeFailure(ctx, tokenID)
	}

	return nil
}

// classifyConsumeFailure turns an update that matched no row into the right
// error: already consumed, or never existed.
func (s *PostgresVerificationTokenStore) classifyConsumeFailure(ctx context.Context, tokenID string) error {
	const existsQuery = `SELECT consumed_at FROM verification_tokens WHERE id = $1`

	var consumedAt *time.Time
	if err := s.pool.QueryRow(ctx, existsQuery, tokenID).Scan(&consumedAt); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return ErrVerificationTokenNotFound
		}
		return fmt.Errorf("auth: consume verification token: %w", err)
	}

	return ErrVerificationTokenConsumed
}

// scanVerificationToken reads one token row in verificationTokenColumns order.
func scanVerificationToken(row pgx.Row) (*VerificationToken, error) {
	var token VerificationToken

	if err := row.Scan(
		&token.ID,
		&token.UserID,
		&token.TokenHash,
		&token.TokenType,
		&token.ExpiresAt,
		&token.ConsumedAt,
		&token.CreatedAt,
	); err != nil {
		return nil, err
	}

	return &token, nil
}
