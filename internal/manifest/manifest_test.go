package manifest

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"ytreconstruct/internal/chunk"
)

// seedWorkDir builds a fake work/<id>/ tree: chunks_deduped.json (or only
// chunks.json when dedupe=false), transcripts, and frame files.
func seedWorkDir(t *testing.T, base string, dedupe bool) string {
	t.Helper()
	dir := filepath.Join(base, "work", "vid123")
	if err := os.MkdirAll(filepath.Join(dir, "chunks_raw", "0001"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(dir, "chunks_raw", "0002"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(dir, "chunks_raw", "0003"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(dir, "transcripts"), 0o755); err != nil {
		t.Fatal(err)
	}
	for i := 1; i <= 3; i++ {
		sub := filepath.Join(dir, "chunks_raw", "0001")
		if i == 2 {
			sub = filepath.Join(dir, "chunks_raw", "0002")
		} else if i == 3 {
			sub = filepath.Join(dir, "chunks_raw", "0003")
		}
		if err := os.WriteFile(filepath.Join(sub, "frame.png"), []byte("png"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	// Transcripts: chunk 1 and 2 are merged later; 3 stands alone.
	write := func(rel, content string) {
		if err := os.WriteFile(filepath.Join(dir, rel), []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	write(filepath.Join("transcripts", "0001.txt"), "[00:00:01.000 --> 00:00:02.000] one\n")
	write(filepath.Join("transcripts", "0002.txt"), "[00:00:02.500 --> 00:00:03.000] two\n")
	write(filepath.Join("transcripts", "0003.txt"), "[00:00:05.000 --> 00:00:06.000] three\n")

	list := chunk.ChunkList{
		VideoID: "vid123",
		Source:  "video.mp4",
		Chunks: []chunk.Chunk{
			{ID: 1, Start: 0, End: 2.5, Frame: filepath.Join("chunks_raw", "0001", "frame.png"), Audio: filepath.Join("chunks_raw", "0001", "audio.wav")},
			{ID: 2, Start: 2.5, End: 5, Frame: filepath.Join("chunks_raw", "0002", "frame.png"), Audio: filepath.Join("chunks_raw", "0002", "audio.wav")},
			{ID: 3, Start: 5, End: 7, Frame: filepath.Join("chunks_raw", "0003", "frame.png"), Audio: filepath.Join("chunks_raw", "0003", "audio.wav")},
		},
	}
	if dedupe {
		// Merge chunks 1+2 into one deduped chunk.
		list.Chunks = []chunk.Chunk{
			{ID: 1, Start: 0, End: 5, Frame: filepath.Join("chunks_raw", "0001", "frame.png"), Audio: filepath.Join("chunks_raw", "0001", "audio.wav"), SourceIDs: []int{1, 2}},
			{ID: 2, Start: 5, End: 7, Frame: filepath.Join("chunks_raw", "0003", "frame.png"), Audio: filepath.Join("chunks_raw", "0003", "audio.wav")},
		}
	}
	data, err := json.Marshal(list)
	if err != nil {
		t.Fatal(err)
	}
	name := "chunks_deduped.json"
	if !dedupe {
		name = "chunks.json"
	}
	if err := os.WriteFile(filepath.Join(dir, name), data, 0o644); err != nil {
		t.Fatal(err)
	}
	return dir
}

func TestRunBuildsTree(t *testing.T) {
	base := t.TempDir()
	seedWorkDir(t, base, true)
	outDir := filepath.Join(base, "output")

	if err := Run(Options{VideoID: "vid123", WorkDir: filepath.Join(base, "work"), OutputDir: outDir, SourceURL: "https://youtu.be/vid123"}); err != nil {
		t.Fatalf("Run: %v", err)
	}

	// manifest.json
	raw, err := os.ReadFile(filepath.Join(outDir, "vid123", "manifest.json"))
	if err != nil {
		t.Fatal(err)
	}
	var m Manifest
	if err := json.Unmarshal(raw, &m); err != nil {
		t.Fatalf("manifest.json invalid: %v", err)
	}
	if m.TotalChunks != 2 || m.TotalDuration != 7 {
		t.Errorf("manifest totals = %d chunks, %.1fs; want 2, 7.0", m.TotalChunks, m.TotalDuration)
	}
	if m.VideoID != "vid123" || m.SourceURL != "https://youtu.be/vid123" || m.CreatedAt == "" {
		t.Errorf("manifest header = %+v", m)
	}
	if len(m.Chunks) != 2 {
		t.Fatalf("manifest chunks = %d, want 2", len(m.Chunks))
	}
	if m.Chunks[0].Start != 0 || m.Chunks[0].End != 5 || m.Chunks[0].Duration != 5 {
		t.Errorf("chunk 1 bounds = %+v", m.Chunks[0])
	}
	if got, want := m.Chunks[0].SourceIDs, []int{1, 2}; !intsEqual(got, want) {
		t.Errorf("chunk 1 source_ids = %v, want %v", got, want)
	}
	if m.Chunks[1].Start != 5 || m.Chunks[1].End != 7 {
		t.Errorf("chunk 2 bounds = %+v", m.Chunks[1])
	}

	// Output tree files.
	chunk1 := filepath.Join(outDir, "vid123", "chunks", "0001")
	if _, err := os.Stat(filepath.Join(chunk1, "frame.png")); err != nil {
		t.Errorf("chunk 1 frame missing: %v", err)
	}
	// Merged transcript = concat of source transcripts in order.
	tr, err := os.ReadFile(filepath.Join(chunk1, "transcript.txt"))
	if err != nil {
		t.Fatalf("chunk 1 transcript: %v", err)
	}
	wantTr := "[00:00:01.000 --> 00:00:02.000] one\n[00:00:02.500 --> 00:00:03.000] two\n"
	if string(tr) != wantTr {
		t.Errorf("merged transcript = %q, want %q", tr, wantTr)
	}
	// meta.json
	metaRaw, err := os.ReadFile(filepath.Join(chunk1, "meta.json"))
	if err != nil {
		t.Fatal(err)
	}
	var meta ChunkMeta
	if err := json.Unmarshal(metaRaw, &meta); err != nil {
		t.Fatal(err)
	}
	if meta.ID != 1 || meta.Duration != 5 || meta.Transcript != "transcript.txt" {
		t.Errorf("meta = %+v", meta)
	}
	// reconstruction.md seeded.
	rec, err := os.ReadFile(filepath.Join(outDir, "vid123", "reconstruction.md"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(rec), "## Chunk 0001") || !strings.Contains(string(rec), "## Chunk index") {
		t.Errorf("reconstruction.md missing headings:\n%s", rec)
	}
}

func TestRunFallsBackToRawChunks(t *testing.T) {
	base := t.TempDir()
	seedWorkDir(t, base, false)
	if err := Run(Options{VideoID: "vid123", WorkDir: filepath.Join(base, "work"), OutputDir: filepath.Join(base, "output")}); err != nil {
		t.Fatalf("Run: %v", err)
	}
	raw, err := os.ReadFile(filepath.Join(base, "output", "vid123", "manifest.json"))
	if err != nil {
		t.Fatal(err)
	}
	var m Manifest
	if err := json.Unmarshal(raw, &m); err != nil {
		t.Fatal(err)
	}
	if m.TotalChunks != 3 {
		t.Errorf("fallback run: total chunks = %d, want 3", m.TotalChunks)
	}
}

func TestRunMissingChunkLists(t *testing.T) {
	base := t.TempDir()
	err := Run(Options{VideoID: "nope", WorkDir: filepath.Join(base, "work"), OutputDir: filepath.Join(base, "output")})
	if err == nil || !strings.Contains(err.Error(), "chunks_deduped.json") {
		t.Errorf("expected missing-list error, got: %v", err)
	}
}

func TestRunMissingTranscript(t *testing.T) {
	base := t.TempDir()
	seedWorkDir(t, base, true)
	// Remove one transcript so merging must fail loudly.
	if err := os.Remove(filepath.Join(base, "work", "vid123", "transcripts", "0002.txt")); err != nil {
		t.Fatal(err)
	}
	err := Run(Options{VideoID: "vid123", WorkDir: filepath.Join(base, "work"), OutputDir: filepath.Join(base, "output")})
	if err == nil || !strings.Contains(err.Error(), "transcribe") {
		t.Errorf("expected missing-transcript error mentioning transcribe, got: %v", err)
	}
}

func TestRunSkippedTranscribeWritesPlaceholders(t *testing.T) {
	base := t.TempDir()
	seedWorkDir(t, base, true)
	// Simulate --skip-transcribe: no transcripts at all.
	if err := os.RemoveAll(filepath.Join(base, "work", "vid123", "transcripts")); err != nil {
		t.Fatal(err)
	}
	if err := Run(Options{VideoID: "vid123", WorkDir: filepath.Join(base, "work"), OutputDir: filepath.Join(base, "output")}); err != nil {
		t.Fatalf("Run with no transcripts should succeed with placeholders: %v", err)
	}
	b, err := os.ReadFile(filepath.Join(base, "output", "vid123", "chunks", "0001", "transcript.txt"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(b), "unavailable") {
		t.Errorf("expected placeholder transcript, got %q", b)
	}
}

func TestRunResume(t *testing.T) {
	base := t.TempDir()
	seedWorkDir(t, base, true)
	outDir := filepath.Join(base, "output")
	if err := Run(Options{VideoID: "vid123", WorkDir: filepath.Join(base, "work"), OutputDir: outDir}); err != nil {
		t.Fatal(err)
	}
	// Second run skips.
	if err := Run(Options{VideoID: "vid123", WorkDir: filepath.Join(base, "work"), OutputDir: outDir}); err != nil {
		t.Fatalf("resume run: %v", err)
	}
}

func TestRunCopiesInstructions(t *testing.T) {
	base := t.TempDir()
	seedWorkDir(t, base, true)
	// instructions.md lives at docs/instructions.md (repo-root layout in production).
	if err := os.MkdirAll(filepath.Join(base, "docs"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(base, "docs", "instructions.md"), []byte("# Instructions\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	t.Chdir(base)
	if err := Run(Options{VideoID: "vid123", WorkDir: filepath.Join(base, "work"), OutputDir: filepath.Join(base, "output")}); err != nil {
		t.Fatalf("Run: %v", err)
	}
	b, err := os.ReadFile(filepath.Join(base, "output", "vid123", "instructions.md"))
	if err != nil {
		t.Fatalf("instructions.md not copied: %v", err)
	}
	if string(b) != "# Instructions\n" {
		t.Errorf("instructions content = %q", b)
	}
}

func TestRunCopiesInstructionsLegacyRoot(t *testing.T) {
	base := t.TempDir()
	seedWorkDir(t, base, true)
	// Legacy layout: instructions.md at the repo root, no docs/ dir.
	if err := os.WriteFile(filepath.Join(base, "instructions.md"), []byte("# Legacy\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	t.Chdir(base)
	if err := Run(Options{VideoID: "vid123", WorkDir: filepath.Join(base, "work"), OutputDir: filepath.Join(base, "output")}); err != nil {
		t.Fatalf("Run: %v", err)
	}
	b, err := os.ReadFile(filepath.Join(base, "output", "vid123", "instructions.md"))
	if err != nil {
		t.Fatalf("instructions.md not copied: %v", err)
	}
	if string(b) != "# Legacy\n" {
		t.Errorf("instructions content = %q", b)
	}
}

func intsEqual(a, b []int) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}
