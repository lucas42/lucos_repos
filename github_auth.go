package main

import (
	"crypto"
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha256"
	"crypto/x509"
	"encoding/base64"
	"encoding/json"
	"encoding/pem"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"strings"
	"sync"
	"time"
)

// GitHubAuthClient manages GitHub App authentication, including JWT generation
// and installation token caching.
type GitHubAuthClient struct {
	appID          string
	privateKey     *rsa.PrivateKey
	installationID int64

	mu           sync.Mutex
	cachedToken  string
	tokenExpires time.Time
}

// NewGitHubAuthClient creates a GitHubAuthClient from GITHUB_APP_ID and GITHUB_APP_PEM
// environment variables. It discovers the installation ID from the GitHub API.
func NewGitHubAuthClient() (*GitHubAuthClient, error) {
	appID := os.Getenv("GITHUB_APP_ID")
	if appID == "" {
		return nil, fmt.Errorf("GITHUB_APP_ID environment variable is not set")
	}

	pemRaw := os.Getenv("GITHUB_APP_PEM")
	if pemRaw == "" {
		return nil, fmt.Errorf("GITHUB_APP_PEM environment variable is not set")
	}

	privateKey, err := parseRSAPrivateKey(pemRaw)
	if err != nil {
		return nil, fmt.Errorf("failed to parse GITHUB_APP_PEM: %w", err)
	}

	client := &GitHubAuthClient{
		appID:      appID,
		privateKey: privateKey,
	}

	// Discover the installation ID at startup.
	installationID, err := client.discoverInstallationID()
	if err != nil {
		return nil, fmt.Errorf("failed to discover GitHub App installation ID: %w", err)
	}
	client.installationID = installationID
	slog.Info("GitHub App installation discovered", "installation_id", installationID)

	return client, nil
}

// parseRSAPrivateKey parses an RSA private key from a PEM string.
// lucos_creds stores PEM keys with spaces instead of newlines in the PEM body;
// this function reconstructs the correct PEM block before parsing.
func parseRSAPrivateKey(pemStr string) (*rsa.PrivateKey, error) {
	// lucos_creds flattens newlines to spaces in the PEM body. The header and
	// footer contain spaces too ("-----BEGIN RSA PRIVATE KEY-----"), so we
	// locate them explicitly and fix only the base64 body in between.
	const header = "-----BEGIN RSA PRIVATE KEY-----"
	const footer = "-----END RSA PRIVATE KEY-----"

	headerIdx := strings.Index(pemStr, header)
	footerIdx := strings.Index(pemStr, footer)
	if headerIdx == -1 || footerIdx == -1 {
		return nil, fmt.Errorf("PEM key is missing header or footer")
	}

	body := pemStr[headerIdx+len(header) : footerIdx]
	// The body may have spaces instead of newlines — replace them.
	body = strings.ReplaceAll(strings.TrimSpace(body), " ", "\n")

	reconstructed := header + "\n" + body + "\n" + footer + "\n"

	block, _ := pem.Decode([]byte(reconstructed))
	if block == nil {
		return nil, fmt.Errorf("failed to decode PEM block")
	}

	key, err := x509.ParsePKCS1PrivateKey(block.Bytes)
	if err != nil {
		return nil, fmt.Errorf("failed to parse RSA private key: %w", err)
	}

	return key, nil
}

// generateJWT creates a signed JWT for the GitHub App.
// GitHub requires iat 60 seconds in the past (clock skew tolerance) and
// exp no more than 10 minutes in the future.
func (c *GitHubAuthClient) generateJWT() (string, error) {
	now := time.Now()
	iat := now.Add(-60 * time.Second).Unix()
	exp := now.Add(9 * time.Minute).Unix()

	headerJSON := `{"alg":"RS256","typ":"JWT"}`
	payloadJSON := fmt.Sprintf(`{"iat":%d,"exp":%d,"iss":"%s"}`, iat, exp, c.appID)

	encodedHeader := base64.RawURLEncoding.EncodeToString([]byte(headerJSON))
	encodedPayload := base64.RawURLEncoding.EncodeToString([]byte(payloadJSON))

	signingInput := encodedHeader + "." + encodedPayload

	hash := sha256.Sum256([]byte(signingInput))
	signature, err := rsa.SignPKCS1v15(rand.Reader, c.privateKey, crypto.SHA256, hash[:])
	if err != nil {
		return "", fmt.Errorf("failed to sign JWT: %w", err)
	}

	encodedSignature := base64.RawURLEncoding.EncodeToString(signature)

	return signingInput + "." + encodedSignature, nil
}

type installation struct {
	ID int64 `json:"id"`
}

// discoverInstallationID fetches the installation ID for this GitHub App from the API.
func (c *GitHubAuthClient) discoverInstallationID() (int64, error) {
	jwt, err := c.generateJWT()
	if err != nil {
		return 0, err
	}

	req, err := http.NewRequest("GET", "https://api.github.com/app/installations", nil)
	if err != nil {
		return 0, err
	}
	req.Header.Set("Authorization", "Bearer "+jwt)
	req.Header.Set("Accept", "application/vnd.github+json")
	req.Header.Set("X-GitHub-Api-Version", "2022-11-28")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return 0, fmt.Errorf("failed to fetch installations: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return 0, fmt.Errorf("GitHub API returned %d fetching installations", resp.StatusCode)
	}

	var installations []installation
	if err := json.NewDecoder(resp.Body).Decode(&installations); err != nil {
		return 0, fmt.Errorf("failed to decode installations response: %w", err)
	}

	if len(installations) == 0 {
		return 0, fmt.Errorf("no installations found for this GitHub App")
	}

	// Use the first installation (lucos_repos only has one: the lucas42 org/account).
	return installations[0].ID, nil
}

type accessTokenResponse struct {
	Token     string    `json:"token"`
	ExpiresAt time.Time `json:"expires_at"`
}

// GetInstallationToken returns a valid installation access token, refreshing
// it automatically when it is within 5 minutes of expiry.
func (c *GitHubAuthClient) GetInstallationToken() (string, error) {
	c.mu.Lock()
	defer c.mu.Unlock()

	// Return the cached token if it still has more than 5 minutes left.
	if c.cachedToken != "" && time.Now().Before(c.tokenExpires.Add(-5*time.Minute)) {
		return c.cachedToken, nil
	}

	jwt, err := c.generateJWT()
	if err != nil {
		return "", err
	}

	url := fmt.Sprintf("https://api.github.com/app/installations/%d/access_tokens", c.installationID)
	req, err := http.NewRequest("POST", url, nil)
	if err != nil {
		return "", err
	}
	req.Header.Set("Authorization", "Bearer "+jwt)
	req.Header.Set("Accept", "application/vnd.github+json")
	req.Header.Set("X-GitHub-Api-Version", "2022-11-28")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return "", fmt.Errorf("failed to exchange JWT for installation token: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusCreated {
		return "", fmt.Errorf("GitHub API returned %d exchanging JWT for installation token", resp.StatusCode)
	}

	var tokenResp accessTokenResponse
	if err := json.NewDecoder(resp.Body).Decode(&tokenResp); err != nil {
		return "", fmt.Errorf("failed to decode access token response: %w", err)
	}

	c.cachedToken = tokenResp.Token
	c.tokenExpires = tokenResp.ExpiresAt
	slog.Info("GitHub installation token refreshed", "expires_at", tokenResp.ExpiresAt)

	return c.cachedToken, nil
}
