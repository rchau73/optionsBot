// Command assistantreview writes the assistant's weekly quality review (#438)
// from the monitor's review log: answers by kind, guard rejections, flag
// rates, the review list of every flagged and "I don't know" answer, gap
// candidates grouped by the nearest help passage (helpkb.Search over
// help-index.json) and golden-set candidates. It reads the two files only:
// no model, no network, and the log is never written.
//
//	go run ./cmd/assistantreview -log data/monitor/assistant.jsonl -week 2026-W44
//
// → <out>/<week>.md|json (private: questions and answers), <week>.summary.json
// (counts and percentages only, what scripts/agents/usage.sh posts) and
// <week>.golden_candidates.jsonl. Prints one RESULT line.
package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"optionsbot/internal/assistantreview"
	"optionsbot/internal/evidence"
	"optionsbot/internal/helpkb"
)

func main() {
	logPath := flag.String("log", "data/monitor/assistant.jsonl", "the monitor's assistant review log")
	week := flag.String("week", "", "ISO week, e.g. 2026-W44 (default: the last complete week)")
	out := flag.String("out", "", "folder for the report (default: assistant_reviews/ beside the log)")
	index := flag.String("index", "frontend/content/help-index.json", "the help index the gap candidates are matched against")
	flag.Parse()
	if *out == "" {
		*out = filepath.Join(filepath.Dir(*logPath), "assistant_reviews")
	}
	if err := run(*logPath, *week, *out, *index); err != nil {
		fmt.Fprintln(os.Stderr, "assistantreview:", err)
		os.Exit(1)
	}
}

func run(logPath, week, out, indexPath string) error {
	w := evidence.LastWeek(time.Now())
	if week != "" {
		var err error
		if w, err = evidence.ParseWeek(week); err != nil {
			return err
		}
	}
	lg, err := assistantreview.Load(logPath)
	if err != nil {
		return err
	}
	ix, err := loadIndex(indexPath)
	if err != nil {
		return err
	}
	r := assistantreview.Build(lg, w.From, w.To, w.Label, ix)
	paths, err := assistantreview.Write(out, r)
	if err != nil {
		return err
	}
	for _, p := range paths {
		fmt.Println("wrote", p)
	}
	s := assistantreview.Summary(r)
	fmt.Printf("RESULT week=%s answers=%d flagged=%d review=%d skipped=%d\n", w.Label, s.Answers, s.Flagged, s.ReviewItems, s.Skipped)
	return nil
}

func loadIndex(path string) (*helpkb.Index, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read help index: %w", err)
	}
	var f helpkb.File
	if err := json.Unmarshal(b, &f); err != nil {
		return nil, fmt.Errorf("decode %s: %w", path, err)
	}
	return helpkb.NewIndex(f.Chunks), nil
}
