package main

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestPRDashboardHandler_HTML(t *testing.T) {
	sweeper := &PRSweeper{}
	sweeper.data = PRDashboardData{
		Repos: []RepoPRCounts{
			{
				RepoName:      "lucas42/lucos_photos",
				FailingChecks: 1,
				PendingChecks: 2,
				Total:         3,
			},
		},
		LastFetchAt: time.Date(2026, 3, 28, 12, 0, 0, 0, time.UTC),
	}

	handler := newPRDashboardHandler(sweeper)
	req := httptest.NewRequest("GET", "/prs", nil)
	req.Header.Set("Accept", "text/html")
	w := httptest.NewRecorder()

	handler(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}
	body := w.Body.String()
	if !strings.Contains(body, "lucas42/lucos_photos") {
		t.Error("expected repo name in HTML output")
	}
	if !strings.Contains(body, "Open Pull Requests") {
		t.Error("expected page title in HTML output")
	}
}

func TestPRDashboardHandler_JSON(t *testing.T) {
	sweeper := &PRSweeper{}
	sweeper.data = PRDashboardData{
		Repos: []RepoPRCounts{
			{
				RepoName:        "lucas42/lucos_test",
				FailingChecks:   1,
				BotApprovedOnly: 2,
				Total:           3,
			},
		},
		LastFetchAt: time.Date(2026, 3, 28, 12, 0, 0, 0, time.UTC),
	}

	handler := newPRDashboardHandler(sweeper)
	req := httptest.NewRequest("GET", "/prs", nil)
	req.Header.Set("Accept", "application/json")
	w := httptest.NewRecorder()

	handler(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}

	var data PRDashboardData
	if err := json.NewDecoder(w.Body).Decode(&data); err != nil {
		t.Fatalf("failed to decode JSON: %v", err)
	}
	if len(data.Repos) != 1 {
		t.Fatalf("expected 1 repo, got %d", len(data.Repos))
	}
	if data.Repos[0].RepoName != "lucas42/lucos_test" {
		t.Errorf("expected repo name 'lucas42/lucos_test', got %q", data.Repos[0].RepoName)
	}
	if data.Repos[0].Total != 3 {
		t.Errorf("expected total 3, got %d", data.Repos[0].Total)
	}
}

func TestClassifyPR_FailingChecks(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if strings.Contains(r.URL.Path, "/status") {
			w.Write([]byte(`{"state":"failure","statuses":[{"state":"failure"}]}`))
			return
		}
		if strings.Contains(r.URL.Path, "/check-runs") {
			w.Write([]byte(`{"check_runs":[]}`))
			return
		}
		w.WriteHeader(http.StatusNotFound)
	}))
	defer server.Close()

	p := &PRSweeper{githubAPIBase: server.URL}
	state := p.classifyPR("fake", "lucas42/test", 1)
	if state != PRStateFailingChecks {
		t.Errorf("expected PRStateFailingChecks, got %d", state)
	}
}

func TestClassifyPR_AllPassing_NoReviews(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if strings.Contains(r.URL.Path, "/status") {
			w.Write([]byte(`{"state":"success","statuses":[{"state":"success"}]}`))
			return
		}
		if strings.Contains(r.URL.Path, "/check-runs") {
			conclusion := "success"
			w.Write([]byte(`{"check_runs":[{"status":"completed","conclusion":"` + conclusion + `"}]}`))
			return
		}
		if strings.Contains(r.URL.Path, "/reviews") {
			w.Write([]byte(`[]`))
			return
		}
		w.WriteHeader(http.StatusNotFound)
	}))
	defer server.Close()

	p := &PRSweeper{githubAPIBase: server.URL}
	state := p.classifyPR("fake", "lucas42/test", 1)
	if state != PRStateNoReviews {
		t.Errorf("expected PRStateNoReviews, got %d", state)
	}
}
