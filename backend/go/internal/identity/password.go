package identity

import (
	"crypto/rand"
	"crypto/subtle"
	"encoding/base64"
	"fmt"
	"strings"

	"golang.org/x/crypto/argon2"
)

// argon2id parameters, fixed for the whole application.
//
// These are recorded inside every encoded hash, so changing them here does not
// invalidate existing hashes: VerifyPassword always uses the parameters that were
// stored alongside the hash.
const (
	argonTime    uint32 = 1
	argonMemory  uint32 = 64 * 1024 // 64 MiB, expressed in KiB as argon2 requires
	argonThreads uint8  = 4
	argonKeyLen  uint32 = 32
	argonSaltLen        = 16
)

// HashPassword derives an argon2id hash for password and returns it as a PHC
// string of the form:
//
//	$argon2id$v=19$m=65536,t=1,p=4$<base64 salt>$<base64 key>
//
// The salt is generated from crypto/rand. The parameters are embedded in the
// string so verification never depends on this package's current constants.
func HashPassword(password string) (string, error) {
	salt := make([]byte, argonSaltLen)
	if _, err := rand.Read(salt); err != nil {
		return "", fmt.Errorf("identity: generate salt: %w", err)
	}

	key := argon2.IDKey([]byte(password), salt, argonTime, argonMemory, argonThreads, argonKeyLen)

	return fmt.Sprintf(
		"$argon2id$v=%d$m=%d,t=%d,p=%d$%s$%s",
		argon2.Version,
		argonMemory,
		argonTime,
		argonThreads,
		base64.RawStdEncoding.EncodeToString(salt),
		base64.RawStdEncoding.EncodeToString(key),
	), nil
}

// VerifyPassword reports whether password matches the encoded PHC hash.
//
// The comparison is constant-time. A malformed encoded hash returns
// ErrInvalidHash, which callers must treat as an internal error rather than as a
// failed login.
func VerifyPassword(encoded, password string) (bool, error) {
	params, salt, want, err := decodePasswordHash(encoded)
	if err != nil {
		return false, err
	}

	got := argon2.IDKey([]byte(password), salt, params.time, params.memory, params.threads, params.keyLen)

	return subtle.ConstantTimeCompare(want, got) == 1, nil
}

// argonParams are the cost parameters read back out of an encoded hash.
type argonParams struct {
	memory  uint32
	time    uint32
	threads uint8
	keyLen  uint32
}

// decodePasswordHash parses a PHC argon2id string into its parameters, salt, and
// derived key. It rejects anything that is not a well-formed argon2id hash at the
// expected version.
func decodePasswordHash(encoded string) (*argonParams, []byte, []byte, error) {
	// The encoded form has six $-separated fields, the first of which is empty.
	fields := strings.Split(encoded, "$")
	if len(fields) != 6 || fields[0] != "" {
		return nil, nil, nil, fmt.Errorf("%w: expected 6 fields", ErrInvalidHash)
	}

	if fields[1] != "argon2id" {
		return nil, nil, nil, fmt.Errorf("%w: unsupported algorithm %q", ErrInvalidHash, fields[1])
	}

	var version int
	if _, err := fmt.Sscanf(fields[2], "v=%d", &version); err != nil {
		return nil, nil, nil, fmt.Errorf("%w: bad version field %q", ErrInvalidHash, fields[2])
	}
	if version != argon2.Version {
		return nil, nil, nil, fmt.Errorf("%w: unsupported argon2 version %d", ErrInvalidHash, version)
	}

	var (
		memory  uint32
		time_   uint32
		threads uint32
	)
	if _, err := fmt.Sscanf(fields[3], "m=%d,t=%d,p=%d", &memory, &time_, &threads); err != nil {
		return nil, nil, nil, fmt.Errorf("%w: bad parameter field %q", ErrInvalidHash, fields[3])
	}
	if memory == 0 || time_ == 0 || threads == 0 || threads > 255 {
		return nil, nil, nil, fmt.Errorf("%w: out-of-range parameters %q", ErrInvalidHash, fields[3])
	}

	salt, err := base64.RawStdEncoding.DecodeString(fields[4])
	if err != nil || len(salt) == 0 {
		return nil, nil, nil, fmt.Errorf("%w: bad salt", ErrInvalidHash)
	}

	key, err := base64.RawStdEncoding.DecodeString(fields[5])
	if err != nil || len(key) == 0 {
		return nil, nil, nil, fmt.Errorf("%w: bad key", ErrInvalidHash)
	}

	return &argonParams{
		memory:  memory,
		time:    time_,
		threads: uint8(threads),
		keyLen:  uint32(len(key)),
	}, salt, key, nil
}
