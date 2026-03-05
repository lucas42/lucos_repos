# ADR-0002: Audit Issue Lifecycle

**Date:** 2026-03-05
**Status:** Accepted
**Issue:** [#30](https://github.com/lucas42/lucos_repos/issues/30)

## Context

ADR-0001 established that the lucos_repos audit tool raises GitHub issues when conventions fail, but deferred the design of how those issues interact with GitHub's auto-close behaviour.

The tension arises because GitHub automatically closes issues when a PR with a closing keyword (e.g. `Closes #N`) is merged. This applies to human merges natively, and some repos (e.g. lucos_photos) have a workflow that replicates it for bot merges. The audit tool cannot prevent this, and should not try to.

The question is: what should the audit tool do on its next sweep after an issue has been closed -- whether by a PR, by a human, or by any other means?

Several edge cases need clear answers:

1. A PR claims to fix the violation but does not actually fix it.
2. Someone manually closes the issue without fixing the violation.
3. A previously-passing convention starts failing again after its issue was closed.
4. A convention genuinely does not apply to a specific repo and someone wants to suppress the issue permanently.

## Decision

### Principle: the audit result is the source of truth

The audit tool does not care about issue state. It cares about one thing: does the convention pass or fail right now? The issue tracker is a notification mechanism, not the canonical record of compliance.

### Sweep behaviour

On each audit sweep, for each repo + convention pair:

| Convention result | Open issue exists? | Action |
|---|---|---|
| Pass | No | Do nothing |
| Pass | Yes | Do nothing |
| Fail | Yes (open) | Do nothing |
| Fail | No (none or only closed) | Create a new issue |

When a convention passes, the audit tool takes no action regardless of whether there are open or closed issues for that convention. It does not comment "now passing" on existing issues -- that would be noise. If someone wants to know whether a convention is currently passing, they check the dashboard or API, not the issue history.

### New issues, not reopened ones

When a convention fails and no open issue exists, the audit tool always creates a **new** issue rather than reopening a previously closed one. The new issue references the old one for context (e.g. "This convention was previously addressed in #N but is failing again").

Reopening was rejected because:

- A reopened issue makes it look like the closing PR was wrong. That may not be the case -- the convention could have regressed for an entirely different reason.
- Reopened issues lose their place in triage workflows. A new issue enters the normal flow.
- The GitHub timeline becomes ambiguous: "closed by PR #X" followed by "reopened by bot" does not clearly communicate whether the original fix was inadequate or a new violation occurred.

### Identifying audit-raised issues

Every issue created by the audit tool carries an `audit-finding` label. This serves two purposes:

1. **Efficient search.** The audit tool queries for its own open issues using `label:audit-finding is:open` combined with a convention identifier (either a second label or a standardised title prefix).
2. **Visibility.** Humans and other agents can see at a glance which issues were raised by the audit tool versus filed manually.

The convention identifier in each issue (whether via label or title pattern) is left as an implementation detail for the developer to decide when building [#28](https://github.com/lucas42/lucos_repos/issues/28).

### No suppression mechanism

The original design proposal included an `audit-suppressed` label that could be applied to closed issues to prevent the audit tool from re-raising them. lucas42 rejected this:

> Having a suppression list available will always cause temptation to use it, when really we'd like to see issues fixed. If there's cases where something genuinely shouldn't apply, then I'd prefer we update the audit logic to handle that nuance, rather than tagging exceptions on a case-by-case basis.

This means: if a convention genuinely does not apply to a specific repo, the convention's `Check` function must encode that logic (e.g. "skip repos without a Dockerfile" for Docker conventions). There is no external override mechanism.

The accepted risk is that someone who manually closes an audit-raised issue without fixing the violation will see a new issue created on the next sweep. This is intentional -- it reinforces that the audit result, not human intent, drives issue creation.

### Interaction with GitHub auto-close

The audit tool does not need to know or care how an issue was closed. All closure paths -- PR merge with closing keyword, manual close, bot merge with the close-linked-issues workflow -- are treated identically. The audit tool only ever checks for open issues on the next sweep.

This means:

- **Successful fix via PR:** PR merges, GitHub closes the issue. Next sweep finds the convention passing. No action needed. This is the happy path.
- **Incomplete fix via PR:** PR merges, GitHub closes the issue, but the convention still fails. Next sweep finds no open issue and creates a new one. The system self-heals within one sweep cycle (6 hours maximum).
- **Manual close without fix:** Same as incomplete fix -- the next sweep creates a new issue.
- **Regression after fix:** Convention passes for a while, then fails again. The old issue is closed. The sweep creates a new one referencing the old issue.

In all cases, the worst-case detection latency is one sweep interval (6 hours).

## Consequences

### Positive

- **Simple implementation.** The sweep logic has exactly two code paths: create an issue, or do nothing. There is no state machine for issue lifecycle, no reopening logic, no suppression checks.
- **Self-healing.** Incomplete fixes, manual closes, and regressions are all handled identically and automatically. No human intervention is needed to "fix" the issue tracker.
- **Works with any merge workflow.** The design is agnostic to whether PRs are merged by humans, bots, or auto-merge workflows. It does not depend on any specific GitHub automation being in place.
- **Clean issue history.** Each issue represents a distinct occurrence of a violation, with its own timeline, linked PRs, and resolution. No ambiguity from reopened issues.

### Negative

- **Duplicate issues over time.** A convention that regresses repeatedly will produce multiple closed issues. This is a trade-off for timeline clarity -- the alternative (reopening) was judged worse.
- **No accepted-risk workflow.** There is no way to permanently dismiss a finding without changing the audit code. For edge cases where a convention genuinely does not apply, someone must update the convention's `Check` function. This is intentional but adds friction compared to a label-based suppression mechanism.
- **6-hour self-healing lag.** An incomplete fix that closes an issue will not be re-raised until the next sweep. During that window, the dashboard shows the violation but there is no open issue tracking it.
- **No "now passing" notification.** When a violation is fixed, the audit tool does not comment on or update the issue. The person who fixed it must check the dashboard or wait for the issue to not reappear. This keeps the issue tracker clean but reduces feedback immediacy.

### Changes to existing issues

Issue [#28](https://github.com/lucas42/lucos_repos/issues/28) (Raise GitHub issues for failing conventions) previously specified that the audit tool should comment on existing issues when a convention passes. This ADR supersedes that requirement: the audit tool does nothing when a convention passes. A comment has been posted on #28 to reflect this change.

ADR-0001's note that "the interaction between audit-raised issues and GitHub's auto-close behaviour requires separate design work" is now resolved by this ADR.
