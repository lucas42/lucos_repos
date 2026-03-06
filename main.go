package main

import (
	"encoding/json"
	"log/slog"
	"net/http"
	"os"
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
}

func main() {
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
	for _, c := range AllConventions() {
		if err := db.UpsertConvention(c.ID, c.Description); err != nil {
			slog.Warn("Failed to sync convention to database", "convention", c.ID, "error", err)
		}
	}
	slog.Info("Conventions synced to database", "count", len(AllConventions()))

	mux := http.NewServeMux()

	mux.HandleFunc("GET /_info", func(w http.ResponseWriter, r *http.Request) {
		_, tokenErr := githubAuth.GetInstallationToken()
		githubAuthCheck := Check{
			OK:         tokenErr == nil,
			TechDetail: "Checks whether a valid GitHub App installation token can be obtained",
		}
		if tokenErr != nil {
			slog.Warn("GitHub auth check failed", "error", tokenErr)
		}

		// Probe the database with a minimal query.
		dbCheck := Check{
			TechDetail: "Checks whether the SQLite database is accessible",
		}
		dbErr := db.Ping()
		dbCheck.OK = dbErr == nil
		if dbErr != nil {
			slog.Warn("Database check failed", "error", dbErr)
		}

		info := InfoResponse{
			System: system,
			Checks: map[string]any{
				"github-auth": githubAuthCheck,
				"database":    dbCheck,
			},
			Metrics: map[string]any{},
			CI: map[string]string{
				"circle": "gh/lucas42/lucos_repos",
			},
			Icon:           "/icon",
			NetworkOnly:    true,
			ShowOnHomepage: false,
			StartURL:       "/",
			Title:          "Repos",
		}
		w.Header().Set("Content-Type", "application/json")
		if err := json.NewEncoder(w).Encode(info); err != nil {
			slog.Error("Failed to encode /_info response", "error", err)
		}
	})

	slog.Info("Server listening", "port", port)
	if err := http.ListenAndServe(":"+port, mux); err != nil {
		slog.Error("Server failed", "error", err)
		os.Exit(1)
	}
}
