// Package transcribe wraps the local whisper.cpp binary (whisper-cli).
// For each raw chunk's audio slice it produces transcripts/<id>.txt whose
// lines carry absolute timestamps aligned to the video timeline.
package transcribe

import "errors"

// Options configures a transcribe run.
type Options struct {
	VideoID  string
	WorkDir  string
	Model    string // path to a ggml whisper model
	Threads  int    // whisper inference threads
	Jobs     int    // parallel chunk transcription
	Language string // "" = auto-detect, "en" etc.
}

// Run reads chunks.json and writes transcripts/<id>.txt per chunk.
func Run(opts Options) error {
	return errors.New("transcribe: not implemented yet")
}
