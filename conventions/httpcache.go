package conventions

import (
	"bytes"
	"io"
	"net/http"
	"sync"
	"sync/atomic"
)

// cachedResponse stores the full response data for replay.
type cachedResponse struct {
	statusCode int
	header     http.Header
	body       []byte
}

// CachingTransport is an http.RoundTripper that caches GET responses by URL.
// Within a single sweep, the same URL will only hit the network once —
// subsequent requests return the cached response. This eliminates redundant
// API calls (e.g. branch protection fetched by multiple conventions per repo).
//
// The cache is in-memory and scoped to the lifetime of the transport instance.
// Create a new CachingTransport for each sweep to avoid stale data.
//
// Concurrency note: the cache is safe for concurrent reads, but two goroutines
// requesting the same URL simultaneously may both make a network call (the
// second write simply overwrites the first with an identical response). This is
// acceptable for the current sequential sweep use case. If true dedup under
// concurrency is needed, use a singleflight pattern instead.
type CachingTransport struct {
	// Wrapped is the underlying transport to use for actual network requests.
	Wrapped http.RoundTripper

	mu    sync.Mutex
	cache map[string]*cachedResponse

	// hits counts the number of cache hits (requests served from cache).
	hits atomic.Int64
	// misses counts the number of cache misses (requests that hit the network).
	misses atomic.Int64
}

// NewCachingTransport creates a CachingTransport wrapping the given transport.
// If wrapped is nil, http.DefaultTransport is used.
func NewCachingTransport(wrapped http.RoundTripper) *CachingTransport {
	if wrapped == nil {
		wrapped = http.DefaultTransport
	}
	return &CachingTransport{
		Wrapped: wrapped,
		cache:   make(map[string]*cachedResponse),
	}
}

// RoundTrip implements http.RoundTripper. Only GET requests are cached;
// all other methods are passed through to the wrapped transport.
func (t *CachingTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	// Only cache GET requests.
	if req.Method != http.MethodGet {
		return t.Wrapped.RoundTrip(req)
	}

	key := req.URL.String()

	t.mu.Lock()
	if cached, ok := t.cache[key]; ok {
		t.mu.Unlock()
		t.hits.Add(1)
		return cached.toResponse(req), nil
	}
	t.mu.Unlock()

	// Not cached — make the actual request.
	resp, err := t.Wrapped.RoundTrip(req)
	if err != nil {
		return nil, err
	}

	// Read and cache the response body.
	body, err := io.ReadAll(resp.Body)
	resp.Body.Close()
	if err != nil {
		return nil, err
	}

	entry := &cachedResponse{
		statusCode: resp.StatusCode,
		header:     resp.Header.Clone(),
		body:       body,
	}

	t.mu.Lock()
	t.cache[key] = entry
	t.mu.Unlock()

	t.misses.Add(1)

	// Return a response with a fresh body reader.
	resp.Body = io.NopCloser(bytes.NewReader(body))
	return resp, nil
}

// Stats returns the number of unique URLs cached.
func (t *CachingTransport) Stats() int {
	t.mu.Lock()
	defer t.mu.Unlock()
	return len(t.cache)
}

// Hits returns the number of requests served from cache.
func (t *CachingTransport) Hits() int64 {
	return t.hits.Load()
}

// Misses returns the number of requests that hit the network.
func (t *CachingTransport) Misses() int64 {
	return t.misses.Load()
}

// toResponse reconstructs an *http.Response from the cached data.
func (c *cachedResponse) toResponse(req *http.Request) *http.Response {
	return &http.Response{
		StatusCode: c.statusCode,
		Header:     c.header.Clone(),
		Body:       io.NopCloser(bytes.NewReader(c.body)),
		Request:    req,
	}
}
