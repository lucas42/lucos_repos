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

	mux := http.NewServeMux()

	mux.HandleFunc("GET /_info", func(w http.ResponseWriter, r *http.Request) {
		info := InfoResponse{
			System:  system,
			Checks:  map[string]any{},
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
