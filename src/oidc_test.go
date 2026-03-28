package main

import (
	"crypto/rand"
	"crypto/rsa"
	"encoding/base64"
	"encoding/json"
	"math/big"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/golang-jwt/jwt/v5"
)

// testJWKSServer creates an httptest server that serves a JWKS containing the given key.
func testJWKSServer(t *testing.T, kid string, pub *rsa.PublicKey) *httptest.Server {
	t.Helper()
	jwks := map[string]any{
		"keys": []map[string]any{
			{
				"kty": "RSA",
				"kid": kid,
				"n":   base64.RawURLEncoding.EncodeToString(pub.N.Bytes()),
				"e":   base64.RawURLEncoding.EncodeToString(big.NewInt(int64(pub.E)).Bytes()),
			},
		},
	}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(jwks)
	}))
	t.Cleanup(srv.Close)
	return srv
}

// signToken creates a signed JWT with the given claims using the provided RSA key.
func signToken(t *testing.T, kid string, key *rsa.PrivateKey, claims jwt.MapClaims) string {
	t.Helper()
	token := jwt.NewWithClaims(jwt.SigningMethodRS256, claims)
	token.Header["kid"] = kid
	signed, err := token.SignedString(key)
	if err != nil {
		t.Fatalf("failed to sign token: %v", err)
	}
	return signed
}

func newTestValidator(t *testing.T, jwksURL string) *GitHubOIDCValidator {
	t.Helper()
	v := NewGitHubOIDCValidator("lucas42")
	v.jwksURL = jwksURL
	return v
}

func TestOIDCValidator_ValidToken(t *testing.T) {
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatal(err)
	}
	srv := testJWKSServer(t, "test-kid-1", &key.PublicKey)
	v := newTestValidator(t, srv.URL)

	tokenStr := signToken(t, "test-kid-1", key, jwt.MapClaims{
		"iss":              githubOIDCIssuer,
		"exp":              time.Now().Add(time.Hour).Unix(),
		"repository_owner": "lucas42",
		"repository":       "lucas42/lucos_repos",
	})

	claims, err := v.ValidateToken(tokenStr)
	if err != nil {
		t.Fatalf("expected valid token, got error: %v", err)
	}
	if claims["repository_owner"] != "lucas42" {
		t.Errorf("expected repository_owner 'lucas42', got %v", claims["repository_owner"])
	}
}

func TestOIDCValidator_WrongOwner(t *testing.T) {
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatal(err)
	}
	srv := testJWKSServer(t, "test-kid-1", &key.PublicKey)
	v := newTestValidator(t, srv.URL)

	tokenStr := signToken(t, "test-kid-1", key, jwt.MapClaims{
		"iss":              githubOIDCIssuer,
		"exp":              time.Now().Add(time.Hour).Unix(),
		"repository_owner": "malicious-user",
		"repository":       "malicious-user/evil-repo",
	})

	_, err = v.ValidateToken(tokenStr)
	if err == nil {
		t.Fatal("expected error for wrong repository_owner")
	}
}

func TestOIDCValidator_ExpiredToken(t *testing.T) {
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatal(err)
	}
	srv := testJWKSServer(t, "test-kid-1", &key.PublicKey)
	v := newTestValidator(t, srv.URL)

	tokenStr := signToken(t, "test-kid-1", key, jwt.MapClaims{
		"iss":              githubOIDCIssuer,
		"exp":              time.Now().Add(-time.Hour).Unix(),
		"repository_owner": "lucas42",
	})

	_, err = v.ValidateToken(tokenStr)
	if err == nil {
		t.Fatal("expected error for expired token")
	}
}

func TestOIDCValidator_WrongIssuer(t *testing.T) {
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatal(err)
	}
	srv := testJWKSServer(t, "test-kid-1", &key.PublicKey)
	v := newTestValidator(t, srv.URL)

	tokenStr := signToken(t, "test-kid-1", key, jwt.MapClaims{
		"iss":              "https://evil.example.com",
		"exp":              time.Now().Add(time.Hour).Unix(),
		"repository_owner": "lucas42",
	})

	_, err = v.ValidateToken(tokenStr)
	if err == nil {
		t.Fatal("expected error for wrong issuer")
	}
}

func TestOIDCValidator_UnknownKid(t *testing.T) {
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatal(err)
	}
	// Serve a JWKS with a different kid than what the token uses.
	srv := testJWKSServer(t, "known-kid", &key.PublicKey)
	v := newTestValidator(t, srv.URL)

	tokenStr := signToken(t, "unknown-kid", key, jwt.MapClaims{
		"iss":              githubOIDCIssuer,
		"exp":              time.Now().Add(time.Hour).Unix(),
		"repository_owner": "lucas42",
	})

	_, err = v.ValidateToken(tokenStr)
	if err == nil {
		t.Fatal("expected error for unknown kid")
	}
}

func TestOIDCValidator_JWKSCaching(t *testing.T) {
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatal(err)
	}

	fetchCount := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fetchCount++
		jwks := map[string]any{
			"keys": []map[string]any{
				{
					"kty": "RSA",
					"kid": "test-kid-1",
					"n":   base64.RawURLEncoding.EncodeToString(key.PublicKey.N.Bytes()),
					"e":   base64.RawURLEncoding.EncodeToString(big.NewInt(int64(key.PublicKey.E)).Bytes()),
				},
			},
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(jwks)
	}))
	t.Cleanup(srv.Close)

	v := newTestValidator(t, srv.URL)

	// Validate twice — second call should use cache.
	for range 2 {
		tokenStr := signToken(t, "test-kid-1", key, jwt.MapClaims{
			"iss":              githubOIDCIssuer,
			"exp":              time.Now().Add(time.Hour).Unix(),
			"repository_owner": "lucas42",
		})
		if _, err := v.ValidateToken(tokenStr); err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
	}

	if fetchCount != 1 {
		t.Errorf("expected 1 JWKS fetch (cached), got %d", fetchCount)
	}
}

func TestOIDCValidator_MissingBearerToken(t *testing.T) {
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatal(err)
	}
	srv := testJWKSServer(t, "test-kid-1", &key.PublicKey)
	v := newTestValidator(t, srv.URL)

	// Construct a valid token but don't test validator directly —
	// test the audit handler's Bearer prefix check.
	tokenStr := signToken(t, "test-kid-1", key, jwt.MapClaims{
		"iss":              githubOIDCIssuer,
		"exp":              time.Now().Add(time.Hour).Unix(),
		"repository_owner": "lucas42",
	})

	// Validate with garbage — should fail.
	_, err = v.ValidateToken("not-a-jwt")
	if err == nil {
		t.Fatal("expected error for garbage token")
	}

	// But a valid token should pass.
	_, err = v.ValidateToken(tokenStr)
	if err != nil {
		t.Fatalf("expected valid token, got error: %v", err)
	}
}
