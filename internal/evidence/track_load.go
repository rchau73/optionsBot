package evidence

import (
	"bufio"
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"optionsbot/internal/idlemargin"
)

// The impure side of the track record and the trend: finding the weekly
// reports, reading the P&L history and writing the outputs.

// TrackOutPaths are the track record's files:
// <root>/data/evidence/track-record/<bot>.md and .json.
func TrackOutPaths(root, bot string) (md, js string) {
	dir := filepath.Join(root, "data", "evidence", "track-record")
	return filepath.Join(dir, bot+".md"), filepath.Join(dir, bot+".json")
}

// TrendOutPaths are the trend's files: <root>/data/evidence/trend/<bot>.md
// and .json.
func TrendOutPaths(root, bot string) (md, js string) {
	dir := filepath.Join(root, "data", "evidence", "trend")
	return filepath.Join(dir, bot+".md"), filepath.Join(dir, bot+".json")
}

// WeeklyGlob names the weekly reports a track record reads.
func WeeklyGlob(root, bot string) string {
	return filepath.Join(root, "data", "evidence", "<YYYY-Www>", bot+".json")
}

// ReadWeekly reads every weekly report of bot under <root>/data/evidence/:
// the folders named as an ISO week (a -from/-to report, direction/, trend/
// and track-record/ are not weeks). A file that does not parse is listed in
// unreadable and skipped; its week then shows as not reported.
func ReadWeekly(root, bot string) (reports []Report, unreadable []string, err error) {
	dir := filepath.Join(root, "data", "evidence")
	entries, err := os.ReadDir(dir)
	if os.IsNotExist(err) {
		return nil, nil, nil
	}
	if err != nil {
		return nil, nil, err
	}
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		if w, err := ParseWeek(e.Name()); err != nil || w.Label != e.Name() {
			continue
		}
		path := filepath.Join(dir, e.Name(), bot+".json")
		data, err := os.ReadFile(path)
		if os.IsNotExist(err) {
			continue
		}
		var r Report
		if err == nil {
			err = json.Unmarshal(data, &r)
		}
		if err != nil || r.Week != e.Name() {
			unreadable = append(unreadable, path)
			continue
		}
		reports = append(reports, r)
	}
	sort.Strings(unreadable)
	return reports, unreadable, nil
}

// LoadTrack reads one bot's track-record inputs. A missing history is
// written to notes and leaves the record empty.
func LoadTrack(env, bot, root string, notes io.Writer) TrackInputs {
	p := BotPaths(root, root, bot)
	in := TrackInputs{Env: env, Bot: bot, Account: DeribitAccount(root), History: p.History, WeeklySource: WeeklyGlob(root, bot)}
	pts, bad, err := idlemargin.ReadHistoryFile(p.History)
	if err != nil {
		fmt.Fprintf(notes, "%s %s: P&L history not read: %v\n", env, bot, err)
	} else if bad > 0 {
		fmt.Fprintf(notes, "%s %s: P&L history: %d unreadable lines skipped\n", env, bot, bad)
	}
	in.Points = pts
	if in.Weekly, in.Unreadable, err = ReadWeekly(root, bot); err != nil {
		fmt.Fprintf(notes, "%s %s: weekly reports not read: %v\n", env, bot, err)
	}
	return in
}

// DeribitAccount is "live" when the clone's .env says DERIBIT_ENV=live, else
// "testnet" (the default, as the bot's).
func DeribitAccount(root string) string {
	f, err := os.Open(filepath.Join(root, ".env"))
	if err != nil {
		return "testnet"
	}
	defer f.Close()
	account := "testnet"
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		line := strings.TrimSpace(strings.TrimPrefix(strings.TrimSpace(sc.Text()), "export "))
		if v, ok := strings.CutPrefix(line, "DERIBIT_ENV="); ok {
			if strings.EqualFold(strings.Trim(strings.TrimSpace(v), `"'`), "live") {
				account = "live"
			} else {
				account = "testnet"
			}
		}
	}
	if sc.Err() != nil {
		return "testnet"
	}
	return account
}

// WriteTrack writes the track record as Markdown and JSON.
func WriteTrack(t TrackRecord, md, js string) error {
	return writeBoth(md, js, t, func(w io.Writer) error { return WriteTrackMarkdown(w, t) })
}

// WriteTrend writes the trend as Markdown and JSON.
func WriteTrend(tr Trend, md, js string) error {
	return writeBoth(md, js, tr, func(w io.Writer) error { return WriteTrendMarkdown(w, tr) })
}

func writeBoth(md, js string, v any, markdown func(io.Writer) error) error {
	if err := os.MkdirAll(filepath.Dir(md), 0o755); err != nil {
		return err
	}
	var b bytes.Buffer
	if err := markdown(&b); err != nil {
		return err
	}
	if err := os.WriteFile(md, b.Bytes(), 0o644); err != nil {
		return fmt.Errorf("write %s: %w", md, err)
	}
	data, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		return err
	}
	if err := os.WriteFile(js, append(data, '\n'), 0o644); err != nil {
		return fmt.Errorf("write %s: %w", js, err)
	}
	return nil
}
