package inquiries

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/knot/backend/internal/moderation"
)

// inquiryPageHasID reports whether a page of inquiries contains the id.
func inquiryPageHasID(page []Inquiry, id string) bool {
	for _, inquiry := range page {
		if inquiry.ID == id {
			return true
		}
	}
	return false
}

// answerPageHasID reports whether a page of answers contains the id.
func answerPageHasID(page []Answer, id string) bool {
	for _, answer := range page {
		if answer.ID == id {
			return true
		}
	}
	return false
}

// TestPostgresStoreBlockFilterHidesInquiriesAndAnswers proves the block filter on
// the inquiry list and the answer list: a blocked author's inquiry and answer are
// absent, while another author's remain.
func TestPostgresStoreBlockFilterHidesInquiriesAndAnswers(t *testing.T) {
	store, pool, prefix := integrationSetup(t)
	ctx := context.Background()
	authorID := createUser(t, pool, prefix, "author")
	blockedID := createUser(t, pool, prefix, "blocked")

	// A per-run place scopes both the page and the block assertion to this test's
	// rows (KNOT-ADR-063).
	place := uniquePlace(prefix, "Filter")
	visible := seedInquiry(t, pool, authorID, "visible", place, time.Hour)
	hidden := seedInquiry(t, pool, blockedID, "hidden", place, 2*time.Hour)

	filtered := moderation.WithExcludedAuthors(ctx, []string{blockedID})

	page, _, err := store.ListInquiries(filtered, nil, 50, place)
	if err != nil {
		t.Fatalf("ListInquiries() error = %v, want nil", err)
	}
	if inquiryPageHasID(page, hidden) {
		t.Error("blocked author's inquiry is visible")
	}
	if !inquiryPageHasID(page, visible) {
		t.Error("unblocked author's inquiry is missing")
	}

	// One answer by each author on the visible inquiry.
	if _, _, err := store.CreateAnswer(ctx, Answer{
		InquiryID: visible, AuthorID: authorID, Language: "eng", Body: "by author",
	}); err != nil {
		t.Fatalf("CreateAnswer(author) error = %v, want nil", err)
	}
	blockedAnswer, _, err := store.CreateAnswer(ctx, Answer{
		InquiryID: visible, AuthorID: blockedID, Language: "eng", Body: "by blocked",
	})
	if err != nil {
		t.Fatalf("CreateAnswer(blocked) error = %v, want nil", err)
	}

	answers, _, err := store.ListAnswers(filtered, visible, nil, 50)
	if err != nil {
		t.Fatalf("ListAnswers() error = %v, want nil", err)
	}
	if answerPageHasID(answers, blockedAnswer.ID) {
		t.Error("blocked author's answer is visible")
	}
	if len(answers) != 1 {
		t.Errorf("filtered answers = %d, want 1 (only the unblocked answer)", len(answers))
	}

	// Without a block set both rows are present: the filter, not a missing row.
	unfiltered, _, err := store.ListInquiries(ctx, nil, 50, place)
	if err != nil {
		t.Fatalf("ListInquiries() error = %v, want nil", err)
	}
	if !inquiryPageHasID(unfiltered, hidden) {
		t.Error("unfiltered page is missing the blocked author's inquiry")
	}
}

// TestGetInquiryAndAnswerBlockedAuthorIsNotFound proves a blocked author's
// inquiry and answer are reported as not found on the detail routes
// (KNOT-ADR-064).
func TestGetInquiryAndAnswerBlockedAuthorIsNotFound(t *testing.T) {
	store := &fakeStore{
		getResult:       Inquiry{ID: testInquiryID, AuthorID: testAuthorID},
		getAnswerResult: Answer{ID: testAnswerID, InquiryID: testInquiryID, AuthorID: testAnswererID},
	}
	service := newTestService(t, store, &fakeRouting{}, &fakeNotifier{})

	ctx := moderation.WithExcludedAuthors(context.Background(), []string{testAuthorID, testAnswererID})
	if _, err := service.GetInquiry(ctx, testInquiryID); !errors.Is(err, ErrNotFound) {
		t.Fatalf("GetInquiry() error = %v, want ErrNotFound", err)
	}
	if _, err := service.GetAnswer(ctx, testAnswerID); !errors.Is(err, ErrNotFound) {
		t.Fatalf("GetAnswer() error = %v, want ErrNotFound", err)
	}
}

// TestCreateAnswerRejectsBlockedInquiryAuthor proves a blocked user cannot answer
// an inquiry authored by someone on either side of the block (KNOT-ADR-060).
func TestCreateAnswerRejectsBlockedInquiryAuthor(t *testing.T) {
	store := &fakeStore{getResult: Inquiry{ID: testInquiryID, AuthorID: testAuthorID}}
	service := newTestService(t, store, &fakeRouting{}, &fakeNotifier{})

	ctx := moderation.WithExcludedAuthors(context.Background(), []string{testAuthorID})
	_, err := service.CreateAnswer(ctx, testInquiryID, testAnswererID, CreateAnswerInput{
		Body:     "b",
		Language: "eng",
	})
	if !errors.Is(err, moderation.ErrBlocked) {
		t.Fatalf("CreateAnswer() error = %v, want ErrBlocked", err)
	}
	if store.answerCalls != 0 {
		t.Errorf("CreateAnswer store called %d times, want 0", store.answerCalls)
	}
}
