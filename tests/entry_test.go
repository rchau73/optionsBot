package tests

import (
	"testing"
	"time"

	"optionsbot/internal/marketdata"
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
		expiry := now.Add(time.Duration(d) * 24 * time.Hour)
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

// ── SelectExpiryFallback rollout guard tests ──────────────────────────────────

func TestSelectExpiryFallback_RolloutDTE_Respected(t *testing.T) {
	// DTE=8 is below rollout floor, DTE=12 is the fallback.
	occupied := map[time.Time]bool{}
	insts := makeInstruments(8, 12, 20)
	expiry, ok := strategy.SelectExpiryFallback(insts, time.Now(), 15, 10, 9, occupied)
	if !ok {
		t.Fatal("expected DTE=12 as fallback")
	}
	dte := int(time.Until(expiry).Hours() / 24)
	if dte <= 9 {
		t.Errorf("fallback selected DTE=%d which is ≤ rolloutDTE=9", dte)
	}
}

func TestSelectExpiryFallback_OccupiedAndBelowFloor_Skipped(t *testing.T) {
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
	expiry, ok := strategy.SelectExpiryFallback(insts, time.Now(), 15, 10, 9, occupied)
	if !ok {
		t.Fatal("expected DTE=18 as the only valid fallback")
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
	if strategy.DaysToExpiry(expiry, past) != 30 {
		t.Error("DaysToExpiry should count from the given now")
	}
}
