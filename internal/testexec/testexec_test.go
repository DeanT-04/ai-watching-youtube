package testexec

import (
	"errors"
	"os/exec"
	"testing"
)

func TestHelperProcess(t *testing.T) { HelperProcess(t) }

func TestCommandFakeEmitsAndExits(t *testing.T) {
	cmd := Command("hello\n", "", 0)("yt-dlp", "arg1")
	out, err := cmd.Output()
	if err != nil {
		t.Fatalf("fake command failed: %v", err)
	}
	if string(out) != "hello\n" {
		t.Fatalf("stdout = %q, want %q", out, "hello\n")
	}
}

func TestCommandFakeExitCode(t *testing.T) {
	cmd := Command("", "boom", 3)("yt-dlp")
	_, err := cmd.Output()
	if err == nil {
		t.Fatal("expected ExitError for code 3, got nil")
	}
	var ee *exec.ExitError
	if !errors.As(err, &ee) {
		t.Fatalf("error = %v, want *exec.ExitError", err)
	}
	if ee.ExitCode() != 3 {
		t.Fatalf("exit code = %d, want 3", ee.ExitCode())
	}
}
