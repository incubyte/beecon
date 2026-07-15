package access

import (
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"time"
)

// OperatorSessionID is the session row's own internal id ("opsess_<cuid2>",
// PD58) — never sent to the browser; the opaque token (below) is the only
// session credential a client ever holds.
type OperatorSessionID string

// sessionTokenEntropyBytes mirrors secret.go's secretEntropyBytes (PD51):
// ~32 bytes of crypto/rand entropy behind the opaque session token.
const sessionTokenEntropyBytes = 32

// OperatorSession is the persisted record of one login (PD51): the opaque
// token itself is never stored, only its SHA-256 hash (TokenHash) — a
// database leak never yields a usable session. ExpiresAt is an absolute
// instant (CreatedAt + BEECON_SESSION_TTL), not a sliding renewal (§2.1).
// CSRFToken is a plaintext double-submit value, not a secret (PD52) — Slice
// 3 wires the middleware's comparison against it.
type OperatorSession struct {
	ID         OperatorSessionID
	OperatorID OperatorID
	TokenHash  []byte
	CSRFToken  string
	CreatedAt  time.Time
	ExpiresAt  time.Time
	RevokedAt  *time.Time
}

// IsExpired reports whether the session's absolute TTL has passed as of now.
func (s OperatorSession) IsExpired(now time.Time) bool {
	return !now.Before(s.ExpiresAt)
}

// IsRevoked reports whether the session has been explicitly ended (Slice 2:
// Logout, password change, deactivation). Always false this slice — nothing
// yet sets RevokedAt.
func (s OperatorSession) IsRevoked() bool {
	return s.RevokedAt != nil
}

// generateSessionToken mints a new opaque, crypto/rand session token (PD51)
// — never carries a recognizable prefix (unlike access.SecretPrefix's issued
// keys): it is bearer-only, cookie-borne, and never displayed or typed by an
// operator.
func generateSessionToken() (string, error) {
	buf := make([]byte, sessionTokenEntropyBytes)
	if _, err := rand.Read(buf); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(buf), nil
}

// generateCSRFToken mints the double-submit CSRF value (PD52) — independent
// entropy from the session token itself, so leaking one never reveals the
// other.
func generateCSRFToken() (string, error) {
	buf := make([]byte, sessionTokenEntropyBytes)
	if _, err := rand.Read(buf); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(buf), nil
}

// hashSessionToken hashes the whole opaque token (PD51) — a dedicated
// helper, deliberately not reusing secret.go's hashSecretRemainder, which is
// coupled to access.SecretPrefix's lookup-prefix scheme that session tokens
// don't use (FindByTokenHash looks sessions up by the full hash directly).
func hashSessionToken(token string) []byte {
	sum := sha256.Sum256([]byte(token))
	return sum[:]
}

// sessionTokenHashMatches reports, in constant time, whether hash equals
// candidateHash — an extra defense-in-depth check after FindByTokenHash's
// own indexed exact-match lookup, mirroring secret.go's
// secretMatchesHash/subtle.ConstantTimeCompare convention for the api-key
// secret.
func sessionTokenHashMatches(hash, candidateHash []byte) bool {
	return subtle.ConstantTimeCompare(hash, candidateHash) == 1
}
