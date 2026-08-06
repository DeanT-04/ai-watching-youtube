// Package download wraps yt-dlp: given a YouTube URL (or a local video
// file for offline development), produce work/<video_id>/video.mp4 and
// work/<video_id>/audio.wav (16 kHz mono PCM, ready for Whisper).
package download

import "errors"

// Options configures a download run.
type Options struct {
	URL     string // YouTube URL. Mutually exclusive with File.
	File    string // Local video file to use instead of downloading.
	WorkDir string // Root scratch directory (work/ by default).
}

// Run downloads the video and extracts audio into WorkDir/<video_id>/.
func Run(opts Options) error {
	return errors.New("download: not implemented yet")
}
