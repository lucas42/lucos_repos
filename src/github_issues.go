package main

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"strings"
)

// auditFindingLabel is the label applied to all issues raised by the audit tool.
// It is used to identify audit-raised issues when searching for existing ones.
const auditFindingLabel = "audit-finding"

// ErrIssuesUnavailable is returned when the GitHub Issues API responds with
// 403 (repository archived / read-only) or 410 (issues disabled). These are
// expected states for some repos and should not be treated as API errors.
var ErrIssuesUnavailable = errors.New("issues unavailable for repo")

// conventionIssueTitle returns the standardised issue title for a convention violation.
// The format is: [Convention] <id>: <description>
func conventionIssueTitle(conventionID, description string) string {
	return fmt.Sprintf("[Convention] %s: %s", conventionID, description)
}

// conventionIssuePrefix returns the stable title prefix for a convention.
// The format is: [Convention] <id>:
// The convention ID is documented as immutable, so this prefix is safe to use
// for matching existing issues even when the description has changed.
func conventionIssuePrefix(conventionID string) string {
	return fmt.Sprintf("[Convention] %s:", conventionID)
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
	// Detail is the specific detail string from the convention check result,
	// e.g. "Expected: lucos/deploy-avalon; Found: none". May be empty.
	Detail string
}

// EnsureIssueExists checks whether an open audit-finding issue exists for the
// given convention on the given repo. If one exists, it returns its URL.
// If none exists, it creates a new one (referencing the most recent closed
// issue if any) and returns the new issue's URL.
func (c *GitHubIssueClient) EnsureIssueExists(repo string, conv ConventionInfo) (string, error) {
	title := conventionIssueTitle(conv.ID, conv.Description)

	// Check for an existing open issue (matched by convention ID prefix,
	// not exact title, so stale-description issues are still found).
	existingURL, err := c.findOpenIssue(repo, conv.ID)
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
// and returns the HTML URL of the first one whose title matches the convention ID
// prefix, or "" if none.
func (c *GitHubIssueClient) findOpenIssue(repo, conventionID string) (string, error) {
	issue, err := c.findOpenIssueObject(repo, conventionID)
	if err != nil {
		return "", err
	}
	if issue == nil {
		return "", nil
	}
	return issue.HTMLURL, nil
}

// findOpenIssueObject fetches open issues with the audit-finding label on the
// given repo and returns the first gitHubIssue whose title starts with the
// "[Convention] <id>:" prefix, or nil if none.
//
// Prefix matching rather than exact title matching means issues raised under an
// older Description value are still found when the convention's Description
// field changes. The convention ID is documented as immutable, so the prefix is
// stable.
func (c *GitHubIssueClient) findOpenIssueObject(repo, conventionID string) (*gitHubIssue, error) {
	// Issues List API: fetch all open audit-finding issues, then filter locally.
	// per_page=100 is the maximum; pagination is not needed in practice since there should be
	// at most one open audit-finding issue per convention per repo.
	listURL := fmt.Sprintf("%s/repos/%s/issues?labels=%s&state=open&per_page=100", c.baseURL, repo, auditFindingLabel)

	issues, err := c.fetchIssuesList(listURL)
	if err != nil {
		return nil, fmt.Errorf("failed to list open issues: %w", err)
	}

	prefix := conventionIssuePrefix(conventionID)
	for i, issue := range issues {
		if strings.HasPrefix(issue.Title, prefix) {
			return &issues[i], nil
		}
	}
	return nil, nil
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
// returns the decoded slice of issues. It handles rate limit responses by
// waiting for the reset window (up to rateLimitMaxWait) and retrying once.
func (c *GitHubIssueClient) fetchIssuesList(listURL string) ([]gitHubIssue, error) {
	for attempt := 0; attempt < 2; attempt++ {
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

		if resp.StatusCode == http.StatusGone {
			// 410 means issues have been disabled on this repo.
			return nil, fmt.Errorf("GitHub issues list API returned 410 (issues disabled): %w", ErrIssuesUnavailable)
		}

		if resp.StatusCode == http.StatusForbidden {
			if waitErr := handleRateLimitError(resp, body); waitErr != nil {
				if isRateLimitBody(body) {
					return nil, waitErr
				}
				// Non-rate-limit 403 — repo is archived or otherwise read-only.
				return nil, fmt.Errorf("%s: %w", waitErr.Error(), ErrIssuesUnavailable)
			}
			// Rate limit wait succeeded — loop to retry.
			continue
		}

		checkRateLimitHeaders(resp)

		if resp.StatusCode != http.StatusOK {
			return nil, fmt.Errorf("GitHub issues list API returned %d: %s", resp.StatusCode, body)
		}

		var issues []gitHubIssue
		if err := json.Unmarshal(body, &issues); err != nil {
			return nil, fmt.Errorf("failed to decode issues list response: %w", err)
		}

		return issues, nil
	}

	return nil, fmt.Errorf("GitHub issues list API: rate limit retry exhausted")
}

// CloseIssueIfOpen finds any open audit-finding issue for the given convention
// on the given repo and closes it with state_reason "completed". If no open
// issue is found, it is a no-op. A closing comment is posted before closing
// to explain why the issue was resolved.
func (c *GitHubIssueClient) CloseIssueIfOpen(repo string, conv ConventionInfo) error {
	issue, err := c.findOpenIssueObject(repo, conv.ID)
	if err != nil {
		return fmt.Errorf("failed to search for open issue to close: %w", err)
	}
	if issue == nil {
		// No open issue — nothing to do.
		return nil
	}

	// Post a comment before closing so there is a visible explanation.
	comment := fmt.Sprintf("The `%s` convention is now passing for this repository — closing this issue as completed.\n\nThis issue was automatically closed by the lucos_repos audit tool.", conv.ID)
	if err := c.postIssueComment(repo, issue.Number, comment); err != nil {
		// Non-fatal: log and continue — closing without a comment is better than not closing.
		slog.Warn("Failed to post closing comment on audit-finding issue",
			"repo", repo, "convention", conv.ID, "issue", issue.Number, "error", err)
	}

	if err := c.closeIssue(repo, issue.Number); err != nil {
		return fmt.Errorf("failed to close audit-finding issue #%d: %w", issue.Number, err)
	}

	slog.Info("Closed audit-finding issue", "repo", repo, "convention", conv.ID, "issue", issue.Number)
	return nil
}

// postIssueComment posts a comment on the given issue.
func (c *GitHubIssueClient) postIssueComment(repo string, issueNumber int, body string) error {
	payload := map[string]string{"body": body}
	payloadJSON, err := json.Marshal(payload)
	if err != nil {
		return fmt.Errorf("failed to marshal comment payload: %w", err)
	}

	url := fmt.Sprintf("%s/repos/%s/issues/%d/comments", c.baseURL, repo, issueNumber)
	req, err := http.NewRequest("POST", url, bytes.NewReader(payloadJSON))
	if err != nil {
		return fmt.Errorf("failed to build comment request: %w", err)
	}
	req.Header.Set("Authorization", "Bearer "+c.token)
	req.Header.Set("Accept", "application/vnd.github+json")
	req.Header.Set("X-GitHub-Api-Version", "2022-11-28")
	req.Header.Set("Content-Type", "application/json")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return fmt.Errorf("GitHub post comment request failed: %w", err)
	}
	respBody, err := io.ReadAll(resp.Body)
	resp.Body.Close()
	if err != nil {
		return fmt.Errorf("failed to read post comment response: %w", err)
	}
	if resp.StatusCode != http.StatusCreated {
		return fmt.Errorf("GitHub post comment API returned %d: %s", resp.StatusCode, respBody)
	}
	return nil
}

// closeIssueRequest is the JSON body for closing a GitHub issue.
type closeIssueRequest struct {
	State       string `json:"state"`
	StateReason string `json:"state_reason"`
}

// closeIssue closes the given issue with state_reason "completed".
func (c *GitHubIssueClient) closeIssue(repo string, issueNumber int) error {
	payload := closeIssueRequest{
		State:       "closed",
		StateReason: "completed",
	}
	payloadJSON, err := json.Marshal(payload)
	if err != nil {
		return fmt.Errorf("failed to marshal close payload: %w", err)
	}

	url := fmt.Sprintf("%s/repos/%s/issues/%d", c.baseURL, repo, issueNumber)
	req, err := http.NewRequest("PATCH", url, bytes.NewReader(payloadJSON))
	if err != nil {
		return fmt.Errorf("failed to build close issue request: %w", err)
	}
	req.Header.Set("Authorization", "Bearer "+c.token)
	req.Header.Set("Accept", "application/vnd.github+json")
	req.Header.Set("X-GitHub-Api-Version", "2022-11-28")
	req.Header.Set("Content-Type", "application/json")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return fmt.Errorf("GitHub close issue request failed: %w", err)
	}
	respBody, err := io.ReadAll(resp.Body)
	resp.Body.Close()
	if err != nil {
		return fmt.Errorf("failed to read close issue response: %w", err)
	}
	if resp.StatusCode == http.StatusForbidden || resp.StatusCode == http.StatusGone {
		return fmt.Errorf("GitHub close issue API returned %d (issues unavailable): %s: %w", resp.StatusCode, respBody, ErrIssuesUnavailable)
	}
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("GitHub close issue API returned %d: %s", resp.StatusCode, respBody)
	}
	return nil
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

	if conv.Detail != "" {
		body += fmt.Sprintf("\n\n**Detail:** %s", conv.Detail)
	}

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

	if resp.StatusCode == http.StatusForbidden || resp.StatusCode == http.StatusGone {
		// 403 = repo archived (read-only); 410 = issues disabled.
		return "", fmt.Errorf("GitHub create issue API returned %d: %s: %w", resp.StatusCode, respBody, ErrIssuesUnavailable)
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
