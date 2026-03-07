# lucos_repos

Audits all lucos GitHub repositories against a set of coding conventions, raises GitHub issues for failures, and exposes a status API.

## Project structure

All Go source lives in the root directory (single `main` package):

| File | Purpose |
|---|---|
| `main.go` | HTTP server, startup, `/_info` and `/api/status` endpoints |
| `convention.go` | Convention registry, shared types (`Convention`, `RepoContext`, `ConventionResult`, `RepoType`), and GitHub API helpers |
| `audit.go` | Audit sweep — iterates repos and conventions, persists results |
| `database.go` | SQLite database access |
| `github_auth.go` | GitHub App authentication |
| `github_issues.go` | Opens and closes GitHub issues for convention failures |

Convention implementations follow the same pattern and live alongside these files (e.g. a future `has-codeowners.go` would sit here too).

## Conventions

Each coding convention is a `Convention` value with an `ID`, `Description`, optional `AppliesTo` filter, and a `Check` function. Conventions register themselves at init time using `RegisterConvention` — no manual wiring is needed.

### Self-registration pattern

Create a new `.go` file in the root directory named after the convention ID (e.g. `has-codeowners.go`) with a `package main` declaration and an `init()` function:

```go
package main

func init() {
    RegisterConvention(Convention{
        ID:          "has-codeowners",
        Description: "Every repo must have a CODEOWNERS file",
        Check: func(repo RepoContext) ConventionResult {
            // ... check logic using GitHubFileExists, etc. ...
            return ConventionResult{
                Convention: "has-codeowners",
                Pass:       true,
                Detail:     "CODEOWNERS found",
            }
        },
    })
}
```

The `init()` function runs automatically when the binary starts, adding the convention to the global registry before the first audit sweep. No changes to `main.go` or any other file are needed.

### AppliesTo

Use `AppliesTo: []RepoType{RepoTypeSystem}` (or `RepoTypeComponent`) if the convention only applies to a subset of repo types. Omit it to apply to all repos.

### Helpers

`convention.go` provides `GitHubFileExists(token, repo, path string) (bool, error)` for the common case of checking whether a file exists in a repo. For tests, use `GitHubFileExistsFromBase` with a fake `httptest` server to avoid real network calls — see `convention_test.go` for examples.

## Running tests

```bash
/usr/local/go/bin/go test ./...
```
