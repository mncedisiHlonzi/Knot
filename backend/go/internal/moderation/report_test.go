package moderation

import (
	"context"
	"errors"
	"testing"
	"time"
)

const (
	reporterID = "33333333-3333-4333-8333-333333333333"
	entityID   = "44444444-4444-4444-8444-444444444444"
)

// fakeReportStore is an in-memory ReportStore for the service tests.
type fakeReportStore struct {
	createErr error
	gotReport Report
	page      []Report
	next      *Cursor
	listErr   error
	upserts   int
	upsertErr error
}

func (f *fakeReportStore) CreateReport(_ context.Context, report Report) (Report, error) {
	f.gotReport = report
	if f.createErr != nil {
		return Report{}, f.createErr
	}
	report.ID = "stored-id"
	report.CreatedAt = time.Now().UTC()
	return report, nil
}

func (f *fakeReportStore) ListMyReports(_ context.Context, _ string, _ *Cursor, _ int) ([]Report, *Cursor, error) {
	return f.page, f.next, f.listErr
}

func (f *fakeReportStore) UpsertCaseForEntity(_ context.Context, _ EntityType, _ string) error {
	f.upserts++
	return f.upsertErr
}

// fakeEntityLookup is a scriptable EntityLookup.
type fakeEntityLookup struct {
	exists  bool
	err     error
	gotType EntityType
	gotID   string
	calls   int
}

func (f *fakeEntityLookup) EntityExists(_ context.Context, entityType EntityType, entityID string) (bool, error) {
	f.calls++
	f.gotType = entityType
	f.gotID = entityID
	return f.exists, f.err
}

func newReportService(t *testing.T, store *fakeReportStore, entities *fakeEntityLookup, auditor *recordingAuditor) *ReportService {
	t.Helper()

	service, err := NewReportService(store, entities, auditor)
	if err != nil {
		t.Fatalf("NewReportService() error = %v, want nil", err)
	}
	return service
}

func validReportInput() CreateReportInput {
	return CreateReportInput{
		ReporterID: reporterID,
		EntityType: EntityStory,
		EntityID:   entityID,
		Category:   CategoryHarassment,
	}
}

func TestCreateReportStoresTrimsAndAudits(t *testing.T) {
	store := &fakeReportStore{}
	entities := &fakeEntityLookup{exists: true}
	auditor := &recordingAuditor{}
	service := newReportService(t, store, entities, auditor)

	in := validReportInput()
	in.Reason = "  repeated abuse  "

	created, err := service.CreateReport(context.Background(), in)
	if err != nil {
		t.Fatalf("CreateReport() error = %v, want nil", err)
	}
	if created.ID != "stored-id" {
		t.Errorf("CreateReport() id = %q, want stored-id", created.ID)
	}
	if store.gotReport.Reason != "repeated abuse" {
		t.Errorf("stored reason = %q, want trimmed", store.gotReport.Reason)
	}
	if entities.gotType != EntityStory || entities.gotID != entityID {
		t.Errorf("EntityExists(%q, %q), want (story, %q)", entities.gotType, entities.gotID, entityID)
	}
	if len(auditor.events) != 1 || auditor.events[0] != "report.created" {
		t.Errorf("audit events = %v, want one report.created", auditor.events)
	}
}

func TestCreateReportRequiresReasonForOther(t *testing.T) {
	service := newReportService(t, &fakeReportStore{}, &fakeEntityLookup{exists: true}, &recordingAuditor{})

	in := validReportInput()
	in.Category = CategoryOther

	if _, err := service.CreateReport(context.Background(), in); !errors.Is(err, ErrValidation) {
		t.Fatalf("CreateReport(other, no reason) error = %v, want a validation error", err)
	}
}

func TestCreateReportAcceptsOtherWithReason(t *testing.T) {
	entities := &fakeEntityLookup{exists: true}
	service := newReportService(t, &fakeReportStore{}, entities, &recordingAuditor{})

	in := validReportInput()
	in.Category = CategoryOther
	in.Reason = "something else"

	if _, err := service.CreateReport(context.Background(), in); err != nil {
		t.Fatalf("CreateReport(other with reason) error = %v, want nil", err)
	}
}

func TestCreateReportRejectsBadInput(t *testing.T) {
	service := newReportService(t, &fakeReportStore{}, &fakeEntityLookup{exists: true}, &recordingAuditor{})

	cases := map[string]func(CreateReportInput) CreateReportInput{
		"bad entity type": func(in CreateReportInput) CreateReportInput { in.EntityType = "song"; return in },
		"bad entity id":   func(in CreateReportInput) CreateReportInput { in.EntityID = "nope"; return in },
		"bad category":    func(in CreateReportInput) CreateReportInput { in.Category = "whatever"; return in },
		"bad reporter":    func(in CreateReportInput) CreateReportInput { in.ReporterID = "nope"; return in },
	}

	for name, mutate := range cases {
		t.Run(name, func(t *testing.T) {
			if _, err := service.CreateReport(context.Background(), mutate(validReportInput())); !errors.Is(err, ErrValidation) {
				t.Fatalf("CreateReport() error = %v, want a validation error", err)
			}
		})
	}
}

func TestCreateReportEntityNotFound(t *testing.T) {
	service := newReportService(t, &fakeReportStore{}, &fakeEntityLookup{exists: false}, &recordingAuditor{})

	if _, err := service.CreateReport(context.Background(), validReportInput()); !errors.Is(err, ErrEntityNotFound) {
		t.Fatalf("CreateReport() error = %v, want ErrEntityNotFound", err)
	}
}

func TestCreateReportPropagatesAlreadyReported(t *testing.T) {
	store := &fakeReportStore{createErr: ErrAlreadyReported}
	service := newReportService(t, store, &fakeEntityLookup{exists: true}, &recordingAuditor{})

	if _, err := service.CreateReport(context.Background(), validReportInput()); !errors.Is(err, ErrAlreadyReported) {
		t.Fatalf("CreateReport() error = %v, want ErrAlreadyReported", err)
	}
}

func TestListMyReportsValidatesLimitAndReturnsPage(t *testing.T) {
	store := &fakeReportStore{page: []Report{{ID: "r1", Category: CategorySpam, EntityType: EntityComment, EntityID: entityID}}}
	service := newReportService(t, store, &fakeEntityLookup{}, &recordingAuditor{})

	if _, _, err := service.ListMyReports(context.Background(), reporterID, "", 0); !errors.Is(err, ErrValidation) {
		t.Fatalf("ListMyReports(limit 0) error = %v, want a validation error", err)
	}

	page, next, err := service.ListMyReports(context.Background(), reporterID, "", DefaultListLimit)
	if err != nil {
		t.Fatalf("ListMyReports() error = %v, want nil", err)
	}
	if len(page) != 1 {
		t.Errorf("ListMyReports() length = %d, want 1", len(page))
	}
	if next != "" {
		t.Errorf("ListMyReports() next = %q, want empty", next)
	}
}

func TestUpsertCaseForEntityValidates(t *testing.T) {
	store := &fakeReportStore{}
	service := newReportService(t, store, &fakeEntityLookup{}, &recordingAuditor{})

	if err := service.UpsertCaseForEntity(context.Background(), EntityStory, entityID); err != nil {
		t.Fatalf("UpsertCaseForEntity() error = %v, want nil", err)
	}
	if store.upserts != 1 {
		t.Errorf("upserts = %d, want 1", store.upserts)
	}

	if err := service.UpsertCaseForEntity(context.Background(), "song", entityID); !errors.Is(err, ErrValidation) {
		t.Errorf("UpsertCaseForEntity(bad type) error = %v, want a validation error", err)
	}
}
