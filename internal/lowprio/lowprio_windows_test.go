//go:build windows

package lowprio

import "testing"

// TestApplySetsBelowNormal verifies the child gets the BELOW_NORMAL
// priority creation flag (0x4000) — the mechanism that keeps the user's
// machine responsive while the pipeline runs.
func TestApplySetsBelowNormal(t *testing.T) {
	cmd := Command("ffmpeg")
	if cmd.SysProcAttr == nil {
		t.Fatal("SysProcAttr not set")
	}
	if cmd.SysProcAttr.CreationFlags&belowNormalPriority == 0 {
		t.Errorf("CreationFlags = %#x, want BELOW_NORMAL (0x%x) set",
			cmd.SysProcAttr.CreationFlags, belowNormalPriority)
	}
}
