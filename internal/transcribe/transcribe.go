// Package transcribe wraps the local whisper.cpp binary (whisper-cli).
// The full audio track is transcribed ONCE (one model load, one inference
// pass — no per-chunk process storm), then segments are partitioned into
// per-chunk transcripts/<id>.txt with absolute timestamps on the video
// timeline.
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

	"ytreconstruct/internal/chunk"
	"ytreconstruct/internal/lowprio"
)

// command and LookPath are indirections so tests can fake os/exec.
// LookPath is exported for tests only (the CLI's own tests live in
// package main and cannot touch an unexported var) — production code
// must treat it as read-only.
var (
	command  = lowprio.Command
	LookPath = exec.LookPath
)

// Options configures a transcribe run.
type Options struct {
	VideoID  string
	WorkDir  string
	Model    string // path to a ggml whisper model
	Threads  int    // whisper inference threads
	Jobs     int    // unused by the single-pass design, kept for the CLI contract
	Language string // "" = auto-detect, "en" etc.
}

// segment is one spoken phrase from whisper-cli, in seconds on the video
// timeline (the audio track starts at 0, so slice-relative == absolute).
type segment struct {
	From float64
	To   float64
	Text string
}

// whisperOutput mirrors the JSON schema whisper-cli -oj writes.
type whisperOutput struct {
	Transcription []struct {
		Offsets struct {
			From float64 `json:"from"`
			To   float64 `json:"to"`
		} `json:"offsets"`
		Text string `json:"text"`
	} `json:"transcription"`
}

// fullName is the single-whisper-pass JSON (raw provenance + partition source).
const fullName = "full.json"

// Run transcribes work/<video_id>/audio.wav once and writes per-chunk
// transcripts/<NNNN>.txt. Idempotent: if the full pass exists and every
// chunk transcript exists, it skips (partition is cheap, so it re-runs
// whenever full.json is present).
func Run(opts Options) error {
	if opts.Model == "" {
		return fmt.Errorf("transcribe: no model specified (path to a ggml whisper model)")
	}
	if _, err := LookPath("whisper-cli"); err != nil {
		return fmt.Errorf("transcribe: required binary %q not found in PATH — install whisper.cpp and retry (https://github.com/ggml-org/whisper.cpp): %w", "whisper-cli", err)
	}
	if _, err := os.Stat(opts.Model); err != nil {
		return fmt.Errorf("transcribe: model not found at %s — download a ggml model from https://huggingface.co/ggerganov/whisper.cpp/tree/main", opts.Model)
	}

	dir := filepath.Join(opts.WorkDir, opts.VideoID)
	chunks, err := readChunks(dir)
	if err != nil {
		return err
	}
	if len(chunks) == 0 {
		return fmt.Errorf("transcribe: %s contains no chunks", filepath.Join(dir, "chunks.json"))
	}

	transcriptsDir := filepath.Join(dir, "transcripts")
	if err := os.MkdirAll(transcriptsDir, 0o755); err != nil {
		return fmt.Errorf("transcribe: mkdir %s: %w", transcriptsDir, err)
	}

	fullJSON := filepath.Join(transcriptsDir, fullName)
	if _, err := os.Stat(fullJSON); err != nil {
		if err := runWhisperFull(opts, dir, fullJSON); err != nil {
			return err
		}
	}

	// Partition the full pass into per-chunk transcripts. Segments are
	// assigned to the chunk containing their start time; a segment spanning
	// a boundary belongs to the earlier chunk (timestamps make it precise).
	// Partitioning is milliseconds of work, so it always runs — the whisper
	// pass is the expensive part and that is what resume skips.
	segs, err := parseWhisperJSONFile(fullJSON)
	if err != nil {
		return err
	}

	for _, c := range chunks {
		path := transcriptPath(transcriptsDir, c.ID)
		var b strings.Builder
		for _, s := range segmentsForRange(segs, c.Start, c.End) {
			b.WriteString(transcriptLine(s))
			b.WriteByte('\n')
		}
		if err := os.WriteFile(path, []byte(b.String()), 0o644); err != nil {
			return fmt.Errorf("transcribe: write %s: %w", path, err)
		}
	}
	fmt.Printf("transcribe: partitioned %d segments into %d chunk transcripts\n", len(segs), len(chunks))
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

// runWhisperFull transcribes the whole audio track in one whisper-cli
// process: one model load, one inference pass over the entire video.
func runWhisperFull(opts Options, dir, fullJSON string) error {
	audio := filepath.Join(dir, "audio.wav")
	if _, err := os.Stat(audio); err != nil {
		return fmt.Errorf("transcribe: %s missing — run `ytreconstruct download` first", audio)
	}
	prefix := strings.TrimSuffix(fullJSON, ".json")
	args := []string{"-m", opts.Model, "-f", audio, "-oj", "-of", prefix, "-l", "auto"}
	if opts.Threads > 0 {
		args = append(args, "-t", strconv.Itoa(opts.Threads))
	}
	if opts.Language != "" {
		args = append(args, "-l", opts.Language)
	}
	cmd := command("whisper-cli", args...)
	cmd.Stdout = io.Discard
	cmd.Stderr = io.Discard
	fmt.Printf("transcribe: transcribing full audio track (one pass)...\n")
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("transcribe: whisper-cli failed (exit %v) — check the model and audio", err)
	}
	if _, err := os.Stat(fullJSON); err != nil {
		return fmt.Errorf("transcribe: whisper-cli exited 0 but produced no %s", fullJSON)
	}
	return nil
}

func parseWhisperJSONFile(path string) ([]segment, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("transcribe: read whisper output %s: %w", path, err)
	}
	segs, err := parseWhisperJSON(data)
	if err != nil {
		return nil, fmt.Errorf("transcribe: parse whisper output %s: %w", path, err)
	}
	return segs, nil
}

// parseWhisperJSON decodes whisper-cli's -oj JSON into segments, skipping
// entries with no text. whisper.cpp reports offsets.from/to in milliseconds
// (confirmed against real output: 1216480 → 1216.48 s); we convert to
// seconds on the video timeline (the audio track starts at 0).
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
		segs = append(segs, segment{From: t.Offsets.From / 1000, To: t.Offsets.To / 1000, Text: text})
	}
	return segs, nil
}

// segmentsForRange returns the segments whose start falls in [start, end).
// A segment spanning a boundary belongs to the earlier chunk; its absolute
// timestamps keep it precise for the downstream agent.
func segmentsForRange(segs []segment, start, end float64) []segment {
	var out []segment
	for _, s := range segs {
		if s.From >= start && s.From < end {
			out = append(out, s)
		}
	}
	return out
}

// transcriptLine renders one segment with absolute timestamps.
func transcriptLine(s segment) string {
	return fmt.Sprintf("[%s --> %s] %s",
		formatTimestamp(s.From), formatTimestamp(s.To), s.Text)
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
