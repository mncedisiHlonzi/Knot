package identity

import "context"

// UserStore is the persistence contract for users.
//
// It is defined here, in the domain package, so the service layer depends on an
// abstraction rather than on pgx. Implementations must accept a context and must
// never return a partially populated User together with a nil error.
type UserStore interface {
	// CreateUser inserts a new user and returns the stored row, including the
	// database-generated id and timestamps. It returns ErrEmailTaken when the
	// email already exists.
	CreateUser(ctx context.Context, user *User) (*User, error)

	// FindUserByEmail returns the user with the given email, or ErrUserNotFound.
	// The email is matched case-insensitively.
	FindUserByEmail(ctx context.Context, email string) (*User, error)

	// FindUserByID returns the user with the given id, or ErrUserNotFound.
	// It is not used by Register or Login yet; it exists because verifying a JWT
	// subject requires a way to resolve a user id, which the next task needs.
	FindUserByID(ctx context.Context, id string) (*User, error)

	// FindActiveUserByEmail is FindUserByEmail except that an account inside its
	// deletion window is reported as ErrUserNotFound (KNOT-ADR-081).
	//
	// It exists so sign-in can refuse a deleted account without the User type
	// carrying deletion state: the caller that must not see it — the login
	// endpoint, which must not confirm a deletion to whoever is guessing — gets
	// the same answer as for an address that was never registered.
	FindActiveUserByEmail(ctx context.Context, email string) (*User, error)

	// FindActiveUserByID is FindUserByID with the same deletion filter. It is what
	// a token refresh uses: a session must not outlive the account it belongs to.
	FindActiveUserByID(ctx context.Context, id string) (*User, error)

	// UpdateAvatarURL stores the object key of the user's current avatar and
	// returns the stored row, including the refreshed updated_at. Passing ""
	// clears the avatar. It returns ErrUserNotFound when no such user exists.
	UpdateAvatarURL(ctx context.Context, userID, avatarURL string) (*User, error)

	// MarkEmailVerified records that the address was proven and returns the stored
	// row. It is idempotent: verifying an already verified account keeps the
	// original timestamp rather than moving it, because the first proof is the fact
	// worth keeping (KNOT-ADR-083). A malformed id and an id with no row are both
	// ErrUserNotFound.
	MarkEmailVerified(ctx context.Context, userID string) (*User, error)

	// UpdatePasswordHash replaces the stored password credential and returns the
	// updated row. It is how a password reset takes effect; callers are responsible
	// for revoking the sessions that were authorised by the old password.
	UpdatePasswordHash(ctx context.Context, userID, passwordHash string) (*User, error)

	// FindUsersByID returns the users with the given ids. A malformed id and an id
	// that matches no row are both simply absent from the result, and the order is
	// not guaranteed; an empty or entirely malformed input yields an empty slice
	// and a nil error.
	//
	// It exists so a page of authors can be resolved with one query rather than one
	// query per author: the notifications inbox renders the actor of every
	// notification in a page.
	FindUsersByID(ctx context.Context, ids []string) ([]*User, error)
}
