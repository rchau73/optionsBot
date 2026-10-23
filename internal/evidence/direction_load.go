package evidence

import (
	"bufio"
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"

	"optionsbot/internal/config"
	"optionsbot/internal/history"
	"optionsbot/internal/orders"
	"optionsbot/internal/recorder"
	"optionsbot/internal/replay"
)

// The impure side of the direction scorecard: reading both environments'
// files and writing the two outputs. BuildDirection stays pure.

// DirectionEnvRoot is one environment and its clone.
type DirectionEnvRoot struct {
	Env, Root string
}

// LoadDirection reads every input it can for bot: each environment's
// config, journal, direction history and equity (data/stress/<bot>/
// account.json), and base's recorded books (under booksRoot), market.json
// and minute GEX. A missing input is written to notes and left empty.
func LoadDirection(bot string, envs []DirectionEnvRoot, base, booksRoot string, w Window, notes io.Writer) DirectionInputs {
	in := DirectionInputs{Bot: bot, Window: w, Base: base}
	for _, er := range envs {
		note := func(format string, a ...any) {
			fmt.Fprintf(notes, "%s %s: "+format+"\n", append([]any{er.Env, bot}, a...)...)
		}
		root := booksRoot
		if er.Env != base {
			root = er.Root // only base's books are read
		}
		p := BotPaths(er.Root, root, bot)
		e := DirectionEnv{Env: er.Env, Config: p.Config, Journal: p.Journal, DirHistory: p.Directions}
		cfg, err := config.Load(p.Config)
		if err != nil {
			note("config not loaded: %v", err)
		} else {
			e.Direction = cfg.GEXDirection
		}
		if rp, err := orders.ReplayFile(p.Journal, 0); err != nil {
			note("journal not read: %v", err)
		} else {
			e.Trades = rp.Trades
		}
		recs, bad, err := ReadDirectionHistory(p.Directions)
		if err != nil {
			note("direction history not read: %v", err)
		} else if bad > 0 {
			note("direction history: %d unreadable lines skipped", bad)
		}
		e.Directions = recs
		acct := filepath.Join(p.Stress, "account.json")
		e.Equity, e.EquitySource = replay.ReadEquity(acct, bot), acct+" (margin balance when captured)"
		in.Envs = append(in.Envs, e)

		if er.Env != base {
			continue
		}
		in.Cfg, in.Market, in.GEXDir = cfg, p.Market, p.GEX
		if in.Snaps, err = recorder.ReadDir(p.Market, w.From.Add(-WarmUp), w.Last()); err != nil {
			note("recorded books not read: %v", err)
		}
		if in.GEX, err = recorder.ReadGEXDir(p.GEX, w.From.Add(-WarmUp), w.Last()); err != nil {
			note("minute GEX not read: %v", err)
		}
		if in.Closes, in.DVOL, err = replay.ReadMarketJSON(filepath.Join(p.Stress, "market.json")); err != nil {
			note("%v: the SMA trend and IV percentile start from the recording alone", err)
		}
	}
	return in
}

// ReadDirectionHistory reads data/direction_history.jsonl (history.
// DirectionLog), oldest first as written; unreadable lines are counted and
// skipped.
func ReadDirectionHistory(path string) ([]history.DirectionRecord, int, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, 0, err
	}
	defer f.Close()
	var out []history.DirectionRecord
	bad := 0
	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 64*1024), 1024*1024)
	for sc.Scan() {
		line := bytes.TrimSpace(sc.Bytes())
		if len(line) == 0 {
			continue
		}
		var r history.DirectionRecord
		if json.Unmarshal(line, &r) != nil || r.At.IsZero() {
			bad++
			continue
		}
		out = append(out, r)
	}
	return out, bad, sc.Err()
}

// WriteDirection writes r as Markdown to md and as JSON to js, creating
// their folder.
func WriteDirection(r DirectionReport, md, js string) error {
	if err := os.MkdirAll(filepath.Dir(md), 0o755); err != nil {
		return err
	}
	var b bytes.Buffer
	if err := WriteDirectionMarkdown(&b, r); err != nil {
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
