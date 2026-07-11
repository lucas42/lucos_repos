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

// c4Model holds the complete derived C4 model ready for rendering.
type c4Model struct {
	systems       []configySystem  // sorted by ID
	sysSet        map[string]bool  // set of all system IDs (for edge validation)
	syncEdges     []c4SyncEdge     // sorted by (From, To)
	asyncEdges    []c4AsyncEdge    // sorted by (Consumer, Event)
	producerEdges []c4ProducerEdge // sorted by (Source, Event)
	divergences   []string         // sorted; each line starts with "- "
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
		b.WriteString(strings.Join(m.divergences, "\n"))
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
// edges, recording a divergence for any source that isn't in the configy sysSet.
func parseLoganneProducers(raw map[string][]string, sysSet map[string]bool) ([]c4ProducerEdge, []string) {
	var edges []c4ProducerEdge
	var divergences []string
	for source, types := range raw {
		if !sysSet[source] {
			divergences = append(divergences,
				fmt.Sprintf("- loganne producer `%s` is not a known configy system", source))
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
	sort.Strings(divergences)
	return edges, divergences
}

// parseLoganneWebhooks parses loganne's webhooks-config.json and returns a
// sorted list of async edges (event → consumer system) and any divergence
// messages for webhook targets that don't map to a known configy system.
// domain2sys maps public domains (e.g. "arachne.l42.eu") to system IDs.
func parseLoganneWebhooks(data []byte, domain2sys map[string]string) ([]c4AsyncEdge, []string) {
	var raw map[string]json.RawMessage
	if err := json.Unmarshal(data, &raw); err != nil {
		return nil, []string{"- failed to parse loganne webhooks-config.json: " + err.Error()}
	}

	var edges []c4AsyncEdge
	var divergences []string
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
				msg := fmt.Sprintf("- loganne event `%s` -> `%s` has no matching configy system", event, dom)
				if !seenDivergences[msg] {
					seenDivergences[msg] = true
					divergences = append(divergences, msg)
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
	sort.Strings(divergences)
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
// the output artifacts (docs/c4/model.dsl, docs/c4/landscape.md,
// docs/c4/divergences.md) to the lucos_repos GitHub repository. Files are
// only committed if their content has changed.
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

	asyncEdges, asyncDivergences := parseLoganneWebhooks([]byte(loganneFile.Content), domain2sys)

	// Shared HTTP client for all live endpoint probes (/_info and /producers).
	infoClient := &http.Client{Timeout: c4InfoTimeout}

	// 2b. Probe loganne's /producers endpoint for observed async-producer edges.
	// This is non-critical: failure is logged and produces an empty producer set,
	// not a sweep error. The loganne domain is resolved from the domain2sys map.
	var producerEdges []c4ProducerEdge
	var producerDivergences []string
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
			producerEdges, producerDivergences = parseLoganneProducers(raw, sysSet)
		}
	} else {
		slog.Warn("C4: lucos_loganne has no domain in configy; skipping /producers probe")
	}

	// 3. Probe /_info for each system that has a public domain.
	var syncEdges []c4SyncEdge
	var syncDivergences []string
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
		// Flag a divergence if the reported system name disagrees with the configy key.
		if reportedName != "" && reportedName != sys.ID {
			syncDivergences = append(syncDivergences, fmt.Sprintf(
				"- `%s` (configy) reports `system: %s` in /_info on %s",
				sys.ID, reportedName, sys.Domain))
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
	sort.Strings(allDivergences)
	sort.Strings(unreachable)

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

	// 4. Commit changed artifacts to the lucos_repos repository.
	outputRepo := s.githubOrg + "/lucos_repos"
	commitMsg := "Auto-generate C4 estate model from sweep\n\n" +
		"Regenerated from configy systems, loganne webhooks, loganne /producers, and /_info probes."

	artifacts := []struct {
		path    string
		content string
	}{
		{"docs/c4/model.dsl", dsl},
		{"docs/c4/landscape.md", mermaid},
		{"docs/c4/divergences.md", divs},
	}

	var writeErr error
	for _, artifact := range artifacts {
		current, err := s.fetchC4GitHubFile(token, outputRepo, artifact.path)
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
		if err := s.putC4GitHubFile(token, outputRepo, artifact.path,
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
