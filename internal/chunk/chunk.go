// Package chunk wraps ffmpeg scene-change detection: it turns
// work/<video_id>/video.mp4 into a list of scene chunks, each with one
// representative frame and a matching audio slice, plus the chunks.json
// data-shape contract every later phase consumes.
package chunk

import "errors"

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

// Run detects scenes and writes chunks_raw/ plus chunks.json.
func Run(opts Options) error {
	return errors.New("chunk: not implemented yet")
}
