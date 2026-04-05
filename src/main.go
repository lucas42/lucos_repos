package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"time"

	"lucos_repos/conventions"
)

type InfoResponse struct {
	System         string            `json:"system"`
	Checks         map[string]any    `json:"checks"`
	Metrics        map[string]any    `json:"metrics"`
	CI             map[string]string `json:"ci"`
	Icon           string            `json:"icon"`
	NetworkOnly    bool              `json:"network_only"`
	ShowOnHomepage bool              `json:"show_on_homepage"`
	StartURL       string            `json:"start_url"`
	Title          string            `json:"title"`
}

type Check struct {
	OK         bool   `json:"ok"`
	TechDetail string `json:"techDetail"`
	Debug      string `json:"debug,omitempty"`
}

func main() {
	// CLI subcommand dispatch: "audit --dry-run" and "audit diff".
	if len(os.Args) > 1 && os.Args[1] == "audit" {
		auditArgs := os.Args[2:]
		if len(auditArgs) > 0 && auditArgs[0] == "diff" {
			// audit diff --baseline <file> --candidate <file> [--fetched-at <ts>] [--branch <name>]
			fs := flag.NewFlagSet("audit diff", flag.ExitOnError)
			baseline := fs.String("baseline", "", "path to baseline JSON file (required)")
			candidate := fs.String("candidate", "", "path to candidate JSON file (required)")
			fetchedAt := fs.String("fetched-at", "", "timestamp when baseline was fetched (optional)")
			branch := fs.String("branch", "", "name of PR branch (optional)")
			if err := fs.Parse(auditArgs[1:]); err != nil {
				fmt.Fprintf(os.Stderr, "error: %v\n", err)
				os.Exit(1)
			}
			if *baseline == "" || *candidate == "" {
				fmt.Fprintf(os.Stderr, "error: --baseline and --candidate are required\n")
				fs.Usage()
				os.Exit(1)
			}
			runAuditDiff(*baseline, *candidate, *fetchedAt, *branch)
			return
		}

		// audit [--dry-run]
		fs := flag.NewFlagSet("audit", flag.ExitOnError)
		dryRun := fs.Bool("dry-run", false, "run without creating issues; output findings as JSON")
		if err := fs.Parse(auditArgs); err != nil {
			fmt.Fprintf(os.Stderr, "error: %v\n", err)
			os.Exit(1)
		}
		if *dryRun {
			runAuditDryRun()
			return
		}
		fmt.Fprintf(os.Stderr, "usage: lucos_repos audit --dry-run\n       lucos_repos audit diff --baseline <file> --candidate <file>\n")
		os.Exit(1)
	}

	port := os.Getenv("PORT")
	if port == "" {
		slog.Error("Environment variable `PORT` not set")
		os.Exit(2)
	}

	system := os.Getenv("SYSTEM")
	if system == "" {
		system = "lucos_repos"
	}

	githubAuth, err := NewGitHubAuthClient()
	if err != nil {
		slog.Error("Failed to initialise GitHub App authentication", "error", err)
		os.Exit(2)
	}

	dbPath := "/data/lucos_repos.db"
	db, err := OpenDB(dbPath)
	if err != nil {
		slog.Error("Failed to open database", "path", dbPath, "error", err)
		os.Exit(2)
	}
	defer db.Close()

	// Sync all registered conventions into the database on startup.
	for _, c := range conventions.All() {
		if err := db.UpsertConvention(c.ID, c.Description); err != nil {
			slog.Warn("Failed to sync convention to database", "convention", c.ID, "error", err)
		}
	}
	slog.Info("Conventions synced to database", "count", len(conventions.All()))

	sweeper := NewAuditSweeper(db, githubAuth, system)
	sweeper.scheduleTrackerEndpoint = os.Getenv("SCHEDULE_TRACKER_ENDPOINT")
	sweeper.Start()

	prSweeper := NewPRSweeper(githubAuth)
	prSweeper.Start()

	mux := http.NewServeMux()

	mux.HandleFunc("GET /", newDashboardHandler(db, sweeper))
	mux.HandleFunc("GET /prs", newPRDashboardHandler(prSweeper))

	mux.HandleFunc("GET /lucos_navbar.js", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/javascript")
		http.ServeFile(w, r, "/app/lucos_navbar.js")
	})

	mux.HandleFunc("GET /icon", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "image/png")
		http.ServeFile(w, r, "/app/icon.png")
	})

	mux.HandleFunc("GET /_info", func(w http.ResponseWriter, r *http.Request) {
		_, tokenErr := githubAuth.GetInstallationToken()
		githubAuthCheck := Check{
			OK:         tokenErr == nil,
			TechDetail: "Checks whether a valid GitHub App installation token can be obtained",
		}
		if tokenErr != nil {
			slog.Warn("GitHub auth check failed", "error", tokenErr)
			githubAuthCheck.Debug = tokenErr.Error()
		}

		// Probe the database with a minimal query.
		dbCheck := Check{
			TechDetail: "Checks whether the SQLite database is accessible",
		}
		dbErr := db.Ping()
		dbCheck.OK = dbErr == nil
		if dbErr != nil {
			slog.Warn("Database check failed", "error", dbErr)
			dbCheck.Debug = dbErr.Error()
		}

		// Report the last audit sweep status.
		completedAt, sweepErr := sweeper.Status()
		auditCheck := Check{
			TechDetail: "Checks whether the last scheduled audit sweep completed successfully",
		}
		if sweepErr != nil {
			auditCheck.OK = false
			auditCheck.Debug = sweepErr.Error()
		} else if completedAt.IsZero() {
			// First sweep hasn't finished yet — not an error, just not ready.
			auditCheck.OK = true
			auditCheck.Debug = "No sweep has completed yet"
		} else {
			auditCheck.OK = true
			auditCheck.Debug = "Last sweep completed at " + completedAt.UTC().Format(time.RFC3339)
		}

		// Report stale unmerged Dependabot PRs.
		prData := prSweeper.Data()
		staleDependabotCheck := Check{
			TechDetail: fmt.Sprintf("Checks whether any Dependabot PRs have been open for more than %.0f hours without being merged", staleDependabotThreshold.Hours()),
		}
		if prData.LastFetchAt.IsZero() {
			staleDependabotCheck.OK = true
			staleDependabotCheck.Debug = "No PR sweep has completed yet"
		} else if len(prData.StaleDependabotPRs) == 0 {
			staleDependabotCheck.OK = true
			staleDependabotCheck.Debug = "No stale Dependabot PRs found"
		} else {
			staleDependabotCheck.OK = false
			oldest := prData.StaleDependabotPRs[0]
			staleDependabotCheck.Debug = fmt.Sprintf(
				"%d unmerged Dependabot PR(s) open for more than %.0fh; oldest: %s#%d (open since %s)",
				len(prData.StaleDependabotPRs),
				staleDependabotThreshold.Hours(),
				oldest.Repo, oldest.Number,
				oldest.CreatedAt.UTC().Format(time.RFC3339),
			)
		}

		info := InfoResponse{
			System: system,
			Checks: map[string]any{
				"github-auth":            githubAuthCheck,
				"database":               dbCheck,
				"last-audit-completed":   auditCheck,
				"stale-dependabot-prs":   staleDependabotCheck,
			},
			Metrics: map[string]any{},
			CI: map[string]string{
				"circle": "gh/lucas42/lucos_repos",
			},
			Icon:           "/icon",
			NetworkOnly:    true,
			ShowOnHomepage: true,
			StartURL:       "/",
			Title:          "Repos",
		}
		w.Header().Set("Content-Type", "application/json")
		if err := json.NewEncoder(w).Encode(info); err != nil {
			slog.Error("Failed to encode /_info response", "error", err)
		}
	})

	mux.HandleFunc("GET /api/status/", newSingleRepoStatusHandler(db))
	mux.HandleFunc("POST /api/rerun", newRerunHandler(db, githubAuth, githubAPIBaseURL, configyBaseURL))
	mux.HandleFunc("POST /api/sweep", newSweepHandler(sweeper))
	oidcValidator := NewGitHubOIDCValidator("lucas42")
	mux.HandleFunc("POST /api/audit/", newAuditHandler(db, githubAuth, githubAPIBaseURL, oidcValidator))

	mux.HandleFunc("GET /api/status", func(w http.ResponseWriter, r *http.Request) {
		report, err := db.GetStatusReport()
		if err != nil {
			slog.Error("Failed to build status report", "error", err)
			http.Error(w, "internal server error", http.StatusInternalServerError)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		if err := json.NewEncoder(w).Encode(report); err != nil {
			slog.Error("Failed to encode /api/status response", "error", err)
		}
	})

	slog.Info("Server listening", "port", port)
	if err := http.ListenAndServe(":"+port, mux); err != nil {
		slog.Error("Server failed", "error", err)
		os.Exit(1)
	}
}
