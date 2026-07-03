# ADR-0008: Relocate the generated C4 output to a dedicated repo, with active write-failure monitoring

**Date:** 2026-07-03
**Status:** Proposed (awaiting lucas42 sign-off on the PR that introduces this ADR)
**Issue:** [#444](https://github.com/lucas42/lucos_repos/issues/444)
**Related:** [ADR-0006](0006-c4-estate-model.md) (the C4 estate model — this ADR amends its §5 output location), [ADR-0001](0001-greenfield-rewrite-as-convention-auditor.md) (establishes `lucos_repos` as the static-config reader), [ADR-0004](0004-auto-close-audit-finding-issues.md) (audit-finding issue mechanism)

## Context

ADR-0006 established a generated C4 model of the estate and (its §5) committed the generated artifacts — `model.dsl`, `landscape.md`, `divergences.md` — under `docs/c4/` **in `lucos_repos` itself**, so the artifact's git history serves as the estate's architecture changelog.

Two problems surfaced once generation was wired into the sweep:

1. **Write scope is broader than the output warrants.** The write-back needs a GitHub App with `contents: write` on the target repo. If the target is `lucos_repos`, that App holds write on the repo that also contains the auditor's own Go source, CI config and ADRs — far more blast radius than a set of disposable generated documents justifies. lucas42's explicit steer on [#444](https://github.com/lucas42/lucos_repos/issues/444) was to avoid granting broad write; the same least-privilege logic applies *within* `lucos_repos`.

2. **Write failures were invisible.** The commit step failed on every sweep with `403 Resource not accessible by integration` (the estate-read `lucos-repo-audit` App lacks `contents: write`), and the failure was **logged, not propagated** (the gap noted in [#424](https://github.com/lucas42/lucos_repos/issues/424)). So the committed `docs/c4/` froze at the 2026-06-14 prototype snapshot for days — still showing the decommissioned `lucos_authentication` as live — with no signal. This is precisely the rot ADR-0006's "generated, not hand-maintained" goal was meant to prevent, defeated by a silent write failure.

The generation logic itself is sound and unchanged; what changes is **where the output goes** and **how a write failure is surfaced**.

## Decision

Amend ADR-0006 on two points. Everything else in ADR-0006 (source→edge mapping, modelling rules, phasing, divergence-as-audit) stands.

### 1. The generated output relocates to a dedicated, output-only repo

The C4 artifacts (`model.dsl`, `landscape.md`, `divergences.md`) are written to a new repo, **`lucas42/lucos_architecture_models`**, which contains *only* generated C4 output plus a README. **Generation logic stays in `lucos_repos`** — the reasons ADR-0006 gives for `lucos_repos` being the generator's home (it already clones every repo, reads configy estate-wide, and owns the audit-finding machinery) are unaffected by where the output lands. This supersedes ADR-0006 §5's "committed under `docs/c4/` in `lucos_repos`".

`lucos_architecture_models` is an **output sink, not a founded system**: it has no service, no Docker/CI-deploy scaffold, no stack decision, and therefore **no founding ADR-0001 of its own**. It is a README plus three machine-written files.

### 2. Write-back authenticates as a scoped write App

The write-back uses a dedicated GitHub App, **`lucos-architecture-writer`**, installed on `lucos_architecture_models` **only**. The estate-read `lucos-repo-audit` App is unchanged and remains read-only across the estate. The write App's blast radius is exactly the disposable output — nothing executable, nothing sensitive, no auditor source.

### 3. Write failures are reported to schedule-tracker as a fail

A failed C4 write-back is reported to schedule-tracker as a **fail**, not merely logged. This supersedes ADR-0006's "committed generated output goes stale between sweeps … the seed artifact is dated and labelled accordingly" acceptance of silent staleness, and closes the [#424](https://github.com/lucas42/lucos_repos/issues/424) logged-not-propagated gap that caused [#444](https://github.com/lucas42/lucos_repos/issues/444). A dedicated C4 signal (distinct from the main audit-sweep report) is preferred, so a red state points directly at "C4 output stale".

### 4. The stale in-repo artifacts are removed

Once the first cross-repo sweep succeeds, the generated files under `docs/c4/` in `lucos_repos` are deleted. The prototype generator (`docs/c4/prototype-generator.py`) and the generation code stay; only the generated *output* leaves. The `lucos_repos` README/CLAUDE.md points readers at `lucos_architecture_models`.

## Consequences

### Positive

- **Least privilege for the write App.** `lucos-architecture-writer` holds write only on a repo of disposable artifacts — the tightest scope available, and the natural finish to "don't grant broad write".
- **Machine-vs-hand separation.** #444 happened because generated output sat in a code repo and silently went stale. A repo that is *only* machine-written, labelled "generated — do not hand-edit", removes the drift temptation and stops muddling generated files with hand-committed ones (`README.md`, `prototype-generator.py`).
- **The changelog property is preserved and sharpened.** ADR-0006's "git history of the artifact is the architecture changelog" still holds — and in a dedicated repo the *entire* history is that changelog, uncluttered by auditor-code commits.
- **Failures are now loud.** The write-failure-as-schedule-tracker-fail signal means the model can no longer freeze unnoticed — the specific failure mode of #444.
- **A natural home for later rendering/publishing** (Structurizr, GitHub Pages) without adding surface to the auditing tool. Out of scope here; just the door it opens.

### Negative / limitations

- **One more repo.** Mitigated: it is *not a service* — no deploy scaffold, no founding ADR, no stack. A README plus three generated files plus the write App installed on it. Minimal standing cost.
- **The sweep now writes cross-repo.** Mitigated: it was already a `Contents` PUT; only the hardcoded repo+path in `src/c4.go` changes, and the new App is scoped to that repo anyway.
- **Mild indirection for anyone reading the DSL in a `lucos_repos` checkout.** Mitigated: the model is meant to be browsed/rendered, not read as raw DSL in a code checkout, and a dedicated repo is arguably more discoverable for the onboarding/impact-analysis purpose ([#422](https://github.com/lucas42/lucos_repos/issues/422)); the `lucos_repos` README/CLAUDE.md points at it.
- **The write credential lives in production only.** `LUCOS_ARCHITECTURE_WRITER_PEM` is stored in the `lucos_repos` production environment, so the cross-repo write is only exercisable post-deploy — agents can't verify it in dev. This is *why* consequence-4's schedule-tracker signal matters: it is the mechanism that makes a post-deploy write failure visible.

## Follow-up actions

Both are tracked GitHub issues (raised on scoping, cross-referencing this ADR and [#444](https://github.com/lucas42/lucos_repos/issues/444)):

1. **Retarget the C4 write-back** to `lucas42/lucos_architecture_models` using the `lucos-architecture-writer` App; seed the new repo's README; remove the stale `docs/c4/` output; point `lucos_repos` docs at the new location — [#446](https://github.com/lucas42/lucos_repos/issues/446).
2. **Report C4 write-back failures to schedule-tracker as a fail** (dedicated signal preferred), closing the [#424](https://github.com/lucas42/lucos_repos/issues/424) gap — [#445](https://github.com/lucas42/lucos_repos/issues/445). Recommended to land before/with the retarget, so a broken cross-repo write goes red immediately rather than silently.
