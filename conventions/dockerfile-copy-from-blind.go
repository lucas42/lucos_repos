package conventions

import (
	"fmt"
	"log/slog"
	"strings"

	"gopkg.in/yaml.v3"
)

// composeBuildAdditionalContexts extracts the additional_contexts name→value
// pairs from a service's build config. It accepts both the list form
// (["key=value", ...]) and the map form ({key: value, ...}). Returns a map of
// lowercased context name to raw value string. Returns an empty map when the
// build config is a plain string (e.g. build: ".") or has no additional_contexts.
func composeBuildAdditionalContexts(build interface{}) map[string]string {
	result := map[string]string{}
	buildMap, ok := build.(map[string]interface{})
	if !ok {
		return result
	}
	raw, ok := buildMap["additional_contexts"]
	if !ok {
		return result
	}
	switch v := raw.(type) {
	case []interface{}:
		// List form: ["name=value", ...]
		for _, item := range v {
			s, ok := item.(string)
			if !ok {
				continue
			}
			idx := strings.Index(s, "=")
			if idx < 0 {
				continue
			}
			key := strings.ToLower(strings.TrimSpace(s[:idx]))
			val := strings.TrimSpace(s[idx+1:])
			if key != "" {
				result[key] = val
			}
		}
	case map[string]interface{}:
		// Map form: {name: value, ...}
		for k, val := range v {
			if s, ok := val.(string); ok {
				result[strings.ToLower(k)] = s
			}
		}
	}
	return result
}

// isDockerImageContext reports whether an additional_contexts value refers to
// a Docker image (i.e. starts with "docker-image://").
func isDockerImageContext(val string) bool {
	return strings.HasPrefix(val, "docker-image://")
}

// dockerfileNamedStages returns the set of stage identifiers (lowercased)
// that a Dockerfile declares via "FROM ... AS <name>" instructions. Numeric
// stage indices ("0", "1", ...) are also included so they can be used as
// COPY --from=0 targets without triggering a finding.
func dockerfileNamedStages(content []byte) map[string]bool {
	stages := map[string]bool{}
	stageIdx := 0
	for _, rawLine := range strings.Split(string(content), "\n") {
		line := strings.TrimSpace(rawLine)
		if !strings.HasPrefix(strings.ToUpper(line), "FROM ") {
			continue
		}
		// Record numeric index of this stage.
		stages[fmt.Sprintf("%d", stageIdx)] = true
		stageIdx++

		// Look for an "AS <name>" suffix (case-insensitive).
		upper := strings.ToUpper(line)
		asIdx := strings.LastIndex(upper, " AS ")
		if asIdx < 0 {
			continue
		}
		name := strings.TrimSpace(line[asIdx+4:])
		if name != "" {
			stages[strings.ToLower(name)] = true
		}
	}
	return stages
}

// dockerfileBlindCopyFromImages returns the image references used in
// "COPY --from=<T>" instructions that are NOT declared as a named stage in the
// same Dockerfile and NOT bound to a local-path additional_context. Only
// references that look like external image references (containing '/', ':', or
// '@') are returned — bare names like "build" or "config" are ignored.
func dockerfileBlindCopyFromImages(content []byte, namedStages, localContexts map[string]bool) []string {
	var findings []string
	seen := map[string]bool{}
	for _, rawLine := range strings.Split(string(content), "\n") {
		line := strings.TrimSpace(rawLine)
		if !strings.HasPrefix(strings.ToUpper(line), "COPY ") {
			continue
		}
		lower := strings.ToLower(line)
		fromIdx := strings.Index(lower, "--from=")
		if fromIdx < 0 {
			continue
		}
		after := line[fromIdx+7:] // skip "--from="
		// T ends at the first space or tab.
		endIdx := strings.IndexAny(after, " \t")
		var t string
		if endIdx < 0 {
			t = after
		} else {
			t = after[:endIdx]
		}
		if t == "" {
			continue
		}
		tLower := strings.ToLower(t)

		// Skip if this is a named stage or a numeric stage index in this Dockerfile.
		if namedStages[tLower] {
			continue
		}
		// Skip if this name is bound to a local-path additional_context
		// (e.g. lucos_configy's "config=config" mapping).
		if localContexts[tLower] {
			continue
		}
		// Only flag T values that look like external image references.
		// A bare stage alias ("build", "config") contains none of '/', ':', '@'.
		if !strings.ContainsAny(t, "/:@") {
			continue
		}
		if !seen[tLower] {
			seen[tLower] = true
			findings = append(findings, t)
		}
	}
	return findings
}

// dockerfileHasArgInFrom reports whether a Dockerfile uses ARG variable
// substitution in a FROM instruction's image reference (e.g. FROM ${BASE}).
// Such a Dockerfile is invisible to Dependabot's docker parser.
func dockerfileHasArgInFrom(content []byte) bool {
	for _, rawLine := range strings.Split(string(content), "\n") {
		line := strings.TrimSpace(rawLine)
		if !strings.HasPrefix(strings.ToUpper(line), "FROM ") {
			continue
		}
		image := line[5:] // skip "FROM "
		// Strip any " AS <name>" suffix.
		if idx := strings.Index(strings.ToUpper(image), " AS "); idx >= 0 {
			image = image[:idx]
		}
		image = strings.TrimSpace(image)
		if strings.Contains(image, "$") {
			return true
		}
	}
	return false
}

func init() {
	Register(Convention{
		ID:          "dockerfile-copy-from-dependabot-blind",
		Description: "No Dockerfile uses COPY --from=<external-image> without declaring that image as a named FROM stage, which would hide it from Dependabot",
		Rationale: "Dependabot's Docker ecosystem parser only scans FROM instructions for update PRs. A COPY --from=<external-image> that is not backed by a matching FROM <image> AS <stage> instruction is completely invisible to Dependabot — no version-bump PRs are ever opened, no error is shown, and the pinned image silently goes stale. This is a security-relevant gap: a shared data or artifact image pinned this way will never receive vulnerability-bump PRs. The same blind spot applies to FROM ${VAR} (ARG-in-FROM) and to additional_contexts entries that reference images via docker-image:// URLs.",
		Guidance: "Declare the image as a named build stage and reference that stage name in COPY:\n\n```dockerfile\n# Instead of:\nCOPY --from=registry.example.com/my-image:1.2.3@sha256:abc... /src /dst\n\n# Use:\nFROM registry.example.com/my-image:1.2.3@sha256:abc... AS my-stage\n# ... other build steps ...\nCOPY --from=my-stage /src /dst\n```\n\nUse both a tag AND a digest (image:tag@sha256:...) — a tag alone is mutable, a digest alone tracks latest with no semver signal. Dependabot bumps images that carry both a tag and a digest in a FROM instruction.",
		AppliesTo: []RepoType{RepoTypeSystem},
		Check: func(repo RepoContext) ConventionResult {
			base := repo.GitHubBaseURL
			if base == "" {
				base = GitHubBaseURL
			}
			const conventionID = "dockerfile-copy-from-dependabot-blind"

			// Step 1: fetch and parse docker-compose.yml.
			composeContent, err := GitHubFileContentFromBase(base, repo.GitHubToken, repo.Name, "docker-compose.yml", repo.Ref)
			if err != nil {
				slog.Warn("Convention check failed", "convention", conventionID, "repo", repo.Name, "step", "fetch-compose", "error", err)
				return ConventionResult{
					Convention: conventionID,
					Err:        fmt.Errorf("error fetching docker-compose.yml: %w", err),
				}
			}
			if composeContent == nil {
				return ConventionResult{
					Convention: conventionID,
					Pass:       true,
					Detail:     "docker-compose.yml not found; convention does not apply",
				}
			}

			var compose composeFile
			if err := yaml.Unmarshal(composeContent, &compose); err != nil {
				slog.Warn("Convention check failed", "convention", conventionID, "repo", repo.Name, "step", "parse-compose", "error", err)
				return ConventionResult{
					Convention: conventionID,
					Pass:       false,
					Detail:     fmt.Sprintf("Failed to parse docker-compose.yml: %v", err),
				}
			}

			// Step 2: identify built services and collect per-Dockerfile metadata.
			// dfLocalContexts maps Dockerfile path → set of local-path additional_context names.
			dfLocalContexts := map[string]map[string]bool{}
			var dockerImageContextFindings []string // secondary check B findings

			for _, svc := range compose.Services {
				if svc.Build == nil || isTestProfileService(svc) {
					continue
				}
				dfPath := dockerfilePathForBuild(svc.Build)
				ctxMap := composeBuildAdditionalContexts(svc.Build)

				// Initialise the local-contexts set for this Dockerfile if needed.
				if dfLocalContexts[dfPath] == nil {
					dfLocalContexts[dfPath] = map[string]bool{}
				}
				for name, val := range ctxMap {
					if isDockerImageContext(val) {
						// Secondary check B: additional_contexts that reference a Docker image
						// via docker-image:// are also invisible to Dependabot.
						dockerImageContextFindings = append(dockerImageContextFindings,
							fmt.Sprintf("%s=%s", name, val))
					} else {
						// Local-path context: record its name so COPY --from=<name> is
						// not flagged as an external-image reference.
						dfLocalContexts[dfPath][name] = true
					}
				}
			}

			if len(dfLocalContexts) == 0 {
				return ConventionResult{
					Convention: conventionID,
					Pass:       true,
					Detail:     "No built services found; convention does not apply",
				}
			}

			// Step 3: check each Dockerfile (deduplicated by path).
			var blindCopyFromFindings []string
			var argInFromFindings []string

			for dfPath, localCtxs := range dfLocalContexts {
				dfContent, err := GitHubFileContentFromBase(base, repo.GitHubToken, repo.Name, dfPath, repo.Ref)
				if err != nil {
					slog.Warn("Convention check failed", "convention", conventionID, "repo", repo.Name, "step", "fetch-dockerfile", "path", dfPath, "error", err)
					return ConventionResult{
						Convention: conventionID,
						Err:        fmt.Errorf("error fetching %s: %w", dfPath, err),
					}
				}
				if dfContent == nil {
					return ConventionResult{
						Convention: conventionID,
						Pass:       false,
						Detail:     fmt.Sprintf("%s not found", dfPath),
					}
				}

				namedStages := dockerfileNamedStages(dfContent)

				// Primary check: COPY --from=<external-image> with no matching FROM stage.
				blind := dockerfileBlindCopyFromImages(dfContent, namedStages, localCtxs)
				for _, img := range blind {
					blindCopyFromFindings = append(blindCopyFromFindings,
						fmt.Sprintf("%s: COPY --from=%s", dfPath, img))
				}

				// Secondary check A: FROM with ARG variable substitution.
				if dockerfileHasArgInFrom(dfContent) {
					argInFromFindings = append(argInFromFindings, dfPath)
				}
			}

			// Step 4: collect all findings and return.
			var parts []string
			if len(blindCopyFromFindings) > 0 {
				parts = append(parts, fmt.Sprintf("COPY --from without a FROM stage (invisible to Dependabot): %s",
					strings.Join(blindCopyFromFindings, "; ")))
			}
			if len(argInFromFindings) > 0 {
				parts = append(parts, fmt.Sprintf("FROM uses ARG variable substitution (invisible to Dependabot): %s",
					strings.Join(argInFromFindings, ", ")))
			}
			if len(dockerImageContextFindings) > 0 {
				parts = append(parts, fmt.Sprintf("additional_contexts uses docker-image:// (invisible to Dependabot): %s",
					strings.Join(dockerImageContextFindings, ", ")))
			}

			if len(parts) == 0 {
				return ConventionResult{
					Convention: conventionID,
					Pass:       true,
					Detail:     "No Dependabot-blind image references found",
				}
			}
			return ConventionResult{
				Convention: conventionID,
				Pass:       false,
				Detail:     strings.Join(parts, "; "),
			}
		},
	})
}
