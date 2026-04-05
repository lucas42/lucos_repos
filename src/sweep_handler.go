package main

import (
	"net/http"
)

// newSweepHandler returns the POST /api/sweep handler.
//
// It triggers a full audit sweep equivalent to the scheduled sweep — running
// all registered conventions across all repos, updating issue state, and
// updating sweeper.Status() so the last-audit-completed check reflects the
// new run.
//
// The sweep runs in the background and the endpoint returns 202 Accepted
// immediately. If a sweep is already in progress the endpoint returns 409
// Conflict.
//
// No auth is required — this is an internal operational tool on the trusted
// l42.eu network.
func newSweepHandler(sweeper *AuditSweeper) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if !sweeper.TriggerSweep() {
			http.Error(w, "sweep already in progress", http.StatusConflict)
			return
		}
		w.WriteHeader(http.StatusAccepted)
	}
}
