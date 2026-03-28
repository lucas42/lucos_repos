package conventions

import (
	"fmt"
	"log/slog"

	"gopkg.in/yaml.v3"
)

const codeqlWorkflowPath = ".github/workflows/codeql-analysis.yml"

func init() {
	// has-codeql-workflow: the codeql-analysis.yml file must exist.
	Register(Convention{
		ID:          "has-codeql-workflow",
		Description: "Repository has a .github/workflows/codeql-analysis.yml workflow file",
		Rationale: "All lucos repos with meaningful application code should have a CodeQL " +
			"analysis workflow to catch security vulnerabilities automatically. Without it, " +
			"the repo is flying blind on code-level security issues.",
		Guidance: "Add a `.github/workflows/codeql-analysis.yml` file to the repository. " +
			"Use the GitHub-provided CodeQL starter workflow as a base, configuring it for " +
			"the languages used in the repo.",
		AppliesTo: []RepoType{RepoTypeSystem, RepoTypeComponent},
		Check: func(repo RepoContext) ConventionResult {
			base := repo.GitHubBaseURL
			if base == "" {
				base = GitHubBaseURL
			}

			// Precondition: skip repos with no CodeQL-supported languages.
			languages, err := GitHubRepoLanguagesFromBase(base, repo.GitHubToken, repo.Name)
			if err != nil {
				slog.Warn("Convention check failed", "convention", "has-codeql-workflow", "repo", repo.Name, "step", "fetch-languages", "error", err)
				return ConventionResult{
					Convention: "has-codeql-workflow",
					Err:        fmt.Errorf("error fetching languages: %w", err),
				}
			}
			if !HasCodeQLLanguage(languages) {
				return ConventionResult{
					Convention: "has-codeql-workflow",
					Pass:       true,
					Detail:     "no CodeQL-supported languages detected; convention does not apply",
				}
			}

			exists, err := GitHubFileExistsFromBase(base, repo.GitHubToken, repo.Name, codeqlWorkflowPath, repo.Ref)
			if err != nil {
				slog.Warn("Convention check failed", "convention", "has-codeql-workflow", "repo", repo.Name, "error", err)
				return ConventionResult{
					Convention: "has-codeql-workflow",
					Err:        fmt.Errorf("error checking for %s: %w", codeqlWorkflowPath, err),
				}
			}

			if exists {
				return ConventionResult{
					Convention: "has-codeql-workflow",
					Pass:       true,
					Detail:     "codeql-analysis.yml exists",
				}
			}

			return ConventionResult{
				Convention: "has-codeql-workflow",
				Pass:       false,
				Detail:     "codeql-analysis.yml not found",
			}
		},
	})

	// codeql-workflow-security-settings: the codeql-analysis.yml file must
	// contain the required security-relevant settings.
	Register(Convention{
		ID:          "codeql-workflow-security-settings",
		Description: "codeql-analysis.yml has required security settings: pull_request trigger, schedule trigger, top-level permissions, and security-events: write on analyze job",
		Rationale: "A CodeQL workflow that only runs on push misses vulnerabilities introduced " +
			"in PRs. A schedule trigger catches new vulnerabilities in unchanged code. A " +
			"top-level permissions block restricts the default token scope. And " +
			"`security-events: write` on the analyze job is required for CodeQL to upload " +
			"its findings to GitHub.",
		Guidance: "Ensure your `codeql-analysis.yml` includes:\n\n" +
			"1. A `pull_request:` entry in the `on:` block\n" +
			"2. A `schedule:` entry with a `cron` value in the `on:` block\n" +
			"3. A top-level `permissions:` key in the workflow\n" +
			"4. `security-events: write` in the analyze job's `permissions` block\n\n" +
			"Example:\n```yaml\non:\n  push:\n    branches: [main]\n  pull_request:\n    branches: [main]\n  schedule:\n    - cron: '0 6 * * 1'\n\npermissions: {}\n\njobs:\n  analyze:\n    permissions:\n      security-events: write\n```",
		AppliesTo: []RepoType{RepoTypeSystem, RepoTypeComponent},
		Check: func(repo RepoContext) ConventionResult {
			base := repo.GitHubBaseURL
			if base == "" {
				base = GitHubBaseURL
			}

			// Precondition: skip repos with no CodeQL-supported languages.
			languages, err := GitHubRepoLanguagesFromBase(base, repo.GitHubToken, repo.Name)
			if err != nil {
				slog.Warn("Convention check failed", "convention", "codeql-workflow-security-settings", "repo", repo.Name, "step", "fetch-languages", "error", err)
				return ConventionResult{
					Convention: "codeql-workflow-security-settings",
					Err:        fmt.Errorf("error fetching languages: %w", err),
				}
			}
			if !HasCodeQLLanguage(languages) {
				return ConventionResult{
					Convention: "codeql-workflow-security-settings",
					Pass:       true,
					Detail:     "no CodeQL-supported languages detected; convention does not apply",
				}
			}

			content, err := GitHubFileContentFromBase(base, repo.GitHubToken, repo.Name, codeqlWorkflowPath, repo.Ref)
			if err != nil {
				slog.Warn("Convention check failed", "convention", "codeql-workflow-security-settings", "repo", repo.Name, "step", "fetch-file", "error", err)
				return ConventionResult{
					Convention: "codeql-workflow-security-settings",
					Err:        fmt.Errorf("error fetching %s: %w", codeqlWorkflowPath, err),
				}
			}

			if content == nil {
				// File doesn't exist — pass trivially (convention 1 will catch this).
				return ConventionResult{
					Convention: "codeql-workflow-security-settings",
					Pass:       true,
					Detail:     "codeql-analysis.yml not found; convention does not apply (see has-codeql-workflow)",
				}
			}

			var workflow codeqlWorkflow
			if err := yaml.Unmarshal(content, &workflow); err != nil {
				slog.Warn("Convention check failed", "convention", "codeql-workflow-security-settings", "repo", repo.Name, "step", "parse-yaml", "error", err)
				return ConventionResult{
					Convention: "codeql-workflow-security-settings",
					Pass:       false,
					Detail:     fmt.Sprintf("Failed to parse codeql-analysis.yml: %v", err),
				}
			}

			var issues []string

			// Check 1: pull_request trigger
			if !workflow.On.hasPullRequest() {
				issues = append(issues, "missing pull_request trigger")
			}

			// Check 2: schedule trigger
			if !workflow.On.hasSchedule() {
				issues = append(issues, "missing schedule trigger")
			}

			// Check 3: top-level permissions block
			if !workflow.HasPermissions {
				issues = append(issues, "missing top-level permissions block")
			}

			// Check 4: security-events: write on analyze job
			if !workflow.hasSecurityEventsWrite() {
				issues = append(issues, "missing security-events: write in analyze job permissions")
			}

			if len(issues) == 0 {
				return ConventionResult{
					Convention: "codeql-workflow-security-settings",
					Pass:       true,
					Detail:     "All required security settings are present",
				}
			}

			detail := "Security settings issues: "
			for i, issue := range issues {
				if i > 0 {
					detail += "; "
				}
				detail += issue
			}

			return ConventionResult{
				Convention: "codeql-workflow-security-settings",
				Pass:       false,
				Detail:     detail,
			}
		},
	})
}

// codeqlWorkflow represents the subset of a GitHub Actions workflow we need
// to validate for CodeQL security settings.
type codeqlWorkflow struct {
	On             codeqlWorkflowOn          `yaml:"on"`
	HasPermissions bool                      `yaml:"-"`
	Jobs           map[string]codeqlWorkflowJob `yaml:"jobs"`
}

// UnmarshalYAML custom unmarshals the workflow to detect the top-level
// permissions key, which may be any type (empty map, scalar, etc.).
func (w *codeqlWorkflow) UnmarshalYAML(node *yaml.Node) error {
	// Check for top-level permissions key.
	if node.Kind == yaml.MappingNode {
		for i := 0; i < len(node.Content)-1; i += 2 {
			if node.Content[i].Value == "permissions" {
				w.HasPermissions = true
				break
			}
		}
	}

	// Decode the rest normally using a type alias to avoid infinite recursion.
	type rawWorkflow codeqlWorkflow
	var raw rawWorkflow
	if err := node.Decode(&raw); err != nil {
		return err
	}
	raw.HasPermissions = w.HasPermissions
	*w = codeqlWorkflow(raw)
	return nil
}

// codeqlWorkflowOn handles the "on" trigger block. GitHub Actions allows
// "on" to be either a map or a list (or even a string for single events).
// We use custom unmarshalling to handle all forms.
type codeqlWorkflowOn struct {
	Events map[string]interface{}
}

func (o *codeqlWorkflowOn) UnmarshalYAML(node *yaml.Node) error {
	o.Events = make(map[string]interface{})

	switch node.Kind {
	case yaml.MappingNode:
		// Map form: on: { push: ..., pull_request: ... }
		for i := 0; i < len(node.Content)-1; i += 2 {
			o.Events[node.Content[i].Value] = true
		}
	case yaml.SequenceNode:
		// List form: on: [push, pull_request]
		for _, item := range node.Content {
			o.Events[item.Value] = true
		}
	case yaml.ScalarNode:
		// Single event: on: push
		o.Events[node.Value] = true
	}
	return nil
}

func (o *codeqlWorkflowOn) hasPullRequest() bool {
	_, ok := o.Events["pull_request"]
	return ok
}

func (o *codeqlWorkflowOn) hasSchedule() bool {
	_, ok := o.Events["schedule"]
	return ok
}

// codeqlWorkflowJob represents a single job in the workflow.
type codeqlWorkflowJob struct {
	Permissions map[string]string `yaml:"permissions"`
}

// hasSecurityEventsWrite checks whether any job named "analyze" (case-insensitive
// match) has security-events: write in its permissions.
func (w *codeqlWorkflow) hasSecurityEventsWrite() bool {
	for name, job := range w.Jobs {
		// The analyze job is typically named "analyze" but we check all jobs
		// that have the permission set, since there could be variations.
		_ = name
		if job.Permissions["security-events"] == "write" {
			return true
		}
	}
	return false
}
