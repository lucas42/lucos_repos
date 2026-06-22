# ADR-0007: A generated catalogue is the single source of truth for enforced conventions

**Date:** 2026-06-17
**Status:** Proposed (awaiting lucas42 sign-off on the PR that introduces this ADR)
**Issue:** [#436](https://github.com/lucas42/lucos_repos/issues/436)
**Related:** [ADR-0001](0001-greenfield-rewrite-as-convention-auditor.md) (establishes conventions as Go source of truth), [ADR-0006](0006-c4-estate-model.md) (the generate-from-source, committed-artifact precedent this follows)

## Context

The enforced conventions live as `Convention` structs in `conventions/*.go`. Each carries structured, human-readable fields — `Description`, `Rationale`, `Guidance`, `AppliesTo`, `ExcludeRepos` — which are already rendered into the issue bodies the auditor raises. This is the authoritative definition of every convention.

Separately, human-readable convention docs (notably the `lucos_claude_config` reference docs `circleci-conventions.md` and `docker-conventions.md`) **paraphrase** some of these rules in hand-written prose. A paraphrase can silently drift from the enforcement, and agents trust the doc over the source. Two incidents in a single session:

- An agent trusted an incomplete `docker-conventions.md` (a wrong "single-container uses `_app`" bullet) and raised a bogus rename ticket ([#154](https://github.com/lucas42/lucos_repos/issues/154)).
- An agent trusted an incomplete `circleci-conventions.md` (it omitted the **build** serial-group), removed a convention-required key, and broke `circleci-deploy-serial-group`; it reached `main` and the audit only caught it post-merge ([#177](https://github.com/lucas42/lucos_repos/issues/177)).

Two facts shape the fix:

1. **The docs are a *superset*, not a mirror, of the enforced conventions.** Drift is only possible at the *overlap* — the parts of a doc that paraphrase an enforced rule. The majority of each doc is genuinely un-enforceable guidance (CI templates, API-access runbooks, the Alpine DNS gotcha, the `FROM scratch` CA-bundle rule, the three-stage env-var wiring narrative, the live-restore recovery runbook, incident histories) with no enforcement counterpart and never will have one. "Generate the docs entirely from source" would discard that, and is wrong.
2. **Exhortation is not a control.** `circleci-conventions.md` already carried a "verify against the source first" banner on the serial-group section, and #177 still happened on a sibling doc. A banner relies on a human or agent obeying it; it is not a structural guarantee.

## Decision

The enforced subset gets a single source of truth that the docs **link to**, never re-state.

### 1. A generated catalogue, committed and version-controlled

`conventions.RenderCatalogue()` renders every registered convention (`conventions.All()`) as Markdown — `ID`, `Description`, `Rationale`, `Guidance`, `AppliesTo`, `ExcludeRepos`, `ScheduledOnly`. A `conventions` subcommand writes it to stdout; the committed copy lives at `docs/conventions.md`. Regenerate with:

```
go run ./src conventions > docs/conventions.md
```

This mirrors ADR-0006 (the C4 model: generated-from-source, committed as the model of record) — and is *simpler*, because the source is in-process Go data with no network fetches, so generation is deterministic and cannot flake.

### 2. A golden-file test makes drift structurally impossible

`TestConventionCatalogueIsCurrent` regenerates the catalogue in memory and fails if the committed `docs/conventions.md` differs. It runs inside the **existing** `go test ./...` CI job — no new workflow, no new orb job, no cross-repo CI coupling. A companion `TestAllConventionsHaveRequiredFields` fails the build if any convention ships without a `Description`, `Rationale`, or `Guidance`, so the catalogue (and the generated issues) can never contain a hollow entry.

### 3. The enforced-vs-guidance boundary (the governing principle)

This is the load-bearing decision, not the Markdown file:

> **Documentation must not paraphrase an enforced convention. For any rule defined in `conventions/*.go`, the docs link to the generated catalogue (or the specific `.go` file). Only genuinely un-enforceable guidance is hand-written — and it is kept in clearly-demarcated sections so a future editor knows which side of the boundary they are on.**

Removing the parallel hand-written copy of an enforced rule removes the surface on which it can drift. This principle binds the `lucos_claude_config` reference docs; their refactor to link rather than paraphrase is tracked in lucas42/lucos_claude_config#120 (Blocked on this catalogue landing, since it must link to a real surface).

### 4. Deliberately out of scope

- **No `GET /conventions` HTTP endpoint.** The consumers — agents reading the reference docs, and humans — are served by a committed `docs/conventions.md` at a stable GitHub URL. An endpoint adds a route, content negotiation, and tests for marginal value. It is an easy later addition if a live-service consumer ever materialises; it should not gate this work.
- **No prose-diff CI check against the hand-written docs.** The docs are English, not structured data; you cannot mechanically diff "single-container uses `_app`" against `container-naming.go` without parsing natural language. The single-source approach (link, don't paraphrase) removes the need entirely. A cheap link-*presence* lint could be a later secondary guard, but it is not the mechanism.

## Consequences

### Positive

- Drift between the docs and the enforcement, on the enforced subset, becomes **structurally impossible** rather than merely discouraged — the failure class behind #154 and #177 is closed.
- The catalogue is a single stable surface the whole estate's documentation can link to.
- The required-fields guard catches a convention shipped without a usable rationale or fix — improving issue quality as a side effect.
- Zero new CI surface; consistent with the repo's own ADR-0006 precedent, and cheaper than it.

### Negative / trade-offs (stated honestly)

- It introduces a **second committed artifact to keep in step** with the source. This is mitigated — but not eliminated — by the failing test; regeneration is a single command, and the test names it in its failure message.
- Readers must **follow a link** for enforced rules instead of reading inline prose. This is a real ergonomic cost, paid deliberately in exchange for non-drift.
- The boundary still requires **editorial discipline on the hand-written side**: nothing mechanically stops a future editor paraphrasing an enforced rule inside a "guidance" section. The test cannot police prose; this ADR and the section demarcation are the only guard there. This is a genuine residual risk, not a solved problem.
- The catalogue documents *what* is enforced, not the `Check` logic. Subtle check semantics still require reading the `.go`. Accepted: the catalogue is the contract surface, not the implementation.
