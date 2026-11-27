package tests

import (
	"strings"
	"testing"
	"time"

	"optionsbot/internal/marketdata"
	"optionsbot/internal/orders"
)

// The DVOL sanity gate (#796): a live DVOL reading the market does not
// support is kept out of the tracker, so the flip buffer and the IV
// percentile keep the last good value.

var gateT0 = time.Date(2026, 11, 14, 5, 36, 0, 0, time.UTC)

func reading(min float64, value, spot, atm float64) marketdata.DVOLReading {
	return marketdata.DVOLReading{At: gateT0.Add(time.Duration(min * float64(time.Minute))), Value: value, Spot: spot, ATMIV: atm}
}

// Scenario: an implausible DVOL jump is not used (no option marks: the jump rule).
func TestDVOLGate_ImplausibleJumpIsNotUsed(t *testing.T) {
	var g marketdata.DVOLGate
	if ok, _ := g.CheckDVOL(reading(0, 40.9, 66800, 0), 0); !ok {
		t.Fatal("the first reading must be accepted")
	}
	ok, rej := g.CheckDVOL(reading(8, 110.7, 67100, 0), 40.9) // spot +0.45 %
	if ok || rej == nil {
		t.Fatalf("a +170 %% jump with spot flat must be rejected, got ok=%v rej=%v", ok, rej)
	}
	if rej.Reason != "jump" || rej.Value != 110.7 || rej.Kept != 40.9 ||
		!strings.Contains(rej.Detail, "110.7") || !strings.Contains(rej.Detail, "40.9") {
		t.Errorf("the rejection must name both values: %+v", rej)
	}
	// Later readings of the same episode are rejected without a new line.
	if ok, rej := g.CheckDVOL(reading(9, 111, 67000, 0), 40.9); ok || rej != nil {
		t.Errorf("the episode goes on silently, got ok=%v rej=%v", ok, rej)
	}
	// The 2026-11-14 staircase: each step is a jump from the last good value.
	var s marketdata.DVOLGate
	s.CheckDVOL(reading(0, 56, 66800, 0), 0)
	for i, v := range []float64{72, 84, 103, 111} {
		if ok, _ := s.CheckDVOL(reading(float64(2*i+2), v, 66800, 0), 56); ok {
			t.Errorf("staircase step %.0f with spot flat must be rejected", v)
		}
	}
}

// Scenario: an implausible DVOL jump is not used (the option marks disagree).
func TestDVOLGate_ReadingFarFromTheMarksIsNotUsed(t *testing.T) {
	var g marketdata.DVOLGate
	g.CheckDVOL(reading(0, 46.3, 65800, 40), 0)
	ok, rej := g.CheckDVOL(reading(170, 123.5, 65800, 39.5), 46.3) // 2026-11-15 09:00
	if ok || rej == nil || rej.Reason != "marks" || rej.ATMIV != 39.5 {
		t.Fatalf("DVOL 3× the marks' ATM IV must be rejected, got ok=%v %+v", ok, rej)
	}
	// It stays rejected however long the marks disagree, re-reported every 30 min.
	if ok, rej := g.CheckDVOL(reading(190, 120, 65800, 40), 46.3); ok || rej != nil {
		t.Errorf("within 30 min: rejected silently, got ok=%v rej=%v", ok, rej)
	}
	ok, rej = g.CheckDVOL(reading(201, 120, 65800, 40), 46.3)
	if ok || rej == nil || !strings.Contains(rej.Detail, "rejecting since 31m") {
		t.Errorf("a long episode is reported again with its age, got ok=%v %+v", ok, rej)
	}
	// Back in line with the marks: accepted, the episode ends.
	if ok, _ := g.CheckDVOL(reading(202, 44, 65800, 40), 46.3); !ok {
		t.Error("a reading close to the marks must be accepted")
	}
}

// Scenario: a real vol spike is still followed.
func TestDVOLGate_RealVolSpikeIsFollowed(t *testing.T) {
	// The marks rose with it.
	var g marketdata.DVOLGate
	g.CheckDVOL(reading(0, 40, 100000, 38), 0)
	if ok, rej := g.CheckDVOL(reading(60, 70, 92000, 64), 40); !ok {
		t.Errorf("DVOL 70 with marks at 64 must be accepted, got %+v", rej)
	}
	// No marks: spot fell 8 %, so the jump has a market behind it.
	var j marketdata.DVOLGate
	j.CheckDVOL(reading(0, 40, 100000, 0), 0)
	if ok, rej := j.CheckDVOL(reading(60, 70, 92000, 0), 40); !ok {
		t.Errorf("DVOL 40 → 70 with spot −8 %% must be accepted, got %+v", rej)
	}
}

func TestDVOLGate_EdgeCases(t *testing.T) {
	// A move within 25 % is not a jump.
	var g marketdata.DVOLGate
	g.CheckDVOL(reading(0, 40, 66800, 0), 0)
	if ok, _ := g.CheckDVOL(reading(1, 49.9, 66800, 0), 40); !ok {
		t.Error("+24.75 % must be accepted")
	}
	// A drop is a jump too.
	var d marketdata.DVOLGate
	d.CheckDVOL(reading(0, 127.3, 63900, 0), 0)
	if ok, _ := d.CheckDVOL(reading(1, 46.3, 63900, 0), 127.3); ok {
		t.Error("−64 % with spot flat must be rejected")
	}
	// Without marks a jump is held off at most 30 min, then the level is taken.
	var h marketdata.DVOLGate
	h.CheckDVOL(reading(0, 40, 66800, 0), 0)
	h.CheckDVOL(reading(1, 80, 66800, 0), 40)
	if ok, _ := h.CheckDVOL(reading(30, 80, 66800, 0), 40); ok {
		t.Error("29 min into the episode: still rejected")
	}
	if ok, _ := h.CheckDVOL(reading(31, 80, 66800, 0), 40); !ok {
		t.Error("30 min into the episode the new level must be accepted")
	}
	// Spot unknown counts as no move: the jump is rejected.
	var u marketdata.DVOLGate
	u.CheckDVOL(reading(0, 40, 0, 0), 0)
	if ok, _ := u.CheckDVOL(reading(1, 90, 0, 0), 40); ok {
		t.Error("a jump with spot unknown must be rejected")
	}
	// Marks decide over the jump rule: within ×/÷ 1.5 of them, accepted.
	var m marketdata.DVOLGate
	m.CheckDVOL(reading(0, 40, 66800, 0), 0)
	if ok, _ := m.CheckDVOL(reading(1, 58, 66800, 40), 40); !ok {
		t.Error("1.45× the marks must be accepted even as a jump")
	}
	// The first reading far from the marks is rejected; the startup value stays.
	var f marketdata.DVOLGate
	ok, rej := f.CheckDVOL(reading(0, 125, 66800, 40), 55)
	if ok || rej == nil || rej.Kept != 55 {
		t.Errorf("a first reading 3× the marks must be rejected, keeping 55: ok=%v %+v", ok, rej)
	}
}

func TestDVOLGate_ATMMarkIV(t *testing.T) {
	now := gateT0
	opt := func(dte int, strike float64, typ string, iv float64, age time.Duration) *marketdata.Instrument {
		i := &marketdata.Instrument{Strike: strike, OptionType: typ, Expiry: now.Add(time.Duration(dte)*24*time.Hour + time.Hour)}
		i.Greeks.IV = iv
		if age >= 0 {
			i.UpdatedAt = now.Add(-age)
		}
		return i
	}
	insts := []*marketdata.Instrument{
		opt(28, 66000, "call", 0.40, time.Second), opt(28, 66000, "put", 0.42, time.Second),
		opt(28, 70000, "call", 0.55, time.Second), // farther from spot
		opt(3, 66000, "call", 0.90, time.Second),  // under 7 DTE
		opt(60, 66000, "call", 0.60, time.Second), // farther from 30 DTE
		opt(31, 66000, "call", 0.99, -1),          // never quoted
		opt(32, 66000, "call", 0.99, time.Hour),   // stale ticker
	}
	if got := marketdata.ATMMarkIV(insts, 66500, now); got < 40.99 || got > 41.01 {
		t.Errorf("ATM IV = mean of the 28-DTE 66000 call and put = 41, got %v", got)
	}
	if got := marketdata.ATMMarkIV(insts, 0, now); got != 0 {
		t.Errorf("spot unknown: 0, got %v", got)
	}
	if got := marketdata.ATMMarkIV(insts[3:4], 66500, now); got != 0 {
		t.Errorf("only a 3-DTE option: 0, got %v", got)
	}
	if got := marketdata.ATMMarkIV(nil, 66500, now); got != 0 {
		t.Errorf("no instruments: 0, got %v", got)
	}
}

// The manager applies the gate to live pushes: the flip buffer (DVOL) and
// the IV percentile keep the last good value, and the rejection is queued once.
func TestMarketData_RejectsImplausibleDVOLPush(t *testing.T) {
	gw := newFakeFeedGateway()
	gw.results["public/get_instruments"] = instrumentsJSON(time.Now(), 24)
	m := startManager(t, gw)

	gw.push("deribit_price_index.btc_usd", `{"index_name":"btc_usd","price":66800}`)
	gw.push("deribit_volatility_index.btc_usd", `{"volatility":40.9,"index_name":"btc_usd"}`)
	eventually(t, time.Second, "first DVOL applied", func() bool { return m.DVOL() == 40.9 })
	pct := m.IVPercentile()

	gw.push("deribit_volatility_index.btc_usd", `{"volatility":110.7,"index_name":"btc_usd"}`)
	gw.push("deribit_volatility_index.btc_usd", `{"volatility":111.2,"index_name":"btc_usd"}`)
	gw.push("deribit_price_index.btc_usd", `{"index_name":"btc_usd","price":66801}`) // marker
	eventually(t, time.Second, "marker processed", func() bool { return m.UnderlyingPrice() == 66801 })

	if m.DVOL() != 40.9 || m.IVPercentile() != pct {
		t.Errorf("the rejected readings must not be used: DVOL %v, percentile %v (was %v)", m.DVOL(), m.IVPercentile(), pct)
	}
	rej := m.TakeDVOLRejections()
	if len(rej) != 1 || rej[0].Value != 110.7 || rej[0].Kept != 40.9 {
		t.Fatalf("one rejection with both values, got %+v", rej)
	}
	if again := m.TakeDVOLRejections(); len(again) != 0 {
		t.Errorf("the queue is drained, got %+v", again)
	}
}

// The strategy journals each queued rejection as a dvol_rejected risk line.
func TestStrategy_JournalsDVOLRejected(t *testing.T) {
	f := newStrategyFixture(t)
	f.market.rejectDVOL(marketdata.DVOLRejection{Value: 110.7, Kept: 40.9, Reason: "jump",
		Detail: "DVOL 110.7 rejected, 40.9 kept: +171 % in 8m0s with spot +0.45 %"})
	f.startRun()

	eventually(t, 2*time.Second, "dvol_rejected journaled", func() bool {
		return len(f.journal.riskChanges(orders.RiskDVOLRejected)) > 0
	})
	got := f.journal.riskChanges(orders.RiskDVOLRejected)
	if len(got) != 1 || !strings.Contains(got[0].Detail, "110.7") || !strings.Contains(got[0].Detail, "40.9") {
		t.Errorf("one dvol_rejected line with both values, got %+v", got)
	}
}
