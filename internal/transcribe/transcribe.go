// Package transcribe wraps the local whisper.cpp binary (whisper-cli).
// For each raw chunk's audio slice it produces transcripts/<id>.txt whose
// lines carry absolute timestamps aligned to the video timeline.
package transcribe

import (
	"encoding/json"
	"fmt"
	"io"
	"math"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"sync"

	"ytreconstruct/internal/chunk"
)

// command is an indirection so tests can fake os/exec.
var command = exec.Command

// Options configures a transcribe run.
type Options struct {
	VideoID  string
	WorkDir  string
	Model    string // path to a ggml whisper model
	Threads  int    // whisper inference threads
	Jobs     int    // parallel chunk transcription
	Language string // "" = auto-detect, "en" etc.
}

// segment is one spoken phrase from whisper-cli, in seconds relative to the
// audio slice it was transcribed from.
type segment struct {
	From float64
	To   float64
	Text string
}

// whisperOutput mirrors the JSON schema whisper-cli -oj writes per slice.
// Only the numeric offsets (float seconds) and text are needed; the string
// "timestamps" fields are redundant with offsets.
type whisperOutput struct {
	Transcription []struct {
		Offsets struct {
			From float64 `json:"from"`
			To   float64 `json:"to"`
		} `json:"offsets"`
		Text string `json:"text"`
	} `json:"transcription"`
}

// Run reads work/<video_id>/chunks.json and transcribes every chunk whose
// transcripts/<NNNN>.txt is missing, then writes aligned transcripts. It is
// idempotent: chunks with an existing transcript are skipped.
func Run(opts Options) error {
	if opts.Model == "" {
		return fmt.Errorf("transcribe: no model specified (path to a ggml whisper model)")
	}
	if _, err := os.Stat(opts.Model); err != nil {
		return fmt.Errorf("transcribe: model not found at %s — download a ggml model from https://huggingface.co/ggerganov/whisper.cpp/tree/main", opts.Model)
	}
	jobs := opts.Jobs
	if jobs < 1 {
		jobs = 1
	}

	dir := filepath.Join(opts.WorkDir, opts.VideoID)
	chunks, err := readChunks(dir)
	if err != nil {
		return err
	}

	transcriptsDir := filepath.Join(dir, "transcripts")
	if err := os.MkdirAll(transcriptsDir, 0o755); err != nil {
		return fmt.Errorf("transcribe: mkdir %s: %w", transcriptsDir, err)
	}

	// Resume / idempotency: skip chunks that already have a transcript.
	missing := make([]chunk.Chunk, 0, len(chunks))
	for _, c := range chunks {
		if _, err := os.Stat(transcriptPath(transcriptsDir, c.ID)); err != nil {
			missing = append(missing, c)
		}
	}
	if len(missing) == 0 {
		fmt.Printf("transcribe: all %d transcripts already present, skipping\n", len(chunks))
		return nil
	}

	total := len(chunks)
	ids := make(chan chunk.Chunk)
	errCh := make(chan error, len(missing))
	var (
		wg   sync.WaitGroup
		mu   sync.Mutex
		done int
	)
	for w := 0; w < jobs; w++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for c := range ids {
				if err := transcribeChunk(opts, dir, transcriptsDir, c); err != nil {
					errCh <- err
				}
				mu.Lock()
				done++
				fmt.Printf("transcribe: %d/%d done\n", done, total)
				mu.Unlock()
			}
		}()
	}
	for _, c := range missing {
		ids <- c
	}
	close(ids)
	wg.Wait()
	close(errCh)
	for err := range errCh {
		if err != nil {
			return err
		}
	}
	return nil
}

// readChunks loads work/<video_id>/chunks.json, failing clearly when the
// chunk phase hasn't produced it yet.
func readChunks(dir string) ([]chunk.Chunk, error) {
	path := filepath.Join(dir, "chunks.json")
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("transcribe: %s missing — run `ytreconstruct chunk` first", path)
	}
	var list chunk.ChunkList
	if err := json.Unmarshal(data, &list); err != nil {
		return nil, fmt.Errorf("transcribe: %s is not valid JSON: %w", path, err)
	}
	return list.Chunks, nil
}

// transcribeChunk runs whisper-cli on one chunk's audio slice, aligns the
// returned segments to the absolute video timeline, and writes
// transcripts/<NNNN>.txt.
func transcribeChunk(opts Options, dir, transcriptsDir string, c chunk.Chunk) error {
	audio := filepath.Join(dir, c.Audio)
	if _, err := os.Stat(audio); err != nil {
		return fmt.Errorf("chunk %04d: audio slice missing at %s", c.ID, audio)
	}
	prefix := filepath.Join(transcriptsDir, fmt.Sprintf("%04d", c.ID))
	if err := runWhisper(opts, audio, prefix); err != nil {
		return fmt.Errorf("chunk %04d: %w", c.ID, err)
	}
	segs, err := parseWhisperJSONFile(prefix + ".json")
	if err != nil {
		return fmt.Errorf("chunk %04d: %w", c.ID, err)
	}
	var b strings.Builder
	for _, s := range segs {
		b.WriteString(transcriptLine(c, s))
		b.WriteByte('\n')
	}
	path := transcriptPath(transcriptsDir, c.ID)
	if err := os.WriteFile(path, []byte(b.String()), 0o644); err != nil {
		return fmt.Errorf("chunk %04d: write %s: %w", c.ID, path, err)
	}
	return nil
}

// runWhisper invokes whisper-cli once for one audio slice. stdout/stderr are
// discarded; success is exit code 0 plus the <prefix>.json it writes.
func runWhisper(opts Options, audio, prefix string) error {
	args := []string{"-m", opts.Model, "-f", audio, "-oj", "-of", prefix}
	if opts.Threads > 0 {
		args = append(args, "-t", strconv.Itoa(opts.Threads))
	}
	if opts.Language != "" {
		args = append(args, "-l", opts.Language)
	}
	cmd := command("whisper-cli", args...)
	cmd.Stdout = io.Discard
	cmd.Stderr = io.Discard
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("whisper-cli failed (exit %v) for %s", err, audio)
	}
	jsonPath := prefix + ".json"
	if _, err := os.Stat(jsonPath); err != nil {
		return fmt.Errorf("whisper-cli exited 0 but produced no JSON at %s", jsonPath)
	}
	return nil
}

func parseWhisperJSONFile(path string) ([]segment, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read whisper output %s: %w", path, err)
	}
	segs, err := parseWhisperJSON(data)
	if err != nil {
		return nil, fmt.Errorf("parse whisper output %s: %w", path, err)
	}
	return segs, nil
}

// parseWhisperJSON decodes whisper-cli's -oj JSON into segments, skipping
// entries with no text. offsets.from/to are float seconds relative to the
// slice; JSON numbers may be ints or floats, both handled by float64.
func parseWhisperJSON(data []byte) ([]segment, error) {
	var out whisperOutput
	if err := json.Unmarshal(data, &out); err != nil {
		return nil, err
	}
	segs := make([]segment, 0, len(out.Transcription))
	for _, t := range out.Transcription {
		text := strings.TrimSpace(t.Text)
		if text == "" {
			continue
		}
		segs = append(segs, segment{From: t.Offsets.From, To: t.Offsets.To, Text: text})
	}
	return segs, nil
}

// transcriptLine renders one segment with timestamps aligned to the absolute
// video timeline (the chunk start offset is added to the slice-relative
// offsets).
func transcriptLine(c chunk.Chunk, s segment) string {
	return fmt.Sprintf("[%s --> %s] %s",
		formatTimestamp(s.From+c.Start), formatTimestamp(s.To+c.Start), s.Text)
}

// formatTimestamp renders seconds as HH:MM:SS.mmm (e.g. 83.456 → "00:01:23.456").
func formatTimestamp(seconds float64) string {
	if seconds < 0 {
		seconds = 0
	}
	totalMs := int64(math.Round(seconds * 1000))
	ms := totalMs % 1000
	totalSec := totalMs / 1000
	sec := totalSec % 60
	min := (totalSec / 60) % 60
	hr := totalSec / 3600
	return fmt.Sprintf("%02d:%02d:%02d.%03d", hr, min, sec, ms)
}

func transcriptPath(dir string, id int) string {
	return filepath.Join(dir, fmt.Sprintf("%04d.txt", id))
}
