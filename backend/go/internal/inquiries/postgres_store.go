package inquiries

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
)

// foreignKeyViolation is the PostgreSQL SQLSTATE raised by a foreign key
// violation. It is how an inquiry or answer whose author does not exist is
// recognised.
const foreignKeyViolation = "23503"

// inquiryAuthorFK and answerAuthorFK name the foreign keys that point at
// users(id). Matching on the constraint name keeps a violation of some other key
// — the answer's own inquiry_id, for instance — from being mistaken for a missing
// user.
const (
	inquiryAuthorFK = "inquiries_author_id_fkey"
	answerAuthorFK  = "inquiry_answers_author_id_fkey"
)

// inquiryColumns is the canonical SELECT/RETURNING column list. It is a constant
// so every query in this file stays consistent with scanInquiry, and its order is
// the order scanInquiry reads.
const inquiryColumns = `id, author_id, title, body, language, place, place_country, latitude, longitude, answer_count, created_at, updated_at`

// answerColumns is the canonical SELECT/RETURNING column list for answers, and the
// order scanAnswer reads.
const answerColumns = `id, inquiry_id, author_id, language, body, created_at, updated_at`

// PostgresStore is the pgx-backed implementation of InquiryStore.
//
// It holds an injected pool and no other state, so it is safe for concurrent use.
type PostgresStore struct {
	pool *pgxpool.Pool
}

// Compile-time proof that the store satisfies the domain contract.
var _ InquiryStore = (*PostgresStore)(nil)

// NewPostgresStore returns a store backed by pool. The pool is injected rather
// than reached for globally so that callers own its lifecycle.
func NewPostgresStore(pool *pgxpool.Pool) (*PostgresStore, error) {
	if pool == nil {
		return nil, fmt.Errorf("inquiries: postgres store requires a non-nil pool")
	}
	return &PostgresStore{pool: pool}, nil
}

// CreateInquiry stores an inquiry and returns it with the id, the zero answer
// count, and the timestamps PostgreSQL assigned.
//
// An inquiry for a user that does not exist is reported as ErrUserNotFound rather
// than as a raw driver error, so the handler can answer 404.
func (s *PostgresStore) CreateInquiry(ctx context.Context, inquiry Inquiry) (Inquiry, error) {
	const query = `
		INSERT INTO inquiries (author_id, title, body, language, place, place_country, latitude, longitude)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8)
		RETURNING ` + inquiryColumns

	stored, err := scanInquiry(s.pool.QueryRow(
		ctx,
		query,
		inquiry.AuthorID,
		inquiry.Title,
		inquiry.Body,
		inquiry.Language,
		inquiry.Place,
		inquiry.PlaceCountry,
		inquiry.Latitude,
		inquiry.Longitude,
	))
	if err != nil {
		if isForeignKeyViolation(err, inquiryAuthorFK) {
			return Inquiry{}, ErrUserNotFound
		}
		return Inquiry{}, fmt.Errorf("inquiries: create inquiry: %w", err)
	}

	return stored, nil
}

// GetInquiry returns one inquiry, or ErrNotFound when it does not exist. A
// malformed id is reported as ErrNotFound rather than as a database error.
func (s *PostgresStore) GetInquiry(ctx context.Context, id string) (Inquiry, error) {
	if !isUUID(id) {
		return Inquiry{}, ErrNotFound
	}

	const query = `SELECT ` + inquiryColumns + ` FROM inquiries WHERE id = $1`

	inquiry, err := scanInquiry(s.pool.QueryRow(ctx, query, id))
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return Inquiry{}, ErrNotFound
		}
		return Inquiry{}, fmt.Errorf("inquiries: get inquiry: %w", err)
	}

	return inquiry, nil
}

// ListInquiries returns one page of inquiries, newest first, plus the cursor that
// resumes after it (nil when this page is the last one).
//
// Pagination is keyset over (created_at DESC, id DESC), so the next page is
// everything strictly "before" the last row of this page in that order. The id
// tiebreaker is what makes the page correct when two questions share a timestamp.
//
// A non-empty place adds an equality filter, which is the "questions about this
// place" read. The filter is an exact match on the stored place — the same
// spelling an inquiry was given — and is served by inquiries_place_idx.
//
// One extra row is fetched to decide whether more remain; that row is not
// returned, and the cursor is derived from the last row that is.
func (s *PostgresStore) ListInquiries(ctx context.Context, cursor *Cursor, limit int, place string) ([]Inquiry, *Cursor, error) {
	query := `SELECT ` + inquiryColumns + ` FROM inquiries`
	args := make([]any, 0, 4)
	conditions := make([]string, 0, 2)

	if place != "" {
		args = append(args, place)
		conditions = append(conditions, fmt.Sprintf("place = $%d", len(args)))
	}

	if cursor != nil {
		// The explicit casts are required because a row comparison does not
		// reliably infer the parameter types, and they are safe because
		// DecodeCursor has already proven the id is canonical UUID text.
		args = append(args, cursor.CreatedAt(), cursor.ID())
		conditions = append(conditions, fmt.Sprintf("(created_at, id) < ($%d::timestamptz, $%d::uuid)", len(args)-1, len(args)))
	}

	if len(conditions) > 0 {
		query += " WHERE " + strings.Join(conditions, " AND ")
	}

	args = append(args, limit+1)
	query += fmt.Sprintf(" ORDER BY created_at DESC, id DESC LIMIT $%d", len(args))

	rows, err := s.pool.Query(ctx, query, args...)
	if err != nil {
		return nil, nil, fmt.Errorf("inquiries: list inquiries: %w", err)
	}
	defer rows.Close()

	page := make([]Inquiry, 0, limit)
	for rows.Next() {
		inquiry, err := scanInquiry(rows)
		if err != nil {
			return nil, nil, fmt.Errorf("inquiries: list inquiries: %w", err)
		}
		page = append(page, inquiry)
	}
	if err := rows.Err(); err != nil {
		return nil, nil, fmt.Errorf("inquiries: list inquiries: %w", err)
	}

	if len(page) <= limit {
		// Nothing was truncated, so this page is the last one.
		return page, nil, nil
	}

	last := page[limit-1]
	next := NewCursor(last.CreatedAt, last.ID)

	return page[:limit], &next, nil
}

// CreateAnswer appends an answer and increments its inquiry's answer count, in one
// transaction.
//
// It returns the stored answer and the id of the inquiry's author. The author is
// read under the same lock that guards the count, so the caller can notify the
// asker without a second lookup and without racing a concurrent edit — and so a
// missing inquiry is reported as ErrNotFound before anything is written.
//
// The row lock is on inquiries.id — one stable row, and the entity whose count is
// being changed — not on inquiry_answers rows. It serialises every answer writer
// for that inquiry, so two answers arriving at once cannot both read the same count
// and write the same incremented value (the KNOT-ADR-054 pattern).
//
// `updated_at` on the inquiry is deliberately NOT touched: an answer is new
// activity on the inquiry rather than an edit of it, and the wall orders by
// created_at.
func (s *PostgresStore) CreateAnswer(ctx context.Context, answer Answer) (Answer, string, error) {
	if !isUUID(answer.InquiryID) {
		return Answer{}, "", ErrNotFound
	}

	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return Answer{}, "", fmt.Errorf("inquiries: begin create answer: %w", err)
	}
	// A rollback after a successful commit is a no-op, so this covers every early
	// return.
	defer func() { _ = tx.Rollback(ctx) }()

	var inquiryAuthorID string
	if err := tx.QueryRow(
		ctx,
		`SELECT author_id FROM inquiries WHERE id = $1 FOR UPDATE`,
		answer.InquiryID,
	).Scan(&inquiryAuthorID); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return Answer{}, "", ErrNotFound
		}
		return Answer{}, "", fmt.Errorf("inquiries: lock inquiry: %w", err)
	}

	const insert = `
		INSERT INTO inquiry_answers (inquiry_id, author_id, language, body)
		VALUES ($1, $2, $3, $4)
		RETURNING ` + answerColumns

	stored, err := scanAnswer(tx.QueryRow(
		ctx,
		insert,
		answer.InquiryID,
		answer.AuthorID,
		answer.Language,
		answer.Body,
	))
	if err != nil {
		if isForeignKeyViolation(err, answerAuthorFK) {
			return Answer{}, "", ErrUserNotFound
		}
		return Answer{}, "", fmt.Errorf("inquiries: create answer: %w", err)
	}

	if _, err := tx.Exec(
		ctx,
		`UPDATE inquiries SET answer_count = answer_count + 1 WHERE id = $1`,
		answer.InquiryID,
	); err != nil {
		return Answer{}, "", fmt.Errorf("inquiries: increment answer count: %w", err)
	}

	if err := tx.Commit(ctx); err != nil {
		return Answer{}, "", fmt.Errorf("inquiries: commit create answer: %w", err)
	}

	return stored, inquiryAuthorID, nil
}

// ListAnswers returns one page of an inquiry's answers, oldest first, plus the
// cursor that resumes after it (nil when this page is the last one).
//
// An answer thread reads as a conversation, so the order is (created_at ASC,
// id ASC) — the reverse of the inquiry list — and the cursor comparison is a
// strict "greater than" to match.
func (s *PostgresStore) ListAnswers(ctx context.Context, inquiryID string, cursor *Cursor, limit int) ([]Answer, *Cursor, error) {
	if !isUUID(inquiryID) {
		return []Answer{}, nil, nil
	}

	query := `SELECT ` + answerColumns + ` FROM inquiry_answers WHERE inquiry_id = $1`
	args := []any{inquiryID}

	if cursor != nil {
		args = append(args, cursor.CreatedAt(), cursor.ID())
		query += fmt.Sprintf(" AND (created_at, id) > ($%d::timestamptz, $%d::uuid)", len(args)-1, len(args))
	}

	args = append(args, limit+1)
	query += fmt.Sprintf(" ORDER BY created_at ASC, id ASC LIMIT $%d", len(args))

	rows, err := s.pool.Query(ctx, query, args...)
	if err != nil {
		return nil, nil, fmt.Errorf("inquiries: list answers: %w", err)
	}
	defer rows.Close()

	page := make([]Answer, 0, limit)
	for rows.Next() {
		answer, err := scanAnswer(rows)
		if err != nil {
			return nil, nil, fmt.Errorf("inquiries: list answers: %w", err)
		}
		page = append(page, answer)
	}
	if err := rows.Err(); err != nil {
		return nil, nil, fmt.Errorf("inquiries: list answers: %w", err)
	}

	if len(page) <= limit {
		return page, nil, nil
	}

	last := page[limit-1]
	next := NewCursor(last.CreatedAt, last.ID)

	return page[:limit], &next, nil
}

// GetAnswer returns one answer, or ErrNotFound when it does not exist.
func (s *PostgresStore) GetAnswer(ctx context.Context, id string) (Answer, error) {
	if !isUUID(id) {
		return Answer{}, ErrNotFound
	}

	const query = `SELECT ` + answerColumns + ` FROM inquiry_answers WHERE id = $1`

	answer, err := scanAnswer(s.pool.QueryRow(ctx, query, id))
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return Answer{}, ErrNotFound
		}
		return Answer{}, fmt.Errorf("inquiries: get answer: %w", err)
	}

	return answer, nil
}

// rowScanner is the subset of pgx.Row and pgx.Rows that the scan helpers need.
type rowScanner interface {
	Scan(dest ...any) error
}

// scanInquiry reads one row of inquiryColumns into an Inquiry. It returns the
// driver error unwrapped so callers can classify it (for example as a foreign key
// violation, or as pgx.ErrNoRows).
func scanInquiry(row rowScanner) (Inquiry, error) {
	var inquiry Inquiry

	err := row.Scan(
		&inquiry.ID,
		&inquiry.AuthorID,
		&inquiry.Title,
		&inquiry.Body,
		&inquiry.Language,
		&inquiry.Place,
		&inquiry.PlaceCountry,
		&inquiry.Latitude,
		&inquiry.Longitude,
		&inquiry.AnswerCount,
		&inquiry.CreatedAt,
		&inquiry.UpdatedAt,
	)
	if err != nil {
		return Inquiry{}, err
	}

	return inquiry, nil
}

// scanAnswer reads one row of answerColumns into an Answer.
func scanAnswer(row rowScanner) (Answer, error) {
	var answer Answer

	err := row.Scan(
		&answer.ID,
		&answer.InquiryID,
		&answer.AuthorID,
		&answer.Language,
		&answer.Body,
		&answer.CreatedAt,
		&answer.UpdatedAt,
	)
	if err != nil {
		return Answer{}, err
	}

	return answer, nil
}

// isForeignKeyViolation reports whether err is a foreign key violation on the
// named constraint.
func isForeignKeyViolation(err error, constraint string) bool {
	var pgErr *pgconn.PgError
	return errors.As(err, &pgErr) && pgErr.Code == foreignKeyViolation && pgErr.ConstraintName == constraint
}
