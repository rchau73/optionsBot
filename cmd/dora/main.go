// Command dora writes the delivery report (#470): the four DORA metrics per
// ISO week for mvp and stable, and every merged PR with the time it went live
// on each, from files only — main's history, tags, PRs and issues as
// scripts/agents/dora.sh dumps them, and <envs>/history/deploys.tsv and
// promotions.tsv. No git, GitHub or Deribit call.
//
//	go run ./cmd/dora -main main.tsv -tags tags.tsv -prs prs.json -issues issues.json \
//	  -deploys ~/dev/go/optionsBot-env/history/deploys.tsv \
//	  -promotions ~/dev/go/optionsBot-env/history/promotions.tsv \
//	  -out ~/dev/go/optionsBot-env/history/delivery
//	go run ./cmd/dora … -summary 2026-W44   # also print that week's four numbers (Markdown)
//
// Writes <out>/dora.json and <out>/<week>.md. Lines it cannot use are printed
// as "skipped: …" and listed in dora.json's problems: never counted.
package main

import (
	"flag"
	"fmt"
	"os"
	"time"

	"optionsbot/internal/delivery"
)

func main() {
	var f delivery.Files
	flag.StringVar(&f.Main, "main", "", "main's first-parent history (dora.sh's main.tsv), required")
	flag.StringVar(&f.Tags, "tags", "", "tags and their commits (tags.tsv)")
	flag.StringVar(&f.PRs, "prs", "", "merged pull requests (gh pr list --json)")
	flag.StringVar(&f.Issues, "issues", "", "issues (gh issue list --json)")
	flag.StringVar(&f.Deploys, "deploys", "", "<envs>/history/deploys.tsv")
	flag.StringVar(&f.Promotions, "promotions", "", "<envs>/history/promotions.tsv")
	from := flag.String("from", "2026-10-02", "backfill start (YYYY-MM-DD): weeks begin at its ISO week")
	out := flag.String("out", "", "folder for dora.json and <week>.md, required")
	summary := flag.String("summary", "", "print this ISO week's four numbers as Markdown (e.g. 2026-W44)")
	flag.Parse()
	if f.Main == "" || *out == "" {
		fmt.Fprintln(os.Stderr, "dora: -main and -out are required")
		os.Exit(2)
	}
	start, err := time.Parse(time.DateOnly, *from)
	if err != nil {
		fmt.Fprintf(os.Stderr, "dora: -from %q: want YYYY-MM-DD\n", *from)
		os.Exit(2)
	}
	in, err := delivery.Load(f)
	if err != nil {
		fmt.Fprintln(os.Stderr, "dora:", err)
		os.Exit(1)
	}
	in.From = start
	r := delivery.Build(in, time.Now())
	if err := delivery.Write(*out, r); err != nil {
		fmt.Fprintln(os.Stderr, "dora:", err)
		os.Exit(1)
	}
	for _, p := range r.Problems {
		fmt.Fprintln(os.Stderr, "skipped:", p)
	}
	fmt.Printf("dora: %d weeks, %d PRs, %d releases, %d failures, %d problems → %s\n",
		len(r.Weeks), len(r.PRs), len(r.Releases), len(r.Failures), len(r.Problems), *out)
	if *summary != "" {
		for _, w := range r.Weeks {
			if w.Week == *summary {
				fmt.Print(delivery.SummaryMarkdown(w))
				return
			}
		}
		fmt.Fprintf(os.Stderr, "dora: week %s is not in the report\n", *summary)
		os.Exit(1)
	}
}
