// Package testexec provides a hermetic fake for exec.Command, for tests.
// No real external binary or network is ever invoked: the fake
// re-executes the current test binary with -test.run=TestHelperProcess,
// which each package's test file wires to testexec.HelperProcess.
package testexec

import (
	"os"
	"os/exec"
	"strconv"
	"testing"
)

const (
	envHelper = "YTRE_TEST_HELPER"
	envStdout = "YTRE_TEST_STDOUT"
	envStderr = "YTRE_TEST_STDERR"
	envCode   = "YTRE_TEST_CODE"
)

// Command returns a fake exec.Command function: every invocation emits the
// given stdout/stderr and exits with code. Side effects (e.g. "yt-dlp
// produced video.mp4") belong in the test's own closure around this fake.
func Command(stdout, stderr string, code int) func(name string, args ...string) *exec.Cmd {
	return func(name string, args ...string) *exec.Cmd {
		all := append([]string{name}, args...)
		cmd := exec.Command(os.Args[0], "-test.run=TestHelperProcess", "--")
		cmd.Args = append(cmd.Args, all...)
		cmd.Env = append(os.Environ(),
			envHelper+"=1",
			envStdout+"="+stdout,
			envStderr+"="+stderr,
			envCode+"="+strconv.Itoa(code),
		)
		return cmd
	}
}

// HelperProcess is the body of the TestHelperProcess test each package
// defines. It replays the canned output and exit code from the env vars
// and exits the child process. As a normal test it is a no-op.
func HelperProcess(t *testing.T) {
	if os.Getenv(envHelper) != "1" {
		return
	}
	os.Stdout.WriteString(os.Getenv(envStdout))
	os.Stderr.WriteString(os.Getenv(envStderr))
	code, _ := strconv.Atoi(os.Getenv(envCode))
	os.Exit(code)
}
