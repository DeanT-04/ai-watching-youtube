// Package download wraps yt-dlp: given a YouTube URL (or a local video
// file for offline development), produce work/<video_id>/video.mp4 and
// work/<video_id>/audio.wav (16 kHz mono PCM, ready for Whisper).
package download

import (
	"errors"
	"fmt"
	"io"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
)

// command and lookPath are indirections so tests can fake os/exec.
var (
	command  = exec.Command
	lookPath = exec.LookPath
)

// Options configures a download run.
type Options struct {
	URL     string // YouTube URL. Mutually exclusive with File.
	File    string // Local video file to use instead of downloading.
	WorkDir string // Root scratch directory (work/ by default).
}

// idPattern is YouTube's canonical video id: 11 chars of [A-Za-z0-9_-].
var idPattern = regexp.MustCompile(`^[A-Za-z0-9_-]{11}$`)

// VideoID extracts a YouTube video id from a URL. It understands
// youtube.com/watch?v=, youtu.be/, /shorts/, /embed/ and /live/ forms.
func VideoID(rawURL string) (string, error) {
	u, err := url.Parse(rawURL)
	if err != nil {
		return "", fmt.Errorf("download: invalid URL %q: %w", rawURL, err)
	}
	if u.Host == "" {
		return "", fmt.Errorf("download: %q is not a URL (use --file for a local video)", rawURL)
	}

	host := strings.ToLower(u.Host)
	switch {
	case strings.Contains(host, "youtu.be"):
		// youtu.be/<id>
		parts := strings.Split(strings.Trim(u.Path, "/"), "/")
		if len(parts) == 1 && parts[0] != "" {
			return checkID(parts[0])
		}
	case strings.Contains(host, "youtube.com"):
		if id := u.Query().Get("v"); id != "" {
			return checkID(id)
		}
		parts := strings.Split(strings.Trim(u.Path, "/"), "/")
		// /shorts/<id>, /embed/<id>, /live/<id>
		if len(parts) == 2 && (parts[0] == "shorts" || parts[0] == "embed" || parts[0] == "live") {
			return checkID(parts[1])
		}
	}
	return "", fmt.Errorf("download: could not find a video id in %q", rawURL)
}

func checkID(id string) (string, error) {
	if !idPattern.MatchString(id) {
		return "", fmt.Errorf("download: %q is not a valid YouTube video id", id)
	}
	return id, nil
}

// FileID derives a safe directory name from a local video file's basename.
func FileID(path string) string {
	base := strings.TrimSuffix(filepath.Base(path), filepath.Ext(path))
	base = strings.Map(func(r rune) rune {
		switch {
		case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r >= '0' && r <= '9', r == '-', r == '_':
			return r
		default:
			return '-'
		}
	}, base)
	// Collapse runs of dashes from the sanitization, then trim edges.
	base = strings.Join(strings.FieldsFunc(base, func(r rune) bool { return r == '-' }), "-")
	if base == "" {
		return "video"
	}
	return base
}

// Run downloads the video and extracts audio into WorkDir/<video_id>/.
// It is idempotent: if both outputs already exist, it skips them.
func Run(opts Options) error {
	if opts.URL == "" && opts.File == "" {
		return errors.New("download: provide a URL argument or --file")
	}
	if opts.URL != "" && opts.File != "" {
		return errors.New("download: URL and --file are mutually exclusive")
	}
	if opts.WorkDir == "" {
		opts.WorkDir = "work"
	}

	// Check binaries: yt-dlp only when actually downloading; ffmpeg always
	// (audio extraction runs for both URL and local-file mode).
	if opts.URL != "" {
		if _, err := lookPath("yt-dlp"); err != nil {
			return fmt.Errorf("download: required binary %q not found in PATH — install it and retry (https://github.com/yt-dlp/yt-dlp): %w", "yt-dlp", err)
		}
	}
	if _, err := lookPath("ffmpeg"); err != nil {
		return fmt.Errorf("download: required binary %q not found in PATH — install it and retry (https://ffmpeg.org): %w", "ffmpeg", err)
	}

	// Determine the video id and target paths.
	var id string
	switch {
	case opts.File != "":
		if _, err := os.Stat(opts.File); err != nil {
			return fmt.Errorf("download: cannot read --file %q: %w", opts.File, err)
		}
		id = FileID(opts.File)
	default:
		var err error
		id, err = VideoID(opts.URL)
		if err != nil {
			return err
		}
	}

	dir := filepath.Join(opts.WorkDir, id)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return fmt.Errorf("download: mkdir %s: %w", dir, err)
	}

	videoPath := filepath.Join(dir, "video.mp4")
	audioPath := filepath.Join(dir, "audio.wav")

	// Phase A: obtain video.mp4.
	videoExists := fileExists(videoPath)
	if opts.File != "" {
		if videoExists {
			fmt.Printf("download: %s already present, skipping\n", videoPath)
		} else {
			if err := copyFile(opts.File, videoPath); err != nil {
				return fmt.Errorf("download: copy %s: %w", opts.File, err)
			}
			fmt.Printf("download: copied %s -> %s\n", opts.File, videoPath)
		}
	} else {
		if videoExists {
			fmt.Printf("download: %s already present, skipping\n", videoPath)
		} else {
			// Prefer mp4 containers; fall back to anything yt-dlp finds.
			// The produced file may be webm/mkv — we rename it to video.mp4
			// afterwards; ffmpeg probes content, not extensions.
			cmd := command("yt-dlp",
				"-f", "bv*[ext=mp4]+ba[ext=m4a]/b[ext=mp4]/bv*+ba/b",
				"--merge-output-format", "mp4",
				"--no-playlist",
				"-o", filepath.Join(dir, "video.%(ext)s"),
				opts.URL,
			)
			cmd.Stdout = os.Stderr // progress stays visible, nothing buffered
			cmd.Stderr = os.Stderr
			if err := runCmd("yt-dlp", cmd); err != nil {
				return err
			}
			if err := renameToVideoMP4(dir, videoPath); err != nil {
				return err
			}
		}
	}
	if !fileExists(videoPath) {
		return fmt.Errorf("download: %s was not produced; is the URL valid and the video public?", videoPath)
	}

	// Phase B: extract 16 kHz mono PCM audio. Written to a .part file and
	// renamed on success so a killed run never leaves a partial audio.wav
	// that the resume gate would treat as complete.
	if fileExists(audioPath) {
		fmt.Printf("download: %s already present, skipping\n", audioPath)
		return nil
	}
	partPath := audioPath + ".part"
	cmd := command("ffmpeg",
		"-y", "-i", videoPath,
		"-vn", "-ac", "1", "-ar", "16000", "-c:a", "pcm_s16le",
		"-f", "wav", // explicit muxer: the .part suffix must not drive format detection
		partPath,
	)
	cmd.Stdout = os.Stderr
	cmd.Stderr = os.Stderr
	if err := runCmd("ffmpeg", cmd); err != nil {
		return err
	}
	if !fileExists(partPath) {
		return fmt.Errorf("download: %s was not produced by ffmpeg", audioPath)
	}
	if err := os.Rename(partPath, audioPath); err != nil {
		return fmt.Errorf("download: finalize %s: %w", audioPath, err)
	}
	fmt.Printf("download: audio extracted -> %s\n", audioPath)
	return nil
}

// renameToVideoMP4 finds the single final video.* file yt-dlp produced and
// renames it to video.mp4. yt-dlp's in-progress format parts (video.f401.mp4
// and similar) are ignored.
func renameToVideoMP4(dir, videoPath string) error {
	matches, err := filepath.Glob(filepath.Join(dir, "video.*"))
	if err != nil {
		return fmt.Errorf("download: glob video files: %w", err)
	}
	var produced []string
	for _, m := range matches {
		if m == videoPath {
			continue
		}
		// Part files from an interrupted merge contain a dot in their base
		// name (video.f401.mp4); the final file's base is exactly "video".
		if strings.Contains(filepath.Base(m), ".") {
			continue
		}
		produced = append(produced, m)
	}
	if len(produced) == 0 {
		return nil // video.mp4 already exists
	}
	if len(produced) > 1 {
		return fmt.Errorf("download: unexpected multiple outputs from yt-dlp: %v", produced)
	}
	if err := os.Rename(produced[0], videoPath); err != nil {
		return fmt.Errorf("download: rename %s -> %s: %w", produced[0], videoPath, err)
	}
	return nil
}

func runCmd(name string, cmd *exec.Cmd) error {
	if err := cmd.Run(); err != nil {
		var ee *exec.ExitError
		if errors.As(err, &ee) {
			return fmt.Errorf("download: %s failed with exit code %d", name, ee.ExitCode())
		}
		return fmt.Errorf("download: %s: %w", name, err)
	}
	return nil
}

func fileExists(path string) bool {
	info, err := os.Stat(path)
	return err == nil && !info.IsDir()
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
