package conventions

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

// TestAll_HasAtLeastOne verifies that at least one convention is registered.
func TestAll_HasAtLeastOne(t *testing.T) {
	cs := All()
	if len(cs) == 0 {
		t.Fatal("expected at least one convention to be registered, got none")
	}
}

// TestAll_HasCircleCIConvention verifies that the has-circleci-config convention is registered.
func TestAll_HasCircleCIConvention(t *testing.T) {
	cs := All()
	found := false
	for _, c := range cs {
		if c.ID == "has-circleci-config" {
			found = true
			if c.Description == "" {
				t.Error("has-circleci-config convention has empty description")
			}
			if c.Check == nil {
				t.Error("has-circleci-config convention has nil Check function")
			}
			break
		}
	}
	if !found {
		t.Error("has-circleci-config convention not found in registry")
	}
}

// TestAll_ReturnsCopy verifies that All returns an independent copy
// (modifying the returned slice should not affect the registry).
func TestAll_ReturnsCopy(t *testing.T) {
	first := All()
	second := All()
	if len(first) != len(second) {
		t.Errorf("expected both calls to return same length, got %d and %d", len(first), len(second))
	}
	// Modifying the returned slice should not affect the next call.
	first[0].ID = "mutated"
	third := All()
	if third[0].ID == "mutated" {
		t.Error("All returned a reference to the internal slice, not a copy")
	}
}

// TestHasCircleCIConfig_Pass verifies the convention passes when the file exists.
func TestHasCircleCIConfig_Pass(t *testing.T) {
	// Set up a fake GitHub API server that returns 200 for the file path.
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/repos/lucas42/test_repo/contents/.circleci/config.yml" {
			w.WriteHeader(http.StatusOK)
			w.Write([]byte(`{"type":"file"}`))
			return
		}
		w.WriteHeader(http.StatusNotFound)
	}))
	defer server.Close()

	exists, err := GitHubFileExistsFromBase(server.URL, "fake-token", "lucas42/test_repo", ".circleci/config.yml")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !exists {
		t.Error("expected file to exist, got false")
	}
}

// TestHasCircleCIConfig_Fail verifies the convention fails when the file is absent.
func TestHasCircleCIConfig_Fail(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNotFound)
		w.Write([]byte(`{"message":"Not Found"}`))
	}))
	defer server.Close()

	exists, err := GitHubFileExistsFromBase(server.URL, "fake-token", "lucas42/test_repo", ".circleci/config.yml")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if exists {
		t.Error("expected file to not exist, got true")
	}
}

// TestHasCircleCIConfig_Error verifies that unexpected HTTP status codes return an error.
func TestHasCircleCIConfig_Error(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer server.Close()

	_, err := GitHubFileExistsFromBase(server.URL, "fake-token", "lucas42/test_repo", ".circleci/config.yml")
	if err == nil {
		t.Error("expected error for 500 response, got nil")
	}
}
