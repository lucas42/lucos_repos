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
	"lucos_repos/conventions"
)

// auditTestJWKSServer creates an httptest server for audit handler tests.
func auditTestJWKSServer(t *testing.T, kid string, pub *rsa.PublicKey) *httptest.Server {
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

// auditTestToken creates a signed JWT for audit handler tests.
func auditTestToken(t *testing.T, kid string, key *rsa.PrivateKey, owner string) string {
	t.Helper()
	token := jwt.NewWithClaims(jwt.SigningMethodRS256, jwt.MapClaims{
		"iss":              githubOIDCIssuer,
		"aud":              githubOIDCAudience,
		"exp":              time.Now().Add(time.Hour).Unix(),
		"repository_owner": owner,
	})
	token.Header["kid"] = kid
	signed, err := token.SignedString(key)
	if err != nil {
		t.Fatalf("failed to sign token: %v", err)
	}
	return signed
}

func newAuditTestValidator(t *testing.T, jwksURL string) *GitHubOIDCValidator {
	t.Helper()
	v := NewGitHubOIDCValidator("lucas42")
	v.jwksURL = jwksURL
	return v
}

func TestSingleRepoStatusHandler_NotFound(t *testing.T) {
	db := openTestDB(t)

	handler := newSingleRepoStatusHandler(db)
	req := httptest.NewRequest("GET", "/api/status/lucas42/unknown_repo", nil)
	w := httptest.NewRecorder()

	handler(w, req)

	if w.Code != http.StatusNotFound {
		t.Errorf("expected 404, got %d", w.Code)
	}
}

func TestSingleRepoStatusHandler_Found(t *testing.T) {
	db := openTestDB(t)
	db.UpsertConvention("test-convention", "test")
	db.UpsertRepo("lucas42/test_repo", "system", false)
	db.SaveFinding(conventions.ConventionResult{
		Convention: "test-convention",
		Pass:       true,
		Detail:     "all good",
	}, "lucas42/test_repo", "")

	handler := newSingleRepoStatusHandler(db)
	req := httptest.NewRequest("GET", "/api/status/lucas42/test_repo", nil)
	w := httptest.NewRecorder()

	handler(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}

	var resp singleRepoStatusResponse
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}
	if resp.Repo != "lucas42/test_repo" {
		t.Errorf("expected repo 'lucas42/test_repo', got %q", resp.Repo)
	}
	if resp.RepoType != "system" {
		t.Errorf("expected repo_type 'system', got %q", resp.RepoType)
	}
	if check, ok := resp.Checks["test-convention"]; !ok {
		t.Error("expected test-convention in checks")
	} else {
		if check.Status != "pass" {
			t.Errorf("expected check status 'pass', got %q", check.Status)
		}
		if check.Detail != "all good" {
			t.Errorf("expected check detail 'all good', got %q", check.Detail)
		}
	}
}

func TestSingleRepoStatusHandler_BadPath(t *testing.T) {
	db := openTestDB(t)

	handler := newSingleRepoStatusHandler(db)
	req := httptest.NewRequest("GET", "/api/status/noslash", nil)
	w := httptest.NewRecorder()

	handler(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("expected 400, got %d", w.Code)
	}
}

func TestAuditHandler_NoBearerToken(t *testing.T) {
	db := openTestDB(t)
	key, _ := rsa.GenerateKey(rand.Reader, 2048)
	srv := auditTestJWKSServer(t, "k1", &key.PublicKey)
	v := newAuditTestValidator(t, srv.URL)

	handler := newAuditHandler(db, nil, "", "", v)
	req := httptest.NewRequest("POST", "/api/audit/lucas42/test_repo?ref=my-branch", nil)
	w := httptest.NewRecorder()

	handler(w, req)

	if w.Code != http.StatusUnauthorized {
		t.Errorf("expected 401 when no Bearer token, got %d", w.Code)
	}
}

func TestAuditHandler_InvalidToken(t *testing.T) {
	db := openTestDB(t)
	key, _ := rsa.GenerateKey(rand.Reader, 2048)
	srv := auditTestJWKSServer(t, "k1", &key.PublicKey)
	v := newAuditTestValidator(t, srv.URL)

	handler := newAuditHandler(db, nil, "", "", v)
	req := httptest.NewRequest("POST", "/api/audit/lucas42/test_repo?ref=my-branch", nil)
	req.Header.Set("Authorization", "Bearer invalid-token")
	w := httptest.NewRecorder()

	handler(w, req)

	if w.Code != http.StatusUnauthorized {
		t.Errorf("expected 401 for invalid token, got %d", w.Code)
	}
}

func TestAuditHandler_WrongOwner(t *testing.T) {
	db := openTestDB(t)
	key, _ := rsa.GenerateKey(rand.Reader, 2048)
	srv := auditTestJWKSServer(t, "k1", &key.PublicKey)
	v := newAuditTestValidator(t, srv.URL)

	handler := newAuditHandler(db, nil, "", "", v)
	tokenStr := auditTestToken(t, "k1", key, "evil-user")
	req := httptest.NewRequest("POST", "/api/audit/lucas42/test_repo?ref=my-branch", nil)
	req.Header.Set("Authorization", "Bearer "+tokenStr)
	w := httptest.NewRecorder()

	handler(w, req)

	if w.Code != http.StatusUnauthorized {
		t.Errorf("expected 401 for wrong owner, got %d", w.Code)
	}
}

func TestAuditHandler_KeySchemeRejected(t *testing.T) {
	db := openTestDB(t)
	key, _ := rsa.GenerateKey(rand.Reader, 2048)
	srv := auditTestJWKSServer(t, "k1", &key.PublicKey)
	v := newAuditTestValidator(t, srv.URL)

	handler := newAuditHandler(db, nil, "", "", v)
	req := httptest.NewRequest("POST", "/api/audit/lucas42/test_repo?ref=my-branch", nil)
	req.Header.Set("Authorization", "Key some-key")
	w := httptest.NewRecorder()

	handler(w, req)

	if w.Code != http.StatusUnauthorized {
		t.Errorf("expected 401 for Key scheme (must use Bearer), got %d", w.Code)
	}
}

func TestAuditHandler_UnknownRepo(t *testing.T) {
	db := openTestDB(t)
	key, _ := rsa.GenerateKey(rand.Reader, 2048)
	srv := auditTestJWKSServer(t, "k1", &key.PublicKey)
	v := newAuditTestValidator(t, srv.URL)

	handler := newAuditHandler(db, nil, "", "", v)
	tokenStr := auditTestToken(t, "k1", key, "lucas42")
	req := httptest.NewRequest("POST", "/api/audit/lucas42/unknown_repo?ref=my-branch", nil)
	req.Header.Set("Authorization", "Bearer "+tokenStr)
	w := httptest.NewRecorder()

	handler(w, req)

	if w.Code != http.StatusNotFound {
		t.Errorf("expected 404 for unknown repo, got %d", w.Code)
	}
}

func TestAuditRateLimiter(t *testing.T) {
	rl := newAuditRateLimiter(2, time.Minute)

	if !rl.allow("repo1") {
		t.Error("first request should be allowed")
	}
	if !rl.allow("repo1") {
		t.Error("second request should be allowed")
	}
	if rl.allow("repo1") {
		t.Error("third request should be rejected")
	}
	// Different repo should still work.
	if !rl.allow("repo2") {
		t.Error("first request for different repo should be allowed")
	}
}

// TestAuditHandler_UsesLiveTypeNotBaselineType verifies that RepoContext.Type
// (and the AppliesToType gate) comes from a fresh configy fetch, not the
// baseline's DB-cached value (#453). The repo's baseline says "component" but
// configy now reports it as a system — a system-only convention
// (container-naming) should become selectable as a result.
func TestAuditHandler_UsesLiveTypeNotBaselineType(t *testing.T) {
	db := openTestDB(t)
	db.UpsertRepo("lucas42/test_repo", "component", false)
	db.UpsertConvention("in-lucos-configy", "In lucos configy")
	db.SaveFinding(conventions.ConventionResult{Convention: "in-lucos-configy", Pass: true, Detail: "ok"}, "lucas42/test_repo", "")

	key, _ := rsa.GenerateKey(rand.Reader, 2048)
	jwksSrv := auditTestJWKSServer(t, "k1", &key.PublicKey)
	v := newAuditTestValidator(t, jwksSrv.URL)
	tokenStr := auditTestToken(t, "k1", key, "lucas42")

	ghServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// docker-compose.yml (and everything else) 404s — container-naming
		// treats a missing compose file as a pass, proving it was selected.
		w.WriteHeader(http.StatusNotFound)
	}))
	defer ghServer.Close()

	configyServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if r.URL.Path == "/systems" {
			json.NewEncoder(w).Encode([]configySystem{{ID: "test_repo", Hosts: []string{"avalon"}}})
			return
		}
		json.NewEncoder(w).Encode([]struct{}{})
	}))
	defer configyServer.Close()

	handler := newAuditHandler(db, fakeGitHubAuth(t), ghServer.URL, configyServer.URL, v)
	req := httptest.NewRequest("POST", "/api/audit/lucas42/test_repo?ref=my-branch", nil)
	req.Header.Set("Authorization", "Bearer "+tokenStr)
	w := httptest.NewRecorder()

	handler(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}

	var resp auditResponse
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}
	if _, ok := resp.Details["container-naming"]; !ok {
		t.Error("expected container-naming (system-only convention) to be selected using the live Type, not the stale 'component' baseline")
	}
}
