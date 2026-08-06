package dedupe

import (
	"bytes"
	"encoding/json"
	"image"
	"image/color"
	"image/png"
	"math/rand"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"ytreconstruct/internal/chunk"
)

const testSize = 32

// writeTestPNG writes a deterministic testSize x testSize grayscale PNG
// whose pixel value at (x, y) is pix(x, y).
func writeTestPNG(t *testing.T, path string, pix func(x, y int) uint8) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	img := image.NewGray(image.Rect(0, 0, testSize, testSize))
	for y := 0; y < testSize; y++ {
		for x := 0; x < testSize; x++ {
			img.SetGray(x, y, color.Gray{Y: pix(x, y)})
		}
	}
	f, err := os.Create(path)
	if err != nil {
		t.Fatalf("create %s: %v", path, err)
	}
	defer f.Close()
	if err := png.Encode(f, img); err != nil {
		t.Fatalf("encode %s: %v", path, err)
	}
}

// parabolicGradient rises then falls along x — a gradient whose dHash is
// non-zero (a plain monotonic gradient hashes to all-zero bits).
func parabolicGradient(x, y int) uint8 {
	peak := testSize - 1
	return uint8(4 * x * (peak - x) * 255 / (peak * peak))
}

// linearGradient rises monotonically along x; its dHash is 0 because every
// right neighbour cell is strictly brighter.
func linearGradient(x, y int) uint8 {
	return uint8(255 * x / (testSize - 1))
}

// darkenedMid is parabolicGradient with a wide region dimmed, guaranteed to
// flip at least one dHash bit in every row (so it never merges at threshold 0).
func darkenedMid(x, y int) uint8 {
	v := int(parabolicGradient(x, y))
	if x >= 3 && x <= 6 {
		v -= 120
	}
	if v < 0 {
		v = 0
	}
	return uint8(v)
}

// nearParabola is parabolicGradient with a few pixels nudged by a small
// deterministic amount, simulating a near-identical frame.
func nearParabola(seed int64) func(x, y int) uint8 {
	rng := rand.New(rand.NewSource(seed))
	bumps := map[[2]int]int{}
	for len(bumps) < 5 {
		p := [2]int{rng.Intn(testSize), rng.Intn(testSize)}
		if _, ok := bumps[p]; !ok {
			bumps[p] = rng.Intn(7) - 3
		}
	}
	return func(x, y int) uint8 {
		v := int(parabolicGradient(x, y)) + bumps[[2]int{x, y}]
		if v < 0 {
			v = 0
		}
		if v > 255 {
			v = 255
		}
		return uint8(v)
	}
}

func TestDHash(t *testing.T) {
	dir := t.TempDir()
	a := filepath.Join(dir, "a.png")
	b := filepath.Join(dir, "b.png")
	c := filepath.Join(dir, "c.png")
	d := filepath.Join(dir, "d.png")
	writeTestPNG(t, a, parabolicGradient)
	writeTestPNG(t, b, parabolicGradient)
	writeTestPNG(t, c, linearGradient)
	writeTestPNG(t, d, nearParabola(7))

	ha, err := DHash(a)
	if err != nil {
		t.Fatalf("DHash(a): %v", err)
	}
	hb, err := DHash(b)
	if err != nil {
		t.Fatalf("DHash(b): %v", err)
	}
	hc, err := DHash(c)
	if err != nil {
		t.Fatalf("DHash(c): %v", err)
	}
	hd, err := DHash(d)
	if err != nil {
		t.Fatalf("DHash(d): %v", err)
	}

	if ha == 0 {
		t.Error("parabolic gradient hashed to 0 — test image is degenerate")
	}
	if ha != hb {
		t.Errorf("identical images hashed differently: %016x vs %016x", ha, hb)
	}
	if ha == hc {
		t.Errorf("different gradients hashed identically: %016x", ha)
	}
	if d := Hamming(ha, hd); d > 5 {
		t.Errorf("near-identical images are %d bits apart, want <= 5", d)
	}
}

func TestDHashMissingFile(t *testing.T) {
	if _, err := DHash(filepath.Join(t.TempDir(), "nope.png")); err == nil {
		t.Fatal("expected error for missing frame")
	}
}

func TestHamming(t *testing.T) {
	if Hamming(0, 0) != 0 {
		t.Errorf("Hamming(0,0) = %d, want 0", Hamming(0, 0))
	}
	if Hamming(1, 0) != 1 {
		t.Errorf("Hamming(1,0) = %d, want 1", Hamming(1, 0))
	}
	if Hamming(0, 1<<5) != 1 {
		t.Errorf("Hamming(0,1<<5) = %d, want 1", Hamming(0, 1<<5))
	}
	if Hamming(^uint64(0), 0) != 64 {
		t.Errorf("Hamming(^0,0) = %d, want 64", Hamming(^uint64(0), 0))
	}
}

func TestMerge(t *testing.T) {
	dir := t.TempDir()
	frames := []struct {
		name string
		pix  func(x, y int) uint8
	}{
		{"f1", parabolicGradient}, // identical to f2 → merge
		{"f2", parabolicGradient},
		{"f3", linearGradient},    // different → standalone
		{"f4", parabolicGradient}, // identical to f5 → merge
		{"f5", parabolicGradient},
	}
	for _, fr := range frames {
		writeTestPNG(t, filepath.Join(dir, fr.name+".png"), fr.pix)
	}

	chunks := []chunk.Chunk{
		{ID: 1, Start: 0, End: 10, Frame: filepath.Join(dir, "f1.png"), Audio: "chunks_raw/0001/audio.wav"},
		{ID: 2, Start: 10, End: 20, Frame: filepath.Join(dir, "f2.png"), Audio: "chunks_raw/0002/audio.wav"},
		{ID: 3, Start: 20, End: 30, Frame: filepath.Join(dir, "f3.png"), Audio: "chunks_raw/0003/audio.wav"},
		{ID: 4, Start: 30, End: 40, Frame: filepath.Join(dir, "f4.png"), Audio: "chunks_raw/0004/audio.wav"},
		{ID: 5, Start: 40, End: 50, Frame: filepath.Join(dir, "f5.png"), Audio: "chunks_raw/0005/audio.wav"},
	}
	list := chunk.ChunkList{VideoID: "abc", Source: "video.mp4", Chunks: chunks}

	got := Merge(list, 5)
	if len(got.Chunks) != 3 {
		t.Fatalf("got %d chunks, want 3", len(got.Chunks))
	}

	// Merged run [1,2]: range extended, first frame/audio kept, sources set.
	m1 := got.Chunks[0]
	if m1.ID != 1 || m1.Start != 0 || m1.End != 20 {
		t.Errorf("merged chunk = id %d [%v,%v], want id 1 [0,20]", m1.ID, m1.Start, m1.End)
	}
	if m1.Frame != list.Chunks[0].Frame || m1.Audio != list.Chunks[0].Audio {
		t.Errorf("merged chunk must keep first frame/audio, got %q / %q", m1.Frame, m1.Audio)
	}
	if !reflect.DeepEqual(m1.SourceIDs, []int{1, 2}) {
		t.Errorf("source_ids = %v, want [1 2]", m1.SourceIDs)
	}

	// Middle chunk (ID 3) is untouched and unmerged.
	if !reflect.DeepEqual(got.Chunks[1], list.Chunks[2]) {
		t.Errorf("standalone chunk changed: got %+v, want %+v", got.Chunks[1], list.Chunks[2])
	}

	// Merged run [4,5].
	m3 := got.Chunks[2]
	if m3.ID != 4 || m3.Start != 30 || m3.End != 50 {
		t.Errorf("merged chunk = id %d [%v,%v], want id 4 [30,50]", m3.ID, m3.Start, m3.End)
	}
	if !reflect.DeepEqual(m3.SourceIDs, []int{4, 5}) {
		t.Errorf("source_ids = %v, want [4 5]", m3.SourceIDs)
	}

	// Order is preserved.
	for i, c := range got.Chunks {
		if want := []int{1, 3, 4}[i]; c.ID != want {
			t.Errorf("chunk %d has id %d, want %d", i, c.ID, want)
		}
	}

	// The input list must not be mutated.
	if len(list.Chunks) != 5 || list.Chunks[0].End != 10 || list.Chunks[0].SourceIDs != nil {
		t.Errorf("Merge mutated its input: %+v", list.Chunks[0])
	}
}

func TestMergeThresholdZero(t *testing.T) {
	dir := t.TempDir()
	id := filepath.Join(dir, "id.png")
	dim := filepath.Join(dir, "dim.png")
	writeTestPNG(t, id, parabolicGradient)
	writeTestPNG(t, dim, darkenedMid)

	list := chunk.ChunkList{Chunks: []chunk.Chunk{
		{ID: 1, Start: 0, End: 10, Frame: id},
		{ID: 2, Start: 10, End: 20, Frame: id},
		{ID: 3, Start: 20, End: 30, Frame: dim},
	}}
	got := Merge(list, 0)
	if len(got.Chunks) != 2 {
		t.Fatalf("threshold 0: got %d chunks, want 2", len(got.Chunks))
	}
	if !reflect.DeepEqual(got.Chunks[0].SourceIDs, []int{1, 2}) {
		t.Errorf("threshold 0: identical frames should merge, source_ids = %v", got.Chunks[0].SourceIDs)
	}
	if got.Chunks[1].SourceIDs != nil {
		t.Errorf("threshold 0: differing frames must not merge, source_ids = %v", got.Chunks[1].SourceIDs)
	}
}

func TestRun(t *testing.T) {
	work := t.TempDir()
	id := "vid123"
	dir := filepath.Join(work, id)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}

	// chunks.json: c1 and c2 have identical frames (merge), c3 differs.
	raw := chunk.ChunkList{
		VideoID: id,
		Source:  "video.mp4",
		Chunks: []chunk.Chunk{
			{ID: 1, Start: 0, End: 10, Frame: "chunks_raw/0001/frame.png", Audio: "chunks_raw/0001/audio.wav"},
			{ID: 2, Start: 10, End: 20, Frame: "chunks_raw/0002/frame.png", Audio: "chunks_raw/0002/audio.wav"},
			{ID: 3, Start: 20, End: 30, Frame: "chunks_raw/0003/frame.png", Audio: "chunks_raw/0003/audio.wav"},
		},
	}
	data, err := json.MarshalIndent(raw, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "chunks.json"), append(data, '\n'), 0o644); err != nil {
		t.Fatal(err)
	}
	writeTestPNG(t, filepath.Join(dir, "chunks_raw", "0001", "frame.png"), parabolicGradient)
	writeTestPNG(t, filepath.Join(dir, "chunks_raw", "0002", "frame.png"), parabolicGradient)
	writeTestPNG(t, filepath.Join(dir, "chunks_raw", "0003", "frame.png"), linearGradient)

	if err := Run(Options{VideoID: id, WorkDir: work, HashThreshold: 5}); err != nil {
		t.Fatalf("Run: %v", err)
	}

	out, err := os.ReadFile(filepath.Join(dir, "chunks_deduped.json"))
	if err != nil {
		t.Fatal(err)
	}
	var got chunk.ChunkList
	if err := json.Unmarshal(out, &got); err != nil {
		t.Fatalf("chunks_deduped.json is not valid JSON: %v", err)
	}
	if got.VideoID != id || got.Source != "video.mp4" {
		t.Errorf("list header = %+v", got)
	}
	if len(got.Chunks) != 2 {
		t.Fatalf("got %d chunks, want 2", len(got.Chunks))
	}

	m := got.Chunks[0]
	if m.ID != 1 || m.Start != 0 || m.End != 20 {
		t.Errorf("merged chunk = id %d [%v,%v], want id 1 [0,20]", m.ID, m.Start, m.End)
	}
	if m.Frame != "chunks_raw/0001/frame.png" || m.Audio != "chunks_raw/0001/audio.wav" {
		t.Errorf("merged chunk must keep relative first frame/audio, got %q / %q", m.Frame, m.Audio)
	}
	if !reflect.DeepEqual(m.SourceIDs, []int{1, 2}) {
		t.Errorf("source_ids = %v, want [1 2]", m.SourceIDs)
	}
	c := got.Chunks[1]
	if c.ID != 3 || c.Start != 20 || c.End != 30 || c.SourceIDs != nil {
		t.Errorf("standalone chunk changed: %+v", c)
	}
	if c.Frame != "chunks_raw/0003/frame.png" {
		t.Errorf("standalone frame = %q, want chunks_raw/0003/frame.png", c.Frame)
	}

	// Resume: a second run skips and leaves the output byte-identical.
	before, _ := os.ReadFile(filepath.Join(dir, "chunks_deduped.json"))
	if err := Run(Options{VideoID: id, WorkDir: work, HashThreshold: 5}); err != nil {
		t.Fatalf("resume Run: %v", err)
	}
	after, err := os.ReadFile(filepath.Join(dir, "chunks_deduped.json"))
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(before, after) {
		t.Error("resume run rewrote chunks_deduped.json")
	}
}

func TestRunMissingChunks(t *testing.T) {
	work := t.TempDir()
	err := Run(Options{VideoID: "ghost", WorkDir: work, HashThreshold: 5})
	if err == nil {
		t.Fatal("expected error for missing chunks.json")
	}
	if !strings.Contains(err.Error(), "ytreconstruct chunk") {
		t.Errorf("error should point at `ytreconstruct chunk`, got: %v", err)
	}
}

func TestRunMissingFrame(t *testing.T) {
	work := t.TempDir()
	id := "vid"
	dir := filepath.Join(work, id)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	raw := chunk.ChunkList{VideoID: id, Source: "video.mp4", Chunks: []chunk.Chunk{
		{ID: 1, Start: 0, End: 10, Frame: "chunks_raw/0001/frame.png"},
	}}
	data, err := json.MarshalIndent(raw, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "chunks.json"), append(data, '\n'), 0o644); err != nil {
		t.Fatal(err)
	}
	err = Run(Options{VideoID: id, WorkDir: work, HashThreshold: 5})
	if err == nil || !strings.Contains(err.Error(), "frame") {
		t.Fatalf("expected frame error, got: %v", err)
	}
}

func TestRunResumeSkipsMissingChunks(t *testing.T) {
	work := t.TempDir()
	dir := filepath.Join(work, "abc")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	// Only the deduped output exists: Run must skip before reading chunks.json.
	if err := os.WriteFile(filepath.Join(dir, "chunks_deduped.json"), []byte("{}"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := Run(Options{VideoID: "abc", WorkDir: work, HashThreshold: 5}); err != nil {
		t.Fatalf("resume Run: %v", err)
	}
}

func TestRunNegativeThreshold(t *testing.T) {
	err := Run(Options{VideoID: "x", WorkDir: t.TempDir(), HashThreshold: -1})
	if err == nil {
		t.Fatal("expected error for negative threshold")
	}
	if !strings.Contains(err.Error(), "hash-threshold") {
		t.Errorf("error should mention hash-threshold, got: %v", err)
	}
}
