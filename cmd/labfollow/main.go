// Command labfollow is the lab follow-up (#855, epic E9): once a manager's
// recorded change (COMPARE_DIR/applied/<run id>.json) has reached the bot, it
// puts the run's backtested return, worst week, drawdown and fees beside what
// the bot realised over [live-from, live-from + days) — the P&L history
// through perf.Summarize, the journal's close fees — and the gap in
// percentage points, each figure labelled "backtested, not a forecast" or
// "realised, one short window". A window not over yet says "too early to
// judge", one without a P&L point "no data"; neither prints a gap.
// Percentages and counts only. Read-only: it writes no config, edits no
// record and calls no bot or exchange; -record-live-from only keeps the day
// in applied/<run id>.live, for the weekly usage report.
//
//	go run ./cmd/labfollow -id 20261104T230000Z -live-from 2026-11-10 [-days 7] [-compare data/compare] [-root .] [-json] [-record-live-from]
//	go run ./cmd/labfollow -all -compare data/compare -root .   # every record with a .live day: the usage report's "Lab follow-up" section
//
// Exit 0 with a follow-up (also too early or no data), 1 on bad input (no
// record, live-from in the future or before the record), writing nothing.
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
	o := compare.LabFollowOptions{}
	id := flag.String("id", "", "the run id of the recorded change (COMPARE_DIR/applied/<id>.json)")
	liveFrom := flag.String("live-from", "", "the UTC day the change reached the bot, YYYY-MM-DD (default the day kept in applied/<id>.live)")
	days := flag.Int("days", compare.LabFollowDays, "the window: whole UTC days from live-from")
	all := flag.Bool("all", false, "every record with a live-from day on file, as one Markdown section")
	record := flag.Bool("record-live-from", false, "keep -live-from in applied/<id>.live once it is checked")
	asJSON := flag.Bool("json", false, "print JSON instead of Markdown")
	flag.StringVar(&o.CompareDir, "compare", "data/compare", "cmd/compare's reports folder (the monitor's COMPARE_DIR)")
	flag.StringVar(&o.Root, "root", ".", "the environment's clone: data/<bot>/orders.log, data/<bot>/data/pnl_history.jsonl, data/stress/<bot>/account.json")
	flag.StringVar(&o.Journal, "journal", "", "the journal (default under -root)")
	flag.StringVar(&o.History, "history", "", "the P&L history (default under -root)")
	flag.Float64Var(&o.Equity, "equity", 0, "the realised figures' equity in the coin (default the run's replay equity, else account.json)")
	flag.Parse()

	log := slog.New(slog.NewTextHandler(os.Stderr, nil))
	now := time.Now().UTC()
	if *all {
		runAll(log, o, *days, now, *asJSON)
		return
	}
	day, err := liveFromDay(o.CompareDir, *id, *liveFrom)
	if err == nil {
		var in compare.LabFollowInput
		if in, err = compare.ReadLabFollow(o, *id, day, *days, now); err == nil {
			f := compare.BuildLabFollow(in)
			if *record {
				if err := compare.RecordLiveFrom(o.CompareDir, *id, day); err != nil {
					log.Error("lab follow-up: live-from not kept", "component", "lab", "run_id", *id, "error", err)
					os.Exit(1)
				}
			}
			emit(log, []compare.LabFollow{f}, nil, *asJSON)
			log.Info("lab follow-up", "component", "lab", "run_id", f.RunID, "state", f.State, "points", f.Points, "flags", len(f.Flags))
			return
		}
	}
	log.Error("lab follow-up refused", "component", "lab", "run_id", *id, "error", err)
	fmt.Fprintln(os.Stderr, "labfollow:", err)
	os.Exit(1)
}

// liveFromDay is -live-from, else the day kept for id.
func liveFromDay(compareDir, id, flagValue string) (time.Time, error) {
	if flagValue != "" {
		return compare.ParseLiveFrom(flagValue)
	}
	day, ok, err := compare.ReadLiveFrom(compareDir, id)
	if err != nil {
		return time.Time{}, err
	}
	if !ok {
		return time.Time{}, fmt.Errorf("no -live-from and none kept for run %s", id)
	}
	return day, nil
}

// runAll follows every record with a live-from day on file; one that cannot
// be followed is listed with its reason, never stopping the others.
func runAll(log *slog.Logger, o compare.LabFollowOptions, days int, now time.Time, asJSON bool) {
	ids, err := compare.LiveFromIDs(o.CompareDir)
	if err != nil {
		log.Error("lab follow-up failed", "component", "lab", "error", err)
		os.Exit(1)
	}
	var out []compare.LabFollow
	refused := map[string]string{}
	for _, id := range ids {
		day, err := liveFromDay(o.CompareDir, id, "")
		var in compare.LabFollowInput
		if err == nil {
			in, err = compare.ReadLabFollow(o, id, day, days, now)
		}
		if err != nil {
			refused[id] = err.Error()
			continue
		}
		out = append(out, compare.BuildLabFollow(in))
	}
	emit(log, out, refused, asJSON)
	log.Info("lab follow-up", "component", "lab", "records", len(ids), "followed", len(out), "refused", len(refused))
}

// emit prints the follow-ups as one Markdown section or JSON.
func emit(log *slog.Logger, fs []compare.LabFollow, refused map[string]string, asJSON bool) {
	if asJSON {
		body, err := json.MarshalIndent(struct {
			FollowUps []compare.LabFollow `json:"follow_ups"`
			Refused   map[string]string   `json:"refused,omitempty"`
		}{fs, refused}, "", "  ")
		if err != nil {
			log.Error("lab follow-up failed", "component", "lab", "error", err)
			os.Exit(1)
		}
		fmt.Println(string(body))
		return
	}
	fmt.Print(compare.LabFollowSectionMarkdown(fs, refused))
}
