package moderation

import (
	"context"
	"fmt"
	"strings"
	"time"
)

// Category is why an entity was reported. The set is closed: it is a CHECK
// constraint in the 0015 migration, and a value outside it cannot be stored
// (KNOT-ADR-062).
type Category string

// The six report categories.
const (
	// CategoryHarassment is targeted abuse of a person.
	CategoryHarassment Category = "harassment"
	// CategoryHateSpeech is content attacking a protected group.
	CategoryHateSpeech Category = "hate_speech"
	// CategoryMisinformation is content presented as fact that is not.
	CategoryMisinformation Category = "misinformation"
	// CategorySpam is unsolicited or repetitive content.
	CategorySpam Category = "spam"
	// CategorySensitiveContent is content that needs care before it is surfaced.
	CategorySensitiveContent Category = "sensitive_content"
	// CategoryOther is anything else. It requires a free-text reason.
	CategoryOther Category = "other"
)

// Valid reports whether c is one of the six categories.
func (c Category) Valid() bool {
	switch c {
	case CategoryHarassment, CategoryHateSpeech, CategoryMisinformation,
		CategorySpam, CategorySensitiveContent, CategoryOther:
		return true
	default:
		return false
	}
}

// EntityType is the kind of thing a report names, and the kind the moderation
// queue aggregates cases by. It mirrors the notifications entity set, widened
// with 'inquiry_answer' because every answer is separately reportable.
type EntityType string

// The six reportable entity kinds.
const (
	// EntityStory is a story.
	EntityStory EntityType = "story"
	// EntityVersion is a story version.
	EntityVersion EntityType = "version"
	// EntityComment is a comment.
	EntityComment EntityType = "comment"
	// EntityBridge is a bridge.
	EntityBridge EntityType = "bridge"
	// EntityInquiry is a question about a place.
	EntityInquiry EntityType = "inquiry"
	// EntityInquiryAnswer is one answer to an inquiry.
	EntityInquiryAnswer EntityType = "inquiry_answer"
)

// Valid reports whether t is one of the six reportable entity kinds.
func (t EntityType) Valid() bool {
	switch t {
	case EntityStory, EntityVersion, EntityComment, EntityBridge, EntityInquiry, EntityInquiryAnswer:
		return true
	default:
		return false
	}
}

// Report is one user's report of one entity.
type Report struct {
	// ID is the canonical UUID text assigned by PostgreSQL.
	ID string
	// ReporterID is the user who filed the report.
	ReporterID string
	// EntityType is the kind of entity reported.
	EntityType EntityType
	// EntityID is the id of the reported entity.
	EntityID string
	// Category is why it was reported.
	Category Category
	// Reason is the free-text explanation. It is required when Category is
	// CategoryOther and optional otherwise; "" when absent.
	Reason string
	// CreatedAt is when the report was filed.
	CreatedAt time.Time
}

// CreateReportInput is the input to CreateReport. It is a domain type, not an
// HTTP type, so the handler layer stays free of validation rules.
type CreateReportInput struct {
	// ReporterID is the authenticated user. Self-reports are allowed: a user may
	// flag their own content for a moderator.
	ReporterID string
	// EntityType is required and must be one of the six kinds.
	EntityType EntityType
	// EntityID is required and must be canonical UUID text naming an existing
	// entity of that kind.
	EntityID string
	// Category is required and must be one of the six categories.
	Category Category
	// Reason is required when Category is CategoryOther, optional otherwise.
	Reason string
}

// EntityLookup reports whether a reported entity exists. It is implemented by the
// HTTP layer, which can resolve any of the six kinds through the content services
// it already holds, so this package never imports a content domain (KNOT-ADR-040
// pattern).
type EntityLookup interface {
	// EntityExists reports whether an entity of the given kind with the given id
	// exists. A malformed id is simply not found.
	EntityExists(ctx context.Context, entityType EntityType, entityID string) (bool, error)
}

// ReportStore is the persistence contract for reports. Creating a report and
// upserting its moderation case happen in one transaction, so a report can never
// exist without its case (KNOT-ADR-061).
type ReportStore interface {
	// CreateReport inserts the report and upserts the moderation case for its
	// entity, incrementing the case's report_count, in one transaction. It returns
	// the stored report, or ErrAlreadyReported when this user already reported
	// this entity.
	CreateReport(ctx context.Context, report Report) (Report, error)
	// ListMyReports returns one page of a user's own reports, newest first,
	// starting after cursor, plus the cursor that resumes after the page (nil when
	// the page is the last one).
	ListMyReports(ctx context.Context, reporterID string, cursor *Cursor, limit int) ([]Report, *Cursor, error)
	// UpsertCaseForEntity creates the moderation case for an entity, or refreshes
	// its updated_at when it already exists. It exists for 017b, which re-opens a
	// case; CreateReport already calls it inside its transaction.
	UpsertCaseForEntity(ctx context.Context, entityType EntityType, entityID string) error
}

// ReportService holds the report business rules.
type ReportService struct {
	reports  ReportStore
	entities EntityLookup
	audit    Auditor
}

// NewReportService wires a report store, an entity lookup, and an audit trail
// into the report domain.
func NewReportService(reports ReportStore, entities EntityLookup, audit Auditor) (*ReportService, error) {
	if reports == nil {
		return nil, fmt.Errorf("moderation: report service requires a report store")
	}
	if entities == nil {
		return nil, fmt.Errorf("moderation: report service requires an entity lookup")
	}
	if audit == nil {
		return nil, fmt.Errorf("moderation: report service requires an auditor")
	}
	return &ReportService{reports: reports, entities: entities, audit: audit}, nil
}

// CreateReport validates the input, confirms the entity exists, and stores the
// report together with its aggregated moderation case.
//
// Self-reports are allowed (disclosed): a user flagging their own content for a
// moderator is a legitimate request. It returns ErrAlreadyReported when the user
// has already reported the entity, and ErrEntityNotFound when the entity does not
// exist.
func (s *ReportService) CreateReport(ctx context.Context, in CreateReportInput) (Report, error) {
	if !isUUID(in.ReporterID) {
		return Report{}, &ValidationError{Field: "reporter", Message: "must be a valid user id"}
	}
	if !in.EntityType.Valid() {
		return Report{}, &ValidationError{Field: "entity_type", Message: "must be one of story, version, comment, bridge, inquiry, inquiry_answer"}
	}
	if !isUUID(in.EntityID) {
		return Report{}, &ValidationError{Field: "entity_id", Message: "must be a valid entity id"}
	}
	if !in.Category.Valid() {
		return Report{}, &ValidationError{Field: "category", Message: "must be one of harassment, hate_speech, misinformation, spam, sensitive_content, other"}
	}

	reason := strings.TrimSpace(in.Reason)
	if in.Category == CategoryOther && reason == "" {
		return Report{}, &ValidationError{Field: "reason", Message: "is required when the category is other"}
	}
	if len(reason) > MaxReasonLength {
		return Report{}, &ValidationError{Field: "reason", Message: fmt.Sprintf("must be at most %d characters", MaxReasonLength)}
	}

	exists, err := s.entities.EntityExists(ctx, in.EntityType, in.EntityID)
	if err != nil {
		return Report{}, fmt.Errorf("moderation: check reported entity: %w", err)
	}
	if !exists {
		return Report{}, ErrEntityNotFound
	}

	created, err := s.reports.CreateReport(ctx, Report{
		ReporterID: in.ReporterID,
		EntityType: in.EntityType,
		EntityID:   in.EntityID,
		Category:   in.Category,
		Reason:     reason,
	})
	if err != nil {
		return Report{}, err
	}

	s.audit.Log(ctx, in.ReporterID, "report.created", string(in.EntityType), in.EntityID, map[string]any{
		"category": string(in.Category),
	})

	return created, nil
}

// ListMyReports returns one page of a user's own reports, newest first, plus the
// cursor that resumes after it ("" when the page is the last one).
func (s *ReportService) ListMyReports(ctx context.Context, reporterID, rawCursor string, limit int) ([]Report, string, error) {
	if !isUUID(reporterID) {
		return nil, "", &ValidationError{Field: "user_id", Message: "must be a valid user id"}
	}
	if limit < 1 || limit > MaxListLimit {
		return nil, "", &ValidationError{Field: "limit", Message: fmt.Sprintf("must be between 1 and %d", MaxListLimit)}
	}

	var cursor *Cursor
	if rawCursor != "" {
		decoded, err := DecodeCursor(rawCursor)
		if err != nil {
			return nil, "", err
		}
		cursor = &decoded
	}

	page, next, err := s.reports.ListMyReports(ctx, reporterID, cursor, limit)
	if err != nil {
		return nil, "", fmt.Errorf("moderation: list my reports: %w", err)
	}
	if page == nil {
		page = []Report{}
	}

	nextCursor := ""
	if next != nil {
		nextCursor = next.Encode()
	}

	return page, nextCursor, nil
}

// UpsertCaseForEntity creates or refreshes the moderation case for an entity. It
// is the exported seam 017b uses; CreateReport already performs the same upsert
// inside its transaction.
func (s *ReportService) UpsertCaseForEntity(ctx context.Context, entityType EntityType, entityID string) error {
	if !entityType.Valid() {
		return &ValidationError{Field: "entity_type", Message: "is not a reportable entity kind"}
	}
	if !isUUID(entityID) {
		return &ValidationError{Field: "entity_id", Message: "must be a valid entity id"}
	}
	if err := s.reports.UpsertCaseForEntity(ctx, entityType, entityID); err != nil {
		return fmt.Errorf("moderation: upsert case: %w", err)
	}
	return nil
}
