package search

import (
	"context"
	"fmt"
	"strings"
	"unicode"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/knot/backend/internal/moderation"
)

// PostgresStore is the pgx-backed implementation of SearchStore.
//
// It holds an injected pool and no other state, so it is safe for concurrent use.
// It reads `stories`, `story_versions`, and `inquiries` directly rather than going
// through the owning domains: search is a different read shape over the same data
// (one ranked list across three tables), and routing it through three services
// would mean three round trips and a merge in Go.
type PostgresStore struct {
	pool *pgxpool.Pool
}

// Compile-time proof that the store satisfies the domain contract.
var _ SearchStore = (*PostgresStore)(nil)

// NewPostgresStore returns a store backed by pool. The pool is injected rather
// than reached for globally so that callers own its lifecycle.
func NewPostgresStore(pool *pgxpool.Pool) (*PostgresStore, error) {
	if pool == nil {
		return nil, fmt.Errorf("search: postgres store requires a non-nil pool")
	}
	return &PostgresStore{pool: pool}, nil
}

// The parameter layout is fixed so the same placeholder can be reused inside all
// three subqueries. PostgreSQL allows a parameter to appear any number of times,
// and every value below is bound, never interpolated:
//
//	$1  phrase       text        "" disables the text match (a filter-only search)
//	$2  excluded     uuid[]      the viewer's hidden authors; an empty array hides
//	                             nobody, which is why no dynamic SQL is needed
//	$3  pillar       text        "" disables the pillar filter
//	$4  language     text        "" disables the language filter
//	$5  author       text        "" disables the author filter
//	$6  place        text        "" disables the place filter
//	$7  preview_len  int4        how much of the body a preview carries
//	$8  tsquery      text        always a syntactically valid tsquery
//	$9  cursor rank  float8      appended only when a cursor is present
//	$10 cursor time  timestamptz appended only when a cursor is present
//	$11 cursor id    text        appended only when a cursor is present
//	$n  limit        int4        always the final argument
//
// Ids are cast to text in the projection. That keeps the union's columns a single
// type across the three subqueries and lets the keyset comparison compare text to
// text; the values are canonical lower-case UUIDs either way, so the ordering is
// the same as a uuid comparison would give.

// storyUnion matches a story through its root version.
//
// A story row carries no title or body (KNOT-ADR-020), so the searchable text
// comes from `story_versions` where `parent_version_id IS NULL`, joined on
// `stories.root_version_id`. The result is the story — its id, its pillar, and its
// author — so a client opens the story detail.
const storyUnion = `
		SELECT
			'story' AS kind,
			s.id::text AS id,
			s.id::text AS story_id,
			sv.title AS title,
			left(sv.body, $7) AS preview,
			sv.language AS language,
			s.pillar AS pillar,
			s.approximate_location AS place,
			s.author_id::text AS author_id,
			s.created_at AS created_at,
			CASE WHEN $1 = '' THEN 0::float8
				ELSE ts_rank(sv.search_vector, to_tsquery('simple', $8))::float8
			END AS search_rank
		FROM stories s
		JOIN story_versions sv ON sv.id = s.root_version_id
		WHERE s.author_id <> ALL($2::uuid[])
			AND ($1 = '' OR sv.search_vector @@ to_tsquery('simple', $8))
			AND ($3 = '' OR s.pillar = $3)
			AND ($4 = '' OR sv.language = $4)
			AND ($5 = '' OR s.author_id = $5::uuid)
			AND ($6 = '' OR s.approximate_location = $6)`

// versionUnion matches an adaptation: any version that is not its story's root.
//
// The joined story supplies the pillar and the place, which live on the story
// rather than on the version.
const versionUnion = `
		SELECT
			'version' AS kind,
			sv.id::text AS id,
			sv.story_id::text AS story_id,
			sv.title AS title,
			left(sv.body, $7) AS preview,
			sv.language AS language,
			s.pillar AS pillar,
			s.approximate_location AS place,
			sv.author_id::text AS author_id,
			sv.created_at AS created_at,
			CASE WHEN $1 = '' THEN 0::float8
				ELSE ts_rank(sv.search_vector, to_tsquery('simple', $8))::float8
			END AS search_rank
		FROM story_versions sv
		JOIN stories s ON s.id = sv.story_id
		WHERE sv.parent_version_id IS NOT NULL
			AND sv.author_id <> ALL($2::uuid[])
			AND ($1 = '' OR sv.search_vector @@ to_tsquery('simple', $8))
			AND ($3 = '' OR s.pillar = $3)
			AND ($4 = '' OR sv.language = $4)
			AND ($5 = '' OR sv.author_id = $5::uuid)
			AND ($6 = '' OR s.approximate_location = $6)`

// inquiryUnion matches a question.
//
// The pillar filter is expressed positively here: an inquiry is considered only
// when the pillar parameter is empty, because an inquiry has no pillar — so asking
// for a pillar excludes inquiries rather than matching none of them explicitly.
const inquiryUnion = `
		SELECT
			'inquiry' AS kind,
			i.id::text AS id,
			NULL::text AS story_id,
			i.title AS title,
			left(i.body, $7) AS preview,
			i.language AS language,
			NULL::text AS pillar,
			i.place AS place,
			i.author_id::text AS author_id,
			i.created_at AS created_at,
			CASE WHEN $1 = '' THEN 0::float8
				ELSE ts_rank(i.search_vector, to_tsquery('simple', $8))::float8
			END AS search_rank
		FROM inquiries i
		WHERE i.author_id <> ALL($2::uuid[])
			AND ($1 = '' OR i.search_vector @@ to_tsquery('simple', $8))
			AND $3 = ''
			AND ($4 = '' OR i.language = $4)
			AND ($5 = '' OR i.author_id = $5::uuid)
			AND ($6 = '' OR i.place = $6)`

// resultColumns is the outer projection, in the order scanResult reads. It is a
// constant so the wrapper and the scanner cannot drift apart.
const resultColumns = `kind, id, story_id, title, preview, language, pillar, place, author_id, created_at, search_rank`

// Search returns one page of ranked results, newest-breaking relevance ties, plus
// the cursor that resumes after it (nil when this page is the last one).
//
// Ordering is (search_rank DESC, created_at DESC, id DESC). Relevance leads because
// that is what a search is for; the other two make the order total, so the keyset
// comparison can never skip or repeat a row within one page walk. Rank is a
// property of the matched document, so it can change between pages while content is
// being written; the trade-off is disclosed in docs/DEVELOPMENT.md.
//
// One extra row is fetched to decide whether more remain; that row is not
// returned, and the cursor is derived from the last row that is.
func (s *PostgresStore) Search(ctx context.Context, query StoreQuery) ([]Result, *Cursor, error) {
	sql, args := buildSearchQuery(ctx, query)

	rows, err := s.pool.Query(ctx, sql, args...)
	if err != nil {
		return nil, nil, fmt.Errorf("search: search: %w", err)
	}
	defer rows.Close()

	page := make([]Result, 0, query.Limit)
	for rows.Next() {
		result, err := scanResult(rows)
		if err != nil {
			return nil, nil, fmt.Errorf("search: search: %w", err)
		}
		page = append(page, result)
	}
	if err := rows.Err(); err != nil {
		return nil, nil, fmt.Errorf("search: search: %w", err)
	}

	if len(page) <= query.Limit {
		// Nothing was truncated, so this page is the last one.
		return page, nil, nil
	}

	last := page[query.Limit-1]
	next := NewCursor(last.Rank, last.CreatedAt, last.ID)

	return page[:query.Limit], &next, nil
}

// buildSearchQuery assembles the SQL and its arguments for one search.
//
// Only two things vary: which of the three subqueries is included (the `type`
// filter) and whether a keyset clause is added (a cursor). Neither introduces a
// value into the statement text — the union members are fixed constants and the
// keyset clause contributes placeholders only — so every client-supplied value
// travels as a bound parameter.
func buildSearchQuery(ctx context.Context, query StoreQuery) (string, []any) {
	excluded := moderation.ExcludedAuthors(ctx)
	if excluded == nil {
		// A nil slice binds as SQL NULL, and `author_id <> ALL(NULL)` is NULL for
		// every row, which would hide the whole corpus. An anonymous reader hides
		// nobody, which is the empty array.
		excluded = []string{}
	}

	args := make([]any, 0, 12)
	args = append(args,
		query.Query,
		excluded,
		query.Pillar,
		query.Language,
		query.AuthorID,
		query.Place,
		PreviewLength,
		tsQuery(query.Query),
	)

	unions := make([]string, 0, 3)
	if query.Kind == "" || query.Kind == KindStory {
		unions = append(unions, storyUnion)
	}
	if query.Kind == "" || query.Kind == KindVersion {
		unions = append(unions, versionUnion)
	}
	if query.Kind == "" || query.Kind == KindInquiry {
		unions = append(unions, inquiryUnion)
	}

	statement := "SELECT " + resultColumns + " FROM (" + strings.Join(unions, "\n\t\tUNION ALL") + "\n\t) AS results"

	if query.Cursor != nil {
		statement += fmt.Sprintf(
			"\n\t\tWHERE (search_rank, created_at, id) < ($%d::float8, $%d::timestamptz, $%d::text)",
			len(args)+1, len(args)+2, len(args)+3,
		)
		args = append(args, query.Cursor.Rank(), query.Cursor.CreatedAt(), query.Cursor.ID())
	}

	statement += fmt.Sprintf("\n\t\tORDER BY search_rank DESC, created_at DESC, id DESC\n\t\tLIMIT $%d", len(args)+1)
	args = append(args, query.Limit+1)

	return statement, args
}

// unmatchableTsQuery is a syntactically valid tsquery that matches no document. It
// stands in when a phrase contains no letters or digits, so the text clause can
// stay in the statement without changing the search's meaning.
const unmatchableTsQuery = "knotunmatchabletermzzz"

// tsQuery turns a search phrase into a tsquery that is always syntactically valid.
//
// Every term is a **prefix** term (`mang:*`), so "mang" matches "Manguzi" and
// "manguzi" matches it too. Prefix matching is a superset of exact-word matching,
// which is why there is no separate exact clause: it delivers partial-word search
// without a trigram index or a second query path (KNOT-ADR-065).
//
// The phrase is reduced to letters and digits, so no character that is meaningful
// in tsquery syntax (`&`, `|`, `!`, `(`, `)`, `:*`, quotes) can reach the parser
// and raise an error on user input. Non-ASCII letters are kept: the `simple`
// configuration does not transliterate, so a word written in another script stays
// a lexeme of its own.
func tsQuery(phrase string) string {
	terms := make([]string, 0, 4)
	var current strings.Builder

	flush := func() {
		if current.Len() == 0 {
			return
		}
		terms = append(terms, current.String()+":*")
		current.Reset()
	}

	for _, r := range strings.ToLower(phrase) {
		if isTermRune(r) {
			current.WriteRune(r)
			continue
		}
		flush()
	}
	flush()

	if len(terms) == 0 {
		return unmatchableTsQuery
	}

	return strings.Join(terms, " & ")
}

// isTermRune reports whether r may appear in a tsquery term. Only letters and
// digits may, in any script.
func isTermRune(r rune) bool {
	return unicode.IsLetter(r) || unicode.IsDigit(r)
}

// rowScanner is the subset of pgx.Row and pgx.Rows that scanResult needs.
type rowScanner interface {
	Scan(dest ...any) error
}

// scanResult reads one row of resultColumns into a Result.
func scanResult(row rowScanner) (Result, error) {
	var (
		result  Result
		kind    string
		pillar  *string
		place   *string
		storyID *string
	)

	err := row.Scan(
		&kind,
		&result.ID,
		&storyID,
		&result.Title,
		&result.Preview,
		&result.Language,
		&pillar,
		&place,
		&result.AuthorID,
		&result.CreatedAt,
		&result.Rank,
	)
	if err != nil {
		return Result{}, err
	}

	result.Kind = Kind(kind)
	result.StoryID = storyID
	result.Pillar = pillar
	result.Place = place

	return result, nil
}
