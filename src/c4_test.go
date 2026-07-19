package main

import (
	"encoding/base64"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

// TestDslIdent verifies that dslIdent replaces hyphens and dots with underscores.
func TestDslIdent(t *testing.T) {
	tests := []struct {
		input string
		want  string
	}{
		{"lucos_photos", "lucos_photos"},
		{"lucos-thing", "lucos_thing"},
		{"lukeblaney.co.uk", "lukeblaney_co_uk"},
		{"tfluke", "tfluke"},
		{"lucos_media_metadata_api", "lucos_media_metadata_api"},
	}
	for _, tc := range tests {
		got := dslIdent(tc.input)
		if got != tc.want {
			t.Errorf("dslIdent(%q) = %q, want %q", tc.input, got, tc.want)
		}
	}
}

// sampleC4Model returns a small c4Model for use in generation tests.
func sampleC4Model() c4Model {
	systems := []configySystem{
		{ID: "lucos_loganne", Domain: "loganne.l42.eu"},
		{ID: "lucos_monitoring", Domain: "monitoring.l42.eu"},
		{ID: "lucos_photos", Domain: "photos.l42.eu"},
		{ID: "lucos_router", Domain: ""},
	}
	sysSet := map[string]bool{
		"lucos_loganne":    true,
		"lucos_monitoring": true,
		"lucos_photos":     true,
		"lucos_router":     true,
	}
	syncEdges := []c4SyncEdge{
		{From: "lucos_photos", To: "lucos_loganne"},
	}
	asyncEdges := []c4AsyncEdge{
		{Event: "deploySystem", Consumer: "lucos_monitoring"},
		{Event: "photoUploaded", Consumer: "lucos_monitoring"},
	}
	producerEdges := []c4ProducerEdge{
		{Source: "lucos_photos", Event: "photoUploaded"},
	}
	return c4Model{
		systems:       systems,
		sysSet:        sysSet,
		syncEdges:     syncEdges,
		asyncEdges:    asyncEdges,
		producerEdges: producerEdges,
	}
}

// TestProbeLoganneProducers_Success verifies that a valid /producers response
// is parsed into a map correctly.
func TestProbeLoganneProducers_Success(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/producers" {
			w.WriteHeader(http.StatusNotFound)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{"lucos_photos":["photoUploaded","albumCreated"],"lucos_arachne":["ingestComplete"]}`))
	}))
	defer server.Close()

	domain := strings.TrimPrefix(server.URL, "http://")
	client := &http.Client{Transport: &plainHTTPTransport{}, Timeout: 2 * time.Second}
	result, err := probeLoganneProducers(domain, client)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(result) != 2 {
		t.Fatalf("expected 2 sources, got %d", len(result))
	}
	if len(result["lucos_photos"]) != 2 {
		t.Errorf("expected 2 event types for lucos_photos, got %v", result["lucos_photos"])
	}
	if len(result["lucos_arachne"]) != 1 || result["lucos_arachne"][0] != "ingestComplete" {
		t.Errorf("expected [ingestComplete] for lucos_arachne, got %v", result["lucos_arachne"])
	}
}

// TestProbeLoganneProducers_NonOKStatus verifies that a non-200 response returns an error.
func TestProbeLoganneProducers_NonOKStatus(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNotFound)
	}))
	defer server.Close()

	domain := strings.TrimPrefix(server.URL, "http://")
	client := &http.Client{Transport: &plainHTTPTransport{}, Timeout: 2 * time.Second}
	_, err := probeLoganneProducers(domain, client)
	if err == nil {
		t.Error("expected error for 404 response, got nil")
	}
}

// TestParseLoganneProducers_Basic verifies that valid sources produce sorted edges.
func TestParseLoganneProducers_Basic(t *testing.T) {
	raw := map[string][]string{
		"lucos_photos":  {"albumCreated", "photoUploaded"},
		"lucos_arachne": {"ingestComplete"},
	}
	sysSet := map[string]bool{"lucos_photos": true, "lucos_arachne": true, "lucos_loganne": true}
	edges, divs := parseLoganneProducers(raw, sysSet, "lucas42")
	if len(divs) != 0 {
		t.Errorf("expected no divergences, got: %v", divs)
	}
	if len(edges) != 3 {
		t.Fatalf("expected 3 edges, got %d: %v", len(edges), edges)
	}
	// Sorted by (Source, Event): lucos_arachne < lucos_photos
	if edges[0].Source != "lucos_arachne" || edges[0].Event != "ingestComplete" {
		t.Errorf("edge[0] = %+v, want {lucos_arachne ingestComplete}", edges[0])
	}
	if edges[1].Source != "lucos_photos" || edges[1].Event != "albumCreated" {
		t.Errorf("edge[1] = %+v, want {lucos_photos albumCreated}", edges[1])
	}
}

// TestParseLoganneProducers_UnknownSourceRaisesDivergence verifies that a
// producer whose source is not in sysSet is recorded as a divergence.
func TestParseLoganneProducers_UnknownSourceRaisesDivergence(t *testing.T) {
	raw := map[string][]string{
		"unknown_system": {"someEvent"},
	}
	sysSet := map[string]bool{"lucos_photos": true}
	edges, divs := parseLoganneProducers(raw, sysSet, "lucas42")
	if len(edges) != 0 {
		t.Errorf("expected no edges for unknown source, got %d", len(edges))
	}
	if len(divs) != 1 || !strings.Contains(divs[0].Message, "unknown_system") {
		t.Errorf("expected divergence mentioning unknown_system, got: %v", divs)
	}
	if divs[0].Repo != "lucas42/lucos_loganne" {
		t.Errorf("expected divergence attributed to lucas42/lucos_loganne, got %q", divs[0].Repo)
	}
	if divs[0].ID == "" {
		t.Errorf("expected a non-empty stable ID for the divergence")
	}
}

// TestParseLoganneProducers_ComponentAndScriptSourcesAreValid verifies that a
// producer whose source is a configy component or script (not a system) is
// treated as valid — the false-positive fixed in #467 — as long as it's in
// the validSources set passed in.
func TestParseLoganneProducers_ComponentAndScriptSourcesAreValid(t *testing.T) {
	raw := map[string][]string{
		"lucos_agent":       {"deployTriggered"}, // configy script
		"lucos_media_utils": {"tagRewritten"},    // configy component
	}
	validSources := map[string]bool{
		"lucos_agent":       true, // from configy /scripts
		"lucos_media_utils": true, // from configy /components
	}
	edges, divs := parseLoganneProducers(raw, validSources, "lucas42")
	if len(divs) != 0 {
		t.Errorf("expected no divergences for component/script producers, got: %v", divs)
	}
	if len(edges) != 2 {
		t.Fatalf("expected 2 edges, got %d: %v", len(edges), edges)
	}
}

// TestBuildProducerElements_SkipsSystemsAndDedupsNonSystems verifies that
// buildProducerElements only returns elements for non-system producer
// sources, dedups repeated sources across multiple edges, and looks up each
// one's kind from kindByID (#467, Render decision).
func TestBuildProducerElements_SkipsSystemsAndDedupsNonSystems(t *testing.T) {
	edges := []c4ProducerEdge{
		{Source: "lucos_photos", Event: "photoUploaded"}, // system — should be skipped
		{Source: "lucos_agent", Event: "deployTriggered"},
		{Source: "lucos_agent", Event: "plannedMaintenance"}, // same source again — should dedup
		{Source: "lucos_media_utils", Event: "tagRewritten"},
	}
	sysSet := map[string]bool{"lucos_photos": true}
	kindByID := map[string]string{
		"lucos_agent":       "script",
		"lucos_media_utils": "component",
	}

	elements := buildProducerElements(edges, sysSet, kindByID)

	if len(elements) != 2 {
		t.Fatalf("expected 2 elements, got %d: %v", len(elements), elements)
	}
	// Sorted by ID: lucos_agent before lucos_media_utils.
	if elements[0].ID != "lucos_agent" || elements[0].Kind != "script" {
		t.Errorf("expected first element lucos_agent/script, got %+v", elements[0])
	}
	if elements[1].ID != "lucos_media_utils" || elements[1].Kind != "component" {
		t.Errorf("expected second element lucos_media_utils/component, got %+v", elements[1])
	}
}

// TestBuildProducerElements_MissingKindFallsBackToProducer verifies the
// defensive fallback for a source with no entry in kindByID.
func TestBuildProducerElements_MissingKindFallsBackToProducer(t *testing.T) {
	edges := []c4ProducerEdge{{Source: "mystery_source", Event: "someEvent"}}
	elements := buildProducerElements(edges, map[string]bool{}, map[string]string{})
	if len(elements) != 1 || elements[0].Kind != "producer" {
		t.Errorf("expected fallback kind %q, got %v", "producer", elements)
	}
}

// TestGenerateC4DSL_ContainsExpectedSections verifies that the generated DSL
// contains the workspace declaration, model block, all systems, and edges.
func TestGenerateC4DSL_ContainsExpectedSections(t *testing.T) {
	m := sampleC4Model()
	dsl := generateC4DSL(m)

	checks := []struct {
		desc string
		want string
	}{
		{"workspace declaration", `workspace "lucOS estate" "Generated C4 model — DO NOT EDIT BY HAND"`},
		{"model block", "model {"},
		{"lucas person", `lucas = person "lucas42"`},
		{"loganne system with domain", `lucos_loganne = softwareSystem "lucos_loganne" "loganne.l42.eu"`},
		{"router system with no domain", `lucos_router = softwareSystem "lucos_router" "(no public domain)"`},
		{"sync edge", `lucos_photos -> lucos_loganne "depends on (sync)"`},
		{"producer edge", `lucos_photos -> lucos_loganne "emits photoUploaded"`},
		{"async edge", `lucos_loganne -> lucos_monitoring "deploySystem"`},
		{"async edge 2", `lucos_loganne -> lucos_monitoring "photoUploaded"`},
		{"views block", "views {"},
		{"landscape view", `systemLandscape "estate" { include * autolayout lr }`},
		{"theme default", "theme default"},
	}
	for _, tc := range checks {
		if !strings.Contains(dsl, tc.want) {
			t.Errorf("DSL missing %s: %q not found in output", tc.desc, tc.want)
		}
	}
}

// TestGenerateC4DSL_SyncEdgeToUnknownSystemSkipped verifies that sync edges
// whose To system is not in the sysSet are omitted from the DSL.
func TestGenerateC4DSL_SyncEdgeToUnknownSystemSkipped(t *testing.T) {
	m := sampleC4Model()
	m.syncEdges = append(m.syncEdges, c4SyncEdge{From: "lucos_photos", To: "unknown_system"})
	dsl := generateC4DSL(m)
	if strings.Contains(dsl, "unknown_system") {
		t.Error("DSL should not include sync edges to systems not in sysSet")
	}
}

// TestGenerateC4DSL_LoganneNotDeclaredAsPersonOrExtra verifies loganne appears
// only as a softwareSystem node (not repeated as a "from" for sync edges,
// since ADR-0006 §3 says loganne's dependsOn should be modelled as async).
func TestGenerateC4DSL_LoganneAppearsAsSoftwareSystem(t *testing.T) {
	m := sampleC4Model()
	dsl := generateC4DSL(m)
	if !strings.Contains(dsl, `lucos_loganne = softwareSystem`) {
		t.Error("expected lucos_loganne to appear as a softwareSystem")
	}
}

// TestGenerateC4Mermaid_ConnectedCoreOnly verifies that only systems with at
// least one edge appear in the Mermaid output (not e.g. lucos_router which has
// no edges in the sample model).
// TestGenerateC4DSL_ProducerElementForComponentScript verifies that a
// non-system producer source gets its own declared "element" and a producer
// edge is drawn from it — #467, Render decision (previously these producers
// validated cleanly but were silently omitted from the diagram).
func TestGenerateC4DSL_ProducerElementForComponentScript(t *testing.T) {
	m := sampleC4Model()
	m.producerEdges = append(m.producerEdges, c4ProducerEdge{Source: "lucos_agent", Event: "plannedMaintenance"})
	m.producerElements = []c4ProducerElement{{ID: "lucos_agent", Kind: "script"}}
	dsl := generateC4DSL(m)

	if !strings.Contains(dsl, `lucos_agent = element "lucos_agent" "configy script (event producer)"`) {
		t.Errorf("expected declared element for lucos_agent script producer, got:\n%s", dsl)
	}
	if !strings.Contains(dsl, `lucos_agent -> lucos_loganne "emits plannedMaintenance"`) {
		t.Errorf("expected producer edge for lucos_agent, got:\n%s", dsl)
	}
}

// TestGenerateC4Mermaid_ProducerEdgeForComponentScript is the Mermaid
// equivalent of TestGenerateC4DSL_ProducerElementForComponentScript.
func TestGenerateC4Mermaid_ProducerEdgeForComponentScript(t *testing.T) {
	m := sampleC4Model()
	m.producerEdges = append(m.producerEdges, c4ProducerEdge{Source: "lucos_agent", Event: "plannedMaintenance"})
	m.producerElements = []c4ProducerElement{{ID: "lucos_agent", Kind: "script"}}
	mermaid := generateC4Mermaid(m)

	if !strings.Contains(mermaid, `lucos_agent["lucos_agent"]`) {
		t.Errorf("expected lucos_agent node in Mermaid connected core, got:\n%s", mermaid)
	}
	if !strings.Contains(mermaid, "lucos_agent -.plannedMaintenance.-> lucos_loganne") {
		t.Errorf("expected dotted producer edge for lucos_agent, got:\n%s", mermaid)
	}
}

func TestGenerateC4Mermaid_ConnectedCoreOnly(t *testing.T) {
	m := sampleC4Model()
	mermaid := generateC4Mermaid(m)

	if !strings.Contains(mermaid, "flowchart LR") {
		t.Error("expected Mermaid flowchart LR declaration")
	}
	if !strings.Contains(mermaid, "lucos_photos") {
		t.Error("expected lucos_photos in Mermaid (it has a sync edge)")
	}
	if !strings.Contains(mermaid, "lucos_monitoring") {
		t.Error("expected lucos_monitoring in Mermaid (it receives async events)")
	}
	if !strings.Contains(mermaid, "lucos_loganne") {
		t.Error("expected lucos_loganne in Mermaid (it is the async event source)")
	}
	// lucos_router has no edges and should not appear.
	if strings.Contains(mermaid, "lucos_router") {
		t.Error("lucos_router has no edges and should not appear in the connected-core Mermaid")
	}
}

// TestGenerateC4Mermaid_SyncAndAsyncEdgesPresent verifies that both solid
// (sync) and dotted (async) edge types appear in the Mermaid output.
func TestGenerateC4Mermaid_SyncAndAsyncEdgesPresent(t *testing.T) {
	m := sampleC4Model()
	mermaid := generateC4Mermaid(m)

	if !strings.Contains(mermaid, "lucos_photos --> lucos_loganne") {
		t.Error("expected solid sync edge lucos_photos --> lucos_loganne")
	}
	if !strings.Contains(mermaid, "lucos_photos -.photoUploaded.-> lucos_loganne") {
		t.Error("expected dotted producer edge lucos_photos -.photoUploaded.-> lucos_loganne")
	}
	if !strings.Contains(mermaid, "lucos_loganne -.deploySystem.-> lucos_monitoring") {
		t.Error("expected dotted async edge lucos_loganne -.deploySystem.-> lucos_monitoring")
	}
}

// TestGenerateC4Divergences_NoDivergences verifies the "None." output when
// there are no divergences and no unreachable systems.
func TestGenerateC4Divergences_NoDivergences(t *testing.T) {
	m := c4Model{}
	divs := generateC4Divergences(m)
	if !strings.Contains(divs, "None.") {
		t.Errorf("expected 'None.' in divergences output when no divergences, got: %q", divs)
	}
	if !strings.Contains(divs, "Unreachable /_info: none") {
		t.Errorf("expected 'Unreachable /_info: none' when no unreachable systems, got: %q", divs)
	}
}

// TestGenerateC4Divergences_WithDivergencesAndUnreachable verifies formatting
// when both divergences and unreachable systems are present.
func TestGenerateC4Divergences_WithDivergencesAndUnreachable(t *testing.T) {
	m := c4Model{
		divergences: []c4Divergence{
			{Repo: "lucas42/tfluke", ID: "c4-info-system-name-divergence",
				Message: "- `tfluke` (configy) reports `system: tfluke_app` in /_info on app.tfluke.uk"},
			{Repo: "lucas42/lucos_loganne", ID: "c4-loganne-webhook-target-unknown_event-gone_l42_eu",
				Message: "- loganne event `unknown_event` -> `gone.l42.eu` has no matching configy system"},
		},
		unreachable: []string{"lucos_dns", "lucos_dns_secondary"},
	}
	divs := generateC4Divergences(m)

	if !strings.Contains(divs, "# Source divergences (audit findings)") {
		t.Error("expected header in divergences output")
	}
	if !strings.Contains(divs, "`tfluke`") {
		t.Error("expected tfluke divergence in output")
	}
	if !strings.Contains(divs, "Unreachable /_info: lucos_dns, lucos_dns_secondary") {
		t.Errorf("expected unreachable list in output, got: %q", divs)
	}
}

// TestParseLoganneWebhooks_BasicParsing verifies that event→consumer edges are
// extracted correctly and consumerTokens is skipped.
func TestParseLoganneWebhooks_BasicParsing(t *testing.T) {
	webhooksJSON := []byte(`{
		"consumerTokens": {
			"arachne.l42.eu": "KEY_ARACHNE"
		},
		"trackAdded": [
			"https://arachne.l42.eu/webhook",
			"https://media-weighting.l42.eu/weight-track"
		],
		"deploySystem": [
			"https://monitoring.l42.eu/suppress/clear"
		]
	}`)
	domain2sys := map[string]string{
		"arachne.l42.eu":         "lucos_arachne",
		"media-weighting.l42.eu": "lucos_media_weightings",
		"monitoring.l42.eu":      "lucos_monitoring",
	}

	edges, divs := parseLoganneWebhooks(webhooksJSON, domain2sys, "lucas42")
	if len(divs) != 0 {
		t.Errorf("expected no divergences, got: %v", divs)
	}
	if len(edges) != 3 {
		t.Errorf("expected 3 edges (trackAdded×2 + deploySystem×1), got %d", len(edges))
	}

	// Edges should be sorted by (Consumer, Event): arachne < media_weightings < monitoring.
	wantEdges := []c4AsyncEdge{
		{Event: "trackAdded", Consumer: "lucos_arachne"},
		{Event: "trackAdded", Consumer: "lucos_media_weightings"},
		{Event: "deploySystem", Consumer: "lucos_monitoring"},
	}
	for i, want := range wantEdges {
		if i >= len(edges) {
			t.Errorf("missing edge at index %d: want %+v", i, want)
			continue
		}
		if edges[i] != want {
			t.Errorf("edge[%d] = %+v, want %+v", i, edges[i], want)
		}
	}
}

// TestParseLoganneWebhooks_UnknownDomainRaisesDivergence verifies that a
// webhook URL whose domain has no matching configy system is recorded as a
// divergence.
func TestParseLoganneWebhooks_UnknownDomainRaisesDivergence(t *testing.T) {
	webhooksJSON := []byte(`{
		"trackAdded": ["https://unknown.l42.eu/webhook"]
	}`)
	edges, divs := parseLoganneWebhooks(webhooksJSON, map[string]string{}, "lucas42")
	if len(edges) != 0 {
		t.Errorf("expected no edges for unknown domain, got %d", len(edges))
	}
	if len(divs) != 1 {
		t.Errorf("expected 1 divergence for unknown domain, got %d: %v", len(divs), divs)
	}
	if !strings.Contains(divs[0].Message, "unknown.l42.eu") {
		t.Errorf("divergence should mention the unknown domain, got: %q", divs[0].Message)
	}
	if divs[0].Repo != "lucas42/lucos_loganne" {
		t.Errorf("expected divergence attributed to lucas42/lucos_loganne, got %q", divs[0].Repo)
	}
}

// TestParseLoganneWebhooks_InvalidJSON verifies that a parse error is reported
// as a divergence and no edges are returned.
func TestParseLoganneWebhooks_InvalidJSON(t *testing.T) {
	edges, divs := parseLoganneWebhooks([]byte("{not valid json"), map[string]string{}, "lucas42")
	if len(edges) != 0 {
		t.Errorf("expected no edges for invalid JSON, got %d", len(edges))
	}
	if len(divs) != 1 || !strings.Contains(divs[0].Message, "failed to parse") {
		t.Errorf("expected parse-error divergence, got: %v", divs)
	}
}

// TestParseLoganneWebhooks_DeduplicatesDivergences verifies that the same
// missing-domain message is not reported more than once even when the domain
// appears under multiple events.
func TestParseLoganneWebhooks_DeduplicatesDivergences(t *testing.T) {
	webhooksJSON := []byte(`{
		"event1": ["https://gone.l42.eu/a"],
		"event2": ["https://gone.l42.eu/b"]
	}`)
	_, divs := parseLoganneWebhooks(webhooksJSON, map[string]string{}, "lucas42")
	if len(divs) != 2 {
		// Each (event, domain) pair is distinct so we should have 2 divergences.
		t.Errorf("expected 2 divergences (one per event), got %d: %v", len(divs), divs)
	}
}

// TestFetchC4GitHubFile_Success verifies that a file is fetched and decoded
// correctly from the GitHub Contents API.
func TestFetchC4GitHubFile_Success(t *testing.T) {
	const expectedContent = "hello c4 world"
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/repos/lucas42/lucos_repos/contents/docs/c4/model.dsl" {
			w.WriteHeader(http.StatusNotFound)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]string{
			"sha":      "abc123",
			"content":  base64.StdEncoding.EncodeToString([]byte(expectedContent)),
			"encoding": "base64",
		})
	}))
	defer server.Close()

	s := &AuditSweeper{githubAPIBaseURL: server.URL}
	file, err := s.fetchC4GitHubFile("fake-token", "lucas42/lucos_repos", "docs/c4/model.dsl")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if file == nil {
		t.Fatal("expected non-nil file, got nil")
	}
	if file.SHA != "abc123" {
		t.Errorf("expected SHA %q, got %q", "abc123", file.SHA)
	}
	if file.Content != expectedContent {
		t.Errorf("expected content %q, got %q", expectedContent, file.Content)
	}
}

// TestFetchC4GitHubFile_NotFound verifies that a 404 response returns (nil, nil).
func TestFetchC4GitHubFile_NotFound(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNotFound)
	}))
	defer server.Close()

	s := &AuditSweeper{githubAPIBaseURL: server.URL}
	file, err := s.fetchC4GitHubFile("fake-token", "lucas42/lucos_repos", "docs/c4/model.dsl")
	if err != nil {
		t.Fatalf("expected nil error for 404, got: %v", err)
	}
	if file != nil {
		t.Errorf("expected nil file for 404, got: %+v", file)
	}
}

// TestPutC4GitHubFile_Create verifies that PUT is called with the correct
// headers and payload when creating a new file (no current SHA).
func TestPutC4GitHubFile_Create(t *testing.T) {
	var received map[string]interface{}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPut {
			w.WriteHeader(http.StatusMethodNotAllowed)
			return
		}
		if err := json.NewDecoder(r.Body).Decode(&received); err != nil {
			w.WriteHeader(http.StatusBadRequest)
			return
		}
		w.WriteHeader(http.StatusCreated)
		w.Write([]byte(`{}`))
	}))
	defer server.Close()

	s := &AuditSweeper{githubAPIBaseURL: server.URL}
	err := s.putC4GitHubFile("fake-token", "lucas42/lucos_repos",
		"docs/c4/model.dsl", "workspace content", "", "Auto-generate C4")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if received["message"] != "Auto-generate C4" {
		t.Errorf("expected message %q in payload, got %q", "Auto-generate C4", received["message"])
	}
	decoded, _ := base64.StdEncoding.DecodeString(received["content"].(string))
	if string(decoded) != "workspace content" {
		t.Errorf("expected content %q after decode, got %q", "workspace content", string(decoded))
	}
	if _, ok := received["sha"]; ok {
		t.Error("expected no 'sha' field in create payload, but it was present")
	}
}

// TestPutC4GitHubFile_Update verifies that SHA is included in the payload when
// updating an existing file.
func TestPutC4GitHubFile_Update(t *testing.T) {
	var received map[string]interface{}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		json.NewDecoder(r.Body).Decode(&received)
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(`{}`))
	}))
	defer server.Close()

	s := &AuditSweeper{githubAPIBaseURL: server.URL}
	err := s.putC4GitHubFile("fake-token", "lucas42/lucos_repos",
		"docs/c4/model.dsl", "new content", "existingsha123", "Auto-generate C4")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if received["sha"] != "existingsha123" {
		t.Errorf("expected sha %q in update payload, got %v", "existingsha123", received["sha"])
	}
}

// TestProbeInfoEndpoint_StringDependsOn verifies that a scalar string dependsOn
// value is returned as a single dep.
func TestProbeInfoEndpoint_StringDependsOn(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/_info" {
			w.WriteHeader(http.StatusNotFound)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{
			"system": "lucos_photos",
			"checks": {
				"loganne": {"dependsOn": "lucos_loganne"}
			}
		}`))
	}))
	defer server.Close()

	// Strip the scheme so we can pass just the host:port as "domain".
	domain := strings.TrimPrefix(server.URL, "http://")
	// probeInfoEndpoint builds "https://" + domain + "/_info", so swap the
	// server to serve over plain HTTP by using a custom transport that strips TLS.
	client := &http.Client{
		Transport: &plainHTTPTransport{},
		Timeout:   2 * time.Second,
	}
	name, deps, err := probeInfoEndpoint(domain, client)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if name != "lucos_photos" {
		t.Errorf("expected system name %q, got %q", "lucos_photos", name)
	}
	if len(deps) != 1 || deps[0] != "lucos_loganne" {
		t.Errorf("expected deps [lucos_loganne], got %v", deps)
	}
}

// TestProbeInfoEndpoint_ArrayDependsOn verifies that an array dependsOn value
// is expanded into multiple deps.
func TestProbeInfoEndpoint_ArrayDependsOn(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{
			"system": "lucos_arachne",
			"checks": {
				"loganne": {"dependsOn": ["lucos_loganne", "lucos_eolas"]}
			}
		}`))
	}))
	defer server.Close()

	domain := strings.TrimPrefix(server.URL, "http://")
	client := &http.Client{Transport: &plainHTTPTransport{}, Timeout: 2 * time.Second}
	name, deps, err := probeInfoEndpoint(domain, client)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if name != "lucos_arachne" {
		t.Errorf("expected system name %q, got %q", "lucos_arachne", name)
	}
	if len(deps) != 2 {
		t.Fatalf("expected 2 deps, got %d: %v", len(deps), deps)
	}
	// Order is map iteration order, but both must be present.
	found := map[string]bool{}
	for _, d := range deps {
		found[d] = true
	}
	if !found["lucos_loganne"] || !found["lucos_eolas"] {
		t.Errorf("expected lucos_loganne and lucos_eolas in deps, got %v", deps)
	}
}

// TestProbeInfoEndpoint_NoDependsOn verifies that a check with no dependsOn
// field returns an empty deps slice without error.
func TestProbeInfoEndpoint_NoDependsOn(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{
			"system": "lucos_monitoring",
			"checks": {
				"db": {}
			}
		}`))
	}))
	defer server.Close()

	domain := strings.TrimPrefix(server.URL, "http://")
	client := &http.Client{Transport: &plainHTTPTransport{}, Timeout: 2 * time.Second}
	name, deps, err := probeInfoEndpoint(domain, client)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if name != "lucos_monitoring" {
		t.Errorf("expected system name %q, got %q", "lucos_monitoring", name)
	}
	if len(deps) != 0 {
		t.Errorf("expected no deps for check without dependsOn, got %v", deps)
	}
}

// TestProbeInfoEndpoint_NonOKStatus verifies that a non-200 HTTP response from
// /_info is returned as an error.
func TestProbeInfoEndpoint_NonOKStatus(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusServiceUnavailable)
	}))
	defer server.Close()

	domain := strings.TrimPrefix(server.URL, "http://")
	client := &http.Client{Transport: &plainHTTPTransport{}, Timeout: 2 * time.Second}
	_, _, err := probeInfoEndpoint(domain, client)
	if err == nil {
		t.Error("expected an error for non-200 status, got nil")
	}
	if !strings.Contains(err.Error(), "503") {
		t.Errorf("expected error to mention 503, got: %v", err)
	}
}

// plainHTTPTransport rewrites https:// scheme to http:// so that tests can use
// httptest.NewServer (plain HTTP) with probeInfoEndpoint (which always builds
// an https:// URL). Only used in tests.
type plainHTTPTransport struct{}

func (t *plainHTTPTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	plain := *req
	u := *req.URL
	u.Scheme = "http"
	plain.URL = &u
	return http.DefaultTransport.RoundTrip(&plain)
}

// TestGenerateAndCommitC4_EarlyExitOnLoganneNotFound verifies that
// generateAndCommitC4 exits gracefully when the loganne webhooks-config.json
// is not found (404 from the GitHub API), without probing any /_info endpoints
// or attempting commits.
func TestGenerateAndCommitC4_EarlyExitOnLoganneNotFound(t *testing.T) {
	configyServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/systems":
			json.NewEncoder(w).Encode([]configySystem{
				{ID: "lucos_photos", Domain: "photos.l42.eu"},
			})
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer configyServer.Close()

	putCalled := false
	githubServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodPut {
			putCalled = true
		}
		// loganne webhooks → 404; everything else → 404
		w.WriteHeader(http.StatusNotFound)
	}))
	defer githubServer.Close()

	s := &AuditSweeper{
		githubAuth:       &GitHubAuthClient{cachedToken: "fake-token", tokenExpires: time.Now().Add(1 * time.Hour)},
		githubOrg:        "lucas42",
		configyBaseURL:   configyServer.URL,
		githubAPIBaseURL: githubServer.URL,
	}
	err := s.generateAndCommitC4() // must not panic
	if err == nil {
		t.Error("expected an error when loganne webhooks-config.json is not found")
	}

	if putCalled {
		t.Error("expected no PUT requests when loganne webhooks not found, but one was made")
	}
}

// TestGenerateAndCommitC4_SkipsUnchangedFiles verifies that files are not
// re-committed when their content has not changed.
func TestGenerateAndCommitC4_SkipsUnchangedFiles(t *testing.T) {
	// We'll wire up a minimal model with one system, no domains (so no /_info probing),
	// and a loganne webhooks config that produces predictable output.
	loganneConfig := `{"consumerTokens": {}}`

	configyServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if r.URL.Path == "/systems" {
			json.NewEncoder(w).Encode([]configySystem{
				{ID: "lucos_test"},
			})
		} else {
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer configyServer.Close()

	// Pre-generate the expected DSL so we can set up the "file already matches" scenario.
	protoModel := c4Model{
		systems: []configySystem{{ID: "lucos_test"}},
		sysSet:  map[string]bool{"lucos_test": true},
	}
	expectedDSL := generateC4DSL(protoModel)
	expectedMermaid := generateC4Mermaid(protoModel)
	expectedDivs := generateC4Divergences(protoModel)

	putCalled := false
	githubServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if r.Method == http.MethodPut {
			putCalled = true
			w.WriteHeader(http.StatusOK)
			w.Write([]byte(`{}`))
			return
		}
		// GET requests for loganne webhooks and the C4 artifact files.
		switch r.URL.Path {
		case "/repos/lucas42/lucos_loganne/contents/src/webhooks-config.json":
			json.NewEncoder(w).Encode(map[string]string{
				"sha":      "loganne-sha",
				"content":  base64.StdEncoding.EncodeToString([]byte(loganneConfig)),
				"encoding": "base64",
			})
		case "/repos/lucas42/lucos_architecture_models/contents/model.dsl":
			json.NewEncoder(w).Encode(map[string]string{
				"sha":      "dsl-sha",
				"content":  base64.StdEncoding.EncodeToString([]byte(expectedDSL)),
				"encoding": "base64",
			})
		case "/repos/lucas42/lucos_architecture_models/contents/landscape.md":
			json.NewEncoder(w).Encode(map[string]string{
				"sha":      "mermaid-sha",
				"content":  base64.StdEncoding.EncodeToString([]byte(expectedMermaid)),
				"encoding": "base64",
			})
		case "/repos/lucas42/lucos_architecture_models/contents/divergences.md":
			json.NewEncoder(w).Encode(map[string]string{
				"sha":      "divs-sha",
				"content":  base64.StdEncoding.EncodeToString([]byte(expectedDivs)),
				"encoding": "base64",
			})
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer githubServer.Close()

	s := &AuditSweeper{
		githubAuth:       &GitHubAuthClient{cachedToken: "fake-token", tokenExpires: time.Now().Add(1 * time.Hour)},
		c4WriteAuth:      &GitHubAuthClient{cachedToken: "fake-write-token", tokenExpires: time.Now().Add(1 * time.Hour)},
		c4OutputRepo:     "lucas42/lucos_architecture_models",
		githubOrg:        "lucas42",
		configyBaseURL:   configyServer.URL,
		githubAPIBaseURL: githubServer.URL,
	}
	if err := s.generateAndCommitC4(); err != nil {
		t.Errorf("expected nil error when all artifacts are unchanged, got %v", err)
	}

	if putCalled {
		t.Error("expected no PUT requests when all file contents are unchanged")
	}
}

// TestGenerateAndCommitC4_NoWriteAuthConfigured verifies that a nil
// c4WriteAuth (the dev-environment state, since the lucos-architecture-writer
// App is only installed in production — #446) is treated as a real failure
// of the write-back stage, not a silent skip.
func TestGenerateAndCommitC4_NoWriteAuthConfigured(t *testing.T) {
	loganneConfig := `{"consumerTokens": {}}`

	configyServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if r.URL.Path == "/systems" {
			json.NewEncoder(w).Encode([]configySystem{
				{ID: "lucos_test"},
			})
		} else {
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer configyServer.Close()

	githubServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/repos/lucas42/lucos_loganne/contents/src/webhooks-config.json":
			json.NewEncoder(w).Encode(map[string]string{
				"sha":      "loganne-sha",
				"content":  base64.StdEncoding.EncodeToString([]byte(loganneConfig)),
				"encoding": "base64",
			})
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer githubServer.Close()

	s := &AuditSweeper{
		githubAuth:       &GitHubAuthClient{cachedToken: "fake-token", tokenExpires: time.Now().Add(1 * time.Hour)},
		c4WriteAuth:      nil, // not configured, as in dev
		c4OutputRepo:     "lucas42/lucos_architecture_models",
		githubOrg:        "lucas42",
		configyBaseURL:   configyServer.URL,
		githubAPIBaseURL: githubServer.URL,
	}
	err := s.generateAndCommitC4()
	if err == nil {
		t.Error("expected an error when c4WriteAuth is not configured")
	}
}

// TestGenerateAndCommitC4_ReturnsErrorOnPutFailure verifies that a failed
// GitHub Contents PUT (the write-back step) is surfaced as a returned error,
// so the caller can report it to schedule-tracker as a fail (#445) rather
// than only logging it.
func TestGenerateAndCommitC4_ReturnsErrorOnPutFailure(t *testing.T) {
	loganneConfig := `{"consumerTokens": {}}`

	configyServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if r.URL.Path == "/systems" {
			json.NewEncoder(w).Encode([]configySystem{
				{ID: "lucos_test"},
			})
		} else {
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer configyServer.Close()

	githubServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodPut {
			// Simulate the write-back failing, e.g. a permissions or conflict error.
			w.WriteHeader(http.StatusForbidden)
			w.Write([]byte(`{"message": "Resource not accessible by integration"}`))
			return
		}
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/repos/lucas42/lucos_loganne/contents/src/webhooks-config.json":
			json.NewEncoder(w).Encode(map[string]string{
				"sha":      "loganne-sha",
				"content":  base64.StdEncoding.EncodeToString([]byte(loganneConfig)),
				"encoding": "base64",
			})
		default:
			// No existing artifact content — every artifact is new and gets PUT.
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer githubServer.Close()

	s := &AuditSweeper{
		githubAuth:       &GitHubAuthClient{cachedToken: "fake-token", tokenExpires: time.Now().Add(1 * time.Hour)},
		c4WriteAuth:      &GitHubAuthClient{cachedToken: "fake-write-token", tokenExpires: time.Now().Add(1 * time.Hour)},
		c4OutputRepo:     "lucas42/lucos_architecture_models",
		githubOrg:        "lucas42",
		configyBaseURL:   configyServer.URL,
		githubAPIBaseURL: githubServer.URL,
	}

	err := s.generateAndCommitC4()
	if err == nil {
		t.Fatal("expected an error when the artifact write-back PUT fails")
	}
}

// TestRouteC4DivergencesToIssues_CreatesAndClosesIssues verifies that a
// current divergence gets an audit-finding issue created, while a repo with
// no current divergence but a stale open "c4-" issue gets it closed (#425).
func TestRouteC4DivergencesToIssues_CreatesAndClosesIssues(t *testing.T) {
	var created []string
	var closedIssueNumbers []int
	var commentedIssueNumbers []int

	githubServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/repos/lucas42/lucos_photos/issues":
			// No existing open or closed issues for lucos_photos.
			json.NewEncoder(w).Encode([]gitHubIssue{})
		case r.Method == http.MethodPost && r.URL.Path == "/repos/lucas42/lucos_photos/issues":
			body, _ := io.ReadAll(r.Body)
			var req createIssueRequest
			json.Unmarshal(body, &req)
			created = append(created, req.Title)
			w.WriteHeader(http.StatusCreated)
			json.NewEncoder(w).Encode(gitHubIssue{Number: 1, HTMLURL: "https://github.com/lucas42/lucos_photos/issues/1"})
		case r.Method == http.MethodGet && r.URL.Path == "/repos/lucas42/lucos_arachne/issues":
			// lucos_arachne has a stale open C4-divergence issue that should close.
			json.NewEncoder(w).Encode([]gitHubIssue{
				{Number: 55, HTMLURL: "https://github.com/lucas42/lucos_arachne/issues/55",
					Title: "[Convention] c4-info-system-name-divergence: C4 model divergence", State: "open"},
			})
		case r.Method == http.MethodPost && r.URL.Path == "/repos/lucas42/lucos_arachne/issues/55/comments":
			commentedIssueNumbers = append(commentedIssueNumbers, 55)
			w.WriteHeader(http.StatusCreated)
		case r.Method == http.MethodPatch && r.URL.Path == "/repos/lucas42/lucos_arachne/issues/55":
			closedIssueNumbers = append(closedIssueNumbers, 55)
			w.WriteHeader(http.StatusOK)
		case r.Method == http.MethodGet && r.URL.Path == "/repos/lucas42/lucos_loganne/issues":
			json.NewEncoder(w).Encode([]gitHubIssue{})
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer githubServer.Close()

	s := &AuditSweeper{
		githubOrg:        "lucas42",
		githubAPIBaseURL: githubServer.URL,
	}
	s.issueClientFactory = func(token string) *GitHubIssueClient {
		return NewGitHubIssueClient(githubServer.URL, token)
	}

	systems := []configySystem{{ID: "lucos_photos"}, {ID: "lucos_arachne"}}
	divergences := []c4Divergence{
		{Repo: "lucas42/lucos_photos", ID: "c4-info-system-name-divergence", Message: "- photos divergence"},
	}

	s.routeC4DivergencesToIssues("fake-token", systems, divergences)

	if len(created) != 1 || !strings.Contains(created[0], "c4-info-system-name-divergence") {
		t.Errorf("expected one issue created for lucos_photos's divergence, got: %v", created)
	}
	if len(closedIssueNumbers) != 1 || closedIssueNumbers[0] != 55 {
		t.Errorf("expected lucos_arachne's stale issue #55 to be closed, got: %v", closedIssueNumbers)
	}
	if len(commentedIssueNumbers) != 1 {
		t.Errorf("expected a closing comment on issue #55, got: %v", commentedIssueNumbers)
	}
}

// TestRouteC4DivergencesToIssues_NilFactoryDoesNotPanic verifies that a nil
// issueClientFactory (e.g. an AuditSweeper built without NewAuditSweeper, as
// several other tests in this file do) is handled gracefully rather than
// panicking.
func TestRouteC4DivergencesToIssues_NilFactoryDoesNotPanic(t *testing.T) {
	s := &AuditSweeper{githubOrg: "lucas42"}
	s.routeC4DivergencesToIssues("fake-token",
		[]configySystem{{ID: "lucos_photos"}},
		[]c4Divergence{{Repo: "lucas42/lucos_photos", ID: "c4-info-system-name-divergence", Message: "- x"}})
	// Should not panic.
}
