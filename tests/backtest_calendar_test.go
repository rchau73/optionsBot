package tests

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"optionsbot/internal/backtest"
	"optionsbot/internal/config"
	"optionsbot/internal/marketdata"
)

func calDay(s string) time.Time {
	d, err := time.Parse("2006-01-02", s)
	if err != nil {
		panic(err)
	}
	return d
}

func TestDeribitListedExpiries(t *testing.T) {
	// 2026-10-14, the day of #83's roll: weeklies up to 22 days (16OCT,
	// 23OCT, 30OCT, 6NOV is 23 days out — not yet listed), monthlies 30OCT,
	// 27NOV, 25DEC, then the quarterlies 26MAR27 and 25JUN27.
	got := backtest.DeribitListedExpiries(calDay("2026-10-14"))
	var names []string
	for _, e := range got {
		names = append(names, e.Format("2006-01-02"))
	}
	want := "2026-10-16 2026-10-23 2026-10-30 2026-11-27 2026-12-25 2027-03-26 2027-06-25"
	if strings.Join(names, " ") != want {
		t.Errorf("listed on 2026-10-14:\n got %s\nwant %s", strings.Join(names, " "), want)
	}
	// The next day 6NOV is 22 days out and listed, as testnet did.
	if got := backtest.DeribitListedExpiries(calDay("2026-10-15")); got[3].Format("2006-01-02") != "2026-11-06" {
		t.Errorf("6NOV should be listed on 2026-10-15: %v", got)
	}
	// An expiry is never listed on or after its own day.
	for _, e := range backtest.DeribitListedExpiries(calDay("2026-10-30")) {
		if !e.After(calDay("2026-10-30")) || e.Hour() != 8 {
			t.Errorf("listed %s on 2026-10-30", e)
		}
	}
	// December's monthly is a quarterly: counted once.
	seen := map[time.Time]bool{}
	for _, e := range backtest.DeribitListedExpiries(calDay("2026-11-30")) {
		if seen[e] {
			t.Errorf("listed twice: %s", e)
		}
		seen[e] = true
	}
}

func TestWriteSynthetic(t *testing.T) {
	from, to := calDay("2026-10-12"), calDay("2026-10-16")
	for _, cal := range []string{backtest.CalendarMonthly, backtest.CalendarDeribit} {
		t.Run(cal, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "o.csv")
			var buf bytes.Buffer
			rows, err := backtest.WriteSynthetic(&buf, backtest.SyntheticOptions{From: from, To: to, Calendar: cal})
			if err != nil || rows == 0 {
				t.Fatalf("rows %d err %v", rows, err)
			}
			if err := os.WriteFile(path, buf.Bytes(), 0o600); err != nil {
				t.Fatal(err)
			}
			feed, err := backtest.NewHistoricalFeed(path, from, to, 30)
			if err != nil {
				t.Fatal(err)
			}
			expiries := map[time.Time]bool{}
			for !feed.Done() {
				tk, err := feed.NextTick()
				if err != nil {
					t.Fatal(err)
				}
				expiries[tk.Expiry] = true
				dte := marketdata.DaysToExpiry(tk.Expiry, tk.Timestamp)
				if (cal == backtest.CalendarMonthly && dte < 21) || dte < 0 {
					t.Errorf("%s calendar lists %s at %d DTE", cal, tk.Instrument, dte)
				}
			}
			weekly := false
			for e := range expiries {
				weekly = weekly || !backtest.IsMonthlyExpiry(e)
			}
			if weekly != (cal == backtest.CalendarDeribit) {
				t.Errorf("weeklies listed = %v on the %s calendar", weekly, cal)
			}
		})
	}
	if _, err := backtest.WriteSynthetic(&bytes.Buffer{}, backtest.SyntheticOptions{From: from, To: to, Calendar: "daily"}); err == nil {
		t.Error("an unknown calendar must fail")
	}
	if _, err := backtest.WriteSynthetic(&bytes.Buffer{}, backtest.SyntheticOptions{From: to, To: from}); err == nil {
		t.Error("to before from must fail")
	}
}

// ladderStudy accumulates one ladder's results over several price paths.
type ladderStudy struct {
	n                              int
	occ, minOcc, pnl, rom, pnlDown float64
	worstWeek, maxDD               float64 // the worst of any path
	trades                         int
}

// Issue #83's study: the ladder sweep (shipped BTC config; a ladder replaces
// the whole matrix, so ETH's 30/50 is the 30/50 row) on 90 days of synthetic
// data listed like Deribit (weeklies 22 days out, 3 monthlies, quarterlies),
// over 5 price paths (seeds). The table is logged (go test -run
// TestLadderSweep_DeribitCalendar90Days -v): means over the paths, the worst
// week and drawdown of the worst path. The assertions are the acceptance
// scenarios:
//   - a slot right after its time roll: with weeklies, today's ladders keep
//     their short slot occupied far more than on monthlies alone;
//   - slots stay on separate expiries: no ladder ever shares one.
func TestLadderSweep_DeribitCalendar90Days(t *testing.T) {
	from, to := calDay("2026-07-01"), calDay("2026-09-28") // 90 days
	cfg, err := config.Load("../config_btc.yaml")
	if err != nil {
		t.Fatal(err)
	}
	seeds := []int64{42, 43, 44, 45, 46}
	var order []string
	studies := map[string]*ladderStudy{}
	for _, seed := range seeds {
		path := filepath.Join(t.TempDir(), "options.csv")
		f, err := os.Create(path)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := backtest.WriteSynthetic(f, backtest.SyntheticOptions{From: from, To: to, Calendar: backtest.CalendarDeribit, Seed: seed}); err != nil {
			t.Fatal(err)
		}
		if err := f.Close(); err != nil {
			t.Fatal(err)
		}
		results, err := backtest.RunLadderSweep(cfg, path, from, to, t.TempDir())
		if err != nil {
			t.Fatal(err)
		}
		for _, r := range results {
			if r.SharedExpiryDays != 0 {
				t.Errorf("seed %d %s: two slots shared an expiry on %d days", seed, r.Ladder.Name(), r.SharedExpiryDays)
			}
			name := r.Ladder.Name()
			s := studies[name]
			if s == nil {
				s = &ladderStudy{}
				studies[name] = s
				order = append(order, name)
			}
			s.n++
			s.occ += r.OccupancyPct
			s.minOcc += r.MinSlotOccupancyPct
			s.pnl += r.TotalPnLUSD
			s.rom += r.ReturnOnMarginPct
			if r.TotalPnLUSD < 0 {
				s.pnlDown++
			}
			s.worstWeek = min(s.worstWeek, r.WorstWeekPct)
			s.maxDD = max(s.maxDD, r.MaxDrawdownPct)
			s.trades += r.Trades
		}
	}

	var table strings.Builder
	fmt.Fprintf(&table, "\n90 days (%s → %s), %d paths, means unless noted\n", from.Format("2006-01-02"), to.Format("2006-01-02"), len(seeds))
	fmt.Fprintf(&table, "%-18s %6s %6s %10s %8s %6s %13s %11s %7s\n", "ladder", "occ%", "min%", "pnl_usd", "rom%", "losing", "worst_week%", "max_dd%", "trades")
	for _, name := range order {
		s := studies[name]
		n := float64(s.n)
		fmt.Fprintf(&table, "%-18s %6.1f %6.1f %10.0f %8.1f %4.0f/%d %13.2f %11.2f %7.1f\n",
			name, s.occ/n, s.minOcc/n, s.pnl/n, s.rom/n, s.pnlDown, s.n, s.worstWeek, s.maxDD, float64(s.trades)/n)
	}
	t.Log(table.String())

	for _, today := range []string{"25/50", "30/50"} { // BTC, ETH
		with, without := studies[today], studies[today+" monthlies"]
		if with == nil || without == nil {
			t.Fatalf("%s missing from the sweep", today)
		}
		if with.minOcc <= without.minOcc {
			t.Errorf("%s: weeklies should fill the roll gap: mean min slot occupancy %.1f%% with, %.1f%% without",
				today, with.minOcc/float64(with.n), without.minOcc/float64(without.n))
		}
	}
}
