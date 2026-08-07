// Package store packs a finished output/<video_id>/ deliverable into the
// single-file .ytr store format (see docs/storage-format.md): one zip per
// video holding a queryable ytr/spec.json index plus WebP-lossless frames,
// and a tiny store/library.json index across videos. An agent can then
// "watch" the whole video with `ytreconstruct store dump <id>` and pull any
// frame with `ytreconstruct store frame <id> <NNNN> out.png`.
package store

import (
	"archive/zip"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"sync"
	"time"

	"ytreconstruct/internal/lowprio"
	"ytreconstruct/internal/manifest"
)

// SchemaVersion is the .ytr format version this package reads and writes.
const SchemaVersion = 1

// Member names inside a .ytr zip.
const (
	specMember   = "ytr/spec.json"
	framesPrefix = "frames/"
)

// FileExt is the store file suffix (without the dot).
const FileExt = "ytr"

// command and LookPath are indirections so tests can fake os/exec.
// command defaults to lowprio.Command so child processes run at
// BELOW_NORMAL priority and never starve the user's interactive apps.
// LookPath is exported for tests only (the CLI's own tests live in
// package main and cannot touch an unexported var) — production code
// must treat it as read-only.
var (
	command  = lowprio.Command
	LookPath = exec.LookPath
)

// Options configures a store operation.
type Options struct {
	VideoID   string
	WorkDir   string
	OutputDir string
	StoreDir  string
	Force     bool // pack: rebuild even if the store file already exists
	Jobs      int  // pack: parallel frame-conversion workers
}

// Report summarizes a pack or verify run.
type Report struct {
	VideoID    string
	StorePath  string
	Chunks     int
	Frames     int
	TotalBytes int64
	Skipped    bool // pack: the store file already existed and Force was off
}

// Segment is one spoken phrase, absolute on the video timeline.
type Segment struct {
	From int64  `json:"from"` // milliseconds
	To   int64  `json:"to"`   // milliseconds
	Text string `json:"text"`
}

// Chunk mirrors the manifest's ordered playback spine. Never reorder.
type Chunk struct {
	ID         int       `json:"id"`
	Start      float64   `json:"start"` // seconds, inclusive
	End        float64   `json:"end"`   // seconds, exclusive
	Duration   float64   `json:"duration"`
	SourceIDs  []int     `json:"source_ids"`
	Frame      string    `json:"frame"` // zip member, e.g. frames/0001.webp
	Transcript string    `json:"transcript"`
	Segments   []Segment `json:"segments"`
}

// Spec is the ytr/spec.json index: everything an agent needs to watch the
// video in order, without the frames.
type Spec struct {
	SchemaVersion    int     `json:"schema_version"`
	VideoID          string  `json:"video_id"`
	SourceURL        string  `json:"source_url"`
	CreatedAt        string  `json:"created_at"`
	PackedAt         string  `json:"packed_at"`
	TotalChunks      int     `json:"total_chunks"`
	TotalDuration    float64 `json:"total_duration"`
	ReconstructionMD string  `json:"reconstruction_md"`
	InstructionsMD   string  `json:"instructions_md"`
	Chunks           []Chunk `json:"chunks"`
}

// whisperFull mirrors the schema whisper-cli -oj writes (work/<id>/transcripts/full.json).
// offsets.from/to are milliseconds on the video timeline.
type whisperFull struct {
	Transcription []struct {
		Offsets struct {
			From float64 `json:"from"`
			To   float64 `json:"to"`
		} `json:"offsets"`
		Text string `json:"text"`
	} `json:"transcription"`
}

// Run packs output/<video_id>/ into store/<video_id>.ytr and updates
// store/library.json. Idempotent: an existing store file is skipped unless
// Force is set. The archive is written atomically (.part + rename), so a
// killed pack never fakes completion.
func Run(opts Options) (*Report, error) {
	if opts.OutputDir == "" {
		opts.OutputDir = "output"
	}
	if opts.StoreDir == "" {
		opts.StoreDir = "store"
	}
	if opts.WorkDir == "" {
		opts.WorkDir = "work"
	}
	if opts.Jobs < 1 {
		opts.Jobs = 1
	}

	outDir := filepath.Join(opts.OutputDir, opts.VideoID)
	m, err := readManifest(filepath.Join(outDir, "manifest.json"))
	if err != nil {
		return nil, err
	}

	storePath := filepath.Join(opts.StoreDir, opts.VideoID+"."+FileExt)
	if !opts.Force {
		if _, err := os.Stat(storePath); err == nil {
			// Skip path: the store file already exists. Reconcile the library
			// index in case the entry is missing/stale (e.g. library.json was
			// deleted) — a skip must never require ffmpeg.
			if spec, err := Read(opts); err == nil {
				_ = updateLibrary(opts.StoreDir, entryFromSpec(spec, storePath))
			}
			return &Report{VideoID: opts.VideoID, StorePath: storePath, Skipped: true}, nil
		}
	}

	if _, err := LookPath("ffmpeg"); err != nil {
		return nil, fmt.Errorf("store: required binary %q not found in PATH — install ffmpeg and retry: %w", "ffmpeg", err)
	}

	// Convert every frame PNG → WebP lossless into a scratch dir in parallel
	// (keeps memory flat — the zip writer is sequential), then assemble the
	// archive from the converted files.
	if err := os.MkdirAll(opts.StoreDir, 0o755); err != nil {
		return nil, fmt.Errorf("store: mkdir %s: %w", opts.StoreDir, err)
	}
	tmpDir, err := os.MkdirTemp(opts.StoreDir, ".pack-"+opts.VideoID+"-")
	if err != nil {
		return nil, fmt.Errorf("store: mkdir temp: %w", err)
	}
	defer os.RemoveAll(tmpDir)

	webps, err := convertFrames(outDir, m, tmpDir, opts.Jobs)
	if err != nil {
		return nil, err
	}

	spec, err := buildSpec(opts, outDir, m)
	if err != nil {
		return nil, err
	}
	if err := writeStore(storePath, spec, webps); err != nil {
		return nil, err
	}

	entry := entryFromSpec(spec, storePath)
	if err := updateLibrary(opts.StoreDir, entry); err != nil {
		return nil, err
	}

	fi, err := os.Stat(storePath)
	if err != nil {
		return nil, fmt.Errorf("store: stat %s: %w", storePath, err)
	}
	return &Report{
		VideoID:    spec.VideoID,
		StorePath:  storePath,
		Chunks:     spec.TotalChunks,
		Frames:     len(webps),
		TotalBytes: fi.Size(),
	}, nil
}

// entryFromSpec builds the library row for a packed store.
func entryFromSpec(spec *Spec, storePath string) LibraryEntry {
	return LibraryEntry{
		VideoID:       spec.VideoID,
		SourceURL:     spec.SourceURL,
		CreatedAt:     spec.CreatedAt,
		PackedAt:      spec.PackedAt,
		TotalChunks:   spec.TotalChunks,
		TotalDuration: spec.TotalDuration,
		StoreFile:     filepath.Base(storePath),
	}
}

// readManifest loads and validates output/<video_id>/manifest.json.
func readManifest(path string) (*manifest.Manifest, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("store: no manifest at %s — run `ytreconstruct all` (or `manifest`) first: %w", path, err)
	}
	var m manifest.Manifest
	if err := json.Unmarshal(data, &m); err != nil {
		return nil, fmt.Errorf("store: %s is not a valid manifest: %w", path, err)
	}
	if len(m.Chunks) == 0 {
		return nil, fmt.Errorf("store: %s contains no chunks", path)
	}
	return &m, nil
}

// convertFrames runs PNG → WebP lossless conversion for every manifest chunk
// in parallel and returns the webp file paths, indexed by chunk id (0001…).
func convertFrames(outDir string, m *manifest.Manifest, tmpDir string, jobs int) ([]string, error) {
	webps := make([]string, len(m.Chunks))
	errCh := make(chan error, len(m.Chunks))
	ids := make(chan int)
	var wg sync.WaitGroup
	for w := 0; w < jobs; w++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for i := range ids {
				e := m.Chunks[i]
				src := filepath.Join(outDir, filepath.FromSlash(e.Frame))
				dst := filepath.Join(tmpDir, fmt.Sprintf("%04d.webp", i+1))
				if err := webpLossless(src, dst); err != nil {
					errCh <- err
					continue
				}
				webps[i] = dst
			}
		}()
	}
	for i := range m.Chunks {
		ids <- i
	}
	close(ids)
	wg.Wait()
	close(errCh)
	for err := range errCh {
		if err != nil {
			return nil, err
		}
	}
	return webps, nil
}

// webpLossless converts one PNG to lossless WebP with ffmpeg. Lossless means
// the round trip back to PNG is pixel-identical — frame quality never degrades.
func webpLossless(src, dst string) error {
	cmd := command("ffmpeg", "-y", "-hide_banner", "-loglevel", "error",
		"-i", src, "-c:v", "libwebp", "-lossless", "1", dst)
	if out, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("store: webp encode %s: %w\n%s", src, err, out)
	}
	return nil
}

// buildSpec assembles the ytr/spec.json index from the manifest, the output
// tree's transcripts/seeds, and work/<id>/transcripts/full.json provenance.
func buildSpec(opts Options, outDir string, m *manifest.Manifest) (*Spec, error) {
	segs, err := loadSegments(opts.WorkDir, opts.VideoID)
	if err != nil {
		return nil, err
	}

	spec := &Spec{
		SchemaVersion: SchemaVersion,
		VideoID:       m.VideoID,
		SourceURL:     m.SourceURL,
		CreatedAt:     m.CreatedAt,
		PackedAt:      time.Now().UTC().Format(time.RFC3339),
		TotalChunks:   m.TotalChunks,
		TotalDuration: m.TotalDuration,
		Chunks:        make([]Chunk, len(m.Chunks)),
	}
	for i, e := range m.Chunks {
		tr, err := os.ReadFile(filepath.Join(outDir, filepath.FromSlash(e.Transcript)))
		if err != nil {
			return nil, fmt.Errorf("store: transcript %s: %w — run `ytreconstruct all` first", e.Transcript, err)
		}
		spec.Chunks[i] = Chunk{
			ID:         e.ID,
			Start:      e.Start,
			End:        e.End,
			Duration:   e.Duration,
			SourceIDs:  e.SourceIDs,
			Frame:      fmt.Sprintf("%s%04d.webp", framesPrefix, i+1),
			Transcript: string(tr),
			Segments:   partition(segs, e.Start, e.End),
		}
	}

	rec, err := os.ReadFile(filepath.Join(outDir, "reconstruction.md"))
	if err != nil {
		return nil, fmt.Errorf("store: reconstruction.md: %w — run `ytreconstruct all` first", err)
	}
	spec.ReconstructionMD = string(rec)

	// instructions.md is copied by the manifest stage only when the repo's
	// docs/instructions.md exists — tolerate its absence.
	if data, err := os.ReadFile(filepath.Join(outDir, "instructions.md")); err == nil {
		spec.InstructionsMD = string(data)
	}
	return spec, nil
}

// loadSegments parses work/<id>/transcripts/full.json, dropping empty entries.
// A missing file is not an error: when transcription was skipped there is no
// segment provenance, and the store simply carries no segments for that video.
func loadSegments(workDir, videoID string) ([]Segment, error) {
	data, err := os.ReadFile(filepath.Join(workDir, videoID, "transcripts", "full.json"))
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, fmt.Errorf("store: transcripts/full.json: %w — run `ytreconstruct transcribe` first", err)
	}
	var w whisperFull
	if err := json.Unmarshal(data, &w); err != nil {
		return nil, fmt.Errorf("store: transcripts/full.json is not valid whisper output: %w", err)
	}
	var segs []Segment
	for _, t := range w.Transcription {
		if len(t.Text) == 0 {
			continue // whisper's empty entries are silence, not speech
		}
		segs = append(segs, Segment{
			From: int64(t.Offsets.From),
			To:   int64(t.Offsets.To),
			Text: t.Text,
		})
	}
	return segs, nil
}

// partition assigns each segment to the chunk containing its start, exactly
// like the transcript partitioner (a boundary belongs to the earlier chunk).
// A segment that falls outside every chunk is dropped rather than guessed.
func partition(segs []Segment, start, end float64) []Segment {
	lo, hi := int64(start*1000), int64(end*1000)
	var out []Segment
	for _, s := range segs {
		if s.From >= lo && s.From < hi {
			out = append(out, s)
		}
	}
	return out
}

// writeStore writes the .ytr zip atomically: spec.json deflated (compresses
// well), frames stored verbatim (already-compressed WebP — recompressing them
// would waste CPU and gain ~0%, and byte-exact members keep quality bounded).
func writeStore(path string, spec *Spec, webps []string) error {
	part := path + ".part"
	f, err := os.Create(part)
	if err != nil {
		return fmt.Errorf("store: create %s: %w", part, err)
	}
	removePart := true
	defer func() {
		f.Close()
		if removePart {
			os.Remove(part)
		}
	}()

	zw := zip.NewWriter(f)
	hw := &zip.FileHeader{Name: specMember, Method: zip.Deflate}
	w, err := zw.CreateHeader(hw)
	if err != nil {
		return fmt.Errorf("store: %s: %w", specMember, err)
	}
	data, err := json.MarshalIndent(spec, "", "  ")
	if err != nil {
		return fmt.Errorf("store: encode spec: %w", err)
	}
	data = append(data, '\n')
	if _, err := w.Write(data); err != nil {
		return fmt.Errorf("store: write %s: %w", specMember, err)
	}
	for i, webp := range webps {
		fh := &zip.FileHeader{Name: fmt.Sprintf("%s%04d.webp", framesPrefix, i+1), Method: zip.Store}
		fw, err := zw.CreateHeader(fh)
		if err != nil {
			return fmt.Errorf("store: %s: %w", fh.Name, err)
		}
		if err := copyFileTo(webp, fw); err != nil {
			return fmt.Errorf("store: write %s: %w", fh.Name, err)
		}
	}
	if err := zw.Close(); err != nil {
		return fmt.Errorf("store: finalize zip: %w", err)
	}
	if err := f.Close(); err != nil {
		return fmt.Errorf("store: close %s: %w", part, err)
	}
	if err := os.Rename(part, path); err != nil {
		return fmt.Errorf("store: rename %s → %s: %w", part, path, err)
	}
	removePart = false
	return nil
}

func copyFileTo(src string, dst io.Writer) error {
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()
	_, err = io.Copy(dst, in)
	return err
}
