package main

import (
	"net/http"
)

// newPRSweepHandler returns the POST /api/pr-sweep handler.
//
// It triggers a PR sweep equivalent to the scheduled sweep — fetching open PRs
// for all repos, updating stale Dependabot PR data, and refreshing the PR
// dashboard.
//
// The sweep runs in the background and the endpoint returns 202 Accepted
// immediately. If a sweep is already in progress the endpoint returns 409
// Conflict.
//
// No auth is required — this is an internal operational tool on the trusted
// l42.eu network.
func newPRSweepHandler(sweeper *PRSweeper) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if !sweeper.TriggerSweep() {
			http.Error(w, "sweep already in progress", http.StatusConflict)
			return
		}
		w.WriteHeader(http.StatusAccepted)
	}
}
