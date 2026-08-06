// Package chunk wraps ffmpeg scene-change detection: it turns
// work/<video_id>/video.mp4 into a list of scene chunks, each with one
// representative frame and a matching audio slice, plus the chunks.json
// data-shape contract every later phase consumes.
package chunk

import (
	"bufio"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"sync"
)

// command and lookPath are indirections so tests can fake os/exec.
var (
	command  = exec.Command
	lookPath = exec.LookPath
)

// Chunk is one scene segment. Paths are relative to work/<video_id>/.
type Chunk struct {
	ID        int     `json:"id"`
	Start     float64 `json:"start"`                // seconds, inclusive
	End       float64 `json:"end"`                  // seconds, exclusive
	Frame     string  `json:"frame"`                // e.g. chunks_raw/0001/frame.png
	Audio     string  `json:"audio"`                // e.g. chunks_raw/0001/audio.wav
	SourceIDs []int   `json:"source_ids,omitempty"` // set on deduped chunks
}

// ChunkList is the on-disk contract: work/<video_id>/chunks.json.
type ChunkList struct {
	VideoID string  `json:"video_id"`
	Source  string  `json:"source"` // video file, relative to work/<video_id>/
	Chunks  []Chunk `json:"chunks"`
}

// Options configures a chunking run.
type Options struct {
	VideoID        string
	WorkDir        string
	SceneThreshold float64 // ffmpeg scdet threshold, 0..1 (default 0.4)
	Jobs           int     // parallel extraction workers
}

// scdTimeRe matches ffmpeg's scdet scene-change line:
//
//	[Parsed_scdet_0 @ 0x...] lavfi.scd.time: 12.340000
var scdTimeRe = regexp.MustCompile(`lavfi\.scd\.time:\s*([0-9.]+)`)

// Run detects scenes and writes chunks_raw/ plus chunks.json.
// It is idempotent: an existing chunks.json means the phase is done.
func Run(opts Options) error {
	if opts.SceneThreshold <= 0 || opts.SceneThreshold > 1 {
		return fmt.Errorf("chunk: scene-threshold must be in (0, 1], got %v", opts.SceneThreshold)
	}
	if opts.Jobs < 1 {
		opts.Jobs = 1
	}
	for _, bin := range []string{"ffmpeg", "ffprobe"} {
		if _, err := lookPath(bin); err != nil {
			return fmt.Errorf("chunk: required binary %q not found in PATH — install it and retry (https://ffmpeg.org): %w", bin, err)
		}
	}
	dir := filepath.Join(opts.WorkDir, opts.VideoID)
	videoPath := filepath.Join(dir, "video.mp4")
	audioPath := filepath.Join(dir, "audio.wav")

	for _, p := range []string{videoPath, audioPath} {
		if info, err := os.Stat(p); err != nil || info.IsDir() {
			return fmt.Errorf("chunk: %s missing — run `ytreconstruct download` first (or resume)", p)
		}
	}

	manifestPath := filepath.Join(dir, "chunks.json")
	if _, err := os.Stat(manifestPath); err == nil {
		fmt.Printf("chunk: %s already present, skipping\n", manifestPath)
		return nil
	}

	boundaries, err := sceneBoundaries(videoPath, opts.SceneThreshold)
	if err != nil {
		return err
	}
	total, err := videoDuration(videoPath)
	if err != nil {
		return err
	}
	if total <= 0 {
		return fmt.Errorf("chunk: could not determine video duration for %s", videoPath)
	}

	chunks := buildChunks(boundaries, total)
	if len(chunks) == 0 {
		return fmt.Errorf("chunk: no chunks produced (duration %.2fs, boundaries %v)", total, boundaries)
	}
	for i := range chunks {
		sub := fmt.Sprintf("chunks_raw/%04d", chunks[i].ID)
		chunks[i].Frame = filepath.ToSlash(filepath.Join(sub, "frame.png"))
		chunks[i].Audio = filepath.ToSlash(filepath.Join(sub, "audio.wav"))
	}
	fmt.Printf("chunk: %d scene chunk(s) from %.2fs video\n", len(chunks), total)

	rawDir := filepath.Join(dir, "chunks_raw")
	if err := os.MkdirAll(rawDir, 0o755); err != nil {
		return fmt.Errorf("chunk: mkdir %s: %w", rawDir, err)
	}

	// Extract frame + audio per chunk, capped at opts.Jobs workers.
	ids := make(chan int)
	errCh := make(chan error, len(chunks))
	var wg sync.WaitGroup
	for w := 0; w < opts.Jobs; w++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for id := range ids {
				c := chunks[id-1]
				cdir := filepath.Join(rawDir, fmt.Sprintf("%04d", id))
				if err := os.MkdirAll(cdir, 0o755); err != nil {
					errCh <- err
					continue
				}
				if err := extractChunk(videoPath, audioPath, cdir, c); err != nil {
					errCh <- err
				}
			}
		}()
	}
	for _, c := range chunks {
		ids <- c.ID
	}
	close(ids)
	wg.Wait()
	close(errCh)
	for err := range errCh {
		if err != nil {
			return fmt.Errorf("chunk: extraction failed: %w", err)
		}
	}

	list := ChunkList{VideoID: opts.VideoID, Source: "video.mp4", Chunks: chunks}
	if err := writeJSON(manifestPath, list); err != nil {
		return err
	}
	fmt.Printf("chunk: wrote %s\n", manifestPath)
	return nil
}

// sceneBoundaries returns the scene-cut timestamps in seconds, always
// including 0.0. It runs ffmpeg's scdet filter (ffmpeg >= 6.0) and
// streams/parses its stderr line by line (never buffered in memory).
func sceneBoundaries(videoPath string, threshold float64) ([]float64, error) {
	cmd := command("ffmpeg",
		"-hide_banner", "-i", videoPath,
		"-vf", fmt.Sprintf("scdet=threshold=%g", threshold),
		"-f", "null", "-",
	)
	stderr, err := cmd.StderrPipe()
	if err != nil {
		return nil, fmt.Errorf("chunk: scene detection pipe: %w", err)
	}
	if err := cmd.Start(); err != nil {
		return nil, fmt.Errorf("chunk: scene detection: %w", err)
	}

	var cuts []float64
	seen := map[float64]bool{}
	sc := bufio.NewScanner(stderr)
	sc.Buffer(make([]byte, 0, 64*1024), 1024*1024)
	for sc.Scan() {
		m := scdTimeRe.FindStringSubmatch(sc.Text())
		if m == nil {
			continue
		}
		t, err := strconv.ParseFloat(m[1], 64)
		if err != nil || t <= 0.01 { // skip the synthetic t=0 event
			continue
		}
		if !seen[t] {
			seen[t] = true
			cuts = append(cuts, t)
		}
	}
	pipeErr := sc.Err()
	runErr := cmd.Wait()
	if runErr != nil {
		return nil, fmt.Errorf("chunk: ffmpeg scene detection failed (exit %v) — is the video readable?", runErr)
	}
	if pipeErr != nil {
		return nil, fmt.Errorf("chunk: reading scene detection output: %w", pipeErr)
	}

	sort.Float64s(cuts)
	return append([]float64{0.0}, cuts...), nil
}

// videoDuration probes the duration with ffprobe.
func videoDuration(videoPath string) (float64, error) {
	cmd := command("ffprobe",
		"-v", "error", "-show_entries", "format=duration",
		"-of", "default=noprint_wrappers=1:nokey=1", videoPath,
	)
	out, err := cmd.Output()
	if err != nil {
		return 0, fmt.Errorf("chunk: ffprobe duration: %w", err)
	}
	d, err := strconv.ParseFloat(strings.TrimSpace(string(out)), 64)
	if err != nil {
		return 0, fmt.Errorf("chunk: ffprobe returned unparsable duration %q", strings.TrimSpace(string(out)))
	}
	return d, nil
}

// buildChunks converts cut timestamps + total duration into the ordered
// chunk list. The first chunk starts at 0; the last ends at duration.
func buildChunks(boundaries []float64, total float64) []Chunk {
	sorted := make([]float64, 0, len(boundaries))
	seen := map[float64]bool{}
	for _, b := range boundaries {
		if b < 0 || b > total {
			continue
		}
		if !seen[b] {
			seen[b] = true
			sorted = append(sorted, b)
		}
	}
	sort.Float64s(sorted)

	var chunks []Chunk
	for i := 0; i < len(sorted); i++ {
		start := sorted[i]
		end := total
		if i+1 < len(sorted) {
			end = sorted[i+1]
		}
		if end <= start {
			continue
		}
		chunks = append(chunks, Chunk{
			ID:    len(chunks) + 1,
			Start: start,
			End:   end,
		})
	}
	return chunks
}

// extractChunk writes one representative frame and the audio slice for a
// chunk. Frame uses fast seek (-ss before -i); the representative frame is
// the nearest keyframe at/after the scene cut, which is the scene's first
// on-screen content for typical encoders. Audio slices come from the
// already-16k-mono audio.wav, so slicing is sample-accurate and cheap.
func extractChunk(videoPath, audioPath, dir string, c Chunk) error {
	start := fmt.Sprintf("%.3f", c.Start)
	end := fmt.Sprintf("%.3f", c.End)

	frame := filepath.Join(dir, "frame.png")
	cmd := command("ffmpeg", "-y", "-hide_banner", "-loglevel", "error",
		"-ss", start, "-i", videoPath,
		"-frames:v", "1", frame)
	cmd.Stdout = io.Discard
	cmd.Stderr = io.Discard
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("chunk %d: frame extraction: %w", c.ID, err)
	}
	if _, err := os.Stat(frame); err != nil {
		return fmt.Errorf("chunk %d: frame not produced", c.ID)
	}

	slice := filepath.Join(dir, "audio.wav")
	cmd = command("ffmpeg", "-y", "-hide_banner", "-loglevel", "error",
		"-ss", start, "-to", end, "-i", audioPath, slice)
	cmd.Stdout = io.Discard
	cmd.Stderr = io.Discard
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("chunk %d: audio slice: %w", c.ID, err)
	}
	if _, err := os.Stat(slice); err != nil {
		return fmt.Errorf("chunk %d: audio slice not produced", c.ID)
	}
	return nil
}

func writeJSON(path string, v any) error {
	data, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		return fmt.Errorf("chunk: encode json: %w", err)
	}
	data = append(data, '\n')
	if err := os.WriteFile(path, data, 0o644); err != nil {
		return fmt.Errorf("chunk: write %s: %w", path, err)
	}
	return nil
}
