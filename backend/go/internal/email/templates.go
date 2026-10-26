package email

import (
	"bytes"
	"embed"
	"fmt"
	"html/template"
	"path"
	texttemplate "text/template"
)

// Templates are embedded so a built binary carries its own email bodies: nothing
// has to be deployed beside the server, and a missing template is a build failure
// rather than a runtime surprise.
//
//go:embed templates/*.html templates/*.txt
var templateFS embed.FS

// Subjects of the two messages. They are constants because a test and the docs
// both name them.
const (
	// VerificationSubject is the subject of the address verification email.
	VerificationSubject = "Verify your Knot email address"
	// PasswordResetSubject is the subject of the password reset email.
	PasswordResetSubject = "Reset your Knot password"
)

// Action is the call to action one message carries.
//
// It holds three views of the same thing, because a mail client cannot be relied
// on to support all of them: WebLink is the https URL behind the button, DeepLink
// is the knot:// URL that opens the app directly, and Token is the raw secret both
// of them carry, printed so the person can always paste it by hand
// (KNOT-031a-fix).
type Action struct {
	// WebLink is the clickable https URL. It is what the button points at.
	WebLink string
	// DeepLink is the knot:// URL, offered as a secondary note for clients that do
	// treat a custom scheme as a link.
	DeepLink string
	// Token is the raw secret, shown verbatim under the button.
	Token string
}

// templateData is what every template is rendered with.
//
// The link fields are template.URL rather than string for a reason worth
// recording: Go's html/template refuses to put an unknown URL scheme into an
// href, and knot:// is not http, https, or mailto. Typing the value as
// template.URL marks it as already validated, which is true here because the
// service builds it from a constant prefix plus hex-encoded random material.
// Without this the deep-link note would render href="#ZgotmplZ".
type templateData struct {
	WebLink  template.URL
	DeepLink template.URL
	Token    string
}

// messageTemplates is one message in both of its bodies.
type messageTemplates struct {
	html *template.Template
	text *texttemplate.Template
}

// verificationTemplates is parsed once at start-up. ParseFS panics on a broken
// template, which is the right moment to find out: the files are embedded, so a
// failure means the binary was built from broken source.
var verificationTemplates = parseTemplates(
	"templates/verification.html",
	"templates/verification.txt",
)

// resetTemplates is the password reset message.
var resetTemplates = parseTemplates(
	"templates/reset.html",
	"templates/reset.txt",
)

// parseTemplates loads one message's HTML and plain-text bodies.
//
// ParseFS names each template after the file's base name, so the root template
// is created with the same base name; naming it with the full path instead would
// leave the root empty and the execution would fail.
func parseTemplates(htmlPath, textPath string) messageTemplates {
	return messageTemplates{
		html: template.Must(template.New(path.Base(htmlPath)).ParseFS(templateFS, htmlPath)),
		text: texttemplate.Must(texttemplate.New(path.Base(textPath)).ParseFS(templateFS, textPath)),
	}
}

// Verification renders the address verification email.
//
// The caller owns the action: the authentication service builds both links and
// holds the token, because all three belong to authentication policy, not to
// presentation.
func Verification(to string, action Action) (Email, error) {
	return render(verificationTemplates, VerificationSubject, to, action)
}

// PasswordReset renders the password reset email.
func PasswordReset(to string, action Action) (Email, error) {
	return render(resetTemplates, PasswordResetSubject, to, action)
}

// render executes both bodies of one message.
func render(templates messageTemplates, subject, to string, action Action) (Email, error) {
	data := templateData{
		WebLink:  template.URL(action.WebLink),
		DeepLink: template.URL(action.DeepLink),
		Token:    action.Token,
	}

	var htmlBody bytes.Buffer
	if err := templates.html.Execute(&htmlBody, data); err != nil {
		return Email{}, fmt.Errorf("email: render %s html: %w", subject, err)
	}

	var textBody bytes.Buffer
	if err := templates.text.Execute(&textBody, data); err != nil {
		return Email{}, fmt.Errorf("email: render %s text: %w", subject, err)
	}

	return Email{
		To:      to,
		Subject: subject,
		HTML:    htmlBody.String(),
		Text:    textBody.String(),
	}, nil
}
