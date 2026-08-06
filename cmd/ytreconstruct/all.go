package main

import "errors"

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

// runAll orchestrates download → chunk → dedupe + transcribe → manifest,
// resuming from the furthest completed stage. Implemented in Phase 7.
func runAll(o allOptions) error {
	return errors.New("all: not implemented yet")
}
