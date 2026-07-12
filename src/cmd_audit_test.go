package main

import (
	"testing"
	"time"
)

// TestAuditDryRunMaxWait_Default verifies that with AUDIT_DRYRUN_MAX_WAIT
// unset, auditDryRunMaxWait falls back to auditDryRunMaxWaitDefault.
func TestAuditDryRunMaxWait_Default(t *testing.T) {
	t.Setenv(auditDryRunMaxWaitEnvVar, "")
	got := auditDryRunMaxWait()
	if got != auditDryRunMaxWaitDefault {
		t.Errorf("expected default %s, got %s", auditDryRunMaxWaitDefault, got)
	}
}

// TestAuditDryRunMaxWait_Override verifies a valid AUDIT_DRYRUN_MAX_WAIT
// duration string overrides the default.
func TestAuditDryRunMaxWait_Override(t *testing.T) {
	t.Setenv(auditDryRunMaxWaitEnvVar, "45m")
	got := auditDryRunMaxWait()
	want := 45 * time.Minute
	if got != want {
		t.Errorf("expected %s, got %s", want, got)
	}
}

// TestAuditDryRunMaxWait_InvalidValueFallsBackToDefault verifies an
// unparseable or non-positive AUDIT_DRYRUN_MAX_WAIT falls back to the
// default rather than erroring or blocking indefinitely.
func TestAuditDryRunMaxWait_InvalidValueFallsBackToDefault(t *testing.T) {
	for _, v := range []string{"not-a-duration", "0m", "-5m"} {
		t.Run(v, func(t *testing.T) {
			t.Setenv(auditDryRunMaxWaitEnvVar, v)
			got := auditDryRunMaxWait()
			if got != auditDryRunMaxWaitDefault {
				t.Errorf("value %q: expected fallback to default %s, got %s", v, auditDryRunMaxWaitDefault, got)
			}
		})
	}
}
