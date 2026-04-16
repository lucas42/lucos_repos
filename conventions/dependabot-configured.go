package conventions

import (
	"fmt"
	"log/slog"
	"strings"

	"gopkg.in/yaml.v3"
)

const dependabotPath = ".github/dependabot.yml"

// dependabotConfig represents the subset of dependabot.yml we need.
type dependabotConfig struct {
	Updates []dependabotUpdate `yaml:"updates"`
}

type dependabotUpdate struct {
	PackageEcosystem string                      `yaml:"package-ecosystem"`
	Directory        string                      `yaml:"directory"`
	Allow            []dependabotAllow            `yaml:"allow"`
	Groups           map[string]dependabotGroup  `yaml:"groups"`
}

type dependabotAllow struct {
	DependencyType string `yaml:"dependency-type"`
}

type dependabotGroup struct {
	UpdateTypes []string `yaml:"update-types"`
}

func init() {
	Register(Convention{
		ID:          "dependabot-configured",
		Description: "Repository has a valid .github/dependabot.yml with github-actions monitoring and allow-all on all entries",
		Rationale: "Any repo without Dependabot configured is flying blind on dependency " +
			"vulnerabilities. Supply chain attacks via GitHub Actions are a growing attack " +
			"class, so keeping action versions up to date is critical. Allowing all dependency " +
			"types keeps deps current so that when critical security patches land, they arrive " +
			"on a well-maintained base rather than months of accumulated drift. Grouping updates " +
			"by type collapses the daily wave of individual Dependabot PRs into ~2 PRs per " +
			"ecosystem, which reduces deploy-wave noise, CI concurrency saturation, and " +
			"monitoring alert churn.",
		Guidance: "Create or update `.github/dependabot.yml` to include:\n\n" +
			"1. At least one entry with `package-ecosystem: github-actions` and `directory: /`\n" +
			"2. An `allow` block with `dependency-type: all` on every update entry\n" +
			"3. A `groups` block on every update entry covering `minor`, `patch`, and `major`\n\n" +
			"Example:\n```yaml\nversion: 2\nupdates:\n  - package-ecosystem: github-actions\n" +
			"    directory: /\n    schedule:\n      interval: weekly\n    allow:\n" +
			"      - dependency-type: all\n    groups:\n      minor-and-patch:\n" +
			"        update-types: [minor, patch]\n      major:\n        update-types: [major]\n" +
			"  - package-ecosystem: npm\n    directory: /\n    schedule:\n      interval: weekly\n" +
			"    allow:\n      - dependency-type: all\n    groups:\n      minor-and-patch:\n" +
			"        update-types: [minor, patch]\n      major:\n        update-types: [major]\n```",
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

			// Check 4: every entry must have at least one group, and the groups must
			// collectively cover minor, patch, and major update types.
			requiredTypes := []string{"minor", "patch", "major"}
			for _, u := range config.Updates {
				if len(u.Groups) == 0 {
					issues = append(issues, fmt.Sprintf("%s (directory: %s) missing groups block", u.PackageEcosystem, u.Directory))
					continue
				}
				covered := make(map[string]bool)
				for _, group := range u.Groups {
					for _, ut := range group.UpdateTypes {
						covered[ut] = true
					}
				}
				var missing []string
				for _, rt := range requiredTypes {
					if !covered[rt] {
						missing = append(missing, rt)
					}
				}
				if len(missing) > 0 {
					issues = append(issues, fmt.Sprintf("%s (directory: %s) groups do not cover update types: %s", u.PackageEcosystem, u.Directory, strings.Join(missing, ", ")))
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
