// Package access_test (see facade_test.go for the external-package
// rationale). This file exercises VerifyUserToken's tamper/wrong-secret/
// expired matrix (PD20, Flagged Decision 2): tokens are minted right here
// with the exact same HS256 construction the SDK will use in Slice 9 — a
// compact "header.payload.signature" JWT, each segment base64url (no
// padding) encoded, signed over "header.payload" with HMAC-SHA256 under the
// raw signing secret — rather than importing any JWT library, proving the
// server's hand-rolled verifier actually interoperates with that
// construction rather than only with itself. Every test here builds its own
// facade with an explicit fixed clock (rather than newFacade()'s default),
// so a token's iat/exp claims and VerifyUserToken's own notion of "now"
// agree deterministically instead of racing the real wall clock.
package access_test

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"testing"
	"time"

	"beecon/internal/access"
	memory "beecon/internal/access/driven/memory"
	"beecon/internal/organizations"
)

const testUserID = organizations.UserID("user_ada")

var userTokenTestNow = time.Date(2026, 6, 1, 12, 0, 0, 0, time.UTC)

func newFacadeWithFixedClock(now time.Time) *access.Facade {
	return memory.NewFacadeWithOverrides(memory.Overrides{Now: func() time.Time { return now }})
}

func b64url(raw []byte) string {
	return base64.RawURLEncoding.EncodeToString(raw)
}

// mintUserToken builds a compact HS256 JWT exactly as VerifyUserToken expects
// to parse one: {"alg":alg,"kid":kid}.{"sub":sub,"iat":iat,"exp":exp} signed
// with secret. Passing an alg other than "HS256" (including "none") lets
// tests prove the hard pin actually rejects everything else.
func mintUserToken(t *testing.T, secret string, kid access.SigningSecretID, alg string, sub organizations.UserID, iat, exp int64) string {
	t.Helper()
	header, err := json.Marshal(map[string]string{"alg": alg, "kid": string(kid), "typ": "JWT"})
	if err != nil {
		t.Fatalf("marshal header: %v", err)
	}
	payload, err := json.Marshal(map[string]any{"sub": string(sub), "iat": iat, "exp": exp})
	if err != nil {
		t.Fatalf("marshal payload: %v", err)
	}
	signingInput := b64url(header) + "." + b64url(payload)
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write([]byte(signingInput))
	return signingInput + "." + b64url(mac.Sum(nil))
}

// issueSigningSecretForUserToken issues a real signing secret through the
// facade so the resulting fixture matches how VerifyUserToken looks up and
// decrypts a secret in production, rather than reaching into unexported
// storage internals.
func issueSigningSecretForUserToken(t *testing.T, f *access.Facade, org organizations.OrgID) access.IssuedSigningSecret {
	t.Helper()
	issued, err := f.IssueSigningSecret(context.Background(), org)
	if err != nil {
		t.Fatalf("IssueSigningSecret: %v", err)
	}
	return issued
}

func TestVerifyUserToken_AcceptsAValidTokenAndReturnsTheOrgAndUserItNames(t *testing.T) {
	f := newFacadeWithFixedClock(userTokenTestNow)
	secret := issueSigningSecretForUserToken(t, f, orgA)
	token := mintUserToken(t, secret.Secret, secret.ID, "HS256", testUserID, userTokenTestNow.Unix(), userTokenTestNow.Add(2*time.Hour).Unix())

	gotOrg, gotUser, err := f.VerifyUserToken(context.Background(), token)

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if gotOrg != orgA {
		t.Errorf("org = %q, want %q", gotOrg, orgA)
	}
	if gotUser != testUserID {
		t.Errorf("user = %q, want %q", gotUser, testUserID)
	}
}

func TestVerifyUserToken_RejectsAnExpiredToken(t *testing.T) {
	f := newFacadeWithFixedClock(userTokenTestNow)
	secret := issueSigningSecretForUserToken(t, f, orgA)
	token := mintUserToken(t, secret.Secret, secret.ID, "HS256", testUserID, userTokenTestNow.Add(-3*time.Hour).Unix(), userTokenTestNow.Add(-1*time.Hour).Unix())

	_, _, err := f.VerifyUserToken(context.Background(), token)

	assertDomainError(t, err, "unauthorized", 401)
}

func TestVerifyUserToken_RejectsATokenWithAnExpiryOfExactlyNow(t *testing.T) {
	f := newFacadeWithFixedClock(userTokenTestNow)
	secret := issueSigningSecretForUserToken(t, f, orgA)
	// exp == now is already expired ("exp" is the instant after which the
	// token must no longer verify, so the boundary itself must reject).
	token := mintUserToken(t, secret.Secret, secret.ID, "HS256", testUserID, userTokenTestNow.Add(-2*time.Hour).Unix(), userTokenTestNow.Unix())

	_, _, err := f.VerifyUserToken(context.Background(), token)

	assertDomainError(t, err, "unauthorized", 401)
}

func TestVerifyUserToken_RejectsATokenSignedWithTheWrongSecret(t *testing.T) {
	f := newFacadeWithFixedClock(userTokenTestNow)
	secret := issueSigningSecretForUserToken(t, f, orgA)
	token := mintUserToken(t, "totally-different-secret-value", secret.ID, "HS256", testUserID, userTokenTestNow.Unix(), userTokenTestNow.Add(2*time.Hour).Unix())

	_, _, err := f.VerifyUserToken(context.Background(), token)

	assertDomainError(t, err, "unauthorized", 401)
}

func TestVerifyUserToken_RejectsATokenWithATamperedPayload(t *testing.T) {
	f := newFacadeWithFixedClock(userTokenTestNow)
	secret := issueSigningSecretForUserToken(t, f, orgA)
	token := mintUserToken(t, secret.Secret, secret.ID, "HS256", testUserID, userTokenTestNow.Unix(), userTokenTestNow.Add(2*time.Hour).Unix())

	// Swap in a different (validly encoded) payload claiming a different
	// user, without re-signing — the original signature must no longer
	// verify against the tampered payload.
	forgedPayload, err := json.Marshal(map[string]any{"sub": "user_someone_else", "iat": userTokenTestNow.Unix(), "exp": userTokenTestNow.Add(2 * time.Hour).Unix()})
	if err != nil {
		t.Fatalf("marshal forged payload: %v", err)
	}
	segments := splitCompactJWT(t, token)
	tampered := segments[0] + "." + b64url(forgedPayload) + "." + segments[2]

	_, _, err = f.VerifyUserToken(context.Background(), tampered)

	assertDomainError(t, err, "unauthorized", 401)
}

func TestVerifyUserToken_RejectsATokenWithAlgNone(t *testing.T) {
	f := newFacadeWithFixedClock(userTokenTestNow)
	secret := issueSigningSecretForUserToken(t, f, orgA)
	token := mintUserToken(t, secret.Secret, secret.ID, "none", testUserID, userTokenTestNow.Unix(), userTokenTestNow.Add(2*time.Hour).Unix())

	_, _, err := f.VerifyUserToken(context.Background(), token)

	assertDomainError(t, err, "unauthorized", 401)
}

func TestVerifyUserToken_RejectsATokenWithAnUnsupportedNonNoneAlgorithm(t *testing.T) {
	f := newFacadeWithFixedClock(userTokenTestNow)
	secret := issueSigningSecretForUserToken(t, f, orgA)
	token := mintUserToken(t, secret.Secret, secret.ID, "HS512", testUserID, userTokenTestNow.Unix(), userTokenTestNow.Add(2*time.Hour).Unix())

	_, _, err := f.VerifyUserToken(context.Background(), token)

	assertDomainError(t, err, "unauthorized", 401)
}

func TestVerifyUserToken_RejectsATokenNamingAnUnknownSigningSecretID(t *testing.T) {
	f := newFacadeWithFixedClock(userTokenTestNow)
	token := mintUserToken(t, "any-secret", access.SigningSecretID("usk_never_issued"), "HS256", testUserID, userTokenTestNow.Unix(), userTokenTestNow.Add(2*time.Hour).Unix())

	_, _, err := f.VerifyUserToken(context.Background(), token)

	assertDomainError(t, err, "unauthorized", 401)
}

func TestVerifyUserToken_RejectsAMalformedTokenThatIsNotThreeSegments(t *testing.T) {
	f := newFacadeWithFixedClock(userTokenTestNow)

	_, _, err := f.VerifyUserToken(context.Background(), "not-a-jwt-at-all")

	assertDomainError(t, err, "unauthorized", 401)
}

func TestVerifyUserToken_RejectsAnEmptyToken(t *testing.T) {
	f := newFacadeWithFixedClock(userTokenTestNow)

	_, _, err := f.VerifyUserToken(context.Background(), "")

	assertDomainError(t, err, "unauthorized", 401)
}

// splitCompactJWT is a tiny test-only helper (distinct from the production
// splitUserToken, which is unexported) so TestVerifyUserToken_...Tampered can
// reassemble a token from its three segments.
func splitCompactJWT(t *testing.T, token string) [3]string {
	t.Helper()
	var out [3]string
	start := 0
	part := 0
	for i := 0; i < len(token); i++ {
		if token[i] == '.' {
			out[part] = token[start:i]
			part++
			start = i + 1
		}
	}
	out[2] = token[start:]
	if part != 2 {
		t.Fatalf("token %q is not a three-segment compact JWT", token)
	}
	return out
}
