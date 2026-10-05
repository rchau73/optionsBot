package tests

import (
	"testing"
	"time"

	"optionsbot/internal/marketdata"
	"optionsbot/internal/orders"
	"optionsbot/internal/strategy"
)

// makeInstruments builds a minimal instrument slice with one entry per expiry.
// Expiries are placed exactly d*24h from now (not calendar-day midnight) so that
// SelectExpiry's DTE calculation (which also uses time.Now()) rounds to d reliably
// regardless of what time of day the test runs.
func makeInstruments(dteDays ...int) []*marketdata.Instrument {
	now := time.Now()
	var out []*marketdata.Instrument
	for _, d := range dteDays {
		// d whole days plus an hour: DTE counts whole days left (rounded down),
		// and an expiry exactly d days from a slightly earlier "now" would be
		// d−1 by the time the test reads it.
		expiry := now.Add(time.Duration(d)*24*time.Hour + time.Hour)
		out = append(out, &marketdata.Instrument{
			Name:   "BTC-TEST",
			Expiry: expiry,
		})
	}
	return out
}

// ── SelectExpiry rollout guard tests ─────────────────────────────────────────

func TestSelectExpiry_RolloutDTE_FiltersNearExpiry(t *testing.T) {
	// Slot targetDTE=15, dev=10, rolloutDTE=9 → valid range is [10, 25].
	// A 6 DTE expiry would fit the raw deviation window [5,25] but must be
	// rejected because opening it would trigger an immediate rollout.
	insts := makeInstruments(6, 12, 20)
	expiry, ok := strategy.SelectExpiry(insts, time.Now(), 15, 10, 9)
	if !ok {
		t.Fatal("expected an expiry to be found (DTE 12 is in [10,25])")
	}
	dte := int(time.Until(expiry).Hours() / 24)
	if dte <= 9 {
		t.Errorf("selected expiry has DTE=%d which is ≤ rollout_dte=9 — must be filtered", dte)
	}
}

func TestSelectExpiry_RolloutDTE_AllExpiriesBelowFloor_NoResult(t *testing.T) {
	// Only a 6 DTE expiry available — below rolloutDTE=9. Should return false.
	insts := makeInstruments(6)
	_, ok := strategy.SelectExpiry(insts, time.Now(), 15, 10, 9)
	if ok {
		t.Error("expected no valid expiry when only DTE=6 is available and rolloutDTE=9")
	}
}

func TestSelectExpiry_RolloutDTE_ExactlyAtFloor_Rejected(t *testing.T) {
	// DTE == rolloutDTE (9) must be rejected — would roll out immediately.
	insts := makeInstruments(9, 14)
	expiry, ok := strategy.SelectExpiry(insts, time.Now(), 15, 10, 9)
	if !ok {
		t.Fatal("expected DTE=14 to be found")
	}
	dte := int(time.Until(expiry).Hours() / 24)
	if dte == 9 {
		t.Error("selected DTE=9 which equals rolloutDTE — must be filtered")
	}
}

func TestSelectExpiry_RolloutDTE_JustAboveFloor_Accepted(t *testing.T) {
	// DTE = rolloutDTE+1 = 10 must be accepted (first valid DTE).
	insts := makeInstruments(10, 20)
	exp10 := insts[0].Expiry
	expiry, ok := strategy.SelectExpiry(insts, time.Now(), 15, 10, 9)
	if !ok {
		t.Fatal("expected DTE=10 to be found")
	}
	if !expiry.Equal(exp10) {
		t.Errorf("expected nearest valid expiry (DTE≈10), got %v", expiry)
	}
}

func TestSelectExpiry_RolloutDTE_Zero_NoFilter(t *testing.T) {
	// rolloutDTE=0 means no floor — original deviation window applies.
	insts := makeInstruments(3, 14)
	_, ok := strategy.SelectExpiry(insts, time.Now(), 15, 10, 0)
	if !ok {
		t.Fatal("expected DTE=3 or DTE=14 to be found with no rollout floor")
	}
}

func TestSelectExpiry_RolloutDTE_PreferNearestAboveFloor(t *testing.T) {
	// With rolloutDTE=9, multiple expiries above the floor — must pick nearest valid.
	// DTE=6 filtered (below floor), DTE=11/18/22 valid → nearest is DTE=11.
	insts := makeInstruments(6, 11, 18, 22)
	exp11 := insts[1].Expiry // index 1 = DTE 11
	expiry, ok := strategy.SelectExpiry(insts, time.Now(), 15, 10, 9)
	if !ok {
		t.Fatal("expected an expiry to be found")
	}
	if !expiry.Equal(exp11) {
		t.Errorf("expected nearest valid expiry (DTE≈11), got %v", expiry)
	}
}

// ── SelectSlotExpiry rollout guard tests ──────────────────────────────────

func TestSelectSlotExpiry_RolloutDTE_Respected(t *testing.T) {
	// DTE=8 is below rollout floor, DTE=12 is the fallback.
	occupied := map[time.Time]bool{}
	insts := makeInstruments(8, 12, 20)
	expiry, pick := strategy.SelectSlotExpiry(insts, time.Now(), 15, 10, 9, 1, occupied)
	if pick != marketdata.PickWindow {
		t.Fatal("expected DTE=12")
	}
	dte := int(time.Until(expiry).Hours() / 24)
	if dte <= 9 {
		t.Errorf("fallback selected DTE=%d which is ≤ rolloutDTE=9", dte)
	}
}

func TestSelectSlotExpiry_HeldAndBelowFloor_Skipped(t *testing.T) {
	// DTE=12 is occupied, DTE=7 is below rollout floor — only DTE=18 is valid.
	now := time.Now().UTC().Truncate(24 * time.Hour)
	exp12 := now.AddDate(0, 0, 12)
	exp18 := now.AddDate(0, 0, 18)
	insts := []*marketdata.Instrument{
		{Name: "A", Expiry: now.AddDate(0, 0, 7)},
		{Name: "B", Expiry: exp12},
		{Name: "C", Expiry: exp18},
	}
	occupied := map[time.Time]bool{exp12: true}
	expiry, pick := strategy.SelectSlotExpiry(insts, time.Now(), 15, 10, 9, 1, occupied)
	if pick != marketdata.PickStretched {
		t.Fatal("expected DTE=18 as the only free expiry")
	}
	if !expiry.Equal(exp18) {
		t.Errorf("expected expiry at DTE≈18, got %v", expiry)
	}
}

// SelectExpiry counts days from the given now, not the wall clock, so the
// backtest can replay past dates.
func TestSelectExpiry_UsesGivenNow(t *testing.T) {
	past := time.Date(2023, 1, 1, 8, 0, 0, 0, time.UTC)
	insts := []*marketdata.Instrument{{Name: "BTC-X", Expiry: past.AddDate(0, 0, 30)}}

	expiry, ok := strategy.SelectExpiry(insts, past, 30, 0, 10)
	if !ok || !expiry.Equal(past.AddDate(0, 0, 30)) {
		t.Errorf("expected the 30-DTE expiry relative to %s, got %v ok=%v", past.Format("2006-01-02"), expiry, ok)
	}
	if marketdata.DaysToExpiry(expiry, past) != 30 {
		t.Error("DaysToExpiry should count from the given now")
	}
}

func TestSelectStrike(t *testing.T) {
	exp := time.Date(2026, 3, 27, 8, 0, 0, 0, time.UTC)
	opt := func(name, typ string, delta, mid float64) *marketdata.Instrument {
		return &marketdata.Instrument{Name: name, Expiry: exp, OptionType: typ, Mid: mid, Greeks: marketdata.Greeks{Delta: delta}}
	}
	chain := []*marketdata.Instrument{
		opt("C-ITM", "call", 0.70, 0.1),
		opt("C-20", "call", 0.20, 0.03),
		opt("C-15", "call", 0.15, 0.02),
		opt("C-NODATA", "call", 0.16, 0),
		opt("P-17", "put", -0.17, 0.02),
	}

	got, err := strategy.SelectStrike(chain, exp, "call", 0.16, 0.05)
	if err != nil || got.Name != "C-15" {
		t.Errorf("closest OTM call with data should win, got %v (%v)", got, err)
	}
	if got, err := strategy.SelectStrike(chain, exp, "put", 0.16, 0); err != nil || got.Name != "P-17" {
		t.Errorf("put by |delta|: got %v (%v)", got, err)
	}
	if _, err := strategy.SelectStrike(chain, exp, "call", 0.40, 0.05); err == nil {
		t.Error("no strike within delta slippage must be an error")
	}
	if _, err := strategy.SelectStrike(chain, exp.AddDate(0, 1, 0), "call", 0.16, 0); err == nil {
		t.Error("unknown expiry must be an error")
	}
	if _, err := strategy.SelectStrike(chain[:1], exp, "call", 0.16, 0); err == nil {
		t.Error("only ITM candidates must be an error")
	}
}

func TestAvailableAndNextMonthlyExpiry(t *testing.T) {
	ref := time.Date(2026, 1, 10, 0, 0, 0, 0, time.UTC)
	feb := time.Date(2026, 2, 27, 8, 0, 0, 0, time.UTC) // last Friday of February
	mar := time.Date(2026, 3, 27, 8, 0, 0, 0, time.UTC)
	insts := []*marketdata.Instrument{{Expiry: mar}, {Expiry: feb}, {Expiry: feb}}

	exps := strategy.AvailableExpiries(insts)
	if len(exps) != 2 || !exps[0].Equal(feb) {
		t.Errorf("expiries should be unique and sorted, got %v", exps)
	}
	if got, ok := strategy.NextMonthlyExpiry(ref, exps); !ok || !got.Equal(feb) {
		t.Errorf("next monthly after %s = %v, want %v", ref.Format("2006-01-02"), got, feb)
	}
	if _, ok := strategy.NextMonthlyExpiry(ref, nil); ok {
		t.Error("no expiries → not found")
	}
}

// Regression (found in a ±40 % stress simulation): entry counted DTE rounded,
// the exit rules rounded down, so an expiry 15.5 days out passed entry ("16",
// above rollout_dte 15) and was rolled on the next cycle ("15"), over and over.
func TestSelectExpiry_NeverPicksWhatTheExitRuleWouldRollAtOnce(t *testing.T) {
	now := time.Date(2026, 10, 8, 4, 0, 0, 0, time.UTC)
	expiry := time.Date(2026, 10, 23, 16, 0, 0, 0, time.UTC) // 15.5 days out
	insts := []*marketdata.Instrument{{Name: "BTC-X", Expiry: expiry}}
	if _, ok := strategy.SelectExpiry(insts, now, 25, 10, 15); ok {
		t.Error("15.5 days left is DTE 15: inside the rollout window, must not be entered")
	}
	pos := &orders.Position{Expiry: expiry}
	if dec := strategy.EvaluateLeg(pos, now, 15, 0.10, 0.5, 2, 0); dec.Action != strategy.ActionRollNextMonth {
		t.Errorf("and the exit rule agrees it is inside the window: %v", dec.Action)
	}
	// From 16 days left it is a valid entry, and not rolled.
	later := expiry.Add(12 * time.Hour) // 16.0 days out
	insts[0].Expiry = later
	if _, ok := strategy.SelectExpiry(insts, now, 25, 10, 15); !ok {
		t.Error("16 days left must be a valid entry")
	}
	pos.Expiry, pos.MarkLive = later, true
	if dec := strategy.EvaluateLeg(pos, now, 15, 0.10, 0.5, 2, 0); dec.Action == strategy.ActionRollNextMonth {
		t.Error("a position the entry rule accepts must not be rolled at once")
	}
}

func TestDaysToExpiry_WholeDaysLeftRoundedDown(t *testing.T) {
	now := time.Date(2026, 10, 8, 0, 0, 0, 0, time.UTC)
	for hours, want := range map[float64]int{-5: 0, 0: 0, 23.9: 0, 24: 1, 371.9: 15, 372: 15, 384: 16} {
		exp := now.Add(time.Duration(hours * float64(time.Hour)))
		if got := marketdata.DaysToExpiry(exp, now); got != want {
			t.Errorf("%v hours → DTE %d, want %d", hours, got, want)
		}
		if got := (&orders.Position{Expiry: exp}).DTEAt(now); got != want {
			t.Errorf("Position.DTEAt must use the same rule: %v hours → %d, want %d", hours, got, want)
		}
	}
}
