package identity

import (
	"encoding/base64"
	"errors"
	"strings"
	"testing"

	"github.com/knot/backend/internal/testutil"
)

func TestHashPasswordProducesParseablePHCString(t *testing.T) {
	hash, err := HashPassword("correct horse battery staple")
	if err != nil {
		t.Fatalf("HashPassword() error = %v, want nil", err)
	}

	if !strings.HasPrefix(hash, "$argon2id$") {
		t.Errorf("hash = %q, want the $argon2id$ prefix", hash)
	}

	params, salt, key, err := decodePasswordHash(hash)
	if err != nil {
		t.Fatalf("decodePasswordHash() error = %v, want nil", err)
	}

	if params.memory != argonMemory {
		t.Errorf("memory = %d, want %d", params.memory, argonMemory)
	}
	if params.time != argonTime {
		t.Errorf("time = %d, want %d", params.time, argonTime)
	}
	if params.threads != argonThreads {
		t.Errorf("threads = %d, want %d", params.threads, argonThreads)
	}
	if params.keyLen != argonKeyLen {
		t.Errorf("keyLen = %d, want %d", params.keyLen, argonKeyLen)
	}
	if len(salt) != argonSaltLen {
		t.Errorf("salt length = %d, want %d", len(salt), argonSaltLen)
	}
	if len(key) != int(argonKeyLen) {
		t.Errorf("key length = %d, want %d", len(key), argonKeyLen)
	}
}

func TestHashPasswordNeverStoresPlaintext(t *testing.T) {
	const password = "correct horse battery staple"

	hash, err := HashPassword(password)
	if err != nil {
		t.Fatalf("HashPassword() error = %v, want nil", err)
	}
	if strings.Contains(hash, password) {
		t.Errorf("hash contains the plaintext password")
	}
}

func TestHashPasswordUsesFreshSalt(t *testing.T) {
	const password = "correct horse battery staple"

	first, err := HashPassword(password)
	if err != nil {
		t.Fatalf("first HashPassword() error = %v, want nil", err)
	}
	second, err := HashPassword(password)
	if err != nil {
		t.Fatalf("second HashPassword() error = %v, want nil", err)
	}

	if first == second {
		t.Error("two hashes of the same password are identical, want distinct salts")
	}
}

func TestVerifyPasswordAcceptsCorrectPassword(t *testing.T) {
	const password = "correct horse battery staple"

	hash, err := HashPassword(password)
	if err != nil {
		t.Fatalf("HashPassword() error = %v, want nil", err)
	}

	ok, err := VerifyPassword(hash, password)
	if err != nil {
		t.Fatalf("VerifyPassword() error = %v, want nil", err)
	}
	if !ok {
		t.Error("VerifyPassword() = false, want true for the correct password")
	}
}

func TestVerifyPasswordRejectsWrongPassword(t *testing.T) {
	hash, err := HashPassword("correct horse battery staple")
	if err != nil {
		t.Fatalf("HashPassword() error = %v, want nil", err)
	}

	ok, err := VerifyPassword(hash, "incorrect horse battery staple")
	if err != nil {
		t.Fatalf("VerifyPassword() error = %v, want nil", err)
	}
	if ok {
		t.Error("VerifyPassword() = true, want false for a wrong password")
	}
}

// TestVerifyPasswordRejectsTamperedHash flips a bit in the decoded derived key and
// checks that verification rejects it.
//
// The key is tampered with through tamperBase64Body, which mutates decoded bytes and
// refuses to let the mutation be a silent no-op. An earlier version of this test
// edited the final character of the encoded key instead, which left the key
// byte-identical roughly one run in sixteen.
func TestVerifyPasswordRejectsTamperedHash(t *testing.T) {
	const password = "correct horse battery staple"

	hash, err := HashPassword(password)
	if err != nil {
		t.Fatalf("HashPassword() error = %v, want nil", err)
	}

	// A PHC string is $argon2id$v=...$m=...$<salt>$<key>. Only the key is tampered
	// with; the parameters and salt are left intact so the hash stays well-formed.
	fields := strings.Split(hash, "$")
	if len(fields) != 6 {
		t.Fatalf("encoded hash has %d fields, want 6: %q", len(fields), hash)
	}

	fields[5], _, _ = testutil.TamperBase64Body(t, base64.RawStdEncoding, fields[5], func(key []byte) {
		key[0] ^= 0x01
	})
	tampered := strings.Join(fields, "$")

	// The tampered hash is still well-formed, so verification has to fail on the
	// constant-time comparison rather than on parsing.
	ok, err := VerifyPassword(tampered, password)
	if err != nil {
		t.Fatalf("VerifyPassword() error = %v, want nil for a well-formed hash with the wrong key", err)
	}
	if ok {
		t.Error("VerifyPassword() = true, want false for a tampered key")
	}
}

func TestVerifyPasswordRejectsMalformedHash(t *testing.T) {
	tests := []struct {
		name  string
		shown string
	}{
		{name: "empty", shown: ""},
		{name: "not a hash", shown: "not-a-hash"},
		{name: "wrong algorithm", shown: "$argon2i$v=19$m=65536,t=1,p=4$c2FsdA$a2V5"},
		{name: "wrong version", shown: "$argon2id$v=99$m=65536,t=1,p=4$c2FsdA$a2V5"},
		{name: "too few fields", shown: "$argon2id$v=19$m=65536,t=1,p=4$c2FsdA"},
		{name: "bad parameters", shown: "$argon2id$v=19$m=x,t=y,p=z$c2FsdA$a2V5"},
		{name: "bad salt", shown: "$argon2id$v=19$m=65536,t=1,p=4$!!!$a2V5"},
		{name: "bad key", shown: "$argon2id$v=19$m=65536,t=1,p=4$c2FsdA$!!!"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ok, err := VerifyPassword(tt.shown, "whatever")
			if ok {
				t.Error("VerifyPassword() = true, want false for a malformed hash")
			}
			if !errors.Is(err, ErrInvalidHash) {
				t.Errorf("VerifyPassword() error = %v, want ErrInvalidHash", err)
			}
		})
	}
}
