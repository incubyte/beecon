package delivery

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"fmt"
	"strconv"
	"strings"
	"time"

	"beecon/internal/access"
)

// Standard Webhooks (PD27) header names.
const (
	HeaderWebhookID        = "webhook-id"
	HeaderWebhookTimestamp = "webhook-timestamp"
	HeaderWebhookSignature = "webhook-signature"
)

// SignaturePrefix is the Standard Webhooks version tag prepended to each
// base64 HMAC value in the webhook-signature header.
const SignaturePrefix = "v1,"

// SignedHeaders is the set of Standard Webhooks headers one delivery
// attempt carries (PD27).
type SignedHeaders struct {
	ID        string
	Timestamp string
	Signature string
}

// Sign computes the Standard Webhooks signature (PD27) for one delivery
// attempt: a pure function of the persisted body bytes, the idempotency
// id, the attempt's own timestamp, and every currently active secret (1-2
// during a rotation's overlap window, PD31). It produces one
// HMAC-SHA256 "v1,<base64>" value per secret, space-joined, so a verifier
// holding either secret passes. The signed content is
// "{id}.{timestamp}.{raw body}" — timestamp is unix seconds, matching the
// webhook-timestamp header value exactly. Per the Standard Webhooks spec
// (and every off-the-shelf verifier — standardwebhooks JS/Python/Go,
// svix), the HMAC key is the base64-decoded bytes AFTER stripping the
// "whsec_" prefix, not the secret's raw text — decodeSecretKey below is
// what makes third-party verifiers accept a Beecon delivery (PD27's own
// rationale). A secret that fails to decode (never true for a
// Beecon-minted one) fails Sign loudly rather than silently falling back
// to raw bytes.
func Sign(id EventID, timestamp time.Time, body []byte, secrets []string) (SignedHeaders, error) {
	ts := strconv.FormatInt(timestamp.Unix(), 10)
	content := signedContent(string(id), ts, body)
	values := make([]string, 0, len(secrets))
	for _, secret := range secrets {
		value, err := signatureValue(content, secret)
		if err != nil {
			return SignedHeaders{}, err
		}
		values = append(values, SignaturePrefix+value)
	}
	return SignedHeaders{
		ID:        string(id),
		Timestamp: ts,
		Signature: strings.Join(values, " "),
	}, nil
}

// Headers renders h as the three Standard Webhooks HTTP headers a delivery
// attempt carries.
func (h SignedHeaders) Headers() map[string]string {
	return map[string]string{
		HeaderWebhookID:        h.ID,
		HeaderWebhookTimestamp: h.Timestamp,
		HeaderWebhookSignature: h.Signature,
	}
}

func signedContent(id, timestamp string, body []byte) []byte {
	content := make([]byte, 0, len(id)+len(timestamp)+len(body)+2)
	content = append(content, id...)
	content = append(content, '.')
	content = append(content, timestamp...)
	content = append(content, '.')
	content = append(content, body...)
	return content
}

func signatureValue(content []byte, secret string) (string, error) {
	key, err := decodeSecretKey(secret)
	if err != nil {
		return "", err
	}
	mac := hmac.New(sha256.New, key)
	mac.Write(content)
	return base64.StdEncoding.EncodeToString(mac.Sum(nil)), nil
}

// decodeSecretKey derives the actual HMAC key from a whsec_-prefixed
// secret: strip the prefix, then base64-decode the remainder (PD27, the
// Standard Webhooks convention every off-the-shelf verifier expects — NOT
// the secret's raw text). A Beecon-minted secret always decodes; a
// malformed one fails loudly rather than silently keying with raw bytes.
func decodeSecretKey(secret string) ([]byte, error) {
	trimmed := strings.TrimPrefix(secret, access.WebhookSecretPrefix)
	key, err := base64.StdEncoding.DecodeString(trimmed)
	if err != nil {
		return nil, fmt.Errorf("decode webhook signing secret: %w", err)
	}
	return key, nil
}
