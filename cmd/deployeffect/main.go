// Command deployeffect prints, for one bot, every startup in a window and
// the orders it sent in its first 30 minutes by trigger, each startup
// classified neutral, expected or restart-made (#219, E1: "no restart or
// deploy changes the book by itself"; the rules are in internal/deployeffect).
// It reads orders.log, the config and optionally deploys.tsv — no Deribit
// or monitor call — and prints counts and lots only.
//
//	go run ./cmd/deployeffect -journal data/btc/orders.log -config config_btc.mvp.yaml -deploys ~/dev/go/optionsBot-env/history/deploys.tsv -days 3
package main

import (
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"optionsbot/internal/config"
	"optionsbot/internal/deployeffect"
)

func main() {
	journal := flag.String("journal", "", "the bot's journal (orders.log)")
	cfgPath := flag.String("config", "", "the bot's config_*.yaml (exit thresholds and repair cooldown)")
	deploys := flag.String("deploys", "", "optional deploys.tsv: names each startup's commit, adds deploys into a flat book")
	env := flag.String("env", "mvp", "environment column of deploys.tsv to read (empty = all)")
	name := flag.String("name", "", "bot name in the title (default: the journal's directory)")
	fromStr := flag.String("from", "", "window start, YYYY-MM-DD or RFC 3339 (default: -days before -to)")
	toStr := flag.String("to", "", "window end, YYYY-MM-DD or RFC 3339 (default: now)")
	days := flag.Int("days", 7, "window length when -from is not set")
	flag.Parse()

	if err := run(*journal, *cfgPath, *deploys, *env, *name, *fromStr, *toStr, *days); err != nil {
		fmt.Fprintln(os.Stderr, "deployeffect:", err)
		os.Exit(1)
	}
}

func run(journal, cfgPath, deploysPath, env, name, fromStr, toStr string, days int) error {
	if journal == "" || cfgPath == "" {
		return fmt.Errorf("-journal and -config are required")
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

	cfg, err := config.Load(cfgPath)
	if err != nil {
		return err
	}
	lines, badJournal, err := deployeffect.ReadJournalFile(journal)
	if err != nil {
		return err
	}
	var deps []deployeffect.Deploy
	badDeploys := 0
	if deploysPath != "" {
		if deps, badDeploys, err = deployeffect.ReadDeploysFile(deploysPath, env); err != nil {
			return err
		}
	}
	if badJournal+badDeploys > 0 {
		fmt.Fprintf(os.Stderr, "deployeffect: skipped unreadable lines: journal %d, deploys %d\n", badJournal, badDeploys)
	}
	if name == "" {
		name = filepath.Base(filepath.Dir(journal))
	}
	rep := deployeffect.Analyze(lines, deps, deployeffect.RulesFrom(cfg), from, to)
	return deployeffect.Format(os.Stdout, name, rep)
}

func parseTime(s string) (time.Time, error) {
	if t, err := time.Parse(time.RFC3339, s); err == nil {
		return t, nil
	}
	return time.Parse(time.DateOnly, s)
}
