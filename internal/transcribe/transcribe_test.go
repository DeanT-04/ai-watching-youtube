package transcribe

import (
	"encoding/json"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"ytreconstruct/internal/chunk"
	"ytreconstruct/internal/testexec"
)

func TestHelperProcess(t *testing.T) { testexec.HelperProcess(t) }

// TestMain makes the startup binary check hermetic by default: Run tests
// must pass on machines with no whisper-cli installed (e.g. CI runners),
// so LookPath resolves every binary unless a test overrides it on purpose.
func TestMain(m *testing.M) {
	LookPath = testexec.LookPath
	os.Exit(m.Run())
}

func TestFormatTimestamp(t *testing.T) {
	cases := []struct {
		in   float64
		want string
	}{
		{0.0, "00:00:00.000"},
		{83.456, "00:01:23.456"},
		{3723.999, "01:02:03.999"},
		{3600.0, "01:00:00.000"},
		{-5, "00:00:00.000"},
	}
	for _, c := range cases {
		if got := formatTimestamp(c.in); got != c.want {
			t.Errorf("formatTimestamp(%v) = %q, want %q", c.in, got, c.want)
		}
	}
}

func TestParseWhisperJSON(t *testing.T) {
	// whisper.cpp reports offsets in milliseconds.
	data := []byte(`{"transcription":[
		{"offsets":{"from":1500,"to":2100},"text":" hello "},
		{"offsets":{"from":2100,"to":3000},"text":"   "},
		{"offsets":{"from":3000,"to":4000},"text":"world"}
	]}`)
	segs, err := parseWhisperJSON(data)
	if err != nil {
		t.Fatalf("parseWhisperJSON: %v", err)
	}
	if len(segs) != 2 {
		t.Fatalf("got %d segments, want 2 (whitespace-only skipped)", len(segs))
	}
	if segs[0].From != 1.5 || segs[0].Text != "hello" {
		t.Errorf("segment 0 = %+v", segs[0])
	}
	if segs[1].From != 3 || segs[1].To != 4 {
		t.Errorf("int offsets not converted to seconds: %+v", segs[1])
	}
}

func TestParseWhisperJSONInvalid(t *testing.T) {
	if _, err := parseWhisperJSON([]byte("not json")); err == nil {
		t.Error("expected error for invalid JSON")
	}
}

func TestTranscriptLine(t *testing.T) {
	got := transcriptLine(segment{From: 11.5, To: 12.1, Text: "hi"})
	want := "[00:00:11.500 --> 00:00:12.100] hi"
	if got != want {
		t.Errorf("line = %q, want %q", got, want)
	}
}

func TestSegmentsForRange(t *testing.T) {
	segs := []segment{
		{From: 0.5, To: 1.0, Text: "a"},
		{From: 2.5, To: 3.0, Text: "b"},
		{From: 3.5, To: 4.0, Text: "c"},
	}
	got := segmentsForRange(segs, 0, 2)
	if len(got) != 1 || got[0].Text != "a" {
		t.Errorf("range [0,2) = %+v", got)
	}
	got = segmentsForRange(segs, 2, 10)
	if len(got) != 2 || got[0].Text != "b" || got[1].Text != "c" {
		t.Errorf("range [2,10) = %+v", got)
	}
}

// setupWork writes a chunks.json with the given chunks plus an audio.wav
// and a dummy model file (the model must exist for Run to proceed).
func setupWork(t *testing.T, videoID string, chunks []chunk.Chunk) string {
	t.Helper()
	work := t.TempDir()
	dir := filepath.Join(work, videoID)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "audio.wav"), []byte("audio"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(work, "model.bin"), []byte("model"), 0o644); err != nil {
		t.Fatal(err)
	}
	list := chunk.ChunkList{VideoID: videoID, Source: "video.mp4", Chunks: chunks}
	data, err := json.Marshal(list)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "chunks.json"), data, 0o644); err != nil {
		t.Fatal(err)
	}
	return work
}

// fakeWhisper returns a command fake that writes <prefix>.json (from the
// -of arg) and records the invocation.
func fakeWhisper(t *testing.T, jsonBody string, calls *[]string) func(string, ...string) *exec.Cmd {
	return func(name string, args ...string) *exec.Cmd {
		*calls = append(*calls, strings.Join(append([]string{name}, args...), " "))
		if name == "whisper-cli" {
			for i, a := range args {
				if a == "-of" && i+1 < len(args) {
					if err := os.WriteFile(args[i+1]+".json", []byte(jsonBody), 0o644); err != nil {
						t.Fatalf("fake whisper write: %v", err)
					}
				}
			}
		}
		return testexec.Command("", "", 0)(name, args...)
	}
}

func TestRunEndToEnd(t *testing.T) {
	chunks := []chunk.Chunk{
		{ID: 1, Start: 0, End: 5, Frame: "chunks_raw/0001/frame.png", Audio: "chunks_raw/0001/audio.wav"},
		{ID: 2, Start: 5, End: 10, Frame: "chunks_raw/0002/frame.png", Audio: "chunks_raw/0002/audio.wav"},
	}
	work := setupWork(t, "vid", chunks)

	// Segments are already absolute on the timeline; offsets are ms.
	jsonBody := `{"transcription":[
		{"offsets":{"from":1500,"to":2000},"text":"first"},
		{"offsets":{"from":6500,"to":7000},"text":"second"},
		{"offsets":{"from":99000,"to":100000},"text":"later"}
	]}`
	var calls []string
	oldCmd := command
	command = fakeWhisper(t, jsonBody, &calls)
	defer func() { command = oldCmd }()

	if err := Run(Options{VideoID: "vid", WorkDir: work, Model: filepath.Join(work, "model.bin")}); err != nil {
		t.Fatalf("Run: %v", err)
	}

	// Exactly one whisper-cli invocation for the whole audio track.
	if len(calls) != 1 || !strings.Contains(calls[0], "audio.wav") {
		t.Fatalf("expected one full-audio whisper call, got: %v", calls)
	}
	// -l auto must be the default (whisper-cli's own default is 'en').
	if !strings.Contains(calls[0], "-l auto") {
		t.Errorf("expected -l auto default, got: %v", calls[0])
	}
	// full.json provenance kept.
	if _, err := os.Stat(filepath.Join(work, "vid", "transcripts", "full.json")); err != nil {
		t.Errorf("full.json missing: %v", err)
	}
	// Partitioning: chunk 1 gets "first", chunk 2 gets "second", "later" is dropped.
	c1, err := os.ReadFile(filepath.Join(work, "vid", "transcripts", "0001.txt"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(c1), "first") || strings.Contains(string(c1), "second") {
		t.Errorf("chunk 1 transcript = %q", c1)
	}
	c2, err := os.ReadFile(filepath.Join(work, "vid", "transcripts", "0002.txt"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(c2), "00:00:06.500") || !strings.Contains(string(c2), "second") {
		t.Errorf("chunk 2 transcript = %q", c2)
	}
}

func TestRunResume(t *testing.T) {
	chunks := []chunk.Chunk{
		{ID: 1, Start: 0, End: 5, Frame: "chunks_raw/0001/frame.png", Audio: "chunks_raw/0001/audio.wav"},
	}
	work := setupWork(t, "vid", chunks)
	transcriptsDir := filepath.Join(work, "vid", "transcripts")
	if err := os.MkdirAll(transcriptsDir, 0o755); err != nil {
		t.Fatal(err)
	}
	// full.json present + all chunk transcripts present → fully skipped.
	if err := os.WriteFile(filepath.Join(transcriptsDir, "full.json"), []byte("{}"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(transcriptsDir, "0001.txt"), []byte("done"), 0o644); err != nil {
		t.Fatal(err)
	}

	var calls []string
	oldCmd := command
	command = fakeWhisper(t, "{}", &calls)
	defer func() { command = oldCmd }()

	if err := Run(Options{VideoID: "vid", WorkDir: work, Model: filepath.Join(work, "model.bin")}); err != nil {
		t.Fatalf("Run: %v", err)
	}
	if len(calls) != 0 {
		t.Errorf("resume run should not invoke whisper-cli, got %d calls", len(calls))
	}
}

func TestRunRepartitionsWhenTranscriptMissing(t *testing.T) {
	chunks := []chunk.Chunk{
		{ID: 1, Start: 0, End: 5, Frame: "chunks_raw/0001/frame.png", Audio: "chunks_raw/0001/audio.wav"},
	}
	work := setupWork(t, "vid", chunks)
	transcriptsDir := filepath.Join(work, "vid", "transcripts")
	if err := os.MkdirAll(transcriptsDir, 0o755); err != nil {
		t.Fatal(err)
	}
	// full.json exists but the chunk transcript was deleted → re-partition,
	// no whisper re-run.
	jsonBody := `{"transcription":[{"offsets":{"from":1000,"to":2000},"text":"hi"}]}`
	if err := os.WriteFile(filepath.Join(transcriptsDir, "full.json"), []byte(jsonBody), 0o644); err != nil {
		t.Fatal(err)
	}

	var calls []string
	oldCmd := command
	command = fakeWhisper(t, "{}", &calls)
	defer func() { command = oldCmd }()

	if err := Run(Options{VideoID: "vid", WorkDir: work, Model: filepath.Join(work, "model.bin")}); err != nil {
		t.Fatalf("Run: %v", err)
	}
	if len(calls) != 0 {
		t.Errorf("partition-only run should not invoke whisper-cli")
	}
	b, err := os.ReadFile(filepath.Join(transcriptsDir, "0001.txt"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(b), "hi") {
		t.Errorf("repartitioned transcript = %q", b)
	}
}

func TestRunMissingChunksJSON(t *testing.T) {
	work := t.TempDir()
	if err := os.WriteFile(filepath.Join(work, "model.bin"), []byte("m"), 0o644); err != nil {
		t.Fatal(err)
	}
	err := Run(Options{VideoID: "nope", WorkDir: work, Model: filepath.Join(work, "model.bin")})
	if err == nil || !strings.Contains(err.Error(), "chunks.json") {
		t.Errorf("expected chunks.json error, got: %v", err)
	}
}

func TestRunMissingAudio(t *testing.T) {
	chunks := []chunk.Chunk{{ID: 1, Start: 0, End: 5, Frame: "f", Audio: "a"}}
	work := setupWork(t, "vid", chunks)
	if err := os.Remove(filepath.Join(work, "vid", "audio.wav")); err != nil {
		t.Fatal(err)
	}
	err := Run(Options{VideoID: "vid", WorkDir: work, Model: filepath.Join(work, "model.bin")})
	if err == nil || !strings.Contains(err.Error(), "audio.wav") {
		t.Errorf("expected audio.wav error, got: %v", err)
	}
}

func TestRunMissingModel(t *testing.T) {
	oldLook := LookPath
	LookPath = func(bin string) (string, error) { return bin, nil }
	defer func() { LookPath = oldLook }()

	work := t.TempDir()
	model := filepath.Join(work, "nope.bin")
	err := Run(Options{VideoID: "abc", WorkDir: work, Model: model})
	if err == nil || !strings.Contains(err.Error(), model) {
		t.Errorf("expected error mentioning the model path, got: %v", err)
	}
	if err == nil || !strings.Contains(err.Error(), "huggingface.co") {
		t.Errorf("error should point at a model download location, got: %v", err)
	}
}

func TestRunMissingWhisperBinary(t *testing.T) {
	oldLook := LookPath
	LookPath = func(string) (string, error) { return "", errors.New("not found") }
	defer func() { LookPath = oldLook }()

	err := Run(Options{VideoID: "abc", WorkDir: t.TempDir(), Model: filepath.Join(t.TempDir(), "m.bin")})
	if err == nil {
		t.Fatal("expected error for missing whisper-cli")
	}
	if !strings.Contains(err.Error(), "whisper-cli") {
		t.Errorf("error should name whisper-cli, got: %v", err)
	}
}

func TestRunWhisperFailure(t *testing.T) {
	chunks := []chunk.Chunk{{ID: 1, Start: 0, End: 5, Frame: "f", Audio: "a"}}
	work := setupWork(t, "vid", chunks)
	oldCmd := command
	command = func(name string, args ...string) *exec.Cmd {
		return testexec.Command("", "boom", 1)(name, args...)
	}
	defer func() { command = oldCmd }()

	err := Run(Options{VideoID: "vid", WorkDir: work, Model: filepath.Join(work, "model.bin")})
	if err == nil {
		t.Fatal("expected error when whisper-cli fails")
	}
	if !strings.Contains(err.Error(), "whisper-cli failed") {
		t.Errorf("error should report the whisper-cli failure, got: %v", err)
	}
}

func TestRunWhisperNoJSON(t *testing.T) {
	chunks := []chunk.Chunk{{ID: 1, Start: 0, End: 5, Frame: "f", Audio: "a"}}
	work := setupWork(t, "vid", chunks)
	oldCmd := command
	command = func(name string, args ...string) *exec.Cmd {
		return testexec.Command("", "", 0)(name, args...) // no file written
	}
	defer func() { command = oldCmd }()

	err := Run(Options{VideoID: "vid", WorkDir: work, Model: filepath.Join(work, "model.bin")})
	if err == nil || !strings.Contains(err.Error(), "produced no") {
		t.Errorf("expected no-JSON error, got: %v", err)
	}
}

func TestRunLanguageOverride(t *testing.T) {
	chunks := []chunk.Chunk{{ID: 1, Start: 0, End: 5, Frame: "f", Audio: "a"}}
	work := setupWork(t, "vid", chunks)
	var calls []string
	oldCmd := command
	command = fakeWhisper(t, `{"transcription":[]}`, &calls)
	defer func() { command = oldCmd }()

	if err := Run(Options{VideoID: "vid", WorkDir: work, Model: filepath.Join(work, "model.bin"), Language: "ja"}); err != nil {
		t.Fatalf("Run: %v", err)
	}
	if !strings.Contains(calls[0], "-l ja") {
		t.Errorf("language override not passed: %v", calls[0])
	}
}
