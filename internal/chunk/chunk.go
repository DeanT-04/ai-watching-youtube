// Package chunk wraps ffmpeg scene-change detection: it turns
// work/<video_id>/video.mp4 into a list of scene chunks, each with one
// representative frame and a matching audio slice, plus the chunks.json
// data-shape contract every later phase consumes.
package chunk

import (
	"bufio"
	"encoding/binary"
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

	"ytreconstruct/internal/lowprio"
)

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
		if _, err := LookPath(bin); err != nil {
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

	// Extract one representative frame per chunk (parallel, jobs-capped),
	// then slice the full audio track into per-chunk files in pure Go.
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
				if err := extractFrame(videoPath, cdir, c); err != nil {
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

	if err := sliceAudioWAV(audioPath, rawDir, chunks); err != nil {
		return err
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

// extractFrame writes one representative frame for a chunk. It first tries
// -skip_frame nokey: the output is the first keyframe at/after the chunk
// start — encoders place keyframes at scene cuts, so this is usually the
// scene's first frame, and it avoids decoding forward from the previous
// keyframe (which is what made per-chunk extraction slow on high-res video).
// When the video has no keyframe at/after the target (sparse-keyframe
// content, or the final chunk near EOF) it falls back to an accurate seek.
func extractFrame(videoPath, dir string, c Chunk) error {
	start := fmt.Sprintf("%.3f", c.Start)
	frame := filepath.Join(dir, "frame.png")

	try := func(extraArgs ...string) error {
		args := append([]string{"-y", "-hide_banner", "-loglevel", "error"},
			"-ss", start)
		args = append(args, extraArgs...)
		args = append(args, "-i", videoPath, "-frames:v", "1", frame)
		cmd := command("ffmpeg", args...)
		cmd.Stdout = io.Discard
		cmd.Stderr = io.Discard
		if err := cmd.Run(); err != nil {
			return err
		}
		_, err := os.Stat(frame)
		return err
	}

	if err := try("-skip_frame", "nokey"); err == nil {
		return nil
	}
	if err := try(); err != nil {
		return fmt.Errorf("chunk %d: frame extraction: %w", c.ID, err)
	}
	return nil
}

// wavInfo is the parsed header of a WAV file we can slice in pure Go.
type wavInfo struct {
	dataSize    int64 // bytes of PCM payload
	byteRate    int64 // bytes per second
	blockAlign  int64 // bytes per sample frame
	dataOffset  int64 // absolute offset of the payload in the file
	audioFormat uint16
	channels    uint16
	sampleRate  uint32
	bitsPer     uint16
}

// parseWAV reads the RIFF header. Returns an error for non-WAV input.
func parseWAV(path string) (wavInfo, error) {
	var info wavInfo
	f, err := os.Open(path)
	if err != nil {
		return info, err
	}
	defer f.Close()
	var hdr [44]byte
	if _, err := io.ReadFull(f, hdr[:]); err != nil {
		return info, fmt.Errorf("not a WAV file (short header): %w", err)
	}
	if string(hdr[0:4]) != "RIFF" || string(hdr[8:12]) != "WAVE" || string(hdr[12:16]) != "fmt " {
		return info, fmt.Errorf("not a WAV file (bad RIFF/WAVE/fmt magic)")
	}
	info.audioFormat = binary.LittleEndian.Uint16(hdr[20:22])
	info.channels = binary.LittleEndian.Uint16(hdr[22:24])
	info.sampleRate = binary.LittleEndian.Uint32(hdr[24:28])
	info.byteRate = int64(binary.LittleEndian.Uint32(hdr[28:32]))
	info.blockAlign = int64(binary.LittleEndian.Uint16(hdr[32:34]))
	info.bitsPer = binary.LittleEndian.Uint16(hdr[34:36])
	// Walk the chunks after the fmt chunk to locate the payload. The fmt
	// chunk ends at 12 + 8 + fmtSize (36 for the standard 16-byte fmt),
	// where the next chunk header ("data") begins.
	pos := 12 + 8 + int64(binary.LittleEndian.Uint32(hdr[16:20]))
	for {
		if _, err := f.Seek(pos, io.SeekStart); err != nil {
			return info, fmt.Errorf("no data chunk in WAV: %w", err)
		}
		var chunkHdr [8]byte
		if _, err := io.ReadFull(f, chunkHdr[:]); err != nil {
			return info, fmt.Errorf("no data chunk in WAV: %w", err)
		}
		size := int64(binary.LittleEndian.Uint32(chunkHdr[4:8]))
		if string(chunkHdr[0:4]) == "data" {
			info.dataSize = size
			info.dataOffset = pos + 8 // payload starts after this chunk header
			return info, nil
		}
		pos += 8 + size + size%2 // chunks are padded to even sizes
	}
}

// sliceAudioWAV slices the full 16 kHz mono s16le audio track into
// chunks_raw/NNNN/audio.wav with pure byte-range copies — zero processes,
// milliseconds of work. If the WAV is not the expected format it falls back
// to per-chunk ffmpeg slicing.
func sliceAudioWAV(audioPath, rawDir string, chunks []Chunk) error {
	info, err := parseWAV(audioPath)
	if err != nil || info.audioFormat != 1 || info.channels != 1 || info.bitsPer != 16 {
		fmt.Printf("chunk: audio not 16k mono s16le PCM (%v) — using ffmpeg per-chunk slicing\n", err)
		return sliceAudioWAVffmpeg(audioPath, rawDir, chunks)
	}

	data, err := os.ReadFile(audioPath)
	if err != nil {
		return fmt.Errorf("chunk: read %s: %w", audioPath, err)
	}
	payload := int64(len(data)) - info.dataOffset
	if info.dataSize > 0 && info.dataSize < payload {
		payload = info.dataSize
	}

	for _, c := range chunks {
		start := int64(c.Start * float64(info.byteRate))
		end := int64(c.End * float64(info.byteRate))
		start = clampTo(start, 0, payload)
		end = clampTo(end, 0, payload)
		start -= start % info.blockAlign
		end -= end % info.blockAlign
		if end <= start {
			end = start // empty slice: still write a valid empty WAV
		}
		outPath := filepath.Join(rawDir, fmt.Sprintf("%04d", c.ID), "audio.wav")
		if err := writeWAVSlice(outPath, data[info.dataOffset+start:info.dataOffset+end], info); err != nil {
			return fmt.Errorf("chunk %d: audio slice: %w", c.ID, err)
		}
	}
	return nil
}

func clampTo(v, lo, hi int64) int64 {
	if v < lo {
		return lo
	}
	if v > hi {
		return hi
	}
	return v
}

// writeWAVSlice writes a self-contained 16 kHz mono s16le WAV from a raw
// PCM byte range (same format as our audio.wav, so the header is standard).
func writeWAVSlice(path string, pcm []byte, info wavInfo) error {
	var hdr [44]byte
	copy(hdr[0:4], "RIFF")
	binary.LittleEndian.PutUint32(hdr[4:8], uint32(36+len(pcm)))
	copy(hdr[8:12], "WAVE")
	copy(hdr[12:16], "fmt ")
	binary.LittleEndian.PutUint32(hdr[16:20], 16)
	binary.LittleEndian.PutUint16(hdr[20:22], 1) // PCM
	binary.LittleEndian.PutUint16(hdr[22:24], 1) // mono
	binary.LittleEndian.PutUint32(hdr[24:28], 16000)
	binary.LittleEndian.PutUint32(hdr[28:32], 32000) // byte rate
	binary.LittleEndian.PutUint16(hdr[32:34], 2)     // block align
	binary.LittleEndian.PutUint16(hdr[34:36], 16)    // bits
	copy(hdr[36:40], "data")
	binary.LittleEndian.PutUint32(hdr[40:44], uint32(len(pcm)))
	out, err := os.Create(path)
	if err != nil {
		return err
	}
	defer out.Close()
	if _, err := out.Write(hdr[:]); err != nil {
		return err
	}
	_, err = out.Write(pcm)
	return err
}

// sliceAudioWAVffmpeg is the fallback for non-standard WAV input.
func sliceAudioWAVffmpeg(audioPath, rawDir string, chunks []Chunk) error {
	for _, c := range chunks {
		cdir := filepath.Join(rawDir, fmt.Sprintf("%04d", c.ID))
		slice := filepath.Join(cdir, "audio.wav")
		cmd := command("ffmpeg", "-y", "-hide_banner", "-loglevel", "error",
			"-ss", fmt.Sprintf("%.3f", c.Start), "-to", fmt.Sprintf("%.3f", c.End),
			"-i", audioPath, "-f", "wav", slice)
		cmd.Stdout = io.Discard
		cmd.Stderr = io.Discard
		if err := cmd.Run(); err != nil {
			return fmt.Errorf("chunk %d: audio slice: %w", c.ID, err)
		}
		if _, err := os.Stat(slice); err != nil {
			return fmt.Errorf("chunk %d: audio slice not produced", c.ID)
		}
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
