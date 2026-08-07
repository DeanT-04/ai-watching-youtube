package main

import (
	"fmt"

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
