package versions

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"unicode/utf8"
)

// CreateAdaptation validates the input, checks that the parent version belongs
// to the same story, stores the new version, and returns it.
//
// It returns a *ValidationError for bad input, and ErrNotFound when the parent
// version does not exist (or the story id is not a UUID, so it cannot name a
// story). The stored version's id and timestamps come from the store, so the
// returned value is whatever was actually persisted.
func (s *Service) CreateAdaptation(ctx context.Context, in CreateAdaptationInput) (StoryVersion, error) {
	version, err := validateCreateAdaptation(in)
	if err != nil {
		return StoryVersion{}, err
	}

	// The parent must exist and belong to the same story. This is the one rule
	// that cannot be checked without the store: it needs the parent's own
	// story_id, which only the stored row carries.
	parent, err := s.store.GetVersion(ctx, version.ParentVersionID)
	if err != nil {
		if errors.Is(err, ErrNotFound) {
			return StoryVersion{}, ErrNotFound
		}
		return StoryVersion{}, fmt.Errorf("versions: load parent version: %w", err)
	}
	if parent.StoryID != version.StoryID {
		return StoryVersion{}, &ValidationError{
			Field:   "parent_version_id",
			Message: "must belong to the same story",
		}
	}

	created, err := s.store.CreateVersion(ctx, version)
	if err != nil {
		return StoryVersion{}, fmt.Errorf("versions: create adaptation: %w", err)
	}

	return created, nil
}

// GetVersion returns a single version by id.
//
// An id that is not canonical UUID text is reported as ErrNotFound rather than
// as a validation error: the resource genuinely does not exist, and answering
// 404 keeps the id column's index usable instead of casting it to text in SQL.
func (s *Service) GetVersion(ctx context.Context, id string) (StoryVersion, error) {
	if !isUUID(id) {
		return StoryVersion{}, ErrNotFound
	}

	version, err := s.store.GetVersion(ctx, id)
	if err != nil {
		if errors.Is(err, ErrNotFound) {
			return StoryVersion{}, ErrNotFound
		}
		return StoryVersion{}, fmt.Errorf("versions: get version: %w", err)
	}

	return version, nil
}

// GetTree returns every version of one story as a flat list ordered oldest
// first, each carrying its parent pointer.
//
// The tree structure is not materialised here: it is an adjacency list, and
// assembling it into a nesting is the client's job. A story that does not exist
// is reported as ErrNotFound; a story with no versions would return an empty
// slice, which the schema makes unreachable (every story has a root version).
func (s *Service) GetTree(ctx context.Context, storyID string) ([]StoryVersion, error) {
	if !isUUID(storyID) {
		return nil, ErrNotFound
	}

	versions, err := s.store.ListByStory(ctx, storyID)
	if err != nil {
		if errors.Is(err, ErrNotFound) {
			return nil, ErrNotFound
		}
		return nil, fmt.Errorf("versions: list versions: %w", err)
	}

	if versions == nil {
		// Emit [] rather than null so a client never has to special-case an
		// empty tree.
		versions = []StoryVersion{}
	}

	return versions, nil
}

// validateCreateAdaptation applies every input rule that does not need the
// database and returns the version to store.
//
// Title, language, and adaptation_note are trimmed because surrounding
// whitespace is never meaningful there. Body is stored verbatim so the author's
// formatting survives; it is only trimmed to decide whether it is empty.
func validateCreateAdaptation(in CreateAdaptationInput) (StoryVersion, error) {
	// A story id from the request path that is not a UUID cannot name a story,
	// so it is reported as not-found rather than as a validation failure.
	if !isUUID(in.StoryID) {
		return StoryVersion{}, ErrNotFound
	}

	if !isUUID(in.AuthorID) {
		return StoryVersion{}, &ValidationError{Field: "author_id", Message: "must be a UUID"}
	}

	// parent_version_id is a required body field, so a non-UUID is a malformed
	// request. A well-formed id that names no version is reported as not-found
	// by the parent lookup instead.
	if !isUUID(in.ParentVersionID) {
		return StoryVersion{}, &ValidationError{Field: "parent_version_id", Message: "must be a UUID"}
	}

	title := strings.TrimSpace(in.Title)
	if utf8.RuneCountInString(title) < minTitleLen {
		return StoryVersion{}, &ValidationError{Field: "title", Message: "is required"}
	}
	if utf8.RuneCountInString(title) > MaxTitleLen {
		return StoryVersion{}, &ValidationError{
			Field:   "title",
			Message: fmt.Sprintf("must be at most %d characters", MaxTitleLen),
		}
	}

	if strings.TrimSpace(in.Body) == "" {
		return StoryVersion{}, &ValidationError{Field: "body", Message: "is required"}
	}
	if utf8.RuneCountInString(in.Body) > MaxBodyLen {
		return StoryVersion{}, &ValidationError{
			Field:   "body",
			Message: fmt.Sprintf("must be at most %d characters", MaxBodyLen),
		}
	}

	language, err := validateLanguage(in.Language)
	if err != nil {
		return StoryVersion{}, err
	}

	note := strings.TrimSpace(in.AdaptationNote)
	if utf8.RuneCountInString(note) > MaxAdaptationNoteLength {
		return StoryVersion{}, &ValidationError{
			Field:   "adaptation_note",
			Message: fmt.Sprintf("must be at most %d characters", MaxAdaptationNoteLength),
		}
	}

	return StoryVersion{
		StoryID:         in.StoryID,
		ParentVersionID: in.ParentVersionID,
		AuthorID:        in.AuthorID,
		Language:        language,
		Title:           title,
		Body:            in.Body,
		AdaptationNote:  note,
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
