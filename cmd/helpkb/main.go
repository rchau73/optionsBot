// Command helpkb searches the help knowledge index (internal/helpkb, #259)
// from the command line, with no model and no network:
//
//	go run ./cmd/helpkb -q "what is a stop-loss"              # the top passages and their citations
//	go run ./cmd/helpkb -eval tests/testdata/helpkb_questions.yaml   # hit@1, hit@3, dont_know precision per category
//
// The index is built from the sources in the checkout (-root), the same way
// cmd/helpindex writes it.
package main

import (
	"flag"
	"fmt"
	"os"
	"strings"

	"optionsbot/internal/helpkb"
)

func main() {
	root := flag.String("root", ".", "repository root the sources are read from")
	q := flag.String("q", "", "question to search for")
	k := flag.Int("k", 3, "passages to show")
	evalFile := flag.String("eval", "", "question set (YAML) to score retrieval on")
	threshold := flag.Float64("threshold", helpkb.DefaultNoAnswer, "least share of the question's weight the best passage must hold, else \"I don't know\"")
	flag.Parse()
	if err := run(*root, *q, *k, *evalFile, *threshold); err != nil {
		fmt.Fprintln(os.Stderr, "helpkb:", err)
		os.Exit(1)
	}
}

func run(root, q string, k int, evalFile string, threshold float64) error {
	if (q == "") == (evalFile == "") {
		return fmt.Errorf("give exactly one of -q or -eval")
	}
	f, err := helpkb.LoadIndex(root)
	if err != nil {
		return err
	}
	ix := helpkb.NewIndex(f.Chunks)
	if evalFile != "" {
		data, err := os.ReadFile(evalFile)
		if err != nil {
			return err
		}
		qs, err := helpkb.ParseQuestions(data)
		if err != nil {
			return err
		}
		fmt.Print(helpkb.Evaluate(ix, qs, threshold).Text())
		return nil
	}
	hits := ix.Search(q, k)
	if helpkb.NoAnswer(hits, threshold) {
		fmt.Println("I don't know: no passage of the help answers this question.")
	}
	for i, h := range hits {
		fmt.Printf("%d. %s  (score %.2f, coverage %.0f %%)\n   %s\n   %s\n\n", i+1, h.Source, h.Score, 100*h.Coverage, h.Title, indent(h.Text))
	}
	return nil
}

func indent(s string) string { return strings.ReplaceAll(s, "\n", "\n   ") }
