package main

import (
	"embed"
	"encoding/json"
	"html/template"
	"log/slog"
	"net/http"
	"sort"
	"strings"
	"time"

	"lucos_repos/conventions"
)

//go:embed templates/index.html.tmpl
var templateFS embed.FS

// dashboardTemplate is the parsed index page template.
var dashboardTemplate = template.Must(
	template.New("index.html.tmpl").ParseFS(templateFS, "templates/index.html.tmpl"),
)

// dashboardCell holds the display state for a single repo × convention cell.
type dashboardCell struct {
	// Present is true when a finding exists for this repo + convention pair.
	// It is false when the convention does not apply to the repo (e.g. because
	// no finding has been recorded yet, or the convention is repo-type-specific).
	Present  bool
	Pass     bool
	Detail   string
	IssueURL string
}

// dashboardRepo is a row in the compliance matrix.
type dashboardRepo struct {
	Name      string
	RepoType  conventions.RepoType
	Compliant bool
	// Cells are ordered to match DashboardData.Conventions.
	Cells []dashboardCell
}

// DashboardData is the data passed to the HTML template.
type DashboardData struct {
	// Conventions is the sorted list of convention IDs used as column headers.
	Conventions []string
	// Repos is the sorted list of repo rows.
	Repos []dashboardRepo
	// Summary statistics.
	Summary       StatusSummary
	CompliancePct int
	// LastAuditAt is the time the most recent successful audit sweep completed.
	// It is zero if no sweep has completed yet.
	LastAuditAt time.Time
}

// BuildDashboardData converts a StatusReport into the data structure expected
// by the HTML template.
func BuildDashboardData(report StatusReport) DashboardData {
	// Collect all convention IDs seen across all repos.
	conventionSet := map[string]struct{}{}
	for _, rs := range report.Repos {
		for conv := range rs.Conventions {
			conventionSet[conv] = struct{}{}
		}
	}

	// Sort conventions for stable column ordering.
	conventions := make([]string, 0, len(conventionSet))
	for c := range conventionSet {
		conventions = append(conventions, c)
	}
	sort.Strings(conventions)

	// Sort repo names for stable row ordering.
	repoNames := make([]string, 0, len(report.Repos))
	for name := range report.Repos {
		repoNames = append(repoNames, name)
	}
	sort.Strings(repoNames)

	// Build rows.
	rows := make([]dashboardRepo, 0, len(repoNames))
	for _, name := range repoNames {
		rs := report.Repos[name]
		cells := make([]dashboardCell, len(conventions))
		for i, conv := range conventions {
			cs, ok := rs.Conventions[conv]
			if !ok {
				cells[i] = dashboardCell{Present: false}
			} else {
				cells[i] = dashboardCell{
					Present:  true,
					Pass:     cs.Pass,
					Detail:   cs.Detail,
					IssueURL: cs.IssueURL,
				}
			}
		}
		rows = append(rows, dashboardRepo{
			Name:      name,
			RepoType:  rs.Type,
			Compliant: rs.Compliant,
			Cells:     cells,
		})
	}

	// Compute compliance percentage.
	pct := 0
	if report.Summary.TotalRepos > 0 {
		pct = (report.Summary.CompliantRepos * 100) / report.Summary.TotalRepos
	}

	return DashboardData{
		Conventions:   conventions,
		Repos:         rows,
		Summary:       report.Summary,
		CompliancePct: pct,
	}
}

// jsonCheckResult is a single check entry in the JSON API response.
type jsonCheckResult struct {
	Status string `json:"status"`
	Issue  string `json:"issue,omitempty"`
}

// jsonRepoResult is one repo row in the JSON API response.
type jsonRepoResult struct {
	Repo     string                     `json:"repo"`
	RepoType string                     `json:"repo_type"`
	Checks   map[string]jsonCheckResult `json:"checks"`
}

// buildJSONResponse converts DashboardData into the slice used for JSON output.
func buildJSONResponse(data DashboardData) []jsonRepoResult {
	results := make([]jsonRepoResult, 0, len(data.Repos))
	for _, row := range data.Repos {
		checks := make(map[string]jsonCheckResult, len(data.Conventions))
		for i, conv := range data.Conventions {
			cell := row.Cells[i]
			var cr jsonCheckResult
			switch {
			case !cell.Present:
				cr.Status = "na"
			case cell.Pass:
				cr.Status = "pass"
			default:
				cr.Status = "fail"
				cr.Issue = cell.IssueURL
			}
			checks[conv] = cr
		}
		results = append(results, jsonRepoResult{
			Repo:     row.Name,
			RepoType: string(row.RepoType),
			Checks:   checks,
		})
	}
	return results
}

// wantsJSON returns true when the request's Accept header prefers JSON over HTML.
// curl's default Accept is */* which should resolve to JSON; browsers send text/html first.
func wantsJSON(r *http.Request) bool {
	accept := r.Header.Get("Accept")
	if accept == "" {
		return false
	}
	// Walk through comma-separated media ranges in order and return true if
	// application/json appears before text/html (or if */* appears without text/html).
	for _, part := range strings.Split(accept, ",") {
		mediaType := strings.TrimSpace(strings.SplitN(part, ";", 2)[0])
		switch mediaType {
		case "application/json":
			return true
		case "text/html":
			return false
		}
	}
	// */* or other wildcard without explicit text/html preference → JSON.
	return true
}

// sweepStatusProvider is the subset of AuditSweeper used by the dashboard handler.
type sweepStatusProvider interface {
	Status() (completedAt time.Time, lastErr error)
}

// newDashboardHandler returns the GET / handler backed by the given DB and sweep status provider.
func newDashboardHandler(db *DB, sweeper sweepStatusProvider) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		report, err := db.GetStatusReport()
		if err != nil {
			slog.Error("Failed to build status report for dashboard", "error", err)
			http.Error(w, "internal server error", http.StatusInternalServerError)
			return
		}

		data := BuildDashboardData(report)
		lastAuditAt, _ := sweeper.Status()
		data.LastAuditAt = lastAuditAt

		w.Header().Set("Vary", "Accept")

		if wantsJSON(r) {
			results := buildJSONResponse(data)
			w.Header().Set("Content-Type", "application/json")
			if err := json.NewEncoder(w).Encode(results); err != nil {
				slog.Error("Failed to encode JSON response", "error", err)
			}
			return
		}

		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		if err := dashboardTemplate.Execute(w, data); err != nil {
			slog.Error("Failed to render dashboard template", "error", err)
		}
	}
}
