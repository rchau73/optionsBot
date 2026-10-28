// Command analyze turns a cmd/compare report into a plain-words AI analysis
// (#426): one model call with the contract's prompt (internal/aianalysis),
// the answer checked by aianalysis.Check — every number against the report,
// no advice, no forecast, the AI label — and analysis.md|json written beside
// the report only when it passes; otherwise nothing is written, the reasons
// are printed and the exit code is 1. Offline: nothing serves the result, and
// the AI never changes a setting. The key is ANTHROPIC_API_KEY from the
// environment of whoever runs it; -fake <answer.md> uses a written answer
// instead and sends nothing. See README "AI analysis of a comparison".
//
//	go run ./cmd/analyze -report data/compare/20261028T120000Z/report.json
//	go run ./cmd/analyze -report …/report.json -fake my-answer.md
package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"os"
	"time"

	"optionsbot/internal/aianalysis"
)

func main() {
	report := flag.String("report", "", "a cmd/compare report: data/compare/<time>/report.json")
	fake := flag.String("fake", "", "use this file's text as the model's answer (no API call)")
	flag.Parse()
	if *report == "" {
		fmt.Fprintln(os.Stderr, "analyze: -report is needed")
		os.Exit(2)
	}
	model, name, err := aianalysis.ModelFor(*fake, os.Getenv("ANTHROPIC_API_KEY"))
	if err != nil {
		fmt.Fprintln(os.Stderr, "analyze:", err)
		os.Exit(1)
	}
	md, js, vs, err := aianalysis.Run(context.Background(), aianalysis.Options{
		ReportPath: *report, Model: model, ModelName: name, Now: time.Now()})
	if errors.Is(err, aianalysis.ErrRejected) {
		fmt.Fprintln(os.Stderr, "analyze:", err)
		for _, v := range vs {
			fmt.Fprintln(os.Stderr, "  -", v)
		}
		os.Exit(1)
	}
	if err != nil {
		fmt.Fprintln(os.Stderr, "analyze:", err)
		os.Exit(1)
	}
	fmt.Println(md)
	fmt.Println(js)
}
