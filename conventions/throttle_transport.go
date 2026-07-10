package conventions

import (
	"net/http"
	"sync"
	"time"
)

// ThrottleTransport is an http.RoundTripper that enforces a minimum interval
// between outgoing requests. GitHub's secondary rate limit (abuse detection)
// can trip from a high sequential request rate alone, with no concurrency
// involved — a full sweep's convention checks fire content-API requests
// (Contents, branch protection, languages, etc.) back-to-back with no pacing,
// which is exactly what tripped it in lucas42/lucos_repos#433 (~2,760
// requests fired in a tight sequential burst across ~92 repos). Pacing
// outbound requests keeps well under GitHub's undocumented burst threshold so
// RateLimitTransport rarely needs to intervene at all.
//
// Wrap ThrottleTransport innermost (closest to the network) and put
// RateLimitTransport and CachingTransport around it, so cache hits never pay
// the pacing cost — only requests that actually reach the network do:
//
//	throttle := NewThrottleTransport(http.DefaultTransport, interval)
//	rateLimit := NewRateLimitTransport(throttle)
//	caching := NewCachingTransport(rateLimit)
type ThrottleTransport struct {
	// Wrapped is the underlying transport for actual network requests.
	Wrapped http.RoundTripper

	// MinInterval is the minimum time between the start of one request and
	// the start of the next.
	MinInterval time.Duration

	mu   sync.Mutex
	last time.Time
}

// NewThrottleTransport creates a ThrottleTransport wrapping the given
// transport, pacing requests to no more than one per minInterval. If wrapped
// is nil, http.DefaultTransport is used.
func NewThrottleTransport(wrapped http.RoundTripper, minInterval time.Duration) *ThrottleTransport {
	if wrapped == nil {
		wrapped = http.DefaultTransport
	}
	return &ThrottleTransport{Wrapped: wrapped, MinInterval: minInterval}
}

// RoundTrip implements http.RoundTripper. It reserves the next available
// request slot under the lock (so concurrent callers are correctly
// serialised rather than racing past the wait check together), then sleeps
// outside the lock before making the request.
func (t *ThrottleTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	if t.MinInterval > 0 {
		t.mu.Lock()
		now := time.Now()
		nextAllowed := t.last.Add(t.MinInterval)
		if now.Before(nextAllowed) {
			wait := nextAllowed.Sub(now)
			t.last = nextAllowed
			t.mu.Unlock()
			time.Sleep(wait)
		} else {
			t.last = now
			t.mu.Unlock()
		}
	}
	return t.Wrapped.RoundTrip(req)
}
