// Command backupcheck is the restore check of the nightly backup (#328): it
// replays a copied journal (internal/backup.Check, orders.ReplayFile) and
// fails when it cannot be decoded or holds fewer events than the previous
// night's copy. On success it writes check.json into the copy, which the next
// night compares with. Reads the copy only; never a live bot's files.
//
//	go run ./cmd/backupcheck -dir <envs>/history/backups/2026-10-24/stable/btc \
//	  -previous <envs>/history/backups/2026-10-23/stable/btc
//
// With -lines <file> it checks a copied JSON-lines file instead (the monitor's
// assistant.jsonl, #438): every line must parse, a torn last one aside.
//
//	go run ./cmd/backupcheck -lines <envs>/history/backups/2026-10-24/mvp/monitor/assistant.jsonl
package main

import (
	"flag"
	"fmt"
	"os"
	"time"

	"optionsbot/internal/backup"
)

func main() {
	dir := flag.String("dir", "", "the copied bot folder (orders.log, archive/, …)")
	prev := flag.String("previous", "", "the previous good copy of the same bot (optional)")
	lines := flag.String("lines", "", "instead: check that every line of this copied JSON-lines file parses (the monitor's assistant.jsonl, #438)")
	flag.Parse()
	if *lines != "" {
		res, err := backup.CheckLines(*lines)
		if err != nil {
			fmt.Fprintln(os.Stderr, "backupcheck:", err)
			os.Exit(1)
		}
		fmt.Printf("RESULT lines=%d skipped=%d\n", res.Lines, res.Skipped)
		return
	}
	if *dir == "" {
		fmt.Fprintln(os.Stderr, "backupcheck: -dir or -lines is required")
		os.Exit(2)
	}
	res, err := backup.Check(*dir, *prev, time.Now())
	if err != nil {
		fmt.Fprintln(os.Stderr, "backupcheck:", err)
		os.Exit(1)
	}
	if err := backup.WriteResult(*dir, res); err != nil {
		fmt.Fprintln(os.Stderr, "backupcheck: write result:", err)
		os.Exit(1)
	}
	fmt.Printf("RESULT events=%d archived_events=%d skipped=%d\n", res.Events, res.ArchivedEvents, res.Skipped)
}
