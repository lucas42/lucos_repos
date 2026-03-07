package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
)

// auditFindingLabel is the label applied to all issues raised by the audit tool.
// It is used to identify audit-raised issues when searching for existing ones.
const auditFindingLabel = "audit-finding"

// conventionIssueTitle returns the standardised issue title for a convention violation.
// The format is: [Convention] <id>: <description>
func conventionIssueTitle(conventionID, description string) string {
	return fmt.Sprintf("[Convention] %s: %s", conventionID, description)
}

// gitHubIssue represents a GitHub issue as returned by the Issues API.
type gitHubIssue struct {
	Number  int    `json:"number"`
	HTMLURL string `json:"html_url"`
	Title   string `json:"title"`
	State   string `json:"state"`
}

// GitHubIssueClient handles creating and searching for audit-finding issues on GitHub repos.
type GitHubIssueClient struct {
	baseURL string
	token   string
}

// NewGitHubIssueClient creates a new GitHubIssueClient.
func NewGitHubIssueClient(baseURL, token string) *GitHubIssueClient {
	return &GitHubIssueClient{
		baseURL: baseURL,
		token:   token,
	}
}

// ConventionInfo holds the metadata for a convention that is used when creating
// an audit-finding issue. This keeps the EnsureIssueExists signature manageable
// as new fields are added.
type ConventionInfo struct {
	ID          string
	Description string
	Rationale   string
	Guidance    string
}

// EnsureIssueExists checks whether an open audit-finding issue exists for the
// given convention on the given repo. If one exists, it returns its URL.
// If none exists, it creates a new one (referencing the most recent closed
// issue if any) and returns the new issue's URL.
func (c *GitHubIssueClient) EnsureIssueExists(repo string, conv ConventionInfo) (string, error) {
	title := conventionIssueTitle(conv.ID, conv.Description)

	// Check for an existing open issue.
	existingURL, err := c.findOpenIssue(repo, title)
	if err != nil {
		return "", fmt.Errorf("failed to search for existing issue: %w", err)
	}
	if existingURL != "" {
		slog.Debug("Open audit-finding issue already exists", "repo", repo, "convention", conv.ID, "url", existingURL)
		return existingURL, nil
	}

	// Look for a previously closed issue to reference in the new one.
	previousURL, err := c.findMostRecentClosedIssue(repo, title)
	if err != nil {
		// Non-fatal — we can still create the issue without a back-reference.
		slog.Warn("Failed to search for closed issues", "repo", repo, "convention", conv.ID, "error", err)
	}

	// Create a new issue.
	newURL, err := c.createIssue(repo, title, conv, previousURL)
	if err != nil {
		return "", fmt.Errorf("failed to create audit-finding issue: %w", err)
	}
	slog.Info("Created audit-finding issue", "repo", repo, "convention", conv.ID, "url", newURL)
	return newURL, nil
}

// findOpenIssue fetches open issues with the audit-finding label on the given repo
// and returns the HTML URL of the first one whose title matches exactly, or "" if none.
func (c *GitHubIssueClient) findOpenIssue(repo, title string) (string, error) {
	// Issues List API: fetch all open audit-finding issues, then filter locally for exact title.
	// per_page=100 is the maximum; pagination is not needed in practice since there should be
	// at most one open audit-finding issue per convention per repo.
	listURL := fmt.Sprintf("%s/repos/%s/issues?labels=%s&state=open&per_page=100", c.baseURL, repo, auditFindingLabel)

	issues, err := c.fetchIssuesList(listURL)
	if err != nil {
		return "", fmt.Errorf("failed to list open issues: %w", err)
	}

	for _, issue := range issues {
		if issue.Title == title {
			return issue.HTMLURL, nil
		}
	}
	return "", nil
}

// findMostRecentClosedIssue fetches the most recently updated closed audit-finding
// issues on the given repo and returns the HTML URL of the first one whose title
// matches exactly, or "" if none.
func (c *GitHubIssueClient) findMostRecentClosedIssue(repo, title string) (string, error) {
	// Issues List API: fetch closed audit-finding issues sorted by most recently updated.
	// per_page=100 gives us a broad window to find the matching title without pagination.
	listURL := fmt.Sprintf("%s/repos/%s/issues?labels=%s&state=closed&sort=updated&direction=desc&per_page=100", c.baseURL, repo, auditFindingLabel)

	issues, err := c.fetchIssuesList(listURL)
	if err != nil {
		return "", fmt.Errorf("failed to list closed issues: %w", err)
	}

	for _, issue := range issues {
		if issue.Title == title {
			return issue.HTMLURL, nil
		}
	}
	return "", nil
}

// fetchIssuesList performs a GET request to the given Issues List API URL and
// returns the decoded slice of issues.
func (c *GitHubIssueClient) fetchIssuesList(listURL string) ([]gitHubIssue, error) {
	req, err := http.NewRequest("GET", listURL, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to build issues list request: %w", err)
	}
	req.Header.Set("Authorization", "Bearer "+c.token)
	req.Header.Set("Accept", "application/vnd.github+json")
	req.Header.Set("X-GitHub-Api-Version", "2022-11-28")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("GitHub issues list request failed: %w", err)
	}
	body, err := io.ReadAll(resp.Body)
	resp.Body.Close()
	if err != nil {
		return nil, fmt.Errorf("failed to read issues list response: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("GitHub issues list API returned %d: %s", resp.StatusCode, body)
	}

	var issues []gitHubIssue
	if err := json.Unmarshal(body, &issues); err != nil {
		return nil, fmt.Errorf("failed to decode issues list response: %w", err)
	}

	return issues, nil
}

// createIssueRequest is the JSON body for creating a GitHub issue.
type createIssueRequest struct {
	Title  string   `json:"title"`
	Body   string   `json:"body"`
	Labels []string `json:"labels"`
}

// createIssue creates a new GitHub issue for a failing convention.
// If previousURL is non-empty, the issue body references the previously closed issue.
func (c *GitHubIssueClient) createIssue(repo, title string, conv ConventionInfo, previousURL string) (string, error) {
	body := fmt.Sprintf("The `%s` convention is failing for this repository.\n\n**Convention:** %s\n**Description:** %s",
		conv.ID, conv.ID, conv.Description)

	if conv.Rationale != "" {
		body += fmt.Sprintf("\n\n**Why this matters:** %s", conv.Rationale)
	}

	if conv.Guidance != "" {
		body += fmt.Sprintf("\n\n**Suggested fix:** %s", conv.Guidance)
	}

	body += "\n\nThis issue was automatically raised by the lucos_repos audit tool."

	if previousURL != "" {
		body += fmt.Sprintf("\n\nA previous issue for this convention was raised and closed: %s", previousURL)
	}

	payload := createIssueRequest{
		Title:  title,
		Body:   body,
		Labels: []string{auditFindingLabel},
	}

	payloadJSON, err := json.Marshal(payload)
	if err != nil {
		return "", fmt.Errorf("failed to marshal issue payload: %w", err)
	}

	issueURL := fmt.Sprintf("%s/repos/%s/issues", c.baseURL, repo)
	req, err := http.NewRequest("POST", issueURL, bytes.NewReader(payloadJSON))
	if err != nil {
		return "", fmt.Errorf("failed to build create issue request: %w", err)
	}
	req.Header.Set("Authorization", "Bearer "+c.token)
	req.Header.Set("Accept", "application/vnd.github+json")
	req.Header.Set("X-GitHub-Api-Version", "2022-11-28")
	req.Header.Set("Content-Type", "application/json")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return "", fmt.Errorf("GitHub create issue request failed: %w", err)
	}
	respBody, err := io.ReadAll(resp.Body)
	resp.Body.Close()
	if err != nil {
		return "", fmt.Errorf("failed to read create issue response: %w", err)
	}

	if resp.StatusCode != http.StatusCreated {
		return "", fmt.Errorf("GitHub create issue API returned %d: %s", resp.StatusCode, respBody)
	}

	var created gitHubIssue
	if err := json.Unmarshal(respBody, &created); err != nil {
		return "", fmt.Errorf("failed to decode created issue response: %w", err)
	}

	return created.HTMLURL, nil
}
