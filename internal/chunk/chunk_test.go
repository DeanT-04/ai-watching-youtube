package chunk

import (
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"ytreconstruct/internal/testexec"
)

func TestHelperProcess(t *testing.T) { testexec.HelperProcess(t) }

func TestSceneBoundaries(t *testing.T) {
	old := command
	scd := "[Parsed_scdet_0 @ 0x1] lavfi.scd.score: 0.50\n" +
		"[Parsed_scdet_0 @ 0x1] lavfi.scd.time: 0.000000\n" +
		"[Parsed_scdet_0 @ 0x1] lavfi.scd.time: 3.700000\n" +
		"[Parsed_scdet_0 @ 0x1] lavfi.scd.time: 8.123456\n"
	command = func(name string, args ...string) *exec.Cmd {
		return testexec.Command("", scd, 0)(name, args...)
	}
	defer func() { command = old }()

	got, err := sceneBoundaries("video.mp4", 0.4)
	if err != nil {
		t.Fatalf("sceneBoundaries: %v", err)
	}
	want := []float64{0.0, 3.7, 8.123456}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("boundaries = %v, want %v", got, want)
	}
}

func TestSceneBoundariesFailure(t *testing.T) {
	old := command
	command = func(name string, args ...string) *exec.Cmd {
		return testexec.Command("", "", 1)(name, args...)
	}
	defer func() { command = old }()

	_, err := sceneBoundaries("video.mp4", 0.4)
	if err == nil || !strings.Contains(err.Error(), "scene detection") {
		t.Errorf("expected scene detection error, got: %v", err)
	}
}

func TestBuildChunks(t *testing.T) {
	cases := []struct {
		name       string
		boundaries []float64
		total      float64
		want       [][2]float64 // (start, end) pairs
	}{
		{"basic", []float64{0, 2, 5}, 7, [][2]float64{{0, 2}, {2, 5}, {5, 7}}},
		{"single scene", []float64{0}, 10, [][2]float64{{0, 10}}},
		{"out of range dropped", []float64{0, 3, 99}, 5, [][2]float64{{0, 3}, {3, 5}}},
		{"duplicates collapsed", []float64{0, 0, 2, 2, 4}, 5, [][2]float64{{0, 2}, {2, 4}, {4, 5}}},
	}
	for _, c := range cases {
		chunks := buildChunks(c.boundaries, c.total)
		if len(chunks) != len(c.want) {
			t.Errorf("%s: got %d chunks, want %d", c.name, len(chunks), len(c.want))
			continue
		}
		for i, w := range c.want {
			if chunks[i].Start != w[0] || chunks[i].End != w[1] {
				t.Errorf("%s: chunk %d = [%v,%v], want [%v,%v]", c.name, i, chunks[i].Start, chunks[i].End, w[0], w[1])
			}
			if chunks[i].ID != i+1 {
				t.Errorf("%s: chunk %d id = %d, want %d", c.name, i, chunks[i].ID, i+1)
			}
		}
	}
}

// fakeChunkCmd fakes ffmpeg/ffprobe: scene detection emits scd lines on
// stderr, ffprobe emits a duration on stdout, and frame/audio extraction
// creates their output files.
func fakeChunkCmd(t *testing.T, scdLines, duration string) func(string, ...string) *exec.Cmd {
	return func(name string, args ...string) *exec.Cmd {
		switch name {
		case "ffprobe":
			return testexec.Command(duration, "", 0)(name, args...)
		case "ffmpeg":
			for _, a := range args {
				if strings.HasPrefix(a, "scdet=") {
					return testexec.Command("", scdLines, 0)(name, args...)
				}
			}
			// Extraction call: last arg is the output file.
			out := args[len(args)-1]
			if out == "-" { // the -f null - detection call must not create a file
				return testexec.Command("", "", 0)(name, args...)
			}
			if err := os.WriteFile(out, []byte("out"), 0o644); err != nil {
				t.Fatalf("fake ffmpeg write %s: %v", out, err)
			}
			return testexec.Command("", "", 0)(name, args...)
		}
		t.Fatalf("unexpected command %q", name)
		return nil
	}
}

func TestRunFullFlow(t *testing.T) {
	work := t.TempDir()
	dir := filepath.Join(work, "abc123")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	for _, f := range []string{"video.mp4", "audio.wav"} {
		if err := os.WriteFile(filepath.Join(dir, f), []byte("x"), 0o644); err != nil {
			t.Fatal(err)
		}
	}

	scd := "[Parsed_scdet_0 @ 0x1] lavfi.scd.time: 0.000000\n" +
		"[Parsed_scdet_0 @ 0x1] lavfi.scd.time: 2.500000\n" +
		"[Parsed_scdet_0 @ 0x1] lavfi.scd.time: 5.000000\n"

	old := command
	command = fakeChunkCmd(t, scd, "7.000000\n")
	defer func() { command = old }()

	if err := Run(Options{VideoID: "abc123", WorkDir: work, SceneThreshold: 0.4, Jobs: 2}); err != nil {
		t.Fatalf("Run: %v", err)
	}

	// chunks.json must be valid and ordered.
	raw, err := os.ReadFile(filepath.Join(dir, "chunks.json"))
	if err != nil {
		t.Fatal(err)
	}
	var list ChunkList
	if err := json.Unmarshal(raw, &list); err != nil {
		t.Fatalf("chunks.json is not valid JSON: %v", err)
	}
	if list.VideoID != "abc123" || list.Source != "video.mp4" {
		t.Errorf("list header = %+v", list)
	}
	if len(list.Chunks) != 3 {
		t.Fatalf("got %d chunks, want 3", len(list.Chunks))
	}
	wantBounds := [][2]float64{{0, 2.5}, {2.5, 5}, {5, 7}}
	for i, w := range wantBounds {
		if list.Chunks[i].Start != w[0] || list.Chunks[i].End != w[1] {
			t.Errorf("chunk %d = [%v,%v], want [%v,%v]", i, list.Chunks[i].Start, list.Chunks[i].End, w[0], w[1])
		}
		wantDir := filepath.ToSlash(filepath.Join("chunks_raw", fmt.Sprintf("%04d", i+1)))
		if list.Chunks[i].Frame != filepath.ToSlash(filepath.Join(wantDir, "frame.png")) {
			t.Errorf("chunk %d frame path = %q, want %q", i, list.Chunks[i].Frame, filepath.ToSlash(filepath.Join(wantDir, "frame.png")))
		}
		if list.Chunks[i].Audio != filepath.ToSlash(filepath.Join(wantDir, "audio.wav")) {
			t.Errorf("chunk %d audio path = %q, want %q", i, list.Chunks[i].Audio, filepath.ToSlash(filepath.Join(wantDir, "audio.wav")))
		}
	}

	// Every referenced file must exist.
	for _, c := range list.Chunks {
		for _, rel := range []string{c.Frame, c.Audio} {
			if _, err := os.Stat(filepath.Join(dir, rel)); err != nil {
				t.Errorf("missing %s: %v", rel, err)
			}
		}
	}
}

func TestRunMissingInputs(t *testing.T) {
	work := t.TempDir()
	old := command
	command = testexec.Command("", "", 0)
	defer func() { command = old }()

	err := Run(Options{VideoID: "nope", WorkDir: work, SceneThreshold: 0.4})
	if err == nil || !strings.Contains(err.Error(), "video.mp4") {
		t.Errorf("expected missing video error, got: %v", err)
	}
}

func TestRunBadThreshold(t *testing.T) {
	for _, th := range []float64{0, 1.5} {
		if err := Run(Options{VideoID: "x", WorkDir: t.TempDir(), SceneThreshold: th}); err == nil {
			t.Errorf("threshold %v: expected error", th)
		}
	}
}

func TestRunResume(t *testing.T) {
	work := t.TempDir()
	dir := filepath.Join(work, "abc")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	for _, f := range []string{"video.mp4", "audio.wav", "chunks.json"} {
		if err := os.WriteFile(filepath.Join(dir, f), []byte("{}"), 0o644); err != nil {
			t.Fatal(err)
		}
	}

	var calls []string
	old := command
	command = func(name string, args ...string) *exec.Cmd {
		calls = append(calls, name)
		return testexec.Command("", "", 0)(name, args...)
	}
	defer func() { command = old }()

	if err := Run(Options{VideoID: "abc", WorkDir: work, SceneThreshold: 0.4}); err != nil {
		t.Fatalf("Run: %v", err)
	}
	if len(calls) != 0 {
		t.Errorf("resume run should not invoke commands, got %v", calls)
	}
}
