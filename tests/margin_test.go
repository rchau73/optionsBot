package tests

import (
	"testing"

	"optionsbot/internal/strategy"
)

// ── Sizing helpers (inputs are Deribit's simulated margins) ─────────────────

func TestEntryLots(t *testing.T) {
	cases := []struct {
		name             string
		headroom, perLot float64
		want             int
	}{
		{"several lots fit", 0.12, 0.03, 4},
		{"partial lot floored", 0.11, 0.03, 3},
		{"exact fit despite float error", 0.3, 0.1, 3},
		{"less than one lot", 0.02, 0.03, 0},
		{"netting: one lot, never unbounded", 5, -0.01, 1},
		{"no margin added: one lot", 5, 0, 1},
	}
	for _, c := range cases {
		if got := strategy.EntryLots(c.headroom, c.perLot); got != c.want {
			t.Errorf("%s: EntryLots(%v, %v) = %d, want %d", c.name, c.headroom, c.perLot, got, c.want)
		}
	}
}

func TestTargetLots(t *testing.T) {
	if got := strategy.TargetLots(0.5, 0.1); got != 5 {
		t.Errorf("TargetLots = %d, want 5", got)
	}
	if got := strategy.TargetLots(0.01, 0.1); got != 1 {
		t.Errorf("the IM limit keeps at least one lot, got %d", got)
	}
	if got := strategy.TargetLots(0.5, 0); got != 0 {
		t.Errorf("unknown IM per lot → no target, got %d", got)
	}
}

func TestMMKeepQty(t *testing.T) {
	cases := []struct {
		name                         string
		qty, lot, mmPct, maxMM, want float64
	}{
		{"under the limit: keep all", 1.0, 0.1, 30, 35, 1.0},
		{"proportional with slack", 1.0, 0.1, 50, 35, 0.6}, // 1 × 35/50 × 0.95 = 0.665 → 0.6
		{"always at least one lot", 1.0, 0.1, 36, 35, 0.9},
		{"one lot can be closed", 0.1, 0.1, 40, 35, 0},
		{"deep breach closes all", 1.0, 0.1, 1000, 35, 0},
	}
	for _, c := range cases {
		if got := strategy.MMKeepQty(c.qty, c.lot, c.mmPct, c.maxMM); !near(got, c.want, 1e-9) {
			t.Errorf("%s: MMKeepQty = %v, want %v", c.name, got, c.want)
		}
	}
}

func TestSlotAndEntryShare(t *testing.T) {
	// The BTC book on 2026-10-03: limit 20 % of 1.8022 BTC, 3 slots, IM 8.1 %.
	slot := strategy.SlotShare(20, 1.8022, 3)
	if !near(slot, 0.120147, 1e-6) {
		t.Fatalf("slot share = %v, want 20%% × 1.8022 ÷ 3", slot)
	}
	headroom := 0.20*1.8022 - 0.1466 // ≈ 0.214 BTC
	if got := strategy.EntryShare(headroom, slot, 1); got != slot {
		t.Errorf("one vacant slot gets its slot share, not all the headroom: %v", got)
	}
	if got := strategy.EntryShare(0.05, slot, 2); !near(got, 0.025, 1e-12) {
		t.Errorf("little headroom is split between the vacant slots: %v", got)
	}
	if strategy.EntryShare(0, slot, 1) != 0 || strategy.EntryShare(1, slot, 0) != 0 || strategy.SlotShare(20, 1, 0) != 0 {
		t.Error("no headroom, no vacancy or no slots → nothing")
	}
}

func TestCapLots(t *testing.T) {
	cases := []struct {
		name         string
		lots, normal int
		multiple     float64
		want         int
		bound        bool
	}{
		{"normal day: well inside", 12, 12, 2, 12, false},
		{"hedge day: capped at 2x", 166, 12, 2, 24, true},
		{"exactly at the cap", 24, 12, 2, 24, false},
		{"unknown normal size: no cap", 166, 0, 2, 166, false},
		{"cap disabled", 166, 12, 0, 166, false},
		{"at least one lot", 5, 0, 2, 5, false},
		{"tiny normal still allows one lot", 3, 1, 0.5, 1, true},
	}
	for _, c := range cases {
		got, bound := strategy.CapLots(c.lots, c.normal, c.multiple)
		if got != c.want || bound != c.bound {
			t.Errorf("%s: CapLots(%d, %d, %v) = %d, %v; want %d, %v", c.name, c.lots, c.normal, c.multiple, got, bound, c.want, c.bound)
		}
	}
}
