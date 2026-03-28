package conventions

import (
	"fmt"

	"gopkg.in/yaml.v3"
)

// circleCIConfig is the subset of a CircleCI config.yml that we care about.
type circleCIConfig struct {
	Orbs      map[string]string     `yaml:"orbs"`
	Workflows map[string]ciWorkflow `yaml:"-"`
}

// UnmarshalYAML implements yaml.Unmarshaler for circleCIConfig. The standard
// struct decoder cannot handle the `workflows` block because it contains a
// scalar `version: 2` key alongside the workflow map entries. We decode the
// struct fields normally (via an alias type to avoid infinite recursion) and
// then manually walk the workflows mapping node, skipping any scalar values.
func (c *circleCIConfig) UnmarshalYAML(value *yaml.Node) error {
	// aliasConfig breaks the recursion: decoding into it will NOT call this
	// method again, so all non-workflows fields are decoded normally.
	type aliasConfig struct {
		Orbs      map[string]string `yaml:"orbs"`
		Workflows yaml.Node         `yaml:"workflows"`
	}
	var raw aliasConfig
	if err := value.Decode(&raw); err != nil {
		return err
	}
	c.Orbs = raw.Orbs

	// workflows is optional — not all configs have it.
	if raw.Workflows.Kind == 0 {
		return nil
	}
	if raw.Workflows.Kind != yaml.MappingNode {
		return fmt.Errorf("expected workflows to be a mapping, got kind %v", raw.Workflows.Kind)
	}

	c.Workflows = make(map[string]ciWorkflow)
	// MappingNode.Content is a flat list of alternating key/value nodes.
	nodes := raw.Workflows.Content
	for i := 0; i+1 < len(nodes); i += 2 {
		keyNode := nodes[i]
		valNode := nodes[i+1]
		// Skip scalar values (e.g. `version: 2`) — they are not workflow defs.
		if valNode.Kind != yaml.MappingNode {
			continue
		}
		var wf ciWorkflow
		if err := valNode.Decode(&wf); err != nil {
			return fmt.Errorf("failed to decode workflow %q: %w", keyNode.Value, err)
		}
		c.Workflows[keyNode.Value] = wf
	}
	return nil
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
func parseCIConfig(baseURL, token, repo string, ref ...string) (*circleCIConfig, error) {
	content, err := GitHubFileContentFromBase(baseURL, token, repo, ".circleci/config.yml", ref...)
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
