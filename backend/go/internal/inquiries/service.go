package inquiries

import (
	"context"
	"errors"
	"fmt"
	"math"
	"strings"
	"unicode/utf8"

	"github.com/knot/backend/internal/language"
)

// Logger is the slice of slog.Logger this package needs: a warning when routing
// or notification fails. Depending on an interface keeps the service tests free of
// a logger.
type Logger interface {
	WarnContext(ctx context.Context, msg string, args ...any)
}

// Notifier is the notification hook this domain fires. It is the two one-method
// primitives the notifications service satisfies, named for the events rather than
// the entities, so this package never imports the notifications package's types
// (KNOT-ADR-040).
//
// Both are best-effort: a failure to write a notification must never fail the ask
// or the answer that caused it (KNOT-ADR-038).
type Notifier interface {
	// NotifyInquiryAnswered records that actorID answered an inquiry asked by
	// recipientID.
	NotifyInquiryAnswered(ctx context.Context, recipientID, actorID, inquiryID string) error
	// NotifyInquiryNearby records that actorID asked a question about the place
	// recipientID is rooted in.
	NotifyInquiryNearby(ctx context.Context, recipientID, actorID, inquiryID string) error
}

// RootedRouting resolves which users are Rooted in a place. It is how an inquiry
// finds its first readers.
//
// It is deliberately narrower than the whole Rooted service — one read, the ids
// only — so this package depends on exactly the capability it uses. The query
// lives in the Rooted domain rather than here because Rooted owns rooted_signals
// and is Knot's trust and community layer (KNOT-ADR-056).
type RootedRouting interface {
	// RootedUserIDsByPlace returns the ids of at most limit users whose primary
	// public Rooted signal is for place, earliest declarer first. An unknown place
	// yields an empty slice, not an error.
	RootedUserIDsByPlace(ctx context.Context, place string, limit int) ([]string, error)
}

// Service holds the inquiry business rules.
//
// It depends on the InquiryStore abstraction, a RootedRouting lookup, a Notifier,
// and a Logger, and knows nothing about HTTP, JSON, or SQL.
type Service struct {
	store    InquiryStore
	routing  RootedRouting
	notifier Notifier
	logger   Logger
}

// NewService wires the store, routing, notifier, and logger into the inquiry
// domain.
func NewService(store InquiryStore, routing RootedRouting, notifier Notifier, logger Logger) (*Service, error) {
	if store == nil {
		return nil, fmt.Errorf("inquiries: service requires an inquiry store")
	}
	if routing == nil {
		return nil, fmt.Errorf("inquiries: service requires a rooted routing lookup")
	}
	if notifier == nil {
		return nil, fmt.Errorf("inquiries: service requires a notifier")
	}
	if logger == nil {
		return nil, fmt.Errorf("inquiries: service requires a logger")
	}
	return &Service{store: store, routing: routing, notifier: notifier, logger: logger}, nil
}

// CreateInquiry validates the input, stores the question, and routes it to the
// first Rooted users of its place.
//
// Routing happens after the inquiry is committed, never inside its transaction:
// the question must survive a failure to announce it. It is also the only place a
// question with no place differs — a place-less inquiry is stored and routed
// nowhere.
//
// It returns a *ValidationError for bad input and ErrUserNotFound when the author
// does not exist.
func (s *Service) CreateInquiry(ctx context.Context, authorID string, in CreateInquiryInput) (Inquiry, error) {
	if !isUUID(authorID) {
		return Inquiry{}, ErrUserNotFound
	}

	inquiry, err := validateCreateInquiry(in)
	if err != nil {
		return Inquiry{}, err
	}
	inquiry.AuthorID = authorID

	stored, err := s.store.CreateInquiry(ctx, inquiry)
	if err != nil {
		if errors.Is(err, ErrUserNotFound) {
			return Inquiry{}, ErrUserNotFound
		}
		return Inquiry{}, fmt.Errorf("inquiries: create inquiry: %w", err)
	}

	// Routing uses the place the request validated, not the place the store
	// happened to echo back: announcing a question is the service's
	// responsibility, and it must not silently stop working because a store chose
	// to return fewer columns. The id, by contrast, can only come from the store.
	if inquiry.Place != nil {
		s.routeToNearby(ctx, *inquiry.Place, authorID, stored.ID)
	}

	return stored, nil
}

// GetInquiry returns one inquiry, or ErrNotFound when it does not exist (or the id
// is not a UUID, so it cannot name one).
func (s *Service) GetInquiry(ctx context.Context, id string) (Inquiry, error) {
	if !isUUID(id) {
		return Inquiry{}, ErrNotFound
	}

	inquiry, err := s.store.GetInquiry(ctx, id)
	if err != nil {
		if errors.Is(err, ErrNotFound) {
			return Inquiry{}, ErrNotFound
		}
		return Inquiry{}, fmt.Errorf("inquiries: get inquiry: %w", err)
	}

	return inquiry, nil
}

// ListInquiries returns one page of open inquiries, newest first, plus the cursor
// that resumes after it.
//
// An empty rawCursor asks for the first page; the returned next cursor is the
// empty string when the page is the last one. A non-empty place restricts the page
// to that place — this is how "questions about Manguzi" is asked, and how a
// place-less inquiry is reached: pass no place and it appears in the unfiltered
// list.
func (s *Service) ListInquiries(ctx context.Context, rawCursor string, limit int, place string) ([]Inquiry, string, error) {
	if limit < 1 || limit > MaxListLimit {
		return nil, "", &ValidationError{
			Field:   "limit",
			Message: fmt.Sprintf("must be between 1 and %d", MaxListLimit),
		}
	}

	var cursor *Cursor
	if rawCursor != "" {
		decoded, err := DecodeCursor(rawCursor)
		if err != nil {
			return nil, "", err
		}
		cursor = &decoded
	}

	page, next, err := s.store.ListInquiries(ctx, cursor, limit, strings.TrimSpace(place))
	if err != nil {
		return nil, "", fmt.Errorf("inquiries: list inquiries: %w", err)
	}

	if page == nil {
		// Emit [] rather than null so a client never special-cases an empty list.
		page = []Inquiry{}
	}

	nextCursor := ""
	if next != nil {
		nextCursor = next.Encode()
	}

	return page, nextCursor, nil
}

// CreateAnswer validates the input, appends the answer, and notifies the asker.
//
// The answer is committed first and the notification written afterwards, so a
// failure to notify never loses an answer. A user answering their own question is
// never notified: acting on your own content does not notify you (KNOT-ADR-038).
//
// It returns ErrNotFound when the inquiry does not exist and ErrUserNotFound when
// the answering author does not, and a *ValidationError for bad input.
func (s *Service) CreateAnswer(ctx context.Context, inquiryID, authorID string, in CreateAnswerInput) (Answer, error) {
	if !isUUID(inquiryID) {
		return Answer{}, ErrNotFound
	}
	if !isUUID(authorID) {
		return Answer{}, ErrUserNotFound
	}

	answer, err := validateCreateAnswer(in)
	if err != nil {
		return Answer{}, err
	}
	answer.InquiryID = inquiryID
	answer.AuthorID = authorID

	stored, inquiryAuthorID, err := s.store.CreateAnswer(ctx, answer)
	if err != nil {
		switch {
		case errors.Is(err, ErrNotFound):
			return Answer{}, ErrNotFound
		case errors.Is(err, ErrUserNotFound):
			return Answer{}, ErrUserNotFound
		default:
			return Answer{}, fmt.Errorf("inquiries: create answer: %w", err)
		}
	}

	// The store read the inquiry's author under the same row lock that guards the
	// count, so this is the author as of the write rather than a second, racier
	// lookup.
	if inquiryAuthorID != authorID {
		_ = s.notifier.NotifyInquiryAnswered(ctx, inquiryAuthorID, authorID, inquiryID)
	}

	return stored, nil
}

// ListAnswers returns one page of an inquiry's answers, oldest first, plus the
// cursor that resumes after it.
//
// An answer thread reads as a conversation, so the earliest answer leads — the
// opposite of the inquiry list's newest-first order.
//
// The inquiry's existence is checked before the page is read, so an unknown
// inquiry is ErrNotFound rather than an empty thread, which would be
// indistinguishable from a question nobody has answered yet.
func (s *Service) ListAnswers(ctx context.Context, inquiryID, rawCursor string, limit int) ([]Answer, string, error) {
	if !isUUID(inquiryID) {
		return nil, "", ErrNotFound
	}
	if limit < 1 || limit > MaxAnswerListLimit {
		return nil, "", &ValidationError{
			Field:   "limit",
			Message: fmt.Sprintf("must be between 1 and %d", MaxAnswerListLimit),
		}
	}

	if _, err := s.GetInquiry(ctx, inquiryID); err != nil {
		return nil, "", err
	}

	var cursor *Cursor
	if rawCursor != "" {
		decoded, err := DecodeCursor(rawCursor)
		if err != nil {
			return nil, "", err
		}
		cursor = &decoded
	}

	page, next, err := s.store.ListAnswers(ctx, inquiryID, cursor, limit)
	if err != nil {
		return nil, "", fmt.Errorf("inquiries: list answers: %w", err)
	}

	if page == nil {
		page = []Answer{}
	}

	nextCursor := ""
	if next != nil {
		nextCursor = next.Encode()
	}

	return page, nextCursor, nil
}

// GetAnswer returns one answer, or ErrNotFound when it does not exist.
func (s *Service) GetAnswer(ctx context.Context, id string) (Answer, error) {
	if !isUUID(id) {
		return Answer{}, ErrNotFound
	}

	answer, err := s.store.GetAnswer(ctx, id)
	if err != nil {
		if errors.Is(err, ErrNotFound) {
			return Answer{}, ErrNotFound
		}
		return Answer{}, fmt.Errorf("inquiries: get answer: %w", err)
	}

	return answer, nil
}

// routeToNearby announces an inquiry to the first Rooted users of its place.
//
// Every failure here is logged and swallowed. Routing is a courtesy to the
// question, not part of it: a Rooted lookup that fails, or a notification that
// cannot be written, must not turn a stored question into a failed request. The
// asker is skipped so asking about your own place never notifies you, and the
// notifications service would refuse to store it anyway (KNOT-ADR-056).
func (s *Service) routeToNearby(ctx context.Context, place, authorID, inquiryID string) {
	recipients, err := s.routing.RootedUserIDsByPlace(ctx, place, NearbyRecipientLimit)
	if err != nil {
		s.logger.WarnContext(
			ctx,
			"could not route inquiry to nearby rooted users",
			"inquiry_id", inquiryID,
			"place", place,
			"error", err.Error(),
		)
		return
	}

	for _, recipientID := range recipients {
		if recipientID == authorID {
			continue
		}
		_ = s.notifier.NotifyInquiryNearby(ctx, recipientID, authorID, inquiryID)
	}
}

// validateCreateInquiry applies every input rule and returns the inquiry to store.
//
// The title and body are trimmed because surrounding whitespace is never
// meaningful. The place, when given, is trimmed and rejected when it contains a
// line break or a tab, so a "place" cannot smuggle in a multi-line address —
// exactly the rule a Rooted signal's place obeys, and the reason an inquiry's
// place can be matched against one. Lengths are counted in runes so a multi-byte
// script is not penalised.
func validateCreateInquiry(in CreateInquiryInput) (Inquiry, error) {
	title := strings.TrimSpace(in.Title)
	if utf8.RuneCountInString(title) < MinTitleLength {
		return Inquiry{}, &ValidationError{Field: "title", Message: "is required"}
	}
	if utf8.RuneCountInString(title) > MaxTitleLength {
		return Inquiry{}, &ValidationError{
			Field:   "title",
			Message: fmt.Sprintf("must be at most %d characters", MaxTitleLength),
		}
	}

	body := strings.TrimSpace(in.Body)
	if utf8.RuneCountInString(body) < MinBodyLength {
		return Inquiry{}, &ValidationError{Field: "body", Message: "is required"}
	}
	if utf8.RuneCountInString(body) > MaxBodyLength {
		return Inquiry{}, &ValidationError{
			Field:   "body",
			Message: fmt.Sprintf("must be at most %d characters", MaxBodyLength),
		}
	}

	if !language.IsValid(in.Language) {
		return Inquiry{}, &ValidationError{
			Field:   "language",
			Message: "must be a valid ISO 639-3 language code",
		}
	}

	place := strings.TrimSpace(in.Place)
	if place != "" {
		if utf8.RuneCountInString(place) < MinPlaceLength {
			return Inquiry{}, &ValidationError{Field: "place", Message: "is required"}
		}
		if strings.ContainsAny(place, "\n\r\t") {
			return Inquiry{}, &ValidationError{Field: "place", Message: "must be a single line with no tabs"}
		}
		if utf8.RuneCountInString(place) > MaxPlaceLength {
			return Inquiry{}, &ValidationError{
				Field:   "place",
				Message: fmt.Sprintf("must be at most %d characters", MaxPlaceLength),
			}
		}
	}
	var inquiryPlace *string
	if place != "" {
		inquiryPlace = &place
	}

	latitude, longitude, err := validateCoordinates(in.Latitude, in.Longitude)
	if err != nil {
		return Inquiry{}, err
	}

	country := strings.TrimSpace(in.PlaceCountry)
	if utf8.RuneCountInString(country) > MaxPlaceCountryLength {
		return Inquiry{}, &ValidationError{
			Field:   "place_country",
			Message: fmt.Sprintf("must be at most %d characters", MaxPlaceCountryLength),
		}
	}
	var placeCountry *string
	if country != "" {
		placeCountry = &country
	}

	return Inquiry{
		Title:        title,
		Body:         body,
		Language:     in.Language,
		Place:        inquiryPlace,
		PlaceCountry: placeCountry,
		Latitude:     latitude,
		Longitude:    longitude,
	}, nil
}

// validateCreateAnswer applies every input rule and returns the answer to store.
func validateCreateAnswer(in CreateAnswerInput) (Answer, error) {
	body := strings.TrimSpace(in.Body)
	if utf8.RuneCountInString(body) < MinBodyLength {
		return Answer{}, &ValidationError{Field: "body", Message: "is required"}
	}
	if utf8.RuneCountInString(body) > MaxAnswerBodyLength {
		return Answer{}, &ValidationError{
			Field:   "body",
			Message: fmt.Sprintf("must be at most %d characters", MaxAnswerBodyLength),
		}
	}

	if !language.IsValid(in.Language) {
		return Answer{}, &ValidationError{
			Field:   "language",
			Message: "must be a valid ISO 639-3 language code",
		}
	}

	return Answer{Body: body, Language: in.Language}, nil
}

// validateCoordinates enforces that a coordinate is supplied as a pair, with each
// component in range, and returns the values to store.
//
// It duplicates the rooted and stories domains' helper rather than sharing one:
// the three live in different bounded contexts and the helper is a few lines of
// range checking (KNOT-ADR-010). A lone latitude or longitude is rejected, and
// supplying neither is valid and yields two nils.
func validateCoordinates(latitude, longitude *float64) (*float64, *float64, error) {
	if latitude == nil && longitude == nil {
		return nil, nil, nil
	}
	if latitude == nil || longitude == nil {
		return nil, nil, &ValidationError{
			Field:   "latitude",
			Message: "latitude and longitude must be provided together",
		}
	}

	if math.IsNaN(*latitude) || *latitude < -90 || *latitude > 90 {
		return nil, nil, &ValidationError{Field: "latitude", Message: "must be between -90 and 90"}
	}
	if math.IsNaN(*longitude) || *longitude < -180 || *longitude > 180 {
		return nil, nil, &ValidationError{Field: "longitude", Message: "must be between -180 and 180"}
	}

	return latitude, longitude, nil
}
