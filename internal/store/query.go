package store

import (
	"fmt"
	"strings"
)

// QueryResult is one matching chunk plus the segment lines that hit.
type QueryResult struct {
	Chunk   Chunk    `json:"chunk"`
	Matches []string `json:"matches"`
}

// Query returns chunks whose transcript or segments contain term
// (case-insensitive), in playback order. A time window t1..t2 (seconds;
// either may be nil for open-ended) narrows the search. The timestamp
// index is the ordered chunk list itself: the window is applied by
// skipping chunks that end before t1 or start at/after t2.
func Query(opts Options, term string, t1, t2 *float64) ([]QueryResult, error) {
	spec, err := Read(opts)
	if err != nil {
		return nil, err
	}
	needle := strings.ToLower(term)
	var out []QueryResult
	for _, c := range spec.Chunks {
		if t1 != nil && c.End <= *t1 {
			continue
		}
		if t2 != nil && c.Start >= *t2 {
			continue
		}
		var matches []string
		for _, s := range c.Segments {
			if strings.Contains(strings.ToLower(s.Text), needle) {
				matches = append(matches, fmt.Sprintf("[%s --> %s] %s", formatMS(s.From), formatMS(s.To), s.Text))
			}
		}
		// Fall back to the whole transcript line when the structured
		// segments don't carry the match (e.g. transcription was skipped).
		if len(matches) == 0 && strings.Contains(strings.ToLower(c.Transcript), needle) {
			matches = append(matches, c.Transcript)
		}
		if len(matches) > 0 {
			out = append(out, QueryResult{Chunk: c, Matches: matches})
		}
	}
	return out, nil
}

// formatMS renders milliseconds as HH:MM:SS.mmm (e.g. 83456 → "00:01:23.456").
func formatMS(ms int64) string {
	if ms < 0 {
		ms = 0
	}
	m := ms % 1000
	totalSec := ms / 1000
	sec := totalSec % 60
	min := (totalSec / 60) % 60
	hr := totalSec / 3600
	return fmt.Sprintf("%02d:%02d:%02d.%03d", hr, min, sec, m)
}
