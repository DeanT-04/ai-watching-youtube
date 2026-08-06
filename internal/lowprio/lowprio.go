// Package lowprio makes child processes play nice with the user's machine:
// on Windows they run at BELOW_NORMAL priority so interactive apps stay
// responsive even while ytreconstruct grinds through a video.
package lowprio

import "os/exec"

// Command is exec.Command with the child deprioritized.
func Command(name string, args ...string) *exec.Cmd {
	cmd := exec.Command(name, args...)
	apply(cmd)
	return cmd
}
