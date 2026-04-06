package conventions

import "encoding/json"

// branchProtectionFixture builds a minimal branch protection JSON response
// containing the given required status check context names in the legacy
// "contexts" field.
func branchProtectionFixture(contexts []string) []byte {
	type requiredStatusChecks struct {
		Contexts []string `json:"contexts"`
	}
	type response struct {
		RequiredStatusChecks requiredStatusChecks `json:"required_status_checks"`
	}
	b, _ := json.Marshal(response{
		RequiredStatusChecks: requiredStatusChecks{Contexts: contexts},
	})
	return b
}

// branchProtectionFixtureWithChecks builds a branch protection JSON response
// containing required status checks in the modern "checks" array (as populated
// by the current GitHub UI), leaving "contexts" empty.
func branchProtectionFixtureWithChecks(checkNames []string) []byte {
	type checkEntry struct {
		Context string `json:"context"`
		AppID   int    `json:"app_id"`
	}
	type requiredStatusChecks struct {
		Contexts []string     `json:"contexts"`
		Checks   []checkEntry `json:"checks"`
	}
	type response struct {
		RequiredStatusChecks requiredStatusChecks `json:"required_status_checks"`
	}
	entries := make([]checkEntry, len(checkNames))
	for i, name := range checkNames {
		entries[i] = checkEntry{Context: name, AppID: 12345}
	}
	b, _ := json.Marshal(response{
		RequiredStatusChecks: requiredStatusChecks{
			Contexts: []string{},
			Checks:   entries,
		},
	})
	return b
}
