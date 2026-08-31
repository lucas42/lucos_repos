package conventions

import (
	"net/http"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

// TestThrottleTransport_PacesSequentialRequests verifies that consecutive
// requests are spaced at least MinInterval apart, in aggregate across the
// whole run.
//
// This asserts on total elapsed (first call to last call) rather than on
// each pairwise gap. Timestamps are taken inside the mock, after dispatch,
// so a pairwise gap carries per-call jitter (mutex/allocation cost, or —
// after a sleep — scheduler wake-up delay) on both ends and can measure
// fractionally under MinInterval even though the transport paced the
// underlying slot reservations exactly (lucas42/lucos_repos#492). Summing
// telescopes that jitter away: only the first call's dispatch-only cost and
// the last call's post-sleep wake cost survive, and the latter is never
// smaller, since time.Sleep does not return early.
func TestThrottleTransport_PacesSequentialRequests(t *testing.T) {
	var callTimes []time.Time
	var mu sync.Mutex
	mock := roundTripFunc(func(_ *http.Request) (*http.Response, error) {
		mu.Lock()
		callTimes = append(callTimes, time.Now())
		mu.Unlock()
		return makeResp(http.StatusOK, `{}`, nil), nil
	})

	const interval = 20 * time.Millisecond
	const n = 3
	transport := NewThrottleTransport(mock, interval)

	for i := 0; i < n; i++ {
		req, _ := http.NewRequest("GET", "http://example.com/test", nil)
		if _, err := transport.RoundTrip(req); err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
	}

	if len(callTimes) != n {
		t.Fatalf("expected %d calls, got %d", n, len(callTimes))
	}

	elapsed := callTimes[n-1].Sub(callTimes[0])
	minExpected := time.Duration(n-1) * interval
	if elapsed < minExpected {
		t.Errorf("expected at least %s elapsed across %d paced requests, got %s", minExpected, n, elapsed)
	}
}

// TestThrottleTransport_ZeroInterval_NoDelay verifies that a zero MinInterval
// disables throttling entirely (used by tests that don't want to slow down).
func TestThrottleTransport_ZeroInterval_NoDelay(t *testing.T) {
	mock := roundTripFunc(func(_ *http.Request) (*http.Response, error) {
		return makeResp(http.StatusOK, `{}`, nil), nil
	})
	transport := NewThrottleTransport(mock, 0)

	start := time.Now()
	for i := 0; i < 5; i++ {
		req, _ := http.NewRequest("GET", "http://example.com/test", nil)
		if _, err := transport.RoundTrip(req); err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
	}
	if elapsed := time.Since(start); elapsed > 50*time.Millisecond {
		t.Errorf("expected near-instant completion with zero interval, took %s", elapsed)
	}
}

// TestThrottleTransport_ConcurrentCallers_AllPaced verifies that concurrent
// callers are correctly serialised — the slot reservation happens under the
// lock, so no two requests fire within the same interval even under
// concurrency.
func TestThrottleTransport_ConcurrentCallers_AllPaced(t *testing.T) {
	var callCount atomic.Int64
	mock := roundTripFunc(func(_ *http.Request) (*http.Response, error) {
		callCount.Add(1)
		return makeResp(http.StatusOK, `{}`, nil), nil
	})

	const interval = 5 * time.Millisecond
	const n = 10
	transport := NewThrottleTransport(mock, interval)

	start := time.Now()
	var wg sync.WaitGroup
	for i := 0; i < n; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			req, _ := http.NewRequest("GET", "http://example.com/test", nil)
			transport.RoundTrip(req)
		}()
	}
	wg.Wait()
	elapsed := time.Since(start)

	if callCount.Load() != n {
		t.Errorf("expected %d calls, got %d", n, callCount.Load())
	}
	// n requests paced at `interval` apart must take at least (n-1)*interval.
	minExpected := time.Duration(n-1) * interval
	if elapsed < minExpected {
		t.Errorf("expected at least %s elapsed for %d paced requests, got %s", minExpected, n, elapsed)
	}
}

// roundTripFunc adapts a function to http.RoundTripper.
type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(req *http.Request) (*http.Response, error) {
	return f(req)
}
