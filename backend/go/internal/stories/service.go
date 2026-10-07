package stories

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"unicode/utf8"
)

// CreateStory validates the input, stores it, and returns the stored story.
//
// It returns a *ValidationError for bad input. The story's id, timestamps, and
// any database-maintained defaults come from the store, so the returned value is
// whatever was actually persisted rather than what was requested.
func (s *Service) CreateStory(ctx context.Context, in CreateStoryInput) (Story, error) {
	story, err := validateCreateStory(in)
	if err != nil {
		return Story{}, err
	}

	created, err := s.store.CreateStory(ctx, story)
	if err != nil {
		return Story{}, fmt.Errorf("stories: create story: %w", err)
	}

	return created, nil
}

// GetStory returns a single story by id.
//
// An id that is not canonical UUID text is reported as ErrNotFound rather than
// as a validation error: the resource genuinely does not exist, and answering
// 404 keeps the id column's index usable instead of casting it to text in SQL.
func (s *Service) GetStory(ctx context.Context, id string) (Story, error) {
	if !isUUID(id) {
		return Story{}, ErrNotFound
	}

	story, err := s.store.GetStory(ctx, id)
	if err != nil {
		if errors.Is(err, ErrNotFound) {
			return Story{}, ErrNotFound
		}
		return Story{}, fmt.Errorf("stories: get story: %w", err)
	}

	return story, nil
}

// ListStories returns one page of the feed, newest first, plus the cursor that
// resumes after it.
//
// An empty rawCursor asks for the first page. The returned next cursor is the
// empty string when the page is the last one, which is how the HTTP layer
// signals "no more pages" without inventing a null-versus-absent distinction.
//
// The limit is validated rather than clamped so that a caller passing an
// out-of-range value learns about it; the HTTP layer applies the
// DefaultListLimit/MaxListLimit policy before calling in.
func (s *Service) ListStories(ctx context.Context, rawCursor string, limit int) ([]Story, string, error) {
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

	page, next, err := s.store.ListStories(ctx, cursor, limit)
	if err != nil {
		return nil, "", fmt.Errorf("stories: list stories: %w", err)
	}

	if page == nil {
		// Emit [] rather than null so a client never has to special-case an
		// empty feed.
		page = []Story{}
	}

	nextCursor := ""
	if next != nil {
		nextCursor = next.Encode()
	}

	return page, nextCursor, nil
}

// validateCreateStory applies every input rule and returns the story to store.
//
// Title, language, and approximate_location are trimmed because surrounding
// whitespace is never meaningful there. Body is stored verbatim so that the
// author's formatting survives; it is only trimmed to decide whether it is
// empty.
func validateCreateStory(in CreateStoryInput) (Story, error) {
	if !isUUID(in.AuthorID) {
		return Story{}, &ValidationError{Field: "author_id", Message: "must be a UUID"}
	}

	if !in.Pillar.Valid() {
		return Story{}, &ValidationError{
			Field:   "pillar",
			Message: fmt.Sprintf("must be one of %s, %s", PillarWonder, PillarHeritage),
		}
	}

	title := strings.TrimSpace(in.Title)
	if utf8.RuneCountInString(title) < minTitleLen {
		return Story{}, &ValidationError{Field: "title", Message: "is required"}
	}
	if utf8.RuneCountInString(title) > MaxTitleLen {
		return Story{}, &ValidationError{
			Field:   "title",
			Message: fmt.Sprintf("must be at most %d characters", MaxTitleLen),
		}
	}

	if strings.TrimSpace(in.Body) == "" {
		return Story{}, &ValidationError{Field: "body", Message: "is required"}
	}
	if utf8.RuneCountInString(in.Body) > MaxBodyLen {
		return Story{}, &ValidationError{
			Field:   "body",
			Message: fmt.Sprintf("must be at most %d characters", MaxBodyLen),
		}
	}

	language, err := validateLanguage(in.Language)
	if err != nil {
		return Story{}, err
	}

	location := strings.TrimSpace(in.ApproximateLocation)
	if utf8.RuneCountInString(location) > MaxLocationLength {
		return Story{}, &ValidationError{
			Field:   "approximate_location",
			Message: fmt.Sprintf("must be at most %d characters", MaxLocationLength),
		}
	}

	media, err := validateMediaURLs(in.MediaURLs)
	if err != nil {
		return Story{}, err
	}

	return Story{
		AuthorID:            in.AuthorID,
		Pillar:              in.Pillar,
		Language:            language,
		Title:               title,
		Body:                in.Body,
		ApproximateLocation: location,
		MediaURLs:           media,
		Sensitive:           in.Sensitive,
	}, nil
}

// validateLanguage normalises a language tag to lower case and checks its
// length. Only ASCII letters are accepted: the 2-8 character bound in the
// product spec describes a simple tag, not a full BCP 47 production.
func validateLanguage(raw string) (string, error) {
	language := strings.ToLower(strings.TrimSpace(raw))

	if utf8.RuneCountInString(language) < minLanguageLen || utf8.RuneCountInString(language) > maxLanguageLen {
		return "", &ValidationError{
			Field:   "language",
			Message: fmt.Sprintf("must be between %d and %d characters", minLanguageLen, maxLanguageLen),
		}
	}

	for i := 0; i < len(language); i++ {
		if language[i] < 'a' || language[i] > 'z' {
			return "", &ValidationError{
				Field:   "language",
				Message: "must contain only letters",
			}
		}
	}

	return language, nil
}

// validateMediaURLs bounds the media list and normalises nil to an empty slice,
// so the TEXT[] NOT NULL column never receives NULL.
func validateMediaURLs(urls []string) ([]string, error) {
	if len(urls) > MaxMediaURLs {
		return nil, &ValidationError{
			Field:   "media_urls",
			Message: fmt.Sprintf("must contain at most %d entries", MaxMediaURLs),
		}
	}

	media := make([]string, 0, len(urls))
	for _, url := range urls {
		trimmed := strings.TrimSpace(url)
		if trimmed == "" {
			return nil, &ValidationError{Field: "media_urls", Message: "must not contain empty entries"}
		}
		if len(trimmed) > maxMediaURLLength {
			return nil, &ValidationError{
				Field:   "media_urls",
				Message: fmt.Sprintf("must contain only links of at most %d characters", maxMediaURLLength),
			}
		}
		media = append(media, trimmed)
	}

	return media, nil
}

// isUUID reports whether s is a canonical 8-4-4-4-12 hexadecimal UUID string.
//
// It duplicates the unexported validator in internal/identity rather than
// exporting that one: the alternative would widen the identity package's API for
// a helper that is three lines of character testing.
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
