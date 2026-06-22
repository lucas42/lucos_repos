package conventions

import (
	"os"
	"strings"
	"testing"
)

// cataloguePath is the committed catalogue, relative to this package directory.
const cataloguePath = "../docs/conventions.md"

// TestConventionCatalogueIsCurrent fails if the committed docs/conventions.md has
// drifted from what RenderCatalogue() produces from the live registry. This is
// what makes the catalogue unable to silently fall out of step with the enforced
// conventions (lucos_repos ADR-0007). When it fails, regenerate the file:
//
//	go run ./src conventions > docs/conventions.md
func TestConventionCatalogueIsCurrent(t *testing.T) {
	want := RenderCatalogue()

	got, err := os.ReadFile(cataloguePath)
	if err != nil {
		t.Fatalf("could not read %s: %v\nRegenerate with: go run ./src conventions > docs/conventions.md", cataloguePath, err)
	}

	if string(got) != want {
		t.Errorf("%s is stale — it no longer matches the convention registry.\n"+
			"Regenerate with: go run ./src conventions > docs/conventions.md", cataloguePath)
	}
}

// TestAllConventionsHaveRequiredFields guards the quality fields the catalogue
// (and the generated issue bodies) depend on. The convention-guide marks
// Description, Rationale and Guidance as required; an empty one produces a
// useless catalogue entry and a useless issue.
func TestAllConventionsHaveRequiredFields(t *testing.T) {
	for _, c := range All() {
		if strings.TrimSpace(c.ID) == "" {
			t.Errorf("a convention has an empty ID")
			continue
		}
		if strings.TrimSpace(c.Description) == "" {
			t.Errorf("convention %q has an empty Description", c.ID)
		}
		if strings.TrimSpace(c.Rationale) == "" {
			t.Errorf("convention %q has an empty Rationale", c.ID)
		}
		if strings.TrimSpace(c.Guidance) == "" {
			t.Errorf("convention %q has an empty Guidance", c.ID)
		}
	}
}
