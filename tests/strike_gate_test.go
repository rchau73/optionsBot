package tests

import (
	"fmt"
	"strings"
	"testing"
	"time"

	"optionsbot/internal/config"
	"optionsbot/internal/marketdata"
	"optionsbot/internal/orders"
	"optionsbot/internal/strategy"
)

// The strike-quality gate (#553): a strike without a two-sided quote, or
// whose mark IV is far off its expiry's ATM IV, is never picked.

// strikeOpt is a quoted option of one expiry (bid = ask = 0: mark only).
func strikeOpt(exp time.Time, typ string, strike, delta, iv, bid, ask float64) *marketdata.Instrument {
	mid := (bid + ask) / 2
	if bid == 0 && ask == 0 {
		mid = 0.0004 // the mark, as handleTicker keeps it
	}
	return &marketdata.Instrument{Name: fmt.Sprintf("BTC-%.0f-%s", strike, typ[:1]), Expiry: exp, OptionType: typ,
		Strike: strike, Bid: bid, Ask: ask, Mid: mid, Greeks: marketdata.Greeks{Delta: delta, IV: iv}}
}

func TestStrikeQualityReason(t *testing.T) {
	exp := time.Date(2026, 12, 25, 8, 0, 0, 0, time.UTC)
	const atm, ratio = 0.38, 1.5
	cases := []struct {
		name string
		inst *marketdata.Instrument
		want string
	}{
		{"two-sided, IV on the smile", strikeOpt(exp, "call", 90000, 0.18, 0.40, 0.01, 0.011), ""},
		{"no bid, no ask", strikeOpt(exp, "call", 118000, 0.152, 0.40, 0, 0), strategy.StrikeNoQuote},
		{"ask only", strikeOpt(exp, "call", 100000, 0.15, 0.40, 0, 0.01), strategy.StrikeNoQuote},
		{"bid only", strikeOpt(exp, "call", 100000, 0.15, 0.40, 0.01, 0), strategy.StrikeNoQuote},
		{"IV at the upper edge (×1.5) passes", strikeOpt(exp, "call", 100000, 0.15, 0.57, 0.01, 0.011), ""},
		{"IV past the upper edge", strikeOpt(exp, "call", 100000, 0.15, 0.58, 0.01, 0.011), strategy.StrikeOffSmile},
		{"IV far below the ATM", strikeOpt(exp, "put", 60000, -0.15, 0.25, 0.01, 0.011), strategy.StrikeOffSmile},
		{"IV unknown: only the quote is judged", strikeOpt(exp, "call", 100000, 0.15, 0, 0.01, 0.011), ""},
	}
	for _, c := range cases {
		if got := strategy.StrikeQualityReason(c.inst, atm, ratio); got != c.want {
			t.Errorf("%s: %q, want %q", c.name, got, c.want)
		}
	}
	off := strikeOpt(exp, "call", 118000, 0.152, 0.882, 0.01, 0.011)
	if got := strategy.StrikeQualityReason(off, 0, ratio); got != "" {
		t.Errorf("ATM IV unknown: the IV gate is off, got %q", got)
	}
	if got := strategy.StrikeQualityReason(off, atm, 0); got != "" {
		t.Errorf("ratio 0: the IV gate is off, got %q", got)
	}
}

func TestATMIV(t *testing.T) {
	exp := time.Date(2026, 12, 25, 8, 0, 0, 0, time.UTC)
	chain := []*marketdata.Instrument{
		strikeOpt(exp, "call", 118000, 0.152, 0.882, 0, 0),
		strikeOpt(exp, "call", 100000, 0.48, 0.377, 0.05, 0.051),
		strikeOpt(exp, "put", 100000, -0.53, 0.39, 0.05, 0.051),
		strikeOpt(exp.AddDate(0, 1, 0), "call", 100000, 0.50, 0.60, 0.05, 0.051), // another expiry
	}
	if got := strategy.ATMIV(chain, exp); got != 0.377 {
		t.Errorf("ATM IV = %v, want 0.377 (|Δ| 0.48 is nearest 0.5)", got)
	}
	if got := strategy.ATMIV(chain, exp.AddDate(0, 0, 1)); got != 0 {
		t.Errorf("no option of that expiry: ATM IV = %v, want 0", got)
	}
}

// 2026-10-30 BTC (mvp), the chain the call repair read at 15:42 UTC.
func broken118000Chain(exp time.Time) []*marketdata.Instrument {
	return []*marketdata.Instrument{
		strikeOpt(exp, "call", 100000, 0.50, 0.377, 0.05, 0.051), // ATM
		strikeOpt(exp, "call", 118000, 0.152, 0.882, 0, 0),       // mark only, IV 2.3× the ATM
		strikeOpt(exp, "call", 90000, 0.18, 0.39, 0.012, 0.013),  // the strike that filled at 17:55
		strikeOpt(exp, "put", 66000, -0.16, 0.45, 0.010, 0.011),
	}
}

// Scenario: a strike with no quote and an off-smile IV is never picked.
func TestStrikeGate_BrokenMarkIsNeverPicked(t *testing.T) {
	exp := time.Date(2026, 12, 25, 8, 0, 0, 0, time.UTC)
	chain := broken118000Chain(exp)
	ratio := config.FixedStrikeIVMaxRatio

	// 118000-C's |Δ| 0.152 is the nearest 0.16; only the gate keeps it out.
	got, err := strategy.SelectStrike(chain, exp, "call", 0.16, 0.03, ratio)
	if err != nil || got.Strike != 90000 {
		t.Fatalf("repair pick = %v (%v), want 90000-C", got, err)
	}
	call, put, err := strategy.SelectStrikePair(chain, exp, 0.16, 0.03, ratio)
	if err != nil || call.Strike != 90000 || put.Strike != 66000 {
		t.Errorf("entry pair = %v / %v (%v), want 90000-C / 66000-P", call, put, err)
	}

	// Each cause alone keeps 118000-C out.
	for name, mutate := range map[string]func(*marketdata.Instrument){
		"quoted but off the smile":  func(in *marketdata.Instrument) { in.Bid, in.Ask, in.Mid = 0.0004, 0.0005, 0.00045 },
		"on the smile but no quote": func(in *marketdata.Instrument) { in.Greeks.IV = 0.40 },
	} {
		chain := broken118000Chain(exp)
		mutate(chain[1])
		if got, err := strategy.SelectStrike(chain, exp, "call", 0.16, 0.03, ratio); err != nil || got.Strike != 90000 {
			t.Errorf("%s: pick = %v (%v), want 90000-C", name, got, err)
		}
	}

	// Nothing passes: an error naming why, which the bot journals as no_strike.
	_, err = strategy.SelectStrike(chain[:2], exp, "call", 0.16, 0.03, ratio)
	if err == nil || !strings.Contains(err.Error(), strategy.StrikeNoQuote) {
		t.Errorf("only 118000-C: err = %v, want the reason named", err)
	}
}

// Scenario: a normal smile is unaffected — with every strike quoted and its
// IV within the gate, the pick is the same as without the gate.
func TestStrikeGate_NormalSmileIsUnaffected(t *testing.T) {
	exp := time.Date(2026, 12, 25, 8, 0, 0, 0, time.UTC)
	for _, atm := range []float64{0.30, 0.38, 0.55, 0.80} {
		var chain []*marketdata.Instrument
		for i := 0; i <= 20; i++ {
			d := 0.05 + float64(i)*0.0225 // |Δ| 0.05 … 0.50
			skew := 1 + 0.4*(0.5-d)       // wings up to 1.18× the ATM
			chain = append(chain,
				strikeOpt(exp, "call", 100000+float64(20-i)*2000, d, atm*skew*0.95, 0.01, 0.011),
				strikeOpt(exp, "put", 100000-float64(20-i)*2000, -d, atm*skew*1.1, 0.01, 0.011))
		}
		for _, target := range []float64{0.10, 0.16, 0.25} {
			c0, p0, err0 := strategy.SelectStrikePair(chain, exp, target, 0.03, 0)
			c1, p1, err1 := strategy.SelectStrikePair(chain, exp, target, 0.03, config.FixedStrikeIVMaxRatio)
			if err0 != nil || err1 != nil || c0 != c1 || p0 != p1 {
				t.Errorf("ATM %.2f target %.2f: gated pair %v/%v (%v) differs from today's %v/%v (%v)",
					atm, target, c1, p1, err1, c0, p0, err0)
			}
		}
	}
}

// The repair end to end: the lone put's call repair skips the mark-only,
// off-smile strike, journals no_strike while nothing passes, and sells the
// first strike that does.
func TestStrategy_RepairSkipsBrokenMarkAndJournalsNoStrike(t *testing.T) {
	f := newStrategyFixture(t)
	f.cfg.StrikeIVMaxRatio = config.FixedStrikeIVMaxRatio
	f.withOpenStrangle(0.2, 0.02)
	f.exch.positions = f.exch.positions[1:] // only the put is held: the call is missing
	f.market.mu.Lock()
	broken := f.market.instruments[f.call]
	broken.Bid, broken.Ask, broken.Mid = 0, 0, 0.0004 // mark only
	broken.Greeks = marketdata.Greeks{Delta: 0.152, IV: 0.882}
	f.market.instruments[f.put].Greeks.IV = 0.45
	atm := *broken
	atm.Name, atm.Strike, atm.Bid, atm.Ask, atm.Mid = "BTC-ATM-100000-C", 100000, 0.05, 0.051, 0.0505
	atm.Greeks = marketdata.Greeks{Delta: 0.50, IV: 0.377}
	f.market.instruments[atm.Name] = &atm
	f.market.mu.Unlock()
	f.startRun()

	eventually(t, 2*time.Second, "no_strike journaled", func() bool {
		for _, s := range f.journal.events(orders.EventSkipped) {
			if strings.HasPrefix(s.reason, strategy.SkipNoStrike+": repair call") {
				return true
			}
		}
		return false
	})
	if n := len(f.exch.sells()); n != 0 {
		t.Fatalf("nothing passes the gate: want no order, got %d", n)
	}

	good := atm
	good.Name, good.Strike, good.Bid, good.Ask, good.Mid = "BTC-GOOD-90000-C", 90000, 0.012, 0.013, 0.0125
	good.Greeks = marketdata.Greeks{Delta: 0.18, IV: 0.39}
	f.market.mu.Lock()
	f.market.instruments[good.Name] = &good
	f.market.mu.Unlock()

	eventually(t, 2*time.Second, "call repair sold", func() bool { return len(f.exch.sells()) >= 1 })
	for _, o := range f.exch.sells() {
		if o.Instrument != good.Name || o.TriggerReason != orders.TriggerRepair {
			t.Errorf("repair sold %s (%s), want %s and never %s", o.Instrument, o.TriggerReason, good.Name, f.call)
		}
	}
}
