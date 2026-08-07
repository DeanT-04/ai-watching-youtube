package store

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
)

// LibraryEntry is one row of store/library.json: enough to find and list a
// packed video without opening its .ytr file.
type LibraryEntry struct {
	VideoID       string  `json:"video_id"`
	SourceURL     string  `json:"source_url"`
	CreatedAt     string  `json:"created_at"`
	PackedAt      string  `json:"packed_at"`
	TotalChunks   int     `json:"total_chunks"`
	TotalDuration float64 `json:"total_duration"`
	StoreFile     string  `json:"store_file"`
}

// Library is store/library.json.
type Library struct {
	SchemaVersion int            `json:"schema_version"`
	Videos        []LibraryEntry `json:"videos"`
}

// libraryPath is the per-machine index file.
func libraryPath(storeDir string) string {
	return filepath.Join(storeDir, "library.json")
}

// LoadLibrary reads the library index; a missing file is an empty library.
func LoadLibrary(storeDir string) (Library, error) {
	data, err := os.ReadFile(libraryPath(storeDir))
	if err != nil {
		if os.IsNotExist(err) {
			return Library{SchemaVersion: SchemaVersion}, nil
		}
		return Library{}, fmt.Errorf("store: read %s: %w", libraryPath(storeDir), err)
	}
	var lib Library
	if err := json.Unmarshal(data, &lib); err != nil {
		return Library{}, fmt.Errorf("store: %s is corrupt — delete it and re-run `store pack`: %w", libraryPath(storeDir), err)
	}
	return lib, nil
}

// SaveLibrary writes the library atomically (.part + rename).
func SaveLibrary(storeDir string, lib Library) error {
	if err := os.MkdirAll(storeDir, 0o755); err != nil {
		return fmt.Errorf("store: mkdir %s: %w", storeDir, err)
	}
	data, err := json.MarshalIndent(lib, "", "  ")
	if err != nil {
		return fmt.Errorf("store: encode library: %w", err)
	}
	data = append(data, '\n')
	path := libraryPath(storeDir)
	part := path + ".part"
	if err := os.WriteFile(part, data, 0o644); err != nil {
		return fmt.Errorf("store: write %s: %w", part, err)
	}
	if err := os.Rename(part, path); err != nil {
		return fmt.Errorf("store: rename %s → %s: %w", part, path, err)
	}
	return nil
}

// updateLibrary upserts one video's entry and saves — unless the entry is
// already exactly what's on disk, in which case nothing is rewritten (so
// idempotent re-runs leave library.json untouched).
func updateLibrary(storeDir string, entry LibraryEntry) error {
	lib, err := LoadLibrary(storeDir)
	if err != nil {
		return err
	}
	for i := range lib.Videos {
		if lib.Videos[i].VideoID == entry.VideoID {
			if lib.Videos[i] == entry {
				return nil // unchanged — do not rewrite the index
			}
			lib.Videos[i] = entry
			return SaveLibrary(storeDir, lib)
		}
	}
	lib.Videos = append(lib.Videos, entry)
	return SaveLibrary(storeDir, lib)
}

// List returns the library entries sorted by video id.
func List(opts Options) ([]LibraryEntry, error) {
	if opts.StoreDir == "" {
		opts.StoreDir = "store"
	}
	lib, err := LoadLibrary(opts.StoreDir)
	if err != nil {
		return nil, err
	}
	out := append([]LibraryEntry(nil), lib.Videos...)
	sort.Slice(out, func(i, j int) bool { return out[i].VideoID < out[j].VideoID })
	return out, nil
}
