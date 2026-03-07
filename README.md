# lucos_repos

Audits all lucos GitHub repositories against a set of coding conventions, raises GitHub issues for failures, and exposes a status API.

## Project structure

```
src/            Go source for the HTTP server and audit sweeper
conventions/    Convention definitions (see below)
docs/adr/       Architectural Decision Records
```

### The `src/` package

The main binary lives here. On startup it syncs all registered conventions to a SQLite database, then schedules a periodic audit sweep. The sweep checks every known repo against every applicable convention and raises (or closes) GitHub issues accordingly. Two HTTP endpoints are exposed: `/_info` for health/monitoring and `/api/status` for a machine-readable audit report.

### The `conventions/` package

Each coding convention is a `Convention` value with an `ID`, `Description`, optional `AppliesTo` filter, and a `Check` function. Conventions are held in a package-level registry and registered at init time.

#### Self-registration pattern

Each convention lives in its own `.go` file inside `conventions/`. The file calls `Register(...)` inside an `init()` function so it is included in the registry automatically when the package is imported — no manual wiring needed:

```go
package conventions

func init() {
    Register(Convention{
        ID:          "my-new-convention",
        Description: "Every repo must have a CODEOWNERS file",
        Check: func(repo RepoContext) ConventionResult {
            // ... check logic ...
            return ConventionResult{
                Convention: "my-new-convention",
                Pass:       true,
                Detail:     "CODEOWNERS found",
            }
        },
    })
}
```

The `init()` function runs automatically when the binary starts, adding the convention to the global registry before the first audit sweep. No changes to any other file are needed.

#### Adding a new convention

1. Create a new `.go` file in `conventions/` named after the convention ID (e.g. `has-codeowners.go`).
2. Declare `package conventions` at the top.
3. Add an `init()` function that calls `Register(Convention{...})`.
4. Use `AppliesTo: []RepoType{RepoTypeSystem}` (or `RepoTypeComponent`) if the convention only applies to a subset of repo types. Omit `AppliesTo` to apply to all repos.
5. Add tests in `conventions/` alongside the implementation — see the existing `*_test.go` files for the pattern (fake HTTP servers via `httptest` work well for GitHub API calls).

`conventions/conventions.go` defines the shared types (`Convention`, `RepoContext`, `ConventionResult`, `RepoType`) and helper utilities (`GitHubFileExists`, `GitHubFileExistsFromBase`) that convention checks can use.

## Running tests

```bash
/usr/local/go/bin/go test ./...
```
