package main

import (
	"encoding/json"
	"fmt"
	"strconv"
	"strings"

	"github.com/spf13/cobra"

	"ytreconstruct/internal/store"
)

// newStoreCmd returns the `store` command group: pack, list and verify the
// single-file .ytr store (see docs/storage-format.md).
func newStoreCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "store",
		Short: "Pack and query the single-file .ytr store",
		Long: `The store is the queryable form of a finished video: one .ytr file
per video (zip container holding a ytr/spec.json index and WebP-lossless
frames) plus a tiny library.json index across videos. An agent "watches" a
video with ` + "`store dump <id>`" + ` and pulls frames with ` + "`store frame <id> <NNNN> out.png`" + `.`,
	}
	cmd.AddCommand(
		newStorePackCmd(),
		newStoreListCmd(),
		newStoreVerifyCmd(),
		newStoreDumpCmd(),
		newStoreQueryCmd(),
		newStoreFrameCmd(),
	)
	return cmd
}

func newStorePackCmd() *cobra.Command {
	var force bool
	cmd := &cobra.Command{
		Use:   "pack <video_id>",
		Short: "Pack output/<video_id>/ into store/<video_id>.ytr",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			rep, err := store.Run(store.Options{
				VideoID:   args[0],
				WorkDir:   flagString(cmd, "work-dir"),
				OutputDir: flagString(cmd, "output-dir"),
				StoreDir:  flagString(cmd, "store-dir"),
				Force:     force,
				Jobs:      flagInt(cmd, "jobs"),
			})
			if err != nil {
				return err
			}
			if rep.Skipped {
				fmt.Printf("store: %s already present, skipping (--force to rebuild)\n", rep.StorePath)
				return nil
			}
			fmt.Printf("store: packed %s (%d chunks, %d frames, %s)\n", rep.StorePath, rep.Chunks, rep.Frames, humanBytes(rep.TotalBytes))
			return nil
		},
	}
	cmd.Flags().BoolVar(&force, "force", false, "rebuild even if the store file already exists")
	return cmd
}

func newStoreListCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "list",
		Short: "List packed videos from store/library.json",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			entries, err := store.List(store.Options{StoreDir: flagString(cmd, "store-dir")})
			if err != nil {
				return err
			}
			if len(entries) == 0 {
				fmt.Println("store: library is empty — run `ytreconstruct store pack <video_id>` first")
				return nil
			}
			for _, e := range entries {
				fmt.Printf("%s  %d chunks  %.1fs  %s\n", e.VideoID, e.TotalChunks, e.TotalDuration, e.StoreFile)
			}
			return nil
		},
	}
	return cmd
}

func newStoreVerifyCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "verify <video_id>",
		Short: "Verify a store file's integrity (CRCs, schema, frame members)",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			rep, err := store.Verify(store.Options{
				VideoID:  args[0],
				StoreDir: flagString(cmd, "store-dir"),
			})
			if err != nil {
				return err
			}
			fmt.Printf("store: %s OK — %d chunks, %d frames, %s verified\n", rep.StorePath, rep.Chunks, rep.Frames, humanBytes(rep.TotalBytes))
			return nil
		},
	}
	return cmd
}

func newStoreDumpCmd() *cobra.Command {
	var asJSON bool
	cmd := &cobra.Command{
		Use:   "dump <video_id>",
		Short: "Print the whole video in order: metadata + every chunk's transcript",
		Long: `Dump is the agent's "watch the video" command: one call prints the
ordered story — metadata, then every chunk's timespan, source chunks and
transcript — so the agent can replay the video without touching the frame
files. Add --json for the raw ytr/spec.json index.`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			opts := store.Options{VideoID: args[0], StoreDir: flagString(cmd, "store-dir")}
			spec, err := store.Read(opts)
			if err != nil {
				return err
			}
			if asJSON {
				data, err := json.MarshalIndent(spec, "", "  ")
				if err != nil {
					return fmt.Errorf("store dump: encode: %w", err)
				}
				fmt.Println(string(data))
				return nil
			}
			printDump(spec)
			return nil
		},
	}
	cmd.Flags().BoolVar(&asJSON, "json", false, "print the raw ytr/spec.json index instead of the summary")
	return cmd
}

// printDump renders the ordered story in a greppable, human-readable form.
func printDump(spec *store.Spec) {
	fmt.Printf("Video: %s\n", spec.VideoID)
	if spec.SourceURL != "" {
		fmt.Printf("Source: %s\n", spec.SourceURL)
	}
	fmt.Printf("Chunks: %d  •  Duration: %.2fs  •  Packed: %s\n", spec.TotalChunks, spec.TotalDuration, spec.PackedAt)
	for _, c := range spec.Chunks {
		fmt.Printf("== Chunk %04d  [%.3fs → %.3fs]  (source chunk(s) %v)\n", c.ID, c.Start, c.End, c.SourceIDs)
		if c.Transcript != "" {
			fmt.Print(c.Transcript)
		}
		fmt.Println()
	}
}

func newStoreQueryCmd() *cobra.Command {
	var (
		grep     string
		rangeArg string
		asJSON   bool
	)
	cmd := &cobra.Command{
		Use:   "query <video_id>",
		Short: "Search chunks by transcript text (ordered, timestamped)",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if grep == "" {
				return fmt.Errorf("store query: --grep is required (substring to search)")
			}
			t1, t2, err := parseRange(rangeArg)
			if err != nil {
				return err
			}
			opts := store.Options{VideoID: args[0], StoreDir: flagString(cmd, "store-dir")}
			results, err := store.Query(opts, grep, t1, t2)
			if err != nil {
				return err
			}
			if asJSON {
				data, err := json.MarshalIndent(results, "", "  ")
				if err != nil {
					return fmt.Errorf("store query: encode: %w", err)
				}
				fmt.Println(string(data))
				return nil
			}
			if len(results) == 0 {
				fmt.Printf("store query: no chunks match %q\n", grep)
				return nil
			}
			for _, r := range results {
				fmt.Printf("== Chunk %04d  [%.3fs → %.3fs]\n", r.Chunk.ID, r.Chunk.Start, r.Chunk.End)
				for _, m := range r.Matches {
					fmt.Println(m)
				}
			}
			return nil
		},
	}
	cmd.Flags().StringVar(&grep, "grep", "", "substring to search transcripts and segments (case-insensitive)")
	cmd.Flags().StringVar(&rangeArg, "range", "", "time window \"t1,t2\" in seconds (e.g. 60,120)")
	cmd.Flags().BoolVar(&asJSON, "json", false, "emit results as JSON")
	return cmd
}

// parseRange parses --range "t1,t2" into open-ended float pointers.
func parseRange(s string) (t1, t2 *float64, err error) {
	if s == "" {
		return nil, nil, nil
	}
	parts := strings.Split(s, ",")
	if len(parts) != 2 {
		return nil, nil, fmt.Errorf("store query: --range must be \"t1,t2\" in seconds, got %q", s)
	}
	var a, b float64
	if a, err = strconv.ParseFloat(strings.TrimSpace(parts[0]), 64); err != nil {
		return nil, nil, fmt.Errorf("store query: --range t1 %q is not a number", parts[0])
	}
	if b, err = strconv.ParseFloat(strings.TrimSpace(parts[1]), 64); err != nil {
		return nil, nil, fmt.Errorf("store query: --range t2 %q is not a number", parts[1])
	}
	if a > b {
		return nil, nil, fmt.Errorf("store query: --range t1 (%v) must be <= t2 (%v)", a, b)
	}
	return &a, &b, nil
}

func newStoreFrameCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "frame <video_id> <chunk> [out.png]",
		Short: "Extract a chunk frame as PNG (pixel-identical, for OCR/vision)",
		Long: `Frame materializes one chunk's stored frame as a PNG file (lossless
WebP → PNG round trip, so it is pixel-identical to the original). Hand the
path to scripts/ocr.ps1 or any image tool. Default output name:
<video_id>-<NNNN>.png in the current directory.`,
		Args: cobra.RangeArgs(2, 3),
		RunE: func(cmd *cobra.Command, args []string) error {
			chunkID, err := strconv.Atoi(args[1])
			if err != nil {
				return fmt.Errorf("store frame: chunk must be a number, got %q", args[1])
			}
			dst := fmt.Sprintf("%s-%04d.png", args[0], chunkID)
			if len(args) == 3 {
				dst = args[2]
			}
			if err := store.FramePNG(store.Options{
				VideoID:  args[0],
				StoreDir: flagString(cmd, "store-dir"),
			}, chunkID, dst); err != nil {
				return err
			}
			fmt.Printf("store: wrote %s (chunk %04d frame, PNG)\n", dst, chunkID)
			return nil
		},
	}
	return cmd
}

// humanBytes renders a byte count for logs (e.g. 280 MiB).
func humanBytes(n int64) string {
	switch {
	case n >= 1<<30:
		return fmt.Sprintf("%.1f GiB", float64(n)/(1<<30))
	case n >= 1<<20:
		return fmt.Sprintf("%.1f MiB", float64(n)/(1<<20))
	case n >= 1<<10:
		return fmt.Sprintf("%.1f KiB", float64(n)/(1<<10))
	default:
		return fmt.Sprintf("%d B", n)
	}
}
