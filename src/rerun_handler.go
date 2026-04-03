package main

import (
	"encoding/json"
	"log/slog"
	"net/http"
	"sort"

	"lucos_repos/conventions"
)

// rerunRepoResult is one repo entry in the POST /api/rerun response.
type rerunRepoResult struct {
	Repo     string                     `json:"repo"`
	RepoType string                     `json:"repo_type"`
	Checks   map[string]jsonCheckResult `json:"checks"`
}

// newRerunHandler returns the POST /api/rerun handler.
//
// It accepts ?repo and/or ?convention query parameters (at least one is
// required) and immediately re-runs the matching convention checks against
// the live repository state. Results are saved to the database so the
// dashboard reflects the new state without waiting for the next scheduled
// sweep.
//
// No auth is required — this is an internal operational tool on the trusted
// l42.eu network. The re-run does not interfere with the scheduled sweep:
// SaveFinding is an upsert and the next sweep simply overwrites the result.
func newRerunHandler(db *DB, githubAuth *GitHubAuthClient, githubAPIBase, configyBase string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		repoFilter := r.URL.Query().Get("repo")
		conventionFilter := r.URL.Query().Get("convention")

		if repoFilter == "" && conventionFilter == "" {
			http.Error(w, "at least one of ?repo or ?convention is required", http.StatusBadRequest)
			return
		}

		// Validate the convention filter if provided.
		if conventionFilter != "" {
			found := false
			for _, c := range conventions.All() {
				if c.ID == conventionFilter {
					found = true
					break
				}
			}
			if !found {
				http.Error(w, "unknown convention: "+conventionFilter, http.StatusBadRequest)
				return
			}
		}

		// Load current repo types and issue URLs from the database.
		report, err := db.GetStatusReport()
		if err != nil {
			slog.Error("Failed to build status report for rerun", "error", err)
			http.Error(w, "internal server error", http.StatusInternalServerError)
			return
		}

		// Determine which repos to process.
		var repoNames []string
		if repoFilter != "" {
			if _, ok := report.Repos[repoFilter]; !ok {
				http.Error(w, "repo not found in audit data", http.StatusNotFound)
				return
			}
			repoNames = []string{repoFilter}
		} else {
			for name := range report.Repos {
				repoNames = append(repoNames, name)
			}
			sort.Strings(repoNames)
		}

		// Get a GitHub token for running conventions.
		token, err := githubAuth.GetInstallationToken()
		if err != nil {
			slog.Error("Failed to get GitHub token for rerun", "error", err)
			http.Error(w, "internal server error", http.StatusInternalServerError)
			return
		}

		allConventions := conventions.All()
		var results []rerunRepoResult

		for _, repoName := range repoNames {
			rs := report.Repos[repoName]

			// Fetch configy metadata (hosts, unsupervisedAgentCode) for this repo.
			// Errors are non-fatal — the check still runs, but host-dependent
			// conventions (e.g. circleci-system-deploy-jobs) may give incomplete results.
			hosts, unsupervisedAgentCode, configyErr := fetchConfigyRepoInfo(configyBase, repoName)
			if configyErr != nil {
				slog.Warn("Failed to fetch configy data for rerun, proceeding without hosts",
					"repo", repoName, "error", configyErr)
			}

			ctx := conventions.RepoContext{
				Name:                  repoName,
				GitHubToken:           token,
				Type:                  rs.Type,
				Hosts:                 hosts,
				GitHubBaseURL:         githubAPIBase,
				UnsupervisedAgentCode: unsupervisedAgentCode,
			}

			checks := make(map[string]jsonCheckResult)
			hasAnyCheck := false

			for _, conv := range allConventions {
				if conventionFilter != "" && conv.ID != conventionFilter {
					continue
				}
				if !conv.AppliesToType(rs.Type) {
					continue
				}
				if !conv.AppliesToRepo(repoName) {
					continue
				}

				hasAnyCheck = true
				result := conv.Check(ctx)

				var cr jsonCheckResult
				if result.Err != nil {
					cr.Status = "error"
					cr.Detail = result.Err.Error()
				} else if result.Pass {
					cr.Status = "pass"
					cr.Detail = result.Detail
				} else {
					cr.Status = "fail"
					cr.Detail = result.Detail
					// Preserve the existing issue URL — the re-run does not create issues.
					if existing, ok := rs.Conventions[conv.ID]; ok {
						cr.Issue = existing.IssueURL
					}
				}

				// Save the finding to the database (skip on error so we don't
				// overwrite good data with an indeterminate result).
				if result.Err == nil {
					issueURL := ""
					if !result.Pass {
						if existing, ok := rs.Conventions[conv.ID]; ok {
							issueURL = existing.IssueURL
						}
					}
					if err := db.SaveFinding(result, repoName, issueURL); err != nil {
						slog.Warn("Failed to save rerun finding", "repo", repoName, "convention", conv.ID, "error", err)
					}
				}

				checks[conv.ID] = cr
			}

			// Skip repos where no convention matched the filter (avoids noise in
			// cross-repo convention re-runs against repos where it doesn't apply).
			if !hasAnyCheck {
				continue
			}

			results = append(results, rerunRepoResult{
				Repo:     repoName,
				RepoType: string(rs.Type),
				Checks:   checks,
			})
		}

		w.Header().Set("Content-Type", "application/json")
		if err := json.NewEncoder(w).Encode(results); err != nil {
			slog.Error("Failed to encode rerun response", "error", err)
		}
	}
}
