package main

import (
	"crypto/rsa"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"log/slog"
	"math/big"
	"net/http"
	"sync"
	"time"

	"github.com/golang-jwt/jwt/v5"
)

const (
	githubOIDCIssuer  = "https://token.actions.githubusercontent.com"
	githubJWKSURL     = "https://token.actions.githubusercontent.com/.well-known/jwks"
	jwksCacheDuration = time.Hour
)

// GitHubOIDCValidator validates GitHub Actions OIDC tokens.
type GitHubOIDCValidator struct {
	requiredOwner string
	jwksURL       string
	httpClient    *http.Client

	mu        sync.Mutex
	keys      map[string]*rsa.PublicKey
	fetchedAt time.Time
}

// NewGitHubOIDCValidator creates a validator that checks tokens from GitHub Actions
// and requires the repository_owner claim to match the given owner.
func NewGitHubOIDCValidator(requiredOwner string) *GitHubOIDCValidator {
	return &GitHubOIDCValidator{
		requiredOwner: requiredOwner,
		jwksURL:       githubJWKSURL,
		httpClient:    &http.Client{Timeout: 10 * time.Second},
	}
}

// ValidateToken validates a GitHub Actions OIDC JWT token.
// It returns the validated claims on success.
func (v *GitHubOIDCValidator) ValidateToken(tokenString string) (jwt.MapClaims, error) {
	keys, err := v.getKeys()
	if err != nil {
		return nil, fmt.Errorf("fetching JWKS: %w", err)
	}

	token, err := jwt.Parse(tokenString, func(token *jwt.Token) (any, error) {
		if _, ok := token.Method.(*jwt.SigningMethodRSA); !ok {
			return nil, fmt.Errorf("unexpected signing method: %v", token.Header["alg"])
		}
		kid, ok := token.Header["kid"].(string)
		if !ok {
			return nil, fmt.Errorf("missing kid in token header")
		}
		key, ok := keys[kid]
		if !ok {
			// Key not found — try refreshing JWKS in case keys rotated.
			refreshedKeys, err := v.refreshKeys()
			if err != nil {
				return nil, fmt.Errorf("refreshing JWKS: %w", err)
			}
			key, ok = refreshedKeys[kid]
			if !ok {
				return nil, fmt.Errorf("unknown key ID: %s", kid)
			}
		}
		return key, nil
	}, jwt.WithIssuer(githubOIDCIssuer), jwt.WithExpirationRequired())
	if err != nil {
		return nil, fmt.Errorf("validating token: %w", err)
	}

	claims, ok := token.Claims.(jwt.MapClaims)
	if !ok || !token.Valid {
		return nil, fmt.Errorf("invalid token claims")
	}

	// Check repository_owner claim.
	owner, _ := claims["repository_owner"].(string)
	if owner != v.requiredOwner {
		return nil, fmt.Errorf("repository_owner %q does not match required owner %q", owner, v.requiredOwner)
	}

	return claims, nil
}

// getKeys returns cached JWKS keys, fetching them if the cache is empty or expired.
func (v *GitHubOIDCValidator) getKeys() (map[string]*rsa.PublicKey, error) {
	v.mu.Lock()
	defer v.mu.Unlock()

	if v.keys != nil && time.Since(v.fetchedAt) < jwksCacheDuration {
		return v.keys, nil
	}

	return v.fetchKeysLocked()
}

// refreshKeys forces a JWKS refresh (for key rotation).
func (v *GitHubOIDCValidator) refreshKeys() (map[string]*rsa.PublicKey, error) {
	v.mu.Lock()
	defer v.mu.Unlock()
	return v.fetchKeysLocked()
}

// fetchKeysLocked fetches JWKS from GitHub. Caller must hold v.mu.
func (v *GitHubOIDCValidator) fetchKeysLocked() (map[string]*rsa.PublicKey, error) {
	resp, err := v.httpClient.Get(v.jwksURL)
	if err != nil {
		return nil, fmt.Errorf("fetching JWKS from %s: %w", v.jwksURL, err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("JWKS endpoint returned status %d", resp.StatusCode)
	}

	var jwks struct {
		Keys []struct {
			Kid string `json:"kid"`
			Kty string `json:"kty"`
			N   string `json:"n"`
			E   string `json:"e"`
		} `json:"keys"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&jwks); err != nil {
		return nil, fmt.Errorf("decoding JWKS: %w", err)
	}

	keys := make(map[string]*rsa.PublicKey, len(jwks.Keys))
	for _, k := range jwks.Keys {
		if k.Kty != "RSA" {
			continue
		}
		nBytes, err := base64.RawURLEncoding.DecodeString(k.N)
		if err != nil {
			slog.Warn("Skipping JWKS key with invalid modulus", "kid", k.Kid, "error", err)
			continue
		}
		eBytes, err := base64.RawURLEncoding.DecodeString(k.E)
		if err != nil {
			slog.Warn("Skipping JWKS key with invalid exponent", "kid", k.Kid, "error", err)
			continue
		}
		n := new(big.Int).SetBytes(nBytes)
		e := 0
		for _, b := range eBytes {
			e = e<<8 + int(b)
		}
		keys[k.Kid] = &rsa.PublicKey{N: n, E: e}
	}

	if len(keys) == 0 {
		return nil, fmt.Errorf("no valid RSA keys found in JWKS")
	}

	v.keys = keys
	v.fetchedAt = time.Now()
	slog.Info("Refreshed GitHub OIDC JWKS", "key_count", len(keys))
	return keys, nil
}
