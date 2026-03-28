package conventions

import (
	"io"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
)

func TestCachingTransport_DeduplicatesGETs(t *testing.T) {
	var callCount atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		callCount.Add(1)
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{"ok": true}`))
	}))
	defer server.Close()

	ct := NewCachingTransport(http.DefaultTransport)
	client := &http.Client{Transport: ct}

	// Make the same request 3 times.
	for i := 0; i < 3; i++ {
		resp, err := client.Get(server.URL + "/repos/lucas42/test/branches/main/protection")
		if err != nil {
			t.Fatalf("request %d failed: %v", i, err)
		}
		body, _ := io.ReadAll(resp.Body)
		resp.Body.Close()
		if string(body) != `{"ok": true}` {
			t.Errorf("request %d: unexpected body %q", i, body)
		}
	}

	if got := callCount.Load(); got != 1 {
		t.Errorf("expected 1 backend call, got %d", got)
	}
	if got := ct.Stats(); got != 1 {
		t.Errorf("expected 1 cache entry, got %d", got)
	}
}

func TestCachingTransport_DifferentURLsNotCached(t *testing.T) {
	var callCount atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		callCount.Add(1)
		w.Write([]byte(r.URL.Path))
	}))
	defer server.Close()

	ct := NewCachingTransport(http.DefaultTransport)
	client := &http.Client{Transport: ct}

	_, _ = client.Get(server.URL + "/a")
	_, _ = client.Get(server.URL + "/b")

	if got := callCount.Load(); got != 2 {
		t.Errorf("expected 2 backend calls, got %d", got)
	}
}

func TestCachingTransport_POSTNotCached(t *testing.T) {
	var callCount atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		callCount.Add(1)
		w.Write([]byte("ok"))
	}))
	defer server.Close()

	ct := NewCachingTransport(http.DefaultTransport)
	client := &http.Client{Transport: ct}

	_, _ = client.Post(server.URL+"/api", "application/json", nil)
	_, _ = client.Post(server.URL+"/api", "application/json", nil)

	if got := callCount.Load(); got != 2 {
		t.Errorf("expected 2 backend calls for POST, got %d", got)
	}
}

func TestCachingTransport_CachesErrorResponses(t *testing.T) {
	var callCount atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		callCount.Add(1)
		w.WriteHeader(http.StatusNotFound)
	}))
	defer server.Close()

	ct := NewCachingTransport(http.DefaultTransport)
	client := &http.Client{Transport: ct}

	resp1, _ := client.Get(server.URL + "/missing")
	resp1.Body.Close()
	resp2, _ := client.Get(server.URL + "/missing")
	resp2.Body.Close()

	if got := callCount.Load(); got != 1 {
		t.Errorf("expected 1 backend call for 404, got %d", got)
	}
	if resp2.StatusCode != http.StatusNotFound {
		t.Errorf("expected cached 404, got %d", resp2.StatusCode)
	}
}
