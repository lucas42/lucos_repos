package conventions

import (
	"fmt"
	"log/slog"
	"strings"

	"gopkg.in/yaml.v3"
)

// circleCIConfig is the subset of a CircleCI config.yml that we care about.
type circleCIConfig struct {
	Orbs      map[string]string      `yaml:"orbs"`
	Workflows map[string]ciWorkflow  `yaml:"workflows"`
}

// ciWorkflow is the subset of a CircleCI workflow we care about.
type ciWorkflow struct {
	Jobs []ciJobEntry `yaml:"jobs"`
}

// ciJobEntry represents a single entry in the jobs list of a workflow. In
// CircleCI YAML, each entry is either a plain string (the job name) or a
// mapping with a single key (the job name) and a value containing config.
// We only care about the job name, so we decode either form into a string.
type ciJobEntry struct {
	Name string
}

// UnmarshalYAML implements yaml.Unmarshaler for ciJobEntry. A job entry is
// either a bare string or a single-key mapping. In both cases we extract just
// the key/string as the job name.
func (e *ciJobEntry) UnmarshalYAML(value *yaml.Node) error {
	switch value.Kind {
	case yaml.ScalarNode:
		// Plain string: "- lucos/build-amd64"
		e.Name = value.Value
		return nil
	case yaml.MappingNode:
		// Mapping: "- lucos/deploy-avalon:\n    requires: [...]"
		// The first key is the job name.
		if len(value.Content) >= 1 {
			e.Name = value.Content[0].Value
		}
		return nil
	default:
		return fmt.Errorf("unexpected YAML node kind %v for job entry", value.Kind)
	}
}

// parseCIConfig fetches and parses the CircleCI config for a repo. It returns
// (nil, nil) if the file does not exist.
func parseCIConfig(baseURL, token, repo string) (*circleCIConfig, error) {
	content, err := GitHubFileContentFromBase(baseURL, token, repo, ".circleci/config.yml")
	if err != nil {
		return nil, err
	}
	if content == nil {
		return nil, nil
	}
	var cfg circleCIConfig
	if err := yaml.Unmarshal(content, &cfg); err != nil {
		return nil, fmt.Errorf("failed to parse .circleci/config.yml in %s: %w", repo, err)
	}
	return &cfg, nil
}

// allJobNames returns all job property names referenced across all workflows in
// a CircleCI config.
func allJobNames(cfg *circleCIConfig) []string {
	var names []string
	for _, wf := range cfg.Workflows {
		for _, job := range wf.Jobs {
			if job.Name != "" {
				names = append(names, job.Name)
			}
		}
	}
	return names
}

// hasOrbAlias reports whether the config declares the lucos deploy orb under
// the alias "lucos".
func hasLucosOrb(cfg *circleCIConfig) bool {
	v, ok := cfg.Orbs["lucos"]
	return ok && v == "lucos/deploy@0"
}

func init() {
	// circleci-config-exists: system and component repos must have a CircleCI
	// configuration file.
	Register(Convention{
		ID:          "circleci-config-exists",
		Description: "System and component repositories must have a .circleci/config.yml file",
		Rationale: "Without a CircleCI config, changes to this repository are not automatically " +
			"built, tested, or deployed. This means code changes require manual intervention to " +
			"reach production, which is error-prone and slows down delivery.",
		Guidance: "Add a `.circleci/config.yml` following the standard lucos CI template " +
			"(see the lucos CLAUDE.md for the canonical config).",
		AppliesTo: []RepoType{RepoTypeSystem, RepoTypeComponent},
		Check: func(repo RepoContext) ConventionResult {
			base := repo.GitHubBaseURL
			if base == "" {
				base = GitHubBaseURL
			}
			exists, err := GitHubFileExistsFromBase(base, repo.GitHubToken, repo.Name, ".circleci/config.yml")
			if err != nil {
				slog.Warn("Convention check failed", "convention", "circleci-config-exists", "repo", repo.Name, "error", err)
				return ConventionResult{
					Convention: "circleci-config-exists",
					Pass:       false,
					Detail:     fmt.Sprintf("Error checking file: %v", err),
				}
			}
			if exists {
				return ConventionResult{
					Convention: "circleci-config-exists",
					Pass:       true,
					Detail:     ".circleci/config.yml found",
				}
			}
			return ConventionResult{
				Convention: "circleci-config-exists",
				Pass:       false,
				Detail:     ".circleci/config.yml not found",
			}
		},
	})

	// circleci-uses-lucos-orb: system and component repos must declare the
	// lucos deploy orb as "lucos: lucos/deploy@0".
	Register(Convention{
		ID:          "circleci-uses-lucos-orb",
		Description: "CircleCI config must declare the lucos deploy orb (`lucos: lucos/deploy@0`)",
		Rationale: "The lucos deploy orb provides standardised build and deploy jobs. " +
			"Without it, repos must implement their own build/deploy logic, leading to " +
			"inconsistency and maintenance burden.",
		Guidance: "Add the following to the `orbs:` section of `.circleci/config.yml`:\n\n" +
			"```yaml\norbs:\n  lucos: lucos/deploy@0\n```",
		AppliesTo: []RepoType{RepoTypeSystem, RepoTypeComponent},
		Check: func(repo RepoContext) ConventionResult {
			base := repo.GitHubBaseURL
			if base == "" {
				base = GitHubBaseURL
			}
			cfg, err := parseCIConfig(base, repo.GitHubToken, repo.Name)
			if err != nil {
				slog.Warn("Convention check failed", "convention", "circleci-uses-lucos-orb", "repo", repo.Name, "error", err)
				return ConventionResult{
					Convention: "circleci-uses-lucos-orb",
					Pass:       false,
					Detail:     fmt.Sprintf("Error reading config: %v", err),
				}
			}
			if cfg == nil {
				// File doesn't exist — circleci-config-exists will catch this.
				return ConventionResult{
					Convention: "circleci-uses-lucos-orb",
					Pass:       true,
					Detail:     ".circleci/config.yml not found; checked by circleci-config-exists",
				}
			}
			if hasLucosOrb(cfg) {
				return ConventionResult{
					Convention: "circleci-uses-lucos-orb",
					Pass:       true,
					Detail:     "lucos orb declared as lucos/deploy@0",
				}
			}
			return ConventionResult{
				Convention: "circleci-uses-lucos-orb",
				Pass:       false,
				Detail:     "CircleCI config does not declare `lucos: lucos/deploy@0` in its orbs",
			}
		},
	})

	// circleci-has-release-job: component repos must have at least one job
	// whose name begins with "lucos/release-".
	Register(Convention{
		ID:          "circleci-has-release-job",
		Description: "Component CircleCI config must include at least one `lucos/release-*` job",
		Rationale: "Component repos are shared libraries or infrastructure that other services " +
			"depend on. The `lucos/release-*` job publishes new versions to the package registry. " +
			"Without it, updates to the component cannot be consumed by downstream services.",
		Guidance: "Add a `lucos/release-*` job (e.g. `lucos/release-npm`) to the `jobs:` list " +
			"in a workflow in `.circleci/config.yml`. Refer to the lucos deploy orb documentation " +
			"for the correct job name for your package type.",
		AppliesTo: []RepoType{RepoTypeComponent},
		Check: func(repo RepoContext) ConventionResult {
			base := repo.GitHubBaseURL
			if base == "" {
				base = GitHubBaseURL
			}
			cfg, err := parseCIConfig(base, repo.GitHubToken, repo.Name)
			if err != nil {
				slog.Warn("Convention check failed", "convention", "circleci-has-release-job", "repo", repo.Name, "error", err)
				return ConventionResult{
					Convention: "circleci-has-release-job",
					Pass:       false,
					Detail:     fmt.Sprintf("Error reading config: %v", err),
				}
			}
			if cfg == nil {
				// File doesn't exist — circleci-config-exists will catch this.
				return ConventionResult{
					Convention: "circleci-has-release-job",
					Pass:       true,
					Detail:     ".circleci/config.yml not found; checked by circleci-config-exists",
				}
			}
			for _, name := range allJobNames(cfg) {
				if strings.HasPrefix(name, "lucos/release-") {
					return ConventionResult{
						Convention: "circleci-has-release-job",
						Pass:       true,
						Detail:     fmt.Sprintf("Found release job: %s", name),
					}
				}
			}
			return ConventionResult{
				Convention: "circleci-has-release-job",
				Pass:       false,
				Detail:     "No job beginning with `lucos/release-` found in CircleCI config",
			}
		},
	})

	// circleci-system-deploy-jobs: system repos must have exactly the right set
	// of deploy jobs — one per host listed in configy, and no extras.
	Register(Convention{
		ID:          "circleci-system-deploy-jobs",
		Description: "System CircleCI config must include exactly the correct `lucos/deploy-*` jobs for its configured hosts",
		Rationale: "Each host listed in lucos_configy for a system needs its own deploy job so " +
			"that changes are automatically deployed to every target host. Extra deploy jobs risk " +
			"deploying to hosts that aren't configured to run the service.",
		Guidance: "Edit the `jobs:` list in `.circleci/config.yml` to include exactly one " +
			"`lucos/deploy-{host}` job per host listed in lucos_configy — no more, no fewer. " +
			"Check `lucos_configy/config/systems.yaml` for the authoritative list of hosts.",
		AppliesTo: []RepoType{RepoTypeSystem},
		Check: func(repo RepoContext) ConventionResult {
			base := repo.GitHubBaseURL
			if base == "" {
				base = GitHubBaseURL
			}
			cfg, err := parseCIConfig(base, repo.GitHubToken, repo.Name)
			if err != nil {
				slog.Warn("Convention check failed", "convention", "circleci-system-deploy-jobs", "repo", repo.Name, "error", err)
				return ConventionResult{
					Convention: "circleci-system-deploy-jobs",
					Pass:       false,
					Detail:     fmt.Sprintf("Error reading config: %v", err),
				}
			}
			if cfg == nil {
				// File doesn't exist — circleci-config-exists will catch this.
				return ConventionResult{
					Convention: "circleci-system-deploy-jobs",
					Pass:       true,
					Detail:     ".circleci/config.yml not found; checked by circleci-config-exists",
				}
			}

			// Build a set of expected deploy job names from the configured hosts.
			expected := make(map[string]bool, len(repo.Hosts))
			for _, host := range repo.Hosts {
				expected["lucos/deploy-"+host] = false // false = not yet found
			}

			// Scan all job names in the config.
			var extraDeployJobs []string
			for _, name := range allJobNames(cfg) {
				if !strings.HasPrefix(name, "lucos/deploy-") {
					continue
				}
				if _, ok := expected[name]; ok {
					expected[name] = true // mark as found
				} else {
					extraDeployJobs = append(extraDeployJobs, name)
				}
			}

			// Check for missing expected jobs.
			var missingJobs []string
			for jobName, found := range expected {
				if !found {
					missingJobs = append(missingJobs, jobName)
				}
			}

			if len(missingJobs) > 0 && len(extraDeployJobs) > 0 {
				return ConventionResult{
					Convention: "circleci-system-deploy-jobs",
					Pass:       false,
					Detail: fmt.Sprintf("Missing deploy jobs: %s; unexpected deploy jobs: %s",
						strings.Join(missingJobs, ", "), strings.Join(extraDeployJobs, ", ")),
				}
			}
			if len(missingJobs) > 0 {
				return ConventionResult{
					Convention: "circleci-system-deploy-jobs",
					Pass:       false,
					Detail:     fmt.Sprintf("Missing deploy jobs for configured hosts: %s", strings.Join(missingJobs, ", ")),
				}
			}
			if len(extraDeployJobs) > 0 {
				return ConventionResult{
					Convention: "circleci-system-deploy-jobs",
					Pass:       false,
					Detail:     fmt.Sprintf("Unexpected deploy jobs not matching any configured host: %s", strings.Join(extraDeployJobs, ", ")),
				}
			}
			return ConventionResult{
				Convention: "circleci-system-deploy-jobs",
				Pass:       true,
				Detail:     "Deploy jobs match configured hosts",
			}
		},
	})

	// circleci-no-forbidden-jobs: repos that are not systems or components must
	// not use lucos/release-* or lucos/deploy-* jobs in their CircleCI config
	// (if they have one). Unconfigured repos pass trivially.
	Register(Convention{
		ID:          "circleci-no-forbidden-jobs",
		Description: "Non-system, non-component repositories must not include `lucos/release-*` or `lucos/deploy-*` jobs in their CircleCI config",
		Rationale: "Release and deploy jobs are reserved for components and systems respectively. " +
			"Including them in other repo types (e.g. scripts) indicates a misconfiguration — " +
			"either the repo type in lucos_configy is wrong, or the CI config contains jobs that " +
			"shouldn't be there.",
		Guidance: "Remove any `lucos/release-*` and `lucos/deploy-*` jobs from the CircleCI config. " +
			"If this repo should be deploying to a server or releasing a package, update its type " +
			"in lucos_configy (`config/systems.yaml` or `config/components.yaml`) accordingly.",
		Check: func(repo RepoContext) ConventionResult {
			// Unconfigured repos pass trivially — the in-lucos-configy convention
			// handles the gap. Systems and components are checked by their own
			// targeted deploy/release job conventions.
			if repo.Type == RepoTypeUnconfigured || repo.Type == RepoTypeSystem || repo.Type == RepoTypeComponent {
				return ConventionResult{
					Convention: "circleci-no-forbidden-jobs",
					Pass:       true,
					Detail:     fmt.Sprintf("Convention does not apply to %s repos", repo.Type),
				}
			}

			base := repo.GitHubBaseURL
			if base == "" {
				base = GitHubBaseURL
			}
			cfg, err := parseCIConfig(base, repo.GitHubToken, repo.Name)
			if err != nil {
				slog.Warn("Convention check failed", "convention", "circleci-no-forbidden-jobs", "repo", repo.Name, "error", err)
				return ConventionResult{
					Convention: "circleci-no-forbidden-jobs",
					Pass:       false,
					Detail:     fmt.Sprintf("Error reading config: %v", err),
				}
			}
			if cfg == nil {
				// No config file — nothing to check.
				return ConventionResult{
					Convention: "circleci-no-forbidden-jobs",
					Pass:       true,
					Detail:     "No .circleci/config.yml present",
				}
			}

			var forbidden []string
			for _, name := range allJobNames(cfg) {
				if strings.HasPrefix(name, "lucos/release-") || strings.HasPrefix(name, "lucos/deploy-") {
					forbidden = append(forbidden, name)
				}
			}

			if len(forbidden) > 0 {
				return ConventionResult{
					Convention: "circleci-no-forbidden-jobs",
					Pass:       false,
					Detail:     fmt.Sprintf("CircleCI config contains forbidden jobs: %s", strings.Join(forbidden, ", ")),
				}
			}
			return ConventionResult{
				Convention: "circleci-no-forbidden-jobs",
				Pass:       true,
				Detail:     "No forbidden jobs found in CircleCI config",
			}
		},
	})
}
