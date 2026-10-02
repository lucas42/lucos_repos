package main

import (
	"bytes"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"syscall"
	"testing"
)

func captureLogs(t *testing.T, fn func()) string {
	t.Helper()
	var buf bytes.Buffer
	old := slog.Default()
	slog.SetDefault(slog.New(slog.NewTextHandler(&buf, &slog.HandlerOptions{Level: slog.LevelDebug})))
	defer slog.SetDefault(old)
	fn()
	return buf.String()
}

func TestLogResponseWriteError_ClientDisconnectIsNotError(t *testing.T) {
	disconnects := []error{
		fmt.Errorf("write tcp 1.2.3.4:80->5.6.7.8:9: %w", &net.OpError{Op: "write", Err: os.NewSyscallError("write", syscall.EPIPE)}),
		os.NewSyscallError("write", syscall.ECONNRESET),
		net.ErrClosed,
	}
	for _, err := range disconnects {
		out := captureLogs(t, func() { logResponseWriteError("Failed to encode JSON response", err) })
		if strings.Contains(out, "level=ERROR") {
			t.Errorf("client disconnect %v logged at ERROR: %s", err, out)
		}
		if !strings.Contains(out, "level=DEBUG") {
			t.Errorf("expected DEBUG log for %v, got: %s", err, out)
		}
	}
}

func TestLogResponseWriteError_OtherErrorsStayError(t *testing.T) {
	out := captureLogs(t, func() { logResponseWriteError("Failed to encode JSON response", errors.New("json: unsupported type")) })
	if !strings.Contains(out, "level=ERROR") {
		t.Errorf("expected ERROR log, got: %s", out)
	}
}

type brokenPipeWriter struct{ *httptest.ResponseRecorder }

func (brokenPipeWriter) Write([]byte) (int, error) { return 0, syscall.EPIPE }

func TestDashboardHandler_JSONClientDisconnectNotLoggedAsError(t *testing.T) {
	db := openTestDB(t)
	req := httptest.NewRequest("GET", "/", nil)
	req.Header.Set("Accept", "application/json")
	out := captureLogs(t, func() {
		newDashboardHandler(db, noopSweeper{})(brokenPipeWriter{httptest.NewRecorder()}, req)
	})
	if strings.Contains(out, "level=ERROR") {
		t.Errorf("client disconnect mid-response logged at ERROR: %s", out)
	}
}

func TestDashboardHandler_UnroutedPathReturns404(t *testing.T) {
	db := openTestDB(t)
	mux := http.NewServeMux()
	mux.HandleFunc("GET /", newDashboardHandler(db, noopSweeper{}))
	mux.HandleFunc("GET /_info", func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(http.StatusOK) })

	for path, want := range map[string]int{
		"/":                                 http.StatusOK,
		"/_info":                            http.StatusOK,
		"/.env":                             http.StatusNotFound,
		"/definitely-not-a-real-path-12345": http.StatusNotFound,
		"/wp-config.php.bak":                http.StatusNotFound,
	} {
		w := httptest.NewRecorder()
		mux.ServeHTTP(w, httptest.NewRequest("GET", path, nil))
		if w.Code != want {
			t.Errorf("GET %s: expected %d, got %d", path, want, w.Code)
		}
		if want == http.StatusNotFound && w.Body.Len() > 100 {
			t.Errorf("GET %s: expected small 404 body, got %d bytes", path, w.Body.Len())
		}
	}
}
