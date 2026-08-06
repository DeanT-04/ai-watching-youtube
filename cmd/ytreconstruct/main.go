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
	var (
		workDir        string
		outputDir      string
		jobs           int
		skipTranscript bool
	)

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

	// Persistent flags shared by every subcommand.
	pf := root.PersistentFlags()
	pf.StringVar(&workDir, "work-dir", "work", "scratch directory (never committed)")
	pf.StringVar(&outputDir, "output-dir", "output", "deliverable directory (never committed)")
	pf.IntVarP(&jobs, "jobs", "j", runtime.NumCPU(), "parallel workers (default: CPU count)")
	pf.BoolVar(&skipTranscript, "skip-transcribe", false, "skip the transcription stage")

	root.AddCommand(
		newDownloadCmd(workDir),
		newChunkCmd(workDir),
		newDedupeCmd(workDir),
		newTranscribeCmd(workDir, jobs),
		newManifestCmd(workDir, outputDir),
		newAllCmd(workDir, outputDir, jobs, skipTranscript),
	)
	return root
}

func newDownloadCmd(workDir string) *cobra.Command {
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
				WorkDir: workDir,
			})
		},
	}
	cmd.Flags().StringVar(&file, "file", "", "use a local video file instead of a URL (offline development)")
	return cmd
}

func newChunkCmd(workDir string) *cobra.Command {
	var sceneThreshold float64
	cmd := &cobra.Command{
		Use:   "chunk <video_id>",
		Short: "Split the video into scene chunks (frame + audio slice each)",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return chunk.Run(chunk.Options{
				VideoID:        args[0],
				WorkDir:        workDir,
				SceneThreshold: sceneThreshold,
				Jobs:           1, // wired to --jobs in Phase 7
			})
		},
	}
	cmd.Flags().Float64Var(&sceneThreshold, "scene-threshold", 0.4, "ffmpeg scene-change threshold, 0..1 (higher = fewer cuts)")
	return cmd
}

func newDedupeCmd(workDir string) *cobra.Command {
	var hashThreshold int
	cmd := &cobra.Command{
		Use:   "dedupe <video_id>",
		Short: "Merge visually-static consecutive chunks",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return dedupe.Run(dedupe.Options{
				VideoID:       args[0],
				WorkDir:       workDir,
				HashThreshold: hashThreshold,
			})
		},
	}
	cmd.Flags().IntVar(&hashThreshold, "hash-threshold", 5, "max dHash Hamming distance to treat frames as identical")
	return cmd
}

func newTranscribeCmd(workDir string, jobs int) *cobra.Command {
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
				WorkDir:  workDir,
				Model:    model,
				Threads:  threads,
				Jobs:     jobs,
				Language: language,
			})
		},
	}
	cmd.Flags().StringVar(&model, "model", defaultModelPath(), "path to a ggml whisper model")
	cmd.Flags().IntVar(&threads, "threads", 4, "whisper inference threads")
	cmd.Flags().StringVar(&language, "language", "", "whisper language hint ('' = auto-detect)")
	return cmd
}

func newManifestCmd(workDir, outputDir string) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "manifest <video_id>",
		Short: "Write the final ordered output tree + manifest.json",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return manifest.Run(manifest.Options{
				VideoID:   args[0],
				WorkDir:   workDir,
				OutputDir: outputDir,
			})
		},
	}
	return cmd
}

func newAllCmd(workDir, outputDir string, jobs int, skipTranscribe bool) *cobra.Command {
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
				workDir:        workDir,
				outputDir:      outputDir,
				jobs:           jobs,
				skipTranscribe: skipTranscribe,
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

// defaultModelPath returns the conventional whisper model location.
func defaultModelPath() string {
	home, err := os.UserHomeDir()
	if err != nil {
		return ""
	}
	return filepath.Join(home, ".cache", "ytreconstruct", "whisper", "ggml-base.bin")
}
