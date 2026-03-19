package main

import (
	"strings"
	"testing"

	"lucos_repos/conventions"
)

// makeBaseline builds a baselineReport from a map of repo → convention → pass.
func makeBaseline(data map[string]map[string]bool) baselineReport {
	repos := map[string]baselineRepoStatus{}
	for repoName, convs := range data {
		cs := map[string]baselineConvStatus{}
		for convID, pass := range convs {
			cs[convID] = baselineConvStatus{Pass: pass, Detail: "detail"}
		}
		repos[repoName] = baselineRepoStatus{Conventions: cs}
	}
	return baselineReport{Repos: repos}
}

// makeCandidate builds a DryRunReport from a map of repo → convention → pass.
// A nil value for a convention means it was skipped.
func makeCandidate(data map[string]map[string]*bool) DryRunReport {
	repos := map[string]DryRunRepoStatus{}
	for repoName, convs := range data {
		cs := map[string]DryRunConvStatus{}
		compliant := true
		for convID, pass := range convs {
			if pass == nil {
				cs[convID] = DryRunConvStatus{Skipped: true}
			} else {
				cs[convID] = DryRunConvStatus{Pass: *pass, Detail: "detail"}
				if !*pass {
					compliant = false
				}
			}
		}
		repos[repoName] = DryRunRepoStatus{
			Type:        conventions.RepoTypeSystem,
			Conventions: cs,
			Compliant:   compliant,
		}
	}
	return DryRunReport{Repos: repos}
}

func boolPtr(b bool) *bool { return &b }

// TestComputeAuditDiff_NoChanges verifies that identical baseline and candidate produce no diff.
func TestComputeAuditDiff_NoChanges(t *testing.T) {
	baseline := makeBaseline(map[string]map[string]bool{
		"lucas42/repo_a": {"conv-1": true, "conv-2": false},
	})
	candidate := makeCandidate(map[string]map[string]*bool{
		"lucas42/repo_a": {"conv-1": boolPtr(true), "conv-2": boolPtr(false)},
	})

	diff := ComputeAuditDiff(baseline, candidate)

	if len(diff.NewFailures) != 0 {
		t.Errorf("expected no new failures, got %v", diff.NewFailures)
	}
	if len(diff.ResolvedFailures) != 0 {
		t.Errorf("expected no resolved failures, got %v", diff.ResolvedFailures)
	}
	if diff.Unchanged != 2 {
		t.Errorf("expected 2 unchanged, got %d", diff.Unchanged)
	}
}

// TestComputeAuditDiff_NewFailure verifies detection of a new failure.
func TestComputeAuditDiff_NewFailure(t *testing.T) {
	baseline := makeBaseline(map[string]map[string]bool{
		"lucas42/repo_a": {"conv-1": true},
	})
	candidate := makeCandidate(map[string]map[string]*bool{
		"lucas42/repo_a": {"conv-1": boolPtr(false)},
	})

	diff := ComputeAuditDiff(baseline, candidate)

	if len(diff.NewFailures) != 1 {
		t.Fatalf("expected 1 new failure, got %d", len(diff.NewFailures))
	}
	if diff.NewFailures[0].Repo != "lucas42/repo_a" || diff.NewFailures[0].Convention != "conv-1" {
		t.Errorf("unexpected new failure entry: %+v", diff.NewFailures[0])
	}
	if len(diff.ResolvedFailures) != 0 {
		t.Errorf("expected no resolved failures, got %v", diff.ResolvedFailures)
	}
}

// TestComputeAuditDiff_ResolvedFailure verifies detection of a resolved failure.
func TestComputeAuditDiff_ResolvedFailure(t *testing.T) {
	baseline := makeBaseline(map[string]map[string]bool{
		"lucas42/repo_a": {"conv-1": false},
	})
	candidate := makeCandidate(map[string]map[string]*bool{
		"lucas42/repo_a": {"conv-1": boolPtr(true)},
	})

	diff := ComputeAuditDiff(baseline, candidate)

	if len(diff.ResolvedFailures) != 1 {
		t.Fatalf("expected 1 resolved failure, got %d", len(diff.ResolvedFailures))
	}
	if diff.ResolvedFailures[0].Repo != "lucas42/repo_a" || diff.ResolvedFailures[0].Convention != "conv-1" {
		t.Errorf("unexpected resolved failure entry: %+v", diff.ResolvedFailures[0])
	}
	if len(diff.NewFailures) != 0 {
		t.Errorf("expected no new failures, got %v", diff.NewFailures)
	}
}

// TestComputeAuditDiff_SkippedChecksIgnored verifies that skipped checks are not
// counted as new failures or resolved failures.
func TestComputeAuditDiff_SkippedChecksIgnored(t *testing.T) {
	baseline := makeBaseline(map[string]map[string]bool{
		"lucas42/repo_a": {"conv-1": true},
	})
	// conv-1 is skipped in the candidate — should not appear as new failure.
	candidate := makeCandidate(map[string]map[string]*bool{
		"lucas42/repo_a": {"conv-1": nil},
	})

	diff := ComputeAuditDiff(baseline, candidate)

	if len(diff.NewFailures) != 0 {
		t.Errorf("expected no new failures for skipped check, got %v", diff.NewFailures)
	}
	if diff.SkippedInCandidate != 1 {
		t.Errorf("expected 1 skipped, got %d", diff.SkippedInCandidate)
	}
}

// TestComputeAuditDiff_NewConventionNotInBaseline verifies that a new convention
// (not in baseline) appearing as failing in candidate counts as a new failure.
func TestComputeAuditDiff_NewConventionNotInBaseline(t *testing.T) {
	baseline := makeBaseline(map[string]map[string]bool{
		"lucas42/repo_a": {},
	})
	candidate := makeCandidate(map[string]map[string]*bool{
		"lucas42/repo_a": {"new-convention": boolPtr(false)},
	})

	diff := ComputeAuditDiff(baseline, candidate)

	if len(diff.NewFailures) != 1 {
		t.Fatalf("expected 1 new failure for new convention, got %d", len(diff.NewFailures))
	}
	if diff.NewFailures[0].Convention != "new-convention" {
		t.Errorf("unexpected convention: %q", diff.NewFailures[0].Convention)
	}
}

// TestComputeAuditDiff_ConventionRemovedFromCandidate verifies that a failing
// convention in baseline that disappears from candidate counts as resolved.
func TestComputeAuditDiff_ConventionRemovedFromCandidate(t *testing.T) {
	baseline := makeBaseline(map[string]map[string]bool{
		"lucas42/repo_a": {"old-convention": false},
	})
	// old-convention is not in candidate at all.
	candidate := makeCandidate(map[string]map[string]*bool{
		"lucas42/repo_a": {},
	})

	diff := ComputeAuditDiff(baseline, candidate)

	if len(diff.ResolvedFailures) != 1 {
		t.Fatalf("expected 1 resolved failure for removed convention, got %d", len(diff.ResolvedFailures))
	}
	if diff.ResolvedFailures[0].Convention != "old-convention" {
		t.Errorf("unexpected convention: %q", diff.ResolvedFailures[0].Convention)
	}
}

// TestComputeAuditDiff_MultipleRepos verifies handling of multiple repos.
func TestComputeAuditDiff_MultipleRepos(t *testing.T) {
	baseline := makeBaseline(map[string]map[string]bool{
		"lucas42/repo_a": {"conv-1": false},
		"lucas42/repo_b": {"conv-1": true},
		"lucas42/repo_c": {"conv-1": true},
	})
	candidate := makeCandidate(map[string]map[string]*bool{
		"lucas42/repo_a": {"conv-1": boolPtr(true)},  // resolved
		"lucas42/repo_b": {"conv-1": boolPtr(false)}, // new failure
		"lucas42/repo_c": {"conv-1": boolPtr(true)},  // unchanged
	})

	diff := ComputeAuditDiff(baseline, candidate)

	if len(diff.NewFailures) != 1 || diff.NewFailures[0].Repo != "lucas42/repo_b" {
		t.Errorf("expected 1 new failure for repo_b, got %v", diff.NewFailures)
	}
	if len(diff.ResolvedFailures) != 1 || diff.ResolvedFailures[0].Repo != "lucas42/repo_a" {
		t.Errorf("expected 1 resolved failure for repo_a, got %v", diff.ResolvedFailures)
	}
	if diff.Unchanged != 1 {
		t.Errorf("expected 1 unchanged, got %d", diff.Unchanged)
	}
}

// TestFormatAuditDiff_NoChanges verifies the formatted output for no changes.
func TestFormatAuditDiff_NoChanges(t *testing.T) {
	diff := AuditDiff{Unchanged: 42}
	output := FormatAuditDiff(diff)

	if !strings.Contains(output, "Findings unchanged: 42") {
		t.Errorf("expected unchanged count in output, got:\n%s", output)
	}
	if !strings.Contains(output, "No changes to audit findings") {
		t.Errorf("expected no-changes message in output, got:\n%s", output)
	}
}

// TestFormatAuditDiff_WithNewFailures verifies the formatted output includes failure table.
func TestFormatAuditDiff_WithNewFailures(t *testing.T) {
	diff := AuditDiff{
		NewFailures: []DiffEntry{
			{Repo: "lucas42/repo_a", Convention: "conv-1", Detail: "missing file"},
		},
		BaselineFetchedAt: "2026-03-19T10:00:00Z",
		CandidateBranch:   "fix-something",
	}
	output := FormatAuditDiff(diff)

	if !strings.Contains(output, "New failures (1)") {
		t.Errorf("expected failure section header, got:\n%s", output)
	}
	if !strings.Contains(output, "lucas42/repo_a") {
		t.Errorf("expected repo name in output, got:\n%s", output)
	}
	if !strings.Contains(output, "conv-1") {
		t.Errorf("expected convention in output, got:\n%s", output)
	}
	if !strings.Contains(output, "missing file") {
		t.Errorf("expected detail in output, got:\n%s", output)
	}
	if !strings.Contains(output, "2026-03-19T10:00:00Z") {
		t.Errorf("expected baseline timestamp in output, got:\n%s", output)
	}
	if !strings.Contains(output, "fix-something") {
		t.Errorf("expected branch name in output, got:\n%s", output)
	}
}

// TestFormatAuditDiff_WithResolvedFailures verifies the formatted output includes resolved table.
func TestFormatAuditDiff_WithResolvedFailures(t *testing.T) {
	diff := AuditDiff{
		ResolvedFailures: []DiffEntry{
			{Repo: "lucas42/repo_a", Convention: "conv-2"},
			{Repo: "lucas42/repo_b", Convention: "conv-2"},
		},
	}
	output := FormatAuditDiff(diff)

	if !strings.Contains(output, "Resolved failures (2)") {
		t.Errorf("expected resolved section header, got:\n%s", output)
	}
}

// TestFormatAuditDiff_SkippedWarning verifies skipped checks appear in summary.
func TestFormatAuditDiff_SkippedWarning(t *testing.T) {
	diff := AuditDiff{
		SkippedInCandidate: 3,
		Unchanged:          10,
	}
	output := FormatAuditDiff(diff)

	if !strings.Contains(output, "Checks skipped (API errors): 3") {
		t.Errorf("expected skipped count in output, got:\n%s", output)
	}
}
