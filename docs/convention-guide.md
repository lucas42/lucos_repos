# Convention Quality Guide

This guide defines what makes a good convention definition in lucos_repos. Every convention produces real GitHub issues on real repositories, so every definition must be worth the attention cost of someone reading and acting on that issue.

## What a convention is (and isn't)

A convention is a rule that should hold true across all (or a defined subset of) lucos repositories. Conventions exist to catch drift from agreed infrastructure standards -- not to express opinions or preferences.

Every convention must have a clear, concrete rationale. If you cannot explain what goes wrong when a repository violates it, it should not be a convention.

Conventions are **not** the place for:
- Stylistic preferences (e.g. "use tabs not spaces")
- Aspirational goals (e.g. "should have good test coverage")
- Anything without a deterministic pass/fail check

## Where convention definitions live

Convention definitions are Go source files in the `conventions/` directory. Each convention goes in its own file, named after its ID. For example, a convention with ID `has-circleci-config` lives in `conventions/has-circleci-config.go`.

Each file registers its convention in an `init()` function by calling `conventions.Register(...)`. This self-registration pattern means adding a convention requires no changes to any central registry file -- the Go build system handles it.

## Required fields

Each convention is defined as a `Convention` struct with the following fields:

| Field | Type | Required | Purpose |
|---|---|---|---|
| `ID` | `string` | Yes | Short, stable, kebab-case identifier. Used in issue titles, database keys, and filenames. Must never change once deployed. |
| `Description` | `string` | Yes | Plain English explanation of what the convention checks. Appears in issue titles and bodies. |
| `Rationale` | `string` | Yes | Why this convention exists -- what goes wrong if it is violated. Appears in the generated issue body under "Why this matters". |
| `Guidance` | `string` | Yes | Suggested approaches to fix the violation. Appears in the generated issue body under "Suggested fix". |
| `AppliesTo` | `[]RepoType` | No | The set of repo types this convention applies to. If empty, it applies to all types. |
| `Check` | `func(RepoContext) ConventionResult` | Yes | The check function. Returns pass/fail and a human-readable detail string. |

### Field guidance

**ID**: Choose something descriptive and stable. The ID appears in issue titles as `[Convention] {id}: {description}`, is stored in the database, and is used as the filename in `conventions/`. Changing it after deployment would orphan existing issues and database records.

**Description**: This appears in the issue title alongside the ID. Keep it concise and factual -- it describes *what* is checked, not *why*. Example: "Repository has a .circleci/config.yml file".

**Rationale**: This is the most important field for issue quality. It answers the question a reader will have when they see the issue: "Why should I care?" Write it as if explaining to someone who is unfamiliar with the convention. Example: "Without a CircleCI config, changes to this repository are not automatically built, tested, or deployed. This means code changes require manual intervention to reach production."

**Guidance**: Concrete, actionable suggestions for fixing the violation. Reference specific files, templates, or documentation where possible. If there are cases where the convention should not apply, say so here and point the reader toward `AppliesTo`. Example: "Add a `.circleci/config.yml` following the standard lucos CI template. If this repository is intentionally not deployed, consider whether it should be excluded from this convention via `AppliesTo`."

**Check**: The function that performs the actual check. It receives a `RepoContext` (with the repo name, a GitHub token, and the repo type) and returns a `ConventionResult`. On failure, the `Detail` field should provide enough context for debugging -- not just "file not found" but what was expected and where.

## Good conventions vs bad conventions

### Good

```go
Convention{
    ID:          "has-circleci-config",
    Description: "Repository has a .circleci/config.yml file",
    Rationale:   "Without a CircleCI config, changes are not automatically built, tested, or deployed.",
    Guidance:    "Add a .circleci/config.yml following the standard lucos CI template.",
    Check:       func(repo RepoContext) ConventionResult { /* checks file existence */ },
}
```

Why this is good:
- Clear, binary check (file exists or it does not)
- Descriptive ID that matches the filename
- Rationale explains the real-world consequence
- Guidance tells the reader exactly what to do

### Bad

```go
Convention{
    ID:          "good-ci",
    Description: "Repository follows CI best practices",
    Check:       func(repo RepoContext) ConventionResult { /* ??? */ },
}
```

Why this is bad:
- "Best practices" is vague and subjective
- No clear pass/fail criteria -- what counts as "good"?
- Missing `Rationale` and `Guidance` -- the generated issue would be useless
- ID is too generic to be stable or meaningful

### Another bad example

```go
Convention{
    ID:          "well-documented",
    Description: "Repository is well-documented",
    Check:       func(repo RepoContext) ConventionResult { /* checks README exists? */ },
}
```

Why this is bad:
- "Well-documented" is subjective
- Even if narrowed to "has a README", the rationale is weak -- a repo can function perfectly without one
- Convention overhead (issue creation, triage, resolution) exceeds the value of the check

## When to scope conventions with AppliesTo

`AppliesTo` controls which repo types a convention applies to. The available types are:

- `RepoTypeSystem` -- repos that appear in configy's systems list (deployed services)
- `RepoTypeComponent` -- repos that appear in configy's components list (shared libraries, tools)
- `RepoTypeScript` -- repos that appear in configy's scripts list (tools designed to run locally, not deployed as services)
- `RepoTypeUnconfigured` -- repos not in configy at all
- `RepoTypeDuplicate` -- repos that appear in more than one configy category; this is a configuration error state and conventions should not normally target this type

An empty `AppliesTo` means "all repo types". This should be the exception, not the default. Most conventions are specific to deployed systems or to repos that are actively maintained.

Use `AppliesTo` to prevent noise. A convention that is correct for deployed systems but irrelevant for archived repos or documentation-only repos will generate issues that are immediately closed as not-applicable. That erodes trust in the tool and wastes everyone's time. Better to scope tightly from the start.

Example: a convention checking for Docker Compose healthchecks should apply only to systems, not to components or unconfigured repos:

```go
AppliesTo: []RepoType{RepoTypeSystem},
```

## Checklist for reviewing convention PRs

When reviewing a PR that adds or modifies a convention, verify:

- [ ] The convention file is in `conventions/` and named after the convention ID
- [ ] `ID` is descriptive, kebab-case, and stable (not likely to need renaming)
- [ ] `Description` clearly explains what is checked, without restating the ID
- [ ] `Rationale` explains why the convention matters -- what goes wrong if violated
- [ ] `Guidance` suggests concrete steps to fix violations, referencing templates or docs where possible
- [ ] `AppliesTo` is set appropriately (not left empty by default when a narrower scope would be correct)
- [ ] The `Check` function returns a meaningful `Detail` string on failure -- not just a generic error
- [ ] The convention has been considered against representative repos (both ones that should pass and ones that should fail)
- [ ] The convention does not duplicate or conflict with an existing convention
