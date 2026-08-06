//go:build windows

package lowprio

import (
	"os/exec"
	"syscall"
)

// belowNormalPriority is the CREATE_PROCESS creation flag that starts the
// child at BELOW_NORMAL priority, so the user's interactive apps preempt
// the pipeline (Win32: 0x00004000).
const belowNormalPriority = 0x00004000

// apply sets the child's process priority class below normal.
func apply(cmd *exec.Cmd) {
	cmd.SysProcAttr = &syscall.SysProcAttr{
		CreationFlags: belowNormalPriority,
	}
}
