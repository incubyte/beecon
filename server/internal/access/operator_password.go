package access

import (
	"crypto/rand"
	"crypto/subtle"
	"encoding/base64"
	"fmt"
	"strings"

	"golang.org/x/crypto/argon2"
)

// Argon2id parameters (PD50, FD-D): fixed in code, not config — a wrong KDF
// cost is a security bug, not an operator knob. Current OWASP guidance for
// Argon2id's memory-hard default profile.
const (
	argon2Memory      uint32 = 19456 // KiB (19 MiB)
	argon2Time        uint32 = 2
	argon2Parallelism uint8  = 1
	argon2SaltLength         = 16
	argon2KeyLength   uint32 = 32
)

// minPasswordLength is the shortest password Bootstrap/CreateOperator
// accepts (a sane, documented minimum — not a config knob, mirroring the
// Argon2id params themselves).
const minPasswordLength = 12

// decoySalt is a fixed (never secret) 16-byte salt used only to build
// decoyPasswordHash below — it never protects a real credential.
var decoySalt = []byte("beecon-decoy-salt")[:argon2SaltLength]

// decoyPasswordHash is a fixed Argon2id PHC hash that never corresponds to
// any real operator's password. Login compares an unknown email's attempt
// against this hash (and discards the result) so that a request for an
// email that doesn't exist costs the same Argon2id computation as one that
// does — closing the timing side-channel that would otherwise let an
// attacker learn which emails have accounts.
var decoyPasswordHash = encodePHC("beecon-decoy-password-never-a-real-account", decoySalt)

// hashPassword returns password's Argon2id PHC-encoded hash, using a fresh
// crypto/rand salt (PD50). The PHC string is self-describing (algorithm,
// version, params, salt, hash all in one value), so a future params upgrade
// can rehash-on-login without a schema change.
func hashPassword(password string) (string, error) {
	salt := make([]byte, argon2SaltLength)
	if _, err := rand.Read(salt); err != nil {
		return "", err
	}
	return encodePHC(password, salt), nil
}

// verifyPassword reports whether password matches phc, an Argon2id
// PHC-encoded hash produced by hashPassword (or decoyPasswordHash). An
// unparseable phc is treated as a non-match rather than an error — the
// caller (Login) never needs to distinguish "corrupt hash" from "wrong
// password", both are simply "invalid credentials".
func verifyPassword(password, phc string) bool {
	salt, hash, ok := parsePHC(phc)
	if !ok {
		return false
	}
	candidate := argon2.IDKey([]byte(password), salt, argon2Time, argon2Memory, argon2Parallelism, uint32(len(hash)))
	return subtle.ConstantTimeCompare(candidate, hash) == 1
}

// encodePHC computes password's Argon2id key under salt and renders it as a
// PHC string: $argon2id$v=<version>$m=<memory>,t=<time>,p=<parallelism>$<salt>$<hash>.
func encodePHC(password string, salt []byte) string {
	key := argon2.IDKey([]byte(password), salt, argon2Time, argon2Memory, argon2Parallelism, argon2KeyLength)
	return fmt.Sprintf(
		"$argon2id$v=%d$m=%d,t=%d,p=%d$%s$%s",
		argon2.Version, argon2Memory, argon2Time, argon2Parallelism,
		base64.RawStdEncoding.EncodeToString(salt),
		base64.RawStdEncoding.EncodeToString(key),
	)
}

// parsePHC extracts the salt and hash from a PHC string built by encodePHC.
// The algorithm/version/params fields are only ever produced by this
// package's own fixed constants (PD50: not config), so verifyPassword
// doesn't need to parse and branch on them — just the two variable parts.
func parsePHC(phc string) (salt, hash []byte, ok bool) {
	parts := strings.Split(phc, "$")
	if len(parts) != 6 {
		return nil, nil, false
	}
	salt, err := base64.RawStdEncoding.DecodeString(parts[4])
	if err != nil {
		return nil, nil, false
	}
	hash, err = base64.RawStdEncoding.DecodeString(parts[5])
	if err != nil {
		return nil, nil, false
	}
	return salt, hash, true
}
