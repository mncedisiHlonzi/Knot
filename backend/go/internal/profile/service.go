package profile

import (
	"context"
	"errors"
	"fmt"

	"github.com/knot/backend/internal/identity"
)

// Service holds the profile-wall business rules.
//
// It depends on a UserLookup and an ActivityStore, and knows nothing about HTTP,
// JSON wire framing, or SQL.
type Service struct {
	users      UserLookup
	activities ActivityStore
}

// NewService wires the user lookup and the activity store into the profile
// domain.
func NewService(users UserLookup, activities ActivityStore) (*Service, error) {
	if users == nil {
		return nil, fmt.Errorf("profile: service requires a user lookup")
	}
	if activities == nil {
		return nil, fmt.Errorf("profile: service requires an activity store")
	}
	return &Service{users: users, activities: activities}, nil
}

// GetProfile returns one user's public wall: their identity header and one page
// of their activity, newest first, plus the cursor that resumes after it.
//
// It returns ErrNotFound when the user does not exist (or the id is not a UUID,
// so it cannot name a user), and a *ValidationError for a bad limit or an
// unreadable cursor. An empty rawCursor asks for the first page; the returned
// next cursor is the empty string when the page is the last one.
//
// The owner is resolved first, so a 404 is reported even when the user has no
// activity at all.
func (s *Service) GetProfile(ctx context.Context, userID string, rawCursor string, limit int) (Profile, string, error) {
	if !isUUID(userID) {
		return Profile{}, "", ErrNotFound
	}
	if limit < 1 || limit > MaxListLimit {
		return Profile{}, "", &ValidationError{
			Field:   "limit",
			Message: fmt.Sprintf("must be between 1 and %d", MaxListLimit),
		}
	}

	var cursor *Cursor
	if rawCursor != "" {
		decoded, err := DecodeCursor(rawCursor)
		if err != nil {
			return Profile{}, "", err
		}
		cursor = &decoded
	}

	user, err := s.users.UserByID(ctx, userID)
	if err != nil {
		if errors.Is(err, identity.ErrUserNotFound) {
			return Profile{}, "", ErrNotFound
		}
		return Profile{}, "", fmt.Errorf("profile: load user: %w", err)
	}

	page, next, err := s.activities.ListForUser(ctx, userID, cursor, limit)
	if err != nil {
		return Profile{}, "", fmt.Errorf("profile: list activities: %w", err)
	}

	if page == nil {
		// Emit [] rather than null so a client never has to special-case an empty
		// wall.
		page = []Activity{}
	}

	nextCursor := ""
	if next != nil {
		nextCursor = next.Encode()
	}

	return Profile{User: user, Activities: page}, nextCursor, nil
}
