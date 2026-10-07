package identity

import (
	"bytes"
	"encoding/base64"
	"testing"
)

// tamperBase64Body decodes the base64 body of encoded, applies mutate to the decoded
// bytes, and returns the re-encoded string together with the bytes before and after
// the mutation.
//
// # Why this helper exists
//
// A test that "tampers" with an encoded value by editing its final character can be
// a no-op, and therefore pass vacuously. The final character of a base64 encoding
// whose raw bytes are not a multiple of three carries fewer than six significant
// bits; the remaining low bits are padding, and Go's non-strict decoders ignore
// them. Every mutation that touches only those padding bits decodes to
// byte-identical output, so the value under test is never actually tampered with and
// the test asserts nothing.
//
// Two tests in this package were flaky for exactly that reason: one mutated the final
// character of a JWT signature, the other the final character of an argon2id key.
// Each failed roughly one run in sixteen, and each was "fixed" the same way. Mutating
// decoded bytes cannot be undone by padding bits, and routing every tamper through
// this helper makes that the only available approach.
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
// caller's consumer expected standard base64, because it re-encoded a standard body on
// the URL-safe alphabet.
func tamperBase64Body(t *testing.T, encoding *base64.Encoding, encoded string, mutate func([]byte)) (tampered string, before, after []byte) {
	t.Helper()

	if encoding == nil {
		t.Fatal("tamperBase64Body requires a base64 encoding")
	}
	if mutate == nil {
		t.Fatal("tamperBase64Body requires a mutate function")
	}

	before, err := encoding.DecodeString(encoded)
	if err != nil {
		t.Fatalf("tamperBase64Body: decoding %q: %v", encoded, err)
	}
	if len(before) == 0 {
		t.Fatalf("tamperBase64Body: %q decoded to no bytes, so there is nothing to tamper with", encoded)
	}

	after = make([]byte, len(before))
	copy(after, before)
	mutate(after)

	if bytes.Equal(before, after) {
		t.Fatal("tamperBase64Body: the mutation left the decoded bytes unchanged, so the value was never actually tampered with; this is the vacuity trap this helper exists to prevent")
	}

	return encoding.EncodeToString(after), before, after
}
