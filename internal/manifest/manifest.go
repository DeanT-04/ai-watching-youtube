// Package manifest builds the final deliverable: output/<video_id>/ with a
// chunks/ tree (frame, transcript, meta per chunk), manifest.json listing
// chunks in strict order, a seeded reconstruction.md and a copy of
// instructions.md. It reunites dedupe's output with transcribe's output.
package manifest

import "errors"

// Options configures a manifest run.
type Options struct {
	VideoID   string
	WorkDir   string
	OutputDir string
	SourceURL string // original URL, recorded in manifest.json when known
}

// Run writes the output tree and manifest.json.
func Run(opts Options) error {
	return errors.New("manifest: not implemented yet")
}
