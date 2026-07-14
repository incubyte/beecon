//go:build integration

// Package support: FakeReceiver is a scripted httptest.Server standing in
// for a consumer's webhook endpoint (delivery/driven/webhookhttp.Client's
// real POST target) — the crucial_path webhook-channel journey points an
// org's PUT /api/v1/webhook-endpoint URL at this server instead of the real
// internet. It records the raw body and headers of every delivery it
// receives (so a test can assert byte-identical envelopes across retries and
// redeliveries) and answers each call with a scripted, queued response —
// 2xx, a non-2xx status, or TimeoutResponse (hang past the caller's own
// BEECON_DELIVERY_TIMEOUT so the real net/http client gives up).
// VerifyFakeReceiverSignature is an independently written HMAC-SHA256
// recomputation (crypto/hmac/crypto/sha256 directly, not
// delivery.Sign) — a bug in the production signer must not also hide from
// this check.
package support

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"
)

// TimeoutResponse is a sentinel FakeReceiverScript.Responses value: instead
// of answering promptly, the handler sleeps for TimeoutDelay before finally
// writing a response — long enough that a caller bound by a short
// BEECON_DELIVERY_TIMEOUT has already given up (delivery.EndpointCaller.Post
// returns an error, exactly like an unreachable endpoint).
const TimeoutResponse = -1

// defaultTimeoutDelay is FakeReceiverScript.TimeoutDelay's fallback: comfortably
// longer than any BEECON_DELIVERY_TIMEOUT a test configures (typically tens of
// milliseconds), short enough not to slow the test suite down.
const defaultTimeoutDelay = 250 * time.Millisecond

// FakeReceiverDelivery is one POST FakeReceiver observed: the exact raw body
// bytes and headers Beecon sent, in delivery order.
type FakeReceiverDelivery struct {
	Headers    http.Header
	Body       []byte
	ReceivedAt time.Time
}

// FakeReceiverScript configures how FakeReceiver answers each call it
// receives. Responses is a FIFO queue consumed one entry per call; once
// exhausted, every further call succeeds with 200 — so a test only scripts
// the calls it cares about (failures/timeouts) and lets the eventual
// successful delivery fall through to the default.
type FakeReceiverScript struct {
	Responses    []int
	TimeoutDelay time.Duration
}

// FakeReceiver is a running fake webhook receiver plus every delivery it has
// observed.
type FakeReceiver struct {
	URL string

	mu         sync.Mutex
	script     FakeReceiverScript
	deliveries []FakeReceiverDelivery
}

// NewFakeReceiver starts a FakeReceiver server scripted per script, and
// registers it for cleanup with t.
func NewFakeReceiver(t *testing.T, script FakeReceiverScript) *FakeReceiver {
	t.Helper()
	fr := &FakeReceiver{script: script}

	server := httptest.NewServer(http.HandlerFunc(fr.handle))
	t.Cleanup(server.Close)
	fr.URL = server.URL + "/webhook"
	return fr
}

func (fr *FakeReceiver) handle(w http.ResponseWriter, r *http.Request) {
	body, _ := io.ReadAll(r.Body)
	headers := r.Header.Clone()

	fr.mu.Lock()
	fr.deliveries = append(fr.deliveries, FakeReceiverDelivery{Headers: headers, Body: body, ReceivedAt: time.Now()})
	status := fr.nextResponseLocked()
	delay := fr.script.TimeoutDelay
	fr.mu.Unlock()

	if status == TimeoutResponse {
		if delay <= 0 {
			delay = defaultTimeoutDelay
		}
		time.Sleep(delay)
		w.WriteHeader(http.StatusInternalServerError)
		return
	}
	w.WriteHeader(status)
}

// nextResponseLocked pops the next scripted response (must be called with
// fr.mu held); once the queue is empty, every further call defaults to 200.
func (fr *FakeReceiver) nextResponseLocked() int {
	if len(fr.script.Responses) == 0 {
		return http.StatusOK
	}
	next := fr.script.Responses[0]
	fr.script.Responses = fr.script.Responses[1:]
	return next
}

// SetResponses replaces the queue of scripted responses future calls will
// consume — e.g. letting a test that exhausted a run of failures make a
// subsequent redelivery succeed.
func (fr *FakeReceiver) SetResponses(responses []int) {
	fr.mu.Lock()
	defer fr.mu.Unlock()
	fr.script.Responses = responses
}

// Deliveries returns every delivery FakeReceiver has observed so far, in
// order.
func (fr *FakeReceiver) Deliveries() []FakeReceiverDelivery {
	fr.mu.Lock()
	defer fr.mu.Unlock()
	out := make([]FakeReceiverDelivery, len(fr.deliveries))
	copy(out, fr.deliveries)
	return out
}

// CallCount is the number of deliveries FakeReceiver has observed so far.
func (fr *FakeReceiver) CallCount() int {
	fr.mu.Lock()
	defer fr.mu.Unlock()
	return len(fr.deliveries)
}

// LastDelivery returns the most recent delivery FakeReceiver observed, or
// false if none has arrived yet.
func (fr *FakeReceiver) LastDelivery() (FakeReceiverDelivery, bool) {
	fr.mu.Lock()
	defer fr.mu.Unlock()
	if len(fr.deliveries) == 0 {
		return FakeReceiverDelivery{}, false
	}
	return fr.deliveries[len(fr.deliveries)-1], true
}

// VerifyFakeReceiverSignature independently recomputes the Standard
// Webhooks HMAC-SHA256 signature (content "{webhook-id}.{webhook-timestamp}.
// {raw body}", per PD27) for delivery against secret, and reports whether it
// matches one of the space-delimited "v1,<base64>" values in the
// webhook-signature header. Deliberately implemented with crypto/hmac and
// crypto/sha256 directly rather than calling delivery.Sign, so a regression
// in the production signer cannot also hide from this check — the same
// "two independent implementations" discipline the golden vector uses
// between the Go and TypeScript sides (architecture doc section 4). Per the
// Standard Webhooks spec, the HMAC key is the base64-DECODED bytes of
// secret after stripping its "whsec_" prefix — not the secret's raw
// prefixed text; the "whsec_" literal is hardcoded here (rather than
// importing access.WebhookSecretPrefix) deliberately, to keep this
// recomputation genuinely independent of the production package.
func VerifyFakeReceiverSignature(delivery FakeReceiverDelivery, secret string) bool {
	id := delivery.Headers.Get("webhook-id")
	timestamp := delivery.Headers.Get("webhook-timestamp")
	signatureHeader := delivery.Headers.Get("webhook-signature")
	if id == "" || timestamp == "" || signatureHeader == "" {
		return false
	}

	keyMaterial := strings.TrimPrefix(secret, "whsec_")
	key, err := base64.StdEncoding.DecodeString(keyMaterial)
	if err != nil {
		return false
	}

	content := id + "." + timestamp + "." + string(delivery.Body)
	mac := hmac.New(sha256.New, key)
	mac.Write([]byte(content))
	want := "v1," + base64.StdEncoding.EncodeToString(mac.Sum(nil))

	for _, value := range strings.Fields(signatureHeader) {
		if hmac.Equal([]byte(value), []byte(want)) {
			return true
		}
	}
	return false
}
