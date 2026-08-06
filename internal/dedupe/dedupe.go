// Package dedupe merges visually-static consecutive chunks using a
// perceptual hash (dHash) of each representative frame. Merging extends
// the time range and records which raw chunks were combined; it never
// drops audio or transcript data.
package dedupe

import "errors"

// Options configures a dedupe run.
type Options struct {
	VideoID       string
	WorkDir       string
	HashThreshold int // max Hamming distance to consider two frames identical (default 5)
}

// Run reads chunks.json and writes chunks_deduped.json in the same shape.
func Run(opts Options) error {
	return errors.New("dedupe: not implemented yet")
}
