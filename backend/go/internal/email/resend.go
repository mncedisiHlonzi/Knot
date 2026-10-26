package email

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"strings"

	"github.com/resend/resend-go/v4"
)

// DefaultFromAddress is the sender the Resend sandbox accepts without a verified
// domain. A deployment that owns a domain sets KNOT_EMAIL_FROM instead; until
// then this is the only address Resend will actually deliver from.
const DefaultFromAddress = "Knot <onboarding@resend.dev>"

// ErrMissingRecipient is returned when a message has no To address. It is a
// programming error rather than a delivery failure: the templates always address
// a real account.
var ErrMissingRecipient = errors.New("email: missing recipient")

// Option configures the Resend sender.
type Option func(*resendConfig)

// resendConfig is the resolved option set.
type resendConfig struct {
	logger *slog.Logger
}

// WithLogger sets the logger the sender uses for its own messages: the notice
// that delivery is disabled, and nothing else. Delivery failures are returned to
// the caller, which owns the decision to log them.
func WithLogger(logger *slog.Logger) Option {
	return func(cfg *resendConfig) {
		if logger != nil {
			cfg.logger = logger
		}
	}
}

// resendClient is the slice of the Resend client this package uses.
//
// Depending on the interface rather than *resend.Client is what makes the
// request the adapter builds observable in a test without a network call.
type resendClient interface {
	SendWithContext(ctx context.Context, params *resend.SendEmailRequest) (*resend.SendEmailResponse, error)
}

// resendSender delivers through Resend (KNOT-ADR-090).
type resendSender struct {
	client resendClient
	// from is the From header, already validated by the constructor.
	from string
}

// NewResendSender builds the Sender backed by the official Resend Go SDK.
//
// An empty apiKey is a supported configuration: the returned Sender is a no-op
// that logs each message, which is how local development and CI run without a
// Resend account. A non-empty apiKey with an unusable fromAddress (no @) is a
// configuration mistake and is reported, so the process fails at startup rather
// than on the first registration.
func NewResendSender(apiKey, fromAddress string, opts ...Option) (Sender, error) {
	cfg := resendConfig{logger: slog.Default()}
	for _, opt := range opts {
		opt(&cfg)
	}

	key := strings.TrimSpace(apiKey)
	if key == "" {
		cfg.logger.Warn("KNOT_RESEND_API_KEY is empty; verification and reset emails will be logged instead of sent")

		return NewNoopSender(cfg.logger), nil
	}

	from := strings.TrimSpace(fromAddress)
	if from == "" {
		from = DefaultFromAddress
	}
	if !strings.Contains(from, "@") {
		return nil, fmt.Errorf("email: KNOT_EMAIL_FROM %q is not an email address", from)
	}

	return &resendSender{client: resend.NewClient(key).Emails, from: from}, nil
}

// Send delivers one message through Resend.
//
// The error wraps the provider's own so a log line names the cause, and the
// context is passed through so a request that has already gone away is not sent.
func (s *resendSender) Send(ctx context.Context, msg Email) error {
	if err := ctx.Err(); err != nil {
		return fmt.Errorf("email: resend send: %w", err)
	}
	if strings.TrimSpace(msg.To) == "" {
		return fmt.Errorf("email: resend send: %w", ErrMissingRecipient)
	}

	if _, err := s.client.SendWithContext(ctx, s.request(msg)); err != nil {
		return fmt.Errorf("email: resend send: %w", err)
	}

	return nil
}

// request is the Resend payload for one message.
//
// It is a separate method so a test can assert exactly what would go over the
// wire without standing up the provider.
func (s *resendSender) request(msg Email) *resend.SendEmailRequest {
	return &resend.SendEmailRequest{
		From:    s.from,
		To:      []string{msg.To},
		Subject: msg.Subject,
		Html:    msg.HTML,
		Text:    msg.Text,
	}
}
