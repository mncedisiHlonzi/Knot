package email

import (
	"context"
	"log/slog"
)

// NewNoopSender returns a Sender that delivers nothing and logs what it was
// asked to send.
//
// It is what NewResendSender falls back to when no API key is configured, and it
// is what a test can pass when it does not care about delivery. The plain-text
// body is logged at warn level, including the action link, so local development
// keeps its round trip without a Resend account (KNOT-ADR-090).
//
// A nil logger falls back to the process default, so the zero argument is never a
// panic.
func NewNoopSender(logger *slog.Logger) Sender {
	if logger == nil {
		logger = slog.Default()
	}

	return &noopSender{logger: logger}
}

// noopSender is the disabled sender. It is a pointer type so the interface value
// is never re-boxed per call.
type noopSender struct {
	logger *slog.Logger
}

// Send logs the message instead of delivering it and always succeeds: a disabled
// provider is a configuration choice, not a failure the caller should handle.
func (s *noopSender) Send(ctx context.Context, msg Email) error {
	s.logger.WarnContext(ctx,
		"email delivery is disabled (no KNOT_RESEND_API_KEY); logging the message instead",
		slog.String("to", msg.To),
		slog.String("subject", msg.Subject),
		slog.String("body", msg.Text),
	)

	return nil
}
