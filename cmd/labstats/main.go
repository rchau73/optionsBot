// Command labstats is the lab's usage report (#824, epic E9): from the
// request queue (COMPARE_DIR/requests/) and the recorded changes (applied/)
// it prints, over the last -days, the requests made and the share ending
// done, failed and expired, the median minutes to a finished run, the
// requests per manager per week (a short hash, never a name), the done runs
// with a recorded change, those with a dropped proposal (dropped/, #890), the
// share ending in a decision and the AI analyses written — counts only, no
// setting value, amount or account detail. Read-only; exit 0 also when the
// window is empty, 1 when the folder cannot be read.
//
//	go run ./cmd/labstats -compare data/compare [-days 28] [-json]
package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"log/slog"
	"os"
	"time"

	"optionsbot/internal/compare"
)

func main() {
	dir := flag.String("compare", "data/compare", "cmd/compare's reports folder (the monitor's COMPARE_DIR)")
	days := flag.Int("days", compare.LabStatsDays, "the window: requests made in the last N days")
	asJSON := flag.Bool("json", false, "print JSON instead of Markdown")
	flag.Parse()

	log := slog.New(slog.NewTextHandler(os.Stderr, nil))
	files, err := compare.ReadLabUsage(*dir)
	if err != nil {
		log.Error("lab usage report failed", "component", "lab", "error", err)
		os.Exit(1)
	}
	s := compare.ComputeLabStats(files, time.Now(), *days)
	if *asJSON {
		out, err := json.MarshalIndent(s, "", "  ")
		if err != nil {
			log.Error("lab usage report failed", "component", "lab", "error", err)
			os.Exit(1)
		}
		fmt.Println(string(out))
	} else {
		fmt.Print(compare.LabStatsMarkdown(s))
	}
	log.Info("lab usage report", "component", "lab", "requests", s.Requests, "done", s.Done, "unreadable", s.Unreadable, "days", s.Days)
}
