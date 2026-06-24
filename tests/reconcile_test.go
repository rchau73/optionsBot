package tests

import (
	"math"
	"testing"
	"time"

	"optionsbot/internal/config"
	"optionsbot/internal/orders"
	"optionsbot/internal/strategy"
)

func makeReconcilePos(optType string, expiry time.Time, delta float64) *orders.Position {
	return &orders.Position{
		Instrument:    "BTC-TEST-" + optType,
		OptionType:    optType,
		Expiry:        expiry,
		CurrentGreeks: orders.Greeks{Delta: delta},
	}
}

func reconcileSlots() []config.StrangleSlot {
	return []config.StrangleSlot{
		{TargetDTE: 15, EntryDelta: 0.16},
		{TargetDTE: 30, EntryDelta: 0.16},
		{TargetDTE: 45, EntryDelta: 0.16},
	}
}

// Both legs present — slot matched by DTE proximity.
func TestMatchSlot_BothLegs_PicksNearestDTE(t *testing.T) {
	now := time.Now()
	expiry := now.Add(14 * 24 * time.Hour) // DTE ≈ 14 → nearest slot is DTE=15
	call := makeReconcilePos("call", expiry, 0.16)
	put := makeReconcilePos("put", expiry, -0.16)

	slot := strategy.MatchSlotToPosition(call, put, expiry, now, reconcileSlots())
	if slot.TargetDTE != 15 {
		t.Errorf("expected slot DTE=15, got %d", slot.TargetDTE)
	}
}

// Put-only: slot matched using put delta (abs value).
func TestMatchSlot_PutOnly_PicksSlotByPutDelta(t *testing.T) {
	now := time.Now()
	expiry := now.Add(29 * 24 * time.Hour) // DTE ≈ 29 → nearest slot is DTE=30
	put := makeReconcilePos("put", expiry, -0.16)

	slot := strategy.MatchSlotToPosition(nil, put, expiry, now, reconcileSlots())
	if slot.TargetDTE != 30 {
		t.Errorf("expected slot DTE=30 for put-only, got %d", slot.TargetDTE)
	}
	if math.Abs(slot.EntryDelta-0.16) > 1e-9 {
		t.Errorf("expected delta 0.16, got %v", slot.EntryDelta)
	}
}

// Call-only: slot matched using call delta.
func TestMatchSlot_CallOnly_PicksSlotByCallDelta(t *testing.T) {
	now := time.Now()
	expiry := now.Add(44 * 24 * time.Hour) // DTE ≈ 44 → nearest slot is DTE=45
	call := makeReconcilePos("call", expiry, 0.16)

	slot := strategy.MatchSlotToPosition(call, nil, expiry, now, reconcileSlots())
	if slot.TargetDTE != 45 {
		t.Errorf("expected slot DTE=45 for call-only, got %d", slot.TargetDTE)
	}
}

// Delta tie-break: two slots at the same DTE distance — delta wins.
func TestMatchSlot_DeltaTieBreak(t *testing.T) {
	now := time.Now()
	// DTE=22 is equidistant between slot DTE=15 (dist=7) and slot DTE=30 (dist=8).
	// With delta=0.10 the DTE=15/delta=0.10 slot should win over DTE=30/delta=0.16.
	expiry := now.Add(22 * 24 * time.Hour)
	slots := []config.StrangleSlot{
		{TargetDTE: 15, EntryDelta: 0.10},
		{TargetDTE: 30, EntryDelta: 0.16},
	}
	call := makeReconcilePos("call", expiry, 0.10)

	slot := strategy.MatchSlotToPosition(call, nil, expiry, now, slots)
	if slot.TargetDTE != 15 || math.Abs(slot.EntryDelta-0.10) > 1e-9 {
		t.Errorf("expected slot {DTE=15, delta=0.10}, got {DTE=%d, delta=%v}", slot.TargetDTE, slot.EntryDelta)
	}
}
