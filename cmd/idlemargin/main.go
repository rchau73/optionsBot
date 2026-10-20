// Command idlemargin prints, for one bot, what kept its approved margin
// unused over a window (#218): each slot's time occupied, vacant by the
// journal's skip reason, frozen and not measured, and the unused margin by
// book state. Research tool: it reads orders.log and pnl_history.jsonl only.
//
//	go run ./cmd/idlemargin -journal data/btc/orders.log -history data/btc/pnl_history.jsonl -days 7
package main

import (
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"optionsbot/internal/config"
	"optionsbot/internal/idlemargin"
	"optionsbot/internal/orders"
)

func main() {
	journal := flag.String("journal", "", "the bot's journal (orders.log)")
	hist := flag.String("history", "", "the bot's P&L history (pnl_history.jsonl)")
	name := flag.String("name", "", "bot name in the title (default: the journal's directory)")
	cfgPath := flag.String("config", "", "optional config_*.yaml: report its slots only (default: every slot the journal names)")
	fromStr := flag.String("from", "", "window start, YYYY-MM-DD or RFC 3339 (default: -days before -to)")
	toStr := flag.String("to", "", "window end, YYYY-MM-DD or RFC 3339 (default: now)")
	days := flag.Int("days", 7, "window length when -from is not set")
	flag.Parse()

	if err := run(*journal, *hist, *name, *cfgPath, *fromStr, *toStr, *days); err != nil {
		fmt.Fprintln(os.Stderr, "idlemargin:", err)
		os.Exit(1)
	}
}

func run(journal, hist, name, cfgPath, fromStr, toStr string, days int) error {
	if journal == "" || hist == "" {
		return fmt.Errorf("-journal and -history are required")
	}
	to := time.Now().UTC()
	if toStr != "" {
		t, err := parseTime(toStr)
		if err != nil {
			return fmt.Errorf("-to: %w", err)
		}
		to = t
	}
	from := to.AddDate(0, 0, -days)
	if fromStr != "" {
		t, err := parseTime(fromStr)
		if err != nil {
			return fmt.Errorf("-from: %w", err)
		}
		from = t
	}
	if !to.After(from) {
		return fmt.Errorf("window %s → %s is empty", from, to)
	}

	var slots []orders.SlotRef
	if cfgPath != "" {
		cfg, err := config.Load(cfgPath)
		if err != nil {
			return err
		}
		for _, s := range cfg.Slots() {
			slots = append(slots, orders.SlotRef{DTE: s.TargetDTE, Delta: s.EntryDelta})
		}
	}

	events, badJournal, err := idlemargin.ReadJournalFile(journal)
	if err != nil {
		return err
	}
	points, badHistory, err := idlemargin.ReadHistoryFile(hist)
	if err != nil {
		return err
	}
	if badJournal+badHistory > 0 {
		fmt.Fprintf(os.Stderr, "idlemargin: skipped unreadable lines: journal %d, history %d\n", badJournal, badHistory)
	}
	if name == "" {
		name = filepath.Base(filepath.Dir(journal))
	}
	return idlemargin.Format(os.Stdout, name, idlemargin.Attribute(events, points, slots, from, to))
}

func parseTime(s string) (time.Time, error) {
	if t, err := time.Parse(time.RFC3339, s); err == nil {
		return t, nil
	}
	return time.Parse(time.DateOnly, s)
}
