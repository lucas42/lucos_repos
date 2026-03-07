package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/url"
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

// searchIssuesResponse represents the GitHub search API response.
type searchIssuesResponse struct {
	TotalCount int           `json:"total_count"`
	Items      []gitHubIssue `json:"items"`
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

// EnsureIssueExists checks whether an open audit-finding issue exists for the
// given convention on the given repo. If one exists, it returns its URL.
// If none exists, it creates a new one (referencing the most recent closed
// issue if any) and returns the new issue's URL.
func (c *GitHubIssueClient) EnsureIssueExists(repo, conventionID, conventionDescription string) (string, error) {
	title := conventionIssueTitle(conventionID, conventionDescription)

	// Check for an existing open issue.
	existingURL, err := c.findOpenIssue(repo, title)
	if err != nil {
		return "", fmt.Errorf("failed to search for existing issue: %w", err)
	}
	if existingURL != "" {
		slog.Debug("Open audit-finding issue already exists", "repo", repo, "convention", conventionID, "url", existingURL)
		return existingURL, nil
	}

	// Look for a previously closed issue to reference in the new one.
	previousURL, err := c.findMostRecentClosedIssue(repo, title)
	if err != nil {
		// Non-fatal — we can still create the issue without a back-reference.
		slog.Warn("Failed to search for closed issues", "repo", repo, "convention", conventionID, "error", err)
	}

	// Create a new issue.
	newURL, err := c.createIssue(repo, title, conventionID, conventionDescription, previousURL)
	if err != nil {
		return "", fmt.Errorf("failed to create audit-finding issue: %w", err)
	}
	slog.Info("Created audit-finding issue", "repo", repo, "convention", conventionID, "url", newURL)
	return newURL, nil
}

// findOpenIssue searches for an open issue with the audit-finding label and the
// given title on the given repo. Returns the HTML URL if found, or "" if not.
func (c *GitHubIssueClient) findOpenIssue(repo, title string) (string, error) {
	return c.searchIssues(repo, title, "open")
}

// findMostRecentClosedIssue searches for the most recently closed audit-finding
// issue with the given title on the given repo. Returns the HTML URL if found,
// or "" if not.
func (c *GitHubIssueClient) findMostRecentClosedIssue(repo, title string) (string, error) {
	return c.searchIssues(repo, title, "closed")
}

// searchIssues searches for issues with the audit-finding label and matching
// title on the given repo. state should be "open" or "closed".
// Returns the HTML URL of the first (most recent) matching issue, or "" if none.
func (c *GitHubIssueClient) searchIssues(repo, title, state string) (string, error) {
	// GitHub Search API: search within the specific repo, filtering by label, state, and title.
	// Do not pre-encode the title — let the single url.QueryEscape(query) below handle all encoding.
	query := fmt.Sprintf("repo:%s label:%s is:%s is:issue in:title %s", repo, auditFindingLabel, state, title)
	searchURL := fmt.Sprintf("%s/search/issues?q=%s&per_page=1&sort=updated&order=desc", c.baseURL, url.QueryEscape(query))

	req, err := http.NewRequest("GET", searchURL, nil)
	if err != nil {
		return "", fmt.Errorf("failed to build search request: %w", err)
	}
	req.Header.Set("Authorization", "Bearer "+c.token)
	req.Header.Set("Accept", "application/vnd.github+json")
	req.Header.Set("X-GitHub-Api-Version", "2022-11-28")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return "", fmt.Errorf("GitHub search request failed: %w", err)
	}
	body, err := io.ReadAll(resp.Body)
	resp.Body.Close()
	if err != nil {
		return "", fmt.Errorf("failed to read search response: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("GitHub search API returned %d: %s", resp.StatusCode, body)
	}

	var result searchIssuesResponse
	if err := json.Unmarshal(body, &result); err != nil {
		return "", fmt.Errorf("failed to decode search response: %w", err)
	}

	if result.TotalCount == 0 || len(result.Items) == 0 {
		return "", nil
	}

	// Return the most recent match — but only if the title matches exactly
	// (the search API does substring matching, so we must verify).
	for _, issue := range result.Items {
		if issue.Title == title {
			return issue.HTMLURL, nil
		}
	}
	return "", nil
}

// createIssueRequest is the JSON body for creating a GitHub issue.
type createIssueRequest struct {
	Title  string   `json:"title"`
	Body   string   `json:"body"`
	Labels []string `json:"labels"`
}

// createIssue creates a new GitHub issue for a failing convention.
// If previousURL is non-empty, the issue body references the previously closed issue.
func (c *GitHubIssueClient) createIssue(repo, title, conventionID, conventionDescription, previousURL string) (string, error) {
	body := fmt.Sprintf("The `%s` convention is failing for this repository.\n\n**Convention:** %s\n**Description:** %s\n\nThis issue was automatically raised by the lucos_repos audit tool.",
		conventionID, conventionID, conventionDescription)

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
