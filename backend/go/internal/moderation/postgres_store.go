package moderation

import (
	"context"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
)

// uniqueViolation is the PostgreSQL SQLSTATE raised by a unique constraint
// violation. It is how a duplicate report is recognised.
const uniqueViolation = "23505"

// blockColumns is the canonical SELECT column list for a block.
const blockColumns = `id, blocker_id, blocked_id, created_at`

// reportColumns is the canonical SELECT column list for a report.
const reportColumns = `id, reporter_id, entity_type, entity_id, category, reason, created_at`

// ---------------------------------------------------------------------------
// Blocks
// ---------------------------------------------------------------------------

// CreateBlock records that blockerID blocked blockedID and reports whether a new
// row was written. A repeat is a no-op thanks to the unique pair index.
func (s *PostgresStore) CreateBlock(ctx context.Context, blockerID, blockedID string) (bool, error) {
	const query = `
		INSERT INTO blocks (blocker_id, blocked_id)
		VALUES ($1, $2)
		ON CONFLICT (blocker_id, blocked_id) DO NOTHING
		RETURNING id`

	var id string
	err := s.pool.QueryRow(ctx, query, blockerID, blockedID).Scan(&id)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			// The conflict clause swallowed the insert: already blocked.
			return false, nil
		}
		return false, fmt.Errorf("moderation: create block: %w", err)
	}

	return true, nil
}

// DeleteBlock removes the block blockerID placed on blockedID, if any.
func (s *PostgresStore) DeleteBlock(ctx context.Context, blockerID, blockedID string) error {
	const query = `DELETE FROM blocks WHERE blocker_id = $1 AND blocked_id = $2`

	if _, err := s.pool.Exec(ctx, query, blockerID, blockedID); err != nil {
		return fmt.Errorf("moderation: delete block: %w", err)
	}
	return nil
}

// ListBlocks returns one page of the blocks blockerID created, newest first.
func (s *PostgresStore) ListBlocks(ctx context.Context, blockerID string, cursor *Cursor, limit int) ([]Block, *Cursor, error) {
	query := `SELECT ` + blockColumns + ` FROM blocks WHERE blocker_id = $1`
	args := []any{blockerID}

	if cursor != nil {
		// The explicit casts are required because a row comparison does not
		// reliably infer parameter types; the id is already canonical UUID text.
		query += fmt.Sprintf(` AND (created_at, id) < ($%d::timestamptz, $%d::uuid)`, len(args)+1, len(args)+2)
		args = append(args, cursor.CreatedAt(), cursor.ID())
	}

	query += fmt.Sprintf(` ORDER BY created_at DESC, id DESC LIMIT $%d`, len(args)+1)
	args = append(args, limit+1)

	rows, err := s.pool.Query(ctx, query, args...)
	if err != nil {
		return nil, nil, fmt.Errorf("moderation: list blocks: %w", err)
	}
	defer rows.Close()

	page := make([]Block, 0, limit)
	for rows.Next() {
		block, err := scanBlock(rows)
		if err != nil {
			return nil, nil, fmt.Errorf("moderation: list blocks: %w", err)
		}
		page = append(page, block)
	}
	if err := rows.Err(); err != nil {
		return nil, nil, fmt.Errorf("moderation: list blocks: %w", err)
	}

	if len(page) <= limit {
		return page, nil, nil
	}

	last := page[limit-1]
	next := NewCursor(last.CreatedAt, last.ID)
	return page[:limit], &next, nil
}

// IsBlocked reports whether a block exists between the two users in either
// direction.
func (s *PostgresStore) IsBlocked(ctx context.Context, userA, userB string) (bool, error) {
	const query = `
		SELECT EXISTS (
			SELECT 1 FROM blocks
			WHERE (blocker_id = $1 AND blocked_id = $2)
			   OR (blocker_id = $2 AND blocked_id = $1)
		)`

	var blocked bool
	if err := s.pool.QueryRow(ctx, query, userA, userB).Scan(&blocked); err != nil {
		return false, fmt.Errorf("moderation: is blocked: %w", err)
	}
	return blocked, nil
}

// BlockedPairs reports which of otherIDs are on either side of a block with
// userID, in one query.
func (s *PostgresStore) BlockedPairs(ctx context.Context, userID string, otherIDs []string) (map[string]bool, error) {
	ids := uniqueUUIDs(otherIDs)
	if len(ids) == 0 {
		return map[string]bool{}, nil
	}

	const query = `
		SELECT CASE WHEN blocker_id = $1 THEN blocked_id ELSE blocker_id END AS other
		FROM blocks
		WHERE (blocker_id = $1 AND blocked_id = ANY($2::uuid[]))
		   OR (blocked_id = $1 AND blocker_id = ANY($2::uuid[]))`

	rows, err := s.pool.Query(ctx, query, userID, ids)
	if err != nil {
		return nil, fmt.Errorf("moderation: blocked pairs: %w", err)
	}
	defer rows.Close()

	pairs := make(map[string]bool)
	for rows.Next() {
		var other string
		if err := rows.Scan(&other); err != nil {
			return nil, fmt.Errorf("moderation: blocked pairs: %w", err)
		}
		pairs[other] = true
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("moderation: blocked pairs: %w", err)
	}

	return pairs, nil
}

// BlockedUserIDs returns userID's mutual block set: everyone userID has blocked,
// and everyone who has blocked userID.
func (s *PostgresStore) BlockedUserIDs(ctx context.Context, userID string) ([]string, error) {
	const query = `
		SELECT blocked_id FROM blocks WHERE blocker_id = $1
		UNION
		SELECT blocker_id FROM blocks WHERE blocked_id = $1`

	rows, err := s.pool.Query(ctx, query, userID)
	if err != nil {
		return nil, fmt.Errorf("moderation: blocked user ids: %w", err)
	}
	defer rows.Close()

	ids := make([]string, 0, 8)
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			return nil, fmt.Errorf("moderation: blocked user ids: %w", err)
		}
		ids = append(ids, id)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("moderation: blocked user ids: %w", err)
	}

	return ids, nil
}

// ---------------------------------------------------------------------------
// Reports
// ---------------------------------------------------------------------------

// CreateReport inserts a report and upserts its moderation case in one
// transaction, so a report can never exist without its case.
//
// A unique violation on (reporter_id, entity_type, entity_id) is reported as
// ErrAlreadyReported.
func (s *PostgresStore) CreateReport(ctx context.Context, report Report) (Report, error) {
	var stored Report

	err := s.withTx(ctx, func(tx pgx.Tx) error {
		const insert = `
			INSERT INTO reports (reporter_id, entity_type, entity_id, category, reason)
			VALUES ($1, $2, $3, $4, $5)
			RETURNING ` + reportColumns

		created, err := scanReport(tx.QueryRow(
			ctx, insert,
			report.ReporterID,
			string(report.EntityType),
			report.EntityID,
			string(report.Category),
			nullIfEmpty(report.Reason),
		))
		if err != nil {
			var pgErr *pgconn.PgError
			if errors.As(err, &pgErr) && pgErr.Code == uniqueViolation {
				return ErrAlreadyReported
			}
			return fmt.Errorf("moderation: create report: %w", err)
		}

		if err := upsertCase(ctx, tx, report.EntityType, report.EntityID); err != nil {
			return err
		}

		stored = *created
		return nil
	})
	if err != nil {
		return Report{}, err
	}

	return stored, nil
}

// UpsertCaseForEntity creates the moderation case for an entity, or refreshes its
// report count and updated_at. It exists for 017b.
func (s *PostgresStore) UpsertCaseForEntity(ctx context.Context, entityType EntityType, entityID string) error {
	return s.withTx(ctx, func(tx pgx.Tx) error {
		return upsertCase(ctx, tx, entityType, entityID)
	})
}

// upsertCase inserts the case for an entity or, when it already exists,
// increments its report_count and refreshes updated_at. It runs on the caller's
// transaction so CreateReport's insert and its case move together.
func upsertCase(ctx context.Context, tx pgx.Tx, entityType EntityType, entityID string) error {
	const query = `
		INSERT INTO moderation_cases (entity_type, entity_id, report_count, status, updated_at)
		VALUES ($1, $2, 1, 'open', now())
		ON CONFLICT (entity_type, entity_id)
		DO UPDATE SET report_count = moderation_cases.report_count + 1, updated_at = now()`

	if _, err := tx.Exec(ctx, query, string(entityType), entityID); err != nil {
		return fmt.Errorf("moderation: upsert case: %w", err)
	}
	return nil
}

// ListMyReports returns one page of a user's own reports, newest first.
func (s *PostgresStore) ListMyReports(ctx context.Context, reporterID string, cursor *Cursor, limit int) ([]Report, *Cursor, error) {
	query := `SELECT ` + reportColumns + ` FROM reports WHERE reporter_id = $1`
	args := []any{reporterID}

	if cursor != nil {
		query += fmt.Sprintf(` AND (created_at, id) < ($%d::timestamptz, $%d::uuid)`, len(args)+1, len(args)+2)
		args = append(args, cursor.CreatedAt(), cursor.ID())
	}

	query += fmt.Sprintf(` ORDER BY created_at DESC, id DESC LIMIT $%d`, len(args)+1)
	args = append(args, limit+1)

	rows, err := s.pool.Query(ctx, query, args...)
	if err != nil {
		return nil, nil, fmt.Errorf("moderation: list my reports: %w", err)
	}
	defer rows.Close()

	page := make([]Report, 0, limit)
	for rows.Next() {
		report, err := scanReport(rows)
		if err != nil {
			return nil, nil, fmt.Errorf("moderation: list my reports: %w", err)
		}
		page = append(page, *report)
	}
	if err := rows.Err(); err != nil {
		return nil, nil, fmt.Errorf("moderation: list my reports: %w", err)
	}

	if len(page) <= limit {
		return page, nil, nil
	}

	last := page[limit-1]
	next := NewCursor(last.CreatedAt, last.ID)
	return page[:limit], &next, nil
}

// ---------------------------------------------------------------------------
// Audit log
// ---------------------------------------------------------------------------

// Log appends one audit entry.
func (s *PostgresStore) Log(ctx context.Context, actorID, action, targetType, targetID string, metadata map[string]any) error {
	encoded, err := metadataJSON(metadata)
	if err != nil {
		return err
	}

	const query = `
		INSERT INTO audit_log (actor_id, action, target_type, target_id, metadata)
		VALUES ($1, $2, $3, $4, $5)`

	if _, err := s.pool.Exec(ctx, query, actorID, action, targetType, nullIfEmpty(targetID), encoded); err != nil {
		return fmt.Errorf("moderation: log audit: %w", err)
	}
	return nil
}

// ---------------------------------------------------------------------------
// Scanners and helpers
// ---------------------------------------------------------------------------

// rowScanner is the subset of pgx.Row and pgx.Rows the scanners need.
type rowScanner interface {
	Scan(dest ...any) error
}

// scanBlock reads one row of blockColumns into a Block.
func scanBlock(row rowScanner) (Block, error) {
	var block Block
	if err := row.Scan(&block.ID, &block.BlockerID, &block.BlockedID, &block.CreatedAt); err != nil {
		return Block{}, err
	}
	return block, nil
}

// scanReport reads one row of reportColumns into a Report. The nullable reason is
// mapped to "".
func scanReport(row rowScanner) (*Report, error) {
	var (
		report     Report
		entityType string
		category   string
		reason     *string
	)
	if err := row.Scan(
		&report.ID,
		&report.ReporterID,
		&entityType,
		&report.EntityID,
		&category,
		&reason,
		&report.CreatedAt,
	); err != nil {
		return nil, err
	}

	report.EntityType = EntityType(entityType)
	report.Category = Category(category)
	if reason != nil {
		report.Reason = *reason
	}
	return &report, nil
}

// uniqueUUIDs returns the distinct, canonical-UUID entries of ids in first-seen
// order, dropping malformed and empty values. It keeps a parameter list from
// carrying duplicates and from ever holding a non-UUID.
func uniqueUUIDs(ids []string) []string {
	seen := make(map[string]struct{}, len(ids))
	out := make([]string, 0, len(ids))
	for _, id := range ids {
		if !isUUID(id) {
			continue
		}
		if _, ok := seen[id]; ok {
			continue
		}
		seen[id] = struct{}{}
		out = append(out, id)
	}
	return out
}
