# ADR-0004: Auto-close audit-finding issues when conventions pass

**Date:** 2026-04-02
**Status:** Proposed
**Issue:** [#248](https://github.com/lucas42/lucos_repos/issues/248)
**Amends:** [ADR-0002](0002-audit-issue-lifecycle.md)

## Context

ADR-0002 established that the audit tool should take no action when a convention passes, regardless of whether an open issue exists for it. The rationale was simplicity: the sweep logic had exactly two code paths (create an issue, or do nothing), and the issue tracker was treated as a fire-and-forget notification channel rather than a managed lifecycle.

In practice, this has led to a growing backlog of stale `audit-finding` issues. As of 2026-04-02, there were 15 open issues for conventions that now pass on the dashboard. These stale issues:

- Reduce trust in the issue tracker as a source of truth
- Create noise for humans and agents triaging open issues
- Require periodic manual cleanup that nobody remembers to do

The original ADR-0002 decision anticipated that issues would be closed via PR merge (with closing keywords) or manual intervention. This works for the happy path — a human fixes the violation, merges a PR with `Closes #N`, and the issue is closed. But many violations are resolved indirectly: a convention check is refined, a configy entry is updated, or the fix comes from a different repo. In these cases, there is no PR to close the issue, and it stays open indefinitely.

## Decision

### Amend the sweep behaviour table from ADR-0002

The sweep behaviour for the "Pass + Open issue exists" case changes from "Do nothing" to "Close the issue with a comment":

| Convention result | Open issue exists? | Action |
|---|---|---|
| Pass | No | Do nothing |
| Pass | Yes | **Close issue with comment** |
| Fail | Yes (open) | Do nothing |
| Fail | No (none or only closed) | Create a new issue |

### Close on first pass

When the audit sweep finds that a convention passes and an open `audit-finding` issue exists for that repo+convention pair, it closes the issue immediately. No consecutive-pass threshold is required.

The rationale for simplicity:

- The existing sweep already aborts entirely if any convention checks are indeterminate due to API errors (`skippedCount > 0`). This means a "pass" result only occurs when the check ran successfully and returned a definitive result — not when data was missing or an API call failed.
- If a convention does flap (pass then fail on subsequent sweeps), the existing "create a new issue" path handles re-opening cleanly. ADR-0002's decision to always create new issues rather than reopen old ones means the churn cost is a new issue, not a confusing reopened one.
- Adding a consecutive-pass requirement would need schema changes (`first_passed_at` column) and more complex sweep logic. This can be added later if transient-pass churn turns out to be a real problem.

### Comment before closing

The tool posts a comment explaining why the issue was closed before closing it. The comment should include:

- That the convention now passes
- The timestamp of the sweep that observed the pass
- A note that the issue can be reopened if the closure was premature

### Issue URL preservation during the sweep

Currently, `SaveFinding` overwrites `issue_url` with `""` when a convention passes. The closing logic must run **before** `SaveFinding` so it can still find and close the issue via `findOpenIssue`. After successful closure, `SaveFinding` proceeds as before (storing `issue_url = ""`).

No schema changes are needed for this approach — `findOpenIssue` searches GitHub by title, not by stored URL.

### Production-only guard

Like issue creation, auto-closing only runs when `ENVIRONMENT == "production"`. Non-production environments log that closing was skipped.

## Consequences

### Positive

- **Self-maintaining issue tracker.** Audit-finding issues now have a complete lifecycle managed by the tool. No manual cleanup is needed.
- **Feedback on fix.** When someone resolves a convention violation, the closing comment on the issue confirms the fix was detected. This addresses the "no now-passing notification" limitation acknowledged in ADR-0002.
- **Minimal complexity increase.** The sweep loop gains one new code path (close on pass), using the existing `findOpenIssue` method. No schema changes, no state machine, no consecutive-pass tracking.

### Negative

- **Potential churn from flapping conventions.** If a convention oscillates between pass and fail, the tool will close and recreate issues on alternate sweeps. The cost is manageable (each cycle produces one close comment and one new issue) but could be noisy for frequently-flapping conventions. If this becomes a problem, a consecutive-pass threshold can be added as a follow-up.
- **Closes issues that humans may have enriched.** If someone has added context, linked the issue to other work, or is using it to track a broader initiative, auto-closing could be disruptive. The closing comment makes the reason clear, and the issue can be reopened. This is judged preferable to leaving stale issues open.
- **Increased GitHub API usage per sweep.** For each passing convention with a previously-failing result, the sweep now calls `findOpenIssue` (a GET request) and potentially `CloseIssue` (a PATCH + POST). In steady state, most conventions pass consistently and have no open issue, so the additional API calls are proportional to the number of recently-resolved violations — typically a small number.

### Interaction with ADR-0002

ADR-0002's core principles remain intact:

- **The audit result is the source of truth.** This doesn't change — the database and dashboard remain authoritative.
- **New issues, not reopened ones.** Still true — when a convention fails again after being auto-closed, a new issue is created (not the old one reopened).
- **No suppression mechanism.** Still true — there is no way to prevent auto-closing. If a convention passes, its issue is closed.
- **Works with any merge workflow.** Still true — auto-closing is independent of how the violation was fixed.

The only change is the "Pass + Open issue" row in the behaviour table. ADR-0002 is otherwise unmodified.
