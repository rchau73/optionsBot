package assistantreview

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// Write saves the week under dir (0600, the monitor's data stays its own):
// <week>.md and <week>.json (the private report), <week>.summary.json (the
// shareable counts) and <week>.golden_candidates.jsonl. Returns the paths.
func Write(dir string, r Report) ([]string, error) {
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return nil, fmt.Errorf("create %s: %w", dir, err)
	}
	var md strings.Builder
	if err := Markdown(&md, r); err != nil {
		return nil, err
	}
	report, err := json.MarshalIndent(r, "", "  ")
	if err != nil {
		return nil, fmt.Errorf("encode report: %w", err)
	}
	summary, err := json.MarshalIndent(Summary(r), "", "  ")
	if err != nil {
		return nil, fmt.Errorf("encode summary: %w", err)
	}
	var cands bytes.Buffer
	for _, c := range Candidates(r) {
		line, err := json.Marshal(c)
		if err != nil {
			return nil, fmt.Errorf("encode candidate: %w", err)
		}
		cands.Write(append(line, '\n'))
	}
	files := []struct {
		name string
		data []byte
	}{
		{r.Week + ".md", []byte(md.String())},
		{r.Week + ".json", append(report, '\n')},
		{r.Week + ".summary.json", append(summary, '\n')},
		{r.Week + ".golden_candidates.jsonl", cands.Bytes()},
	}
	var paths []string
	for _, f := range files {
		path := filepath.Join(dir, f.name)
		if err := os.WriteFile(path, f.data, 0o600); err != nil {
			return nil, fmt.Errorf("write %s: %w", path, err)
		}
		paths = append(paths, path)
	}
	return paths, nil
}
