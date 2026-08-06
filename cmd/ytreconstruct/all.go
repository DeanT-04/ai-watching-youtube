package main

import (
	"fmt"
	"path/filepath"
	"sync"

	"ytreconstruct/internal/chunk"
	"ytreconstruct/internal/dedupe"
	"ytreconstruct/internal/download"
	"ytreconstruct/internal/manifest"
	"ytreconstruct/internal/transcribe"
)

// allOptions carries every flag the `all` subcommand accepts.
type allOptions struct {
	url            string
	file           string
	workDir        string
	outputDir      string
	jobs           int
	skipTranscribe bool
	sceneThreshold float64
	hashThreshold  int
	model          string
	threads        int
	language       string
}

// runAll orchestrates download → chunk → (dedupe ∥ transcribe) → manifest.
// Every phase is individually idempotent, so re-running `all` resumes from
// the furthest completed stage without redoing expensive work.
func runAll(o allOptions) error {
	if o.url == "" && o.file == "" {
		return fmt.Errorf("all: provide a URL argument or --file")
	}
	if o.url != "" && o.file != "" {
		return fmt.Errorf("all: URL and --file are mutually exclusive")
	}

	fmt.Printf("==> [1/5] download\n")
	if err := download.Run(download.Options{URL: o.url, File: o.file, WorkDir: o.workDir}); err != nil {
		return err
	}

	id, err := videoIDFor(o)
	if err != nil {
		return err
	}

	fmt.Printf("==> [2/5] chunk\n")
	if err := chunk.Run(chunk.Options{
		VideoID:        id,
		WorkDir:        o.workDir,
		SceneThreshold: o.sceneThreshold,
		Jobs:           o.jobs,
	}); err != nil {
		return err
	}

	// Phases 3 and 4 are independent: dedupe needs only the frames,
	// transcribe needs only the audio slices. Run them concurrently.
	fmt.Printf("==> [3/5] dedupe  +  [4/5] transcribe\n")
	var wg sync.WaitGroup
	errCh := make(chan error, 2)
	wg.Add(2)

	go func() {
		defer wg.Done()
		if err := dedupe.Run(dedupe.Options{
			VideoID:       id,
			WorkDir:       o.workDir,
			HashThreshold: o.hashThreshold,
		}); err != nil {
			errCh <- err
		}
	}()

	go func() {
		defer wg.Done()
		if o.skipTranscribe {
			fmt.Println("transcribe: skipped (--skip-transcribe)")
			return
		}
		if err := transcribe.Run(transcribe.Options{
			VideoID:  id,
			WorkDir:  o.workDir,
			Model:    o.model,
			Threads:  o.threads,
			Jobs:     o.jobs,
			Language: o.language,
		}); err != nil {
			errCh <- err
		}
	}()

	wg.Wait()
	close(errCh)
	for err := range errCh {
		if err != nil {
			return err
		}
	}

	fmt.Printf("==> [5/5] manifest\n")
	if err := manifest.Run(manifest.Options{
		VideoID:   id,
		WorkDir:   o.workDir,
		OutputDir: o.outputDir,
		SourceURL: o.url,
		Jobs:      o.jobs,
	}); err != nil {
		return err
	}

	fmt.Printf("done. Deliverable: %s\n", filepath.Join(o.outputDir, id))
	return nil
}

// videoIDFor derives the video id from the URL (or the local file name)
// after the download phase has run.
func videoIDFor(o allOptions) (string, error) {
	if o.file != "" {
		return download.FileID(o.file), nil
	}
	id, err := download.VideoID(o.url)
	if err != nil {
		return "", err
	}
	return id, nil
}
