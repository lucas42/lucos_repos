# CLAUDE.md — lucos_repos

## Code Review: Audit Dry-Run Diff

When a PR is opened on this repo, a GitHub Actions workflow runs a dry-run audit sweep using the PR's convention code and posts a diff comment comparing the results against the current production baseline.

### For code reviewers

Before completing a review on any PR in this repo, **wait for the audit dry-run diff comment** to be posted. Then assess whether the diff matches expectations given the issue being implemented:

- **False-positive fix** (e.g. fixing a convention checker bug): the diff should show resolved failures for the affected repos. If the failure count stays the same or new failures appear, the fix may be incomplete or may have uncovered another issue underneath.
- **New convention**: the diff should show new failures only for repos that genuinely violate the convention. A suspiciously high number of new failures may indicate the convention logic is too broad or has a bug.
- **Bug fix or refactor that does not change convention logic**: the diff should show zero changes. Any unexpected new or resolved failures indicate an unintended side effect.

If the diff shows unexpected results, request changes with an explanation of what was expected versus what the diff showed.

If the dry-run workflow has not run yet (e.g. the workflow file doesn't exist yet because the PR is adding it), note this in your review but do not block on it.
