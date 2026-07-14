package access

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"strings"
	"time"

	"beecon/internal/organizations"
)

// userTokenMaxLifetime is the longest span a user token's exp − iat may
// cover (PD38a, Phase 2 review carry-forward): the SDK's userTokens.create
// refuses to mint anything longer (packages/sdk/src/resources/userTokens.ts),
// and VerifyUserToken refuses to honor one anyway even if minted by another
// client — iat finally has a job, rather than existing only to be echoed
// back unchecked.
const userTokenMaxLifetime = 24 * time.Hour

// userTokenAlgorithm is the only JWT algorithm VerifyUserToken accepts
// (Flagged Decision 2): a hand-rolled, verify-only HS256 implementation
// rather than a general-purpose JWT library, because Beecon's user tokens
// have exactly one algorithm and a fixed, tiny claim set. A header naming
// any other algorithm — including "none" — is rejected outright.
const userTokenAlgorithm = "HS256"

// userTokenHeader is a user token's JWT header: {"alg":"HS256","kid":"usk_...","typ":"JWT"}.
type userTokenHeader struct {
	Alg string `json:"alg"`
	Kid string `json:"kid"`
}

// userTokenClaims is a user token's JWT payload (PD20): sub is the
// organizations.UserID the SDK minted this token for; iat/exp are Unix
// seconds.
type userTokenClaims struct {
	Sub string `json:"sub"`
	Iat int64  `json:"iat"`
	Exp int64  `json:"exp"`
}

// VerifyUserToken authenticates a presented user-token JWT (PD20) and
// returns the organization and user it identifies. It hard-pins HS256
// (rejecting "none" and every other algorithm), looks up the signing secret
// named by the token's "kid" header via SigningSecretLookup — pre-auth,
// exactly like access.PrefixLookup — decrypts it from the vault, and
// compares the signature in constant time via crypto/hmac.Equal. A missing,
// malformed, tampered, wrong-secret, or expired token is rejected as
// unauthorized (PD5) — the caller never learns which. A vault decrypt
// failure is different in kind from all of those: it is Beecon's own crypto
// layer failing, an infrastructure error rather than a verdict on the
// presented token, so it is returned as-is (PD38b) — mirroring
// ActiveWebhookSecrets (webhook_secret.go) — for authmw to surface as 500,
// not 401.
func (f *Facade) VerifyUserToken(ctx context.Context, token string) (organizations.OrgID, organizations.UserID, error) {
	headerB64, payloadB64, signatureB64, ok := splitUserToken(token)
	if !ok {
		return "", "", ErrUnauthorized()
	}

	header, ok := decodeUserTokenHeader(headerB64)
	if !ok || header.Alg != userTokenAlgorithm {
		return "", "", ErrUnauthorized()
	}

	secretRecord, err := f.signingSecretLookup.FindByKid(ctx, SigningSecretID(header.Kid))
	if err != nil {
		return "", "", err
	}
	if secretRecord == nil {
		return "", "", ErrUnauthorized()
	}
	secret, err := f.vault.Decrypt(secretRecord.EncryptedSecret)
	if err != nil {
		return "", "", err // infra failure, not a verdict on the token — mirrors ActiveWebhookSecrets
	}

	signature, ok := decodeUserTokenSegment(signatureB64)
	if !ok || !validUserTokenSignature(headerB64, payloadB64, signature, secret) {
		return "", "", ErrUnauthorized()
	}

	claims, ok := decodeUserTokenClaims(payloadB64)
	if !ok || f.now().Unix() >= claims.Exp || exceedsUserTokenMaxLifetime(claims) {
		return "", "", ErrUnauthorized()
	}

	return secretRecord.OrgID, organizations.UserID(claims.Sub), nil
}

// exceedsUserTokenMaxLifetime reports whether claims names a lifetime
// (exp − iat) longer than userTokenMaxLifetime allows (PD38a) — rejected
// the same unauthorized way as an already-expired token, so the caller
// never learns which rule a token failed.
func exceedsUserTokenMaxLifetime(claims userTokenClaims) bool {
	return time.Duration(claims.Exp-claims.Iat)*time.Second > userTokenMaxLifetime
}

// splitUserToken splits a "header.payload.signature" compact JWT into its
// three base64url segments. ok is false unless the token has exactly three
// non-empty segments.
func splitUserToken(token string) (header, payload, signature string, ok bool) {
	parts := strings.Split(token, ".")
	if len(parts) != 3 || parts[0] == "" || parts[1] == "" || parts[2] == "" {
		return "", "", "", false
	}
	return parts[0], parts[1], parts[2], true
}

func decodeUserTokenSegment(segment string) ([]byte, bool) {
	decoded, err := base64.RawURLEncoding.DecodeString(segment)
	if err != nil {
		return nil, false
	}
	return decoded, true
}

func decodeUserTokenHeader(headerB64 string) (userTokenHeader, bool) {
	raw, ok := decodeUserTokenSegment(headerB64)
	if !ok {
		return userTokenHeader{}, false
	}
	var header userTokenHeader
	if err := json.Unmarshal(raw, &header); err != nil {
		return userTokenHeader{}, false
	}
	return header, true
}

func decodeUserTokenClaims(payloadB64 string) (userTokenClaims, bool) {
	raw, ok := decodeUserTokenSegment(payloadB64)
	if !ok {
		return userTokenClaims{}, false
	}
	var claims userTokenClaims
	if err := json.Unmarshal(raw, &claims); err != nil {
		return userTokenClaims{}, false
	}
	return claims, true
}

// validUserTokenSignature reports, in constant time, whether signature is
// the HMAC-SHA256 of "headerB64.payloadB64" under secret.
func validUserTokenSignature(headerB64, payloadB64 string, signature []byte, secret string) bool {
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write([]byte(headerB64 + "." + payloadB64))
	return hmac.Equal(signature, mac.Sum(nil))
}
