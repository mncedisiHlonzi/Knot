package notifications

import (
	"context"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

// notificationColumns is the canonical SELECT/RETURNING column list. It is a
// constant so every query in this file stays consistent with scanNotification,
// and its order is the order that scanner reads.
const notificationColumns = `id, user_id, actor_id, event_type, entity_type, entity_id, read_at, created_at`

// PostgresStore is the pgx-backed implementation of NotificationStore.
//
// It holds an injected pool and no other state, so it is safe for concurrent use.
type PostgresStore struct {
	pool *pgxpool.Pool
}

// Compile-time proof that the store satisfies the domain contract.
var _ NotificationStore = (*PostgresStore)(nil)

// NewPostgresStore returns a store backed by pool. The pool is injected rather
// than reached for globally so that callers own its lifecycle.
func NewPostgresStore(pool *pgxpool.Pool) (*PostgresStore, error) {
	if pool == nil {
		return nil, fmt.Errorf("notifications: postgres store requires a non-nil pool")
	}
	return &PostgresStore{pool: pool}, nil
}

// Create inserts a notification and returns the stored row, including the id and
// timestamp PostgreSQL generated.
func (s *PostgresStore) Create(ctx context.Context, notification Notification) (Notification, error) {
	const query = `
		INSERT INTO notifications (user_id, actor_id, event_type, entity_type, entity_id)
		VALUES ($1, $2, $3, $4, $5)
		RETURNING ` + notificationColumns

	created, err := scanNotification(s.pool.QueryRow(
		ctx,
		query,
		notification.UserID,
		notification.ActorID,
		string(notification.EventType),
		string(notification.EntityType),
		notification.EntityID,
	))
	if err != nil {
		return Notification{}, fmt.Errorf("notifications: insert: %w", err)
	}

	return created, nil
}

// List returns one page of a user's notifications, newest first, plus the cursor
// that resumes after it (nil when this page is the last one).
//
// Pagination is keyset rather than offset-based: the ORDER BY key is
// (created_at DESC, id DESC), the page predicate is the matching row comparison,
// and one extra row is fetched to decide whether more remain. This matches
// notifications_user_created_idx, so PostgreSQL seeks to the resume point instead
// of sorting the table.
func (s *PostgresStore) List(ctx context.Context, userID string, cursor *Cursor, limit int) ([]Notification, *Cursor, error) {
	if !isUUID(userID) {
		return nil, nil, nil
	}

	query := `SELECT ` + notificationColumns + ` FROM notifications WHERE user_id = $1`
	args := []any{userID}

	if cursor != nil {
		// The explicit casts are required because a row comparison does not
		// reliably infer the parameter types, and they are safe because
		// DecodeCursor has already proven the id is canonical UUID text.
		query += ` AND (created_at, id) < ($2::timestamptz, $3::uuid)`
		args = append(args, cursor.CreatedAt(), cursor.ID())
	}

	query += fmt.Sprintf(` ORDER BY created_at DESC, id DESC LIMIT $%d`, len(args)+1)
	args = append(args, limit+1)

	rows, err := s.pool.Query(ctx, query, args...)
	if err != nil {
		return nil, nil, fmt.Errorf("notifications: list: %w", err)
	}
	defer rows.Close()

	page := make([]Notification, 0, limit)
	for rows.Next() {
		notification, err := scanNotification(rows)
		if err != nil {
			return nil, nil, fmt.Errorf("notifications: list: %w", err)
		}
		page = append(page, notification)
	}
	if err := rows.Err(); err != nil {
		return nil, nil, fmt.Errorf("notifications: list: %w", err)
	}

	if len(page) <= limit {
		return page, nil, nil
	}

	last := page[limit-1]
	next := NewCursor(last.CreatedAt, last.ID)

	return page[:limit], &next, nil
}

// UnreadCount returns how many of a user's notifications are unread. The partial
// index notifications_user_unread_idx serves it without reading read rows.
func (s *PostgresStore) UnreadCount(ctx context.Context, userID string) (int, error) {
	if !isUUID(userID) {
		return 0, nil
	}

	var count int
	if err := s.pool.QueryRow(
		ctx,
		`SELECT count(*) FROM notifications WHERE user_id = $1 AND read_at IS NULL`,
		userID,
	).Scan(&count); err != nil {
		return 0, fmt.Errorf("notifications: unread count: %w", err)
	}

	return count, nil
}

// MarkRead marks one of a user's notifications read in a single UPDATE.
//
// COALESCE keeps the original read time when the row is already read, so marking
// twice is idempotent rather than moving the timestamp. A row that does not exist
// for that user updates nothing, which is reported as ErrNotFound.
func (s *PostgresStore) MarkRead(ctx context.Context, id, userID string) error {
	if !isUUID(id) || !isUUID(userID) {
		return ErrNotFound
	}

	tag, err := s.pool.Exec(
		ctx,
		`UPDATE notifications SET read_at = COALESCE(read_at, now()) WHERE id = $1 AND user_id = $2`,
		id,
		userID,
	)
	if err != nil {
		return fmt.Errorf("notifications: mark read: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return ErrNotFound
	}

	return nil
}

// MarkAllRead marks every unread notification for a user read and returns how
// many rows were updated.
func (s *PostgresStore) MarkAllRead(ctx context.Context, userID string) (int, error) {
	if !isUUID(userID) {
		return 0, nil
	}

	tag, err := s.pool.Exec(
		ctx,
		`UPDATE notifications SET read_at = now() WHERE user_id = $1 AND read_at IS NULL`,
		userID,
	)
	if err != nil {
		return 0, fmt.Errorf("notifications: mark all read: %w", err)
	}

	return int(tag.RowsAffected()), nil
}

// rowScanner is the subset of pgx.Row and pgx.Rows that scanNotification needs.
type rowScanner interface {
	Scan(dest ...any) error
}

// scanNotification reads one row of notificationColumns into a Notification.
func scanNotification(row rowScanner) (Notification, error) {
	var (
		notification Notification
		eventType    string
		entityType   string
		readAt       *time.Time
	)

	err := row.Scan(
		&notification.ID,
		&notification.UserID,
		&notification.ActorID,
		&eventType,
		&entityType,
		&notification.EntityID,
		&readAt,
		&notification.CreatedAt,
	)
	if err != nil {
		return Notification{}, err
	}

	notification.EventType = EventType(eventType)
	notification.EntityType = EntityType(entityType)
	notification.ReadAt = readAt

	return notification, nil
}
