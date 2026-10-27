package tests

import (
	"bytes"
	"encoding/json"
	"math"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"optionsbot/internal/evidence"
	"optionsbot/internal/recorder"
)

// The weekly evidence report's recorded-history coverage section (#362):
// each dataset's share of the week's hours, its gaps, the days decided
// hourly, the replay labelled partial under ReplayFullCoveragePct of market
// hours, and gaps over LongGapHours named for evidence.sh's notice.

// covSets are the recorded sets of a week: the market books from snaps, a
// minute GEX record every minute of the week, and market_usdc absent.
func covSets(snaps []recorder.Snapshot) []evidence.RecordedSet {
	var books, gex []time.Time
	for _, s := range snaps {
		books = append(books, s.At)
	}
	for t := evWeek.From; t.Before(evWeek.To); t = t.Add(time.Minute) {
		gex = append(gex, t.Add(5*time.Second))
	}
	return []evidence.RecordedSet{
		{Name: "market", Path: "data/btc/data/market", Step: time.Hour, Found: true, Times: books},
		{Name: "market_gex", Path: "data/btc/data/market_gex", Step: time.Minute, Found: true, Times: gex},
		{Name: "market_usdc", Path: "data/btc/data/market_usdc", Step: time.Hour},
	}
}

// dropHours removes the books recorded in [from, from+n h).
func dropHours(snaps []recorder.Snapshot, from time.Time, n int) []recorder.Snapshot {
	var out []recorder.Snapshot
	for _, s := range snaps {
		if !s.At.Before(from) && s.At.Before(from.Add(time.Duration(n)*time.Hour)) {
			continue
		}
		out = append(out, s)
	}
	return out
}

func covMarkdown(t *testing.T, r evidence.Report) string {
	t.Helper()
	var b bytes.Buffer
	if err := evidence.WriteMarkdown(&b, r); err != nil {
		t.Fatal(err)
	}
	return b.String()
}

func covDataset(t *testing.T, c evidence.Coverage, name string) evidence.DatasetCoverage {
	t.Helper()
	for _, d := range c.Datasets {
		if d.Dataset == name {
			return d
		}
	}
	t.Fatalf("no %s dataset in %+v", name, c.Datasets)
	return evidence.DatasetCoverage{}
}

// Scenario: Complete week — hourly market books for every hour of the week:
// 100 % for the market dataset, no gap, no "partial" label on the replay.
func TestEvidenceCoverage_CompleteWeek(t *testing.T) {
	snaps := evRecord(t)
	in := evInputs(t, snaps)
	in.Recorded = covSets(snaps)
	r := evidence.Build(in)

	c := r.Coverage
	m := covDataset(t, c, "market")
	if !c.Known || m.Status != evidence.CoverageRecorded || m.CoveredPct != 100 || len(m.Gaps) != 0 || m.MissingHours != 0 {
		t.Fatalf("market coverage %+v", m)
	}
	if c.MarketPct == nil || *c.MarketPct != 100 || c.Partial || len(c.LongGaps) != 0 || len(c.HourlyOnlyDays) != 0 {
		t.Errorf("coverage %+v", c)
	}
	if !r.Replay.Ran || r.Replay.Partial != "" || r.Gap.Partial != "" {
		t.Errorf("replay ran %v partial %q gap partial %q: a complete week carries no label", r.Replay.Ran, r.Replay.Partial, r.Gap.Partial)
	}
	md := covMarkdown(t, r)
	for _, want := range []string{"## Recorded-history coverage", "| market | data/btc/data/market | 100.0 % | 0 h | none | 7 of 7 |",
		"the market books cover 100.0 % of the hours (at least 90 %)", "Gaps over 3 h: none", "## Replay\n\nSource:"} {
		if !strings.Contains(md, want) {
			t.Errorf("markdown lacks %q:\n%s", want, md)
		}
	}
	if strings.Contains(md, "**partial") {
		t.Errorf("a complete week must not be labelled partial:\n%s", md)
	}
}

// Scenario: A recorder gap — the market dataset misses 6 consecutive hours
// on one day: the section lists the gap with its start and length and it
// is a notice; 96 % of the hours stay over the share, so no partial label.
func TestEvidenceCoverage_ARecorderGap(t *testing.T) {
	start := time.Date(2026, 10, 14, 3, 0, 0, 0, time.UTC)
	snaps := dropHours(evRecord(t), start, 6)
	in := evInputs(t, snaps)
	in.Recorded = covSets(snaps)
	r := evidence.Build(in)

	c := r.Coverage
	if len(c.LongGaps) != 1 {
		t.Fatalf("long gaps %+v, want the market's 6 h", c.LongGaps)
	}
	g := c.LongGaps[0]
	if g.Dataset != "market" || !g.From.Equal(start) || !g.To.Equal(start.Add(6*time.Hour)) || g.Hours != 6 {
		t.Errorf("gap %+v", g)
	}
	if c.MarketPct == nil || math.Abs(*c.MarketPct-100*162.0/168) > 1e-9 || c.Partial || r.Replay.Partial != "" {
		t.Errorf("market %v partial %v replay %q: 162 of 168 h is over the share", c.MarketPct, c.Partial, r.Replay.Partial)
	}
	md := covMarkdown(t, r)
	for _, want := range []string{"**Gaps over 3 h** (fix the recorder): market 6.0 h from 2026-10-14 03:00 UTC;",
		"- market missing: 2026-10-14 6.0 h · gaps 2026-10-14 03:00 → 2026-10-14 09:00 (6.0 h)"} {
		if !strings.Contains(md, want) {
			t.Errorf("markdown lacks %q:\n%s", want, md)
		}
	}

	// A gap of 3 h exactly is no notice.
	snaps = dropHours(evRecord(t), start, 3)
	in.Recorded = covSets(snaps)
	if r := evidence.Build(in); len(r.Coverage.LongGaps) != 0 {
		t.Errorf("a 3 h gap is not over %v h: %+v", evidence.LongGapHours, r.Coverage.LongGaps)
	}
}

// Scenario: A recorder gap under the stated share — three days of books
// missing (57 % of the hours): the replay and the gap are labelled partial.
func TestEvidenceCoverage_UnderTheShareIsPartial(t *testing.T) {
	snaps := evRecord(t, 14, 15, 16)
	in := evInputs(t, snaps)
	in.Recorded = covSets(snaps)
	r := evidence.Build(in)
	if !r.Coverage.Partial || !r.Replay.Ran || !strings.Contains(r.Replay.Partial, "57.1 %") || r.Gap.Partial != r.Replay.Partial {
		t.Fatalf("partial %v replay %q gap %q", r.Coverage.Partial, r.Replay.Partial, r.Gap.Partial)
	}
	md := covMarkdown(t, r)
	for _, want := range []string{"## Replay\n\n**partial: the market books cover 57.1 % of the week's hours, under 90 %**",
		"## Gap (replay − live)\n\n**partial:", "market 72.0 h from 2026-10-14 00:00 UTC"} {
		if !strings.Contains(md, want) {
			t.Errorf("markdown lacks %q:\n%s", want, md)
		}
	}
}

// Scenario: A dataset directory is absent — no market_usdc folder: the
// section says "not recorded" for it, names no gap for it, and the report
// still builds; read through Load on a clone that has only the books.
func TestEvidenceCoverage_AbsentDatasetIsNotRecorded(t *testing.T) {
	snaps := evRecord(t)
	in := evInputs(t, snaps)
	in.Recorded = covSets(snaps)
	r := evidence.Build(in)
	u := covDataset(t, r.Coverage, "market_usdc")
	if u.Status != evidence.CoverageNotRecorded || u.Found {
		t.Errorf("market_usdc %+v", u)
	}
	for _, g := range r.Coverage.LongGaps {
		if g.Dataset == "market_usdc" {
			t.Errorf("an absent dataset is no recorder gap: %+v", g)
		}
	}
	if md := covMarkdown(t, r); !strings.Contains(md, "| market_usdc | data/btc/data/market_usdc | not recorded |") {
		t.Errorf("markdown:\n%s", md)
	}

	// Through Load: books recorded, nothing else.
	root := t.TempDir()
	books := filepath.Join(root, "data", "btc", "data", "market")
	for d := 12; d <= 18; d++ {
		record(t, books, time.Date(2026, 10, d, 0, 0, 0, 0, time.UTC), 24, func(int) float64 { return 100000 })
	}
	var notes bytes.Buffer
	loaded := evidence.Load("stable", "btc", evidence.BotPaths(root, root, "btc"), evWeek, 1, &notes)
	got := map[string]string{}
	for _, s := range loaded.Recorded {
		got[s.Name] = map[bool]string{true: "found", false: "absent"}[s.Found]
	}
	if got["market"] != "found" || got["market_usdc"] != "absent" || got["market_gex"] != "absent" || got["pnl_history"] != "absent" {
		t.Fatalf("recorded sets %v", got)
	}
	r = evidence.Build(loaded)
	c := r.Coverage
	if m := covDataset(t, c, "market"); m.CoveredPct != 100 || m.Records != 7*24 {
		t.Errorf("market through Load %+v", m)
	}
	if len(c.LongGaps) != 0 || len(c.HourlyOnlyDays) != 7 {
		t.Errorf("long gaps %+v hourly-only days %v: GEX is not recorded, so every day is decided hourly", c.LongGaps, c.HourlyOnlyDays)
	}
	md, js := evidence.OutPaths(root, evWeek.Label, "btc")
	if err := evidence.Write(r, md, js); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(js)
	if err != nil {
		t.Fatal(err)
	}
	var back struct {
		Coverage struct {
			Datasets []struct {
				Dataset string  `json:"dataset"`
				Status  string  `json:"status"`
				Covered float64 `json:"covered_pct"`
			} `json:"datasets"`
			LongGaps []any `json:"long_gaps"`
		} `json:"coverage"`
	}
	if err := json.Unmarshal(data, &back); err != nil {
		t.Fatal(err)
	}
	if len(back.Coverage.Datasets) == 0 || back.Coverage.Datasets[0].Dataset != "market" || back.Coverage.Datasets[0].Covered != 100 ||
		back.Coverage.LongGaps == nil {
		t.Errorf("json coverage %+v", back.Coverage)
	}
}

// Scenario: Nothing filled in — with a gap, the market's records are only
// the recorded ones and the missing hours are listed by day.
func TestEvidenceCoverage_NothingFilledIn(t *testing.T) {
	snaps := dropHours(evRecord(t), time.Date(2026, 10, 15, 20, 0, 0, 0, time.UTC), 8) // across midnight
	in := evInputs(t, snaps)
	in.Recorded = covSets(snaps)
	m := covDataset(t, evidence.Build(in).Coverage, "market")
	if m.Records != 7*24-8 || m.MissingHours != 8 || m.CoveredBins != 7*24-8 {
		t.Errorf("records %d missing %v covered %d: nothing interpolated", m.Records, m.MissingHours, m.CoveredBins)
	}
	md := covMarkdown(t, evidence.Build(in))
	if !strings.Contains(md, "- market missing: 2026-10-15 4.0 h, 2026-10-16 4.0 h · gaps 2026-10-15 20:00 → 2026-10-16 04:00 (8.0 h)") {
		t.Errorf("markdown does not list the missing hours:\n%s", md)
	}

	// Coverage not read (an older caller): the section says so, nothing is labelled.
	in.Recorded = nil
	r := evidence.Build(in)
	if r.Coverage.Known || r.Replay.Partial != "" || !strings.Contains(covMarkdown(t, r), "Not measured: the recorded datasets were not read") {
		t.Errorf("unread coverage %+v replay %q", r.Coverage, r.Replay.Partial)
	}
}
