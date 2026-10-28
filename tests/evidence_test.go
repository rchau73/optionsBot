package tests

import (
	"bytes"
	"math"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"optionsbot/internal/config"
	"optionsbot/internal/evidence"
	"optionsbot/internal/history"
	"optionsbot/internal/orders"
	"optionsbot/internal/recorder"
)

// cmd/evidence (#176): the weekly evidence report — live, replay, gap,
// stress and hold, each with its source; missing inputs reported.

var evWeek = evidence.Window{From: time.Date(2026, 10, 12, 0, 0, 0, 0, time.UTC),
	To: time.Date(2026, 10, 19, 0, 0, 0, 0, time.UTC), Label: "2026-W42"}

// evRecord records hourly flat books (spot 100,000) from Oct 9 through Oct
// 18, skipping the given days.
func evRecord(t *testing.T, skip ...int) []recorder.Snapshot {
	t.Helper()
	dir := t.TempDir()
	for d := 9; d <= 18; d++ {
		if contains(skip, d) {
			continue
		}
		record(t, dir, time.Date(2026, 10, d, 0, 0, 0, 0, time.UTC), 24, func(int) float64 { return 100000 })
	}
	snaps, err := recorder.ReadDir(dir, time.Time{}, evWeek.Last())
	if err != nil {
		t.Fatal(err)
	}
	return snaps
}

func contains(xs []int, x int) bool {
	for _, v := range xs {
		if v == x {
			return true
		}
	}
	return false
}

// evInputs is a week of one BTC bot: a strangle opened before the week, its
// put stopped inside it, hourly P&L history with spot and margin, and two
// stress outcomes.
func evInputs(t *testing.T, snaps []recorder.Snapshot) evidence.Inputs {
	t.Helper()
	cfg, err := config.Load("../config_btc.yaml")
	if err != nil {
		t.Fatal(err)
	}
	slot := &orders.SlotRef{DTE: 25, Delta: 0.16}
	day := func(d, h int) time.Time { return time.Date(2026, 10, d, h, 0, 0, 0, time.UTC) }
	trades := []orders.Trade{
		{At: day(10, 12), Kind: "open", Slot: slot, Instrument: "BTC-30OCT26-112000-C", OptionType: "call", Qty: 1, Price: 0.004},
		{At: day(10, 12), Kind: "open", Slot: slot, Instrument: "BTC-30OCT26-90000-P", OptionType: "put", Qty: 1, Price: 0.006},
		{At: day(14, 9), Kind: "close", Slot: slot, Instrument: "BTC-30OCT26-90000-P", OptionType: "put", Qty: 1, Reason: "stop_loss", PnL: -0.0065, Fees: 0.0006},
		{At: day(16, 9), Kind: "open", Slot: slot, Instrument: "BTC-30OCT26-90000-P", OptionType: "put", Qty: 1, Price: 0.005},
		{At: day(19, 1), Kind: "close", Slot: slot, Instrument: "BTC-30OCT26-112000-C", OptionType: "call", Qty: 1, Reason: "roi_target", PnL: 0.002, Fees: 0.0005}, // after the week
	}
	var points []history.Point
	for h := 0; h < 7*24; h++ {
		im, limit, allowed := 10.0, 20.0, true
		points = append(points, history.Point{T: evWeek.From.Add(time.Duration(h) * time.Hour),
			Total: 0.0001 * float64(h), Unrealised: 0.003, Spot: 100000 + 10*float64(h),
			IMPct: &im, IMLimitPct: &limit, EntriesAllowed: &allowed})
	}
	crash, err := evidence.ParseStressResult("crash",
		"RESULT\tcrash+hold\t0.30\t0.9500\t-5.00\t-30000\t-25000\t2\t3\t4\t-0.0400\t45.0\t-8.50\t0\t0.0000\t0\t-0.0500\t-3750\n")
	if err != nil {
		t.Fatal(err)
	}
	closes, dvol := flatHistory(time.Date(2026, 10, 9, 0, 0, 0, 0, time.UTC))
	return evidence.Inputs{Env: "stable", Bot: "btc", Window: evWeek, Cfg: cfg, Config: "config_btc.yaml",
		Trades: trades, Journal: "data/btc/orders.log", Points: points, History: "data/btc/data/pnl_history.jsonl",
		Snaps: snaps, Market: "data/btc/data/market", Closes: closes, DVOL: dvol,
		Equity: 1, EquitySource: "account.json",
		Stress: []evidence.StressOutcome{crash, {Shock: "rally", Error: "exit status 1: boom"}}, StressDir: "data/stress/btc",
		StressBookAt: evWeek.To.Add(-2 * time.Hour)}
}

// Scenario: a week's report — given a journal, pnl_history and recorded
// books for a week, the report shows live, replay, gap, stress and hold
// figures, each with its source.
func TestEvidence_AWeeksReport(t *testing.T) {
	t.Parallel()
	r := evidence.Build(evInputs(t, evRecord(t)))

	l := r.Live
	if l.Opens != 1 || l.Closes != 1 || math.Abs(l.Realised+0.0065) > 1e-12 || math.Abs(l.Fees-0.0006) > 1e-12 {
		t.Errorf("live: %d opens %d closes realised %.5f fees %.5f; want the week's 1 open, 1 stop at −0.0065 with 0.0006 fees", l.Opens, l.Closes, l.Realised, l.Fees)
	}
	if s := l.ByReason["stop_loss"]; s.Closes != 1 || len(l.ByReason) != 1 {
		t.Errorf("by reason %+v", l.ByReason)
	}
	if l.Unrealised == nil || *l.Unrealised != 0.003 || math.Abs(l.Perf.NetPnL-0.0167) > 1e-9 || len(l.Perf.MissingDays) != 0 {
		t.Errorf("perf: unrealised %v net %.5f missing %v", l.Unrealised, l.Perf.NetPnL, l.Perf.MissingDays)
	}

	rp := r.Replay
	if !rp.Ran || rp.StartLegs != 2 || !rp.Start.Equal(evWeek.From) || rp.Snapshots != 7*24 {
		t.Fatalf("replay: ran %v (%s), start legs %d, start %s, %d decisions", rp.Ran, rp.Note, rp.StartLegs, rp.Start, rp.Snapshots)
	}
	g := r.Gap
	if !g.Known || g.Replay != rp.Net || math.Abs(g.Gap-(rp.Net-g.Live.Net)) > 1e-12 || g.Live.Closes != 1 || len(g.Unmodelled) == 0 {
		t.Errorf("gap: %+v (replay net %.5f)", g, rp.Net)
	}

	st := r.Stress
	if !st.Ran || st.Stale || len(st.Outcomes) != 2 || st.Outcomes[0].VsHoldCoin != -0.05 || st.Outcomes[0].DeltaExits != 3 {
		t.Errorf("stress: %+v", st)
	}

	h := r.Hold
	if !h.Known || h.SpotStart != 100000 || h.SpotEnd != 100000+10*167 || !h.StrategyKnown ||
		math.Abs(h.ExcessReturnPct-(h.ReturnUSDPct-h.HoldReturnUSDPct)) > 1e-9 {
		t.Errorf("hold: %+v", h)
	}
	if u := r.Util.Stats; u.MeanPct == nil || math.Abs(*u.MeanPct-50) > 1e-9 {
		t.Errorf("utilisation: %+v", u)
	}
	if len(r.Missing.BookDays)+len(r.Missing.HistoryDays) != 0 || len(r.Missing.GEXDays) != 7 {
		t.Errorf("missing: %+v; want only the minute GEX (none recorded)", r.Missing)
	}

	var b bytes.Buffer
	if err := evidence.WriteMarkdown(&b, r); err != nil {
		t.Fatal(err)
	}
	md := b.String()
	for _, want := range []string{"## Live\n\nSource: data/btc/orders.log", "## Replay\n\nSource:", "data/btc/data/market",
		"## Gap (replay − live)\n\nSource:", "not modelled by the replay", "## Stress\n\nSource: data/stress/btc",
		"| crash | -0.0500 |", "| rally | failed: exit status 1: boom", "## Benchmark: holding the coin\n\nSource: data/btc/data/pnl_history.jsonl",
		"## Margin utilisation\n\nSource:", "minute GEX records: **7 day(s) missing**"} {
		if !strings.Contains(md, want) {
			t.Errorf("markdown lacks %q:\n%s", want, md)
		}
	}
}

// Scenario: missing inputs — given no recorded books for two days, the
// report says which days are missing and computes the rest.
func TestEvidence_MissingInputs(t *testing.T) {
	t.Parallel()
	r := evidence.Build(evInputs(t, evRecord(t, 14, 15)))
	if want := []string{"2026-10-14", "2026-10-15"}; !reflect.DeepEqual(r.Missing.BookDays, want) {
		t.Errorf("missing book days %v, want %v", r.Missing.BookDays, want)
	}
	if !r.Replay.Ran || r.Replay.Snapshots != 5*24 || !r.Gap.Known {
		t.Errorf("replay ran %v (%s) on %d decisions: want the other 5 days replayed", r.Replay.Ran, r.Replay.Note, r.Replay.Snapshots)
	}
	if r.Live.Closes != 1 || !r.Hold.Known {
		t.Errorf("live and hold must still be computed: %+v %+v", r.Live, r.Hold)
	}
	var b bytes.Buffer
	_ = evidence.WriteMarkdown(&b, r)
	if !strings.Contains(b.String(), "recorded books: **2 day(s) missing** — 2026-10-14, 2026-10-15") {
		t.Errorf("markdown does not name the missing days:\n%s", b.String())
	}
}

// No books, no equity, no stress inputs, no history: every section says why
// it is empty instead of showing zeros.
func TestEvidence_EmptyInputsSayWhy(t *testing.T) {
	t.Parallel()
	in := evInputs(t, nil)
	in.Points, in.Stress, in.StressBookAt, in.StressNote = nil, nil, time.Time{}, "no captured book"
	r := evidence.Build(in)
	if r.Replay.Ran || !strings.Contains(r.Replay.Note, "no recorded book") || r.Gap.Known {
		t.Errorf("replay %+v gap %+v", r.Replay, r.Gap)
	}
	if len(r.Missing.BookDays) != 7 || len(r.Missing.HistoryDays) != 7 || r.Hold.Known || r.Live.Unrealised != nil {
		t.Errorf("missing %+v hold %+v", r.Missing, r.Hold)
	}
	var b bytes.Buffer
	_ = evidence.WriteMarkdown(&b, r)
	for _, want := range []string{"Not run: no recorded book", "Not available: the replay did not run", "Not run: no captured book",
		"P&L history: no point in the week", "Not available: no P&L history point"} {
		if !strings.Contains(b.String(), want) {
			t.Errorf("markdown lacks %q", want)
		}
	}

	in = evInputs(t, evRecord(t))
	in.Equity = 0
	if r := evidence.Build(in); r.Replay.Ran || !strings.Contains(r.Replay.Note, "margin balance") || r.Hold.StrategyKnown {
		t.Errorf("without equity: replay %+v hold %+v", r.Replay, r.Hold)
	}
	in.Equity, in.StressBookAt = 1, evWeek.To.AddDate(0, 0, -4)
	if r := evidence.Build(in); !r.Stress.Stale {
		t.Errorf("a book captured 4 days before the week's end must be flagged stale")
	}
}

func TestEvidence_Weeks(t *testing.T) {
	t.Parallel()
	day := func(y int, m time.Month, d int) time.Time { return time.Date(y, m, d, 0, 0, 0, 0, time.UTC) }
	for _, c := range []struct {
		in   string
		from time.Time
	}{
		{"2026-W42", day(2026, 10, 12)},
		{"2026-W01", day(2025, 12, 29)},
		{"2026-W53", day(2026, 12, 28)}, // 2026 starts on a Thursday: 53 weeks
		{"2027-W01", day(2027, 1, 4)},
	} {
		w, err := evidence.ParseWeek(c.in)
		if err != nil || !w.From.Equal(c.from) || !w.To.Equal(c.from.AddDate(0, 0, 7)) || w.Label != c.in {
			t.Errorf("%s: %+v %v, want from %s", c.in, w, err, c.from)
		}
	}
	for _, bad := range []string{"2025-W53", "2026-W00", "2026-42", "W42"} {
		if _, err := evidence.ParseWeek(bad); err == nil {
			t.Errorf("%s: want an error", bad)
		}
	}
	if w := evidence.LastWeek(time.Date(2026, 10, 19, 12, 0, 0, 0, time.UTC)); w.Label != "2026-W42" {
		t.Errorf("Monday Oct 19: last week %s, want 2026-W42", w.Label)
	}
	if w := evidence.LastWeek(time.Date(2026, 10, 18, 23, 0, 0, 0, time.UTC)); w.Label != "2026-W41" {
		t.Errorf("Sunday Oct 18: last week %s, want 2026-W41 (W42 not complete)", w.Label)
	}
	w, err := evidence.ParseDays("2026-10-12", "2026-10-14")
	if err != nil || !w.To.Equal(day(2026, 10, 15)) || w.Label != "2026-10-12_2026-10-14" {
		t.Errorf("days: %+v %v", w, err)
	}
	if _, err := evidence.ParseDays("2026-10-14", "2026-10-12"); err == nil {
		t.Errorf("-to before -from: want an error")
	}
}

func TestEvidence_StressResultAndArgs(t *testing.T) {
	t.Parallel()
	if _, err := evidence.ParseStressResult("crash", "no result here\n"); err == nil {
		t.Errorf("output without RESULT: want an error")
	}
	if _, err := evidence.ParseStressResult("crash", "RESULT\tcrash\t0.3\n"); err == nil {
		t.Errorf("short RESULT line: want an error")
	}

	cfg, err := config.Load("../config_btc.yaml")
	if err != nil {
		t.Fatal(err)
	}
	args := strings.Join(evidence.StressArgs(cfg, "d", "rally40"), " ")
	for _, want := range []string{"-dir d", "-currency BTC", "-shock rally40", "-calm 0", "-quiet", "-slots 25:0.16,50:0.16",
		"-direction " + cfg.GEXDirection, "-flipbufsd 1.5", "-breach 0.3", "-bands 20,35,50"} {
		if !strings.Contains(args, want) {
			t.Errorf("stress args %q lack %q", args, want)
		}
	}
	if strings.Contains(args, "-volscale") {
		t.Errorf("BTC: no -volscale")
	}
	eth, err := config.Load("../config_eth.yaml")
	if err != nil {
		t.Fatal(err)
	}
	if args := strings.Join(evidence.StressArgs(eth, "d", "crash"), " "); !strings.Contains(args, "-currency ETH") || !strings.Contains(args, "-volscale 1.4") {
		t.Errorf("ETH stress args %q", args)
	}
}

func TestEvidence_Paths(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	if got := evidence.ConfigPath(root, "btc"); got != filepath.Join(root, "config_btc.yaml") {
		t.Errorf("no deploy.env: %s", got)
	}
	env := "OPTIONSBOT_ENV=mvp\nexport BOT_CONFIG_BTC=\"./config_btc.mvp.yaml\"\n"
	if err := os.WriteFile(filepath.Join(root, "deploy.env"), []byte(env), 0o644); err != nil {
		t.Fatal(err)
	}
	if got := evidence.ConfigPath(root, "btc"); got != filepath.Join(root, "config_btc.mvp.yaml") {
		t.Errorf("mvp BTC: %s", got)
	}
	if got := evidence.ConfigPath(root, "eth"); got != filepath.Join(root, "config_eth.yaml") {
		t.Errorf("mvp ETH: %s", got)
	}
	p := evidence.BotPaths(root, "/stable", "btc")
	if p.Journal != filepath.Join(root, "data/btc/orders.log") || p.History != filepath.Join(root, "data/btc/data/pnl_history.jsonl") ||
		p.Market != "/stable/data/btc/data/market" || p.GEX != "/stable/data/btc/data/market_gex" || p.Stress != filepath.Join(root, "data/stress/btc") {
		t.Errorf("paths %+v", p)
	}
	md, js := evidence.OutPaths(root, "2026-W42", "btc")
	if md != filepath.Join(root, "data/evidence/2026-W42/btc.md") || js != filepath.Join(root, "data/evidence/2026-W42/btc.json") {
		t.Errorf("out %s %s", md, js)
	}
}

// End to end on an environment folder: Load reads the bot's files, RunStress
// runs every standard shock through the stress command (a fake here, one
// shock failing), and Write leaves the .md and .json in data/evidence/<week>/.
func TestEvidence_LoadRunStressWrite(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	mkfile := func(rel, body string) {
		t.Helper()
		p := filepath.Join(root, rel)
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	cfg, err := os.ReadFile("../config_btc.yaml")
	if err != nil {
		t.Fatal(err)
	}
	mkfile("config_btc.yaml", string(cfg))
	mkfile("data/btc/orders.log", `{"timestamp":"2026-10-14T09:00:00Z","event":"closed","status":"closed","instrument":"BTC-30OCT26-90000-P","qty":1,"close_reason":"stop_loss","pnl":-0.0065,"fees":0.0006,"delta":0}`+"\n")
	mkfile("data/btc/data/pnl_history.jsonl", `{"t":"2026-10-12T00:00:00Z","total":0,"spot":100000}`+"\n"+`{"t":"2026-10-18T23:00:00Z","total":0.01,"spot":110000}`+"\n")
	mkfile("data/stress/btc/positions.json", "{}")
	mkfile("data/stress/btc/account.json", `{"snapshot":{"assets":[{"currency":"BTC","margin_balance":2.5}]}}`)
	mkfile("fake_stress.sh", "case \"$*\" in *chop*) echo 'stress: bad book' >&2; exit 1;; esac\n"+
		"printf 'MODEL table\\nRESULT\\tx\\t0.3\\t2.4\\t-4\\t0\\t0\\t1\\t2\\t3\\t-0.01\\t40\\t-6\\t0\\t0\\t0\\t-0.1\\t-11000\\n'\n")

	var notes bytes.Buffer
	in := evidence.Load("stable", "btc", evidence.BotPaths(root, root, "btc"), evWeek, 0, &notes)
	if in.Cfg == nil || len(in.Trades) != 1 || len(in.Points) != 2 || in.Equity != 2.5 {
		t.Fatalf("loaded cfg %v, %d trades, %d points, equity %v; notes:\n%s", in.Cfg != nil, len(in.Trades), len(in.Points), in.Equity, notes.String())
	}
	if !strings.Contains(notes.String(), "market.json") {
		t.Errorf("a missing market.json must be noted: %q", notes.String())
	}
	evidence.RunStress(&in, "sh "+filepath.Join(root, "fake_stress.sh"))
	if len(in.Stress) != len(evidence.StandardShocks) || in.StressBookAt.IsZero() {
		t.Fatalf("stress outcomes %+v", in.Stress)
	}
	for _, o := range in.Stress {
		if o.Shock == "chop" {
			if !strings.Contains(o.Error, "stress: bad book") {
				t.Errorf("chop: error %q, want the tool's last stderr line", o.Error)
			}
		} else if o.Error != "" || o.VsHoldCoin != -0.1 || o.Stops != 1 || !strings.Contains(o.Command, "-shock "+o.Shock) {
			t.Errorf("%s: %+v", o.Shock, o)
		}
	}

	r := evidence.Build(in)
	if r.Replay.Ran || len(r.Missing.BookDays) != 7 || r.Live.Fees != 0.0006 || !r.Hold.Known {
		t.Errorf("report: replay %+v missing %+v live fees %v hold %+v", r.Replay, r.Missing, r.Live.Fees, r.Hold)
	}
	md, js := evidence.OutPaths(root, evWeek.Label, "btc")
	if err := evidence.Write(r, md, js); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(js)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Contains(data, []byte(`"week": "2026-W42"`)) || !bytes.Contains(data, []byte(`"vs_hold_coin": -0.1`)) {
		t.Errorf("json:\n%s", data)
	}
	if body, err := os.ReadFile(md); err != nil || !bytes.Contains(body, []byte("# Evidence report — stable BTC, 2026-W42")) {
		t.Errorf("markdown %v:\n%s", err, body)
	}

	var none evidence.Inputs
	evidence.RunStress(&none, "none")
	if none.StressNote == "" || none.Stress != nil {
		t.Errorf("-stress-cmd none: %+v", none)
	}
	in = evidence.Load("stable", "btc", evidence.BotPaths(t.TempDir(), root, "btc"), evWeek, 0, &notes)
	evidence.RunStress(&in, "sh "+filepath.Join(root, "fake_stress.sh"))
	if in.Stress != nil || !strings.Contains(in.StressNote, "stress_inputs.sh btc") || in.Trades != nil {
		t.Errorf("empty environment: stress %+v note %q trades %v", in.Stress, in.StressNote, in.Trades)
	}
}

// The journal's closes carry their fees into the trades the report sums.
func TestJournalReplay_CloseCarriesFees(t *testing.T) {
	line := `{"timestamp":"2026-10-14T09:00:00Z","event":"closed","status":"closed","instrument":"BTC-30OCT26-90000-P","qty":1,"close_reason":"stop_loss","pnl":-0.0065,"fees":0.0006,"delta":0}` + "\n"
	rp, err := orders.ReplayJournal(strings.NewReader(line), 0)
	if err != nil || len(rp.Trades) != 1 || rp.Trades[0].Fees != 0.0006 || rp.Trades[0].PnL != -0.0065 {
		t.Fatalf("trades %+v err %v", rp.Trades, err)
	}
}
