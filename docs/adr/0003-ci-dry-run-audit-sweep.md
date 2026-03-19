# ADR-0003: CI-integrated dry-run audit sweep

**Status:** Accepted
**Date:** 2026-03-19
**Author:** lucos-developer (prompted by lucas42 and lucos-architect design)

## Context

Two incidents exposed a structural risk in the audit tool:

1. **`circleci-jobs-in-required-checks` false positives (2026-03-18/19)**: A bug in the convention checker read only the legacy `contexts` field from the branch protection API, missing checks configured via the modern `checks` array. ~26 false-positive issues were raised across the org before the bug was identified.

2. **`in-lucos-configy` false positives (2026-03-17)**: A transient DNS failure during a sweep caused the configy lookup to return no data for every repo, generating ~54 false-positive issues. (This class of failure is separately addressed by aborting the sweep on configy fetch failure — see lucos_repos#147.)

Both incidents share a common structure: a convention check that should return "indeterminate" instead returned "failed", and the sweep had no mechanism to detect that a large batch of new issues was anomalous before creating them.

The previous proposed mitigation (a per-convention cap on new issues per sweep) was rejected as addressing the symptom rather than the cause. The agreed approach is to catch these bugs at PR review time, before the code is deployed to production.

## Decision

Add a CI-integrated dry-run audit sweep that runs on every PR to `main`. The dry-run:

1. Builds the `lucos_repos` binary from the PR branch.
2. Runs all convention checks against all live repos (using the real GitHub API and configy), but does **not** create any GitHub issues and does not write to any database.
3. Fetches the current production audit state from `https://repos.l42.eu/api/status`.
4. Computes a diff between the production state (baseline) and the dry-run output (candidate).
5. Posts the diff as a comment on the PR.

The code reviewer uses the diff to assess whether the PR's audit impact matches expectations:
- A bug fix should show resolved failures matching the known false positives, and no new failures.
- A new convention should show new failures only for repos that genuinely violate it.
- If a fix claims to resolve a false positive but the diff shows no resolved failures, the fix did not work (as happened in PR #154 for issue #150).

## CLI interface

The `lucos_repos` binary gains two CLI subcommands:

### `lucos_repos audit --dry-run`

Runs a full convention sweep without creating issues. Outputs a JSON report to stdout with structure:

```json
{
  "repos": {
    "lucas42/repo": {
      "type": "system",
      "conventions": {
        "convention-id": {"pass": true, "detail": "..."},
        "skipped-convention": {"skipped": true}
      },
      "compliant": true
    }
  },
  "summary": {"total_repos": 54, "compliant_repos": 50, "total_violations": 4},
  "skipped_checks": 0
}
```

`skipped_checks` is non-zero when API errors prevented some checks from completing. The CI job logs a warning and exits with code 2 in this case — the diff is still posted but the reviewer should treat it as potentially incomplete.

### `lucos_repos audit diff --baseline <file> --candidate <file> [--fetched-at <ts>] [--branch <name>]`

Reads two JSON files and produces a Markdown-formatted diff report. The baseline is expected to match the structure of `GET /api/status` (a `StatusReport`). The candidate is the output of `audit --dry-run` (a `DryRunReport`).

Output is written to stdout, suitable for posting directly as a GitHub PR comment.

## GitHub Actions workflow

A new workflow (`.github/workflows/audit-dry-run.yml`) runs on every PR targeting `main`. It uses two secrets:

- `AUDIT_APP_ID`: GitHub App ID for the audit app (same app used in production for read access).
- `AUDIT_APP_PEM`: GitHub App private key PEM for the same app.

These must be set in the repository secrets. The app requires read access to all repos in the `lucas42` org (same permissions the production sweep already uses).

## What this does NOT solve

- **Post-deployment infrastructure failures** (like the #147 configy DNS incident): these happen at runtime, not at PR time. The abort-on-configy-failure fix (lucos_repos#147) handles this class.
- **Convention bugs that only affect repos in specific transient states**: the dry-run checks against real repos at a point in time. A bug that only manifests when a repo is in an unusual state may not be caught by the dry-run.

## Consequences

- Every PR to `main` now runs convention checks against all live repos (~750–2250 GitHub API calls). This is within GitHub's rate limits and is a deliberate trade-off for earlier feedback.
- The audit diff comment is updated on each new commit to the PR branch — the previous comment is deleted before posting the new one, keeping the PR clean.
- The `lucos_repos` binary now supports CLI subcommands in addition to its server mode. The server mode is the default (invoked when `audit` is not the first argument).
