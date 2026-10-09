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

	email := normalizeEmail(user.Email)
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

	user, ok := s.byEmail[normalizeEmail(email)]
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
		Password:            "correct horse battery staple",
		DisplayName:         "Ada Lovelace",
		PreferredLanguages:  []string{"en", " fr "},
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
	if len(result.User.PreferredLanguages) != 2 || result.User.PreferredLanguages[1] != "fr" {
		t.Errorf("languages = %v, want [en fr] with entries trimmed", result.User.PreferredLanguages)
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
		Password: "correct horse battery staple",
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
		"missing email": {Password: "correct horse battery staple"},
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
