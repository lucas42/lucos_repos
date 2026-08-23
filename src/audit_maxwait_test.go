package main

import (
	"testing"
	"time"
)

// TestAuditSweepMaxWait_Default verifies that with AUDIT_SWEEP_MAX_WAIT
// unset, auditSweepMaxWait falls back to auditSweepMaxWaitDefault.
func TestAuditSweepMaxWait_Default(t *testing.T) {
	t.Setenv(auditSweepMaxWaitEnvVar, "")
	got := auditSweepMaxWait()
	if got != auditSweepMaxWaitDefault {
		t.Errorf("expected default %s, got %s", auditSweepMaxWaitDefault, got)
	}
}

// TestAuditSweepMaxWait_Override verifies a valid AUDIT_SWEEP_MAX_WAIT
// duration string overrides the default.
func TestAuditSweepMaxWait_Override(t *testing.T) {
	t.Setenv(auditSweepMaxWaitEnvVar, "45m")
	got := auditSweepMaxWait()
	want := 45 * time.Minute
	if got != want {
		t.Errorf("expected %s, got %s", want, got)
	}
}

// TestAuditSweepMaxWait_InvalidValueFallsBackToDefault verifies an
// unparseable or non-positive AUDIT_SWEEP_MAX_WAIT falls back to the
// default rather than erroring or blocking indefinitely.
func TestAuditSweepMaxWait_InvalidValueFallsBackToDefault(t *testing.T) {
	for _, v := range []string{"not-a-duration", "0m", "-5m"} {
		t.Run(v, func(t *testing.T) {
			t.Setenv(auditSweepMaxWaitEnvVar, v)
			got := auditSweepMaxWait()
			if got != auditSweepMaxWaitDefault {
				t.Errorf("value %q: expected fallback to default %s, got %s", v, auditSweepMaxWaitDefault, got)
			}
		})
	}
}
