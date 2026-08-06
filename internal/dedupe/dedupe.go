// Package dedupe merges visually-static consecutive chunks using a
// perceptual hash (dHash) of each representative frame. Merging extends
// the time range and records which raw chunks were combined; it never
// drops audio or transcript data.
package dedupe

import (
	"encoding/json"
	"fmt"
	"image"
	_ "image/jpeg" // register JPEG so image.Decode recognizes frames
	_ "image/png"  // register PNG so image.Decode recognizes frames
	"math/bits"
	"os"
	"path/filepath"
	"sync"

	"ytreconstruct/internal/chunk"
)

// Options configures a dedupe run.
type Options struct {
	VideoID       string
	WorkDir       string
	HashThreshold int // max Hamming distance to consider two frames identical (default 5)
	Jobs          int // parallel frame hashing workers
}

// Run reads chunks.json and writes chunks_deduped.json in the same shape.
func Run(opts Options) error {
	if opts.HashThreshold < 0 {
		return fmt.Errorf("dedupe: hash-threshold must be >= 0, got %d", opts.HashThreshold)
	}
	if opts.Jobs < 1 {
		opts.Jobs = 1
	}
	dir := filepath.Join(opts.WorkDir, opts.VideoID)

	// Idempotent resume: an existing output means the phase is done.
	outPath := filepath.Join(dir, "chunks_deduped.json")
	if _, err := os.Stat(outPath); err == nil {
		fmt.Printf("dedupe: %s already present, skipping\n", outPath)
		return nil
	}

	inPath := filepath.Join(dir, "chunks.json")
	data, err := os.ReadFile(inPath)
	if err != nil {
		return fmt.Errorf("dedupe: %s missing — run `ytreconstruct chunk` first", inPath)
	}
	var list chunk.ChunkList
	if err := json.Unmarshal(data, &list); err != nil {
		return fmt.Errorf("dedupe: %s is not valid JSON: %w", inPath, err)
	}
	if len(list.Chunks) == 0 {
		return fmt.Errorf("dedupe: %s contains no chunks", inPath)
	}

	// Hash frames exactly as given, so hand absolute paths; existence is
	// checked up front so a broken chunk phase fails loudly.
	work := make([]chunk.Chunk, len(list.Chunks))
	for i, c := range list.Chunks {
		if c.Frame == "" {
			return fmt.Errorf("dedupe: chunk %d has an empty frame path", c.ID)
		}
		abs := filepath.Join(dir, c.Frame)
		if _, err := os.Stat(abs); err != nil {
			return fmt.Errorf("dedupe: frame for chunk %d missing at %s — rerun `ytreconstruct chunk`", c.ID, abs)
		}
		work[i] = c
		work[i].Frame = abs
	}

	hashes, ok := hashFrames(work, opts.Jobs)
	merged := mergeWithHashes(chunk.ChunkList{VideoID: list.VideoID, Source: list.Source, Chunks: work}, opts.HashThreshold, hashes, ok)

	// Restore the documented path contract: Frame is relative to the video
	// dir with forward slashes.
	for i := range merged.Chunks {
		if rel, err := filepath.Rel(dir, merged.Chunks[i].Frame); err == nil {
			merged.Chunks[i].Frame = filepath.ToSlash(rel)
		}
	}

	if err := writeJSON(outPath, merged); err != nil {
		return err
	}
	fmt.Printf("dedupe: %d chunks after merging %d duplicate(s)\n", len(merged.Chunks), len(list.Chunks)-len(merged.Chunks))
	fmt.Printf("dedupe: wrote %s\n", outPath)
	return nil
}

// dhashGrid is the downscale grid used by DHash: 9 columns by 8 rows.
const (
	dhashCols = 9
	dhashRows = 8
	dhashBits = dhashRows * (dhashCols - 1) // 64
)

// DHash reads the image file (PNG or JPEG — use image.Decode) and returns a
// 64-bit difference hash: downscale to 9x8 grayscale by box-averaging each
// cell, then for each of the 8 rows, for x in 0..7, bit = 1 if
// cell(x) > cell(x+1). Standard dHash.
func DHash(framePath string) (uint64, error) {
	f, err := os.Open(framePath)
	if err != nil {
		return 0, fmt.Errorf("dedupe: open frame %s: %w", framePath, err)
	}
	defer f.Close()
	img, _, err := image.Decode(f)
	if err != nil {
		return 0, fmt.Errorf("dedupe: decode frame %s: %w", framePath, err)
	}
	return dHashImage(img), nil
}

// dHashImage hashes an in-memory image, using direct pixel-buffer access for
// the common PNG/JPEG decode results (NRGBA, RGBA, Gray) — roughly 30x
// faster than per-pixel interface calls, which matters on 4K frames.
func dHashImage(img image.Image) uint64 {
	switch im := img.(type) {
	case *image.NRGBA:
		return dHashNRGBA(im)
	case *image.RGBA:
		return dHashRGBA(im)
	case *image.Gray:
		return dHashGray(im)
	}
	return dHashGeneric(img)
}

// dHashNRGBA box-averages straight (non-premultiplied) RGBA pixels.
func dHashNRGBA(img *image.NRGBA) uint64 {
	return dHashPix(img.Bounds(), img.Stride, 4, img.Pix, 0)
}

// dHashRGBA treats premultiplied RGBA like straight RGBA: for opaque
// frames (the normal case) the stored values are identical.
func dHashRGBA(img *image.RGBA) uint64 {
	return dHashPix(img.Bounds(), img.Stride, 4, img.Pix, 0)
}

// dHashGray box-averages single-channel pixels.
func dHashGray(img *image.Gray) uint64 {
	return dHashPix(img.Bounds(), img.Stride, 1, img.Pix, 1)
}

// dHashPix is the shared fast box-average + bit assembly over a raw pixel
// buffer. ch is the channel count per pixel; gray sets whether to use the
// single channel directly instead of RGB weights.
func dHashPix(b image.Rectangle, stride, ch int, pix []byte, gray int) uint64 {
	w, h := b.Dx(), b.Dy()
	cells := make([]uint64, dhashRows*dhashCols)
	for cy := 0; cy < dhashRows; cy++ {
		y0 := b.Min.Y + h*cy/dhashRows
		y1 := b.Min.Y + h*(cy+1)/dhashRows
		if y1 <= y0 { // tiny image: keep the cell in bounds
			y1 = y0 + 1
		}
		for cx := 0; cx < dhashCols; cx++ {
			x0 := b.Min.X + w*cx/dhashCols
			x1 := b.Min.X + w*(cx+1)/dhashCols
			if x1 <= x0 {
				x1 = x0 + 1
			}
			var sum uint64
			for y := y0; y < y1; y++ {
				row := (y-b.Min.Y)*stride + (x0-b.Min.X)*ch
				for i := row; i < row+(x1-x0)*ch; i += ch {
					if gray != 0 {
						sum += uint64(pix[i])
					} else {
						sum += (299*uint64(pix[i]) + 587*uint64(pix[i+1]) + 114*uint64(pix[i+2])) / 1000
					}
				}
			}
			n := uint64((y1 - y0) * (x1 - x0))
			cells[cy*dhashCols+cx] = sum / n
		}
	}

	var hash uint64
	for cy := 0; cy < dhashRows; cy++ {
		for cx := 0; cx < dhashCols-1; cx++ {
			if cells[cy*dhashCols+cx] > cells[cy*dhashCols+cx+1] {
				hash |= uint64(1) << uint(cy*(dhashCols-1)+cx)
			}
		}
	}
	return hash
}

// dHashGeneric is the fallback for unusual image types.
func dHashGeneric(img image.Image) uint64 {
	b := img.Bounds()
	cells := make([]uint64, dhashRows*dhashCols)
	for cy := 0; cy < dhashRows; cy++ {
		y0 := b.Min.Y + b.Dy()*cy/dhashRows
		y1 := b.Min.Y + b.Dy()*(cy+1)/dhashRows
		if y1 <= y0 {
			y1 = y0 + 1
		}
		for cx := 0; cx < dhashCols; cx++ {
			x0 := b.Min.X + b.Dx()*cx/dhashCols
			x1 := b.Min.X + b.Dx()*(cx+1)/dhashCols
			if x1 <= x0 {
				x1 = x0 + 1
			}
			var sum uint64
			for y := y0; y < y1; y++ {
				for x := x0; x < x1; x++ {
					r, g, bl, _ := img.At(x, y).RGBA()
					sum += uint64((299*r + 587*g + 114*bl) / 1000)
				}
			}
			n := uint64((y1 - y0) * (x1 - x0))
			cells[cy*dhashCols+cx] = sum / n
		}
	}
	var hash uint64
	for cy := 0; cy < dhashRows; cy++ {
		for cx := 0; cx < dhashCols-1; cx++ {
			if cells[cy*dhashCols+cx] > cells[cy*dhashCols+cx+1] {
				hash |= uint64(1) << uint(cy*(dhashCols-1)+cx)
			}
		}
	}
	return hash
}

// Hamming returns the number of differing bits between a and b.
func Hamming(a, b uint64) int {
	return bits.OnesCount64(a ^ b)
}

// hashFrames computes a dHash for every chunk's frame in parallel.
// ok[i] is false when a frame could not be hashed.
func hashFrames(list []chunk.Chunk, jobs int) ([]uint64, []bool) {
	hashes := make([]uint64, len(list))
	ok := make([]bool, len(list))
	var wg sync.WaitGroup
	ids := make(chan int)
	for w := 0; w < jobs; w++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for i := range ids {
				if h, err := DHash(list[i].Frame); err == nil {
					hashes[i] = h
					ok[i] = true
				}
			}
		}()
	}
	for i := range list {
		ids <- i
	}
	close(ids)
	wg.Wait()
	return hashes, ok
}

// Merge returns a new ChunkList with visually-static consecutive chunks
// merged, in order. Frame paths are read exactly as given. A chunk joins
// the current run only if its frame is within threshold Hamming distance of
// the run's first (representative) frame, so a run never drifts visually.
// A merged chunk keeps the first chunk's Frame/Audio and sets SourceIDs to
// the raw chunk IDs in order; unmerged chunks are copied untouched. Frames
// that cannot be read are treated as unmergeable.
func Merge(list chunk.ChunkList, threshold int) chunk.ChunkList {
	hashes, ok := hashFrames(list.Chunks, 1)
	return mergeWithHashes(list, threshold, hashes, ok)
}

// mergeWithHashes is the shared merge core.
func mergeWithHashes(list chunk.ChunkList, threshold int, hashes []uint64, ok []bool) chunk.ChunkList {
	merged := make([]chunk.Chunk, 0, len(list.Chunks))
	for i := 0; i < len(list.Chunks); {
		c := list.Chunks[i]
		src := []int{c.ID}
		j := i + 1
		for j < len(list.Chunks) && ok[i] && ok[j] && Hamming(hashes[i], hashes[j]) <= threshold {
			src = append(src, list.Chunks[j].ID)
			j++
		}
		if len(src) > 1 {
			c.End = list.Chunks[j-1].End
			c.SourceIDs = src
		}
		merged = append(merged, c)
		i = j
	}
	out := list
	out.Chunks = merged
	return out
}

func writeJSON(path string, v any) error {
	data, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		return fmt.Errorf("dedupe: encode json: %w", err)
	}
	data = append(data, '\n')
	if err := os.WriteFile(path, data, 0o644); err != nil {
		return fmt.Errorf("dedupe: write %s: %w", path, err)
	}
	return nil
}
