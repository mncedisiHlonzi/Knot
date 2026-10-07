package identity

import (
	"bytes"
	"encoding/base64"
	"fmt"
	"strings"
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

	return tamperBase64BodyWith(t, encoding, encoded, mutate)
}

// tamperFataler is the slice of testing.TB that tamperBase64BodyWith needs.
//
// It is a narrow interface rather than testing.TB because testing.TB cannot be
// implemented outside the testing package — it has unexported methods — so a
// test-local recording fake is impossible against it. Both *testing.T and the
// recordingFataler used by this file's tests satisfy this interface, which is what
// makes the failure paths testable.
type tamperFataler interface {
	Helper()
	Fatal(args ...any)
	Fatalf(format string, args ...any)
}

// tamperBase64BodyWith is tamperBase64Body's implementation. It reports failures
// through the supplied tamperFataler rather than a concrete *testing.T.
//
// Each Fatal is followed by a return. With a real *testing.T that return is
// unreachable, because Fatal does not return; with a recording fake it prevents the
// function from continuing into code that assumes the input was valid, such as
// dereferencing a nil encoding.
func tamperBase64BodyWith(f tamperFataler, encoding *base64.Encoding, encoded string, mutate func([]byte)) (tampered string, before, after []byte) {
	f.Helper()

	if encoding == nil {
		f.Fatal("tamperBase64Body requires a base64 encoding")
		return "", nil, nil
	}
	if mutate == nil {
		f.Fatal("tamperBase64Body requires a mutate function")
		return "", nil, nil
	}

	before, err := encoding.DecodeString(encoded)
	if err != nil {
		f.Fatalf("tamperBase64Body: decoding %q: %v", encoded, err)
		return "", nil, nil
	}
	if len(before) == 0 {
		f.Fatalf("tamperBase64Body: %q decoded to no bytes, so there is nothing to tamper with", encoded)
		return "", nil, nil
	}

	after = make([]byte, len(before))
	copy(after, before)
	mutate(after)

	if bytes.Equal(before, after) {
		f.Fatal("tamperBase64Body: the mutation left the decoded bytes unchanged, so the value was never actually tampered with; this is the vacuity trap this helper exists to prevent")
		return "", nil, nil
	}

	return encoding.EncodeToString(after), before, after
}

// recordingFataler records fatal messages instead of aborting the test, so a test can
// assert that the helper rejected an input. Because tamperBase64BodyWith returns
// immediately after reporting a fatal, a recorded fatal never lets execution continue
// into code that assumes the input was valid.
type recordingFataler struct {
	fatals []string
}

// Helper implements tamperFataler.
func (r *recordingFataler) Helper() {}

// Fatal implements tamperFataler.
func (r *recordingFataler) Fatal(args ...any) {
	r.fatals = append(r.fatals, fmt.Sprint(args...))
}

// Fatalf implements tamperFataler.
func (r *recordingFataler) Fatalf(format string, args ...any) {
	r.fatals = append(r.fatals, fmt.Sprintf(format, args...))
}

// requireFatal asserts that exactly one fatal message was recorded and that it
// mentions want.
func (r *recordingFataler) requireFatal(t *testing.T, want string) {
	t.Helper()

	if len(r.fatals) != 1 {
		t.Fatalf("recorded %d fatal messages %q, want exactly 1", len(r.fatals), r.fatals)
	}
	if !strings.Contains(r.fatals[0], want) {
		t.Errorf("fatal message = %q, want it to contain %q", r.fatals[0], want)
	}
}

// standardOnlyBody returns a standard-base64 body containing at least one character
// ('+' or '/') that does not exist in the URL-safe alphabet, together with the bytes
// it decodes to. The self-check keeps the fixture honest: if it ever stopped
// containing such a character, the encoding tests below would stop discriminating.
func standardOnlyBody(t *testing.T) (body string, raw []byte) {
	t.Helper()

	raw = []byte{0xFB, 0xFF}
	body = base64.RawStdEncoding.EncodeToString(raw)
	if !strings.ContainsAny(body, "+/") {
		t.Fatalf("standard fixture %q contains no '+' or '/' character", body)
	}

	return body, raw
}

// urlOnlyBody is the URL-safe counterpart of standardOnlyBody and must contain at
// least one '-' or '_'.
func urlOnlyBody(t *testing.T) (body string, raw []byte) {
	t.Helper()

	raw = []byte{0xFB, 0xFF, 0xFF}
	body = base64.RawURLEncoding.EncodeToString(raw)
	if !strings.ContainsAny(body, "-_") {
		t.Fatalf("URL-safe fixture %q contains no '-' or '_' character", body)
	}

	return body, raw
}

// TestTamperBase64BodyRoundTripStandard covers the success path end to end: the
// returned body differs from the input, decodes back to the mutated bytes with the
// same encoding, and reports the before and after bytes correctly.
func TestTamperBase64BodyRoundTripStandard(t *testing.T) {
	body, raw := standardOnlyBody(t)
	encoding := base64.RawStdEncoding

	original, err := encoding.DecodeString(body)
	if err != nil {
		t.Fatalf("decoding the fixture: %v", err)
	}
	if !bytes.Equal(original, raw) {
		t.Fatalf("fixture decoded to %x, want %x", original, raw)
	}

	tampered, before, after := tamperBase64Body(t, encoding, body, func(decoded []byte) {
		decoded[0] ^= 0x01
	})

	if tampered == body {
		t.Error("tampered body equals the original, so the mutation had no effect on the encoding")
	}
	if !bytes.Equal(before, original) {
		t.Errorf("before = %x, want the original decoded bytes %x", before, original)
	}

	want := make([]byte, len(original))
	copy(want, original)
	want[0] ^= 0x01

	if !bytes.Equal(after, want) {
		t.Errorf("after = %x, want the mutated bytes %x", after, want)
	}
	if bytes.Equal(after, before) {
		t.Error("after equals before, want the mutation to have changed the bytes")
	}

	roundTripped, err := encoding.DecodeString(tampered)
	if err != nil {
		t.Fatalf("decoding the tampered body with the same encoding: %v", err)
	}
	if !bytes.Equal(roundTripped, after) {
		t.Errorf("tampered body decodes to %x, want the mutated bytes %x", roundTripped, after)
	}
}

// TestTamperBase64BodyBothAlphabetsRoundTrip checks that each alphabet round-trips
// with its own encoding, including for bodies that only one alphabet can express and
// for bodies whose characters are common to both.
func TestTamperBase64BodyBothAlphabetsRoundTrip(t *testing.T) {
	standardOnly, _ := standardOnlyBody(t)
	urlOnly, _ := urlOnlyBody(t)

	tests := []struct {
		name     string
		encoding *base64.Encoding
		body     string
	}{
		{
			name:     "standard alphabet with a standard-only body",
			encoding: base64.RawStdEncoding,
			body:     standardOnly,
		},
		{
			name:     "URL-safe alphabet with a URL-safe-only body",
			encoding: base64.RawURLEncoding,
			body:     urlOnly,
		},
		{
			name:     "standard alphabet with a common-character body",
			encoding: base64.RawStdEncoding,
			body:     base64.RawStdEncoding.EncodeToString([]byte("common")),
		},
		{
			name:     "URL-safe alphabet with a common-character body",
			encoding: base64.RawURLEncoding,
			body:     base64.RawURLEncoding.EncodeToString([]byte("common")),
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Mutating the LAST decoded byte is deliberate: it is exactly the byte
			// whose encoded representation used to be mutated by hand, and it is
			// where the padding-bit trap lived.
			tampered, _, after := tamperBase64Body(t, tt.encoding, tt.body, func(decoded []byte) {
				decoded[len(decoded)-1] ^= 0x01
			})

			if tampered == tt.body {
				t.Error("tampered body equals the original")
			}

			decoded, err := tt.encoding.DecodeString(tampered)
			if err != nil {
				t.Fatalf("decoding the tampered body with its own encoding: %v", err)
			}
			if !bytes.Equal(decoded, after) {
				t.Errorf("round-tripped bytes = %x, want %x", decoded, after)
			}
		})
	}
}

// TestTamperBase64BodyVacuityGuardFires is the regression guard for the bug class this
// helper exists to close: a mutation that changes nothing must be reported, not
// silently accepted.
func TestTamperBase64BodyVacuityGuardFires(t *testing.T) {
	body, _ := standardOnlyBody(t)

	t.Run("mutation that does nothing", func(t *testing.T) {
		recorder := &recordingFataler{}

		tamperBase64BodyWith(recorder, base64.RawStdEncoding, body, func([]byte) {})

		recorder.requireFatal(t, "vacuity trap")
	})

	t.Run("mutation that cancels itself out", func(t *testing.T) {
		recorder := &recordingFataler{}

		tamperBase64BodyWith(recorder, base64.RawStdEncoding, body, func(decoded []byte) {
			decoded[0] ^= 0x01
			decoded[0] ^= 0x01
		})

		recorder.requireFatal(t, "vacuity trap")
	})
}

// TestTamperBase64BodyRejectsWrongEncoding documents the KNOT-003d defect directly. A
// body in one alphabet must not be decoded with the other, and the helper must report
// that as a failure rather than re-encoding it in a way its consumer cannot read.
func TestTamperBase64BodyRejectsWrongEncoding(t *testing.T) {
	standardOnly, _ := standardOnlyBody(t)
	urlOnly, _ := urlOnlyBody(t)

	t.Run("a standard-only body is not decodable as URL-safe", func(t *testing.T) {
		if _, err := base64.RawURLEncoding.DecodeString(standardOnly); err == nil {
			t.Fatalf("URL-safe decoding accepted the standard-only body %q", standardOnly)
		}
	})

	t.Run("a URL-safe-only body is not decodable as standard", func(t *testing.T) {
		if _, err := base64.RawStdEncoding.DecodeString(urlOnly); err == nil {
			t.Fatalf("standard decoding accepted the URL-safe-only body %q", urlOnly)
		}
	})

	t.Run("the helper reports the wrong alphabet instead of re-encoding", func(t *testing.T) {
		recorder := &recordingFataler{}

		tamperBase64BodyWith(recorder, base64.RawURLEncoding, standardOnly, func(decoded []byte) {
			decoded[0] ^= 0x01
		})

		recorder.requireFatal(t, "decoding")
	})
}

// TestTamperBase64BodyRejectsInvalidInput covers the remaining contract checks: a nil
// encoding, a nil mutation, a body that does not decode, and an empty body.
func TestTamperBase64BodyRejectsInvalidInput(t *testing.T) {
	body, _ := standardOnlyBody(t)
	increment := func(decoded []byte) { decoded[0] ^= 0x01 }

	tests := []struct {
		name     string
		encoding *base64.Encoding
		body     string
		mutate   func([]byte)
		want     string
	}{
		{
			name:     "nil encoding",
			encoding: nil,
			body:     body,
			mutate:   increment,
			want:     "requires a base64 encoding",
		},
		{
			name:     "nil mutate",
			encoding: base64.RawStdEncoding,
			body:     body,
			mutate:   nil,
			want:     "requires a mutate function",
		},
		{
			name:     "undecodable body",
			encoding: base64.RawStdEncoding,
			body:     "!!! not base64 !!!",
			mutate:   increment,
			want:     "decoding",
		},
		{
			name:     "empty body",
			encoding: base64.RawStdEncoding,
			body:     "",
			mutate:   increment,
			want:     "nothing to tamper with",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			recorder := &recordingFataler{}

			tamperBase64BodyWith(recorder, tt.encoding, tt.body, tt.mutate)

			recorder.requireFatal(t, tt.want)
		})
	}
}
