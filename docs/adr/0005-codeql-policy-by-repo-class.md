# ADR-0005: CodeQL policy for application-code repos vs infrastructure-only repos

**Date:** 2026-04-10
**Status:** Proposed
**Issue:** [#314](https://github.com/lucas42/lucos_repos/issues/314)
**Related:** [ADR-0002](0002-audit-issue-lifecycle.md) (audit-finding lifecycle — not amended)

## Context

During the 2026-04-10 routine, two `lucos_repos` audit findings were raised against `lucos_private` and `lucos_static_media` for stale required status checks (`Analyze (actions)`) that silently blocked all Dependabot auto-merges. The proximate fix was straightforward — remove the unsatisfiable required check — but in discussing the direction with `lucos-security`, a more fundamental gap surfaced: the estate has no written policy on **which repos should have PR-time CodeQL gating at all**, and the `required-status-checks-coherent` convention implicitly encodes a policy without anyone having decided it.

The lack of a policy causes two symmetric failures:

1. **False gate on empty targets.** `lucos_private` (shell scripts) and `lucos_static_media` (a Dockerfile) had `Analyze (actions)` required on `main`. Neither contains a CodeQL-supported language. The "gate" existed on paper but scanned nothing. When the underlying check stopped firing, Dependabot PRs got silently blocked for weeks. The cost was high; the security value was zero from day one.
2. **Missing gate on real code.** Conversely, a repo with real application code in a CodeQL-supported language but no required `Analyze (X)` check can merge security findings before CodeQL finishes. `required-status-checks-coherent` already flags this case, but only by coincidence — there is no explicit policy saying "PR gating is required for application-code repos".

The underlying classification already exists in code: `HasCodeQLLanguage()` in `conventions/conventions.go` checks whether the repo's GitHub-reported language mix contains any CodeQL-supported language (JavaScript, TypeScript, Python, Go, Java, C, C++, C#, Ruby, Kotlin, Swift). Both `has-codeql-workflow` and `required-status-checks-coherent` already use this helper as a precondition. What is missing is a written decision that says: this classification is the policy, and here's what each class should look like.

`lucos-security` articulated the recommendation in [lucos_private#32](https://github.com/lucas42/lucos_private/issues/32#issuecomment-4222499599) and [lucos_static_media#32](https://github.com/lucas42/lucos_static_media/issues/32#issuecomment-4222500157):

> Workflow-based CodeQL with PR gate on repos with application code in supported languages. Infrastructure-only repos (shell, config, Dockerfiles) get scheduled CodeQL at most — no PR gating needed.

This ADR adopts that recommendation as estate policy.

## Decision

### 1. Classification rule

A repository is classified as **application-code** if, and only if, the GitHub Languages API reports at least one CodeQL-supported language for that repository. Otherwise it is **infrastructure-only**.

The authoritative set of CodeQL-supported languages is `codeQLSupportedLanguages` in `conventions/conventions.go`. As of this ADR, that set is:

> JavaScript, TypeScript, Python, Go, Java, C, C++, C#, Ruby, Kotlin, Swift

When GitHub adds support for a new language, this set should be updated and the classification is re-evaluated automatically on the next audit sweep.

**Rationale for using GitHub's language detection rather than a hand-maintained list:**

- It is objective and reproducible. There is no judgement call about whether a repo "counts".
- It updates automatically as repos evolve. A repo that gains a Python helper script becomes application-code on the next sweep; a repo that loses its last Python file becomes infrastructure-only.
- It is the same signal CodeQL itself uses. If GitHub reports Python to us, GitHub will also offer to scan Python with CodeQL.
- The estate is already using this helper in two conventions — formalising it costs nothing.

**Edge case — trivial scripts in otherwise-infrastructure repos.** If an infrastructure-only repo gains a single small Python helper, GitHub will report Python and the repo will be reclassified as application-code. This is intentional: the moment application code exists in a CodeQL-supported language, the PR-time gate becomes meaningful, however small the code is. If the helper is genuinely trivial and the maintainer prefers not to add CodeQL, the correct response is to remove or relocate the helper — not to carve out an exception in the policy.

**Edge case — repos whose primary language is not CodeQL-supported.** Rust, Erlang, Elixir, PHP, and similar languages are not in CodeQL's supported set as of this ADR. A repo written exclusively in such a language is classified **infrastructure-only** by this rule, even though it contains real application code. This is not because the code has no security relevance — it is because CodeQL literally cannot scan it, so there is no PR-time gate to install. Security coverage for these repos must come from other mechanisms (dependency scanning, human review, language-specific linters). This ADR does not attempt to solve that problem; it only governs CodeQL policy.

### 2. Policy for application-code repos

Application-code repos must have:

- A `.github/workflows/codeql-analysis.yml` file with both `pull_request` and `schedule` triggers.
- An explicit `strategy.matrix.language` list covering every language that GitHub reports for the repo and that CodeQL supports.
- A required status check on `main` named `Analyze (<language>)` for each language in the matrix.

This preserves PR-time gating: a security finding blocks the merge, not just a scheduled email. The existing `has-codeql-workflow` and `codeql-workflow-security-settings` conventions already enforce the workflow file and its shape. The existing `required-status-checks-coherent` convention already flags the "missing required `Analyze (X)` check" case. No convention changes are required for this direction of the policy — it is already in force by accident. This ADR merely declares it intentional.

### 3. Policy for infrastructure-only repos

Infrastructure-only repos must not have a required `Analyze (<language>)` status check on `main`. Whether to keep a scheduled CodeQL workflow is optional and carries no obligation — scheduled CodeQL on an infrastructure-only repo runs on empty and has no security value, so removing it is encouraged but not mandated by this ADR.

**Rationale:** a required check on a target CodeQL cannot scan is a gate on nothing — it cannot catch vulnerabilities that don't exist in languages CodeQL doesn't understand, but it **can** silently block all Dependabot auto-merges when the check name drifts, which is exactly what happened in lucos_private and lucos_static_media. The security trade is unambiguous: zero detection value, non-zero operational cost.

**Residual risk:** if an infrastructure-only repo later gains application code, there is a window between the code being committed and the workflow + branch protection being updated during which the new code is not PR-gated. This is mitigated by:

- `has-codeql-workflow` runs on every sweep and will open an audit-finding issue as soon as a CodeQL-supported language appears.
- Code in a freshly-added language tends to be reviewed by a human in its first PR, which is a stronger gate than CodeQL for small amounts of new code.
- The scheduled CodeQL run (if retained) will catch issues on the next cron, bounded to at most a week.

`lucos-security` has reviewed and confirmed this residual risk is acceptable (sign-off on 2026-04-10).

### 4. Default for new repos

The standard repo-bootstrap flow must, for each new repo:

1. Detect whether the initial commit introduces a CodeQL-supported language.
2. If yes: add the CodeQL workflow with `pull_request` trigger, explicit matrix, and a required `Analyze (<language>)` status check from the first day.
3. If no: do not add any required CodeQL check. A scheduled-only CodeQL workflow is optional.

If a new repo changes class later (e.g. a config-only repo gains a Go service), the audit conventions will raise a finding and the maintainer adds the workflow. No bootstrap-time prediction is needed.

### 5. How `lucos_repos` conventions should reflect this

The three existing conventions (`has-codeql-workflow`, `codeql-workflow-security-settings`, `required-status-checks-coherent`) already implement the application-code side of the policy via `HasCodeQLLanguage()` preconditions. No changes are required for that direction.

For the infrastructure-only side, the policy adds one affirmative rule that is not currently checked: **an infrastructure-only repo must not have any required `Analyze (X)` check on main.** Without this check, the class of failure seen in lucos_private and lucos_static_media would not be detected proactively — it was only found when a human noticed Dependabot PRs stuck in the auto-merge queue.

A new convention — suggested name `no-stale-codeql-requirement-on-infra-repos` — would implement this by:

1. Fetching required status checks on `main`.
2. Checking whether any match the `Analyze (<language>)` pattern.
3. Fetching repo languages and calling `HasCodeQLLanguage()`.
4. Failing if and only if a required `Analyze (X)` check exists **and** `HasCodeQLLanguage()` returns false.

This convention is a direct companion to `required-status-checks-coherent`: the existing one ensures application-code repos have a required check; the new one ensures infrastructure-only repos do not. A follow-up issue should be raised against `lucos_repos` to implement it; this ADR does not introduce the convention itself.

An alternative considered was to extend `required-status-checks-coherent` with this additional rule instead of adding a new convention. That was rejected because ADR-0002 and the coherent-check design deliberately consolidate **three** failure modes that tended to cascade (stale names, missing CodeQL, Dependabot-unsatisfiable checks). Adding a fourth, symmetric rule ("wrongly-present CodeQL check") would blur the check's purpose — it is about internal consistency of the **existing** required checks, not about whether there should be any in the first place. A separate convention keeps each concern independently testable and independently fixable.

### 6. Migration plan (outline — not executed in this ADR)

Once this ADR is approved, an estate-rollout will:

1. Enumerate every active repo via `lucos_configy`.
2. For each repo, fetch GitHub's language mix and classify.
3. Build a report of application-code repos that are missing a required `Analyze (X)` check (should be empty if `required-status-checks-coherent` is passing estate-wide).
4. Build a report of infrastructure-only repos that currently have a required `Analyze (X)` check (this is the set the policy says to fix).
5. For each repo in list 4, remove the required check from branch protection. This is a `lucos-system-administrator` task and must go through the usual estate-rollout verification (audit dry-run diff in `lucos_repos`).
6. Close the two audit findings on lucos_private and lucos_static_media once their required check is removed — both of those repos will then pass `required-status-checks-coherent`.

The migration is scoped as a separate issue and is explicitly **not** part of this ADR's implementation. This ADR is the policy decision; the migration is how the policy gets applied to the existing estate.

## Consequences

### Positive

- **Dependabot auto-merge stops being silently blocked** by stale `Analyze (X)` checks on repos that don't have the language. This was the concrete incident that surfaced the problem.
- **Written source of truth.** Auditors, sysadmins, and developers adding workflow files all point at the same document. `required-status-checks-coherent`'s behaviour becomes intentional policy rather than an implicit side effect.
- **Classification is self-maintaining.** The rule is mechanical and updates with each audit sweep — no hand-curated list of repos to keep in sync.
- **Security coverage preserved where it matters.** Every repo CodeQL can actually scan retains PR-time gating. Nothing is weakened on any repo with application code in a supported language.
- **New infrastructure-only convention becomes possible.** The policy creates a precise, checkable rule for the infra-only direction, closing the detection gap.

### Negative

- **Classification depends on GitHub's language detection.** If GitHub misclassifies a file (e.g. counts a generated file as Python), a repo may be mis-bucketed. Mitigations: GitHub's language detection is well-tested and used by thousands of projects; and `.gitattributes` linguist overrides are available for the rare repo where it misfires.
- **Residual scheduled-only gap on infrastructure-only repos that later gain code.** Covered under Residual risk in section 3.
- **Two conventions now touch CodeQL required checks** (`required-status-checks-coherent` and the proposed `no-stale-codeql-requirement-on-infra-repos`), each covering the opposite direction. Their contracts are disjoint by construction (one fires only when `HasCodeQLLanguage()` is true, the other only when it is false), so there is no overlap or conflicting guidance — but future maintainers should be aware they are paired.
- **No policy for non-CodeQL languages.** Rust, Erlang, etc. get no PR-time SAST from this ADR. This is a known gap, not a regression — there was no PR-time SAST for those languages before either.

## Threat model considerations

This section is written for `lucos-security` review.

- **Can removing a required check on an infrastructure-only repo allow a malicious PR to merge without scanning?** No. CodeQL doesn't scan the languages in those repos. The removed check was not scanning anything and had no findings to report. A malicious PR on a shell-script repo would be caught (or not caught) by code review, not by CodeQL, with or without this policy.
- **Can a malicious contributor bypass CodeQL by committing a trivial shell file to an application-code repo and arguing for reclassification?** No. The classification fires on "at least one CodeQL-supported language present" — adding files in other languages does not remove supported languages. An application-code repo remains application-code as long as it contains any supported language.
- **Can a malicious contributor erase the CodeQL-supported language to reclassify the repo?** In principle yes, but such a change would be enormous (removing all Python/JS/Go/etc. code) and obviously suspicious. It would also fail the usual review gates. This is not a realistic attack surface.
- **Does the policy weaken defence-in-depth for any repo?** Only in the narrow sense that infrastructure-only repos lose a scheduled CodeQL run if the maintainer chooses to delete the workflow. Since scheduled CodeQL on an empty target produces no findings by definition, this is not a practical loss.
- **Is there any case where `HasCodeQLLanguage()` returning `false` is wrong?** The likely failure modes are GitHub language mis-detection (rare and correctable via `.gitattributes`) and CodeQL dropping support for a language previously scanned (GitHub announces these). Both are detectable by an audit sweep the day they happen.
- **Can GitHub's language detection misclassify an infrastructure-only repo as application-code?** Yes, if generated or vendored code in a CodeQL-supported language is present in the repo — e.g. a Dockerfile repo containing a vendored JavaScript build artifact. This is a false positive rather than a false negative (the repo is forced into stricter policy than it needs), so it does not let findings escape detection. It is correctable via `.gitattributes` linguist overrides marking the generated/vendored paths as `linguist-generated` or `linguist-vendored`, which removes them from GitHub's language statistics and re-classifies the repo as infrastructure-only on the next audit sweep.

If `lucos-security` identifies any scenario where the policy allows a finding to escape detection that the current implicit policy catches, that is a material concern and this ADR should be revised before being marked Accepted.
