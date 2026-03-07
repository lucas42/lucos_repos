package main

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"lucos_repos/conventions"
)

// newStatusHandler returns the GET /api/status handler backed by the given DB.
// It mirrors the logic in main.go so we can test it in isolation.
func newStatusHandler(db *DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		report, err := db.GetStatusReport()
		if err != nil {
			http.Error(w, "internal server error", http.StatusInternalServerError)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(report)
	}
}

// TestAPIStatus_EmptyDB verifies the endpoint returns a valid empty report
// when no findings exist.
func TestAPIStatus_EmptyDB(t *testing.T) {
	db := openTestDB(t)

	req := httptest.NewRequest("GET", "/api/status", nil)
	w := httptest.NewRecorder()
	newStatusHandler(db)(w, req)

	res := w.Result()
	if res.StatusCode != http.StatusOK {
		t.Fatalf("expected 200, got %d", res.StatusCode)
	}
	if ct := res.Header.Get("Content-Type"); ct != "application/json" {
		t.Errorf("expected Content-Type application/json, got %q", ct)
	}

	var report StatusReport
	if err := json.NewDecoder(res.Body).Decode(&report); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}
	if report.Summary.TotalRepos != 0 {
		t.Errorf("expected TotalRepos 0, got %d", report.Summary.TotalRepos)
	}
}

// TestAPIStatus_WithFindings verifies the endpoint returns populated data.
func TestAPIStatus_WithFindings(t *testing.T) {
	db := openTestDB(t)

	if err := db.UpsertRepo("lucas42/lucos_test"); err != nil {
		t.Fatalf("UpsertRepo failed: %v", err)
	}
	if err := db.UpsertConvention("has-circleci-config", "Has a CircleCI config"); err != nil {
		t.Fatalf("UpsertConvention failed: %v", err)
	}
	if err := db.SaveFinding(
		conventions.ConventionResult{Convention: "has-circleci-config", Pass: true, Detail: ".circleci/config.yml found"},
		"lucas42/lucos_test", "",
	); err != nil {
		t.Fatalf("SaveFinding failed: %v", err)
	}

	req := httptest.NewRequest("GET", "/api/status", nil)
	w := httptest.NewRecorder()
	newStatusHandler(db)(w, req)

	res := w.Result()
	if res.StatusCode != http.StatusOK {
		t.Fatalf("expected 200, got %d", res.StatusCode)
	}

	var report StatusReport
	if err := json.NewDecoder(res.Body).Decode(&report); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}

	if report.Summary.TotalRepos != 1 {
		t.Errorf("expected TotalRepos 1, got %d", report.Summary.TotalRepos)
	}
	if report.Summary.CompliantRepos != 1 {
		t.Errorf("expected CompliantRepos 1, got %d", report.Summary.CompliantRepos)
	}
	if report.Summary.TotalViolations != 0 {
		t.Errorf("expected TotalViolations 0, got %d", report.Summary.TotalViolations)
	}

	repo, ok := report.Repos["lucas42/lucos_test"]
	if !ok {
		t.Fatal("expected entry for 'lucas42/lucos_test'")
	}
	if !repo.Compliant {
		t.Error("expected repo to be compliant")
	}
	cs, ok := repo.Conventions["has-circleci-config"]
	if !ok {
		t.Fatal("expected 'has-circleci-config' convention entry")
	}
	if !cs.Pass {
		t.Error("expected convention to pass")
	}
	if cs.Detail != ".circleci/config.yml found" {
		t.Errorf("unexpected detail: %q", cs.Detail)
	}
}

// TestAPIStatus_MethodNotAllowed verifies that a non-GET request is rejected.
func TestAPIStatus_MethodNotAllowed(t *testing.T) {
	db := openTestDB(t)

	// Register on a mux to test the method routing.
	mux := http.NewServeMux()
	mux.HandleFunc("GET /api/status", newStatusHandler(db))

	req := httptest.NewRequest("POST", "/api/status", nil)
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	if w.Result().StatusCode != http.StatusMethodNotAllowed {
		t.Errorf("expected 405, got %d", w.Result().StatusCode)
	}
}
