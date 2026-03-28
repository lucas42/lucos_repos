package conventions

import (
	"fmt"
	"regexp"
	"strings"

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

// ciBranchFilter holds CircleCI branch filter configuration for a job entry.
type ciBranchFilter struct {
	Only   []string `yaml:"only"`
	Ignore []string `yaml:"ignore"`
}

// UnmarshalYAML implements yaml.Unmarshaler for ciBranchFilter to handle both
// scalar ("only: main") and sequence ("only: [main, develop]") forms.
func (f *ciBranchFilter) UnmarshalYAML(value *yaml.Node) error {
	// Try decoding as a struct with Only/Ignore fields first.
	type plain struct {
		Only   yaml.Node `yaml:"only"`
		Ignore yaml.Node `yaml:"ignore"`
	}
	var p plain
	if err := value.Decode(&p); err != nil {
		return err
	}
	f.Only = decodeStringOrList(&p.Only)
	f.Ignore = decodeStringOrList(&p.Ignore)
	return nil
}

// decodeStringOrList extracts a []string from a YAML node that may be a scalar
// string or a sequence of strings.
func decodeStringOrList(n *yaml.Node) []string {
	if n == nil || n.Kind == 0 {
		return nil
	}
	switch n.Kind {
	case yaml.ScalarNode:
		return []string{n.Value}
	case yaml.SequenceNode:
		var result []string
		for _, child := range n.Content {
			if child.Kind == yaml.ScalarNode {
				result = append(result, child.Value)
			}
		}
		return result
	}
	return nil
}

// ciJobFilters holds the filters block from a CircleCI job entry.
type ciJobFilters struct {
	Branches ciBranchFilter `yaml:"branches"`
}

// ciJobEntry represents a single entry in the jobs list of a workflow. In
// CircleCI YAML, each entry is either a plain string (the job name) or a
// mapping with a single key (the job name) and a value containing config
// (including optional branch filters).
type ciJobEntry struct {
	Name    string
	Filters *ciJobFilters
}

// RunsOnBranch reports whether this job would run on the given branch based
// on its CircleCI branch filters. If there are no filters, the job runs on
// all branches. CircleCI regex patterns are delimited by /.../.
func (e ciJobEntry) RunsOnBranch(branch string) bool {
	if e.Filters == nil {
		return true
	}
	f := e.Filters.Branches

	// "ignore" takes precedence when both are set (per CircleCI docs, only
	// one of only/ignore should be specified, but we handle both defensively).
	if len(f.Ignore) > 0 {
		for _, pattern := range f.Ignore {
			if matchesBranchPattern(pattern, branch) {
				return false
			}
		}
	}

	if len(f.Only) > 0 {
		for _, pattern := range f.Only {
			if matchesBranchPattern(pattern, branch) {
				return true
			}
		}
		return false
	}

	return true
}

// matchesBranchPattern checks if a branch matches a CircleCI branch pattern.
// Patterns wrapped in /.../ are treated as regular expressions; everything
// else is an exact match.
func matchesBranchPattern(pattern, branch string) bool {
	if strings.HasPrefix(pattern, "/") && strings.HasSuffix(pattern, "/") && len(pattern) > 2 {
		re, err := regexp.Compile(pattern[1 : len(pattern)-1])
		if err != nil {
			return false
		}
		return re.MatchString(branch)
	}
	return pattern == branch
}

// UnmarshalYAML implements yaml.Unmarshaler for ciJobEntry. A job entry is
// either a bare string or a single-key mapping. We extract the job name and
// any branch filters from the mapping value.
func (e *ciJobEntry) UnmarshalYAML(value *yaml.Node) error {
	switch value.Kind {
	case yaml.ScalarNode:
		// Plain string: "- lucos/build-amd64"
		e.Name = value.Value
		return nil
	case yaml.MappingNode:
		// Mapping: "- lucos/deploy-avalon:\n    requires: [...]"
		// The first key is the job name; the value is the job config.
		if len(value.Content) < 2 {
			return nil
		}
		e.Name = value.Content[0].Value

		// Decode the job config value to extract filters.
		type jobConfig struct {
			Filters *ciJobFilters `yaml:"filters"`
		}
		var cfg jobConfig
		if err := value.Content[1].Decode(&cfg); err != nil {
			// Non-fatal: we still have the name.
			return nil
		}
		e.Filters = cfg.Filters
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

// allJobEntries returns all job entries (with filters) across all workflows.
func allJobEntries(cfg *circleCIConfig) []ciJobEntry {
	var entries []ciJobEntry
	for _, wf := range cfg.Workflows {
		for _, job := range wf.Jobs {
			if job.Name != "" {
				entries = append(entries, job)
			}
		}
	}
	return entries
}

// hasLucosOrb reports whether the config declares the lucos deploy orb under
// the alias "lucos".
func hasLucosOrb(cfg *circleCIConfig) bool {
	v, ok := cfg.Orbs["lucos"]
	return ok && v == "lucos/deploy@0"
}
