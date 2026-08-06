package download

import (
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"ytreconstruct/internal/testexec"
)

func TestHelperProcess(t *testing.T) { testexec.HelperProcess(t) }

func TestVideoID(t *testing.T) {
	cases := []struct {
		url     string
		wantID  string
		wantErr bool
	}{
		{"https://youtu.be/BL8TfsLk3WM", "BL8TfsLk3WM", false},
		{"https://youtu.be/BL8TfsLk3WM?si=AoAIUHdCsVCBlaIy", "BL8TfsLk3WM", false},
		{"https://www.youtube.com/watch?v=BL8TfsLk3WM", "BL8TfsLk3WM", false},
		{"https://www.youtube.com/watch?v=BL8TfsLk3WM&t=42s", "BL8TfsLk3WM", false},
		{"https://www.youtube.com/shorts/BL8TfsLk3WM", "BL8TfsLk3WM", false},
		{"https://www.youtube.com/embed/BL8TfsLk3WM", "BL8TfsLk3WM", false},
		{"https://www.youtube.com/live/BL8TfsLk3WM", "BL8TfsLk3WM", false},
		{"https://m.youtube.com/watch?v=BL8TfsLk3WM", "BL8TfsLk3WM", false},
		{"not a url", "", true},
		{"https://youtu.be/", "", true},
		{"https://youtu.be/TOOSHORT", "", true},
		{"https://youtu.be/thisidistoolongbye", "", true},
		{"https://www.youtube.com/playlist?list=PL123", "", true},
		{"https://example.com/watch?v=BL8TfsLk3WM", "", true},
	}
	for _, c := range cases {
		id, err := VideoID(c.url)
		if c.wantErr {
			if err == nil {
				t.Errorf("VideoID(%q): expected error, got id %q", c.url, id)
			}
			continue
		}
		if err != nil {
			t.Errorf("VideoID(%q): unexpected error: %v", c.url, err)
			continue
		}
		if id != c.wantID {
			t.Errorf("VideoID(%q) = %q, want %q", c.url, id, c.wantID)
		}
	}
}

func TestFileID(t *testing.T) {
	cases := []struct{ in, want string }{
		{"testsrc.mp4", "testsrc"},
		{"my video (final).mkv", "my-video-final"},
		{"/path/to/cool.webm", "cool"},
		{"!!!!", "video"},
	}
	for _, c := range cases {
		if got := FileID(c.in); got != c.want {
			t.Errorf("FileID(%q) = %q, want %q", c.in, got, c.want)
		}
	}
}

func TestRunMissingBinary(t *testing.T) {
	oldLook := lookPath
	lookPath = func(string) (string, error) { return "", errors.New("not found") }
	defer func() { lookPath = oldLook }()

	err := Run(Options{URL: "https://youtu.be/BL8TfsLk3WM", WorkDir: t.TempDir()})
	if err == nil {
		t.Fatal("expected error for missing binaries")
	}
	if !strings.Contains(err.Error(), `"yt-dlp"`) || !strings.Contains(err.Error(), "not found") {
		t.Errorf("error should name the missing binary, got: %v", err)
	}
}

func TestRunArgumentValidation(t *testing.T) {
	if err := Run(Options{WorkDir: t.TempDir()}); err == nil {
		t.Error("expected error when neither URL nor File given")
	}
	if err := Run(Options{URL: "https://youtu.be/BL8TfsLk3WM", File: "x.mp4", WorkDir: t.TempDir()}); err == nil {
		t.Error("expected error when both URL and File given")
	}
}

// fakeDownloadCmd returns a command fake; the side-effect closure simulates
// yt-dlp/ffmpeg producing their output files.
func fakeDownloadCmd(t *testing.T, dir string, calls *[]string) func(string, ...string) *exec.Cmd {
	return func(name string, args ...string) *exec.Cmd {
		*calls = append(*calls, strings.Join(append([]string{name}, args...), " "))
		switch name {
		case "yt-dlp":
			// yt-dlp wrote video.mp4 (from the -o template).
			for i, a := range args {
				if a == "-o" && i+1 < len(args) {
					out := strings.ReplaceAll(args[i+1], "%(ext)s", "mp4")
					if err := os.WriteFile(out, []byte("fake video"), 0o644); err != nil {
						t.Fatalf("fake yt-dlp: %v", err)
					}
				}
			}
		case "ffmpeg":
			// ffmpeg wrote the audio wav (last arg).
			if err := os.WriteFile(args[len(args)-1], []byte("fake audio"), 0o644); err != nil {
				t.Fatalf("fake ffmpeg: %v", err)
			}
		}
		return testexec.Command("", "", 0)(name, args...)
	}
}

func TestRunDownloadFlow(t *testing.T) {
	work := t.TempDir()
	var calls []string
	oldCmd := command
	command = fakeDownloadCmd(t, work, &calls)
	defer func() { command = oldCmd }()

	err := Run(Options{URL: "https://youtu.be/BL8TfsLk3WM", WorkDir: work})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}

	video := filepath.Join(work, "BL8TfsLk3WM", "video.mp4")
	audio := filepath.Join(work, "BL8TfsLk3WM", "audio.wav")
	for _, p := range []string{video, audio} {
		if _, err := os.Stat(p); err != nil {
			t.Errorf("expected %s to exist: %v", p, err)
		}
	}
	// yt-dlp must have been called with the URL and an output template.
	joined := strings.Join(calls, "\n")
	if !strings.Contains(joined, "yt-dlp") || !strings.Contains(joined, "BL8TfsLk3WM") {
		t.Errorf("yt-dlp invocation missing URL:\n%s", joined)
	}
	if !strings.Contains(joined, "ffmpeg") || !strings.Contains(joined, "audio.wav") {
		t.Errorf("ffmpeg invocation missing audio output:\n%s", joined)
	}
	// Audio extraction must target 16 kHz mono.
	if !strings.Contains(joined, "-ar 16000") || !strings.Contains(joined, "-ac 1") {
		t.Errorf("audio extraction must force 16 kHz mono:\n%s", joined)
	}
}

func TestRunYtDlpFailure(t *testing.T) {
	work := t.TempDir()
	var calls []string
	oldCmd := command
	command = func(name string, args ...string) *exec.Cmd {
		calls = append(calls, name)
		if name == "yt-dlp" {
			return testexec.Command("", "video unavailable", 1)(name, args...)
		}
		return fakeDownloadCmd(t, work, &calls)(name, args...)
	}
	defer func() { command = oldCmd }()

	err := Run(Options{URL: "https://youtu.be/BL8TfsLk3WM", WorkDir: work})
	if err == nil {
		t.Fatal("expected error when yt-dlp fails")
	}
	if !strings.Contains(err.Error(), "yt-dlp") {
		t.Errorf("error should mention yt-dlp, got: %v", err)
	}
	if _, statErr := os.Stat(filepath.Join(work, "BL8TfsLk3WM", "video.mp4")); statErr == nil {
		t.Error("video.mp4 should not exist after a failed download")
	}
}

func TestRunLocalFile(t *testing.T) {
	work := t.TempDir()
	src := filepath.Join(t.TempDir(), "sample clip.mp4")
	if err := os.WriteFile(src, []byte("video bytes"), 0o644); err != nil {
		t.Fatal(err)
	}

	var calls []string
	oldCmd := command
	command = func(name string, args ...string) *exec.Cmd {
		calls = append(calls, name)
		if name == "yt-dlp" {
			t.Error("yt-dlp must not be invoked for a local file")
		}
		if name == "ffmpeg" {
			if err := os.WriteFile(args[len(args)-1], []byte("audio"), 0o644); err != nil {
				t.Fatal(err)
			}
		}
		return testexec.Command("", "", 0)(name, args...)
	}
	defer func() { command = oldCmd }()

	err := Run(Options{File: src, WorkDir: work})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	video := filepath.Join(work, "sample-clip", "video.mp4")
	if b, _ := os.ReadFile(video); string(b) != "video bytes" {
		t.Errorf("video.mp4 content = %q, want the copied source bytes", b)
	}
}

func TestRunResumeSkipsExisting(t *testing.T) {
	work := t.TempDir()
	dir := filepath.Join(work, "BL8TfsLk3WM")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	for _, f := range []string{"video.mp4", "audio.wav"} {
		if err := os.WriteFile(filepath.Join(dir, f), []byte("x"), 0o644); err != nil {
			t.Fatal(err)
		}
	}

	var calls []string
	oldCmd := command
	command = func(name string, args ...string) *exec.Cmd {
		calls = append(calls, name)
		return testexec.Command("", "", 0)(name, args...)
	}
	defer func() { command = oldCmd }()

	if err := Run(Options{URL: "https://youtu.be/BL8TfsLk3WM", WorkDir: work}); err != nil {
		t.Fatalf("Run: %v", err)
	}
	if len(calls) != 0 {
		t.Errorf("resume run should invoke no commands, got: %v", calls)
	}
}
