// Package testutil holds small helpers shared by the backend's tests.
//
// It is a normal package rather than a _test.go file so that tests in other packages
// can import it; no production code imports it. Keeping these helpers in one place is
// what stops each package growing its own subtly different copy, and the tamper
// helper in particular exists to enforce a rule that is very easy to forget.
package testutil

import (
	"bytes"
	"encoding/base64"
	"fmt"
	"testing"
)

// Fataler is the slice of testing.TB that TamperBase64Body needs.
//
// It is a narrow interface rather than testing.TB because testing.TB cannot be
// implemented outside the testing package — it has unexported methods — so a
// recording fake is impossible against it. Both *testing.T and RecordingFataler
// satisfy this interface, which is what makes the failure paths testable.
type Fataler interface {
	Helper()
	Fatal(args ...any)
	Fatalf(format string, args ...any)
}

// TamperBase64Body decodes the base64 body of encoded, applies mutate to the decoded
// bytes, and returns the re-encoded string together with the bytes before and after
// the mutation.
//
// # Why this helper exists
//
// A test that "tampers" with an encoded value by editing its final character can be a
// no-op, and therefore pass vacuously. The final character of a base64 encoding whose
// raw bytes are not a multiple of three carries fewer than six significant bits; the
// remaining low bits are padding, and Go's non-strict decoders ignore them. Every
// mutation that touches only those padding bits decodes to byte-identical output, so
// the value under test is never actually tampered with and the test asserts nothing.
//
// This has already caused two flaky tests in this repository: one mutated the final
// character of a JWT signature, the other the final character of an argon2id key.
// Each failed roughly one run in sixteen. Mutating decoded bytes cannot be undone by
// padding bits, and routing every tamper through this helper makes that the only
// available approach.
//
// The helper enforces the vacuity guard so no call site has to remember it: if mutate
// leaves the bytes unchanged, the test fails loudly instead of passing silently.
//
// # Choosing the encoding
//
// The caller passes the base64 encoding explicitly, because it cannot be inferred
// reliably. A body whose characters all belong to both alphabets decodes identically
// under either encoding, yet re-encoding the mutated bytes may emit '-' or '_'
// (URL-safe) or '+' or '/' (standard), which the other alphabet cannot read. Inferring
// the alphabet from an ambiguous body is a real bug, not a theoretical one: an earlier
// version of this helper did exactly that and failed roughly 1 run in 70 when its
// caller's consumer expected standard base64.
func TamperBase64Body(t *testing.T, encoding *base64.Encoding, encoded string, mutate func([]byte)) (tampered string, before, after []byte) {
	t.Helper()

	return TamperBase64BodyWith(t, encoding, encoded, mutate)
}

// TamperBase64BodyWith is TamperBase64Body's implementation. It reports failures
// through the supplied Fataler rather than a concrete *testing.T.
//
// Each Fatal is followed by a return. With a real *testing.T that return is
// unreachable, because Fatal does not return; with a recording fake it prevents the
// function from continuing into code that assumes the input was valid, such as
// dereferencing a nil encoding.
func TamperBase64BodyWith(f Fataler, encoding *base64.Encoding, encoded string, mutate func([]byte)) (tampered string, before, after []byte) {
	f.Helper()

	if encoding == nil {
		f.Fatal("TamperBase64Body requires a base64 encoding")
		return "", nil, nil
	}
	if mutate == nil {
		f.Fatal("TamperBase64Body requires a mutate function")
		return "", nil, nil
	}

	before, err := encoding.DecodeString(encoded)
	if err != nil {
		f.Fatalf("TamperBase64Body: decoding %q: %v", encoded, err)
		return "", nil, nil
	}
	if len(before) == 0 {
		f.Fatalf("TamperBase64Body: %q decoded to no bytes, so there is nothing to tamper with", encoded)
		return "", nil, nil
	}

	after = make([]byte, len(before))
	copy(after, before)
	mutate(after)

	if bytes.Equal(before, after) {
		f.Fatal("TamperBase64Body: the mutation left the decoded bytes unchanged, so the value was never actually tampered with; this is the vacuity trap this helper exists to prevent")
		return "", nil, nil
	}

	return encoding.EncodeToString(after), before, after
}

// RecordingFataler records fatal messages instead of aborting the test, so a test can
// assert that a helper rejected an input. Because TamperBase64BodyWith returns
// immediately after reporting a fatal, a recorded fatal never lets execution continue
// into code that assumes the input was valid.
//
// It is test-only infrastructure; no production code uses it.
type RecordingFataler struct {
	messages []string
}

// Helper implements Fataler.
func (r *RecordingFataler) Helper() {}

// Fatal implements Fataler by recording the message.
func (r *RecordingFataler) Fatal(args ...any) {
	r.messages = append(r.messages, fmt.Sprint(args...))
}

// Fatalf implements Fataler by recording the formatted message.
func (r *RecordingFataler) Fatalf(format string, args ...any) {
	r.messages = append(r.messages, fmt.Sprintf(format, args...))
}

// Messages returns the recorded fatal messages, in order.
func (r *RecordingFataler) Messages() []string {
	return r.messages
}
