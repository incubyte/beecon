package access

import (
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"strings"
	"time"
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

// DefaultOverlapHours is how long a rotated-out secret keeps authenticating
// when Rotate is not given an explicit overlapHours (PD23, ADR-0010).
const DefaultOverlapHours = 24

// overlapHoursOrDefault resolves Rotate's optional overlapHours to a
// concrete number of hours, falling back to DefaultOverlapHours.
func overlapHoursOrDefault(overlapHours *int) int {
	if overlapHours == nil {
		return DefaultOverlapHours
	}
	return *overlapHours
}

// activeSecretOf returns the member of secrets currently able to
// authenticate indefinitely — the one Issue or the most recent Rotate call
// left with ExpiresAt nil. expireLiveSecrets guarantees exactly one such
// member exists whenever secrets is non-empty, so — unlike scanning for it
// by position — this is unaffected by two secrets sharing a CreatedAt (down
// to a driven adapter's timestamp granularity) or by any particular sort
// order a repository happens to return: every ExpiresAt-nil member is
// equally valid to select, since in well-formed data there is only ever one.
// ok is false for an empty slice, or one with no ExpiresAt-nil member.
func activeSecretOf(secrets []ApiKeySecret) (active ApiKeySecret, ok bool) {
	for _, s := range secrets {
		if s.ExpiresAt == nil {
			active, ok = s, true
		}
	}
	return active, ok
}

// activeSecretPrefix returns the lookup prefix of secrets' currently active
// member (see activeSecretOf) — an empty secrets slice, or one with no
// active member, returns "".
func activeSecretPrefix(secrets []ApiKeySecret) string {
	active, ok := activeSecretOf(secrets)
	if !ok {
		return ""
	}
	return active.LookupPrefix
}

// rotationState derives a key's RotatedAt/OverlapExpiresAt (Slice 8, AC5)
// from its secrets: a key only becomes "rotated" once a retired secret
// exists alongside the active one. RotatedAt is the active secret's own
// CreatedAt — the moment the most recent Issue or Rotate call minted it.
// OverlapExpiresAt is the latest ExpiresAt among the retired secrets, not
// merely any of them: a still-live secret from an earlier rotation can be
// force-expired to an already-past instant by a later, faster rotation
// (expireLiveSecrets), leaving more than one retired secret behind, and the
// most recent rotation's genuine, current overlap window is always the
// latest of the two. Deriving "active" and "retired" this way — by each
// secret's own ExpiresAt, never by its position in secrets — sidesteps the
// ambiguity two equal-CreatedAt secrets would otherwise create.
func rotationState(secrets []ApiKeySecret) (rotatedAt, overlapExpiresAt *time.Time) {
	active, ok := activeSecretOf(secrets)
	if !ok {
		return nil, nil
	}
	latestRetiredExpiry, ok := latestExpiresAt(secrets)
	if !ok {
		return nil, nil
	}
	rotatedAtCopy := active.CreatedAt
	overlapExpiresAtCopy := latestRetiredExpiry
	return &rotatedAtCopy, &overlapExpiresAtCopy
}

// latestExpiresAt returns the latest ExpiresAt among secrets that carry one
// at all (the retired members) — ok is false when none do, i.e. a key that
// has never been rotated.
func latestExpiresAt(secrets []ApiKeySecret) (latest time.Time, ok bool) {
	for _, s := range secrets {
		if s.ExpiresAt == nil {
			continue
		}
		if !ok || s.ExpiresAt.After(latest) {
			latest = *s.ExpiresAt
			ok = true
		}
	}
	return latest, ok
}
