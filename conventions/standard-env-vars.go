package conventions

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"path/filepath"
	"strings"

	"gopkg.in/yaml.v3"
)

// standardEnvVars is the list of well-known env vars that should be declared
// in docker-compose.yml if the repo's code references them. Start narrow and
// expand once the false-positive rate is understood.
var standardEnvVars = []string{
	"LOGANNE_ENDPOINT",
}

// sourceExtensions is the set of file extensions considered "source code" for
// the purpose of env var detection. We only search these to avoid false
// positives from docs, images, lock files, etc.
var sourceExtensions = map[string]bool{
	".js":   true,
	".ts":   true,
	".mjs":  true,
	".cjs":  true,
	".py":   true,
	".go":   true,
	".rb":   true,
	".erl":  true,
	".ex":   true,
	".exs":  true,
	".java": true,
	".kt":   true,
	".rs":   true,
	".sh":   true,
}

// envVarsComposeFile is the structure we need for parsing the environment
// declarations in docker-compose.yml.
type envVarsComposeFile struct {
	Services map[string]envVarsComposeService `yaml:"services"`
}

// envVarsComposeService holds the environment config for a service.
// The environment field can be either a list of strings ("VAR=val" or "VAR")
// or a map of string→string. We use a yaml.Node to handle both forms.
type envVarsComposeService struct {
	Environment yaml.Node `yaml:"environment"`
}

// declaredEnvVars extracts the set of environment variable names declared
// across all services in a docker-compose.yml.
func declaredEnvVars(compose envVarsComposeFile) map[string]bool {
	vars := make(map[string]bool)
	for _, svc := range compose.Services {
		node := &svc.Environment
		switch node.Kind {
		case yaml.SequenceNode:
			// List form: ["VAR=value", "VAR"]
			for _, item := range node.Content {
				name := item.Value
				if idx := strings.IndexByte(name, '='); idx >= 0 {
					name = name[:idx]
				}
				vars[name] = true
			}
		case yaml.MappingNode:
			// Map form: {VAR: value}
			for i := 0; i < len(node.Content)-1; i += 2 {
				vars[node.Content[i].Value] = true
			}
		}
	}
	return vars
}

// gitTreeEntry is a single entry from the GitHub git trees API.
type gitTreeEntry struct {
	Path string `json:"path"`
	Type string `json:"type"` // "blob" or "tree"
	SHA  string `json:"sha"`
	Size int    `json:"size"`
}

// gitTreeResponse is the response from the GitHub git trees API.
type gitTreeResponse struct {
	Tree      []gitTreeEntry `json:"tree"`
	Truncated bool           `json:"truncated"`
}

// gitBlobResponse is the response from the GitHub git blobs API.
type gitBlobResponse struct {
	Content  string `json:"content"`
	Encoding string `json:"encoding"`
	Size     int    `json:"size"`
}

// GitHubRepoTree fetches the full recursive tree for a repo's default branch.
func GitHubRepoTree(baseURL, token, repo string) (*gitTreeResponse, error) {
	return GitHubRepoTreeFromBase(baseURL, token, repo)
}

// GitHubRepoTreeFromBase fetches the recursive tree using HEAD.
func GitHubRepoTreeFromBase(baseURL, token, repo string) (*gitTreeResponse, error) {
	url := fmt.Sprintf("%s/repos/%s/git/trees/HEAD?recursive=1", baseURL, repo)
	req, err := http.NewRequest("GET", url, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to build tree request: %w", err)
	}
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Accept", "application/vnd.github+json")
	req.Header.Set("X-GitHub-Api-Version", "2022-11-28")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("GitHub tree API request failed: %w", err)
	}
	defer func() {
		_, _ = io.Copy(io.Discard, resp.Body)
		resp.Body.Close()
	}()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("unexpected GitHub tree API status %d for %s", resp.StatusCode, repo)
	}

	var tree gitTreeResponse
	if err := json.NewDecoder(resp.Body).Decode(&tree); err != nil {
		return nil, fmt.Errorf("failed to decode tree response: %w", err)
	}
	return &tree, nil
}

// GitHubBlobContent fetches the content of a git blob.
func GitHubBlobContent(baseURL, token, repo, sha string) ([]byte, error) {
	url := fmt.Sprintf("%s/repos/%s/git/blobs/%s", baseURL, repo, sha)
	req, err := http.NewRequest("GET", url, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to build blob request: %w", err)
	}
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Accept", "application/vnd.github+json")
	req.Header.Set("X-GitHub-Api-Version", "2022-11-28")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("GitHub blob API request failed: %w", err)
	}
	defer func() {
		_, _ = io.Copy(io.Discard, resp.Body)
		resp.Body.Close()
	}()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("unexpected GitHub blob API status %d for blob %s in %s", resp.StatusCode, sha, repo)
	}

	var blob gitBlobResponse
	if err := json.NewDecoder(resp.Body).Decode(&blob); err != nil {
		return nil, fmt.Errorf("failed to decode blob response: %w", err)
	}

	if blob.Encoding != "base64" {
		return nil, fmt.Errorf("unexpected blob encoding %q for %s in %s", blob.Encoding, sha, repo)
	}

	decoded, err := base64.StdEncoding.DecodeString(strings.ReplaceAll(blob.Content, "\n", ""))
	if err != nil {
		return nil, fmt.Errorf("failed to base64-decode blob %s in %s: %w", sha, repo, err)
	}
	return decoded, nil
}

// repoContainsEnvVar checks whether any source file in the repo references
// the given env var name by fetching the repo tree and checking source files.
// It uses the git trees and blobs APIs which only require contents:read permission.
func repoContainsEnvVar(baseURL, token, repo, envVar string) (bool, error) {
	tree, err := GitHubRepoTreeFromBase(baseURL, token, repo)
	if err != nil {
		return false, fmt.Errorf("error fetching repo tree: %w", err)
	}
	if tree.Truncated {
		return false, fmt.Errorf("git tree response was truncated for %s; cannot reliably search for env var usage", repo)
	}

	// Find source files to check. Exclude the conventions/ directory to avoid
	// matching the convention definitions themselves (which reference env var
	// names as string literals).
	var sourceBlobs []gitTreeEntry
	for _, entry := range tree.Tree {
		if entry.Type != "blob" {
			continue
		}
		if strings.HasPrefix(entry.Path, "conventions/") {
			continue
		}
		ext := filepath.Ext(entry.Path)
		if sourceExtensions[ext] && entry.Size < 500000 { // Skip very large files
			sourceBlobs = append(sourceBlobs, entry)
		}
	}

	// Check each source file for the env var reference.
	for _, blob := range sourceBlobs {
		content, err := GitHubBlobContent(baseURL, token, repo, blob.SHA)
		if err != nil {
			// Log and skip individual blob fetch failures rather than failing the whole check.
			slog.Warn("Failed to fetch blob", "repo", repo, "path", blob.Path, "error", err)
			continue
		}
		if strings.Contains(string(content), envVar) {
			return true, nil
		}
	}

	return false, nil
}

func init() {
	Register(Convention{
		ID:          "standard-env-vars-in-compose",
		Description: "Standard env vars referenced in code are declared in docker-compose.yml",
		Rationale: "Several incidents have been caused by a service implementing a feature that reads " +
			"an env var, but `docker-compose.yml` not passing that var through to the container. " +
			"The result is silent failure at runtime — the feature simply doesn't work, and there's " +
			"no error to alert on. This convention catches the missing declaration before it causes " +
			"a silent production failure.",
		Guidance: "Add the missing env var(s) to the `environment:` block in `docker-compose.yml`. " +
			"For example:\n\n```yaml\nenvironment:\n  - PORT\n  - LOGANNE_ENDPOINT\n```\n\n" +
			"If the service genuinely does not need the var at runtime (e.g. it only appears in " +
			"test code or documentation), consider whether the code should be restructured to make " +
			"that clearer.",
		AppliesTo: []RepoType{RepoTypeSystem},
		Check: func(repo RepoContext) ConventionResult {
			base := repo.GitHubBaseURL
			if base == "" {
				base = GitHubBaseURL
			}

			// Fetch and parse docker-compose.yml
			content, err := GitHubFileContentFromBase(base, repo.GitHubToken, repo.Name, "docker-compose.yml")
			if err != nil {
				slog.Warn("Convention check failed", "convention", "standard-env-vars-in-compose", "repo", repo.Name, "step", "fetch-compose", "error", err)
				return ConventionResult{
					Convention: "standard-env-vars-in-compose",
					Err:        fmt.Errorf("error fetching docker-compose.yml: %w", err),
				}
			}

			if content == nil {
				return ConventionResult{
					Convention: "standard-env-vars-in-compose",
					Pass:       true,
					Detail:     "docker-compose.yml not found; convention does not apply",
				}
			}

			var compose envVarsComposeFile
			if err := yaml.Unmarshal(content, &compose); err != nil {
				slog.Warn("Convention check failed", "convention", "standard-env-vars-in-compose", "repo", repo.Name, "step", "parse-compose", "error", err)
				return ConventionResult{
					Convention: "standard-env-vars-in-compose",
					Pass:       false,
					Detail:     fmt.Sprintf("Failed to parse docker-compose.yml: %v", err),
				}
			}

			declared := declaredEnvVars(compose)

			// Check each standard var: is it referenced in code but missing from compose?
			var missing []string
			for _, envVar := range standardEnvVars {
				if declared[envVar] {
					continue // Already declared — no problem.
				}

				// Search source files for the env var reference.
				found, err := repoContainsEnvVar(base, repo.GitHubToken, repo.Name, envVar)
				if err != nil {
					slog.Warn("Convention check failed", "convention", "standard-env-vars-in-compose", "repo", repo.Name, "step", "search-code", "envVar", envVar, "error", err)
					return ConventionResult{
						Convention: "standard-env-vars-in-compose",
						Err:        fmt.Errorf("error searching for %s in code: %w", envVar, err),
					}
				}

				if found {
					missing = append(missing, envVar)
				}
			}

			if len(missing) == 0 {
				return ConventionResult{
					Convention: "standard-env-vars-in-compose",
					Pass:       true,
					Detail:     "All standard env vars referenced in code are declared in docker-compose.yml",
				}
			}

			return ConventionResult{
				Convention: "standard-env-vars-in-compose",
				Pass:       false,
				Detail:     fmt.Sprintf("Standard env vars referenced in code but missing from docker-compose.yml: %s", strings.Join(missing, ", ")),
			}
		},
	})
}
