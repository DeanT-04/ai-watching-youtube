package transcribe

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"ytreconstruct/internal/chunk"
	"ytreconstruct/internal/testexec"
)

func TestHelperProcess(t *testing.T) { testexec.HelperProcess(t) }

func TestFormatTimestamp(t *testing.T) {
	cases := []struct {
		seconds float64
		want    string
	}{
		{0.0, "00:00:00.000"},
		{1.5, "00:00:01.500"},
		{83.456, "00:01:23.456"},
		{3723.999, "01:02:03.999"},
		{3600.0, "01:00:00.000"},
		{-5.0, "00:00:00.000"}, // negative clamped to 0
	}
	for _, c := range cases {
		if got := formatTimestamp(c.seconds); got != c.want {
			t.Errorf("formatTimestamp(%v) = %q, want %q", c.seconds, got, c.want)
		}
	}
}

func TestParseWhisperJSON(t *testing.T) {
	data := `{
  "transcription": [
    { "timestamps": {"from": "00:00:01,500", "to": "00:00:02,100"},
      "offsets": {"from": 1.5, "to": 2.1},
      "text": " Hello world" },
    { "timestamps": {"from": "00:00:02,100", "to": "00:00:03,000"},
      "offsets": {"from": 2.1, "to": 3.0},
      "text": "   " },
    { "timestamps": {"from": "00:00:03,000", "to": "00:00:04,000"},
      "offsets": {"from": 3, "to": 4},
      "text": "ints parsed fine" },
    { "offsets": {"from": 4.5, "to": 5.5},
      "text": "" }
  ]
}`
	segs, err := parseWhisperJSON([]byte(data))
	if err != nil {
		t.Fatalf("parseWhisperJSON: %v", err)
	}
	want := []segment{
		{From: 1.5, To: 2.1, Text: "Hello world"},
		{From: 3, To: 4, Text: "ints parsed fine"}, // integer offsets handled
	}
	if len(segs) != len(want) {
		t.Fatalf("got %d segments, want %d: %+v", len(segs), len(want), segs)
	}
	for i := range want {
		if segs[i] != want[i] {
			t.Errorf("segment %d = %+v, want %+v", i, segs[i], want[i])
		}
	}
}

func TestParseWhisperJSONInvalid(t *testing.T) {
	if _, err := parseWhisperJSON([]byte("not json")); err == nil {
		t.Error("expected error for invalid JSON")
	}
}

// TestTranscriptLineAlignment verifies the alignment logic: a segment at 1.5s
// in a chunk starting at 10s lands at 11.5s on the absolute timeline.
func TestTranscriptLineAlignment(t *testing.T) {
	got := transcriptLine(chunk.Chunk{ID: 2, Start: 10}, segment{From: 1.5, To: 2.1, Text: "Hello"})
	want := "[00:00:11.500 --> 00:00:12.100] Hello"
	if got != want {
		t.Errorf("transcriptLine = %q, want %q", got, want)
	}
}

// cannedJSON is the whisper-cli -oj output the fake writes for a chunk.
func cannedJSON(id int) string {
	return fmt.Sprintf(`{
  "transcription": [
    { "timestamps": {"from": "00:00:01,500", "to": "00:00:02,100"},
      "offsets": {"from": 1.5, "to": 2.1},
      "text": " Hello chunk %d" }
  ]
}`, id)
}

// fakeWhisper returns a fake command that records invocations and, for each
// whisper-cli call, writes the JSON file the real binary would produce at the
// -of prefix + ".json". jsonFor returns the JSON body for that chunk's id; a
// nil jsonFor simulates whisper-cli producing no output file.
func fakeWhisper(t *testing.T, code int, jsonFor func(id int) string) (func(string, ...string) *exec.Cmd, *int) {
	t.Helper()
	var (
		mu    sync.Mutex
		calls int
	)
	fn := func(name string, args ...string) *exec.Cmd {
		mu.Lock()
		calls++
		mu.Unlock()
		if name != "whisper-cli" {
			t.Errorf("unexpected command %q with args %v", name, args)
			return testexec.Command("", "", 0)(name, args...)
		}
		prefix := ""
		for i := 0; i+1 < len(args); i++ {
			if args[i] == "-of" {
				prefix = args[i+1]
				break
			}
		}
		if prefix == "" {
			t.Errorf("no -of flag in whisper-cli args: %v", args)
			return testexec.Command("", "", 0)(name, args...)
		}
		id := 0
		fmt.Sscanf(filepath.Base(prefix), "%04d", &id)
		if jsonFor != nil {
			if err := os.WriteFile(prefix+".json", []byte(jsonFor(id)), 0o644); err != nil {
				t.Errorf("fake whisper write %s: %v", prefix+".json", err)
			}
		}
		return testexec.Command("", "", code)(name, args...)
	}
	return fn, &calls
}

// setupWorkDir creates work/<videoID> with chunks.json and dummy audio slices.
func setupWorkDir(t *testing.T, work, videoID string, chunks []chunk.Chunk) string {
	t.Helper()
	dir := filepath.Join(work, videoID)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	data, err := json.Marshal(chunk.ChunkList{VideoID: videoID, Source: "video.mp4", Chunks: chunks})
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "chunks.json"), data, 0o644); err != nil {
		t.Fatal(err)
	}
	for _, c := range chunks {
		p := filepath.Join(dir, c.Audio)
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte("fake audio"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	return dir
}

func writeModel(t *testing.T, work string) string {
	t.Helper()
	model := filepath.Join(work, "ggml-model.bin")
	if err := os.WriteFile(model, []byte("model"), 0o644); err != nil {
		t.Fatal(err)
	}
	return model
}

func TestRunEndToEnd(t *testing.T) {
	work := t.TempDir()
	model := writeModel(t, work)
	chunks := []chunk.Chunk{
		{ID: 1, Start: 0, End: 5, Audio: "chunks_raw/0001/audio.wav"},
		{ID: 2, Start: 10, End: 15, Audio: "chunks_raw/0002/audio.wav"},
	}
	dir := setupWorkDir(t, work, "abc123", chunks)

	fake, calls := fakeWhisper(t, 0, cannedJSON)
	old := command
	command = fake
	defer func() { command = old }()

	if err := Run(Options{VideoID: "abc123", WorkDir: work, Model: model, Jobs: 2}); err != nil {
		t.Fatalf("Run: %v", err)
	}
	if *calls != 2 {
		t.Errorf("whisper-cli invoked %d times, want 2", *calls)
	}

	// Raw whisper JSON kept as provenance.
	for _, id := range []int{1, 2} {
		if _, err := os.Stat(filepath.Join(dir, "transcripts", fmt.Sprintf("%04d.json", id))); err != nil {
			t.Errorf("provenance json %04d missing: %v", id, err)
		}
	}

	// Chunk 1 starts at 0 → slice-relative offsets are absolute.
	one, err := os.ReadFile(filepath.Join(dir, "transcripts", "0001.txt"))
	if err != nil {
		t.Fatal(err)
	}
	if got, want := string(one), "[00:00:01.500 --> 00:00:02.100] Hello chunk 1\n"; got != want {
		t.Errorf("0001.txt = %q, want %q", got, want)
	}

	// Chunk 2 starts at 10 → every line shifted +10s.
	two, err := os.ReadFile(filepath.Join(dir, "transcripts", "0002.txt"))
	if err != nil {
		t.Fatal(err)
	}
	if got, want := string(two), "[00:00:11.500 --> 00:00:12.100] Hello chunk 2\n"; got != want {
		t.Errorf("0002.txt = %q, want %q", got, want)
	}
}

func TestRunMissingChunksJSON(t *testing.T) {
	work := t.TempDir()
	model := writeModel(t, work)
	fake, calls := fakeWhisper(t, 0, cannedJSON)
	old := command
	command = fake
	defer func() { command = old }()

	err := Run(Options{VideoID: "abc", WorkDir: work, Model: model})
	if err == nil || !strings.Contains(err.Error(), "chunks.json") ||
		!strings.Contains(err.Error(), "ytreconstruct chunk") {
		t.Errorf("expected chunks.json error telling user to run chunk first, got: %v", err)
	}
	if *calls != 0 {
		t.Errorf("no whisper-cli should run, got %d calls", *calls)
	}
}

func TestRunMissingModel(t *testing.T) {
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

func TestRunWhisperFailure(t *testing.T) {
	work := t.TempDir()
	model := writeModel(t, work)
	chunks := []chunk.Chunk{
		{ID: 1, Start: 0, End: 5, Audio: "chunks_raw/0001/audio.wav"},
		{ID: 2, Start: 10, End: 15, Audio: "chunks_raw/0002/audio.wav"},
	}
	setupWorkDir(t, work, "abc", chunks)

	fake, _ := fakeWhisper(t, 1, cannedJSON) // whisper-cli exits non-zero
	old := command
	command = fake
	defer func() { command = old }()

	err := Run(Options{VideoID: "abc", WorkDir: work, Model: model, Jobs: 1})
	if err == nil {
		t.Fatal("expected error from failing whisper-cli")
	}
	if !strings.Contains(err.Error(), "whisper-cli") || !strings.Contains(err.Error(), "chunk 0001") {
		t.Errorf("error should name whisper-cli and the chunk, got: %v", err)
	}
}

func TestRunWhisperNoJSON(t *testing.T) {
	work := t.TempDir()
	model := writeModel(t, work)
	chunks := []chunk.Chunk{{ID: 1, Start: 0, End: 5, Audio: "chunks_raw/0001/audio.wav"}}
	setupWorkDir(t, work, "abc", chunks)

	fake, _ := fakeWhisper(t, 0, nil) // exit 0 but no JSON produced
	old := command
	command = fake
	defer func() { command = old }()

	err := Run(Options{VideoID: "abc", WorkDir: work, Model: model, Jobs: 1})
	if err == nil || !strings.Contains(err.Error(), "no JSON") || !strings.Contains(err.Error(), "chunk 0001") {
		t.Errorf("expected missing-JSON error naming the chunk, got: %v", err)
	}
}

func TestRunResume(t *testing.T) {
	work := t.TempDir()
	model := writeModel(t, work)
	chunks := []chunk.Chunk{
		{ID: 1, Start: 0, End: 5, Audio: "chunks_raw/0001/audio.wav"},
		{ID: 2, Start: 10, End: 15, Audio: "chunks_raw/0002/audio.wav"},
	}
	dir := setupWorkDir(t, work, "abc", chunks)
	tdir := filepath.Join(dir, "transcripts")
	if err := os.MkdirAll(tdir, 0o755); err != nil {
		t.Fatal(err)
	}
	for _, id := range []int{1, 2} {
		if err := os.WriteFile(filepath.Join(tdir, fmt.Sprintf("%04d.txt", id)), []byte("old"), 0o644); err != nil {
			t.Fatal(err)
		}
	}

	fake, calls := fakeWhisper(t, 0, cannedJSON)
	old := command
	command = fake
	defer func() { command = old }()

	out := captureStdout(t, func() {
		if err := Run(Options{VideoID: "abc", WorkDir: work, Model: model}); err != nil {
			t.Errorf("Run: %v", err)
		}
	})
	if *calls != 0 {
		t.Errorf("resume should not invoke whisper-cli, got %d calls", *calls)
	}
	if !strings.Contains(out, "all 2 transcripts already present, skipping") {
		t.Errorf("expected all-skipped message, stdout=%q", out)
	}
}

func TestRunConcurrent(t *testing.T) {
	work := t.TempDir()
	model := writeModel(t, work)
	var chunks []chunk.Chunk
	for i := 1; i <= 5; i++ {
		start := float64((i - 1) * 10)
		chunks = append(chunks, chunk.Chunk{
			ID: i, Start: start, End: start + 10,
			Audio: fmt.Sprintf("chunks_raw/%04d/audio.wav", i),
		})
	}
	dir := setupWorkDir(t, work, "abc", chunks)

	fake, calls := fakeWhisper(t, 0, cannedJSON)
	old := command
	command = fake
	defer func() { command = old }()

	if err := Run(Options{VideoID: "abc", WorkDir: work, Model: model, Jobs: 3}); err != nil {
		t.Fatalf("Run: %v", err)
	}
	if *calls != 5 {
		t.Errorf("whisper-cli invoked %d times, want 5", *calls)
	}
	for i := 1; i <= 5; i++ {
		p := filepath.Join(dir, "transcripts", fmt.Sprintf("%04d.txt", i))
		if _, err := os.Stat(p); err != nil {
			t.Errorf("missing transcript %s: %v", p, err)
		}
	}
}

// captureStdout runs fn with os.Stdout redirected to a pipe and returns what
// was written.
func captureStdout(t *testing.T, fn func()) string {
	t.Helper()
	old := os.Stdout
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	os.Stdout = w
	defer func() { os.Stdout = old }()
	fn()
	w.Close()
	out, err := io.ReadAll(r)
	r.Close()
	if err != nil {
		t.Fatal(err)
	}
	return string(out)
}
