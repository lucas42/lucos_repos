package conventions

import (
	"fmt"
	"log/slog"

	"gopkg.in/yaml.v3"
)

const dependabotPath = ".github/dependabot.yml"

// dependabotConfig represents the subset of dependabot.yml we need.
type dependabotConfig struct {
	Updates []dependabotUpdate `yaml:"updates"`
}

type dependabotUpdate struct {
	PackageEcosystem string           `yaml:"package-ecosystem"`
	Directory        string           `yaml:"directory"`
	Allow            []dependabotAllow `yaml:"allow"`
}

type dependabotAllow struct {
	DependencyType string `yaml:"dependency-type"`
}

func init() {
	Register(Convention{
		ID:          "dependabot-configured",
		Description: "Repository has a valid .github/dependabot.yml with github-actions monitoring and allow-all on all entries",
		Rationale: "Any repo without Dependabot configured is flying blind on dependency " +
			"vulnerabilities. Supply chain attacks via GitHub Actions are a growing attack " +
			"class, so keeping action versions up to date is critical. Allowing all dependency " +
			"types keeps deps current so that when critical security patches land, they arrive " +
			"on a well-maintained base rather than months of accumulated drift.",
		Guidance: "Create or update `.github/dependabot.yml` to include:\n\n" +
			"1. At least one entry with `package-ecosystem: github-actions` and `directory: /`\n" +
			"2. An `allow` block with `dependency-type: all` on every update entry\n\n" +
			"Example:\n```yaml\nversion: 2\nupdates:\n  - package-ecosystem: github-actions\n" +
			"    directory: /\n    schedule:\n      interval: weekly\n    allow:\n" +
			"      - dependency-type: all\n  - package-ecosystem: npm\n    directory: /\n" +
			"    schedule:\n      interval: weekly\n    allow:\n      - dependency-type: all\n```",
		Check: func(repo RepoContext) ConventionResult {
			base := repo.GitHubBaseURL
			if base == "" {
				base = GitHubBaseURL
			}

			content, err := GitHubFileContentFromBase(base, repo.GitHubToken, repo.Name, dependabotPath, repo.Ref)
			if err != nil {
				slog.Warn("Convention check failed", "convention", "dependabot-configured", "repo", repo.Name, "step", "fetch-file", "error", err)
				return ConventionResult{
					Convention: "dependabot-configured",
					Err:        fmt.Errorf("error fetching %s: %w", dependabotPath, err),
				}
			}

			if content == nil {
				return ConventionResult{
					Convention: "dependabot-configured",
					Pass:       false,
					Detail:     "dependabot.yml not found",
				}
			}

			var config dependabotConfig
			if err := yaml.Unmarshal(content, &config); err != nil {
				slog.Warn("Convention check failed", "convention", "dependabot-configured", "repo", repo.Name, "step", "parse-yaml", "error", err)
				return ConventionResult{
					Convention: "dependabot-configured",
					Pass:       false,
					Detail:     fmt.Sprintf("Failed to parse dependabot.yml: %v", err),
				}
			}

			var issues []string

			// Check 2: github-actions ecosystem with directory "/"
			hasGitHubActions := false
			for _, u := range config.Updates {
				if u.PackageEcosystem == "github-actions" && u.Directory == "/" {
					hasGitHubActions = true
					break
				}
			}
			if !hasGitHubActions {
				issues = append(issues, "no github-actions entry with directory \"/\"")
			}

			// Check 3: every entry must have allow with dependency-type: all
			for _, u := range config.Updates {
				hasAllowAll := false
				for _, a := range u.Allow {
					if a.DependencyType == "all" {
						hasAllowAll = true
						break
					}
				}
				if !hasAllowAll {
					issues = append(issues, fmt.Sprintf("%s (directory: %s) missing allow with dependency-type: all", u.PackageEcosystem, u.Directory))
				}
			}

			if len(issues) == 0 {
				return ConventionResult{
					Convention: "dependabot-configured",
					Pass:       true,
					Detail:     "Dependabot properly configured",
				}
			}

			detail := "Dependabot configuration issues: "
			for i, issue := range issues {
				if i > 0 {
					detail += "; "
				}
				detail += issue
			}

			return ConventionResult{
				Convention: "dependabot-configured",
				Pass:       false,
				Detail:     detail,
			}
		},
	})
}
