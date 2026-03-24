package conventions

import (
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/url"
	"strings"

	"gopkg.in/yaml.v3"
)

// standardEnvVars is the list of well-known env vars that should be declared
// in docker-compose.yml if the repo's code references them. Start narrow and
// expand once the false-positive rate is understood.
var standardEnvVars = []string{
	"LOGANNE_ENDPOINT",
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

// codeSearchResponse is the subset of the GitHub code search API response we need.
type codeSearchResponse struct {
	TotalCount int `json:"total_count"`
}

// GitHubCodeSearchCount checks how many code results exist for a query in a repo.
// It returns the total_count from the search API.
func GitHubCodeSearchCount(baseURL, token, repo, query string) (int, error) {
	searchURL := fmt.Sprintf("%s/search/code?q=%s", baseURL, url.QueryEscape(query+" repo:"+repo))
	req, err := http.NewRequest("GET", searchURL, nil)
	if err != nil {
		return 0, fmt.Errorf("failed to build search request: %w", err)
	}
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Accept", "application/vnd.github+json")
	req.Header.Set("X-GitHub-Api-Version", "2022-11-28")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return 0, fmt.Errorf("GitHub search API request failed: %w", err)
	}
	defer func() {
		_, _ = io.Copy(io.Discard, resp.Body)
		resp.Body.Close()
	}()

	if resp.StatusCode != http.StatusOK {
		return 0, fmt.Errorf("unexpected GitHub search API status %d for query %q in %s", resp.StatusCode, query, repo)
	}

	var result codeSearchResponse
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return 0, fmt.Errorf("failed to decode search response: %w", err)
	}
	return result.TotalCount, nil
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

				// Search for usage in code.
				count, err := GitHubCodeSearchCount(base, repo.GitHubToken, repo.Name, envVar)
				if err != nil {
					slog.Warn("Convention check failed", "convention", "standard-env-vars-in-compose", "repo", repo.Name, "step", "search-code", "envVar", envVar, "error", err)
					return ConventionResult{
						Convention: "standard-env-vars-in-compose",
						Err:        fmt.Errorf("error searching for %s in code: %w", envVar, err),
					}
				}

				if count > 0 {
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
