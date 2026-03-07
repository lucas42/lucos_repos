package conventions

import "testing"

// TestAll_HasInLucosConfigyConvention verifies that the in-lucos-configy
// convention is registered.
func TestAll_HasInLucosConfigyConvention(t *testing.T) {
	cs := All()
	found := false
	for _, c := range cs {
		if c.ID == "in-lucos-configy" {
			found = true
			if c.Description == "" {
				t.Error("in-lucos-configy convention has empty description")
			}
			if c.Check == nil {
				t.Error("in-lucos-configy convention has nil Check function")
			}
			break
		}
	}
	if !found {
		t.Error("in-lucos-configy convention not found in registry")
	}
}

// TestInLucosConfigy_PassesForSystem verifies the convention passes for a repo
// typed as system.
func TestInLucosConfigy_PassesForSystem(t *testing.T) {
	repo := RepoContext{Name: "lucas42/lucos_photos", Type: RepoTypeSystem}
	result := checkInLucosConfigy(repo)
	if !result.Pass {
		t.Errorf("expected pass for system repo, got fail: %s", result.Detail)
	}
}

// TestInLucosConfigy_PassesForComponent verifies the convention passes for a
// repo typed as component.
func TestInLucosConfigy_PassesForComponent(t *testing.T) {
	repo := RepoContext{Name: "lucas42/lucos_navbar", Type: RepoTypeComponent}
	result := checkInLucosConfigy(repo)
	if !result.Pass {
		t.Errorf("expected pass for component repo, got fail: %s", result.Detail)
	}
}

// TestInLucosConfigy_PassesForScript verifies the convention passes for a repo
// typed as script.
func TestInLucosConfigy_PassesForScript(t *testing.T) {
	repo := RepoContext{Name: "lucas42/lucos_agent", Type: RepoTypeScript}
	result := checkInLucosConfigy(repo)
	if !result.Pass {
		t.Errorf("expected pass for script repo, got fail: %s", result.Detail)
	}
}

// TestInLucosConfigy_FailsForUnconfigured verifies the convention fails for a
// repo not listed in configy at all.
func TestInLucosConfigy_FailsForUnconfigured(t *testing.T) {
	repo := RepoContext{Name: "lucas42/lucos_new", Type: RepoTypeUnconfigured}
	result := checkInLucosConfigy(repo)
	if result.Pass {
		t.Errorf("expected fail for unconfigured repo, got pass: %s", result.Detail)
	}
}

// TestInLucosConfigy_FailsForDuplicate verifies the convention fails for a
// repo that appears under more than one configy type.
func TestInLucosConfigy_FailsForDuplicate(t *testing.T) {
	repo := RepoContext{Name: "lucas42/lucos_both", Type: RepoTypeDuplicate}
	result := checkInLucosConfigy(repo)
	if result.Pass {
		t.Errorf("expected fail for duplicate repo, got pass: %s", result.Detail)
	}
}
