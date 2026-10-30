package evidence

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"optionsbot/internal/config"
	"optionsbot/internal/coverage"
	"optionsbot/internal/deployeffect"
	"optionsbot/internal/idlemargin"
	"optionsbot/internal/orders"
	"optionsbot/internal/recorder"
	"optionsbot/internal/replay"
)

// The impure side of the report: reading the environment's files, running
// cmd/stress and writing the two outputs. Build stays pure.

// WarmUp is how much recording before the window the replay reads: GEX, the
// bias's median flip and the regime's daily closes (2 needed) settle in it.
const WarmUp = 7 * 24 * time.Hour

// Load reads every input of one bot it can. A missing or unreadable input
// is written to notes (one line each) and left empty: the report then says
// what it lacks. equity > 0 overrides account.json's margin balance.
func Load(env, bot string, p Paths, w Window, equity float64, notes io.Writer) Inputs {
	note := func(format string, a ...any) {
		fmt.Fprintf(notes, "%s %s: "+format+"\n", append([]any{env, bot}, a...)...)
	}
	in := Inputs{Env: env, Bot: bot, Window: w, Config: p.Config, Journal: p.Journal, History: p.History,
		Market: p.Market, GEXDir: p.GEX, MarketJ: filepath.Join(p.Stress, "market.json"), StressDir: p.Stress}
	if cfg, err := config.Load(p.Config); err != nil {
		note("config not loaded: %v", err)
	} else {
		in.Cfg = cfg
	}
	if _, err := os.Stat(p.Journal); err != nil {
		note("journal not read: %v", err)
		in.JournalNote = err.Error()
	} else if rp, err := orders.ReplayFile(p.Journal, 0); err != nil {
		note("journal not read: %v", err)
		in.JournalNote = err.Error()
	} else {
		in.Trades = rp.Trades
		loadE1Journal(&in, p, env, note)
	}
	readErrs := map[string]error{} // dataset → why Load could not read it (coverage)
	if pts, bad, err := idlemargin.ReadHistoryFile(p.History); err != nil {
		note("P&L history not read: %v", err)
		readErrs["pnl_history"] = err
	} else {
		in.Points = pts
		if bad > 0 {
			note("P&L history: %d unreadable lines skipped", bad)
		}
	}
	var err error
	if in.Snaps, err = recorder.ReadDir(p.Market, w.From.Add(-WarmUp), w.Last()); err != nil {
		note("recorded books not read: %v", err)
		readErrs["market"] = err
	}
	if in.GEX, err = recorder.ReadGEXDir(p.GEX, w.From.Add(-WarmUp), w.Last()); err != nil {
		note("minute GEX not read: %v", err)
		readErrs["market_gex"] = err
	}
	in.Recorded = loadRecorded(&in, p, w, readErrs, note)
	if in.Closes, in.DVOL, err = replay.ReadMarketJSON(in.MarketJ); err != nil {
		note("%v: the SMA trend and IV percentile start from the recording alone", err)
	}
	in.Equity, in.EquitySource = equity, "-equity"
	if equity <= 0 {
		acct := filepath.Join(p.Stress, "account.json")
		in.Equity = replay.ReadEquity(acct, bot)
		in.EquitySource = acct + " (margin balance when captured)"
	}
	loadEarlier(&in, p, bot, note)
	return in
}

// loadEarlier reads the bot's earlier weekly reports for the scoreboard
// (#537). Without a clone root (Paths built by hand) none are read.
func loadEarlier(in *Inputs, p Paths, bot string, note func(string, ...any)) {
	if p.Root == "" {
		return
	}
	in.WeeklySource = WeeklyGlob(p.Root, bot)
	earlier, unreadable, err := ReadWeekly(p.Root, bot)
	if err != nil {
		note("earlier weekly reports not read: %v", err)
		return
	}
	for _, u := range unreadable {
		note("weekly report %s unreadable: its week counts as missing", u)
	}
	in.Earlier = earlier
}

// RunStress runs the standard shocks on the book captured in in.StressDir
// (positions.json) with the bot's settings, through stressCmd (e.g. "go run
// ./cmd/stress"; "none" skips), and keeps each RESULT. A failed run is kept
// as an outcome with its error; the others still run.
func RunStress(in *Inputs, stressCmd string) {
	positions := filepath.Join(in.StressDir, "positions.json")
	fi, statErr := os.Stat(positions)
	cmd := strings.Fields(stressCmd)
	switch {
	case len(cmd) == 0 || stressCmd == "none":
		in.StressNote = "skipped (-stress-cmd none)"
		return
	case statErr != nil:
		in.StressNote = fmt.Sprintf("no captured book (%s): run scripts/stress_inputs.sh %s first", positions, in.Bot)
		return
	case in.Cfg == nil:
		in.StressNote = "no config: the stress runs the bot's settings"
		return
	}
	in.StressBookAt = fi.ModTime().UTC()
	for _, shock := range StandardShocks {
		args := append(append([]string{}, cmd[1:]...), StressArgs(in.Cfg, in.StressDir, shock)...)
		var out, errOut bytes.Buffer
		c := exec.Command(cmd[0], args...)
		c.Stdout, c.Stderr = &out, &errOut
		o := StressOutcome{Shock: shock}
		if err := c.Run(); err != nil {
			o.Error = fmt.Sprintf("%v: %s", err, lastLine(errOut.String()))
		} else if parsed, err := ParseStressResult(shock, out.String()); err != nil {
			o.Error = err.Error()
		} else {
			o = parsed
		}
		o.Command = strings.Join(append([]string{cmd[0]}, args...), " ")
		in.Stress = append(in.Stress, o)
	}
}

// loadE1Journal reads the journal again for the E1 section (#361): the
// idle-margin events and the startup lines each package decodes, and the
// environment's deploys.tsv. An empty journal gives empty, non-nil reads.
func loadE1Journal(in *Inputs, p Paths, env string, note func(string, ...any)) {
	in.DeploysPath = p.Deploys
	if ev, bad, err := idlemargin.ReadJournalFile(p.Journal); err != nil {
		note("journal not read for idle margin: %v", err)
	} else {
		in.IdleEvents = append([]idlemargin.Event{}, ev...)
		if bad > 0 {
			note("journal: %d unreadable lines skipped", bad)
		}
	}
	if lines, _, err := deployeffect.ReadJournalFile(p.Journal); err != nil {
		note("journal not read for startups: %v", err)
	} else {
		in.DeployLines = append([]deployeffect.Line{}, lines...)
	}
	if p.Deploys == "" {
		in.DeploysNote = "no deploys.tsv: startups from the journal only, commits not named"
		return
	}
	deps, bad, err := deployeffect.ReadDeploysFile(p.Deploys, env)
	if err != nil {
		note("deploys not read: %v", err)
		in.DeploysNote = "deploys.tsv not read: startups from the journal only, commits not named, restarts into a flat book not counted"
		return
	}
	in.Deploys = deps
	if bad > 0 {
		note("deploys: %d unreadable lines skipped", bad)
	}
}

// loadRecorded reads the record times of every dataset cmd/datacoverage
// knows (#362), under the books' folder (beside p.Market) — except the P&L
// history, which is the bot's own (p.History). The books, the minute GEX and
// the history Load already read are reused, not read again (readErrs: why
// Load could not read one of them).
func loadRecorded(in *Inputs, p Paths, w Window, readErrs map[string]error, note func(string, ...any)) []RecordedSet {
	var sets []RecordedSet
	for _, ds := range coverage.Datasets(filepath.Dir(p.Market)) {
		switch ds.Name {
		case "market":
			ds.Path = p.Market
		case "market_gex":
			ds.Path = p.GEX
		case "pnl_history":
			ds.Path = p.History
		}
		s := RecordedSet{Name: ds.Name, Path: ds.Path, Step: ds.Step}
		_, statErr := os.Stat(ds.Path)
		s.Found = statErr == nil
		if statErr != nil && !errors.Is(statErr, os.ErrNotExist) {
			s.Err = statErr.Error()
		}
		if err := readErrs[ds.Name]; s.Found && err != nil {
			s.Err = err.Error()
		} else if s.Found {
			switch ds.Name {
			case "market":
				s.Times = bookTimes(in.Snaps)
			case "market_gex":
				s.Times = gexTimes(in.GEX)
			case "pnl_history":
				for _, pt := range in.Points {
					s.Times = append(s.Times, pt.T)
				}
			default:
				ts, _, err := coverage.ReadTimes(ds, w.From, w.Last())
				if err != nil {
					note("%s not read for coverage: %v", ds.Name, err)
					s.Err = err.Error()
				}
				s.Times = ts
			}
		}
		sets = append(sets, s)
	}
	return sets
}

func lastLine(s string) string {
	lines := strings.Split(strings.TrimSpace(s), "\n")
	return lines[len(lines)-1]
}

// Write writes r as Markdown to md and as JSON to js, creating their folder.
func Write(r Report, md, js string) error {
	if err := os.MkdirAll(filepath.Dir(md), 0o755); err != nil {
		return err
	}
	var b bytes.Buffer
	if err := WriteMarkdown(&b, r); err != nil {
		return err
	}
	if err := os.WriteFile(md, b.Bytes(), 0o644); err != nil {
		return fmt.Errorf("write %s: %w", md, err)
	}
	data, err := json.MarshalIndent(r, "", "  ")
	if err != nil {
		return err
	}
	if err := os.WriteFile(js, append(data, '\n'), 0o644); err != nil {
		return fmt.Errorf("write %s: %w", js, err)
	}
	return nil
}
