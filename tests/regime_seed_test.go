package tests

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"optionsbot/internal/gex"
	"optionsbot/internal/history"
	"optionsbot/internal/orders"
)

// #147: a brand-new bot has no regime closes to confirm against, so it takes
// the live regime as its last closes instead of freezing for days.

func TestRegimeStore_SeedOnlyAnEmptyHistory(t *testing.T) {
	path := filepath.Join(t.TempDir(), "regime_history.jsonl")
	s, err := history.OpenRegimes(path)
	if err != nil {
		t.Fatal(err)
	}
	now := time.Date(2026, 10, 18, 0, 17, 0, 0, time.UTC)
	if !s.Seed(now, "NEGATIVE/ACCELERATION", 2) {
		t.Fatal("an empty history must be seeded")
	}
	if s.Seed(now, "POSITIVE/PINNING", 2) {
		t.Error("a history with days must never be seeded again")
	}
	days := s.Daily()
	if len(days) != 2 || !days[0].Day.Equal(now.Truncate(24*time.Hour).AddDate(0, 0, -2)) ||
		!days[1].Day.Equal(now.Truncate(24*time.Hour).AddDate(0, 0, -1)) {
		t.Fatalf("seeded days = %+v, want the 2 days before today", days)
	}
	for _, d := range days {
		if d.Regime != "NEGATIVE/ACCELERATION" || !d.Seeded {
			t.Errorf("seeded day = %+v", d)
		}
	}
	s.Close()

	data, _ := os.ReadFile(path)
	if strings.Count(string(data), `"seeded":true`) != 2 {
		t.Errorf("the file must mark both seeded closes:\n%s", data)
	}
	again, err := history.OpenRegimes(path) // a restart reads them back
	if err != nil {
		t.Fatal(err)
	}
	if got := again.Daily(); len(got) != 2 || !got[1].Seeded {
		t.Errorf("reloaded = %+v", got)
	}
	if again.Seed(now.AddDate(0, 0, 1), "POSITIVE/PINNING", 2) {
		t.Error("a restart must not seed")
	}
}

func TestRegimeStore_SeedNeedsDays(t *testing.T) {
	s, _ := history.OpenRegimes("")
	if s.Seed(time.Now(), "NEGATIVE/ACCELERATION", 0) || len(s.Daily()) != 0 {
		t.Error("zero confirm days: nothing to seed")
	}
}

// A brand-new bot under a negative regime and a bear trend trades at once,
// under the confirmed-negative rules: calls only, no freeze.
func TestStrategy_BrandNewBotStartsFromTheLiveRegime(t *testing.T) {
	f := newStrategyFixture(t)
	f.withPutSheddingRegime() // bear trend from the daily closes
	f.gex = fixedGEX{&gex.Snapshot{Regime: "NEGATIVE/ACCELERATION", Spot: 100000, GammaFlip: 90000, GammaFlipFound: true}}
	// no withConfirmedRegime: the regime history is empty
	f.startRun()

	eventually(t, 2*time.Second, "call submitted", func() bool { return len(f.exch.sells()) >= 1 })
	time.Sleep(50 * time.Millisecond)
	if n := f.skips("risk_frozen"); n != 0 {
		t.Errorf("a brand-new bot must not freeze on the live regime, got %d risk_frozen skips", n)
	}
	for _, o := range f.exch.sells() {
		if o.Instrument != f.call {
			t.Errorf("the seeded regime is a confirmed negative one: no puts in a bear trend, got %s", o.Instrument)
		}
	}
	seeded := f.journal.riskChanges(orders.RiskRegimeSeeded)
	if len(seeded) != 1 || seeded[0].Regime != "NEGATIVE/ACCELERATION" || !strings.Contains(seeded[0].Detail, "brand-new start") {
		t.Errorf("the seed must be journaled once, got %+v", seeded)
	}
}

// A restart with history keeps the confirmation: a negative live regime after
// positive closes still freezes, and nothing is seeded.
func TestStrategy_RestartWithHistoryIsNotSeeded(t *testing.T) {
	f := newStrategyFixture(t)
	f.withPutSheddingRegime()
	f.withConfirmedRegime("POSITIVE/PINNING")
	f.startRun()

	eventually(t, 2*time.Second, "freeze journaled", func() bool { return f.skips("risk_frozen") > 0 })
	if n := len(f.journal.riskChanges(orders.RiskRegimeSeeded)); n != 0 {
		t.Errorf("a history with closes must not be seeded, got %d records", n)
	}
}
