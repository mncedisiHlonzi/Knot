package identity

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"
)

// fakeStore is an in-memory UserStore used to test the service without a
// database. It mimics the behaviour the service depends on: unique emails and
// nullable columns.
type fakeStore struct {
	byEmail   map[string]*User
	byID      map[string]*User
	createErr error
	findErr   error
	seq       int
}

func newFakeStore() *fakeStore {
	return &fakeStore{
		byEmail: make(map[string]*User),
		byID:    make(map[string]*User),
	}
}

func (s *fakeStore) CreateUser(_ context.Context, user *User) (*User, error) {
	if s.createErr != nil {
		return nil, s.createErr
	}

	email := NormalizeEmail(user.Email)
	if _, exists := s.byEmail[email]; exists {
		return nil, ErrEmailTaken
	}

	s.seq++
	stored := *user
	stored.ID = fmt.Sprintf("11111111-1111-4111-8111-%012d", s.seq)
	stored.Email = email
	stored.CreatedAt = time.Now().UTC().Truncate(time.Microsecond)
	stored.UpdatedAt = stored.CreatedAt
	stored.PreferredLanguages = nonNilLanguages(stored.PreferredLanguages)

	s.byEmail[email] = &stored
	s.byID[stored.ID] = &stored

	return &stored, nil
}

func (s *fakeStore) FindUserByEmail(_ context.Context, email string) (*User, error) {
	if s.findErr != nil {
		return nil, s.findErr
	}

	user, ok := s.byEmail[NormalizeEmail(email)]
	if !ok {
		return nil, ErrUserNotFound
	}

	return user, nil
}

func (s *fakeStore) FindUserByID(_ context.Context, id string) (*User, error) {
	if s.findErr != nil {
		return nil, s.findErr
	}

	user, ok := s.byID[id]
	if !ok {
		return nil, ErrUserNotFound
	}

	return user, nil
}

func (s *fakeStore) FindUsersByID(_ context.Context, ids []string) ([]*User, error) {
	if s.findErr != nil {
		return nil, s.findErr
	}

	found := make([]*User, 0, len(ids))
	for _, id := range ids {
		if user, ok := s.byID[id]; ok {
			found = append(found, user)
		}
	}

	return found, nil
}

func (s *fakeStore) UpdateAvatarURL(_ context.Context, userID, avatarURL string) (*User, error) {
	if s.findErr != nil {
		return nil, s.findErr
	}

	user, ok := s.byID[userID]
	if !ok {
		return nil, ErrUserNotFound
	}

	user.AvatarURL = avatarURL
	user.UpdatedAt = time.Now().UTC().Truncate(time.Microsecond)

	return user, nil
}

// FindActiveUserByEmail mirrors the store's deletion filter. The fake keeps no
// deleted users, so it is the plain lookup.
func (s *fakeStore) FindActiveUserByEmail(ctx context.Context, email string) (*User, error) {
	return s.FindUserByEmail(ctx, email)
}

// FindActiveUserByID mirrors the store's deletion filter.
func (s *fakeStore) FindActiveUserByID(ctx context.Context, id string) (*User, error) {
	return s.FindUserByID(ctx, id)
}

// MarkEmailVerified mirrors the store's COALESCE: the first proof wins.
func (s *fakeStore) MarkEmailVerified(_ context.Context, userID string) (*User, error) {
	if s.findErr != nil {
		return nil, s.findErr
	}

	user, ok := s.byID[userID]
	if !ok {
		return nil, ErrUserNotFound
	}

	if user.EmailVerifiedAt == nil {
		verifiedAt := time.Now().UTC().Truncate(time.Microsecond)
		user.EmailVerifiedAt = &verifiedAt
	}

	return user, nil
}

// UpdatePasswordHash replaces the stored hash, as the real store's UPDATE does.
func (s *fakeStore) UpdatePasswordHash(_ context.Context, userID, passwordHash string) (*User, error) {
	if s.findErr != nil {
		return nil, s.findErr
	}

	user, ok := s.byID[userID]
	if !ok {
		return nil, ErrUserNotFound
	}

	user.PasswordHash = passwordHash
	user.UpdatedAt = time.Now().UTC().Truncate(time.Microsecond)

	return user, nil
}

// MarkDeleted mirrors the store's COALESCE: the first request fixes the timestamps.
func (s *fakeStore) MarkDeleted(_ context.Context, userID string) (*User, error) {
	user, ok := s.byID[userID]
	if !ok {
		return nil, ErrUserNotFound
	}

	if user.DeletedAt == nil {
		deletedAt := time.Now().UTC().Truncate(time.Microsecond)
		scheduled := deletedAt.Add(DeletionRecoveryWindow)
		user.DeletedAt = &deletedAt
		user.DeletionScheduledAt = &scheduled
	}

	return user, nil
}

// ClearDeletion mirrors the store: clearing a live account is a no-op success.
func (s *fakeStore) ClearDeletion(_ context.Context, userID string) (*User, error) {
	user, ok := s.byID[userID]
	if !ok {
		return nil, ErrUserNotFound
	}

	user.DeletedAt = nil
	user.DeletionScheduledAt = nil

	return user, nil
}

// PurgeDueUsers mirrors the store's anonymisation, including the idempotency guard.
func (s *fakeStore) PurgeDueUsers(_ context.Context, now time.Time) (int, error) {
	purged := 0

	for id, user := range s.byID {
		if user.DeletedAt == nil || user.PurgedAt != nil || user.DeletionScheduledAt == nil {
			continue
		}
		if now.Before(*user.DeletionScheduledAt) {
			continue
		}

		delete(s.byEmail, NormalizeEmail(user.Email))
		user.Email = "purged+" + id + purgeAnonymisedEmailDomain
		user.DisplayName = "Deleted User"
		user.Phone = ""
		user.ApproximateLocation = ""
		user.AvatarURL = ""
		user.PreferredLanguages = []string{}
		user.PasswordHash = "!purged"
		purgedAt := now
		user.PurgedAt = &purgedAt
		s.byEmail[user.Email] = user
		purged++
	}

	return purged, nil
}

func newTestService(t *testing.T, store UserStore) *Service {
	t.Helper()

	issuer, err := NewTokenIssuer("test-secret-that-is-at-least-32-bytes-long")
	if err != nil {
		t.Fatalf("NewTokenIssuer() error = %v, want nil", err)
	}

	service, err := NewService(store, issuer)
	if err != nil {
		t.Fatalf("NewService() error = %v, want nil", err)
	}

	return service
}

func validRegisterInput() RegisterInput {
	return RegisterInput{
		Email:               "Ada@Example.COM",
		Password:            "correct horse battery staple 7",
		DisplayName:         "Ada Lovelace",
		PreferredLanguages:  []string{"eng", " fra "},
		ApproximateLocation: "Cape Town",
		Phone:               "+27000000000",
	}
}

func TestNewServiceRejectsMissingDependencies(t *testing.T) {
	issuer, err := NewTokenIssuer("test-secret-that-is-at-least-32-bytes-long")
	if err != nil {
		t.Fatalf("NewTokenIssuer() error = %v, want nil", err)
	}

	if _, err := NewService(nil, issuer); err == nil {
		t.Error("NewService(nil, issuer) error = nil, want an error")
	}
	if _, err := NewService(newFakeStore(), nil); err == nil {
		t.Error("NewService(store, nil) error = nil, want an error")
	}
}

func TestRegisterSuccess(t *testing.T) {
	service := newTestService(t, newFakeStore())

	result, err := service.Register(context.Background(), validRegisterInput())
	if err != nil {
		t.Fatalf("Register() error = %v, want nil", err)
	}

	if result.User.ID == "" {
		t.Error("user id is empty, want a generated id")
	}
	if result.User.Email != "ada@example.com" {
		t.Errorf("email = %q, want it normalised to lower case", result.User.Email)
	}
	if result.User.DisplayName != "Ada Lovelace" {
		t.Errorf("display name = %q, want %q", result.User.DisplayName, "Ada Lovelace")
	}
	if result.User.Phone != "+27000000000" {
		t.Errorf("phone = %q, want %q", result.User.Phone, "+27000000000")
	}
	if result.User.ApproximateLocation != "Cape Town" {
		t.Errorf("location = %q, want %q", result.User.ApproximateLocation, "Cape Town")
	}
	if len(result.User.PreferredLanguages) != 2 || result.User.PreferredLanguages[1] != "fra" {
		t.Errorf("languages = %v, want [eng fra] with entries trimmed", result.User.PreferredLanguages)
	}

	if result.Tokens.AccessToken == "" {
		t.Error("access token is empty, want a signed token")
	}
	if result.Tokens.RefreshToken == "" {
		t.Error("refresh token is empty, want a signed token")
	}
	if result.Tokens.ExpiresIn != int(AccessTokenTTL.Seconds()) {
		t.Errorf("expires_in = %d, want %d", result.Tokens.ExpiresIn, int(AccessTokenTTL.Seconds()))
	}
}

func TestRegisterStoresAHashedPassword(t *testing.T) {
	store := newFakeStore()
	service := newTestService(t, store)

	in := validRegisterInput()
	result, err := service.Register(context.Background(), in)
	if err != nil {
		t.Fatalf("Register() error = %v, want nil", err)
	}

	if result.User.PasswordHash == in.Password {
		t.Fatal("stored password hash equals the plaintext password")
	}

	ok, err := VerifyPassword(result.User.PasswordHash, in.Password)
	if err != nil {
		t.Fatalf("VerifyPassword() error = %v, want nil", err)
	}
	if !ok {
		t.Error("stored hash does not verify against the original password")
	}
}

func TestRegisterRejectsDuplicateEmail(t *testing.T) {
	service := newTestService(t, newFakeStore())

	if _, err := service.Register(context.Background(), validRegisterInput()); err != nil {
		t.Fatalf("first Register() error = %v, want nil", err)
	}

	// Same address, different case: it must still be a duplicate.
	duplicate := validRegisterInput()
	duplicate.Email = "ADA@example.com"

	_, err := service.Register(context.Background(), duplicate)
	if !errors.Is(err, ErrEmailTaken) {
		t.Errorf("second Register() error = %v, want ErrEmailTaken", err)
	}
}

func TestRegisterRejectsWeakPassword(t *testing.T) {
	service := newTestService(t, newFakeStore())

	for _, password := range []string{"", "short", "1234567"} {
		t.Run(password, func(t *testing.T) {
			in := validRegisterInput()
			in.Password = password

			_, err := service.Register(context.Background(), in)

			var validation *ValidationError
			if !errors.As(err, &validation) {
				t.Fatalf("Register() error = %v, want *ValidationError", err)
			}
			if validation.Field != "password" {
				t.Errorf("validation field = %q, want %q", validation.Field, "password")
			}
		})
	}
}

func TestRegisterRejectsInvalidEmail(t *testing.T) {
	service := newTestService(t, newFakeStore())

	for _, email := range []string{"", "   ", "not-an-email", "Knot <ada@example.com>", "@example.com"} {
		t.Run(email, func(t *testing.T) {
			in := validRegisterInput()
			in.Email = email

			_, err := service.Register(context.Background(), in)

			var validation *ValidationError
			if !errors.As(err, &validation) {
				t.Fatalf("Register() error = %v, want *ValidationError", err)
			}
			if validation.Field != "email" {
				t.Errorf("validation field = %q, want %q", validation.Field, "email")
			}
		})
	}
}

func TestRegisterRejectsInvalidDisplayName(t *testing.T) {
	service := newTestService(t, newFakeStore())

	for name, displayName := range map[string]string{
		"empty":    "",
		"blank":    "   ",
		"too long": string(make([]rune, maxDisplayNameLen+1)),
	} {
		t.Run(name, func(t *testing.T) {
			in := validRegisterInput()
			in.DisplayName = displayName

			_, err := service.Register(context.Background(), in)

			var validation *ValidationError
			if !errors.As(err, &validation) {
				t.Fatalf("Register() error = %v, want *ValidationError", err)
			}
			if validation.Field != "display_name" {
				t.Errorf("validation field = %q, want %q", validation.Field, "display_name")
			}
		})
	}
}

func TestLoginSuccess(t *testing.T) {
	service := newTestService(t, newFakeStore())
	in := validRegisterInput()

	if _, err := service.Register(context.Background(), in); err != nil {
		t.Fatalf("Register() error = %v, want nil", err)
	}

	result, err := service.Login(context.Background(), LoginInput{
		Email:    in.Email,
		Password: in.Password,
	})
	if err != nil {
		t.Fatalf("Login() error = %v, want nil", err)
	}

	if result.User.Email != "ada@example.com" {
		t.Errorf("email = %q, want %q", result.User.Email, "ada@example.com")
	}
	if result.Tokens.AccessToken == "" || result.Tokens.RefreshToken == "" {
		t.Error("tokens are empty, want a freshly issued pair")
	}
}

func TestLoginAcceptsMixedCaseEmail(t *testing.T) {
	service := newTestService(t, newFakeStore())
	in := validRegisterInput()

	if _, err := service.Register(context.Background(), in); err != nil {
		t.Fatalf("Register() error = %v, want nil", err)
	}

	if _, err := service.Login(context.Background(), LoginInput{
		Email:    "ADA@EXAMPLE.COM",
		Password: in.Password,
	}); err != nil {
		t.Errorf("Login() error = %v, want nil for a differently-cased email", err)
	}
}

func TestLoginRejectsWrongPassword(t *testing.T) {
	service := newTestService(t, newFakeStore())
	in := validRegisterInput()

	if _, err := service.Register(context.Background(), in); err != nil {
		t.Fatalf("Register() error = %v, want nil", err)
	}

	_, err := service.Login(context.Background(), LoginInput{
		Email:    in.Email,
		Password: "the-wrong-password",
	})
	if !errors.Is(err, ErrInvalidCredentials) {
		t.Errorf("Login() error = %v, want ErrInvalidCredentials", err)
	}
}

func TestLoginRejectsUnknownEmail(t *testing.T) {
	service := newTestService(t, newFakeStore())

	_, err := service.Login(context.Background(), LoginInput{
		Email:    "nobody@example.com",
		Password: "correct horse battery staple 7",
	})
	if !errors.Is(err, ErrInvalidCredentials) {
		t.Errorf("Login() error = %v, want ErrInvalidCredentials", err)
	}
}

// TestLoginDoesNotEnumerateAccounts is the security-critical assertion: a wrong
// password and an unknown email must be indistinguishable to the caller.
func TestLoginDoesNotEnumerateAccounts(t *testing.T) {
	service := newTestService(t, newFakeStore())
	in := validRegisterInput()

	if _, err := service.Register(context.Background(), in); err != nil {
		t.Fatalf("Register() error = %v, want nil", err)
	}

	_, wrongPasswordErr := service.Login(context.Background(), LoginInput{
		Email:    in.Email,
		Password: "the-wrong-password",
	})
	_, unknownEmailErr := service.Login(context.Background(), LoginInput{
		Email:    "nobody@example.com",
		Password: in.Password,
	})

	if !errors.Is(wrongPasswordErr, ErrInvalidCredentials) {
		t.Fatalf("wrong-password error = %v, want ErrInvalidCredentials", wrongPasswordErr)
	}
	if !errors.Is(unknownEmailErr, ErrInvalidCredentials) {
		t.Fatalf("unknown-email error = %v, want ErrInvalidCredentials", unknownEmailErr)
	}
	if wrongPasswordErr.Error() != unknownEmailErr.Error() {
		t.Errorf(
			"error messages differ and leak account existence: wrong password = %q, unknown email = %q",
			wrongPasswordErr.Error(), unknownEmailErr.Error(),
		)
	}
}

func TestLoginRejectsEmptyInput(t *testing.T) {
	service := newTestService(t, newFakeStore())

	for name, in := range map[string]LoginInput{
		"empty":         {},
		"missing email": {Password: "correct horse battery staple 7"},
		"missing pass":  {Email: "ada@example.com"},
	} {
		t.Run(name, func(t *testing.T) {
			if _, err := service.Login(context.Background(), in); !errors.Is(err, ErrInvalidCredentials) {
				t.Errorf("Login() error = %v, want ErrInvalidCredentials", err)
			}
		})
	}
}

func TestRegisterPropagatesStoreFailures(t *testing.T) {
	store := newFakeStore()
	store.createErr = errors.New("connection reset")
	service := newTestService(t, store)

	_, err := service.Register(context.Background(), validRegisterInput())
	if err == nil {
		t.Fatal("Register() error = nil, want the store failure to propagate")
	}
	if errors.Is(err, ErrEmailTaken) || errors.Is(err, ErrInvalidCredentials) {
		t.Errorf("Register() error = %v, want a wrapped internal error", err)
	}
}

func TestLoginPropagatesStoreFailures(t *testing.T) {
	store := newFakeStore()
	store.findErr = errors.New("connection reset")
	service := newTestService(t, store)

	_, err := service.Login(context.Background(), LoginInput{Email: "ada@example.com", Password: "whatever-long-enough"})
	if err == nil {
		t.Fatal("Login() error = nil, want the store failure to propagate")
	}
	if errors.Is(err, ErrInvalidCredentials) {
		t.Errorf("Login() error = %v, want an internal error rather than invalid credentials", err)
	}
}

// registerUser creates a user through the service, so the tests below run
// against a row the store actually holds.
func registerUser(t *testing.T, service *Service) *User {
	t.Helper()

	result, err := service.Register(context.Background(), validRegisterInput())
	if err != nil {
		t.Fatalf("Register() error = %v, want nil", err)
	}

	return result.User
}

func TestUserByID(t *testing.T) {
	service := newTestService(t, newFakeStore())
	user := registerUser(t, service)

	found, err := service.UserByID(context.Background(), user.ID)
	if err != nil {
		t.Fatalf("UserByID() error = %v, want nil", err)
	}
	if found.ID != user.ID {
		t.Errorf("id = %q, want %q", found.ID, user.ID)
	}
	if found.AvatarURL != "" {
		t.Errorf("avatar_url = %q, want empty for a fresh user", found.AvatarURL)
	}
}

func TestUserByIDMissing(t *testing.T) {
	service := newTestService(t, newFakeStore())

	_, err := service.UserByID(context.Background(), "00000000-0000-4000-8000-000000000000")
	if !errors.Is(err, ErrUserNotFound) {
		t.Errorf("UserByID() error = %v, want ErrUserNotFound", err)
	}
}

func TestUserByIDPropagatesStoreFailures(t *testing.T) {
	store := newFakeStore()
	store.findErr = errors.New("connection reset")
	service := newTestService(t, store)

	_, err := service.UserByID(context.Background(), "11111111-1111-4111-8111-111111111111")
	if err == nil {
		t.Fatal("UserByID() error = nil, want the store failure to propagate")
	}
	if errors.Is(err, ErrUserNotFound) {
		t.Errorf("UserByID() error = %v, want an internal error rather than not-found", err)
	}
}

func TestUsersByIDs(t *testing.T) {
	service := newTestService(t, newFakeStore())
	first := registerUser(t, service)

	second, err := service.Register(context.Background(), RegisterInput{
		Email:       "grace@example.com",
		Password:    "correct horse battery staple 7",
		DisplayName: "Grace Hopper",
	})
	if err != nil {
		t.Fatalf("Register() error = %v, want nil", err)
	}

	// A duplicate, a malformed id, and an id with no row are all tolerated: an
	// inbox must still render when one of its actors has gone.
	users, err := service.UsersByIDs(context.Background(), []string{
		first.ID,
		second.User.ID,
		first.ID,
		"not-a-uuid",
		"00000000-0000-4000-8000-000000000000",
	})
	if err != nil {
		t.Fatalf("UsersByIDs() error = %v, want nil", err)
	}

	if len(users) != 2 {
		t.Fatalf("resolved %d users, want 2", len(users))
	}
	if users[first.ID] == nil || users[first.ID].Email != first.Email {
		t.Errorf("first user = %+v, want the registered %q", users[first.ID], first.Email)
	}
	if users[second.User.ID] == nil || users[second.User.ID].DisplayName != "Grace Hopper" {
		t.Errorf("second user = %+v, want the second registered account", users[second.User.ID])
	}
}

func TestUsersByIDsEmptyInput(t *testing.T) {
	service := newTestService(t, newFakeStore())

	for _, ids := range [][]string{nil, {}, {"not-a-uuid"}} {
		users, err := service.UsersByIDs(context.Background(), ids)
		if err != nil {
			t.Fatalf("UsersByIDs(%v) error = %v, want nil", ids, err)
		}
		if len(users) != 0 {
			t.Errorf("UsersByIDs(%v) returned %d users, want 0", ids, len(users))
		}
	}
}

func TestUsersByIDsPropagatesStoreFailures(t *testing.T) {
	store := newFakeStore()
	store.findErr = errors.New("connection reset")
	service := newTestService(t, store)

	_, err := service.UsersByIDs(context.Background(), []string{"11111111-1111-4111-8111-111111111111"})
	if err == nil {
		t.Fatal("UsersByIDs() error = nil, want the store failure to propagate")
	}
}

func TestSetAvatarURLStoresAKeyInTheUsersOwnNamespace(t *testing.T) {
	service := newTestService(t, newFakeStore())
	user := registerUser(t, service)

	key := AvatarKeyPrefix(user.ID) + "01234567-89ab-4def-8123-456789abcdef.png"

	updated, err := service.SetAvatarURL(context.Background(), user.ID, key)
	if err != nil {
		t.Fatalf("SetAvatarURL() error = %v, want nil", err)
	}
	if updated.AvatarURL != key {
		t.Errorf("avatar_url = %q, want %q", updated.AvatarURL, key)
	}
}

// TestSetAvatarURLRejectsKeysOutsideTheUsersNamespace is the guard that keeps a
// mistake in the HTTP layer from pointing one account's avatar at another
// account's object, or at anything else in the bucket.
func TestSetAvatarURLRejectsKeysOutsideTheUsersNamespace(t *testing.T) {
	const otherID = "22222222-2222-4222-8222-222222222222"

	tests := []struct {
		name string
		key  func(ownPrefix string) string
	}{
		{name: "empty", key: func(string) string { return "" }},
		{name: "another user's prefix", key: func(string) string { return AvatarKeyPrefix(otherID) + "a.png" }},
		{name: "the namespace itself", key: func(ownPrefix string) string { return ownPrefix }},
		{name: "no namespace at all", key: func(string) string { return "a.png" }},
		{name: "a neighbouring namespace", key: func(ownPrefix string) string { return strings.TrimSuffix(ownPrefix, "/") + "-x/a.png" }},
		{name: "nested under the prefix", key: func(ownPrefix string) string { return ownPrefix + "nested/a.png" }},
		{name: "traversal out of the prefix", key: func(ownPrefix string) string { return ownPrefix + "../" + otherID + "/a.png" }},
		{name: "an absolute path", key: func(string) string { return "/etc/passwd" }},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			store := newFakeStore()
			service := newTestService(t, store)
			user := registerUser(t, service)

			key := test.key(AvatarKeyPrefix(user.ID))

			_, err := service.SetAvatarURL(context.Background(), user.ID, key)
			if err == nil {
				t.Fatalf("SetAvatarURL(%q) error = nil, want a validation error", key)
			}

			var validation *ValidationError
			if !errors.As(err, &validation) {
				t.Errorf("SetAvatarURL(%q) error = %v, want a *ValidationError", key, err)
			}

			if stored := store.byID[user.ID]; stored.AvatarURL != "" {
				t.Errorf("stored avatar_url = %q, want it untouched", stored.AvatarURL)
			}
		})
	}
}

func TestSetAvatarURLMissingUser(t *testing.T) {
	service := newTestService(t, newFakeStore())
	userID := "11111111-1111-4111-8111-111111111111"

	_, err := service.SetAvatarURL(context.Background(), userID, AvatarKeyPrefix(userID)+"a.png")
	if !errors.Is(err, ErrUserNotFound) {
		t.Errorf("SetAvatarURL() error = %v, want ErrUserNotFound", err)
	}
}

func TestSetAvatarURLPropagatesStoreFailures(t *testing.T) {
	store := newFakeStore()
	service := newTestService(t, store)
	user := registerUser(t, service)

	store.findErr = errors.New("connection reset")

	_, err := service.SetAvatarURL(context.Background(), user.ID, AvatarKeyPrefix(user.ID)+"a.png")
	if err == nil {
		t.Fatal("SetAvatarURL() error = nil, want the store failure to propagate")
	}
	if errors.Is(err, ErrUserNotFound) {
		t.Errorf("SetAvatarURL() error = %v, want an internal error rather than not-found", err)
	}
}

func TestAvatarKeyPrefix(t *testing.T) {
	const userID = "11111111-1111-4111-8111-111111111111"

	if got, want := AvatarKeyPrefix(userID), "avatars/"+userID+"/"; got != want {
		t.Errorf("AvatarKeyPrefix() = %q, want %q", got, want)
	}
}

// --- Account lifecycle (KNOT-034) -------------------------------------------

// lifecycleFixture returns a service over a fake store with one live account.
func lifecycleFixture(t *testing.T) (*Service, *fakeStore, *User) {
	t.Helper()

	store := newFakeStore()
	service := newTestService(t, store)

	hash, err := HashPassword("correct horse 7")
	if err != nil {
		t.Fatalf("HashPassword() error = %v, want nil", err)
	}

	user, err := store.CreateUser(context.Background(), &User{
		Email:        "ada@example.com",
		PasswordHash: hash,
		DisplayName:  "Ada Lovelace",
		Phone:        "+27000000000",
	})
	if err != nil {
		t.Fatalf("CreateUser() error = %v, want nil", err)
	}

	return service, store, user
}

func TestRequestDeletionSoftDeletesTheAccount(t *testing.T) {
	service, store, user := lifecycleFixture(t)
	ctx := context.Background()

	if err := service.RequestDeletion(ctx, user.ID, "correct horse 7"); err != nil {
		t.Fatalf("RequestDeletion() error = %v, want nil", err)
	}

	stored, _ := store.FindUserByID(ctx, user.ID)
	if stored.DeletedAt == nil {
		t.Fatal("deleted_at is nil, want the account soft-deleted")
	}
	if stored.DeletionScheduledAt == nil {
		t.Fatal("deletion_scheduled_at is nil, want the recovery window recorded")
	}

	window := stored.DeletionScheduledAt.Sub(*stored.DeletedAt)
	if window != DeletionRecoveryWindow {
		t.Errorf("recovery window = %s, want %s", window, DeletionRecoveryWindow)
	}

	// The row still exists: deletion is soft, and the content stays attributable.
	if _, err := store.FindUserByID(ctx, user.ID); err != nil {
		t.Errorf("FindUserByID() after deletion error = %v, want the row to remain", err)
	}
}

func TestRequestDeletionRequiresThePassword(t *testing.T) {
	service, store, user := lifecycleFixture(t)
	ctx := context.Background()

	if err := service.RequestDeletion(ctx, user.ID, "wrong horse 7"); !errors.Is(err, ErrInvalidCredentials) {
		t.Fatalf("RequestDeletion() error = %v, want ErrInvalidCredentials", err)
	}

	stored, _ := store.FindUserByID(ctx, user.ID)
	if stored.DeletedAt != nil {
		t.Error("the account was deleted despite a wrong password")
	}

	if err := service.RequestDeletion(ctx, "99999999-9999-4999-8999-999999999999", "correct horse 7"); !errors.Is(err, ErrUserNotFound) {
		t.Fatalf("RequestDeletion(unknown) error = %v, want ErrUserNotFound", err)
	}
}

// TestRequestDeletionIsIdempotent proves a retry cannot extend the window.
func TestRequestDeletionIsIdempotent(t *testing.T) {
	service, store, user := lifecycleFixture(t)
	ctx := context.Background()

	if err := service.RequestDeletion(ctx, user.ID, "correct horse 7"); err != nil {
		t.Fatalf("RequestDeletion() error = %v, want nil", err)
	}
	first, _ := store.FindUserByID(ctx, user.ID)

	if err := service.RequestDeletion(ctx, user.ID, "correct horse 7"); err != nil {
		t.Fatalf("second RequestDeletion() error = %v, want nil", err)
	}
	second, _ := store.FindUserByID(ctx, user.ID)

	if !first.DeletionScheduledAt.Equal(*second.DeletionScheduledAt) {
		t.Errorf("the window moved from %s to %s, want it unchanged", first.DeletionScheduledAt, second.DeletionScheduledAt)
	}
}

func TestCancelDeletionRestoresTheAccount(t *testing.T) {
	service, store, user := lifecycleFixture(t)
	ctx := context.Background()

	if err := service.RequestDeletion(ctx, user.ID, "correct horse 7"); err != nil {
		t.Fatalf("RequestDeletion() error = %v, want nil", err)
	}

	if err := service.CancelDeletion(ctx, user.ID); err != nil {
		t.Fatalf("CancelDeletion() error = %v, want nil", err)
	}

	stored, _ := store.FindUserByID(ctx, user.ID)
	if stored.DeletedAt != nil || stored.DeletionScheduledAt != nil {
		t.Errorf("deletion state = (%v, %v), want both cleared", stored.DeletedAt, stored.DeletionScheduledAt)
	}

	// Cancelling a live account is a harmless no-op.
	if err := service.CancelDeletion(ctx, user.ID); err != nil {
		t.Errorf("CancelDeletion(live) error = %v, want nil", err)
	}
}

func TestRecoverAccount(t *testing.T) {
	ctx := context.Background()

	t.Run("inside the window", func(t *testing.T) {
		service, store, user := lifecycleFixture(t)

		if err := service.RequestDeletion(ctx, user.ID, "correct horse 7"); err != nil {
			t.Fatalf("RequestDeletion() error = %v, want nil", err)
		}

		if err := service.RecoverAccount(ctx, "Ada@Example.com", "correct horse 7"); err != nil {
			t.Fatalf("RecoverAccount() error = %v, want nil", err)
		}

		stored, _ := store.FindUserByID(ctx, user.ID)
		if stored.DeletedAt != nil {
			t.Error("the account is still deleted after recovery")
		}
	})

	t.Run("wrong password", func(t *testing.T) {
		service, store, user := lifecycleFixture(t)

		if err := service.RequestDeletion(ctx, user.ID, "correct horse 7"); err != nil {
			t.Fatalf("RequestDeletion() error = %v, want nil", err)
		}

		if err := service.RecoverAccount(ctx, "ada@example.com", "nope"); !errors.Is(err, ErrInvalidCredentials) {
			t.Fatalf("RecoverAccount() error = %v, want ErrInvalidCredentials", err)
		}

		stored, _ := store.FindUserByID(ctx, user.ID)
		if stored.DeletedAt == nil {
			t.Error("the account was recovered with a wrong password")
		}
	})

	t.Run("past the window", func(t *testing.T) {
		service, store, user := lifecycleFixture(t)

		if err := service.RequestDeletion(ctx, user.ID, "correct horse 7"); err != nil {
			t.Fatalf("RequestDeletion() error = %v, want nil", err)
		}

		past := time.Now().UTC().Add(-time.Hour)
		store.byID[user.ID].DeletionScheduledAt = &past

		if err := service.RecoverAccount(ctx, "ada@example.com", "correct horse 7"); !errors.Is(err, ErrAccountPendingPurge) {
			t.Fatalf("RecoverAccount() error = %v, want ErrAccountPendingPurge", err)
		}
	})

	t.Run("unknown address", func(t *testing.T) {
		service, _, _ := lifecycleFixture(t)

		if err := service.RecoverAccount(ctx, "nobody@example.com", "correct horse 7"); !errors.Is(err, ErrInvalidCredentials) {
			t.Fatalf("RecoverAccount() error = %v, want ErrInvalidCredentials", err)
		}
	})

	t.Run("live account", func(t *testing.T) {
		service, _, _ := lifecycleFixture(t)

		if err := service.RecoverAccount(ctx, "ada@example.com", "correct horse 7"); err != nil {
			t.Fatalf("RecoverAccount(live) error = %v, want nil", err)
		}
	})
}

func TestPurgeDeletedUsersAnonymises(t *testing.T) {
	service, store, user := lifecycleFixture(t)
	ctx := context.Background()

	// Captured before the purge, because the fake mutates the row in place.
	originalHash := user.PasswordHash

	if err := service.RequestDeletion(ctx, user.ID, "correct horse 7"); err != nil {
		t.Fatalf("RequestDeletion() error = %v, want nil", err)
	}

	// Not yet due.
	purged, err := service.PurgeDeletedUsers(ctx)
	if err != nil {
		t.Fatalf("PurgeDeletedUsers() error = %v, want nil", err)
	}
	if purged != 0 {
		t.Fatalf("purged = %d, want 0 while the window is open", purged)
	}

	// Move the window into the past.
	past := time.Now().UTC().Add(-time.Minute)
	store.byID[user.ID].DeletionScheduledAt = &past

	purged, err = service.PurgeDeletedUsers(ctx)
	if err != nil {
		t.Fatalf("PurgeDeletedUsers() error = %v, want nil", err)
	}
	if purged != 1 {
		t.Fatalf("purged = %d, want 1", purged)
	}

	stored, _ := store.FindUserByID(ctx, user.ID)
	if stored.PurgedAt == nil {
		t.Fatal("purged_at is nil, want the account marked as purged")
	}
	if stored.Email == "ada@example.com" {
		t.Error("the address survived the purge")
	}
	if stored.DisplayName != "Deleted User" {
		t.Errorf("display name = %q, want %q", stored.DisplayName, "Deleted User")
	}
	if stored.Phone != "" || stored.ApproximateLocation != "" || stored.AvatarURL != "" {
		t.Error("a contact field survived the purge")
	}
	if stored.PasswordHash == originalHash {
		t.Error("the password hash survived the purge")
	}

	// Idempotent: a second run finds nothing to do.
	purged, err = service.PurgeDeletedUsers(ctx)
	if err != nil {
		t.Fatalf("second PurgeDeletedUsers() error = %v, want nil", err)
	}
	if purged != 0 {
		t.Fatalf("second purge = %d, want 0 (already purged)", purged)
	}
}

// fakeRevoker records the users whose sessions were ended.
type fakeRevoker struct {
	userIDs []string
	err     error
}

func (f *fakeRevoker) RevokeAllForUser(_ context.Context, userID string) error {
	if f.err != nil {
		return f.err
	}
	f.userIDs = append(f.userIDs, userID)

	return nil
}

// TestRequestDeletionEndsEverySession proves a deleted account's tokens stop working
// in the same request, rather than a quarter of an hour later.
func TestRequestDeletionEndsEverySession(t *testing.T) {
	ctx := context.Background()

	t.Run("revokes", func(t *testing.T) {
		store := newFakeStore()
		revoker := &fakeRevoker{}

		issuer, err := NewTokenIssuer("test-secret-that-is-at-least-32-bytes-long")
		if err != nil {
			t.Fatalf("NewTokenIssuer() error = %v, want nil", err)
		}

		service, err := NewService(store, issuer, WithSessionRevoker(revoker))
		if err != nil {
			t.Fatalf("NewService() error = %v, want nil", err)
		}

		hash, _ := HashPassword("correct horse 7")
		user, err := store.CreateUser(ctx, &User{Email: "ada@example.com", PasswordHash: hash, DisplayName: "Ada"})
		if err != nil {
			t.Fatalf("CreateUser() error = %v, want nil", err)
		}

		if err := service.RequestDeletion(ctx, user.ID, "correct horse 7"); err != nil {
			t.Fatalf("RequestDeletion() error = %v, want nil", err)
		}

		if len(revoker.userIDs) != 1 || revoker.userIDs[0] != user.ID {
			t.Fatalf("revoked users = %v, want exactly %q", revoker.userIDs, user.ID)
		}
	})

	t.Run("a failed revoke leaves the account alone", func(t *testing.T) {
		store := newFakeStore()
		revoker := &fakeRevoker{err: errors.New("session store down")}

		issuer, err := NewTokenIssuer("test-secret-that-is-at-least-32-bytes-long")
		if err != nil {
			t.Fatalf("NewTokenIssuer() error = %v, want nil", err)
		}

		service, err := NewService(store, issuer, WithSessionRevoker(revoker))
		if err != nil {
			t.Fatalf("NewService() error = %v, want nil", err)
		}

		hash, _ := HashPassword("correct horse 7")
		user, err := store.CreateUser(ctx, &User{Email: "ada@example.com", PasswordHash: hash, DisplayName: "Ada"})
		if err != nil {
			t.Fatalf("CreateUser() error = %v, want nil", err)
		}

		if err := service.RequestDeletion(ctx, user.ID, "correct horse 7"); err == nil {
			t.Fatal("RequestDeletion() error = nil, want the revoke failure")
		}

		stored, _ := store.FindUserByID(ctx, user.ID)
		if stored.DeletedAt != nil {
			t.Error("the account was deleted even though its sessions could not be ended")
		}
	})
}
