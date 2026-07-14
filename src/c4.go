package main

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"sort"
	"strings"
	"time"
)

// c4InfoTimeout is the maximum time to wait for a /_info response when probing
// system endpoints during C4 generation.
const c4InfoTimeout = 5 * time.Second

// c4GitHubClient is the HTTP client used for all GitHub Contents API calls
// within C4 generation. A 30-second timeout prevents a hung connection from
// stalling the sweep indefinitely.
var c4GitHubClient = &http.Client{Timeout: 30 * time.Second}

// c4SyncEdge is a sync dependency edge in the C4 model, sourced from
// /_info → checks[].dependsOn.
type c4SyncEdge struct {
	From string // system ID (caller)
	To   string // system ID (dependency)
}

// c4AsyncEdge is an async event subscription edge in the C4 model, sourced
// from loganne's webhooks-config.json.
type c4AsyncEdge struct {
	Event    string // loganne event name
	Consumer string // system ID of the webhook target
}

// c4ProducerEdge is an async event production edge in the C4 model, sourced
// from loganne's observed GET /producers map.
type c4ProducerEdge struct {
	Source string // system ID of the event producer
	Event  string // loganne event name emitted
}

// c4InfoCheck holds just the dependsOn field from a single /_info check entry.
// The value may be a string or a JSON array of strings.
type c4InfoCheck struct {
	DependsOn interface{} `json:"dependsOn"` // string, []interface{}, or nil
}

// c4InfoResponse is the subset of /_info we parse for C4 purposes.
type c4InfoResponse struct {
	System string                 `json:"system"`
	Checks map[string]c4InfoCheck `json:"checks"`
}

// c4Divergence is a single detected inconsistency between C4 data sources
// (ADR-0006 §3), annotated so it can be both rendered into divergences.md and
// routed through the audit-finding issue mechanism (ADR-0004, #425): Repo is
// the "{owner}/{repo}" the finding is attributed to, ID is a stable,
// convention-like identifier unique to this specific divergence instance
// (used to open/close its tracked issue across sweeps), and Message is the
// human-readable line used both in the issue body and divergences.md.
type c4Divergence struct {
	Repo    string
	ID      string
	Message string
}

// c4Model holds the complete derived C4 model ready for rendering.
type c4Model struct {
	systems       []configySystem  // sorted by ID
	sysSet        map[string]bool  // set of all system IDs (for edge validation)
	syncEdges     []c4SyncEdge     // sorted by (From, To)
	asyncEdges    []c4AsyncEdge    // sorted by (Consumer, Event)
	producerEdges []c4ProducerEdge // sorted by (Source, Event)
	divergences   []c4Divergence   // sorted by Message
	unreachable   []string         // sorted system IDs that did not respond to /_info
}

// dslIdent converts a system name to a Structurizr DSL-safe identifier by
// replacing hyphens and dots with underscores.
func dslIdent(s string) string {
	return strings.NewReplacer("-", "_", ".", "_").Replace(s)
}

// generateC4DSL produces a Structurizr DSL representation of the C4 model.
// The output matches the format of the prototype-generator.py baseline.
func generateC4DSL(m c4Model) string {
	var b strings.Builder
	b.WriteString("workspace \"lucOS estate\" \"Generated C4 model — DO NOT EDIT BY HAND\" {\n")
	b.WriteString("    model {\n")
	b.WriteString("        lucas = person \"lucas42\"\n")
	for _, sys := range m.systems {
		domain := sys.Domain
		if domain == "" {
			domain = "(no public domain)"
		}
		fmt.Fprintf(&b, "        %s = softwareSystem \"%s\" \"%s\"\n",
			dslIdent(sys.ID), sys.ID, domain)
	}
	b.WriteString("\n")
	b.WriteString("        # sync dependencies (/_info dependsOn)\n")
	for _, edge := range m.syncEdges {
		if m.sysSet[edge.To] {
			fmt.Fprintf(&b, "        %s -> %s \"depends on (sync)\"\n",
				dslIdent(edge.From), dslIdent(edge.To))
		}
	}
	b.WriteString("\n")
	b.WriteString("        # async event producers (→ loganne)\n")
	// Only drawn for real systems (a declared softwareSystem element exists to draw
	// the arrow from) — component/script producers pass validation (#467) but have
	// no C4 element, so they're valid yet not rendered pending a diagram decision.
	for _, edge := range m.producerEdges {
		if m.sysSet[edge.Source] {
			fmt.Fprintf(&b, "        %s -> lucos_loganne \"emits %s\"\n",
				dslIdent(edge.Source), edge.Event)
		}
	}
	b.WriteString("\n")
	b.WriteString("        # async event subscriptions (loganne → consumers)\n")
	for _, edge := range m.asyncEdges {
		fmt.Fprintf(&b, "        lucos_loganne -> %s \"%s\"\n",
			dslIdent(edge.Consumer), edge.Event)
	}
	b.WriteString("    }\n")
	b.WriteString("    views {\n")
	b.WriteString("        systemLandscape \"estate\" { include * autolayout lr }\n")
	b.WriteString("        theme default\n")
	b.WriteString("    }\n")
	b.WriteString("}\n")
	return b.String()
}

// generateC4Mermaid produces a Mermaid flowchart of the connected core
// (systems with at least one edge). The full estate graph is too dense for
// Mermaid; only connected nodes are included.
func generateC4Mermaid(m c4Model) string {
	// Build the connected set: any system that appears in at least one edge.
	connected := make(map[string]bool)
	for _, edge := range m.syncEdges {
		if m.sysSet[edge.To] {
			connected[edge.From] = true
			connected[edge.To] = true
		}
	}
	for _, edge := range m.asyncEdges {
		connected[edge.Consumer] = true
		connected["lucos_loganne"] = true
	}
	// Same real-systems-only restriction as generateC4DSL (see comment there).
	for _, edge := range m.producerEdges {
		if m.sysSet[edge.Source] {
			connected[edge.Source] = true
			connected["lucos_loganne"] = true
		}
	}

	sortedConnected := make([]string, 0, len(connected))
	for s := range connected {
		sortedConnected = append(sortedConnected, s)
	}
	sort.Strings(sortedConnected)

	var b strings.Builder
	b.WriteString("# lucOS estate — connected core (generated)\n")
	b.WriteString("\n")
	b.WriteString("```mermaid\n")
	b.WriteString("flowchart LR\n")
	for _, s := range sortedConnected {
		fmt.Fprintf(&b, "  %s[\"%s\"]\n", dslIdent(s), s)
	}
	b.WriteString("  %% sync deps (solid)\n")
	for _, edge := range m.syncEdges {
		if m.sysSet[edge.To] {
			fmt.Fprintf(&b, "  %s --> %s\n", dslIdent(edge.From), dslIdent(edge.To))
		}
	}
	b.WriteString("  %% async producers (dotted, → loganne)\n")
	// Same real-systems-only restriction as generateC4DSL (see comment there).
	for _, edge := range m.producerEdges {
		if m.sysSet[edge.Source] {
			fmt.Fprintf(&b, "  %s -.%s.-> lucos_loganne\n", dslIdent(edge.Source), edge.Event)
		}
	}
	b.WriteString("  %% async consumers (dotted, loganne →)\n")
	for _, edge := range m.asyncEdges {
		fmt.Fprintf(&b, "  lucos_loganne -.%s.-> %s\n", edge.Event, dslIdent(edge.Consumer))
	}
	b.WriteString("```\n")
	return b.String()
}

// generateC4Divergences produces the divergence audit report — a list of
// inconsistencies found between the different data sources (e.g. a /_info
// system name that disagrees with its configy key, or a loganne webhook
// target that has no matching configy system).
func generateC4Divergences(m c4Model) string {
	var b strings.Builder
	b.WriteString("# Source divergences (audit findings)\n\n")
	if len(m.divergences) == 0 {
		b.WriteString("None.")
	} else {
		lines := make([]string, len(m.divergences))
		for i, d := range m.divergences {
			lines[i] = d.Message
		}
		b.WriteString(strings.Join(lines, "\n"))
	}
	b.WriteString("\n\nUnreachable /_info: ")
	if len(m.unreachable) == 0 {
		b.WriteString("none")
	} else {
		b.WriteString(strings.Join(m.unreachable, ", "))
	}
	b.WriteString("\n")
	return b.String()
}

// probeInfoEndpoint GETs a system's /_info endpoint and extracts the reported
// system name and any dependsOn dependency names from the checks map.
// Returns ("", nil, err) on network or parse error.
func probeInfoEndpoint(domain string, client *http.Client) (reportedName string, deps []string, err error) {
	url := "https://" + domain + "/_info"
	resp, err := client.Get(url) //nolint:gosec // domain is from trusted configy config
	if err != nil {
		return "", nil, err
	}
	defer func() {
		_, _ = io.Copy(io.Discard, resp.Body)
		resp.Body.Close()
	}()
	if resp.StatusCode != http.StatusOK {
		return "", nil, fmt.Errorf("/_info returned HTTP %d", resp.StatusCode)
	}
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", nil, fmt.Errorf("failed to read /_info body: %w", err)
	}
	var info c4InfoResponse
	if err := json.Unmarshal(body, &info); err != nil {
		return "", nil, fmt.Errorf("failed to parse /_info JSON: %w", err)
	}
	reportedName = info.System
	for _, chk := range info.Checks {
		switch v := chk.DependsOn.(type) {
		case string:
			if v != "" {
				deps = append(deps, v)
			}
		case []interface{}:
			for _, item := range v {
				if s, ok := item.(string); ok && s != "" {
					deps = append(deps, s)
				}
			}
		}
	}
	return reportedName, deps, nil
}

// probeLoganneProducers GETs a loganne instance's /producers endpoint and
// returns the observed source→event-type map. The client should have a timeout.
// Returns (nil, err) on network or parse error.
func probeLoganneProducers(domain string, client *http.Client) (map[string][]string, error) {
	url := "https://" + domain + "/producers"
	resp, err := client.Get(url) //nolint:gosec // domain is from trusted configy config
	if err != nil {
		return nil, fmt.Errorf("/producers GET failed: %w", err)
	}
	defer func() {
		_, _ = io.Copy(io.Discard, resp.Body)
		resp.Body.Close()
	}()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("/producers returned HTTP %d", resp.StatusCode)
	}
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("failed to read /producers body: %w", err)
	}
	var result map[string][]string
	if err := json.Unmarshal(body, &result); err != nil {
		return nil, fmt.Errorf("failed to parse /producers JSON: %w", err)
	}
	return result, nil
}

// parseLoganneProducers converts the raw /producers map into sorted producer
// edges, recording a divergence for any source that isn't in validSources —
// the union of configy systems, components, and scripts (#467; a producer
// like lucos_agent is a legitimate configy script, not a system, so
// validating against systems alone false-positives on it). Divergences are
// attributed to lucos_loganne — it's loganne's observed /producers data that
// disagrees with configy, not any specific unrecognised source's own repo
// (which may not even be a real system).
func parseLoganneProducers(raw map[string][]string, validSources map[string]bool, githubOrg string) ([]c4ProducerEdge, []c4Divergence) {
	var edges []c4ProducerEdge
	var divergences []c4Divergence
	for source, types := range raw {
		if !validSources[source] {
			divergences = append(divergences, c4Divergence{
				Repo:    githubOrg + "/lucos_loganne",
				ID:      "c4-loganne-producer-" + dslIdent(source),
				Message: fmt.Sprintf("- loganne producer `%s` is not a known configy system, component, or script", source),
			})
			continue
		}
		for _, eventType := range types {
			edges = append(edges, c4ProducerEdge{Source: source, Event: eventType})
		}
	}
	sort.Slice(edges, func(i, j int) bool {
		if edges[i].Source != edges[j].Source {
			return edges[i].Source < edges[j].Source
		}
		return edges[i].Event < edges[j].Event
	})
	sort.Slice(divergences, func(i, j int) bool { return divergences[i].Message < divergences[j].Message })
	return edges, divergences
}

// parseLoganneWebhooks parses loganne's webhooks-config.json and returns a
// sorted list of async edges (event → consumer system) and any divergences
// for webhook targets that don't map to a known configy system. Divergences
// are attributed to lucos_loganne — its own webhooks-config.json is what
// references a domain configy doesn't recognise. domain2sys maps public
// domains (e.g. "arachne.l42.eu") to system IDs.
func parseLoganneWebhooks(data []byte, domain2sys map[string]string, githubOrg string) ([]c4AsyncEdge, []c4Divergence) {
	var raw map[string]json.RawMessage
	if err := json.Unmarshal(data, &raw); err != nil {
		return nil, []c4Divergence{{
			Repo:    githubOrg + "/lucos_loganne",
			ID:      "c4-loganne-webhooks-config-unparseable",
			Message: "- failed to parse loganne webhooks-config.json: " + err.Error(),
		}}
	}

	var edges []c4AsyncEdge
	var divergences []c4Divergence
	seenDivergences := make(map[string]bool)

	for key, val := range raw {
		// consumerTokens maps domains to env-var names — not an event list.
		if key == "consumerTokens" {
			continue
		}
		// Each remaining key should be an event name with a []string URL list.
		var urls []string
		if err := json.Unmarshal(val, &urls); err != nil {
			continue // not an event list (e.g. some other field type) — skip
		}
		event := key
		for _, rawURL := range urls {
			// Extract hostname: "https://arachne.l42.eu/webhook" → "arachne.l42.eu"
			parts := strings.SplitN(rawURL, "/", 4)
			if len(parts) < 3 {
				continue
			}
			dom := parts[2]
			sysID, ok := domain2sys[dom]
			if !ok {
				id := "c4-loganne-webhook-target-" + dslIdent(event) + "-" + dslIdent(dom)
				if !seenDivergences[id] {
					seenDivergences[id] = true
					divergences = append(divergences, c4Divergence{
						Repo:    githubOrg + "/lucos_loganne",
						ID:      id,
						Message: fmt.Sprintf("- loganne event `%s` -> `%s` has no matching configy system", event, dom),
					})
				}
				continue
			}
			edges = append(edges, c4AsyncEdge{Event: event, Consumer: sysID})
		}
	}

	sort.Slice(edges, func(i, j int) bool {
		if edges[i].Consumer != edges[j].Consumer {
			return edges[i].Consumer < edges[j].Consumer
		}
		return edges[i].Event < edges[j].Event
	})
	sort.Slice(divergences, func(i, j int) bool { return divergences[i].Message < divergences[j].Message })
	return edges, divergences
}

// c4GitHubFile holds the SHA and decoded content of a file fetched via the
// GitHub Contents API.
type c4GitHubFile struct {
	SHA     string
	Content string
}

// fetchC4GitHubFile fetches a file's SHA and decoded content from the GitHub
// Contents API. Returns (nil, nil) if the file does not exist (404).
func (s *AuditSweeper) fetchC4GitHubFile(token, repo, path string) (*c4GitHubFile, error) {
	url := fmt.Sprintf("%s/repos/%s/contents/%s", s.githubAPIBaseURL, repo, path)
	req, err := http.NewRequest(http.MethodGet, url, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to build request: %w", err)
	}
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Accept", "application/vnd.github+json")
	req.Header.Set("X-GitHub-Api-Version", "2022-11-28")

	resp, err := c4GitHubClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("GitHub API request failed: %w", err)
	}
	defer func() {
		_, _ = io.Copy(io.Discard, resp.Body)
		resp.Body.Close()
	}()

	switch resp.StatusCode {
	case http.StatusOK:
		var result struct {
			SHA      string `json:"sha"`
			Content  string `json:"content"`
			Encoding string `json:"encoding"`
		}
		body, err := io.ReadAll(resp.Body)
		if err != nil {
			return nil, fmt.Errorf("failed to read response body: %w", err)
		}
		if err := json.Unmarshal(body, &result); err != nil {
			return nil, fmt.Errorf("failed to decode contents response: %w", err)
		}
		if result.Encoding != "base64" {
			return nil, fmt.Errorf("unexpected encoding %q for %s in %s", result.Encoding, path, repo)
		}
		decoded, err := base64.StdEncoding.DecodeString(
			strings.ReplaceAll(result.Content, "\n", ""))
		if err != nil {
			return nil, fmt.Errorf("failed to base64-decode %s in %s: %w", path, repo, err)
		}
		return &c4GitHubFile{SHA: result.SHA, Content: string(decoded)}, nil
	case http.StatusNotFound:
		return nil, nil
	default:
		return nil, fmt.Errorf("GitHub Contents API returned %d for %s in %s", resp.StatusCode, path, repo)
	}
}

// putC4GitHubFile creates or updates a file via the GitHub Contents API.
// If currentSHA is non-empty, the request includes it (required for updates).
// A successful response is 200 (update) or 201 (create).
func (s *AuditSweeper) putC4GitHubFile(token, repo, path, content, currentSHA, message string) error {
	payload := map[string]interface{}{
		"message": message,
		"content": base64.StdEncoding.EncodeToString([]byte(content)),
	}
	if currentSHA != "" {
		payload["sha"] = currentSHA
	}
	body, err := json.Marshal(payload)
	if err != nil {
		return fmt.Errorf("failed to marshal PUT payload: %w", err)
	}

	url := fmt.Sprintf("%s/repos/%s/contents/%s", s.githubAPIBaseURL, repo, path)
	req, err := http.NewRequest(http.MethodPut, url, bytes.NewReader(body))
	if err != nil {
		return fmt.Errorf("failed to build PUT request: %w", err)
	}
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/vnd.github+json")
	req.Header.Set("X-GitHub-Api-Version", "2022-11-28")

	resp, err := c4GitHubClient.Do(req)
	if err != nil {
		return fmt.Errorf("GitHub Contents PUT failed: %w", err)
	}
	defer func() {
		_, _ = io.Copy(io.Discard, resp.Body)
		resp.Body.Close()
	}()

	if resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusCreated {
		respBody, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("GitHub Contents PUT returned %d: %s", resp.StatusCode, string(respBody))
	}
	return nil
}

// generateAndCommitC4 runs the full C4 model generation pipeline and commits
// the output artifacts (model.dsl, landscape.md, divergences.md) to the
// dedicated s.c4OutputRepo (lucas42/lucos_architecture_models), authenticated
// as the scoped lucos-architecture-writer App (see #446 / ADR-0008). Files
// are only committed if their content has changed. Reading loganne's
// webhooks-config.json and probing /_info still uses the main estate-read
// App (s.githubAuth), which stays read-only w.r.t. other repos.
//
// The returned error reflects whether the pipeline produced and wrote back a
// fresh model. It is non-critical to the audit sweep result (the caller does
// not fail the sweep on it) but is reported to schedule-tracker under its own
// "c4-model" job so a broken write-back is surfaced rather than silently
// leaving the committed model stale (#445).
func (s *AuditSweeper) generateAndCommitC4() error {
	slog.Info("C4 model generation starting")

	token, err := s.githubAuth.GetInstallationToken()
	if err != nil {
		slog.Warn("C4: failed to obtain GitHub token", "error", err)
		return fmt.Errorf("failed to obtain GitHub token: %w", err)
	}

	// 1. Fetch all systems from configy (including domain field).
	systems, err := s.fetchConfigySystems()
	if err != nil {
		slog.Warn("C4: failed to fetch configy systems", "error", err)
		return fmt.Errorf("failed to fetch configy systems: %w", err)
	}

	// Sort systems by ID for deterministic output.
	sort.Slice(systems, func(i, j int) bool { return systems[i].ID < systems[j].ID })

	// Build domain → system ID lookup for loganne webhook resolution.
	domain2sys := make(map[string]string, len(systems))
	sysSet := make(map[string]bool, len(systems))
	for _, sys := range systems {
		sysSet[sys.ID] = true
		if sys.Domain != "" {
			domain2sys[sys.Domain] = sys.ID
		}
	}

	// 2. Fetch loganne's webhooks-config.json via GitHub Contents API.
	loganneFile, err := s.fetchC4GitHubFile(token,
		s.githubOrg+"/lucos_loganne", "src/webhooks-config.json")
	if err != nil {
		slog.Warn("C4: failed to fetch loganne webhooks config", "error", err)
		return fmt.Errorf("failed to fetch loganne webhooks config: %w", err)
	}
	if loganneFile == nil {
		slog.Warn("C4: loganne webhooks-config.json not found")
		return errors.New("loganne webhooks-config.json not found")
	}

	asyncEdges, asyncDivergences := parseLoganneWebhooks([]byte(loganneFile.Content), domain2sys, s.githubOrg)

	// Shared HTTP client for all live endpoint probes (/_info and /producers).
	infoClient := &http.Client{Timeout: c4InfoTimeout}

	// Loganne producers may be configy systems, components, or scripts (#467)
	// — a legitimate producer like lucos_agent lives in scripts.yaml, not
	// systems.yaml, so validating against sysSet alone false-positives on it.
	// Fetch failures here are non-critical: they just mean components/scripts
	// won't be recognised as valid producers this sweep (degrading to the
	// pre-fix systems-only validation), not a sweep error.
	producerValidSources := make(map[string]bool, len(sysSet))
	for id := range sysSet {
		producerValidSources[id] = true
	}
	if components, err := s.fetchConfigyComponents(); err != nil {
		slog.Warn("C4: failed to fetch configy components for producer validation", "error", err)
	} else {
		for _, c := range components {
			producerValidSources[c.ID] = true
		}
	}
	if scripts, err := s.fetchConfigyScripts(); err != nil {
		slog.Warn("C4: failed to fetch configy scripts for producer validation", "error", err)
	} else {
		for _, sc := range scripts {
			producerValidSources[sc.ID] = true
		}
	}

	// 2b. Probe loganne's /producers endpoint for observed async-producer edges.
	// This is non-critical: failure is logged and produces an empty producer set,
	// not a sweep error. The loganne domain is resolved from the domain2sys map.
	var producerEdges []c4ProducerEdge
	var producerDivergences []c4Divergence
	loganneDomain := ""
	for d, id := range domain2sys {
		if id == "lucos_loganne" {
			loganneDomain = d
			break
		}
	}
	if loganneDomain != "" {
		raw, probeErr := probeLoganneProducers(loganneDomain, infoClient)
		if probeErr != nil {
			slog.Warn("C4: failed to probe loganne /producers", "error", probeErr)
		} else {
			producerEdges, producerDivergences = parseLoganneProducers(raw, producerValidSources, s.githubOrg)
		}
	} else {
		slog.Warn("C4: lucos_loganne has no domain in configy; skipping /producers probe")
	}

	// 3. Probe /_info for each system that has a public domain.
	var syncEdges []c4SyncEdge
	var syncDivergences []c4Divergence
	var unreachable []string

	for _, sys := range systems {
		if sys.Domain == "" {
			continue
		}
		reportedName, deps, probeErr := probeInfoEndpoint(sys.Domain, infoClient)
		if probeErr != nil {
			slog.Debug("C4: /_info unreachable",
				"system", sys.ID, "domain", sys.Domain, "error", probeErr)
			unreachable = append(unreachable, sys.ID)
			continue
		}
		// Flag a divergence if the reported system name disagrees with the configy
		// key. Attributed to the system's own repo — it's that system's own
		// /_info that disagrees with the canonical configy key (#425).
		if reportedName != "" && reportedName != sys.ID {
			syncDivergences = append(syncDivergences, c4Divergence{
				Repo: s.githubOrg + "/" + sys.ID,
				ID:   "c4-info-system-name-divergence",
				Message: fmt.Sprintf(
					"- `%s` (configy) reports `system: %s` in /_info on %s",
					sys.ID, reportedName, sys.Domain),
			})
		}
		// ADR-0006 §3: loganne's /_info dependsOn edges are the broker/alert-suppression
		// relationship — they are NOT sync dependencies. Skip them here; the loganne
		// async layer (webhooks-config.json) already captures the fan-out correctly.
		if sys.ID == "lucos_loganne" {
			continue
		}
		for _, dep := range deps {
			syncEdges = append(syncEdges, c4SyncEdge{From: sys.ID, To: dep})
		}
	}

	sort.Slice(syncEdges, func(i, j int) bool {
		if syncEdges[i].From != syncEdges[j].From {
			return syncEdges[i].From < syncEdges[j].From
		}
		return syncEdges[i].To < syncEdges[j].To
	})

	allDivergences := append(syncDivergences, asyncDivergences...)
	allDivergences = append(allDivergences, producerDivergences...)
	sort.Slice(allDivergences, func(i, j int) bool { return allDivergences[i].Message < allDivergences[j].Message })
	sort.Strings(unreachable)

	// Route each divergence through the audit-finding issue mechanism
	// (ADR-0004), so model drift raises/clears a tracked issue exactly as a
	// convention breach does (#425). Uses the estate-read App's token — the
	// same one the main sweep already uses for ordinary convention issues, no
	// new privilege needed. Non-critical: errors are logged inside and don't
	// affect the rest of C4 generation/write-back.
	s.routeC4DivergencesToIssues(token, systems, allDivergences)

	model := c4Model{
		systems:       systems,
		sysSet:        sysSet,
		syncEdges:     syncEdges,
		asyncEdges:    asyncEdges,
		producerEdges: producerEdges,
		divergences:   allDivergences,
		unreachable:   unreachable,
	}

	dsl := generateC4DSL(model)
	mermaid := generateC4Mermaid(model)
	divs := generateC4Divergences(model)

	slog.Info("C4 model generated",
		"systems", len(systems),
		"sync_edges", len(syncEdges),
		"async_edges", len(asyncEdges),
		"producer_edges", len(producerEdges),
		"divergences", len(allDivergences),
		"unreachable", len(unreachable),
	)

	// 4. Commit changed artifacts to the dedicated output repo, authenticated
	// as the scoped lucos-architecture-writer App (#446). Its absence (e.g. in
	// dev, where no writer-App installation exists) is a real failure of this
	// stage, not a silent no-op — the whole point of #445/#446 is that a
	// broken write-back must be visible.
	if s.c4WriteAuth == nil {
		return errors.New("C4 write-back skipped: lucos-architecture-writer credentials not configured")
	}
	writeToken, err := s.c4WriteAuth.GetInstallationToken()
	if err != nil {
		slog.Warn("C4: failed to obtain lucos-architecture-writer GitHub token", "error", err)
		return fmt.Errorf("failed to obtain lucos-architecture-writer GitHub token: %w", err)
	}

	commitMsg := "Auto-generate C4 estate model from sweep\n\n" +
		"Regenerated from configy systems, loganne webhooks, loganne /producers, and /_info probes."

	artifacts := []struct {
		path    string
		content string
	}{
		{"model.dsl", dsl},
		{"landscape.md", mermaid},
		{"divergences.md", divs},
	}

	var writeErr error
	for _, artifact := range artifacts {
		current, err := s.fetchC4GitHubFile(writeToken, s.c4OutputRepo, artifact.path)
		if err != nil {
			slog.Warn("C4: failed to fetch current file content",
				"path", artifact.path, "error", err)
			writeErr = errors.Join(writeErr,
				fmt.Errorf("failed to fetch current content of %s: %w", artifact.path, err))
			continue
		}
		if current != nil && current.Content == artifact.content {
			slog.Debug("C4: artifact unchanged, skipping commit", "path", artifact.path)
			continue
		}
		currentSHA := ""
		if current != nil {
			currentSHA = current.SHA
		}
		if err := s.putC4GitHubFile(writeToken, s.c4OutputRepo, artifact.path,
			artifact.content, currentSHA, commitMsg); err != nil {
			slog.Warn("C4: failed to commit artifact", "path", artifact.path, "error", err)
			writeErr = errors.Join(writeErr,
				fmt.Errorf("failed to write back %s: %w", artifact.path, err))
		} else {
			slog.Info("C4: committed artifact", "path", artifact.path)
		}
	}
	return writeErr
}

// c4DivergenceIDPrefix namespaces all C4-divergence audit-finding issues so
// they can be found and reconciled independently of ordinary per-repo
// conventions (#425).
const c4DivergenceIDPrefix = "c4-"

// routeC4DivergencesToIssues raises or closes an audit-finding issue (ADR-0004)
// for each C4 model divergence (ADR-0006 §3), so drift between data sources is
// tracked exactly like a convention breach: a currently-detected divergence
// gets an open issue; a divergence that no longer reproduces gets its issue
// closed.
//
// Divergences aren't a fixed enumerable (repo × convention) grid the way
// ordinary conventions are, so closure can't rely on "the sweep already
// visited this repo+ID and it passed" — resolution has to be detected by
// reconciling against every repo a divergence could possibly be attributed to
// (every system's own repo, for sync divergences, plus lucos_loganne for
// async/producer divergences) rather than only the repos with a divergence
// this run.
//
// Errors from individual issue operations are logged and skipped — one
// GitHub API hiccup shouldn't abort the rest of the reconciliation, and this
// whole step is non-critical to C4 generation/write-back.
func (s *AuditSweeper) routeC4DivergencesToIssues(token string, systems []configySystem, divergences []c4Divergence) {
	if s.issueClientFactory == nil {
		slog.Warn("C4: issueClientFactory not configured, skipping divergence routing")
		return
	}
	issueClient := s.issueClientFactory(token)

	byRepo := make(map[string][]c4Divergence, len(divergences))
	for _, d := range divergences {
		byRepo[d.Repo] = append(byRepo[d.Repo], d)
	}

	// lucos_loganne is a system in its own right, so it's usually already
	// covered by the systems loop below — the explicit add only matters if
	// configy somehow lacks it. Dedup via a set either way, so it's never
	// processed twice.
	repoSet := make(map[string]bool, len(systems)+1)
	for _, sys := range systems {
		repoSet[s.githubOrg+"/"+sys.ID] = true
	}
	repoSet[s.githubOrg+"/lucos_loganne"] = true

	candidateRepos := make([]string, 0, len(repoSet))
	for repo := range repoSet {
		candidateRepos = append(candidateRepos, repo)
	}

	for _, repo := range candidateRepos {
		current := byRepo[repo]
		currentIDs := make(map[string]bool, len(current))
		for _, d := range current {
			currentIDs[d.ID] = true
			if _, err := issueClient.EnsureIssueExists(repo, ConventionInfo{
				ID:          d.ID,
				Description: "C4 model divergence",
				Detail:      d.Message,
			}); err != nil {
				slog.Warn("C4: failed to ensure divergence issue exists", "repo", repo, "id", d.ID, "error", err)
			}
		}

		open, err := issueClient.findOpenIssuesByIDPrefix(repo, c4DivergenceIDPrefix)
		if err != nil {
			slog.Warn("C4: failed to list open divergence issues for reconciliation", "repo", repo, "error", err)
			continue
		}
		for _, issue := range open {
			id := conventionIDFromTitle(issue.Title)
			if currentIDs[id] {
				continue
			}
			if err := issueClient.CloseIssueIfOpen(repo, ConventionInfo{ID: id}); err != nil {
				slog.Warn("C4: failed to close resolved divergence issue",
					"repo", repo, "id", id, "issue", issue.Number, "error", err)
			}
		}
	}
}
