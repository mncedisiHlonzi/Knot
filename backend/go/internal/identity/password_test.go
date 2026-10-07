package identity

import (
	"errors"
	"strings"
	"testing"
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

func TestVerifyPasswordRejectsTamperedHash(t *testing.T) {
	hash, err := HashPassword("correct horse battery staple")
	if err != nil {
		t.Fatalf("HashPassword() error = %v, want nil", err)
	}

	// Replace the final base64 character with a different valid one. The derived
	// key no longer matches the password.
	last := hash[len(hash)-1]
	replacement := byte('A')
	if last == 'A' {
		replacement = 'B'
	}
	tampered := hash[:len(hash)-1] + string(replacement)

	ok, err := VerifyPassword(tampered, "correct horse battery staple")
	if err != nil {
		// A tampered tail may no longer decode; that is a rejection too.
		return
	}
	if ok {
		t.Error("VerifyPassword() = true, want false for a tampered hash")
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
