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
// the live repository state — including RepoContext.Type, refreshed from a
// fresh configy fetch rather than the last full sweep's DB-cached value
// (#453), since a repo's configy registration can change between sweeps and
// Type also gates which conventions apply at all. If the live configy fetch
// fails, this falls back to the DB-cached Type (degraded, not broken) and
// logs a warning. A repo no longer found in configy is reclassified as
// RepoTypeUnconfigured, matching what a full sweep would compute right now.
// Results are saved to the database so the dashboard reflects the new state
// without waiting for the next scheduled sweep.
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

		// Fetch a fresh repo-type/hosts map once upfront, rather than the
		// per-repo hosts-only fetch this replaces — 3 configy calls total for
		// any number of repos, and it's the only way to get a correct Type
		// (see fetchRepoTypesFrom). A failure here degrades to the DB-cached
		// Type per repo below rather than failing the whole rerun.
		liveTypes, typeErr := fetchRepoTypesFrom(configyBase, defaultGitHubOrg)
		if typeErr != nil {
			slog.Warn("Failed to fetch live repo types from configy for rerun; falling back to last-sweep cached Type", "error", typeErr)
		}

		allConventions := conventions.All()
		var results []rerunRepoResult

		for _, repoName := range repoNames {
			rs := report.Repos[repoName]

			// Resolve this repo's live Type/hosts/unsupervisedAgentCode. A repo
			// missing from a successfully-fetched live map has been removed
			// from configy since the last sweep — reclassify as unconfigured,
			// matching what a full sweep would compute right now.
			repoType := rs.Type
			var hosts []string
			var unsupervisedAgentCode bool
			if typeErr == nil {
				if info, ok := liveTypes[repoName]; ok {
					repoType = info.Type
					hosts = info.Hosts
					unsupervisedAgentCode = info.UnsupervisedAgentCode
				} else {
					repoType = conventions.RepoTypeUnconfigured
				}
			}

			ctx := conventions.RepoContext{
				Name:                  repoName,
				GitHubToken:           token,
				Type:                  repoType,
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
				if !conv.AppliesToType(repoType) {
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
				RepoType: string(repoType),
				Checks:   checks,
			})
		}

		w.Header().Set("Content-Type", "application/json")
		if err := json.NewEncoder(w).Encode(results); err != nil {
			slog.Error("Failed to encode rerun response", "error", err)
		}
	}
}
