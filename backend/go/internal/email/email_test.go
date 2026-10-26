package email

import (
	"context"
	"errors"
	"html/template"
	"log/slog"
	"strings"
	"testing"
	texttemplate "text/template"

	"github.com/resend/resend-go/v4"
)

// recordingClient is a stand-in for the Resend client. It records what the
// adapter would have sent and returns whatever the test asked for, so no test in
// this file can reach the network.
type recordingClient struct {
	sent []*resend.SendEmailRequest
	err  error
}

func (c *recordingClient) SendWithContext(_ context.Context, params *resend.SendEmailRequest) (*resend.SendEmailResponse, error) {
	c.sent = append(c.sent, params)
	if c.err != nil {
		return nil, c.err
	}

	return &resend.SendEmailResponse{Id: "email_123"}, nil
}

// recordingSender is a Sender that keeps what it was handed. It is the mock the
// authentication service tests use, and it proves the interface is usable
// without a provider.
type recordingSender struct {
	sent []Email
	err  error
}

func (s *recordingSender) Send(_ context.Context, msg Email) error {
	if s.err != nil {
		return s.err
	}
	s.sent = append(s.sent, msg)

	return nil
}

func testEmail() Email {
	return Email{
		To:      "ada@example.com",
		Subject: VerificationSubject,
		HTML:    "<p>hello</p>",
		Text:    "hello",
	}
}

// TestResendSenderBuildsTheRequest proves the adapter hands the provider exactly
// the fields of the Email it was given, from the configured sender.
func TestResendSenderBuildsTheRequest(t *testing.T) {
	t.Parallel()

	client := &recordingClient{}
	sender := &resendSender{client: client, from: "Knot <hello@knot.example>"}

	msg := testEmail()
	if err := sender.Send(context.Background(), msg); err != nil {
		t.Fatalf("Send() error = %v, want nil", err)
	}

	if len(client.sent) != 1 {
		t.Fatalf("provider received %d requests, want 1", len(client.sent))
	}

	request := client.sent[0]
	if request.From != "Knot <hello@knot.example>" {
		t.Errorf("from = %q, want the configured sender", request.From)
	}
	if len(request.To) != 1 || request.To[0] != msg.To {
		t.Errorf("to = %v, want [%q]", request.To, msg.To)
	}
	if request.Subject != msg.Subject {
		t.Errorf("subject = %q, want %q", request.Subject, msg.Subject)
	}
	if request.Html != msg.HTML {
		t.Errorf("html = %q, want %q", request.Html, msg.HTML)
	}
	if request.Text != msg.Text {
		t.Errorf("text = %q, want %q", request.Text, msg.Text)
	}
	if len(request.Bcc) != 0 || len(request.Cc) != 0 {
		t.Errorf("unexpected copies on the request: bcc=%v cc=%v", request.Bcc, request.Cc)
	}
}

// TestResendSenderWrapsProviderErrors proves a failure is returned with its
// cause, because the caller logs it and the flow continues.
func TestResendSenderWrapsProviderErrors(t *testing.T) {
	t.Parallel()

	providerErr := errors.New("resend is down")
	sender := &resendSender{client: &recordingClient{err: providerErr}, from: DefaultFromAddress}

	err := sender.Send(context.Background(), testEmail())

	if !errors.Is(err, providerErr) {
		t.Fatalf("Send() error = %v, want it to wrap %v", err, providerErr)
	}
	if !strings.Contains(err.Error(), "email: resend send") {
		t.Errorf("error = %q, want it to name the operation", err)
	}
}

// TestResendSenderRejectsAMissingRecipient proves a message with no To never
// reaches the provider.
func TestResendSenderRejectsAMissingRecipient(t *testing.T) {
	t.Parallel()

	client := &recordingClient{}
	sender := &resendSender{client: client, from: DefaultFromAddress}

	err := sender.Send(context.Background(), Email{Subject: "no recipient", Text: "body"})

	if !errors.Is(err, ErrMissingRecipient) {
		t.Fatalf("Send() error = %v, want ErrMissingRecipient", err)
	}
	if len(client.sent) != 0 {
		t.Errorf("provider received %d requests, want 0", len(client.sent))
	}
}

// TestResendSenderHonoursACancelledContext proves the context reaches the send.
func TestResendSenderHonoursACancelledContext(t *testing.T) {
	t.Parallel()

	client := &recordingClient{}
	sender := &resendSender{client: client, from: DefaultFromAddress}

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	if err := sender.Send(ctx, testEmail()); !errors.Is(err, context.Canceled) {
		t.Fatalf("Send() error = %v, want context.Canceled", err)
	}
	if len(client.sent) != 0 {
		t.Errorf("provider received %d requests, want 0", len(client.sent))
	}
}

// TestNewResendSenderWithoutAPIKeyIsAWorkingNoop proves local development works
// with no Resend account: the sender succeeds and the message is logged.
func TestNewResendSenderWithoutAPIKeyIsAWorkingNoop(t *testing.T) {
	t.Parallel()

	logs := &strings.Builder{}
	logger := slog.New(slog.NewTextHandler(logs, nil))

	sender, err := NewResendSender("", DefaultFromAddress, WithLogger(logger))
	if err != nil {
		t.Fatalf("NewResendSender() error = %v, want nil", err)
	}
	if sender == nil {
		t.Fatal("NewResendSender() returned a nil sender")
	}

	if err := sender.Send(context.Background(), testEmail()); err != nil {
		t.Fatalf("Send() error = %v, want nil from the no-op sender", err)
	}

	logged := logs.String()
	if !strings.Contains(logged, "email delivery is disabled") {
		t.Errorf("the disabled sender did not warn; logs:\n%s", logged)
	}
	if !strings.Contains(logged, "ada@example.com") {
		t.Errorf("the logged message does not name the recipient; logs:\n%s", logged)
	}
}

// TestNewResendSenderValidatesTheFromAddress proves a configured but unusable
// sender is reported at construction rather than on the first registration.
func TestNewResendSenderValidatesTheFromAddress(t *testing.T) {
	t.Parallel()

	logger := slog.New(slog.NewTextHandler(&strings.Builder{}, nil))

	if _, err := NewResendSender("re_test_key", "not-an-address", WithLogger(logger)); err == nil {
		t.Fatal("NewResendSender() with an unusable from address error = nil, want an error")
	}
}

// TestNewResendSenderDefaultsTheFromAddress proves an empty From falls back to
// the sandbox sender rather than producing an empty header.
func TestNewResendSenderDefaultsTheFromAddress(t *testing.T) {
	t.Parallel()

	logger := slog.New(slog.NewTextHandler(&strings.Builder{}, nil))

	sender, err := NewResendSender("re_test_key", "", WithLogger(logger))
	if err != nil {
		t.Fatalf("NewResendSender() error = %v, want nil", err)
	}

	concrete, ok := sender.(*resendSender)
	if !ok {
		t.Fatalf("sender type = %T, want *resendSender", sender)
	}
	if concrete.from != DefaultFromAddress {
		t.Errorf("from = %q, want %q", concrete.from, DefaultFromAddress)
	}
}

// TestSenderIsSatisfiedByBothImplementations is a compile-time-shaped check that
// the interface is the only thing a caller needs.
func TestSenderIsSatisfiedByBothImplementations(t *testing.T) {
	t.Parallel()

	var _ Sender = (*resendSender)(nil)
	var _ Sender = NewNoopSender(nil)
	var _ Sender = (*recordingSender)(nil)
}

// TestVerificationEmailCarriesTheDeepLink proves the template renders the action
// link into both bodies, and that the custom scheme survives html/template's URL
// filter (which would otherwise emit #ZgotmplZ).
func TestVerificationEmailCarriesTheDeepLink(t *testing.T) {
	t.Parallel()

	deepLink := "knot://auth/verify-email?token=abc123"

	msg, err := Verification("ada@example.com", deepLink)
	if err != nil {
		t.Fatalf("Verification() error = %v, want nil", err)
	}

	if msg.To != "ada@example.com" {
		t.Errorf("to = %q, want the recipient", msg.To)
	}
	if msg.Subject != VerificationSubject {
		t.Errorf("subject = %q, want %q", msg.Subject, VerificationSubject)
	}
	if !strings.Contains(msg.HTML, `href="`+deepLink+`"`) {
		t.Errorf("the HTML button does not link to %q; body:\n%s", deepLink, msg.HTML)
	}
	if strings.Contains(msg.HTML, "#ZgotmplZ") {
		t.Errorf("html/template filtered the knot:// scheme out of the button; body:\n%s", msg.HTML)
	}
	if !strings.Contains(msg.Text, deepLink) {
		t.Errorf("the plain-text body does not contain %q; body:\n%s", deepLink, msg.Text)
	}
	if !strings.Contains(msg.Text, "expires in 24 hours") {
		t.Errorf("the plain-text body does not state the expiry; body:\n%s", msg.Text)
	}
}

// TestPasswordResetEmailCarriesTheDeepLink is the same contract for the second
// message.
func TestPasswordResetEmailCarriesTheDeepLink(t *testing.T) {
	t.Parallel()

	deepLink := "knot://auth/reset-password?token=def456"

	msg, err := PasswordReset("ada@example.com", deepLink)
	if err != nil {
		t.Fatalf("PasswordReset() error = %v, want nil", err)
	}

	if msg.Subject != PasswordResetSubject {
		t.Errorf("subject = %q, want %q", msg.Subject, PasswordResetSubject)
	}
	if !strings.Contains(msg.HTML, `href="`+deepLink+`"`) {
		t.Errorf("the HTML button does not link to %q; body:\n%s", deepLink, msg.HTML)
	}
	if strings.Contains(msg.HTML, "#ZgotmplZ") {
		t.Errorf("html/template filtered the knot:// scheme out of the button; body:\n%s", msg.HTML)
	}
	if !strings.Contains(msg.Text, deepLink) {
		t.Errorf("the plain-text body does not contain %q; body:\n%s", deepLink, msg.Text)
	}
	if !strings.Contains(msg.Text, "expires in 30 minutes") {
		t.Errorf("the plain-text body does not state the expiry; body:\n%s", msg.Text)
	}
}

// TestTemplatesDoNotUseRawHTML is a guard on the renderer: a body that echoes the
// deep link as markup rather than text would be an injection surface if the link
// ever stopped being server-generated.
func TestTemplatesDoNotUseRawHTML(t *testing.T) {
	t.Parallel()

	msg, err := Verification("ada@example.com", `knot://auth/verify-email?token="><script>`)
	if err != nil {
		t.Fatalf("Verification() error = %v, want nil", err)
	}

	if strings.Contains(msg.HTML, "<script>") {
		t.Errorf("the rendered HTML contains an unescaped script tag; body:\n%s", msg.HTML)
	}
}

// TestRenderReportsATemplateFailure proves a broken execution is returned rather
// than producing an empty body the provider would reject.
func TestRenderReportsATemplateFailure(t *testing.T) {
	t.Parallel()

	templates := messageTemplates{
		html: template.Must(template.New("failing").Parse("{{.Missing}}")),
		text: texttemplate.Must(texttemplate.New("failing").Parse("unused")),
	}

	if _, err := render(templates, "subject", "ada@example.com", "knot://x"); err == nil {
		t.Fatal("render() with a failing template error = nil, want an error")
	}
}
