package main

import (
	"encoding/json"
	"fmt"
	"os"
	"sort"
	"strings"
)

// DiffEntry represents a single finding that changed between baseline and candidate.
type DiffEntry struct {
	Repo       string
	Convention string
	Detail     string
}

// AuditDiff is the result of comparing a baseline StatusReport against a
// candidate DryRunReport.
type AuditDiff struct {
	// NewFailures are conventions that pass in baseline but fail in candidate.
	// These are unexpected regressions.
	NewFailures []DiffEntry

	// ResolvedFailures are conventions that fail in baseline but pass in candidate.
	// These are fixes — the PR is resolving known violations.
	ResolvedFailures []DiffEntry

	// Unchanged is the count of findings that are the same in both reports.
	Unchanged int

	// SkippedInCandidate is the count of checks that could not be completed
	// in the dry-run (API errors). Excluded from comparison.
	SkippedInCandidate int

	// BaselineFetchedAt is the timestamp the baseline was fetched, if known.
	BaselineFetchedAt string

	// CandidateBranch is the name of the PR branch, if known.
	CandidateBranch string
}

// baselineConvStatus mirrors ConventionStatus for JSON decoding of StatusReport.
type baselineConvStatus struct {
	Pass   bool   `json:"pass"`
	Detail string `json:"detail"`
}

// baselineRepoStatus mirrors RepoStatus for JSON decoding of StatusReport.
type baselineRepoStatus struct {
	Conventions map[string]baselineConvStatus `json:"conventions"`
}

// baselineReport mirrors StatusReport for JSON decoding.
type baselineReport struct {
	Repos map[string]baselineRepoStatus `json:"repos"`
}

// ComputeAuditDiff compares a baseline StatusReport (from /api/status) against
// a candidate DryRunReport. It returns the diff.
func ComputeAuditDiff(baseline baselineReport, candidate DryRunReport) AuditDiff {
	diff := AuditDiff{}

	// For every repo+convention in the candidate, compare against baseline.
	for repoName, candidateRepo := range candidate.Repos {
		for conventionID, candidateConv := range candidateRepo.Conventions {
			if candidateConv.Skipped {
				diff.SkippedInCandidate++
				continue
			}

			baselineRepo, repoInBaseline := baseline.Repos[repoName]
			var baselinePass bool
			var baselineExists bool
			if repoInBaseline {
				if baselineConv, ok := baselineRepo.Conventions[conventionID]; ok {
					baselinePass = baselineConv.Pass
					baselineExists = true
				}
			}

			if !baselineExists {
				// Convention or repo not in baseline — treat as was-passing (new result).
				baselinePass = true
			}

			if baselinePass && !candidateConv.Pass {
				// Was passing, now failing — new failure.
				diff.NewFailures = append(diff.NewFailures, DiffEntry{
					Repo:       repoName,
					Convention: conventionID,
					Detail:     candidateConv.Detail,
				})
			} else if !baselinePass && candidateConv.Pass {
				// Was failing, now passing — resolved.
				diff.ResolvedFailures = append(diff.ResolvedFailures, DiffEntry{
					Repo:       repoName,
					Convention: conventionID,
				})
			} else {
				diff.Unchanged++
			}
		}
	}

	// Also check for repos/conventions in baseline that disappeared from candidate
	// (e.g. repo archived, convention removed).
	for repoName, baselineRepo := range baseline.Repos {
		for conventionID, baselineConv := range baselineRepo.Conventions {
			candidateRepo, repoInCandidate := candidate.Repos[repoName]
			if !repoInCandidate {
				// Repo disappeared — skip, not actionable for diff.
				continue
			}
			if _, convInCandidate := candidateRepo.Conventions[conventionID]; convInCandidate {
				// Already handled above.
				continue
			}
			// Convention was in baseline but not in candidate (convention removed or excluded).
			if !baselineConv.Pass {
				// Was failing and now not checked — count as resolved.
				diff.ResolvedFailures = append(diff.ResolvedFailures, DiffEntry{
					Repo:       repoName,
					Convention: conventionID,
				})
			}
		}
	}

	// Sort for deterministic output.
	sort.Slice(diff.NewFailures, func(i, j int) bool {
		if diff.NewFailures[i].Convention != diff.NewFailures[j].Convention {
			return diff.NewFailures[i].Convention < diff.NewFailures[j].Convention
		}
		return diff.NewFailures[i].Repo < diff.NewFailures[j].Repo
	})
	sort.Slice(diff.ResolvedFailures, func(i, j int) bool {
		if diff.ResolvedFailures[i].Convention != diff.ResolvedFailures[j].Convention {
			return diff.ResolvedFailures[i].Convention < diff.ResolvedFailures[j].Convention
		}
		return diff.ResolvedFailures[i].Repo < diff.ResolvedFailures[j].Repo
	})

	return diff
}

// FormatAuditDiff formats an AuditDiff as a Markdown string suitable for
// posting as a GitHub PR comment.
func FormatAuditDiff(diff AuditDiff) string {
	var sb strings.Builder

	sb.WriteString("## Audit dry-run diff\n\n")

	if diff.BaselineFetchedAt != "" {
		sb.WriteString(fmt.Sprintf("**Baseline:** production (fetched %s)\n", diff.BaselineFetchedAt))
	} else {
		sb.WriteString("**Baseline:** production\n")
	}
	if diff.CandidateBranch != "" {
		sb.WriteString(fmt.Sprintf("**Branch:** %s\n", diff.CandidateBranch))
	}
	sb.WriteString("\n")

	sb.WriteString("### Summary\n\n")
	sb.WriteString(fmt.Sprintf("- Findings unchanged: %d\n", diff.Unchanged))
	sb.WriteString(fmt.Sprintf("- New failures: %d\n", len(diff.NewFailures)))
	sb.WriteString(fmt.Sprintf("- Resolved failures: %d\n", len(diff.ResolvedFailures)))
	if diff.SkippedInCandidate > 0 {
		sb.WriteString(fmt.Sprintf("- Checks skipped (API errors): %d\n", diff.SkippedInCandidate))
	}
	sb.WriteString("\n")

	if len(diff.NewFailures) > 0 {
		sb.WriteString(fmt.Sprintf("### New failures (%d)\n\n", len(diff.NewFailures)))
		sb.WriteString("| Repo | Convention | Detail |\n")
		sb.WriteString("|---|---|---|\n")
		for _, e := range diff.NewFailures {
			detail := e.Detail
			if detail == "" {
				detail = "-"
			}
			sb.WriteString(fmt.Sprintf("| %s | %s | %s |\n", e.Repo, e.Convention, detail))
		}
		sb.WriteString("\n")
	}

	if len(diff.ResolvedFailures) > 0 {
		sb.WriteString(fmt.Sprintf("### Resolved failures (%d)\n\n", len(diff.ResolvedFailures)))
		sb.WriteString("| Repo | Convention |\n")
		sb.WriteString("|---|---|\n")
		for _, e := range diff.ResolvedFailures {
			sb.WriteString(fmt.Sprintf("| %s | %s |\n", e.Repo, e.Convention))
		}
		sb.WriteString("\n")
	}

	if len(diff.NewFailures) == 0 && len(diff.ResolvedFailures) == 0 {
		sb.WriteString("_No changes to audit findings._\n")
	}

	return sb.String()
}

// runAuditDiff reads two JSON files (baseline and candidate), computes a diff,
// and writes the formatted Markdown report to stdout.
func runAuditDiff(baselinePath, candidatePath, fetchedAt, branch string) {
	// Read baseline.
	baselineData, err := os.ReadFile(baselinePath)
	if err != nil {
		fmt.Fprintf(os.Stderr, "error: failed to read baseline file %q: %v\n", baselinePath, err)
		os.Exit(1)
	}
	var baseline baselineReport
	if err := json.Unmarshal(baselineData, &baseline); err != nil {
		fmt.Fprintf(os.Stderr, "error: failed to parse baseline JSON: %v\n", err)
		os.Exit(1)
	}
	if baseline.Repos == nil {
		baseline.Repos = map[string]baselineRepoStatus{}
	}

	// Read candidate.
	candidateData, err := os.ReadFile(candidatePath)
	if err != nil {
		fmt.Fprintf(os.Stderr, "error: failed to read candidate file %q: %v\n", candidatePath, err)
		os.Exit(1)
	}
	var candidate DryRunReport
	if err := json.Unmarshal(candidateData, &candidate); err != nil {
		fmt.Fprintf(os.Stderr, "error: failed to parse candidate JSON: %v\n", err)
		os.Exit(1)
	}
	if candidate.Repos == nil {
		candidate.Repos = map[string]DryRunRepoStatus{}
	}

	diff := ComputeAuditDiff(baseline, candidate)
	diff.BaselineFetchedAt = fetchedAt
	diff.CandidateBranch = branch

	fmt.Print(FormatAuditDiff(diff))
}

