// Package manifest builds the final deliverable: output/<video_id>/ with a
// chunks/ tree (frame, transcript, meta per chunk), manifest.json listing
// chunks in strict order, a seeded reconstruction.md and a copy of
// instructions.md. It reunites dedupe's output with transcribe's output.
package manifest

import (
	"encoding/json"
	"fmt"
	"io"
	"math"
	"os"
	"path/filepath"
	"strings"
	"time"

	"ytreconstruct/internal/chunk"
)

// Options configures a manifest run.
type Options struct {
	VideoID   string
	WorkDir   string
	OutputDir string
	SourceURL string // original URL, recorded in manifest.json when known
}

// ChunkMeta is the per-chunk meta.json payload.
type ChunkMeta struct {
	ID         int     `json:"id"`
	Start      float64 `json:"start"`
	End        float64 `json:"end"`
	Duration   float64 `json:"duration"`
	SourceIDs  []int   `json:"source_ids"`
	Frame      string  `json:"frame"`
	Transcript string  `json:"transcript"`
}

// ManifestEntry describes one chunk in manifest.json.
type ManifestEntry struct {
	ID         int     `json:"id"`
	Start      float64 `json:"start"`
	End        float64 `json:"end"`
	Duration   float64 `json:"duration"`
	Frame      string  `json:"frame"`
	Transcript string  `json:"transcript"`
	Meta       string  `json:"meta"`
	SourceIDs  []int   `json:"source_ids"`
}

// Manifest is the top-level output/<video_id>/manifest.json.
type Manifest struct {
	VideoID       string          `json:"video_id"`
	SourceURL     string          `json:"source_url,omitempty"`
	CreatedAt     string          `json:"created_at"`
	TotalChunks   int             `json:"total_chunks"`
	TotalDuration float64         `json:"total_duration"`
	Chunks        []ManifestEntry `json:"chunks"`
}

// Run writes the output tree and manifest.json. Idempotent: an existing
// manifest.json means the deliverable is already built.
func Run(opts Options) error {
	if opts.OutputDir == "" {
		opts.OutputDir = "output"
	}
	workDir := filepath.Join(opts.WorkDir, opts.VideoID)
	dedupedPath := filepath.Join(workDir, "chunks_deduped.json")

	// Prefer the deduped list; fall back to raw chunks if dedupe never ran.
	listPath := dedupedPath
	list, err := readChunkList(listPath)
	if err != nil {
		listPath = filepath.Join(workDir, "chunks.json")
		list, err = readChunkList(listPath)
		if err != nil {
			return fmt.Errorf("manifest: no chunks_deduped.json or chunks.json in %s — run `ytreconstruct dedupe` (and `chunk`) first", workDir)
		}
		fmt.Printf("manifest: no deduped list found, using %s\n", listPath)
	}
	if len(list.Chunks) == 0 {
		return fmt.Errorf("manifest: %s contains no chunks", listPath)
	}

	// Distinguish "transcription was skipped entirely" from "transcription
	// is partial": with zero transcripts we write placeholders (the user
	// passed --skip-transcribe); with some missing we fail loudly rather
	// than silently drop audio content.
	noTranscripts := countTranscripts(workDir) == 0
	if noTranscripts {
		fmt.Printf("manifest: no transcripts found in %s — writing placeholders (was --skip-transcribe used?)\n", filepath.Join(workDir, "transcripts"))
	}

	outDir := filepath.Join(opts.OutputDir, opts.VideoID)
	manifestPath := filepath.Join(outDir, "manifest.json")
	if _, err := os.Stat(manifestPath); err == nil {
		fmt.Printf("manifest: %s already present, skipping\n", manifestPath)
		return nil
	}

	chunksDir := filepath.Join(outDir, "chunks")
	if err := os.MkdirAll(chunksDir, 0o755); err != nil {
		return fmt.Errorf("manifest: mkdir %s: %w", chunksDir, err)
	}

	var (
		m      Manifest
		index  []string // reconstruction.md index lines
		totalD float64
	)
	m.VideoID = opts.VideoID
	m.SourceURL = opts.SourceURL
	m.CreatedAt = time.Now().UTC().Format(time.RFC3339)

	for i, c := range list.Chunks {
		sources := c.SourceIDs
		if len(sources) == 0 {
			sources = []int{c.ID}
		}
		outID := i + 1
		sub := filepath.Join(chunksDir, fmt.Sprintf("%04d", outID))
		if err := os.MkdirAll(sub, 0o755); err != nil {
			return fmt.Errorf("manifest: mkdir %s: %w", sub, err)
		}

		// Frame: copy the representative raw frame.
		frameDst := filepath.Join(sub, "frame.png")
		if err := copyFile(filepath.Join(workDir, c.Frame), frameDst); err != nil {
			return fmt.Errorf("manifest: chunk %d frame: %w", outID, err)
		}

		// Transcript: concatenate the source chunks' transcripts in order.
		transcriptDst := filepath.Join(sub, "transcript.txt")
		if err := mergeTranscripts(workDir, sources, transcriptDst, noTranscripts); err != nil {
			return fmt.Errorf("manifest: chunk %d: %w", outID, err)
		}

		meta := ChunkMeta{
			ID:         outID,
			Start:      c.Start,
			End:        c.End,
			Duration:   c.End - c.Start,
			SourceIDs:  sources,
			Frame:      "frame.png",
			Transcript: "transcript.txt",
		}
		metaRel := filepath.ToSlash(filepath.Join("chunks", fmt.Sprintf("%04d", outID), "meta.json"))
		if err := writeJSON(filepath.Join(outDir, metaRel), meta); err != nil {
			return err
		}

		entryRel := filepath.ToSlash(filepath.Join("chunks", fmt.Sprintf("%04d", outID)))
		m.Chunks = append(m.Chunks, ManifestEntry{
			ID:         outID,
			Start:      c.Start,
			End:        c.End,
			Duration:   c.End - c.Start,
			Frame:      entryRel + "/frame.png",
			Transcript: entryRel + "/transcript.txt",
			Meta:       metaRel,
			SourceIDs:  sources,
		})
		index = append(index, fmt.Sprintf("- [%s → %s]  (source chunk(s) %v)", formatTimestamp(c.Start), formatTimestamp(c.End), sources))
		totalD += c.End - c.Start
	}
	m.TotalChunks = len(m.Chunks)
	m.TotalDuration = totalD

	if err := writeJSON(manifestPath, m); err != nil {
		return err
	}
	if err := writeSeed(outDir, m, index); err != nil {
		return err
	}
	if err := copyInstructions(outDir); err != nil {
		return err
	}
	fmt.Printf("manifest: wrote %s (%d chunks)\n", manifestPath, m.TotalChunks)
	return nil
}

func readChunkList(path string) (chunk.ChunkList, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return chunk.ChunkList{}, err
	}
	var list chunk.ChunkList
	if err := json.Unmarshal(data, &list); err != nil {
		return chunk.ChunkList{}, fmt.Errorf("manifest: %s is not a valid chunk list: %w", path, err)
	}
	return list, nil
}

// mergeTranscripts concatenates the transcripts of the given raw chunk ids
// in order into dst. A missing transcript is a hard error (the deliverable
// must not silently drop audio content), unless transcription was skipped
// entirely — then a placeholder line is written instead.
func mergeTranscripts(workDir string, sourceIDs []int, dst string, skipped bool) error {
	out, err := os.Create(dst)
	if err != nil {
		return err
	}
	defer out.Close()
	if skipped {
		_, err := io.WriteString(out, "[transcript unavailable — transcription was skipped]\n")
		return err
	}
	last := byte(0)
	for _, id := range sourceIDs {
		src := filepath.Join(workDir, "transcripts", fmt.Sprintf("%04d.txt", id))
		f, err := os.Open(src)
		if err != nil {
			return fmt.Errorf("missing transcript %s — run `ytreconstruct transcribe` first", src)
		}
		// Separate concatenated transcripts on their own line, but never
		// introduce a blank line when the previous file already ends with \n.
		if last != 0 && last != '\n' {
			if _, err := io.WriteString(out, "\n"); err != nil {
				f.Close()
				return err
			}
		}
		buf := make([]byte, 64*1024)
		for {
			n, rerr := f.Read(buf)
			if n > 0 {
				last = buf[n-1]
				if _, werr := out.Write(buf[:n]); werr != nil {
					f.Close()
					return werr
				}
			}
			if rerr == io.EOF {
				break
			}
			if rerr != nil {
				f.Close()
				return rerr
			}
		}
		f.Close()
	}
	return nil
}

// countTranscripts returns how many transcripts/*.txt files exist.
func countTranscripts(workDir string) int {
	matches, err := filepath.Glob(filepath.Join(workDir, "transcripts", "*.txt"))
	if err != nil {
		return 0
	}
	return len(matches)
}

// writeSeed writes reconstruction.md with a header for the consuming agent
// and a chunk index.
func writeSeed(outDir string, m Manifest, index []string) error {
	var b strings.Builder
	b.WriteString("# Reconstruction log\n\n")
	b.WriteString("You are watching a video chunk by chunk, in order. For each chunk below:\n")
	b.WriteString("1. Look at the frame in `chunks/NNNN/frame.png` and read `chunks/NNNN/transcript.txt`\n")
	b.WriteString("   (timestamps are absolute on the video timeline).\n")
	b.WriteString("2. Reconstruct what appeared on screen — code, prompts, configs, URLs — verbatim.\n")
	b.WriteString("3. Append your notes under the chunk heading below; never reorder chunks.\n\n")
	b.WriteString(fmt.Sprintf("Video: %s\n", m.VideoID))
	if m.SourceURL != "" {
		b.WriteString(fmt.Sprintf("Source: %s\n", m.SourceURL))
	}
	b.WriteString(fmt.Sprintf("Chunks: %d  •  Duration: %.2fs\n\n", m.TotalChunks, m.TotalDuration))
	b.WriteString("## Chunk index\n\n")
	for _, line := range index {
		b.WriteString(line + "\n")
	}
	b.WriteString("\n---\n\n")
	for i := range m.Chunks {
		rel := filepath.Join("chunks", fmt.Sprintf("%04d", i+1))
		b.WriteString(fmt.Sprintf("## Chunk %04d  [%.3fs → %.3fs]\n\n", i+1, m.Chunks[i].Start, m.Chunks[i].End))
		b.WriteString(fmt.Sprintf("- Frame: `%s`\n- Transcript: `%s`\n", filepath.Join(rel, "frame.png"), filepath.Join(rel, "transcript.txt")))
		b.WriteString("- Reconstruction notes: _write here_\n\n")
	}
	return os.WriteFile(filepath.Join(outDir, "reconstruction.md"), []byte(b.String()), 0o644)
}

// copyInstructions copies the repo-root instructions.md into the output
// dir when present (it is authored in the docs phase).
func copyInstructions(outDir string) error {
	src := filepath.Join("instructions.md")
	if _, err := os.Stat(src); err != nil {
		fmt.Printf("manifest: instructions.md not found next to the binary — skipping copy\n")
		return nil
	}
	return copyFile(src, filepath.Join(outDir, "instructions.md"))
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

func writeJSON(path string, v any) error {
	data, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		return fmt.Errorf("manifest: encode json: %w", err)
	}
	data = append(data, '\n')
	if err := os.WriteFile(path, data, 0o644); err != nil {
		return fmt.Errorf("manifest: write %s: %w", path, err)
	}
	return nil
}

func copyFile(src, dst string) error {
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()
	out, err := os.Create(dst)
	if err != nil {
		return err
	}
	defer out.Close()
	if _, err := io.Copy(out, in); err != nil {
		return err
	}
	return out.Close()
}
