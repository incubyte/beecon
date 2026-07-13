// ratelimit.go normalizes Graph's and Hubspot's two different rate-limit
// shapes into one signal (PD21, ADR-0009): a plain HTTP 429 from either
// provider, Microsoft Graph's own nested throttle error.code (sometimes
// carried at the top level, sometimes one level deeper under
// error.innerError.code), and Hubspot's RATE_LIMITS error category — whether
// or not the response's HTTP status happens to be 429. retry.go's loop is
// the only caller; the detection itself stays generic Go for exactly these
// two known providers rather than definition-declared (architecture Flagged
// Decision 7).
package execution

import (
	"encoding/json"
	"net/http"
	"strconv"
	"strings"
	"time"
)

// graphThrottleCodes are the error.code values Microsoft Graph uses to
// report a throttled request (PD21) — checked regardless of which of
// error.code or error.innerError.code actually carries it.
var graphThrottleCodes = map[string]bool{
	"TooManyRequests":      true,
	"activityLimitReached": true,
}

// hubspotRateLimitCategory is the value Hubspot's error body carries in its
// "category" field for a rate-limited request (PD21), independent of the
// response's HTTP status code.
const hubspotRateLimitCategory = "RATE_LIMITS"

// rateLimitBody is the subset of both providers' differently-shaped error
// bodies bodyCarriesRateLimitSignal needs: Graph nests its throttle code
// under "error" (sometimes one level deeper still, under "innerError");
// Hubspot carries its rate-limit category as a top-level field.
type rateLimitBody struct {
	Error struct {
		Code       string `json:"code"`
		InnerError struct {
			Code string `json:"code"`
		} `json:"innerError"`
	} `json:"error"`
	Category string `json:"category"`
}

// IsRateLimited normalizes Graph's and Hubspot's rate-limit shapes into one
// signal (PD21, AC1): a plain HTTP 429, Graph's nested throttle error.code,
// or Hubspot's RATE_LIMITS error category.
func IsRateLimited(response ToolCallResponse) bool {
	if response.StatusCode == http.StatusTooManyRequests {
		return true
	}
	return bodyCarriesRateLimitSignal(response.Body)
}

func bodyCarriesRateLimitSignal(body string) bool {
	var parsed rateLimitBody
	if err := json.Unmarshal([]byte(body), &parsed); err != nil {
		return false
	}
	if graphThrottleCodes[parsed.Error.Code] || graphThrottleCodes[parsed.Error.InnerError.Code] {
		return true
	}
	return parsed.Category == hubspotRateLimitCategory
}

// ParseRetryAfter parses an HTTP Retry-After header value (PD21) into a wait
// duration: either a delay in whole seconds (the shape both providers
// actually send) or an HTTP-date, resolved relative to now. ok is false when
// header is empty or not a form Beecon recognizes, so the caller knows to
// fall back to a jittered backoff instead.
func ParseRetryAfter(header string, now time.Time) (delay time.Duration, ok bool) {
	header = strings.TrimSpace(header)
	if header == "" {
		return 0, false
	}
	if seconds, err := strconv.Atoi(header); err == nil {
		if seconds < 0 {
			return 0, false
		}
		return time.Duration(seconds) * time.Second, true
	}
	if when, err := http.ParseTime(header); err == nil {
		if remaining := when.Sub(now); remaining > 0 {
			return remaining, true
		}
		return 0, true
	}
	return 0, false
}
