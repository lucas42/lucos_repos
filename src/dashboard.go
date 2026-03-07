package main

import (
	"embed"
	"html/template"
	"log/slog"
	"net/http"
	"sort"
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

// newDashboardHandler returns the GET / handler backed by the given DB.
func newDashboardHandler(db *DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		report, err := db.GetStatusReport()
		if err != nil {
			slog.Error("Failed to build status report for dashboard", "error", err)
			http.Error(w, "internal server error", http.StatusInternalServerError)
			return
		}

		data := BuildDashboardData(report)

		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		if err := dashboardTemplate.Execute(w, data); err != nil {
			slog.Error("Failed to render dashboard template", "error", err)
		}
	}
}
