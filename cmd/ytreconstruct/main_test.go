package main

import (
	"os"
	"strings"
	"testing"

	"ytreconstruct/internal/chunk"
	"ytreconstruct/internal/download"
	"ytreconstruct/internal/store"
	"ytreconstruct/internal/testexec"
	"ytreconstruct/internal/transcribe"
)

// TestMain makes the CLI hermetic: subcommands invoke the packages' startup
// binary checks, which must not depend on ffmpeg/yt-dlp/whisper-cli being
// installed on the machine running the tests (e.g. CI runners).
func TestMain(m *testing.M) {
	// Subcommands invoke the packages' startup binary checks; fake them so
	// the CLI tests run without ffmpeg/yt-dlp/whisper-cli installed.
	chunk.LookPath = testexec.LookPath
	download.LookPath = testexec.LookPath
	store.LookPath = testexec.LookPath
	transcribe.LookPath = testexec.LookPath
	os.Exit(m.Run())
}

// execute runs the CLI with the given args, returning the error (nil on success).
func execute(args ...string) error {
	root := newRootCmd()
	root.SetArgs(args)
	return root.Execute()
}

func TestHelpRuns(t *testing.T) {
	if err := execute("--help"); err != nil {
		t.Fatalf("--help failed: %v", err)
	}
}

func TestAllRequiresURLOrFile(t *testing.T) {
	if err := execute("all"); err == nil {
		t.Error("all with no URL or --file should fail")
	}
	if err := execute("all", "https://youtu.be/BL8TfsLk3WM", "--file", "x.mp4"); err == nil {
		t.Error("all with both URL and --file should fail")
	}
}

func TestSubcommandsRequireVideoID(t *testing.T) {
	for _, sub := range []string{"chunk", "dedupe", "transcribe", "manifest"} {
		if err := execute(sub); err == nil {
			t.Errorf("%s with no video_id should fail", sub)
		}
	}
	for _, args := range [][]string{
		{"store", "pack"}, {"store", "verify"}, {"store", "dump"},
		{"store", "query"}, {"store", "frame"},
	} {
		if err := execute(args...); err == nil {
			t.Errorf("%v with no video_id should fail", args)
		}
	}
}

func TestStoreQueryRequiresGrep(t *testing.T) {
	err := execute("store", "query", "somevideo")
	if err == nil || !strings.Contains(err.Error(), "--grep") {
		t.Errorf("store query without --grep should fail, got %v", err)
	}
}

func TestUnknownCommandFails(t *testing.T) {
	err := execute("frobnicate")
	if err == nil {
		t.Fatal("unknown command should fail")
	}
	if !strings.Contains(err.Error(), "unknown command") {
		t.Errorf("error = %v, want 'unknown command'", err)
	}
}

// TestPersistentFlagsReachSubcommand guards against the stale-capture bug
// where --work-dir (etc.) was read at command-construction time and ignored
// at run time. The error message embeds the work-dir path, so if the flag
// propagates the temp dir appears in it.
func TestPersistentFlagsReachSubcommand(t *testing.T) {
	work := t.TempDir()
	err := execute("chunk", "somevideo", "--work-dir", work)
	if err == nil {
		t.Fatal("chunk on a missing video should fail")
	}
	if !strings.Contains(err.Error(), work) {
		t.Errorf("work-dir flag did not reach the package: error = %v (want path %s)", err, work)
	}
}
