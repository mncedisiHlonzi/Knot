package auth

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"log/slog"
	"sort"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/alicebob/miniredis/v2"

	"github.com/knot/backend/internal/cache"
	"github.com/knot/backend/internal/email"
	"github.com/knot/backend/internal/identity"
)

// The fakes below implement the stores the service depends on. They are
// deliberately simple and deterministic: they exist so every policy decision the
// service makes — the session cap, rotation, reuse detection, the verification
// window — can be tested without a database, while the real SQL is covered by the
// store integration tests.

// fakeUserStore is an in-memory identity.UserStore.
type fakeUserStore struct {
	mu       sync.Mutex
	byID     map[string]*identity.User
	byEmail  map[string]*identity.User
	deleted  map[string]bool
	seq      int
	createFn func(*identity.User) error
}

func newFakeUserStore() *fakeUserStore {
	return &fakeUserStore{
		byID:    map[string]*identity.User{},
		byEmail: map[string]*identity.User{},
		deleted: map[string]bool{},
	}
}

func (s *fakeUserStore) CreateUser(_ context.Context, user *identity.User) (*identity.User, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.createFn != nil {
		if err := s.createFn(user); err != nil {
			return nil, err
		}
	}

	email := identity.NormalizeEmail(user.Email)
	if _, exists := s.byEmail[email]; exists {
		return nil, identity.ErrEmailTaken
	}

	s.seq++
	stored := *user
	stored.ID = fmt.Sprintf("11111111-1111-4111-8111-%012d", s.seq)
	stored.Email = email
	stored.CreatedAt = time.Now().UTC()
	stored.UpdatedAt = stored.CreatedAt
	if stored.PreferredLanguages == nil {
		stored.PreferredLanguages = []string{}
	}

	s.byID[stored.ID] = &stored
	s.byEmail[email] = &stored

	return &stored, nil
}

func (s *fakeUserStore) FindUserByEmail(_ context.Context, email string) (*identity.User, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	// Deleted accounts are still findable by email: signing in is how an account
	// inside its recovery window is taken back (KNOT-ADR-081). The active lookup
	// below is the one that hides them.
	user, ok := s.byEmail[identity.NormalizeEmail(email)]
	if !ok {
		return nil, identity.ErrUserNotFound
	}

	return user, nil
}

func (s *fakeUserStore) FindUserByID(_ context.Context, id string) (*identity.User, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	user, ok := s.byID[id]
	if !ok || s.deleted[id] {
		return nil, identity.ErrUserNotFound
	}

	return user, nil
}

func (s *fakeUserStore) FindActiveUserByEmail(ctx context.Context, email string) (*identity.User, error) {
	user, err := s.FindUserByEmail(ctx, email)
	if err != nil {
		return nil, err
	}

	s.mu.Lock()
	deleted := s.deleted[user.ID]
	s.mu.Unlock()
	if deleted {
		return nil, identity.ErrUserNotFound
	}

	return user, nil
}

func (s *fakeUserStore) FindActiveUserByID(ctx context.Context, id string) (*identity.User, error) {
	user, err := s.FindUserByID(ctx, id)
	if err != nil {
		return nil, err
	}

	s.mu.Lock()
	deleted := s.deleted[user.ID]
	s.mu.Unlock()
	if deleted {
		return nil, identity.ErrUserNotFound
	}

	return user, nil
}

// CancelDeletion mirrors the identity service's cancellation: it clears the pending
// deletion and is a no-op for a live account.
func (s *fakeUserStore) CancelDeletion(_ context.Context, userID string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	user, ok := s.byID[userID]
	if !ok {
		return identity.ErrUserNotFound
	}

	user.DeletedAt = nil
	user.DeletionScheduledAt = nil
	delete(s.deleted, userID)

	return nil
}

// MarkDeleted mirrors the identity store's COALESCE on the timestamps.
func (s *fakeUserStore) MarkDeleted(_ context.Context, userID string) (*identity.User, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	user, ok := s.byID[userID]
	if !ok {
		return nil, identity.ErrUserNotFound
	}

	if user.DeletedAt == nil {
		deletedAt := time.Now().UTC()
		scheduled := deletedAt.Add(identity.DeletionRecoveryWindow)
		user.DeletedAt = &deletedAt
		user.DeletionScheduledAt = &scheduled
	}

	return user, nil
}

// ClearDeletion mirrors the identity store: a no-op success for a live account.
func (s *fakeUserStore) ClearDeletion(_ context.Context, userID string) (*identity.User, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	user, ok := s.byID[userID]
	if !ok {
		return nil, identity.ErrUserNotFound
	}

	user.DeletedAt = nil
	user.DeletionScheduledAt = nil
	delete(s.deleted, userID)

	return user, nil
}

// PurgeDueUsers mirrors the anonymising purge, including its idempotency guard.
func (s *fakeUserStore) PurgeDueUsers(_ context.Context, now time.Time) (int, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	purged := 0
	for id, user := range s.byID {
		if user.DeletedAt == nil || user.PurgedAt != nil || user.DeletionScheduledAt == nil {
			continue
		}
		if now.Before(*user.DeletionScheduledAt) {
			continue
		}

		delete(s.byEmail, identity.NormalizeEmail(user.Email))
		user.Email = "purged+" + id + "@deleted.invalid"
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

func (s *fakeUserStore) FindUsersByID(_ context.Context, ids []string) ([]*identity.User, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	found := make([]*identity.User, 0, len(ids))
	for _, id := range ids {
		if user, ok := s.byID[id]; ok {
			found = append(found, user)
		}
	}

	return found, nil
}

func (s *fakeUserStore) UpdateAvatarURL(_ context.Context, userID, avatarURL string) (*identity.User, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	user, ok := s.byID[userID]
	if !ok {
		return nil, identity.ErrUserNotFound
	}
	user.AvatarURL = avatarURL

	return user, nil
}

func (s *fakeUserStore) MarkEmailVerified(_ context.Context, userID string) (*identity.User, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	user, ok := s.byID[userID]
	if !ok || s.deleted[userID] {
		return nil, identity.ErrUserNotFound
	}
	if user.EmailVerifiedAt == nil {
		verifiedAt := time.Now().UTC()
		user.EmailVerifiedAt = &verifiedAt
	}

	return user, nil
}

func (s *fakeUserStore) UpdatePasswordHash(_ context.Context, userID, passwordHash string) (*identity.User, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	user, ok := s.byID[userID]
	if !ok || s.deleted[userID] {
		return nil, identity.ErrUserNotFound
	}
	user.PasswordHash = passwordHash

	return user, nil
}

// liveSessions returns the user's sessions the way the SQL does: live only,
// newest first.
func (s *fakeUserStore) markVerified(t *testing.T, userID string) {
	t.Helper()

	if _, err := s.MarkEmailVerified(context.Background(), userID); err != nil {
		t.Fatalf("MarkEmailVerified() error = %v, want nil", err)
	}
}

// fakeSessionStore is an in-memory SessionStore with the same semantics as the
// SQL: a conditional rotation, revoke that keeps the first timestamp, and a live
// list ordered newest first.
type fakeSessionStore struct {
	mu      sync.Mutex
	byID    map[string]*Session
	seq     int
	revoked []string
}

func newFakeSessionStore() *fakeSessionStore {
	return &fakeSessionStore{byID: map[string]*Session{}}
}

func (s *fakeSessionStore) Create(_ context.Context, session *Session) (*Session, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	if session == nil {
		return nil, errors.New("create: nil session")
	}

	stored := *session
	if stored.ID == "" {
		s.seq++
		stored.ID = fmt.Sprintf("22222222-2222-4222-8222-%012d", s.seq)
	}
	stored.LastUsedAt = time.Now().UTC()
	stored.CreatedAt = time.Now().UTC().Add(time.Duration(len(s.byID)) * time.Millisecond)
	if stored.DeviceInfo == nil {
		stored.DeviceInfo = map[string]any{}
	}

	s.byID[stored.ID] = &stored

	return &stored, nil
}

func (s *fakeSessionStore) FindByRefreshTokenHash(_ context.Context, hash string) (*Session, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	for _, session := range s.byID {
		if session.RefreshTokenHash == hash {
			return session, nil
		}
	}

	return nil, ErrSessionNotFound
}

func (s *fakeSessionStore) FindByID(_ context.Context, sessionID string) (*Session, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	session, ok := s.byID[sessionID]
	if !ok {
		return nil, ErrSessionNotFound
	}

	return session, nil
}

func (s *fakeSessionStore) Revoke(_ context.Context, sessionID string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	session, ok := s.byID[sessionID]
	if !ok {
		return ErrSessionNotFound
	}
	if session.RevokedAt == nil {
		revokedAt := time.Now().UTC()
		session.RevokedAt = &revokedAt
		s.revoked = append(s.revoked, sessionID)
	}

	return nil
}

func (s *fakeSessionStore) ListForUser(_ context.Context, userID string) ([]*Session, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	live := make([]*Session, 0, len(s.byID))
	for _, session := range s.byID {
		if session.UserID == userID && session.Active() {
			live = append(live, session)
		}
	}

	sort.Slice(live, func(i, j int) bool { return live[i].CreatedAt.After(live[j].CreatedAt) })

	return live, nil
}

func (s *fakeSessionStore) RevokeAllForUser(_ context.Context, userID string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	for _, session := range s.byID {
		if session.UserID == userID && session.RevokedAt == nil {
			revokedAt := time.Now().UTC()
			session.RevokedAt = &revokedAt
			s.revoked = append(s.revoked, session.ID)
		}
	}

	return nil
}

func (s *fakeSessionStore) RotateRefreshToken(_ context.Context, sessionID, expectedHash, newHash string) (*Session, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	session, ok := s.byID[sessionID]
	if !ok {
		return nil, ErrSessionNotFound
	}
	if !session.Active() {
		return nil, ErrSessionNotFound
	}
	if session.RefreshTokenHash != expectedHash {
		return nil, ErrSessionRefreshTokenMismatch
	}

	session.RefreshTokenHash = newHash
	session.LastUsedAt = time.Now().UTC()

	return session, nil
}

func (s *fakeSessionStore) countActive(userID string) int {
	s.mu.Lock()
	defer s.mu.Unlock()

	count := 0
	for _, session := range s.byID {
		if session.UserID == userID && session.Active() {
			count++
		}
	}

	return count
}

// fakeCredentialStore is an in-memory CredentialStore.
type fakeCredentialStore struct {
	mu   sync.Mutex
	byID map[string]*Credential
	seq  int
}

func newFakeCredentialStore() *fakeCredentialStore {
	return &fakeCredentialStore{byID: map[string]*Credential{}}
}

func (s *fakeCredentialStore) Create(_ context.Context, credential *Credential) (*Credential, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	for _, existing := range s.byID {
		if existing.UserID == credential.UserID && existing.CredentialType == credential.CredentialType {
			return nil, ErrCredentialExists
		}
	}

	s.seq++
	stored := *credential
	stored.ID = fmt.Sprintf("33333333-3333-4333-8333-%012d", s.seq)
	s.byID[stored.ID] = &stored

	return &stored, nil
}

func (s *fakeCredentialStore) FindByUserIDAndType(_ context.Context, userID, credentialType string) (*Credential, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	for _, credential := range s.byID {
		if credential.UserID == userID && credential.CredentialType == credentialType {
			return credential, nil
		}
	}

	return nil, ErrCredentialNotFound
}

func (s *fakeCredentialStore) Update(_ context.Context, credential *Credential) (*Credential, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	stored, ok := s.byID[credential.ID]
	if !ok {
		return nil, ErrCredentialNotFound
	}
	stored.CredentialData = credential.CredentialData

	return stored, nil
}

// fakeVerificationStore is an in-memory VerificationTokenStore.
type fakeVerificationStore struct {
	mu     sync.Mutex
	byHash map[string]*VerificationToken
	seq    int
	// createErr, when set, is returned by Create. It exists so a test can prove a
	// store failure is reported rather than swallowed.
	createErr error
}

func newFakeVerificationStore() *fakeVerificationStore {
	return &fakeVerificationStore{byHash: map[string]*VerificationToken{}}
}

func (s *fakeVerificationStore) Create(_ context.Context, token *VerificationToken) (*VerificationToken, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.createErr != nil {
		return nil, s.createErr
	}

	s.seq++
	stored := *token
	stored.ID = fmt.Sprintf("44444444-4444-4444-8444-%012d", s.seq)
	stored.CreatedAt = time.Now().UTC()
	s.byHash[stored.TokenHash] = &stored

	return &stored, nil
}

func (s *fakeVerificationStore) FindByTokenHash(_ context.Context, hash string) (*VerificationToken, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	token, ok := s.byHash[hash]
	if !ok {
		return nil, ErrVerificationTokenNotFound
	}

	return token, nil
}

func (s *fakeVerificationStore) Consume(_ context.Context, tokenID string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	for _, token := range s.byHash {
		if token.ID != tokenID {
			continue
		}
		if token.ConsumedAt != nil {
			return ErrVerificationTokenConsumed
		}
		consumedAt := time.Now().UTC()
		token.ConsumedAt = &consumedAt

		return nil
	}

	return ErrVerificationTokenNotFound
}

// testWebURL is the origin the tests expect in every emailed link. It is a
// documentation address, so nothing in the suite could ever resolve it.
const testWebURL = "https://knot.example"

// fakeEmailSender records the messages the service hands it, and can be told to
// fail so the best-effort contract can be tested.
type fakeEmailSender struct {
	mu   sync.Mutex
	sent []email.Email
	err  error
}

func (s *fakeEmailSender) Send(_ context.Context, msg email.Email) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.err != nil {
		return s.err
	}

	s.sent = append(s.sent, msg)

	return nil
}

// messages returns a copy of everything sent, so a test cannot mutate the record.
func (s *fakeEmailSender) messages() []email.Email {
	s.mu.Lock()
	defer s.mu.Unlock()

	out := make([]email.Email, len(s.sent))
	copy(out, s.sent)

	return out
}

// testHarness bundles everything a service test needs.
type testHarness struct {
	service       *Service
	users         *fakeUserStore
	sessions      *fakeSessionStore
	credentials   *fakeCredentialStore
	verifications *fakeVerificationStore
	emails        *fakeEmailSender
	cache         cache.Cache
	redis         *miniredis.Miniredis
	logs          *bytes.Buffer
}

func newTestHarness(t *testing.T, opts ...Option) *testHarness {
	t.Helper()

	users := newFakeUserStore()
	sessions := newFakeSessionStore()
	credentials := newFakeCredentialStore()
	verifications := newFakeVerificationStore()
	emails := &fakeEmailSender{}

	redis := miniredis.RunT(t)
	redisCache, err := cache.NewRedisCache(redis.Addr(), "", 0)
	if err != nil {
		t.Fatalf("NewRedisCache() error = %v, want nil", err)
	}
	t.Cleanup(func() { _ = redisCache.Close() })

	logs := &bytes.Buffer{}
	logger := slog.New(slog.NewTextHandler(logs, &slog.HandlerOptions{Level: slog.LevelDebug}))

	issuer, err := identity.NewTokenIssuer("test-secret-that-is-at-least-32-bytes-long")
	if err != nil {
		t.Fatalf("NewTokenIssuer() error = %v, want nil", err)
	}

	service, err := NewService(Deps{
		Users:         users,
		Sessions:      sessions,
		Credentials:   credentials,
		Verifications: verifications,
		Deletions:     users,
		Tokens:        issuer,
		Cache:         redisCache,
		Email:         emails,
		WebURL:        testWebURL,
		Logger:        logger,
	}, opts...)
	if err != nil {
		t.Fatalf("NewService() error = %v, want nil", err)
	}

	return &testHarness{
		service:       service,
		users:         users,
		sessions:      sessions,
		credentials:   credentials,
		verifications: verifications,
		emails:        emails,
		cache:         redisCache,
		redis:         redis,
		logs:          logs,
	}
}

// registerAndVerify creates an account and proves its address, returning it.
func (h *testHarness) registerAndVerify(t *testing.T) *identity.User {
	t.Helper()

	user, err := h.service.Register(context.Background(), RegisterInput{
		Email:         "ada@example.com",
		Password:      "correct horse 7",
		DisplayName:   "Ada Lovelace",
		AgreedToTerms: true,
	})
	if err != nil {
		t.Fatalf("Register() error = %v, want nil", err)
	}

	token := h.latestEmailToken(t, defaultVerificationLinkBase)
	if err := h.service.VerifyEmail(context.Background(), token); err != nil {
		t.Fatalf("VerifyEmail() error = %v, want nil", err)
	}

	return user
}

// latestEmail returns the most recently sent message.
func (h *testHarness) latestEmail(t *testing.T) email.Email {
	t.Helper()

	messages := h.emails.messages()
	if len(messages) == 0 {
		t.Fatal("no email was sent")
	}

	return messages[len(messages)-1]
}

// latestEmailToken pulls the token out of the most recent email body.
func (h *testHarness) latestEmailToken(t *testing.T, base string) string {
	t.Helper()

	return extractToken(t, h.latestEmail(t).Text, base)
}

// extractToken pulls the token out of a link that starts with base.
func extractToken(t *testing.T, body, base string) string {
	t.Helper()

	index := strings.LastIndex(body, base)
	if index < 0 {
		t.Fatalf("no link starting with %q was found; body:\n%s", base, body)
	}

	rest := body[index+len(base):]
	end := strings.IndexAny(rest, " \n\"")
	if end < 0 {
		end = len(rest)
	}

	return rest[:end]
}

func TestNewServiceRejectsMissingDependencies(t *testing.T) {
	t.Parallel()

	logger := slog.New(slog.NewTextHandler(&bytes.Buffer{}, nil))
	redis := miniredis.RunT(t)
	redisCache, err := cache.NewRedisCache(redis.Addr(), "", 0)
	if err != nil {
		t.Fatalf("NewRedisCache() error = %v, want nil", err)
	}
	t.Cleanup(func() { _ = redisCache.Close() })

	complete := Deps{
		Users:         newFakeUserStore(),
		Sessions:      newFakeSessionStore(),
		Credentials:   newFakeCredentialStore(),
		Verifications: newFakeVerificationStore(),
		Deletions:     newFakeUserStore(),
		Tokens:        &fakeIssuer{},
		Cache:         redisCache,
		Email:         &fakeEmailSender{},
		WebURL:        testWebURL,
		Logger:        logger,
	}

	if _, err := NewService(complete); err != nil {
		t.Fatalf("NewService(complete) error = %v, want nil", err)
	}

	tests := map[string]func(*Deps){
		"users":         func(d *Deps) { d.Users = nil },
		"sessions":      func(d *Deps) { d.Sessions = nil },
		"credentials":   func(d *Deps) { d.Credentials = nil },
		"verifications": func(d *Deps) { d.Verifications = nil },
		"deletions":     func(d *Deps) { d.Deletions = nil },
		"tokens":        func(d *Deps) { d.Tokens = nil },
		"cache":         func(d *Deps) { d.Cache = nil },
		"email":         func(d *Deps) { d.Email = nil },
		"web_url":       func(d *Deps) { d.WebURL = "" },
		"logger":        func(d *Deps) { d.Logger = nil },
	}

	for name, breakDep := range tests {
		t.Run(name, func(t *testing.T) {
			deps := complete
			breakDep(&deps)

			if _, err := NewService(deps); err == nil {
				t.Fatalf("NewService() with a nil %s error = nil, want an error", name)
			}
		})
	}
}

// fakeIssuer is a TokenIssuer that never touches a key.
type fakeIssuer struct{}

func (f *fakeIssuer) IssueAccessTokenForSession(userID, sessionID string) (string, error) {
	return "access:" + userID + ":" + sessionID, nil
}

func (f *fakeIssuer) ParseAccessToken(string) (*identity.Claims, error) {
	return nil, identity.ErrInvalidToken
}

func TestRegisterCreatesAnUnverifiedAccount(t *testing.T) {
	t.Parallel()

	h := newTestHarness(t)

	user, err := h.service.Register(context.Background(), RegisterInput{
		Email:              "Ada@Example.COM",
		Password:           "correct horse 7",
		DisplayName:        "Ada Lovelace",
		PreferredLanguages: []string{"eng"},
		AgreedToTerms:      true,
	})
	if err != nil {
		t.Fatalf("Register() error = %v, want nil", err)
	}

	if user.Email != "ada@example.com" {
		t.Errorf("email = %q, want the normalised address", user.Email)
	}
	if user.EmailVerifiedAt != nil {
		t.Errorf("email_verified_at = %v, want nil", user.EmailVerifiedAt)
	}
	if ok, _ := identity.VerifyPassword(user.PasswordHash, "correct horse 7"); !ok {
		t.Error("stored hash does not verify the password")
	}

	// The password credential is mirrored into `credentials` (KNOT-ADR-079).
	credential, err := h.credentials.FindByUserIDAndType(context.Background(), user.ID, CredentialTypePassword)
	if err != nil {
		t.Fatalf("FindByUserIDAndType() error = %v, want nil", err)
	}
	if credential.CredentialData != user.PasswordHash {
		t.Error("credential data does not match the stored password hash")
	}

	// The verification link is emailed, because there is no console stand-in any
	// more (KNOT-ADR-090).
	msg := h.latestEmail(t)
	if msg.To != user.Email {
		t.Errorf("email to = %q, want %q", msg.To, user.Email)
	}
	if !strings.Contains(msg.Text, defaultVerificationLinkBase) {
		t.Errorf("the verification email has no deep link; body:\n%s", msg.Text)
	}
	if !strings.Contains(msg.Text, testWebURL+webVerificationPath) {
		t.Errorf("the verification email has no web link; body:\n%s", msg.Text)
	}
}

func TestRegisterRejectsWeakPasswords(t *testing.T) {
	t.Parallel()

	h := newTestHarness(t)

	for _, password := range []string{"short1", "nodigitsatall", "12345678"} {
		_, err := h.service.Register(context.Background(), RegisterInput{
			Email:         "ada@example.com",
			Password:      password,
			DisplayName:   "Ada Lovelace",
			AgreedToTerms: true,
		})

		var validation *identity.ValidationError
		if !errors.As(err, &validation) || validation.Field != "password" {
			t.Fatalf("Register(%q) error = %v, want a password ValidationError", password, err)
		}
	}
}

func TestRegisterRejectsDuplicateEmail(t *testing.T) {
	t.Parallel()

	h := newTestHarness(t)

	if _, err := h.service.Register(context.Background(), RegisterInput{
		Email:         "ada@example.com",
		Password:      "correct horse 7",
		DisplayName:   "Ada",
		AgreedToTerms: true,
	}); err != nil {
		t.Fatalf("Register() error = %v, want nil", err)
	}

	_, err := h.service.Register(context.Background(), RegisterInput{
		Email:         "ada@example.com",
		Password:      "correct horse 7",
		DisplayName:   "Ada",
		AgreedToTerms: true,
	})
	if !errors.Is(err, identity.ErrEmailTaken) {
		t.Fatalf("second Register() error = %v, want ErrEmailTaken", err)
	}
}

// TestRegisterRequiresConsent proves the Terms of Service and Privacy Policy
// checkbox is enforced by the service and not only by the form: a caller that
// sends everything else correctly is refused, and nothing is created, stored or
// emailed (KNOT-035).
func TestRegisterRequiresConsent(t *testing.T) {
	t.Parallel()

	h := newTestHarness(t)

	_, err := h.service.Register(context.Background(), RegisterInput{
		Email:       "ada@example.com",
		Password:    "correct horse 7",
		DisplayName: "Ada Lovelace",
	})
	if !errors.Is(err, ErrTermsNotAccepted) {
		t.Fatalf("Register(without consent) error = %v, want ErrTermsNotAccepted", err)
	}

	// Consent is the first check, so nothing after it ran: there is no account, no
	// verification token and no message.
	if _, findErr := h.users.FindUserByEmail(context.Background(), "ada@example.com"); !errors.Is(findErr, identity.ErrUserNotFound) {
		t.Errorf("FindUserByEmail() error = %v, want ErrUserNotFound", findErr)
	}
	if got := len(h.emails.messages()); got != 0 {
		t.Errorf("emails sent = %d, want 0", got)
	}
	if got := len(h.verifications.byHash); got != 0 {
		t.Errorf("verification tokens stored = %d, want 0", got)
	}
}

func TestVerifyEmail(t *testing.T) {
	t.Parallel()

	h := newTestHarness(t)

	user, err := h.service.Register(context.Background(), RegisterInput{
		Email:         "ada@example.com",
		Password:      "correct horse 7",
		DisplayName:   "Ada",
		AgreedToTerms: true,
	})
	if err != nil {
		t.Fatalf("Register() error = %v, want nil", err)
	}

	token := h.latestEmailToken(t, defaultVerificationLinkBase)

	if err := h.service.VerifyEmail(context.Background(), token); err != nil {
		t.Fatalf("VerifyEmail() error = %v, want nil", err)
	}

	verified, _ := h.users.FindUserByID(context.Background(), user.ID)
	if verified.EmailVerifiedAt == nil {
		t.Fatal("email_verified_at is still nil after verification")
	}

	// A verification link is single use.
	if err := h.service.VerifyEmail(context.Background(), token); !errors.Is(err, ErrInvalidVerificationToken) {
		t.Fatalf("second VerifyEmail() error = %v, want ErrInvalidVerificationToken", err)
	}

	if err := h.service.VerifyEmail(context.Background(), "not-a-token"); !errors.Is(err, ErrInvalidVerificationToken) {
		t.Fatalf("VerifyEmail(unknown) error = %v, want ErrInvalidVerificationToken", err)
	}
}

func TestVerifyEmailRejectsAnExpiredToken(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, 10, 26, 12, 0, 0, 0, time.UTC)
	h := newTestHarness(t, WithClock(func() time.Time { return now }))

	if _, err := h.service.Register(context.Background(), RegisterInput{
		Email:         "ada@example.com",
		Password:      "correct horse 7",
		DisplayName:   "Ada",
		AgreedToTerms: true,
	}); err != nil {
		t.Fatalf("Register() error = %v, want nil", err)
	}

	token := h.latestEmailToken(t, defaultVerificationLinkBase)

	// Move the service's clock past the token's expiry.
	h.service.now = func() time.Time { return now.Add(EmailVerificationTTL + time.Minute) }

	if err := h.service.VerifyEmail(context.Background(), token); !errors.Is(err, ErrInvalidVerificationToken) {
		t.Fatalf("VerifyEmail(expired) error = %v, want ErrInvalidVerificationToken", err)
	}
}

func TestLoginRefusesAnUnverifiedAccount(t *testing.T) {
	t.Parallel()

	h := newTestHarness(t)

	if _, err := h.service.Register(context.Background(), RegisterInput{
		Email:         "ada@example.com",
		Password:      "correct horse 7",
		DisplayName:   "Ada",
		AgreedToTerms: true,
	}); err != nil {
		t.Fatalf("Register() error = %v, want nil", err)
	}

	_, err := h.service.Login(context.Background(), LoginInput{
		Email: "ada@example.com", Password: "correct horse 7", IPAddress: "203.0.113.7",
	})
	if !errors.Is(err, identity.ErrEmailNotVerified) {
		t.Fatalf("Login() error = %v, want ErrEmailNotVerified", err)
	}
}

func TestLoginIssuesASession(t *testing.T) {
	t.Parallel()

	h := newTestHarness(t)
	h.registerAndVerify(t)

	out, err := h.service.Login(context.Background(), LoginInput{
		Email:      "ada@example.com",
		Password:   "correct horse 7",
		DeviceInfo: map[string]any{"platform": "ios"},
		IPAddress:  "203.0.113.7",
	})
	if err != nil {
		t.Fatalf("Login() error = %v, want nil", err)
	}

	if out.AccessToken == "" || out.RefreshToken == "" {
		t.Fatal("Login() returned an empty token")
	}
	if out.ExpiresIn != int(identity.AccessTokenTTL.Seconds()) {
		t.Errorf("expires_in = %d, want %d", out.ExpiresIn, int(identity.AccessTokenTTL.Seconds()))
	}

	session, err := h.sessions.FindByID(context.Background(), out.SessionID)
	if err != nil {
		t.Fatalf("FindByID() error = %v, want nil", err)
	}
	if session.IPAddress != "203.0.113.7" {
		t.Errorf("ip_address = %q, want %q", session.IPAddress, "203.0.113.7")
	}
	if session.DeviceInfo["platform"] != "ios" {
		t.Errorf("device_info = %v, want platform ios", session.DeviceInfo)
	}

	// The stored hash is of the secret, never the token itself.
	sessionID, ok := SplitRefreshToken(out.RefreshToken)
	if !ok || sessionID != out.SessionID {
		t.Fatalf("refresh token does not name its session: %q", out.RefreshToken)
	}
	if strings.Contains(session.RefreshTokenHash, ".") {
		t.Error("the stored refresh token hash looks like a token, not a digest")
	}
}

func TestLoginRejectsBadCredentials(t *testing.T) {
	t.Parallel()

	h := newTestHarness(t)
	h.registerAndVerify(t)

	tests := map[string]LoginInput{
		"wrong password": {Email: "ada@example.com", Password: "wrong horse 7"},
		"unknown email":  {Email: "nobody@example.com", Password: "correct horse 7"},
		"empty password": {Email: "ada@example.com", Password: ""},
	}

	for name, in := range tests {
		t.Run(name, func(t *testing.T) {
			if _, err := h.service.Login(context.Background(), in); !errors.Is(err, identity.ErrInvalidCredentials) {
				t.Fatalf("Login() error = %v, want ErrInvalidCredentials", err)
			}
		})
	}
}

// TestLoginRecoversAnAccountInsideTheWindow is the founder's automatic recovery: a
// successful sign-in during the 30-day window is the clearest possible signal that
// the person wants the account back (KNOT-ADR-081).
func TestLoginRecoversAnAccountInsideTheWindow(t *testing.T) {
	t.Parallel()

	h := newTestHarness(t)
	user := h.registerAndVerify(t)

	ctx := context.Background()

	// Pending deletion, window still open.
	h.users.mu.Lock()
	deletedAt := time.Now().UTC().Add(-time.Hour)
	scheduled := time.Now().UTC().Add(identity.DeletionRecoveryWindow - time.Hour)
	h.users.byID[user.ID].DeletedAt = &deletedAt
	h.users.byID[user.ID].DeletionScheduledAt = &scheduled
	h.users.deleted[user.ID] = true
	h.users.mu.Unlock()

	out, err := h.service.Login(ctx, LoginInput{
		Email: "ada@example.com", Password: "correct horse 7",
	})
	if err != nil {
		t.Fatalf("Login() error = %v, want nil (the account should be recovered)", err)
	}
	if !out.Recovered {
		t.Error("Recovered = false, want true for a sign-in that cancelled a deletion")
	}
	if out.AccessToken == "" || out.RefreshToken == "" {
		t.Error("Login() issued no tokens for a recovered account")
	}

	// The deletion was cancelled before the tokens were minted, so the account is live.
	stored, findErr := h.users.FindUserByID(ctx, user.ID)
	if findErr != nil {
		t.Fatalf("FindUserByID() error = %v, want nil", findErr)
	}
	if stored.DeletedAt != nil || stored.DeletionScheduledAt != nil {
		t.Errorf("deletion state = (%v, %v), want both cleared", stored.DeletedAt, stored.DeletionScheduledAt)
	}

	// A second sign-in is an ordinary one, not another recovery.
	again, err := h.service.Login(ctx, LoginInput{
		Email: "ada@example.com", Password: "correct horse 7",
	})
	if err != nil {
		t.Fatalf("second Login() error = %v, want nil", err)
	}
	if again.Recovered {
		t.Error("the second sign-in reported a recovery, want an ordinary sign-in")
	}
}

// TestLoginRefusesAnAccountPastItsWindow proves the recovery window is a deadline and
// not a suggestion: past it, the account is gone even though the purge has not run.
func TestLoginRefusesAnAccountPastItsWindow(t *testing.T) {
	t.Parallel()

	h := newTestHarness(t)
	user := h.registerAndVerify(t)

	h.users.mu.Lock()
	deletedAt := time.Now().UTC().Add(-31 * 24 * time.Hour)
	scheduled := time.Now().UTC().Add(-24 * time.Hour)
	h.users.byID[user.ID].DeletedAt = &deletedAt
	h.users.byID[user.ID].DeletionScheduledAt = &scheduled
	h.users.mu.Unlock()

	_, err := h.service.Login(context.Background(), LoginInput{
		Email: "ada@example.com", Password: "correct horse 7",
	})
	if !errors.Is(err, identity.ErrAccountPendingPurge) {
		t.Fatalf("Login() error = %v, want ErrAccountPendingPurge", err)
	}
}

// TestLoginRefusesADeletedAccountWithNoWindow covers the edge case a hand-edited row
// can produce: deleted, but with no window to recover into.
func TestLoginRefusesADeletedAccountWithNoWindow(t *testing.T) {
	t.Parallel()

	h := newTestHarness(t)
	user := h.registerAndVerify(t)

	h.users.mu.Lock()
	deletedAt := time.Now().UTC()
	h.users.byID[user.ID].DeletedAt = &deletedAt
	h.users.byID[user.ID].DeletionScheduledAt = nil
	h.users.mu.Unlock()

	_, err := h.service.Login(context.Background(), LoginInput{
		Email: "ada@example.com", Password: "correct horse 7",
	})
	if !errors.Is(err, identity.ErrAccountDeleted) {
		t.Fatalf("Login() error = %v, want ErrAccountDeleted", err)
	}
}

// TestLoginRefusesAPurgedAccount proves an anonymised account cannot sign in even if
// its (rewritten) address is presented.
func TestLoginRefusesAPurgedAccount(t *testing.T) {
	t.Parallel()

	h := newTestHarness(t)
	user := h.registerAndVerify(t)

	ctx := context.Background()

	h.users.mu.Lock()
	deletedAt := time.Now().UTC().Add(-40 * 24 * time.Hour)
	scheduled := time.Now().UTC().Add(-10 * 24 * time.Hour)
	purgedAt := time.Now().UTC().Add(-9 * 24 * time.Hour)
	h.users.byID[user.ID].DeletedAt = &deletedAt
	h.users.byID[user.ID].DeletionScheduledAt = &scheduled
	h.users.byID[user.ID].PurgedAt = &purgedAt
	h.users.mu.Unlock()

	_, err := h.service.Login(ctx, LoginInput{
		Email: "ada@example.com", Password: "correct horse 7",
	})
	// A purged row is refused before its password is even considered: there is no
	// person left to authenticate, and the account cannot be recovered.
	if !errors.Is(err, identity.ErrAccountPendingPurge) {
		t.Fatalf("Login() error = %v, want ErrAccountPendingPurge", err)
	}
}

func TestLoginEnforcesTheSessionCap(t *testing.T) {
	t.Parallel()

	h := newTestHarness(t)
	user := h.registerAndVerify(t)

	ctx := context.Background()
	issued := make([]string, 0, MaxActiveSessions+1)

	for i := 0; i < MaxActiveSessions+1; i++ {
		out, err := h.service.Login(ctx, LoginInput{
			Email: "ada@example.com", Password: "correct horse 7",
		})
		if err != nil {
			t.Fatalf("Login() #%d error = %v, want nil", i+1, err)
		}
		issued = append(issued, out.SessionID)
	}

	if got := h.sessions.countActive(user.ID); got != MaxActiveSessions {
		t.Fatalf("active sessions = %d, want %d", got, MaxActiveSessions)
	}

	// The oldest session is the one that was ended, and the newest survives.
	oldest, err := h.sessions.FindByID(ctx, issued[0])
	if err != nil {
		t.Fatalf("FindByID(oldest) error = %v, want nil", err)
	}
	if oldest.Active() {
		t.Error("the oldest session is still active after the cap was exceeded")
	}

	newest, err := h.sessions.FindByID(ctx, issued[len(issued)-1])
	if err != nil {
		t.Fatalf("FindByID(newest) error = %v, want nil", err)
	}
	if !newest.Active() {
		t.Error("the newest session was revoked by the cap")
	}
}

func TestRefreshRotatesTheToken(t *testing.T) {
	t.Parallel()

	h := newTestHarness(t)
	h.registerAndVerify(t)

	ctx := context.Background()

	first, err := h.service.Login(ctx, LoginInput{Email: "ada@example.com", Password: "correct horse 7"})
	if err != nil {
		t.Fatalf("Login() error = %v, want nil", err)
	}

	second, err := h.service.Refresh(ctx, first.RefreshToken)
	if err != nil {
		t.Fatalf("Refresh() error = %v, want nil", err)
	}

	if second.RefreshToken == first.RefreshToken {
		t.Fatal("Refresh() returned the same refresh token")
	}
	if second.SessionID != first.SessionID {
		t.Errorf("session id = %q, want the same session %q", second.SessionID, first.SessionID)
	}

	// The new token works and the old one is now reuse.
	if _, err := h.service.Refresh(ctx, second.RefreshToken); err != nil {
		t.Fatalf("Refresh() with the new token error = %v, want nil", err)
	}
}

func TestRefreshDetectsReuseAndRevokesEverySession(t *testing.T) {
	t.Parallel()

	h := newTestHarness(t)
	user := h.registerAndVerify(t)

	ctx := context.Background()

	// Two devices, so "every session" is more than one.
	first, err := h.service.Login(ctx, LoginInput{Email: "ada@example.com", Password: "correct horse 7"})
	if err != nil {
		t.Fatalf("Login() error = %v, want nil", err)
	}
	if _, err := h.service.Login(ctx, LoginInput{Email: "ada@example.com", Password: "correct horse 7"}); err != nil {
		t.Fatalf("second Login() error = %v, want nil", err)
	}

	if _, err := h.service.Refresh(ctx, first.RefreshToken); err != nil {
		t.Fatalf("Refresh() error = %v, want nil", err)
	}

	// Replaying the rotated token is reuse.
	if _, err := h.service.Refresh(ctx, first.RefreshToken); !errors.Is(err, ErrRefreshTokenReused) {
		t.Fatalf("replayed Refresh() error = %v, want ErrRefreshTokenReused", err)
	}

	if got := h.sessions.countActive(user.ID); got != 0 {
		t.Fatalf("active sessions after reuse = %d, want 0", got)
	}

	if !strings.Contains(h.logs.String(), "reuse detected") {
		t.Errorf("reuse was not logged; logs:\n%s", h.logs.String())
	}
}

func TestRefreshRejectsUnknownAndRevokedSessions(t *testing.T) {
	t.Parallel()

	h := newTestHarness(t)
	h.registerAndVerify(t)

	ctx := context.Background()

	out, err := h.service.Login(ctx, LoginInput{Email: "ada@example.com", Password: "correct horse 7"})
	if err != nil {
		t.Fatalf("Login() error = %v, want nil", err)
	}

	if _, err := h.service.Refresh(ctx, "not-a-token"); !errors.Is(err, ErrInvalidRefreshToken) {
		t.Errorf("Refresh(garbage) error = %v, want ErrInvalidRefreshToken", err)
	}
	if _, err := h.service.Refresh(ctx, "00000000-0000-4000-8000-000000000000.secret"); !errors.Is(err, ErrInvalidRefreshToken) {
		t.Errorf("Refresh(unknown session) error = %v, want ErrInvalidRefreshToken", err)
	}

	if err := h.service.Logout(ctx, out.User.ID, out.SessionID); err != nil {
		t.Fatalf("Logout() error = %v, want nil", err)
	}

	if _, err := h.service.Refresh(ctx, out.RefreshToken); !errors.Is(err, ErrInvalidRefreshToken) {
		t.Errorf("Refresh(revoked session) error = %v, want ErrInvalidRefreshToken", err)
	}
}

func TestLogoutIsScopedAndIdempotent(t *testing.T) {
	t.Parallel()

	h := newTestHarness(t)
	h.registerAndVerify(t)

	ctx := context.Background()

	first, err := h.service.Login(ctx, LoginInput{Email: "ada@example.com", Password: "correct horse 7"})
	if err != nil {
		t.Fatalf("Login() error = %v, want nil", err)
	}
	second, err := h.service.Login(ctx, LoginInput{Email: "ada@example.com", Password: "correct horse 7"})
	if err != nil {
		t.Fatalf("second Login() error = %v, want nil", err)
	}

	if err := h.service.Logout(ctx, first.User.ID, first.SessionID); err != nil {
		t.Fatalf("Logout() error = %v, want nil", err)
	}

	session, _ := h.sessions.FindByID(ctx, first.SessionID)
	if session.Active() {
		t.Error("the session is still active after Logout()")
	}

	// Signing out again is not an error: the intent is already satisfied.
	if err := h.service.Logout(ctx, first.User.ID, first.SessionID); err != nil {
		t.Fatalf("second Logout() error = %v, want nil", err)
	}

	// Another user cannot end this session, and is not told that it exists.
	if err := h.service.Logout(ctx, "99999999-9999-4999-8999-999999999999", second.SessionID); !errors.Is(err, ErrSessionNotFound) {
		t.Fatalf("Logout(other user) error = %v, want ErrSessionNotFound", err)
	}
}

func TestListAndRevokeSessions(t *testing.T) {
	t.Parallel()

	h := newTestHarness(t)
	h.registerAndVerify(t)

	ctx := context.Background()

	first, err := h.service.Login(ctx, LoginInput{Email: "ada@example.com", Password: "correct horse 7"})
	if err != nil {
		t.Fatalf("Login() error = %v, want nil", err)
	}
	if _, err := h.service.Login(ctx, LoginInput{Email: "ada@example.com", Password: "correct horse 7"}); err != nil {
		t.Fatalf("second Login() error = %v, want nil", err)
	}

	sessions, err := h.service.ListSessions(ctx, first.User.ID)
	if err != nil {
		t.Fatalf("ListSessions() error = %v, want nil", err)
	}
	if len(sessions) != 2 {
		t.Fatalf("ListSessions() = %d sessions, want 2", len(sessions))
	}

	if err := h.service.RevokeSession(ctx, first.User.ID, first.SessionID); err != nil {
		t.Fatalf("RevokeSession() error = %v, want nil", err)
	}

	sessions, err = h.service.ListSessions(ctx, first.User.ID)
	if err != nil {
		t.Fatalf("ListSessions() error = %v, want nil", err)
	}
	if len(sessions) != 1 {
		t.Fatalf("ListSessions() after revoke = %d sessions, want 1", len(sessions))
	}

	if err := h.service.RevokeSession(ctx, "99999999-9999-4999-8999-999999999999", first.SessionID); !errors.Is(err, ErrSessionNotFound) {
		t.Fatalf("RevokeSession(other user) error = %v, want ErrSessionNotFound", err)
	}
}

func TestForgotPasswordDoesNotRevealAccounts(t *testing.T) {
	t.Parallel()

	h := newTestHarness(t)
	h.registerAndVerify(t)

	ctx := context.Background()

	// An address that is not registered must look exactly like one that is.
	if err := h.service.ForgotPassword(ctx, "nobody@example.com"); err != nil {
		t.Fatalf("ForgotPassword(unknown) error = %v, want nil", err)
	}
	for _, msg := range h.emails.messages() {
		if msg.To == "nobody@example.com" {
			t.Error("a reset link was produced for an unregistered address")
		}
	}

	if err := h.service.ForgotPassword(ctx, "ada@example.com"); err != nil {
		t.Fatalf("ForgotPassword() error = %v, want nil", err)
	}
	if !strings.Contains(h.latestEmail(t).Text, defaultPasswordResetLinkBase) {
		t.Errorf("the reset email has no link; body:\n%s", h.latestEmail(t).Text)
	}

	// A malformed address is not an error either.
	if err := h.service.ForgotPassword(ctx, "not-an-email"); err != nil {
		t.Fatalf("ForgotPassword(malformed) error = %v, want nil", err)
	}
}

func TestResetPasswordChangesTheSecretAndSignsOutEverywhere(t *testing.T) {
	t.Parallel()

	h := newTestHarness(t)
	user := h.registerAndVerify(t)

	ctx := context.Background()

	out, err := h.service.Login(ctx, LoginInput{Email: "ada@example.com", Password: "correct horse 7"})
	if err != nil {
		t.Fatalf("Login() error = %v, want nil", err)
	}

	if err := h.service.ForgotPassword(ctx, "ada@example.com"); err != nil {
		t.Fatalf("ForgotPassword() error = %v, want nil", err)
	}
	token := h.latestEmailToken(t, defaultPasswordResetLinkBase)

	if err := h.service.ResetPassword(ctx, token, "a brand new 9"); err != nil {
		t.Fatalf("ResetPassword() error = %v, want nil", err)
	}

	// Every session is gone (KNOT-ADR-087).
	if got := h.sessions.countActive(user.ID); got != 0 {
		t.Fatalf("active sessions after reset = %d, want 0", got)
	}
	if _, err := h.service.Refresh(ctx, out.RefreshToken); !errors.Is(err, ErrInvalidRefreshToken) {
		t.Errorf("Refresh() after reset error = %v, want ErrInvalidRefreshToken", err)
	}

	// The new password works and the old one does not.
	if _, err := h.service.Login(ctx, LoginInput{Email: "ada@example.com", Password: "a brand new 9"}); err != nil {
		t.Fatalf("Login(new password) error = %v, want nil", err)
	}
	if _, err := h.service.Login(ctx, LoginInput{Email: "ada@example.com", Password: "correct horse 7"}); !errors.Is(err, identity.ErrInvalidCredentials) {
		t.Errorf("Login(old password) error = %v, want ErrInvalidCredentials", err)
	}

	// The reset token is single use.
	if err := h.service.ResetPassword(ctx, token, "another one 3"); !errors.Is(err, ErrInvalidPasswordResetToken) {
		t.Fatalf("second ResetPassword() error = %v, want ErrInvalidPasswordResetToken", err)
	}
}

func TestResetPasswordValidatesInput(t *testing.T) {
	t.Parallel()

	h := newTestHarness(t)

	var validation *identity.ValidationError
	if err := h.service.ResetPassword(context.Background(), "anything", "weak"); !errors.As(err, &validation) {
		t.Fatalf("ResetPassword(weak) error = %v, want a ValidationError", err)
	}

	if err := h.service.ResetPassword(context.Background(), "", "a strong one 1"); !errors.Is(err, ErrInvalidPasswordResetToken) {
		t.Fatalf("ResetPassword(no token) error = %v, want ErrInvalidPasswordResetToken", err)
	}

	if err := h.service.ResetPassword(context.Background(), "unknown-token", "a strong one 1"); !errors.Is(err, ErrInvalidPasswordResetToken) {
		t.Fatalf("ResetPassword(unknown token) error = %v, want ErrInvalidPasswordResetToken", err)
	}
}

func TestResetPasswordRejectsADeletedAccount(t *testing.T) {
	t.Parallel()

	h := newTestHarness(t)
	user := h.registerAndVerify(t)

	ctx := context.Background()

	if err := h.service.ForgotPassword(ctx, "ada@example.com"); err != nil {
		t.Fatalf("ForgotPassword() error = %v, want nil", err)
	}
	token := h.latestEmailToken(t, defaultPasswordResetLinkBase)

	h.users.mu.Lock()
	h.users.deleted[user.ID] = true
	h.users.mu.Unlock()

	if err := h.service.ResetPassword(ctx, token, "a strong one 1"); !errors.Is(err, ErrInvalidPasswordResetToken) {
		t.Fatalf("ResetPassword() error = %v, want ErrInvalidPasswordResetToken", err)
	}
}

// TestRegisterSurvivesAFailedVerificationEmail proves the send is best-effort:
// the account exists and the caller is told nothing about the provider outage
// (KNOT-ADR-090).
func TestRegisterSurvivesAFailedVerificationEmail(t *testing.T) {
	t.Parallel()

	h := newTestHarness(t)
	h.emails.err = errors.New("resend is down")

	user, err := h.service.Register(context.Background(), RegisterInput{
		Email:         "ada@example.com",
		Password:      "correct horse 7",
		DisplayName:   "Ada",
		AgreedToTerms: true,
	})
	if err != nil {
		t.Fatalf("Register() error = %v, want nil when the email fails", err)
	}
	if user.ID == "" {
		t.Fatal("Register() returned a user with no id")
	}

	// The token was still stored, so a link that did reach the person would work
	// and a resend is possible.
	if len(h.verifications.byHash) == 0 {
		t.Error("no verification token was stored")
	}
	if !strings.Contains(h.logs.String(), "sending the verification email failed") {
		t.Errorf("the failed send was not logged; logs:\n%s", h.logs.String())
	}
}

// TestRegisterSendsTheVerificationEmail is the positive half of the contract: a
// freshly registered account is emailed a link, addressed to the account, using
// the verification subject, and the success is recorded in the log. Without a
// success line a silent non-delivery is indistinguishable from a delivery.
func TestRegisterSendsTheVerificationEmail(t *testing.T) {
	t.Parallel()

	h := newTestHarness(t)

	if _, err := h.service.Register(context.Background(), RegisterInput{
		Email:         "ada@example.com",
		Password:      "correct horse 7",
		DisplayName:   "Ada",
		AgreedToTerms: true,
	}); err != nil {
		t.Fatalf("Register() error = %v, want nil", err)
	}

	messages := h.emails.messages()
	if len(messages) != 1 {
		t.Fatalf("emails sent = %d, want exactly 1", len(messages))
	}

	msg := messages[0]
	if msg.To != "ada@example.com" {
		t.Errorf("email sent to %q, want the registered address", msg.To)
	}
	if msg.Subject != email.VerificationSubject {
		t.Errorf("subject = %q, want %q", msg.Subject, email.VerificationSubject)
	}
	if !strings.Contains(msg.Text, defaultVerificationLinkBase) {
		t.Errorf("the verification email has no link; body:\n%s", msg.Text)
	}

	if !strings.Contains(h.logs.String(), "verification email accepted for delivery") {
		t.Errorf("the accepted send was not logged; logs:\n%s", h.logs.String())
	}
}

// TestRegisterResendsVerificationForAnUnverifiedAccount proves the dead end is
// gone: registering again with an address that belongs to an unverified account
// still answers ErrEmailTaken, but a fresh link is emailed first, because the
// client has no other way to ask for one (KNOT-031a-fix2).
func TestRegisterResendsVerificationForAnUnverifiedAccount(t *testing.T) {
	t.Parallel()

	h := newTestHarness(t)
	input := RegisterInput{
		Email:         "ada@example.com",
		Password:      "correct horse 7",
		DisplayName:   "Ada",
		AgreedToTerms: true,
	}

	if _, err := h.service.Register(context.Background(), input); err != nil {
		t.Fatalf("first Register() error = %v, want nil", err)
	}

	firstMessages := h.emails.messages()
	if len(firstMessages) != 1 {
		t.Fatalf("emails after the first register = %d, want 1", len(firstMessages))
	}
	firstToken := h.latestEmailToken(t, defaultVerificationLinkBase)

	if _, err := h.service.Register(context.Background(), input); !errors.Is(err, identity.ErrEmailTaken) {
		t.Fatalf("second Register() error = %v, want ErrEmailTaken", err)
	}

	secondMessages := h.emails.messages()
	if len(secondMessages) != 2 {
		t.Fatalf("emails after the second register = %d, want 2", len(secondMessages))
	}

	secondToken := h.latestEmailToken(t, defaultVerificationLinkBase)
	if secondToken == firstToken {
		t.Error("the re-sent verification link reused the first token")
	}

	// The re-issued link is the one that works, so the recovery is real rather
	// than a second undeliverable message.
	if err := h.service.VerifyEmail(context.Background(), secondToken); err != nil {
		t.Fatalf("VerifyEmail(re-issued token) error = %v, want nil", err)
	}
}

// TestRegisterDoesNotResendVerificationForAVerifiedAccount keeps the re-issue
// narrow: an address whose owner has already proved it has nothing to verify, so
// a duplicate registration attempt must not generate mail.
func TestRegisterDoesNotResendVerificationForAVerifiedAccount(t *testing.T) {
	t.Parallel()

	h := newTestHarness(t)
	h.registerAndVerify(t)

	before := len(h.emails.messages())

	if _, err := h.service.Register(context.Background(), RegisterInput{
		Email:         "ada@example.com",
		Password:      "correct horse 7",
		DisplayName:   "Ada",
		AgreedToTerms: true,
	}); !errors.Is(err, identity.ErrEmailTaken) {
		t.Fatalf("Register() error = %v, want ErrEmailTaken", err)
	}

	if after := len(h.emails.messages()); after != before {
		t.Errorf("emails sent = %d, want %d: a verified account must not be emailed again", after, before)
	}
}

// TestForgotPasswordSurvivesAFailedResetEmail is the same contract for the reset
// message. The endpoint must answer the same way for every address, so a provider
// failure can never surface as an error (KNOT-ADR-087, KNOT-ADR-090).
func TestForgotPasswordSurvivesAFailedResetEmail(t *testing.T) {
	t.Parallel()

	h := newTestHarness(t)
	h.registerAndVerify(t)
	h.emails.err = errors.New("resend is down")

	if err := h.service.ForgotPassword(context.Background(), "ada@example.com"); err != nil {
		t.Fatalf("ForgotPassword() error = %v, want nil when the email fails", err)
	}
	if !strings.Contains(h.logs.String(), "sending the password reset email failed") {
		t.Errorf("the failed send was not logged; logs:\n%s", h.logs.String())
	}
}

// TestResendVerificationSendsANewLink proves the endpoint mints a fresh token and
// emails it, which is the whole point of the endpoint (KNOT-ADR-090).
func TestResendVerificationSendsANewLink(t *testing.T) {
	t.Parallel()

	h := newTestHarness(t)

	if _, err := h.service.Register(context.Background(), RegisterInput{
		Email:         "ada@example.com",
		Password:      "correct horse 7",
		DisplayName:   "Ada",
		AgreedToTerms: true,
	}); err != nil {
		t.Fatalf("Register() error = %v, want nil", err)
	}

	before := len(h.emails.messages())

	if err := h.service.ResendVerification(context.Background(), "Ada@Example.COM"); err != nil {
		t.Fatalf("ResendVerification() error = %v, want nil", err)
	}

	messages := h.emails.messages()
	if len(messages) != before+1 {
		t.Fatalf("emails sent = %d, want %d", len(messages), before+1)
	}

	msg := messages[len(messages)-1]
	if msg.To != "ada@example.com" {
		t.Errorf("email to = %q, want the normalised address", msg.To)
	}
	if !strings.Contains(msg.Text, defaultVerificationLinkBase) {
		t.Errorf("the resent email has no link; body:\n%s", msg.Text)
	}

	// The new link must be redeemable: a resend is useless if it sends a token the
	// store does not know.
	token := extractToken(t, msg.Text, defaultVerificationLinkBase)
	if err := h.service.VerifyEmail(context.Background(), token); err != nil {
		t.Fatalf("VerifyEmail(resent token) error = %v, want nil", err)
	}
}

// TestResendVerificationStaysSilent proves nothing about an address leaks: an
// unknown address, an already verified account, and a malformed address all
// complete quietly and send nothing (KNOT-ADR-087, KNOT-ADR-090).
func TestResendVerificationStaysSilent(t *testing.T) {
	t.Parallel()

	h := newTestHarness(t)
	h.registerAndVerify(t) // ada@example.com, already verified

	before := len(h.emails.messages())

	for _, address := range []string{"nobody@example.com", "ada@example.com", "not-an-email", ""} {
		if err := h.service.ResendVerification(context.Background(), address); err != nil {
			t.Fatalf("ResendVerification(%q) error = %v, want nil", address, err)
		}
	}

	if got := len(h.emails.messages()); got != before {
		t.Errorf("emails sent = %d, want %d (nothing should be sent)", got, before)
	}
}

// TestResendVerificationReportsAStoreFailure proves a genuine infrastructure
// failure is still returned to the caller, which is what lets the handler log it
// while answering 200.
func TestResendVerificationReportsAStoreFailure(t *testing.T) {
	t.Parallel()

	h := newTestHarness(t)
	if _, err := h.service.Register(context.Background(), RegisterInput{
		Email:         "ada@example.com",
		Password:      "correct horse 7",
		DisplayName:   "Ada",
		AgreedToTerms: true,
	}); err != nil {
		t.Fatalf("Register() error = %v, want nil", err)
	}

	h.verifications.createErr = errors.New("the token table is gone")

	if err := h.service.ResendVerification(context.Background(), "ada@example.com"); err == nil {
		t.Fatal("ResendVerification() error = nil, want the store failure")
	}
}

// TestRegisterEmailCarriesAWebLinkAndTheToken pins the shape of the message a
// person actually receives: an https link the button can open in any client, the
// same token printed for copying, and the deep link as a secondary option
// (KNOT-031a-fix).
func TestRegisterEmailCarriesAWebLinkAndTheToken(t *testing.T) {
	t.Parallel()

	h := newTestHarness(t)
	if _, err := h.service.Register(context.Background(), RegisterInput{
		Email:         "ada@example.com",
		Password:      "correct horse 7",
		DisplayName:   "Ada",
		AgreedToTerms: true,
	}); err != nil {
		t.Fatalf("Register() error = %v, want nil", err)
	}

	msg := h.latestEmail(t)
	token := extractToken(t, msg.Text, testWebURL+webVerificationPath)

	assertActionTriple(t, msg, token, testWebURL+webVerificationPath, defaultVerificationLinkBase)
}

// TestForgotPasswordEmailCarriesAWebLinkAndTheToken is the same contract for the
// reset message.
func TestForgotPasswordEmailCarriesAWebLinkAndTheToken(t *testing.T) {
	t.Parallel()

	h := newTestHarness(t)
	h.registerAndVerify(t)

	if err := h.service.ForgotPassword(context.Background(), "ada@example.com"); err != nil {
		t.Fatalf("ForgotPassword() error = %v, want nil", err)
	}

	msg := h.latestEmail(t)
	token := extractToken(t, msg.Text, testWebURL+webPasswordResetPath)

	assertActionTriple(t, msg, token, testWebURL+webPasswordResetPath, defaultPasswordResetLinkBase)
}

// assertActionTriple checks one message against the three views of its secret.
func assertActionTriple(t *testing.T, msg email.Email, token, webPrefix, deepPrefix string) {
	t.Helper()

	if len(token) != 64 {
		t.Errorf("token length = %d, want 64 hex characters", len(token))
	}
	if !strings.Contains(msg.Text, "Or copy this code: "+token) {
		t.Errorf("the plain-text body does not show the token; body:\n%s", msg.Text)
	}
	if !strings.Contains(msg.Text, deepPrefix+token) {
		t.Errorf("the deep link does not carry the same token; body:\n%s", msg.Text)
	}
	if !strings.Contains(msg.HTML, `href="`+webPrefix+token+`"`) {
		t.Errorf("the HTML button does not link to %s%s; body:\n%s", webPrefix, token, msg.HTML)
	}
	if !strings.Contains(msg.HTML, token) {
		t.Errorf("the HTML body does not show the token; body:\n%s", msg.HTML)
	}
}

// TestNormalizeWebURL proves a configured origin is usable whichever way it was
// written, so a trailing slash cannot produce a double slash in a link.
func TestNormalizeWebURL(t *testing.T) {
	t.Parallel()

	cases := map[string]string{
		"https://knot.example":      "https://knot.example",
		"https://knot.example/":     "https://knot.example",
		"https://knot.example//":    "https://knot.example",
		"  https://knot.example/  ": "https://knot.example",
		"":                          "",
	}

	for raw, want := range cases {
		if got := normalizeWebURL(raw); got != want {
			t.Errorf("normalizeWebURL(%q) = %q, want %q", raw, got, want)
		}
	}
}
