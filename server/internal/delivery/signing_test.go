package delivery

import (
	"strings"
	"testing"
	"time"
)

// --- Golden vector (architecture doc section 4): Go signs -> committed
// constant -> the TypeScript SDK's webhook-verify test (Slice 6) asserts
// beecon.webhooks.verify accepts the identical delivery. The vector's own
// values are deliberately readable, not production ids: goldenEventID reads
// as an obvious fixture; goldenTimestampUnix (1700000000) is the well-known
// round Unix instant 2023-11-14T22:13:20Z; goldenBody is the smallest
// non-trivial JSON object. goldenSignature was computed independently
// (outside this codebase, cross-checked via both Node's crypto.createHmac
// and a standalone Go program using crypto/hmac/crypto/sha256 directly, not
// this package's own Sign) using the Standard Webhooks key-derivation
// scheme (PD27, and every off-the-shelf verifier — standardwebhooks
// JS/Python/Go, svix): the HMAC key is the base64-DECODED bytes of
// goldenSecret AFTER stripping its "whsec_" prefix — never the secret's raw
// prefixed text. A prior version of this vector keyed with the raw text and
// was caught by the sdd-verifier's own independent re-derivation; do not
// "fix" a future failure here by recomputing the constant from Sign's own
// output — recompute it externally, the same way, and cross-check.
const (
	goldenEventID       = "evt_golden00000000000000001"
	goldenTimestampUnix = 1700000000
	goldenBody          = `{"test":"data"}`
	goldenSecret        = "whsec_MfKQ9r8GKYqrTwjUPD8ILPZIo2LaLaSw6XYz1Q9F8mI="
	goldenSignature     = "v1,VTFU5mQgmJpE/NnJF4dLKTpfyU53iqJXnn77YvZ9QDw="
)

// TestSign_GoldenVector_ReproducesTheCommittedSignature is the mandated
// interop proof (architecture doc section 4): a fixed id/timestamp/body/
// secret must always reproduce the exact committed signature. If this ever
// fails after a deliberate change to the signing scheme, the committed
// constant here AND packages/sdk/test/webhook-verify.test.ts's identical
// vector must be updated together — that is the point of a shared golden
// vector: two independent implementations of the same misreading cannot
// both pass.
func TestSign_GoldenVector_ReproducesTheCommittedSignature(t *testing.T) {
	timestamp := time.Unix(goldenTimestampUnix, 0).UTC()

	got, err := Sign(EventID(goldenEventID), timestamp, []byte(goldenBody), []string{goldenSecret})

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got.ID != goldenEventID {
		t.Errorf("ID = %q, want %q", got.ID, goldenEventID)
	}
	if got.Timestamp != "1700000000" {
		t.Errorf("Timestamp = %q, want %q", got.Timestamp, "1700000000")
	}
	if got.Signature != goldenSignature {
		t.Errorf("Signature = %q, want the committed golden vector %q", got.Signature, goldenSignature)
	}
}

func TestSign_TimestampHeaderIsUnixSecondsNotNanoseconds(t *testing.T) {
	timestamp := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)

	got, err := Sign("evt_1", timestamp, []byte("{}"), []string{goldenSecret})

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	want := "1767225600" // time.Date(2026,1,1,0,0,0,0,UTC).Unix()
	if got.Timestamp != want {
		t.Errorf("Timestamp = %q, want %q", got.Timestamp, want)
	}
}

// TestSign_ChangingTheBodyChangesTheSignature is a basic tamper-sensitivity
// check on the Go signer itself (the SDK side of tamper-detection is Slice
// 6's job) — the same id/timestamp/secret with a different body must never
// reuse the golden vector's signature.
func TestSign_ChangingTheBodyChangesTheSignature(t *testing.T) {
	timestamp := time.Unix(goldenTimestampUnix, 0).UTC()

	got, err := Sign(EventID(goldenEventID), timestamp, []byte(`{"test":"tampered"}`), []string{goldenSecret})

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got.Signature == goldenSignature {
		t.Fatal("a different body produced the same signature as the golden vector — HMAC content must include the raw body")
	}
}

// anotherValidSecret and secondRotationSecret are both valid whsec_-prefixed,
// base64-decodable secrets distinct from goldenSecret (base64 of readable
// filler text) — used wherever a test needs "some other secret," not the
// golden vector's own value.
const anotherValidSecret = "whsec_YS10b3RhbGx5LWRpZmZlcmVudC1zZWNyZXQtdmFsdWU="
const secondRotationSecret = "whsec_YS1zZWNvbmQtcm90YXRpb24tc2VjcmV0LXZhbHVlISE="

func TestSign_ChangingTheSecretChangesTheSignature(t *testing.T) {
	timestamp := time.Unix(goldenTimestampUnix, 0).UTC()

	got, err := Sign(EventID(goldenEventID), timestamp, []byte(goldenBody), []string{anotherValidSecret})

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got.Signature == goldenSignature {
		t.Fatal("a different secret produced the same signature as the golden vector")
	}
}

// TestSign_EmitsOneV1ValuePerSecretSpaceJoined pins PD31's rotation-overlap
// shape: DispatchOnce hands Sign every currently active secret (1-2), and a
// verifier holding either one must be able to pick out its own signature
// from the space-delimited header value.
func TestSign_EmitsOneV1ValuePerSecretSpaceJoined(t *testing.T) {
	timestamp := time.Unix(goldenTimestampUnix, 0).UTC()

	got, err := Sign(EventID(goldenEventID), timestamp, []byte(goldenBody), []string{goldenSecret, secondRotationSecret})

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	values := strings.Split(got.Signature, " ")
	if len(values) != 2 {
		t.Fatalf("Signature = %q, want exactly 2 space-delimited values for 2 active secrets", got.Signature)
	}
	if values[0] != goldenSignature {
		t.Errorf("first value = %q, want the golden vector's own signature %q (first secret first)", values[0], goldenSignature)
	}
	for i, v := range values {
		if !strings.HasPrefix(v, SignaturePrefix) {
			t.Errorf("value[%d] = %q, want it to start with %q", i, v, SignaturePrefix)
		}
	}
	if values[0] == values[1] {
		t.Error("two different secrets produced the same signature value")
	}
}

func TestSign_WithNoActiveSecretsProducesAnEmptySignatureHeader(t *testing.T) {
	timestamp := time.Unix(goldenTimestampUnix, 0).UTC()

	got, err := Sign(EventID(goldenEventID), timestamp, []byte(goldenBody), nil)

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got.Signature != "" {
		t.Errorf("Signature = %q, want empty when no secret is active", got.Signature)
	}
}

// TestSign_ANonBase64DecodableSecretReturnsAnErrorRatherThanSigningWithRawBytes
// pins the loud-failure half of the fix: a whsec_ value whose remainder
// isn't valid base64 (never true for a Beecon-minted secret, but Sign must
// not silently fall back to keying with the raw prefixed text) makes Sign
// return an error and no SignedHeaders at all.
func TestSign_ANonBase64DecodableSecretReturnsAnErrorRatherThanSigningWithRawBytes(t *testing.T) {
	timestamp := time.Unix(goldenTimestampUnix, 0).UTC()

	_, err := Sign(EventID(goldenEventID), timestamp, []byte(goldenBody), []string{"whsec_not-valid-base64!!!"})

	if err == nil {
		t.Fatal("expected an error for a non-base64-decodable secret, got nil")
	}
}

// TestSign_ANonBase64DecodableSecretAmongOthersStillFailsTheWholeCall pins
// that Sign doesn't partially succeed: if any active secret fails to decode,
// the whole attempt errors (dispatchOne's signAndPost then treats it as one
// failed delivery attempt, not a partially-signed one).
func TestSign_ANonBase64DecodableSecretAmongOthersStillFailsTheWholeCall(t *testing.T) {
	timestamp := time.Unix(goldenTimestampUnix, 0).UTC()

	_, err := Sign(EventID(goldenEventID), timestamp, []byte(goldenBody), []string{goldenSecret, "whsec_not-valid-base64!!!"})

	if err == nil {
		t.Fatal("expected an error when any active secret fails to decode, got nil")
	}
}

func TestSignedHeaders_HeadersRendersAllThreeStandardWebhooksHeaderNames(t *testing.T) {
	timestamp := time.Unix(goldenTimestampUnix, 0).UTC()
	signed, err := Sign(EventID(goldenEventID), timestamp, []byte(goldenBody), []string{goldenSecret})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	headers := signed.Headers()

	if headers[HeaderWebhookID] != goldenEventID {
		t.Errorf("headers[%q] = %q, want %q", HeaderWebhookID, headers[HeaderWebhookID], goldenEventID)
	}
	if headers[HeaderWebhookTimestamp] != "1700000000" {
		t.Errorf("headers[%q] = %q, want %q", HeaderWebhookTimestamp, headers[HeaderWebhookTimestamp], "1700000000")
	}
	if headers[HeaderWebhookSignature] != goldenSignature {
		t.Errorf("headers[%q] = %q, want %q", HeaderWebhookSignature, headers[HeaderWebhookSignature], goldenSignature)
	}
	if len(headers) != 3 {
		t.Errorf("len(headers) = %d, want exactly 3 (webhook-id, webhook-timestamp, webhook-signature)", len(headers))
	}
}
