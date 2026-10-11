package moderation

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// Page sizes. The service enforces them so the domain, not just the HTTP layer,
// refuses an unbounded query.
const (
	// DefaultListLimit is the page size the API uses when none is requested.
	DefaultListLimit = 20
	// MaxListLimit is the largest page the API will serve.
	MaxListLimit = 50
	// MaxReasonLength bounds the free-text reason on a report.
	MaxReasonLength = 1000
)

// PostgresStore is the pgx-backed implementation of BlockStore, ReportStore, and
// AuditStore. One store satisfies all three because blocks, reports, and the audit
// trail are the same bounded context and the same database.
//
// It holds an injected pool and no other state, so it is safe for concurrent use.
type PostgresStore struct {
	pool *pgxpool.Pool
}

// Compile-time proof that the store satisfies every contract it serves.
var (
	_ BlockStore  = (*PostgresStore)(nil)
	_ ReportStore = (*PostgresStore)(nil)
	_ AuditStore  = (*PostgresStore)(nil)
)

// NewPostgresStore returns a store backed by pool.
func NewPostgresStore(pool *pgxpool.Pool) (*PostgresStore, error) {
	if pool == nil {
		return nil, fmt.Errorf("moderation: postgres store requires a non-nil pool")
	}
	return &PostgresStore{pool: pool}, nil
}

// withTx runs fn inside a transaction, committing on success and rolling back on
// any error (including a panic-free early return).
func (s *PostgresStore) withTx(ctx context.Context, fn func(tx pgx.Tx) error) error {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return fmt.Errorf("moderation: begin transaction: %w", err)
	}
	defer func() {
		// Rollback after a successful commit is a no-op that returns
		// pgx.ErrTxClosed, which is why the error is discarded.
		_ = tx.Rollback(ctx)
	}()

	if err := fn(tx); err != nil {
		return err
	}

	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("moderation: commit transaction: %w", err)
	}
	return nil
}

// nullIfEmpty maps "" onto SQL NULL for a nullable text column.
func nullIfEmpty(value string) *string {
	if value == "" {
		return nil
	}
	return &value
}

// metadataJSON renders an audit entry's metadata as JSON. A nil map becomes the
// empty object, so the NOT NULL DEFAULT '{}' column never receives a SQL NULL.
func metadataJSON(metadata map[string]any) ([]byte, error) {
	if metadata == nil {
		return []byte("{}"), nil
	}
	encoded, err := json.Marshal(metadata)
	if err != nil {
		return nil, fmt.Errorf("moderation: encode audit metadata: %w", err)
	}
	return encoded, nil
}

// isUUID reports whether s is a canonical 8-4-4-4-12 hexadecimal UUID string.
//
// It duplicates the unexported validator in the content packages rather than
// exporting one (KNOT-ADR-010).
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
