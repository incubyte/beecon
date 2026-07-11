package access

import (
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"strings"
)

// SecretPrefix makes an issued secret recognizable in config and logs (PD3).
const SecretPrefix = "beecon_sk_"

// secretEntropyBytes is the amount of random data behind the "<random>" part
// of a secret, ~32 bytes of entropy per PD3.
const secretEntropyBytes = 32

// LookupPrefixLength is how many characters of the full secret (including
// SecretPrefix) are stored in plaintext to narrow a Verify lookup down to
// candidate keys, per PD3.
const LookupPrefixLength = 12

// generateSecret mints a new "beecon_sk_<random>" secret using
// crypto/rand.
func generateSecret() (string, error) {
	buf := make([]byte, secretEntropyBytes)
	if _, err := rand.Read(buf); err != nil {
		return "", err
	}
	return SecretPrefix + base64.RawURLEncoding.EncodeToString(buf), nil
}

// hasSecretPrefix reports whether secret is shaped like an issued key at all
// (carries SecretPrefix and is long enough to have a lookup prefix).
func hasSecretPrefix(secret string) bool {
	return strings.HasPrefix(secret, SecretPrefix) && len(secret) >= LookupPrefixLength
}

// lookupPrefix returns the plaintext-stored portion of secret.
func lookupPrefix(secret string) string {
	return secret[:LookupPrefixLength]
}

// hashSecretRemainder hashes the portion of secret after its lookup prefix —
// the part that is never stored in plaintext.
func hashSecretRemainder(secret string) []byte {
	sum := sha256.Sum256([]byte(secret[LookupPrefixLength:]))
	return sum[:]
}

// secretMatchesHash reports, in constant time, whether secret's remainder
// hashes to hash.
func secretMatchesHash(secret string, hash []byte) bool {
	candidate := hashSecretRemainder(secret)
	return subtle.ConstantTimeCompare(candidate, hash) == 1
}
