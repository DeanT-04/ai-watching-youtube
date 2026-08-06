// Command ytreconstruct turns a YouTube video into an ordered set of
// (frame, audio transcript, timestamp) chunks on disk so a downstream
// agent can "watch" the video in order and reconstruct what appeared
// on screen.
package main

import (
	"fmt"
	"os"
	"path/filepath"
	"runtime"

	"github.com/spf13/cobra"

	"ytreconstruct/internal/chunk"
	"ytreconstruct/internal/dedupe"
	"ytreconstruct/internal/download"
	"ytreconstruct/internal/manifest"
	"ytreconstruct/internal/transcribe"
)

func main() {
	if err := newRootCmd().Execute(); err != nil {
		fmt.Fprintln(os.Stderr, "error:", err)
		os.Exit(1)
	}
}

func newRootCmd() *cobra.Command {
	root := &cobra.Command{
		Use:   "ytreconstruct",
		Short: "Turn a YouTube video into ordered (frame, transcript, timestamp) chunks",
		Long: `ytreconstruct downloads a YouTube video, splits it into scene chunks,
extracts one frame per chunk, transcribes each chunk's audio with a local
whisper.cpp binary, dedupes visually-static periods, and writes an ordered
manifest a downstream agent can use to reconstruct the video in sequence.

Everything runs offline after the video is downloaded. No cloud APIs.`,
		SilenceUsage: true,
	}

	// Persistent flags shared by every subcommand. Values are read inside
	// each RunE via cmd.Flags() — never captured at construction time.
	// Default jobs leave half the cores free so the machine stays usable
	// while the pipeline grinds; children additionally run BELOW_NORMAL.
	pf := root.PersistentFlags()
	pf.String("work-dir", "work", "scratch directory (never committed)")
	pf.String("output-dir", "output", "deliverable directory (never committed)")
	pf.IntP("jobs", "j", defaultJobs(), "parallel workers (default: half the CPU count)")
	pf.Bool("skip-transcribe", false, "skip the transcription stage")

	root.AddCommand(
		newDownloadCmd(),
		newChunkCmd(),
		newDedupeCmd(),
		newTranscribeCmd(),
		newManifestCmd(),
		newAllCmd(),
	)
	return root
}

func newDownloadCmd() *cobra.Command {
	var file string
	cmd := &cobra.Command{
		Use:   "download <url>",
		Short: "Download a video to work/<video_id>/",
		Args:  cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			url := ""
			if len(args) == 1 {
				url = args[0]
			}
			return download.Run(download.Options{
				URL:     url,
				File:    file,
				WorkDir: flagString(cmd, "work-dir"),
			})
		},
	}
	cmd.Flags().StringVar(&file, "file", "", "use a local video file instead of a URL (offline development)")
	return cmd
}

func newChunkCmd() *cobra.Command {
	var sceneThreshold float64
	cmd := &cobra.Command{
		Use:   "chunk <video_id>",
		Short: "Split the video into scene chunks (frame + audio slice each)",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return chunk.Run(chunk.Options{
				VideoID:        args[0],
				WorkDir:        flagString(cmd, "work-dir"),
				SceneThreshold: sceneThreshold,
				Jobs:           flagInt(cmd, "jobs"),
			})
		},
	}
	cmd.Flags().Float64Var(&sceneThreshold, "scene-threshold", 0.4, "ffmpeg scene-change threshold, 0..1 (higher = fewer cuts)")
	return cmd
}

func newDedupeCmd() *cobra.Command {
	var hashThreshold int
	cmd := &cobra.Command{
		Use:   "dedupe <video_id>",
		Short: "Merge visually-static consecutive chunks",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return dedupe.Run(dedupe.Options{
				VideoID:       args[0],
				WorkDir:       flagString(cmd, "work-dir"),
				HashThreshold: hashThreshold,
				Jobs:          flagInt(cmd, "jobs"),
			})
		},
	}
	cmd.Flags().IntVar(&hashThreshold, "hash-threshold", 5, "max dHash Hamming distance to treat frames as identical")
	return cmd
}

func newTranscribeCmd() *cobra.Command {
	var (
		model    string
		threads  int
		language string
	)
	cmd := &cobra.Command{
		Use:   "transcribe <video_id>",
		Short: "Transcribe each chunk's audio with whisper-cli",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if model == "" {
				return fmt.Errorf("transcribe: --model is required (path to a ggml whisper model)")
			}
			return transcribe.Run(transcribe.Options{
				VideoID:  args[0],
				WorkDir:  flagString(cmd, "work-dir"),
				Model:    model,
				Threads:  threads,
				Jobs:     flagInt(cmd, "jobs"),
				Language: language,
			})
		},
	}
	cmd.Flags().StringVar(&model, "model", defaultModelPath(), "path to a ggml whisper model")
	cmd.Flags().IntVar(&threads, "threads", 4, "whisper inference threads")
	cmd.Flags().StringVar(&language, "language", "", "whisper language hint ('' = auto-detect)")
	return cmd
}

func newManifestCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "manifest <video_id>",
		Short: "Write the final ordered output tree + manifest.json",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return manifest.Run(manifest.Options{
				VideoID:   args[0],
				WorkDir:   flagString(cmd, "work-dir"),
				OutputDir: flagString(cmd, "output-dir"),
				Jobs:      flagInt(cmd, "jobs"),
			})
		},
	}
	return cmd
}

func newAllCmd() *cobra.Command {
	var (
		file           string
		sceneThreshold float64
		hashThreshold  int
		model          string
		threads        int
		language       string
	)
	cmd := &cobra.Command{
		Use:   "all <url>",
		Short: "Run the full pipeline: download → chunk → dedupe + transcribe → manifest",
		Args:  cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			url := ""
			if len(args) == 1 {
				url = args[0]
			}
			if model == "" {
				return fmt.Errorf("all: --model is required (path to a ggml whisper model)")
			}
			return runAll(allOptions{
				url:            url,
				file:           file,
				workDir:        flagString(cmd, "work-dir"),
				outputDir:      flagString(cmd, "output-dir"),
				jobs:           flagInt(cmd, "jobs"),
				skipTranscribe: flagBool(cmd, "skip-transcribe"),
				sceneThreshold: sceneThreshold,
				hashThreshold:  hashThreshold,
				model:          model,
				threads:        threads,
				language:       language,
			})
		},
	}
	f := cmd.Flags()
	f.StringVar(&file, "file", "", "use a local video file instead of a URL (offline development)")
	f.Float64Var(&sceneThreshold, "scene-threshold", 0.4, "ffmpeg scene-change threshold, 0..1")
	f.IntVar(&hashThreshold, "hash-threshold", 5, "max dHash Hamming distance to treat frames as identical")
	f.StringVar(&model, "model", defaultModelPath(), "path to a ggml whisper model")
	f.IntVar(&threads, "threads", 4, "whisper inference threads")
	f.StringVar(&language, "language", "", "whisper language hint ('' = auto-detect)")
	return cmd
}

// flag helpers read inherited persistent flags at run time (never cached
// at command-construction time, which would go stale).
func flagString(cmd *cobra.Command, name string) string {
	v, _ := cmd.Flags().GetString(name)
	return v
}

func flagInt(cmd *cobra.Command, name string) int {
	v, _ := cmd.Flags().GetInt(name)
	return v
}

func flagBool(cmd *cobra.Command, name string) bool {
	v, _ := cmd.Flags().GetBool(name)
	return v
}

// defaultJobs leaves half the cores free: the machine stays responsive
// while the pipeline runs, and children are BELOW_NORMAL priority anyway.
func defaultJobs() int {
	if n := runtime.NumCPU() / 2; n >= 1 {
		return n
	}
	return 1
}

// defaultModelPath returns the conventional whisper model location. The
// small q8_0 quantized model is the default: roughly half the word-error
// rate of base on technical content (verified A/B on the real video), at
// ~3x the CPU time (14 min vs 4 min for a 20-min video) — an acceptable
// trade for the transcription quality the downstream agent depends on.
func defaultModelPath() string {
	home, err := os.UserHomeDir()
	if err != nil {
		return ""
	}
	return filepath.Join(home, ".cache", "ytreconstruct", "whisper", "ggml-small-q8_0.bin")
}
