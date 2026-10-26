// Package email delivers Knot's transactional messages: the address
// verification link and the password reset link.
//
// The package is provider-agnostic on purpose. A caller depends on the Sender
// interface and the Email value and never on Resend, which keeps the provider
// replaceable and lets the authentication service be tested without a network or
// an account (KNOT-ADR-090).
//
// Delivery is best-effort at every call site: a failed send is a log line, never
// an error a person sees, because creating an account or asking for a reset must
// not depend on a third party being reachable.
package email

import "context"

// Email is one message ready to send.
//
// It holds rendered bodies rather than template names so a Sender only has to
// hand the provider three strings, and so the rendering (which can fail) happens
// in a place the caller can report.
type Email struct {
	// To is the recipient address. Both messages this package renders are
	// transactional and addressed to exactly one person.
	To string
	// Subject is the subject line.
	Subject string
	// HTML is the rich body, rendered from a template in this package.
	HTML string
	// Text is the plain-text body: the fallback for a client that refuses HTML,
	// and the body a local development run logs when delivery is disabled.
	Text string
}

// Sender delivers email.
//
// Implementations must honour the context so a caller can bound the send, and
// must return an error the caller can log. Every caller in this repository
// treats a returned error as a best-effort failure and finishes its own work
// regardless (KNOT-ADR-090).
type Sender interface {
	Send(ctx context.Context, email Email) error
}
