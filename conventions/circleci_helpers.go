package conventions

import (
	"fmt"

	"gopkg.in/yaml.v3"
)

// circleCIConfig is the subset of a CircleCI config.yml that we care about.
type circleCIConfig struct {
	Orbs      map[string]string     `yaml:"orbs"`
	Workflows map[string]ciWorkflow `yaml:"workflows"`
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

// hasLucosOrb reports whether the config declares the lucos deploy orb under
// the alias "lucos".
func hasLucosOrb(cfg *circleCIConfig) bool {
	v, ok := cfg.Orbs["lucos"]
	return ok && v == "lucos/deploy@0"
}
