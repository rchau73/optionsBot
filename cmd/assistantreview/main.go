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
// <week>.golden_candidates.jsonl. Prints one line per closed row (its date and
// how, for the run's log) and one RESULT line.
//
// The subcommand close records how a person closed an answer of the review list
// (#704), one line in <out>/closures.jsonl, and refuses an unknown id, an unknown
// how, an empty ref or a second closure of one id:
//
//	go run ./cmd/assistantreview close -id <answer id> -how help_fix|golden_case|answer_was_right -ref "..."
package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"optionsbot/internal/assistantreview"
	"optionsbot/internal/evidence"
	"optionsbot/internal/helpkb"
)

func main() {
	var err error
	if len(os.Args) > 1 && os.Args[1] == "close" {
		err = closeAnswer(os.Args[2:])
	} else {
		logPath := flag.String("log", defaultLog, "the monitor's assistant review log")
		week := flag.String("week", "", "ISO week, e.g. 2026-W44 (default: the last complete week)")
		out := flag.String("out", "", "folder for the report (default: assistant_reviews/ beside the log)")
		index := flag.String("index", "frontend/content/help-index.json", "the help index the gap candidates are matched against")
		flag.Parse()
		err = run(*logPath, *week, reviewDir(*out, *logPath), *index)
	}
	if err != nil {
		fmt.Fprintln(os.Stderr, "assistantreview:", err)
		os.Exit(1)
	}
}

const defaultLog = "data/monitor/assistant.jsonl"

// reviewDir is the -out folder, by default assistant_reviews/ beside the log.
func reviewDir(out, logPath string) string {
	if out != "" {
		return out
	}
	return filepath.Join(filepath.Dir(logPath), "assistant_reviews")
}

// closeAnswer is the close subcommand.
func closeAnswer(args []string) error {
	fs := flag.NewFlagSet("close", flag.ExitOnError)
	id := fs.String("id", "", "the answer's id in the review list")
	how := fs.String("how", "", strings.Join(assistantreview.Hows, ", "))
	ref := fs.String("ref", "", "the help passage or PR/issue number, the golden id, or a short reason (no personal data)")
	logPath := fs.String("log", defaultLog, "the monitor's assistant review log")
	out := fs.String("out", "", "the review folder (default: assistant_reviews/ beside the log)")
	if err := fs.Parse(args); err != nil {
		return err
	}
	path := filepath.Join(reviewDir(*out, *logPath), assistantreview.ClosuresFile)
	c, err := assistantreview.Close(*logPath, path, *id, *how, *ref, time.Now())
	if err != nil {
		return err
	}
	fmt.Printf("closed %s as %s in %s\n", c.AnswerID, c.How, path)
	return nil
}

func run(logPath, week, out, indexPath string) error {
	now := time.Now()
	w := evidence.LastWeek(now)
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
	closures, err := assistantreview.LoadClosures(filepath.Join(out, assistantreview.ClosuresFile))
	if err != nil {
		return err
	}
	r := assistantreview.Build(lg, closures, w.From, w.To, now, w.Label, ix)
	paths, err := assistantreview.Write(out, r)
	if err != nil {
		return err
	}
	for _, p := range paths {
		fmt.Println("wrote", p)
	}
	for _, line := range assistantreview.ClosureAudit(r) {
		fmt.Println(line)
	}
	s := assistantreview.Summary(r)
	fmt.Printf("RESULT week=%s answers=%d flagged=%d review=%d skipped=%d closed=%d open=%d overdue=%d\n",
		w.Label, s.Answers, s.Flagged, s.ReviewItems, s.Skipped, s.Closed, s.Open, s.Overdue)
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
