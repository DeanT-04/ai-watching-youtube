package store

import (
	"archive/zip"
	"bytes"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"ytreconstruct/internal/testexec"
)

func TestHelperProcess(t *testing.T) { testexec.HelperProcess(t) }

// fakeFFmpeg makes `ffmpeg ... -i <png> ... <webp>` write the webp (last
// arg) and exit 0 — no real ffmpeg, no network.
func fakeFFmpeg(t *testing.T) {
	t.Helper()
	oldCmd := command
	command = func(name string, args ...string) *exec.Cmd {
		if name != "ffmpeg" {
			t.Fatalf("unexpected command %q", name)
		}
		if err := os.WriteFile(args[len(args)-1], []byte("fake-webp"), 0o644); err != nil {
			t.Fatalf("fake ffmpeg: %v", err)
		}
		return testexec.Command("", "", 0)(name, args...)
	}
	t.Cleanup(func() { command = oldCmd })
}

// fixture builds a minimal finished output/<id>/ + work/<id>/ tree: 2 chunks,
// transcripts, reconstruction seed, instructions, and whisper full.json.
func fixture(t *testing.T) Options {
	t.Helper()
	base := t.TempDir()
	opts := Options{
		VideoID:   "BL8TfsLk3WM",
		WorkDir:   filepath.Join(base, "work"),
		OutputDir: filepath.Join(base, "output"),
		StoreDir:  filepath.Join(base, "store"),
		Jobs:      2,
	}
	out := filepath.Join(opts.OutputDir, opts.VideoID)
	mustWrite(t, filepath.Join(out, "manifest.json"), `{
  "video_id": "BL8TfsLk3WM",
  "source_url": "https://youtu.be/BL8TfsLk3WM",
  "created_at": "2026-08-07T00:00:00Z",
  "total_chunks": 2,
  "total_duration": 10,
  "chunks": [
    {"id":1,"start":0,"end":6,"duration":6,"frame":"chunks/0001/frame.png","transcript":"chunks/0001/transcript.txt","meta":"chunks/0001/meta.json","source_ids":[1]},
    {"id":2,"start":6,"end":10,"duration":4,"frame":"chunks/0002/frame.png","transcript":"chunks/0002/transcript.txt","meta":"chunks/0002/meta.json","source_ids":[2]}
  ]
}`)
	for n := 1; n <= 2; n++ {
		dir := filepath.Join(out, "chunks", "000"+string(rune('0'+n)))
		mustWrite(t, filepath.Join(dir, "frame.png"), "png-bytes-"+string(rune('0'+n)))
		mustWrite(t, filepath.Join(dir, "meta.json"), `{"id":`+string(rune('0'+n))+`}`)
	}
	mustWrite(t, filepath.Join(out, "chunks", "0001", "transcript.txt"), "[00:00:00.000 --> 00:00:06.000] Hello there\n")
	mustWrite(t, filepath.Join(out, "chunks", "0002", "transcript.txt"), "[00:00:06.000 --> 00:00:10.000] Second chunk\n")
	mustWrite(t, filepath.Join(out, "reconstruction.md"), "# Reconstruction log\n\nseed text\n")
	mustWrite(t, filepath.Join(out, "instructions.md"), "instructions text\n")
	mustWrite(t, filepath.Join(opts.WorkDir, opts.VideoID, "transcripts", "full.json"), `{
  "transcription": [
    {"timestamps":{"from":"00:00:01,000","to":"00:00:04,000"},"offsets":{"from":1000,"to":4000},"text":"Hello there"},
    {"timestamps":{"from":"00:00:06,500","to":"00:00:09,000"},"offsets":{"from":6500,"to":9000},"text":"Second chunk"},
    {"timestamps":{"from":"00:00:00,000","to":"00:00:00,000"},"offsets":{"from":0,"to":0},"text":""}
  ]
}`)
	return opts
}

func mustWrite(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("write %s: %v", path, err)
	}
}

func TestRunPacksFixture(t *testing.T) {
	opts := fixture(t)
	fakeFFmpeg(t)
	oldLP := LookPath
	LookPath = testexec.LookPath
	t.Cleanup(func() { LookPath = oldLP })

	rep, err := Run(opts)
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if rep.Skipped {
		t.Fatal("first pack must not be skipped")
	}
	if rep.Chunks != 2 || rep.Frames != 2 {
		t.Fatalf("report = chunks %d frames %d, want 2/2", rep.Chunks, rep.Frames)
	}
	storeFile := filepath.Join(opts.StoreDir, opts.VideoID+"."+FileExt)
	if _, err := os.Stat(storeFile); err != nil {
		t.Fatalf("store file missing: %v", err)
	}

	spec, err := Read(opts)
	if err != nil {
		t.Fatalf("Read: %v", err)
	}
	if spec.SchemaVersion != SchemaVersion || spec.TotalChunks != 2 || spec.TotalDuration != 10 {
		t.Fatalf("spec header wrong: %+v", spec)
	}
	if spec.SourceURL != "https://youtu.be/BL8TfsLk3WM" || spec.CreatedAt != "2026-08-07T00:00:00Z" {
		t.Fatalf("spec metadata wrong: %+v", spec)
	}
	if spec.Chunks[0].Transcript != "[00:00:00.000 --> 00:00:06.000] Hello there\n" {
		t.Fatalf("chunk 1 transcript wrong: %q", spec.Chunks[0].Transcript)
	}
	want := []Segment{{From: 1000, To: 4000, Text: "Hello there"}}
	if len(spec.Chunks[0].Segments) != 1 || spec.Chunks[0].Segments[0] != want[0] {
		t.Fatalf("chunk 1 segments = %+v, want %+v", spec.Chunks[0].Segments, want)
	}
	if len(spec.Chunks[1].Segments) != 1 || spec.Chunks[1].Segments[0].Text != "Second chunk" {
		t.Fatalf("chunk 2 segments = %+v", spec.Chunks[1].Segments)
	}
	if !strings.Contains(spec.ReconstructionMD, "seed text") || !strings.Contains(spec.InstructionsMD, "instructions text") {
		t.Fatalf("seeds not embedded: rec=%q instr=%q", spec.ReconstructionMD, spec.InstructionsMD)
	}

	// Member compression policy: spec deflated, frames stored verbatim.
	zr, err := zip.OpenReader(storeFile)
	if err != nil {
		t.Fatalf("open zip: %v", err)
	}
	defer zr.Close()
	methods := map[string]uint16{}
	for _, f := range zr.File {
		methods[f.Name] = f.Method
	}
	if methods[specMember] != zip.Deflate {
		t.Fatalf("spec member method = %d, want Deflate", methods[specMember])
	}
	if methods["frames/0001.webp"] != zip.Store || methods["frames/0002.webp"] != zip.Store {
		t.Fatalf("frame members must be Store, got %v", methods)
	}

	frame, err := FrameBytes(opts, 1)
	if err != nil || string(frame) != "fake-webp" {
		t.Fatalf("FrameBytes = %q, %v", frame, err)
	}

	lib, err := LoadLibrary(opts.StoreDir)
	if err != nil {
		t.Fatalf("LoadLibrary: %v", err)
	}
	if len(lib.Videos) != 1 || lib.Videos[0].VideoID != opts.VideoID || lib.Videos[0].StoreFile != opts.VideoID+"."+FileExt {
		t.Fatalf("library = %+v", lib.Videos)
	}
}

func TestRunIdempotentAndForce(t *testing.T) {
	opts := fixture(t)
	fakeFFmpeg(t)
	oldLP := LookPath
	LookPath = testexec.LookPath
	t.Cleanup(func() { LookPath = oldLP })

	if _, err := Run(opts); err != nil {
		t.Fatalf("first pack: %v", err)
	}
	storeFile := filepath.Join(opts.StoreDir, opts.VideoID+"."+FileExt)
	before, err := os.ReadFile(storeFile)
	if err != nil {
		t.Fatal(err)
	}

	rep, err := Run(opts)
	if err != nil {
		t.Fatalf("second pack: %v", err)
	}
	if !rep.Skipped {
		t.Fatal("second pack must be skipped")
	}
	after, err := os.ReadFile(storeFile)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(before, after) {
		t.Fatal("skipped pack must not rewrite the store file")
	}

	opts.Force = true
	rep, err = Run(opts)
	if err != nil {
		t.Fatalf("force pack: %v", err)
	}
	if rep.Skipped {
		t.Fatal("force pack must rebuild")
	}
	entries, err := List(opts)
	if err != nil || len(entries) != 1 {
		t.Fatalf("library after force pack = %+v, %v", entries, err)
	}
}

func TestRunMissingManifest(t *testing.T) {
	opts := fixture(t)
	// Remove the manifest to simulate an unfinished pipeline.
	if err := os.Remove(filepath.Join(opts.OutputDir, opts.VideoID, "manifest.json")); err != nil {
		t.Fatal(err)
	}
	_, err := Run(opts)
	if err == nil || !strings.Contains(err.Error(), "manifest") {
		t.Fatalf("want manifest guard error, got %v", err)
	}
}

func TestRunMissingFFmpeg(t *testing.T) {
	opts := fixture(t)
	oldLP := LookPath
	LookPath = func(string) (string, error) { return "", os.ErrNotExist }
	t.Cleanup(func() { LookPath = oldLP })

	_, err := Run(opts)
	if err == nil || !strings.Contains(err.Error(), "ffmpeg") {
		t.Fatalf("want ffmpeg guard error, got %v", err)
	}
}

func TestRunMissingTranscript(t *testing.T) {
	opts := fixture(t)
	fakeFFmpeg(t)
	oldLP := LookPath
	LookPath = testexec.LookPath
	t.Cleanup(func() { LookPath = oldLP })

	if err := os.Remove(filepath.Join(opts.OutputDir, opts.VideoID, "chunks", "0001", "transcript.txt")); err != nil {
		t.Fatal(err)
	}
	_, err := Run(opts)
	if err == nil || !strings.Contains(err.Error(), "transcript") {
		t.Fatalf("want transcript integrity error, got %v", err)
	}
}

// writeStoreFile builds a store file directly from a spec and frame members,
// for verify tests that need hand-crafted archives.
func writeStoreFile(t *testing.T, opts Options, spec Spec, frames map[string]string) {
	t.Helper()
	if err := os.MkdirAll(opts.StoreDir, 0o755); err != nil {
		t.Fatal(err)
	}
	f, err := os.Create(filepath.Join(opts.StoreDir, opts.VideoID+"."+FileExt))
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	zw := zip.NewWriter(f)
	if spec.SchemaVersion == 0 {
		spec.SchemaVersion = SchemaVersion
	}
	data, _ := json.Marshal(spec)
	h := &zip.FileHeader{Name: specMember, Method: zip.Deflate}
	w, _ := zw.CreateHeader(h)
	w.Write(data)
	for name, content := range frames {
		fh := &zip.FileHeader{Name: name, Method: zip.Store}
		fw, _ := zw.CreateHeader(fh)
		fw.Write([]byte(content))
	}
	if err := zw.Close(); err != nil {
		t.Fatal(err)
	}
}

func TestVerifyOK(t *testing.T) {
	opts := fixture(t)
	fakeFFmpeg(t)
	LookPath = testexec.LookPath
	t.Cleanup(func() { LookPath = testexec.LookPath })
	if _, err := Run(opts); err != nil {
		t.Fatalf("pack: %v", err)
	}

	rep, err := Verify(opts)
	if err != nil {
		t.Fatalf("Verify: %v", err)
	}
	if rep.Chunks != 2 || rep.Frames != 2 || rep.TotalBytes <= 0 {
		t.Fatalf("verify report = %+v", rep)
	}
}

func TestVerifyCorruptFrame(t *testing.T) {
	opts := fixture(t)
	fakeFFmpeg(t)
	LookPath = testexec.LookPath
	t.Cleanup(func() { LookPath = testexec.LookPath })
	if _, err := Run(opts); err != nil {
		t.Fatalf("pack: %v", err)
	}

	storeFile := filepath.Join(opts.StoreDir, opts.VideoID+"."+FileExt)
	data, err := os.ReadFile(storeFile)
	if err != nil {
		t.Fatal(err)
	}
	// Flip a byte in the middle: inside the frame payload, breaking its CRC.
	data[len(data)/2] ^= 0xFF
	if err := os.WriteFile(storeFile, data, 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := Verify(opts); err == nil {
		t.Fatal("Verify must detect the corrupted store")
	}
}

func TestVerifyMissingMember(t *testing.T) {
	opts := Options{VideoID: "BL8TfsLk3WM", StoreDir: t.TempDir()}
	spec := Spec{VideoID: "BL8TfsLk3WM", TotalChunks: 1, TotalDuration: 6,
		Chunks: []Chunk{{ID: 1, Start: 0, End: 6, Duration: 6, Frame: "frames/0001.webp"}}}
	// Archive has no frames at all — the referenced member is missing.
	writeStoreFile(t, opts, spec, nil)
	if _, err := Verify(opts); err == nil || !strings.Contains(err.Error(), "missing frame member") {
		t.Fatalf("want missing-member error, got %v", err)
	}
}

func TestVerifyUnexpectedMember(t *testing.T) {
	opts := Options{VideoID: "BL8TfsLk3WM", StoreDir: t.TempDir()}
	spec := Spec{VideoID: "BL8TfsLk3WM", TotalChunks: 1, TotalDuration: 6,
		Chunks: []Chunk{{ID: 1, Start: 0, End: 6, Duration: 6, Frame: "frames/0001.webp"}}}
	writeStoreFile(t, opts, spec, map[string]string{"frames/0001.webp": "a", "frames/0002.webp": "b"})
	if _, err := Verify(opts); err == nil || !strings.Contains(err.Error(), "unexpected frame member") {
		t.Fatalf("want unexpected-member error, got %v", err)
	}
}

func TestReadRejectsUnsupportedSchema(t *testing.T) {
	opts := Options{VideoID: "BL8TfsLk3WM", StoreDir: t.TempDir()}
	spec := Spec{SchemaVersion: 99, VideoID: "BL8TfsLk3WM", TotalChunks: 1,
		Chunks: []Chunk{{ID: 1, Frame: "frames/0001.webp"}}}
	writeStoreFile(t, opts, spec, map[string]string{"frames/0001.webp": "a"})
	if _, err := Read(opts); err == nil || !strings.Contains(err.Error(), "schema_version") {
		t.Fatalf("want schema gate error, got %v", err)
	}
	if _, err := Verify(opts); err == nil {
		t.Fatal("Verify must also reject an unsupported schema")
	}
}

func TestFrameBytesMissingChunk(t *testing.T) {
	opts := Options{VideoID: "BL8TfsLk3WM", StoreDir: t.TempDir()}
	spec := Spec{VideoID: "BL8TfsLk3WM", TotalChunks: 1,
		Chunks: []Chunk{{ID: 1, Frame: "frames/0001.webp"}}}
	writeStoreFile(t, opts, spec, map[string]string{"frames/0001.webp": "a"})
	if _, err := FrameBytes(opts, 99); err == nil {
		t.Fatal("FrameBytes for a missing chunk must fail")
	}
}

func TestLibraryRoundTrip(t *testing.T) {
	dir := t.TempDir()
	lib, err := LoadLibrary(dir)
	if err != nil || len(lib.Videos) != 0 {
		t.Fatalf("missing library file must load empty: %+v, %v", lib, err)
	}
	entry := LibraryEntry{VideoID: "AAAA", PackedAt: "now", TotalChunks: 1}
	if err := updateLibrary(dir, entry); err != nil {
		t.Fatalf("update: %v", err)
	}
	if err := updateLibrary(dir, entry); err != nil {
		t.Fatalf("update same id: %v", err)
	}
	if err := updateLibrary(dir, LibraryEntry{VideoID: "BBBB", TotalChunks: 2}); err != nil {
		t.Fatalf("update second: %v", err)
	}
	lib, err = LoadLibrary(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(lib.Videos) != 2 {
		t.Fatalf("want 2 entries, got %d", len(lib.Videos))
	}

	if err := os.WriteFile(filepath.Join(dir, "library.json"), []byte("{not json"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := LoadLibrary(dir); err == nil {
		t.Fatal("corrupt library must error, not silently clobber")
	}
}

func TestListSorted(t *testing.T) {
	dir := t.TempDir()
	if err := updateLibrary(dir, LibraryEntry{VideoID: "BBBB"}); err != nil {
		t.Fatal(err)
	}
	if err := updateLibrary(dir, LibraryEntry{VideoID: "AAAA"}); err != nil {
		t.Fatal(err)
	}
	entries, err := List(Options{StoreDir: dir})
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 2 || entries[0].VideoID != "AAAA" || entries[1].VideoID != "BBBB" {
		t.Fatalf("list order = %+v", entries)
	}
}

func TestQueryGrepRangeOrder(t *testing.T) {
	opts := fixture(t)
	fakeFFmpeg(t)
	oldLP := LookPath
	LookPath = testexec.LookPath
	t.Cleanup(func() { LookPath = oldLP })
	if _, err := Run(opts); err != nil {
		t.Fatalf("pack: %v", err)
	}

	// Grep hits chunk 1's structured segment, with formatted timestamps.
	res, err := Query(opts, "hello", nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(res) != 1 || res[0].Chunk.ID != 1 || len(res[0].Matches) != 1 {
		t.Fatalf("grep 'hello' = %+v", res)
	}
	if !strings.Contains(res[0].Matches[0], "[00:00:01.000 --> 00:00:04.000] Hello there") {
		t.Fatalf("match line wrong: %q", res[0].Matches[0])
	}

	// Case-insensitive.
	res, err = Query(opts, "HELLO", nil, nil)
	if err != nil || len(res) != 1 {
		t.Fatalf("case-insensitive grep failed: %+v, %v", res, err)
	}

	// Range window [6,10] excludes chunk 1, includes chunk 2.
	t1, t2 := 6.0, 10.0
	res, err = Query(opts, "hello", &t1, &t2)
	if err != nil || len(res) != 0 {
		t.Fatalf("range must exclude chunk 1: %+v, %v", res, err)
	}
	res, err = Query(opts, "second", &t1, &t2)
	if err != nil || len(res) != 1 || res[0].Chunk.ID != 2 {
		t.Fatalf("range must include chunk 2: %+v, %v", res, err)
	}

	// Transcript fallback: the timestamp prefix exists only in the merged
	// transcript text, not in the structured segment (segment starts at 1000ms).
	res, err = Query(opts, "00:00:00.000", nil, nil)
	if err != nil || len(res) != 1 || len(res[0].Matches) != 1 {
		t.Fatalf("transcript fallback failed: %+v, %v", res, err)
	}

	// No matches anywhere.
	res, err = Query(opts, "zzz-no-such-term", nil, nil)
	if err != nil || len(res) != 0 {
		t.Fatalf("no-match query = %+v, %v", res, err)
	}
}

func TestFramePNG(t *testing.T) {
	opts := fixture(t)
	fakeFFmpeg(t)
	oldLP := LookPath
	LookPath = testexec.LookPath
	t.Cleanup(func() { LookPath = oldLP })
	if _, err := Run(opts); err != nil {
		t.Fatalf("pack: %v", err)
	}

	dst := filepath.Join(t.TempDir(), "frame.png")
	if err := FramePNG(opts, 1, dst); err != nil {
		t.Fatalf("FramePNG: %v", err)
	}
	data, err := os.ReadFile(dst)
	if err != nil || string(data) != "fake-webp" {
		t.Fatalf("decoded frame = %q, %v (want the fake ffmpeg output)", data, err)
	}
}

// TestRunWithoutFullJSON: when transcription was skipped there is no
// transcripts/full.json — the store must still pack (segments are absent).
func TestRunWithoutFullJSON(t *testing.T) {
	opts := fixture(t)
	fakeFFmpeg(t)
	oldLP := LookPath
	LookPath = testexec.LookPath
	t.Cleanup(func() { LookPath = oldLP })
	if err := os.Remove(filepath.Join(opts.WorkDir, opts.VideoID, "transcripts", "full.json")); err != nil {
		t.Fatal(err)
	}
	rep, err := Run(opts)
	if err != nil {
		t.Fatalf("pack without full.json: %v", err)
	}
	if rep.Skipped {
		t.Fatal("pack must not be skipped")
	}
	spec, err := Read(opts)
	if err != nil {
		t.Fatal(err)
	}
	if len(spec.Chunks[0].Segments) != 0 || len(spec.Chunks[1].Segments) != 0 {
		t.Fatalf("segments must be empty without full.json: %+v", spec.Chunks[0].Segments)
	}
	if spec.Chunks[0].Transcript == "" {
		t.Fatal("transcript text must still be embedded")
	}
}
