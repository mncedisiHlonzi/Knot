package testutil

import (
	"bytes"
	"encoding/base64"
	"strings"
	"testing"
)

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

// requireFatal asserts that exactly one fatal message was recorded and that it
// mentions want.
func requireFatal(t *testing.T, recorder *RecordingFataler, want string) {
	t.Helper()

	messages := recorder.Messages()
	if len(messages) != 1 {
		t.Fatalf("recorded %d fatal messages %q, want exactly 1", len(messages), messages)
	}
	if !strings.Contains(messages[0], want) {
		t.Errorf("fatal message = %q, want it to contain %q", messages[0], want)
	}
}

// TestTamperBase64BodyRoundTripStandard covers the success path end to end.
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

	tampered, before, after := TamperBase64Body(t, encoding, body, func(decoded []byte) {
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
// with its own encoding, for bodies only one alphabet can express and for bodies whose
// characters are common to both.
func TestTamperBase64BodyBothAlphabetsRoundTrip(t *testing.T) {
	standardOnly, _ := standardOnlyBody(t)
	urlOnly, _ := urlOnlyBody(t)

	tests := []struct {
		name     string
		encoding *base64.Encoding
		body     string
	}{
		{"standard alphabet with a standard-only body", base64.RawStdEncoding, standardOnly},
		{"URL-safe alphabet with a URL-safe-only body", base64.RawURLEncoding, urlOnly},
		{"standard alphabet with a common-character body", base64.RawStdEncoding, base64.RawStdEncoding.EncodeToString([]byte("common"))},
		{"URL-safe alphabet with a common-character body", base64.RawURLEncoding, base64.RawURLEncoding.EncodeToString([]byte("common"))},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Mutating the LAST decoded byte is deliberate: it is where the
			// padding-bit trap lived.
			tampered, _, after := TamperBase64Body(t, tt.encoding, tt.body, func(decoded []byte) {
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
// helper exists to close: a mutation that changes nothing must be reported.
func TestTamperBase64BodyVacuityGuardFires(t *testing.T) {
	body, _ := standardOnlyBody(t)

	t.Run("mutation that does nothing", func(t *testing.T) {
		recorder := &RecordingFataler{}

		TamperBase64BodyWith(recorder, base64.RawStdEncoding, body, func([]byte) {})

		requireFatal(t, recorder, "vacuity trap")
	})

	t.Run("mutation that cancels itself out", func(t *testing.T) {
		recorder := &RecordingFataler{}

		TamperBase64BodyWith(recorder, base64.RawStdEncoding, body, func(decoded []byte) {
			decoded[0] ^= 0x01
			decoded[0] ^= 0x01
		})

		requireFatal(t, recorder, "vacuity trap")
	})
}

// TestTamperBase64BodyRejectsWrongEncoding documents the alphabet-inference defect
// directly: a body in one alphabet must not be decoded with the other, and the helper
// must report that rather than re-encoding it in a way its consumer cannot read.
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
		recorder := &RecordingFataler{}

		TamperBase64BodyWith(recorder, base64.RawURLEncoding, standardOnly, func(decoded []byte) {
			decoded[0] ^= 0x01
		})

		requireFatal(t, recorder, "decoding")
	})
}

// TestTamperBase64BodyRejectsInvalidInput covers the remaining contract checks.
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
		{"nil encoding", nil, body, increment, "requires a base64 encoding"},
		{"nil mutate", base64.RawStdEncoding, body, nil, "requires a mutate function"},
		{"undecodable body", base64.RawStdEncoding, "!!! not base64 !!!", increment, "decoding"},
		{"empty body", base64.RawStdEncoding, "", increment, "nothing to tamper with"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			recorder := &RecordingFataler{}

			TamperBase64BodyWith(recorder, tt.encoding, tt.body, tt.mutate)

			requireFatal(t, recorder, tt.want)
		})
	}
}
