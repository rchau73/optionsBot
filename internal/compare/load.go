package compare

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"

	"optionsbot/internal/config"
	"optionsbot/internal/evidence"
	"optionsbot/internal/recorder"
	"optionsbot/internal/replay"
)

// The impure side of a comparison: reading the inputs, running cmd/stress
// for each config and writing the report.

// Paths are the comparison's inputs; an empty data path skips its section.
type Paths struct {
	Base, Proposed string // the two config files
	CSV            string // backtest data
	Market         string // recorded books (data/market)
	GEX            string // minute GEX records; "" = market_gex beside Market, "none" = hourly
	Stress         string // captured book (data/stress/<bot>): positions.json, market.json, account.json
}

// Load reads both configs and the data both runs share. A config that does
// not load (or two configs of different coins) is an error: there is nothing
// to compare. A data input that cannot be read is written to notes and left
// empty, and its section says so. equity > 0 overrides account.json.
func Load(p Paths, from, to time.Time, equity float64, notes io.Writer) (Inputs, error) {
	in := Inputs{BasePath: p.Base, ProposedPath: p.Proposed, From: from, To: to, CSV: p.CSV, Market: p.Market, StressDir: p.Stress}
	var err error
	if in.Base, err = config.Load(p.Base); err != nil {
		return in, fmt.Errorf("base %s: %w", p.Base, err)
	}
	if in.Proposed, err = config.Load(p.Proposed); err != nil {
		return in, fmt.Errorf("proposed %s: %w", p.Proposed, err)
	}
	if !strings.EqualFold(in.Base.Underlying, in.Proposed.Underlying) {
		return in, fmt.Errorf("base trades %s, proposed %s: compare configs of one coin", in.Base.Underlying, in.Proposed.Underlying)
	}
	note := func(format string, a ...any) { fmt.Fprintf(notes, format+"\n", a...) }
	if in.CSV != "" {
		if _, err := os.Stat(in.CSV); err != nil {
			note("backtest data not read: %v", err)
			in.CSV = ""
		}
	}
	if in.Market != "" {
		warm := from.Add(-evidence.WarmUp)
		if in.Snaps, err = recorder.ReadDir(in.Market, warm, lastInstant(to)); err != nil {
			note("recorded books not read: %v", err)
		}
		gexDir := p.GEX
		if gexDir == "" {
			gexDir = filepath.Join(filepath.Dir(filepath.Clean(in.Market)), "market_gex")
		}
		if gexDir != "none" {
			if in.GEX, err = recorder.ReadGEXDir(gexDir, warm, lastInstant(to)); err != nil {
				note("minute GEX not read (%v): hourly replay", err)
			}
		}
	}
	in.Equity, in.EquitySource = equity, "-equity"
	if in.StressDir != "" {
		mj := filepath.Join(in.StressDir, "market.json")
		if in.Closes, in.DVOL, err = replay.ReadMarketJSON(mj); err != nil {
			note("%v: the SMA trend and IV percentile start from the recording alone", err)
		}
		if equity <= 0 {
			acct := filepath.Join(in.StressDir, "account.json")
			in.Equity = replay.ReadEquity(acct, in.Base.Underlying)
			in.EquitySource = acct + " (margin balance when captured)"
		}
	}
	return in, nil
}

// RunStress runs the standard shocks (evidence.StandardShocks) on the book in
// in.StressDir with each config's settings, through stressCmd (e.g. "go run
// ./cmd/stress"; "none" skips) — the evidence report's runner.
func RunStress(in *Inputs, stressCmd string) {
	if in.StressDir == "" {
		in.StressNote = "no captured book (-stress)"
		return
	}
	bot := strings.ToLower(in.Base.Underlying)
	base := evidence.Inputs{Bot: bot, StressDir: in.StressDir, Cfg: in.Base}
	evidence.RunStress(&base, stressCmd)
	if base.Stress == nil {
		in.StressNote = base.StressNote
		return
	}
	proposed := evidence.Inputs{Bot: bot, StressDir: in.StressDir, Cfg: in.Proposed}
	evidence.RunStress(&proposed, stressCmd)
	in.BaseStress, in.ProposedStress, in.StressBookAt = base.Stress, proposed.Stress, base.StressBookAt
}

// OutDir is the report's folder: <root>/<UTC time>.
func OutDir(root string, now time.Time) string {
	return filepath.Join(root, now.UTC().Format("20060102T150405Z"))
}

// Write writes r to dir/report.md and dir/report.json, creating dir.
func Write(r Report, dir string) (md, js string, err error) {
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", "", err
	}
	md, js = filepath.Join(dir, "report.md"), filepath.Join(dir, "report.json")
	var b bytes.Buffer
	if err := WriteMarkdown(&b, r); err != nil {
		return "", "", err
	}
	if err := os.WriteFile(md, b.Bytes(), 0o644); err != nil {
		return "", "", fmt.Errorf("write %s: %w", md, err)
	}
	data, err := json.MarshalIndent(r, "", "  ")
	if err != nil {
		return "", "", err
	}
	if err := os.WriteFile(js, append(data, '\n'), 0o644); err != nil {
		return "", "", fmt.Errorf("write %s: %w", js, err)
	}
	return md, js, nil
}
