package store

import (
	"archive/zip"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
)

// storePath returns the .ytr file for a video.
func storePath(opts Options) string {
	return filepath.Join(opts.StoreDir, opts.VideoID+"."+FileExt)
}

// openStore opens a store file and parses + schema-gates its spec. The
// returned ReadCloser must be closed by the caller.
func openStore(opts Options) (*zip.ReadCloser, *Spec, error) {
	path := storePath(opts)
	zr, err := zip.OpenReader(path)
	if err != nil {
		return nil, nil, fmt.Errorf("store: open %s: %w (does it exist? run `ytreconstruct store pack %s`)", path, err, opts.VideoID)
	}
	spec, err := readSpec(zr)
	if err != nil {
		zr.Close()
		return nil, nil, err
	}
	return zr, spec, nil
}

// readSpec reads and schema-gates ytr/spec.json from an open archive.
func readSpec(zr *zip.ReadCloser) (*Spec, error) {
	data, err := readMember(zr, specMember)
	if err != nil {
		return nil, err
	}
	var spec Spec
	if err := json.Unmarshal(data, &spec); err != nil {
		return nil, fmt.Errorf("store: %s is not valid: %w", specMember, err)
	}
	if spec.SchemaVersion != SchemaVersion {
		return nil, fmt.Errorf("store: %s has unsupported schema_version %d (this build reads %d)", specMember, spec.SchemaVersion, SchemaVersion)
	}
	return &spec, nil
}

// readMember returns one member's bytes, verifying its CRC on the way.
func readMember(zr *zip.ReadCloser, name string) ([]byte, error) {
	for _, f := range zr.File {
		if f.Name != name {
			continue
		}
		rc, err := f.Open()
		if err != nil {
			return nil, fmt.Errorf("store: open %s: %w", name, err)
		}
		defer rc.Close()
		data, err := io.ReadAll(rc)
		if err != nil {
			return nil, fmt.Errorf("store: %s is corrupt: %w", name, err)
		}
		return data, nil
	}
	return nil, fmt.Errorf("store: %s missing from archive", name)
}

// Read opens a store and returns its parsed spec — everything an agent needs
// to "watch" the video without the frames.
func Read(opts Options) (*Spec, error) {
	zr, spec, err := openStore(opts)
	if err != nil {
		return nil, err
	}
	zr.Close()
	return spec, nil
}

// FrameBytes returns the raw WebP bytes of one chunk's frame member.
func FrameBytes(opts Options, chunkID int) ([]byte, error) {
	zr, _, err := openStore(opts)
	if err != nil {
		return nil, err
	}
	defer zr.Close()
	name := fmt.Sprintf("%s%04d.webp", framesPrefix, chunkID)
	data, err := readMember(zr, name)
	if err != nil {
		return nil, fmt.Errorf("store: chunk %04d frame: %w", chunkID, err)
	}
	return data, nil
}

// FramePNG extracts a chunk's frame and re-encodes it as lossless PNG into
// dst. The WebP lossless round trip is pixel-identical, so OCR (e.g.
// scripts/ocr.ps1) and agent image tools see exactly the original frame.
func FramePNG(opts Options, chunkID int, dst string) error {
	if _, err := LookPath("ffmpeg"); err != nil {
		return fmt.Errorf("store: required binary %q not found in PATH — install ffmpeg and retry: %w", "ffmpeg", err)
	}
	webp, err := FrameBytes(opts, chunkID)
	if err != nil {
		return err
	}
	tmp, err := os.CreateTemp("", "ytreconstruct-frame-*.webp")
	if err != nil {
		return fmt.Errorf("store: temp file: %w", err)
	}
	tmpPath := tmp.Name()
	defer os.Remove(tmpPath)
	if _, err := tmp.Write(webp); err != nil {
		tmp.Close()
		return fmt.Errorf("store: write temp: %w", err)
	}
	if err := tmp.Close(); err != nil {
		return fmt.Errorf("store: close temp: %w", err)
	}
	cmd := command("ffmpeg", "-y", "-hide_banner", "-loglevel", "error",
		"-i", tmpPath, "-frames:v", "1", dst)
	if out, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("store: frame decode to %s: %w\n%s", dst, err, out)
	}
	return nil
}

// Verify checks a store's integrity: schema version, every member's CRC
// (read in full), every referenced frame member present, and no unreferenced
// frame members. Returns a report on success, an error naming the first
// problem otherwise.
func Verify(opts Options) (*Report, error) {
	zr, spec, err := openStore(opts)
	if err != nil {
		return nil, err
	}
	defer zr.Close()

	refs := make(map[string]bool, len(spec.Chunks))
	for _, c := range spec.Chunks {
		refs[c.Frame] = true
	}
	seen := make(map[string]bool)
	var total int64
	for _, f := range zr.File {
		rc, err := f.Open()
		if err != nil {
			return nil, fmt.Errorf("store: %s: %w", f.Name, err)
		}
		n, err := io.Copy(io.Discard, rc)
		rc.Close()
		if err != nil {
			return nil, fmt.Errorf("store: %s: corrupt member: %w", f.Name, err)
		}
		total += n
		if strings.HasPrefix(f.Name, framesPrefix) {
			seen[f.Name] = true
		}
	}
	for ref := range refs {
		if !seen[ref] {
			return nil, fmt.Errorf("store: %s: missing frame member %q", storePath(opts), ref)
		}
	}
	for name := range seen {
		if !refs[name] {
			return nil, fmt.Errorf("store: %s: unexpected frame member %q", storePath(opts), name)
		}
	}
	return &Report{
		VideoID:    spec.VideoID,
		StorePath:  storePath(opts),
		Chunks:     len(spec.Chunks),
		Frames:     len(seen),
		TotalBytes: total,
	}, nil
}
