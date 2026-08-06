//go:build !windows

package lowprio

import "os/exec"

// apply is a no-op on non-Windows platforms (no portable nice in stdlib).
func apply(cmd *exec.Cmd) {}
