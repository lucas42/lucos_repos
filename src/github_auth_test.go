package main

import (
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"encoding/base64"
	"encoding/pem"
	"strings"
	"testing"
)

// generateTestKey creates a fresh RSA private key for testing.
func generateTestKey(t *testing.T) *rsa.PrivateKey {
	t.Helper()
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatalf("failed to generate test RSA key: %v", err)
	}
	return key
}

// encodeKeyToPEM encodes an RSA private key as a PEM string in the correct format.
func encodeKeyToPEM(key *rsa.PrivateKey) string {
	pemBytes := pem.EncodeToMemory(&pem.Block{
		Type:  "RSA PRIVATE KEY",
		Bytes: x509.MarshalPKCS1PrivateKey(key),
	})
	return string(pemBytes)
}

// spaceFlattened converts a well-formed PEM string to the space-flattened
// format that lucos_creds stores (spaces in place of newlines in the body).
func spaceFlattened(pemStr string) string {
	const header = "-----BEGIN RSA PRIVATE KEY-----"
	const footer = "-----END RSA PRIVATE KEY-----"

	headerIdx := strings.Index(pemStr, header)
	footerIdx := strings.Index(pemStr, footer)

	body := strings.TrimSpace(pemStr[headerIdx+len(header) : footerIdx])
	flatBody := strings.ReplaceAll(body, "\n", " ")

	return header + " " + flatBody + " " + footer
}

func TestParseRSAPrivateKey_WellFormedPEM(t *testing.T) {
	originalKey := generateTestKey(t)
	pemStr := encodeKeyToPEM(originalKey)

	parsed, err := parseRSAPrivateKey(pemStr)
	if err != nil {
		t.Fatalf("parseRSAPrivateKey returned error for well-formed PEM: %v", err)
	}

	if parsed.N.Cmp(originalKey.N) != 0 {
		t.Error("parsed key modulus does not match original")
	}
}

func TestParseRSAPrivateKey_SpaceFlattened(t *testing.T) {
	originalKey := generateTestKey(t)
	pemStr := encodeKeyToPEM(originalKey)
	flatPEM := spaceFlattened(pemStr)

	parsed, err := parseRSAPrivateKey(flatPEM)
	if err != nil {
		t.Fatalf("parseRSAPrivateKey returned error for space-flattened PEM: %v", err)
	}

	if parsed.N.Cmp(originalKey.N) != 0 {
		t.Error("parsed key modulus does not match original")
	}
}

func TestParseRSAPrivateKey_MissingHeader(t *testing.T) {
	_, err := parseRSAPrivateKey("not a pem key at all")
	if err == nil {
		t.Error("expected error for missing PEM header, got nil")
	}
}

func TestGenerateJWT_Structure(t *testing.T) {
	key := generateTestKey(t)
	client := &GitHubAuthClient{
		appID:      "12345",
		privateKey: key,
	}

	jwt, err := client.generateJWT()
	if err != nil {
		t.Fatalf("generateJWT returned error: %v", err)
	}

	// A JWT must have exactly three dot-separated parts.
	parts := strings.Split(jwt, ".")
	if len(parts) != 3 {
		t.Fatalf("expected 3 JWT parts, got %d", len(parts))
	}

	// Verify the header decodes to the expected algorithm.
	headerJSON, err := base64.RawURLEncoding.DecodeString(parts[0])
	if err != nil {
		t.Fatalf("failed to decode JWT header: %v", err)
	}
	if !strings.Contains(string(headerJSON), `"alg":"RS256"`) {
		t.Errorf("JWT header does not contain expected alg, got: %s", headerJSON)
	}

	// Verify the payload contains the app ID.
	payloadJSON, err := base64.RawURLEncoding.DecodeString(parts[1])
	if err != nil {
		t.Fatalf("failed to decode JWT payload: %v", err)
	}
	if !strings.Contains(string(payloadJSON), `"iss":"12345"`) {
		t.Errorf("JWT payload does not contain expected iss, got: %s", payloadJSON)
	}

	// Verify the signature is non-empty.
	if len(parts[2]) == 0 {
		t.Error("JWT signature is empty")
	}
}

func TestGenerateJWT_DifferentEachTime(t *testing.T) {
	// RSA PKCS1v15 signatures are deterministic for the same input,
	// but we call generateJWT at slightly different times so iat/exp may differ.
	// More importantly, confirm there are no errors with repeated calls.
	key := generateTestKey(t)
	client := &GitHubAuthClient{
		appID:      "99999",
		privateKey: key,
	}

	for i := 0; i < 3; i++ {
		_, err := client.generateJWT()
		if err != nil {
			t.Fatalf("generateJWT call %d returned error: %v", i, err)
		}
	}
}
