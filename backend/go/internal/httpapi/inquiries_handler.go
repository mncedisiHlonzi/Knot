// Package httpapi's inquiries handler: the HTTP surface of Curious Inquiries.
//
// Six routes are the feature's public shape:
//
//	POST   /inquiries                 ask a question              (protected)
//	GET    /inquiries                 the open questions, newest first (public)
//	GET    /inquiries/{id}            one question                (public)
//	POST   /inquiries/{id}/answers    answer a question           (protected)
//	GET    /inquiries/{id}/answers    a question's answers        (public)
//	GET    /answers/{id}              one answer                  (public)
//
// plus the two reaction routes the handler in reactions_handler.go owns
// (POST and GET /inquiries/{id}/reactions, KNOT-ADR-057).
package httpapi

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"strconv"
	"time"

	"github.com/knot/backend/internal/inquiries"
	"github.com/knot/backend/internal/moderation"
	"github.com/knot/backend/internal/reactions"
)

// InquiriesService is the slice of the inquiries service that the HTTP layer
// needs. Depending on an interface (rather than the concrete service) keeps
// handler tests free of a database.
type InquiriesService interface {
	CreateInquiry(ctx context.Context, authorID string, in inquiries.CreateInquiryInput) (inquiries.Inquiry, error)
	GetInquiry(ctx context.Context, id string) (inquiries.Inquiry, error)
	ListInquiries(ctx context.Context, rawCursor string, limit int, place string) ([]inquiries.Inquiry, string, error)
	CreateAnswer(ctx context.Context, inquiryID, authorID string, in inquiries.CreateAnswerInput) (inquiries.Answer, error)
	ListAnswers(ctx context.Context, inquiryID, rawCursor string, limit int) ([]inquiries.Answer, string, error)
	GetAnswer(ctx context.Context, id string) (inquiries.Answer, error)
}

// InquiriesHandler serves the Curious Inquiries endpoints.
type InquiriesHandler struct {
	service   InquiriesService
	authors   AuthorLookup
	rooted    RootedLookup
	reactions ReactionsLookup
	logger    *slog.Logger
}

// NewInquiriesHandler returns a handler backed by service. The author lookup
// names and pictures every author a response names (one batched read per
// response), the rooted lookup attaches each author's inline Rooted summary, and
// the reactions lookup attaches perspective-reaction counts and the reader's own
// signals.
func NewInquiriesHandler(service InquiriesService, authors AuthorLookup, rooted RootedLookup, reactionsLookup ReactionsLookup, logger *slog.Logger) (*InquiriesHandler, error) {
	if service == nil {
		return nil, fmt.Errorf("httpapi: inquiries handler requires a service")
	}
	if authors == nil {
		return nil, fmt.Errorf("httpapi: inquiries handler requires an author lookup")
	}
	if rooted == nil {
		return nil, fmt.Errorf("httpapi: inquiries handler requires a rooted lookup")
	}
	if reactionsLookup == nil {
		return nil, fmt.Errorf("httpapi: inquiries handler requires a reactions lookup")
	}
	if logger == nil {
		return nil, fmt.Errorf("httpapi: inquiries handler requires a logger")
	}
	return &InquiriesHandler{
		service:   service,
		authors:   authors,
		rooted:    rooted,
		reactions: reactionsLookup,
		logger:    logger,
	}, nil
}

// createInquiryRequest is the POST /inquiries body.
//
// There is deliberately no author_id field. The asker is taken from the
// authenticated request, so a client cannot ask as somebody else, and there is no
// anonymity flag: every inquiry is attributed (KNOT-ADR-055).
type createInquiryRequest struct {
	Title string `json:"title"`
	Body  string `json:"body"`
	// Language is the ISO 639-3 code the question is written in.
	Language string `json:"language"`
	// Place is optional. When given it routes the inquiry to the first Rooted
	// users of that place (KNOT-ADR-056).
	Place string `json:"place"`
	// Latitude, Longitude, and PlaceCountry are the structured place data the
	// mobile location picker resolved. The coordinate pair goes together
	// (KNOT-ADR-034).
	Latitude     *float64 `json:"latitude"`
	Longitude    *float64 `json:"longitude"`
	PlaceCountry string   `json:"place_country"`
}

// createAnswerRequest is the POST /inquiries/{id}/answers body.
//
// The answerer comes from the access token and the inquiry from the path, so
// neither is in the body. There is no anonymity flag: every answer is attributed
// (KNOT-ADR-055).
type createAnswerRequest struct {
	Body     string `json:"body"`
	Language string `json:"language"`
}

// inquiryResponse is the public projection of an inquiry.
//
// The author's display name, avatar, and Rooted summary are attached by the
// handler through batched lookups, exactly as every other content response does
// (KNOT-ADR-041, KNOT-ADR-017).
type inquiryResponse struct {
	ID string `json:"id"`
	// AuthorID is the asker. Every inquiry is attributed, so this is never null.
	AuthorID string `json:"author_id"`
	Title    string `json:"title"`
	Body     string `json:"body"`
	// Language is the ISO 639-3 code the question is written in.
	Language string `json:"language"`
	// Place is the place asked about, or null when the question names none.
	Place *string `json:"place"`
	// PlaceCountry is the country the geocoder reported, or null for a legacy or
	// coordinate-less inquiry.
	PlaceCountry *string `json:"place_country"`
	// Latitude and Longitude are the structured coordinate, or null. The pair is
	// null together or set together (KNOT-ADR-034).
	Latitude  *float64 `json:"latitude"`
	Longitude *float64 `json:"longitude"`
	// AnswerCount is how many public answers the inquiry holds. It is maintained
	// transactionally with each answer, so it is never stale.
	AnswerCount int       `json:"answer_count"`
	CreatedAt   time.Time `json:"created_at"`
	UpdatedAt   time.Time `json:"updated_at"`
	// AuthorDisplayName and AuthorAvatarURL are the asker's inline attribution.
	// AuthorAvatarURL is the backend path to fetch an avatar from, or null when the
	// author has none, so a client renders initials (KNOT-ADR-041).
	AuthorDisplayName string  `json:"author_display_name"`
	AuthorAvatarURL   *string `json:"author_avatar_url"`
	// AuthorRooted is the asker's primary public Rooted signal, or null when they
	// have none (KNOT-ADR-017).
	AuthorRooted *rootedSummary `json:"author_rooted"`
	// Reactions is the count of each of the four perspective signals on the
	// inquiry, and MyReactions is the signals the reader holds (KNOT-ADR-057).
	Reactions   reactionCountsResponse `json:"reactions"`
	MyReactions []string               `json:"my_reactions"`
}

// answerResponse is the public projection of an answer.
//
// It carries no reactions: an answer is a reply, and replies carry no reactions
// at MVP (KNOT-ADR-052).
type answerResponse struct {
	ID        string `json:"id"`
	InquiryID string `json:"inquiry_id"`
	// AuthorID is the answerer. Every answer is attributed, so this is never null.
	AuthorID  string    `json:"author_id"`
	Language  string    `json:"language"`
	Body      string    `json:"body"`
	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
	// AuthorDisplayName and AuthorAvatarURL are the answerer's inline attribution,
	// filled by one batched lookup per response.
	AuthorDisplayName string  `json:"author_display_name"`
	AuthorAvatarURL   *string `json:"author_avatar_url"`
	// AuthorRooted is the answerer's primary public Rooted signal, or null.
	AuthorRooted *rootedSummary `json:"author_rooted"`
}

// inquiryEnvelope wraps a single inquiry so the response shape can gain sibling
// fields later without breaking clients.
type inquiryEnvelope struct {
	Inquiry inquiryResponse `json:"inquiry"`
}

// answerEnvelope wraps a single answer.
type answerEnvelope struct {
	Answer answerResponse `json:"answer"`
}

// inquiryListResponse is the GET /inquiries body. Inquiries is always an array,
// never null, and NextCursor is the empty string on the last page.
type inquiryListResponse struct {
	Inquiries  []inquiryResponse `json:"inquiries"`
	NextCursor string            `json:"next_cursor"`
}

// answerListResponse is the GET /inquiries/{id}/answers body. Answers is always an
// array, never null.
type answerListResponse struct {
	Answers    []answerResponse `json:"answers"`
	NextCursor string           `json:"next_cursor"`
}

// Create handles POST /inquiries: asks a question about a place.
//
// The route is protected. A question with a place is routed to the first Rooted
// users of that place (KNOT-ADR-056); that routing happens after the inquiry is
// committed and can never fail the ask.
func (h *InquiriesHandler) Create(w http.ResponseWriter, r *http.Request) {
	userID, ok := UserIDFromContext(r.Context())
	if !ok {
		writeError(w, http.StatusUnauthorized, codeUnauthorized, "authentication required")
		return
	}

	var body createInquiryRequest
	if err := decodeJSON(w, r, &body); err != nil {
		writeDecodeError(w, r, h.logger, err)
		return
	}

	created, err := h.service.CreateInquiry(r.Context(), userID, inquiries.CreateInquiryInput{
		Title:        body.Title,
		Body:         body.Body,
		Language:     body.Language,
		Place:        body.Place,
		PlaceCountry: body.PlaceCountry,
		Latitude:     body.Latitude,
		Longitude:    body.Longitude,
	})
	if err != nil {
		h.writeServiceError(w, r, err, "user not found")
		return
	}

	response := newInquiryResponse(created)
	response.AuthorRooted = authorRootedSummaries(r.Context(), h.rooted, h.logger, []string{created.AuthorID})[created.AuthorID]
	response.AuthorDisplayName, response.AuthorAvatarURL = authorFields(r.Context(), h.authors, h.logger, created.AuthorID)
	response.Reactions, response.MyReactions = reactionsForEntity(r.Context(), h.reactions, h.logger, reactions.EntityInquiry, userID, created.ID)

	writeJSON(w, http.StatusCreated, inquiryEnvelope{Inquiry: response})
}

// List handles GET /inquiries: one page of open inquiries, newest first.
//
// The route is public and uses optional auth: a signed-in caller also receives
// their own reaction highlights, an anonymous caller does not (KNOT-ADR-051).
//
// An optional ?place= restricts the page to that place, which is the "questions
// about Manguzi" read. It is an exact match on the stored place — the same
// spelling the asker gave — and absent means every place, including questions that
// name none.
func (h *InquiriesHandler) List(w http.ResponseWriter, r *http.Request) {
	query := r.URL.Query()

	limit, err := parseInquiryLimit(query.Get("limit"))
	if err != nil {
		h.writeServiceError(w, r, err, "not found")
		return
	}

	page, next, err := h.service.ListInquiries(r.Context(), query.Get("cursor"), limit, query.Get("place"))
	if err != nil {
		h.writeServiceError(w, r, err, "not found")
		return
	}

	readerID, _ := UserIDFromContext(r.Context())

	authorIDs := make([]string, 0, len(page))
	itemIDs := make([]string, 0, len(page))
	for _, inquiry := range page {
		authorIDs = append(authorIDs, inquiry.AuthorID)
		itemIDs = append(itemIDs, inquiry.ID)
	}

	attributions := authorAttributions(r.Context(), h.authors, h.logger, authorIDs)
	summaries := authorRootedSummaries(r.Context(), h.rooted, h.logger, authorIDs)
	enrichment := loadReactions(r.Context(), h.reactions, h.logger, reactions.EntityInquiry, readerID, itemIDs)

	items := make([]inquiryResponse, 0, len(page))
	for _, inquiry := range page {
		item := newInquiryResponse(inquiry)
		item.AuthorDisplayName = attributions[inquiry.AuthorID].DisplayName
		item.AuthorAvatarURL = optionalString(attributions[inquiry.AuthorID].AvatarURL)
		item.AuthorRooted = summaries[inquiry.AuthorID]
		item.Reactions = enrichment.Counts[inquiry.ID]
		item.MyReactions = orEmptyStrings(enrichment.Mine[inquiry.ID])
		items = append(items, item)
	}

	writeJSON(w, http.StatusOK, inquiryListResponse{Inquiries: items, NextCursor: next})
}

// Get handles GET /inquiries/{id}: one inquiry, with its answers summary.
//
// The route is public and uses optional auth so a signed-in reader's own reaction
// highlights travel with the question.
func (h *InquiriesHandler) Get(w http.ResponseWriter, r *http.Request) {
	inquiry, err := h.service.GetInquiry(r.Context(), r.PathValue("id"))
	if err != nil {
		h.writeServiceError(w, r, err, "inquiry not found")
		return
	}

	readerID, _ := UserIDFromContext(r.Context())

	response := newInquiryResponse(inquiry)
	response.AuthorRooted = authorRootedSummaries(r.Context(), h.rooted, h.logger, []string{inquiry.AuthorID})[inquiry.AuthorID]
	response.AuthorDisplayName, response.AuthorAvatarURL = authorFields(r.Context(), h.authors, h.logger, inquiry.AuthorID)
	response.Reactions, response.MyReactions = reactionsForEntity(r.Context(), h.reactions, h.logger, reactions.EntityInquiry, readerID, inquiry.ID)

	writeJSON(w, http.StatusOK, inquiryEnvelope{Inquiry: response})
}

// CreateAnswer handles POST /inquiries/{id}/answers: answers a question publicly.
//
// The route is protected, and the answerer is taken from the access token so a
// client cannot answer as somebody else. The asker is notified after the answer is
// committed; a failure to notify never loses the answer, and answering your own
// question never notifies you (KNOT-ADR-038).
func (h *InquiriesHandler) CreateAnswer(w http.ResponseWriter, r *http.Request) {
	userID, ok := UserIDFromContext(r.Context())
	if !ok {
		writeError(w, http.StatusUnauthorized, codeUnauthorized, "authentication required")
		return
	}

	var body createAnswerRequest
	if err := decodeJSON(w, r, &body); err != nil {
		writeDecodeError(w, r, h.logger, err)
		return
	}

	created, err := h.service.CreateAnswer(r.Context(), r.PathValue("id"), userID, inquiries.CreateAnswerInput{
		Body:     body.Body,
		Language: body.Language,
	})
	if err != nil {
		h.writeServiceError(w, r, err, "inquiry not found")
		return
	}

	response := newAnswerResponse(created)
	response.AuthorRooted = authorRootedSummaries(r.Context(), h.rooted, h.logger, []string{created.AuthorID})[created.AuthorID]
	response.AuthorDisplayName, response.AuthorAvatarURL = authorFields(r.Context(), h.authors, h.logger, created.AuthorID)

	writeJSON(w, http.StatusCreated, answerEnvelope{Answer: response})
}

// ListAnswers handles GET /inquiries/{id}/answers: one page of a question's
// answers, oldest first, so the thread reads as a conversation.
//
// The route is public. An unknown inquiry is a 404 rather than an empty thread,
// so a client can tell "no such question" from "nobody has answered yet".
func (h *InquiriesHandler) ListAnswers(w http.ResponseWriter, r *http.Request) {
	query := r.URL.Query()

	limit, err := parseAnswerLimit(query.Get("limit"))
	if err != nil {
		h.writeServiceError(w, r, err, "inquiry not found")
		return
	}

	page, next, err := h.service.ListAnswers(r.Context(), r.PathValue("id"), query.Get("cursor"), limit)
	if err != nil {
		h.writeServiceError(w, r, err, "inquiry not found")
		return
	}

	authorIDs := make([]string, 0, len(page))
	for _, answer := range page {
		authorIDs = append(authorIDs, answer.AuthorID)
	}

	attributions := authorAttributions(r.Context(), h.authors, h.logger, authorIDs)
	summaries := authorRootedSummaries(r.Context(), h.rooted, h.logger, authorIDs)

	items := make([]answerResponse, 0, len(page))
	for _, answer := range page {
		item := newAnswerResponse(answer)
		item.AuthorDisplayName = attributions[answer.AuthorID].DisplayName
		item.AuthorAvatarURL = optionalString(attributions[answer.AuthorID].AvatarURL)
		item.AuthorRooted = summaries[answer.AuthorID]
		items = append(items, item)
	}

	writeJSON(w, http.StatusOK, answerListResponse{Answers: items, NextCursor: next})
}

// GetAnswer handles GET /answers/{id}: one answer.
//
// The route is public. It exists so an id alone resolves an answer — the shape
// GET /comments/{id} and GET /bridges/{id} already have — which is what a wall card
// or a stored link needs.
func (h *InquiriesHandler) GetAnswer(w http.ResponseWriter, r *http.Request) {
	answer, err := h.service.GetAnswer(r.Context(), r.PathValue("id"))
	if err != nil {
		h.writeServiceError(w, r, err, "answer not found")
		return
	}

	response := newAnswerResponse(answer)
	response.AuthorRooted = authorRootedSummaries(r.Context(), h.rooted, h.logger, []string{answer.AuthorID})[answer.AuthorID]
	response.AuthorDisplayName, response.AuthorAvatarURL = authorFields(r.Context(), h.authors, h.logger, answer.AuthorID)

	writeJSON(w, http.StatusOK, answerEnvelope{Answer: response})
}

// newInquiryResponse projects a domain inquiry onto the wire format. The author
// and reaction fields are left zero; the caller fills them from batched lookups.
func newInquiryResponse(inquiry inquiries.Inquiry) inquiryResponse {
	return inquiryResponse{
		ID:           inquiry.ID,
		AuthorID:     inquiry.AuthorID,
		Title:        inquiry.Title,
		Body:         inquiry.Body,
		Language:     inquiry.Language,
		Place:        inquiry.Place,
		PlaceCountry: inquiry.PlaceCountry,
		Latitude:     inquiry.Latitude,
		Longitude:    inquiry.Longitude,
		AnswerCount:  inquiry.AnswerCount,
		CreatedAt:    inquiry.CreatedAt,
		UpdatedAt:    inquiry.UpdatedAt,
	}
}

// newAnswerResponse projects a domain answer onto the wire format.
func newAnswerResponse(answer inquiries.Answer) answerResponse {
	return answerResponse{
		ID:        answer.ID,
		InquiryID: answer.InquiryID,
		AuthorID:  answer.AuthorID,
		Language:  answer.Language,
		Body:      answer.Body,
		CreatedAt: answer.CreatedAt,
		UpdatedAt: answer.UpdatedAt,
	}
}

// parseInquiryLimit reads the optional "limit" query parameter for the inquiry
// list.
//
// An absent limit means the inquiry domain's default. A limit above the maximum is
// clamped rather than rejected, so a client asking for "as many as you can" gets a
// usable page. A limit that is not a positive integer is a validation failure: it
// is a malformed request, not an over-large one.
func parseInquiryLimit(raw string) (int, error) {
	if raw == "" {
		return inquiries.DefaultListLimit, nil
	}

	value, err := strconv.Atoi(raw)
	if err != nil {
		return 0, &inquiries.ValidationError{Field: "limit", Message: "must be an integer"}
	}
	if value < 1 {
		return 0, &inquiries.ValidationError{Field: "limit", Message: "must be at least 1"}
	}
	if value > inquiries.MaxListLimit {
		return inquiries.MaxListLimit, nil
	}

	return value, nil
}

// parseAnswerLimit reads the optional "limit" query parameter for an answer
// thread. Its bounds are the answer page's, which are wider than the list's.
func parseAnswerLimit(raw string) (int, error) {
	if raw == "" {
		return inquiries.DefaultAnswerListLimit, nil
	}

	value, err := strconv.Atoi(raw)
	if err != nil {
		return 0, &inquiries.ValidationError{Field: "limit", Message: "must be an integer"}
	}
	if value < 1 {
		return 0, &inquiries.ValidationError{Field: "limit", Message: "must be at least 1"}
	}
	if value > inquiries.MaxAnswerListLimit {
		return inquiries.MaxAnswerListLimit, nil
	}

	return value, nil
}

// writeServiceError maps domain errors onto HTTP status codes. Only errors we
// recognise as safe are described to the client; everything else becomes a generic
// 500 and is logged with the request id.
func (h *InquiriesHandler) writeServiceError(w http.ResponseWriter, r *http.Request, err error, notFoundMessage string) {
	var validation *inquiries.ValidationError

	switch {
	case errors.As(err, &validation):
		writeError(w, http.StatusBadRequest, codeValidation, fmt.Sprintf("%s %s", validation.Field, validation.Message))
	case errors.Is(err, inquiries.ErrNotFound):
		writeError(w, http.StatusNotFound, codeNotFound, notFoundMessage)
	case errors.Is(err, inquiries.ErrUserNotFound):
		writeError(w, http.StatusNotFound, codeNotFound, "user not found")
	case errors.Is(err, moderation.ErrBlocked):
		writeError(w, http.StatusForbidden, codeBlocked, "you cannot interact with this content")
	default:
		h.logger.ErrorContext(
			r.Context(),
			"inquiries request failed",
			slog.String("request_id", RequestIDFromContext(r.Context())),
			slog.String("path", r.URL.Path),
			slog.String("error", err.Error()),
		)
		writeError(w, http.StatusInternalServerError, codeInternal, "internal server error")
	}
}
