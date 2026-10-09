package profile

import "context"

// ActivityStore is the persistence contract for a profile wall. The service
// depends on this interface rather than on pgx, so the business rules can be
// tested without a database.
type ActivityStore interface {
	// ListForUser returns at most limit activities authored by userID, newest
	// first, starting after cursor (nil starts at the newest). The returned cursor
	// resumes after the page, or is nil when the page is the last one.
	//
	// It returns an empty slice, not ErrNotFound, for a user with no activity: the
	// owner's existence is the service's concern, not the store's. A malformed
	// userID that cannot match a row also yields an empty slice.
	ListForUser(ctx context.Context, userID string, cursor *Cursor, limit int) ([]Activity, *Cursor, error)
}
