package tests

import (
	"math"
	"testing"
	"time"

	"optionsbot/internal/gex"
)

// ── BSGamma ───────────────────────────────────────────────────────────────────

func TestBSGamma_ZeroOnDegenerateInputs(t *testing.T) {
	cases := [][4]float64{
		{0, 50000, 0.1, 0.75},   // zero spot
		{50000, 0, 0.1, 0.75},   // zero strike
		{50000, 50000, 0, 0.75}, // zero time
		{50000, 50000, 0.1, 0},  // zero vol
	}
	for _, c := range cases {
		got := gex.BSGamma(c[0], c[1], c[2], c[3])
		if got != 0 {
			t.Errorf("BSGamma(%v,%v,%v,%v) = %v, want 0", c[0], c[1], c[2], c[3], got)
		}
	}
}

func TestBSGamma_ATM(t *testing.T) {
	// ATM option: spot==strike, reasonable vol and time
	g := gex.BSGamma(50000, 50000, 0.25, 0.80)
	if g <= 0 {
		t.Errorf("ATM gamma should be positive, got %v", g)
	}
	// Higher spot → lower gamma (per unit) for same ATM
	g2 := gex.BSGamma(60000, 60000, 0.25, 0.80)
	if g2 >= g {
		t.Errorf("expected gamma to decrease with higher spot ATM: %v vs %v", g2, g)
	}
}

func TestBSGamma_OTMvsATM(t *testing.T) {
	// OTM call (strike > spot) should have lower gamma than ATM
	atmGamma := gex.BSGamma(50000, 50000, 0.25, 0.80)
	otmGamma := gex.BSGamma(50000, 60000, 0.25, 0.80)
	if otmGamma >= atmGamma {
		t.Errorf("OTM gamma %v should be < ATM gamma %v", otmGamma, atmGamma)
	}
}

// ── ExpiryWeight ──────────────────────────────────────────────────────────────

func TestExpiryWeight_Friday(t *testing.T) {
	// 2026-06-26 is a Friday (not month-end in June since June has 30 days)
	d := time.Date(2026, 6, 26, 8, 0, 0, 0, time.UTC)
	if d.Weekday() != time.Friday {
		t.Fatalf("expected Friday, got %v", d.Weekday())
	}
	w := gex.ExpiryWeight(d)
	if w != 1.0 {
		t.Errorf("Friday weight should be 1.0, got %v", w)
	}
}

func TestExpiryWeight_MonthEnd(t *testing.T) {
	// June 30 is always month-end
	d := time.Date(2026, 6, 30, 8, 0, 0, 0, time.UTC)
	w := gex.ExpiryWeight(d)
	if w != 1.0 {
		t.Errorf("month-end weight should be 1.0, got %v", w)
	}
}

func TestExpiryWeight_QuarterEnd(t *testing.T) {
	// Sep 30 is quarter-end
	d := time.Date(2026, 9, 30, 8, 0, 0, 0, time.UTC)
	w := gex.ExpiryWeight(d)
	if w != 1.0 {
		t.Errorf("quarter-end weight should be 1.0, got %v", w)
	}
}

func TestExpiryWeight_Monday(t *testing.T) {
	// Find a Monday (not month-end)
	d := time.Date(2026, 6, 22, 8, 0, 0, 0, time.UTC) // Monday
	if d.Weekday() != time.Monday {
		t.Fatalf("expected Monday, got %v", d.Weekday())
	}
	w := gex.ExpiryWeight(d)
	if w != 0.5 {
		t.Errorf("Monday weight should be 0.5, got %v", w)
	}
}

// ── ComputeExpiryGEX ──────────────────────────────────────────────────────────

func TestComputeExpiryGEX_SeparatesCallsAndPuts(t *testing.T) {
	expiry := time.Now().AddDate(0, 0, 30)
	insts := []gex.InstrumentGEXInput{
		{Strike: 50000, Expiry: expiry, OptionType: "call", Spot: 50000, OpenInterest: 100, MarkIV: 0.75},
		{Strike: 50000, Expiry: expiry, OptionType: "put", Spot: 50000, OpenInterest: 80, MarkIV: 0.75},
		{Strike: 55000, Expiry: expiry, OptionType: "call", Spot: 50000, OpenInterest: 50, MarkIV: 0.70},
	}
	strikes := gex.ComputeExpiryGEX(insts)
	if len(strikes) != 2 {
		t.Fatalf("expected 2 strikes, got %d", len(strikes))
	}
	// Strike 50000 should have both call and put OI
	s50 := strikes[0]
	if s50.Strike != 50000 {
		t.Fatalf("expected first strike 50000, got %v", s50.Strike)
	}
	if s50.CallOI != 100 || s50.PutOI != 80 {
		t.Errorf("expected call_oi=100 put_oi=80, got %v %v", s50.CallOI, s50.PutOI)
	}
	// Signed GEX for ATM should be small when calls≈puts (same gamma, different OI)
	// We just verify gex_call > 0 and gex_put > 0
	if s50.GEXCall <= 0 || s50.GEXPut <= 0 {
		t.Errorf("expected positive call/put GEX at ATM, got call=%v put=%v", s50.GEXCall, s50.GEXPut)
	}
}

func TestComputeExpiryGEX_SortedAscByStrike(t *testing.T) {
	expiry := time.Now().AddDate(0, 0, 30)
	insts := []gex.InstrumentGEXInput{
		{Strike: 60000, Expiry: expiry, OptionType: "call", Spot: 50000, OpenInterest: 10, MarkIV: 0.60},
		{Strike: 40000, Expiry: expiry, OptionType: "put", Spot: 50000, OpenInterest: 10, MarkIV: 0.80},
		{Strike: 50000, Expiry: expiry, OptionType: "call", Spot: 50000, OpenInterest: 100, MarkIV: 0.75},
	}
	strikes := gex.ComputeExpiryGEX(insts)
	for i := 1; i < len(strikes); i++ {
		if strikes[i].Strike <= strikes[i-1].Strike {
			t.Errorf("strikes not sorted: [%d]=%v [%d]=%v", i-1, strikes[i-1].Strike, i, strikes[i].Strike)
		}
	}
}

// ── FindGammaFlip ─────────────────────────────────────────────────────────────

func TestFindGammaFlip_CrossingZero(t *testing.T) {
	// GEX: negative below 50000, positive above
	strikes := []gex.WeightedStrike{
		{Strike: 45000, WeightedGEX: -1000},
		{Strike: 50000, WeightedGEX: -200},
		{Strike: 55000, WeightedGEX: 800},
		{Strike: 60000, WeightedGEX: 1200},
	}
	flip, found := gex.FindGammaFlip(strikes, 52000)
	if !found {
		t.Fatal("expected gamma flip to be found")
	}
	// Interpolation: between 50000 (GEX=-200) and 55000 (GEX=800)
	// ratio = 200/(200+800) = 0.2 → flip = 50000 + 5000*0.2 = 51000
	expected := 51000.0
	if math.Abs(flip-expected) > 1.0 {
		t.Errorf("gamma flip: expected ~%.0f, got %.2f", expected, flip)
	}
}

func TestFindGammaFlip_AllPositive_NoFlip(t *testing.T) {
	strikes := []gex.WeightedStrike{
		{Strike: 45000, WeightedGEX: 500},
		{Strike: 50000, WeightedGEX: 1000},
		{Strike: 55000, WeightedGEX: 800},
	}
	_, found := gex.FindGammaFlip(strikes, 50000)
	if found {
		t.Error("expected no gamma flip when all GEX is positive")
	}
}

func TestFindGammaFlip_ZeroAtStrike(t *testing.T) {
	// GEX is exactly zero at strike 50000 → flip IS 50000 (mirrors Python's curr_g==0 branch)
	strikes := []gex.WeightedStrike{
		{Strike: 45000, WeightedGEX: -500},
		{Strike: 50000, WeightedGEX: 0},
		{Strike: 55000, WeightedGEX: 800},
	}
	flip, found := gex.FindGammaFlip(strikes, 50000)
	if !found {
		t.Fatal("expected gamma flip found when GEX is exactly zero")
	}
	if flip != 50000.0 {
		t.Errorf("expected flip at 50000 (the zero-crossing strike), got %v", flip)
	}
}

// ── BuildSnapshot ─────────────────────────────────────────────────────────────

func TestBuildSnapshot_PositiveRegime(t *testing.T) {
	// All positive, no flip → falls back to score → POSITIVE/PINNING.
	strikes := []gex.WeightedStrike{
		{Strike: 48000, WeightedCallOI: 50, WeightedPutOI: 80, WeightedGEX: 500},
		{Strike: 52000, WeightedCallOI: 90, WeightedPutOI: 30, WeightedGEX: 1000},
	}
	snap := gex.BuildSnapshot(strikes, 50000)
	if snap.Regime != "POSITIVE/PINNING" {
		t.Errorf("expected POSITIVE/PINNING, got %q", snap.Regime)
	}
	if snap.RegimeScore != 1500 {
		t.Errorf("expected score 1500, got %v", snap.RegimeScore)
	}
	if snap.Spot != 50000 {
		t.Errorf("expected spot 50000, got %v", snap.Spot)
	}
}

func TestBuildSnapshot_NegativeRegime(t *testing.T) {
	// All negative, no flip → falls back to score → NEGATIVE/ACCELERATION.
	strikes := []gex.WeightedStrike{
		{Strike: 48000, WeightedCallOI: 30, WeightedPutOI: 80, WeightedGEX: -800},
		{Strike: 52000, WeightedCallOI: 40, WeightedPutOI: 60, WeightedGEX: -200},
	}
	snap := gex.BuildSnapshot(strikes, 50000)
	if snap.Regime != "NEGATIVE/ACCELERATION" {
		t.Errorf("expected NEGATIVE/ACCELERATION, got %q", snap.Regime)
	}
}

// TestBuildSnapshot_FlipOverridesNegativeScore is the critical production scenario:
// massive put OI makes the aggregate score deeply negative, but spot is above the
// gamma flip. Regime must be POSITIVE/PINNING regardless of the raw score.
// This was the exact bug that caused the repair logic to refuse reopening put legs
// even when the market structure was actually supportive.
func TestBuildSnapshot_FlipOverridesNegativeScore(t *testing.T) {
	// Huge put wall at 40000 drags total score negative.
	// GEX crosses zero between 55000 (neg) and 60000 (pos) → flip ≈ 56667.
	// Spot 65000 is above the flip.
	strikes := []gex.WeightedStrike{
		{Strike: 40000, WeightedGEX: -5000},
		{Strike: 55000, WeightedGEX: -1000},
		{Strike: 60000, WeightedGEX: 2000},
		{Strike: 70000, WeightedGEX: 500},
	}
	spot := 65000.0
	snap := gex.BuildSnapshot(strikes, spot)

	if !snap.GammaFlipFound {
		t.Fatal("expected gamma flip to be found")
	}
	if snap.Spot <= snap.GammaFlip {
		t.Fatalf("test setup error: spot %.0f must be above flip %.2f", snap.Spot, snap.GammaFlip)
	}
	if snap.RegimeScore >= 0 {
		t.Fatalf("test setup error: score must be negative to test the override, got %.0f", snap.RegimeScore)
	}
	if snap.Regime != "POSITIVE/PINNING" {
		t.Errorf("spot above flip with negative score: expected POSITIVE/PINNING, got %q", snap.Regime)
	}
}

// TestBuildSnapshot_FlipOverridesPositiveScore covers the inverse: a positive
// aggregate score but spot is below the gamma flip → NEGATIVE/ACCELERATION.
func TestBuildSnapshot_FlipOverridesPositiveScore(t *testing.T) {
	// GEX crosses zero between 50000 (neg) and 55000 (pos) → flip ≈ 52500.
	// Spot 45000 is below the flip.
	strikes := []gex.WeightedStrike{
		{Strike: 45000, WeightedGEX: -500},
		{Strike: 50000, WeightedGEX: -500},
		{Strike: 55000, WeightedGEX: 500},
		{Strike: 65000, WeightedGEX: 2000},
	}
	spot := 45000.0
	snap := gex.BuildSnapshot(strikes, spot)

	if !snap.GammaFlipFound {
		t.Fatal("expected gamma flip to be found")
	}
	if snap.Spot >= snap.GammaFlip {
		t.Fatalf("test setup error: spot %.0f must be below flip %.2f", snap.Spot, snap.GammaFlip)
	}
	if snap.Regime != "NEGATIVE/ACCELERATION" {
		t.Errorf("spot below flip: expected NEGATIVE/ACCELERATION, got %q", snap.Regime)
	}
}

// TestBuildSnapshot_NoFlipFallsBackToScore verifies that when no gamma flip is
// found (chain entirely one-sided) the regime is determined by score sign.
func TestBuildSnapshot_NoFlipFallsBackToScore(t *testing.T) {
	allPos := []gex.WeightedStrike{
		{Strike: 50000, WeightedGEX: 200},
		{Strike: 60000, WeightedGEX: 800},
	}
	snapPos := gex.BuildSnapshot(allPos, 55000)
	if snapPos.GammaFlipFound {
		t.Fatal("expected no flip for all-positive chain")
	}
	if snapPos.Regime != "POSITIVE/PINNING" {
		t.Errorf("all-positive no-flip: expected POSITIVE/PINNING, got %q", snapPos.Regime)
	}

	allNeg := []gex.WeightedStrike{
		{Strike: 50000, WeightedGEX: -200},
		{Strike: 60000, WeightedGEX: -800},
	}
	snapNeg := gex.BuildSnapshot(allNeg, 55000)
	if snapNeg.GammaFlipFound {
		t.Fatal("expected no flip for all-negative chain")
	}
	if snapNeg.Regime != "NEGATIVE/ACCELERATION" {
		t.Errorf("all-negative no-flip: expected NEGATIVE/ACCELERATION, got %q", snapNeg.Regime)
	}
}

// ── FilterByStrikeRange ───────────────────────────────────────────────────────

func makeInst(strike float64) gex.InstrumentGEXInput {
	return gex.InstrumentGEXInput{Strike: strike, Spot: 64000, OpenInterest: 100, MarkIV: 0.75,
		Expiry: time.Now().AddDate(0, 0, 30), OptionType: "call"}
}

// TestFilterByStrikeRange_ZeroRangePassthrough verifies that rangePct=0 disables
// the filter and returns all instruments unchanged (explicit "no filter" signal).
func TestFilterByStrikeRange_ZeroRangePassthrough(t *testing.T) {
	insts := []gex.InstrumentGEXInput{makeInst(40000), makeInst(64000), makeInst(90000)}
	got := gex.FilterByStrikeRange(insts, 64000, 0)
	if len(got) != 3 {
		t.Errorf("zero range: expected all 3 instruments, got %d", len(got))
	}
}

// TestFilterByStrikeRange_ZeroSpotPassthrough verifies that spot=0 disables the
// filter (no reference point to compute bounds from).
func TestFilterByStrikeRange_ZeroSpotPassthrough(t *testing.T) {
	insts := []gex.InstrumentGEXInput{makeInst(40000), makeInst(64000)}
	got := gex.FilterByStrikeRange(insts, 0, 0.25)
	if len(got) != 2 {
		t.Errorf("zero spot: expected passthrough, got %d instruments", len(got))
	}
}

// TestFilterByStrikeRange_KeepsInRange verifies that only strikes within ±25% of
// spot are retained. spot=64000, range=0.25 → [48000, 80000].
func TestFilterByStrikeRange_KeepsInRange(t *testing.T) {
	insts := []gex.InstrumentGEXInput{
		makeInst(40000), // 37.5% below → excluded
		makeInst(48000), // exactly at lower bound → included
		makeInst(56000), // 12.5% below → included
		makeInst(64000), // ATM → included
		makeInst(72000), // 12.5% above → included
		makeInst(80000), // exactly at upper bound → included
		makeInst(90000), // 40.6% above → excluded
	}
	got := gex.FilterByStrikeRange(insts, 64000, 0.25)
	if len(got) != 5 {
		t.Errorf("expected 5 in-range instruments, got %d", len(got))
	}
	for _, inst := range got {
		if inst.Strike < 48000 || inst.Strike > 80000 {
			t.Errorf("out-of-range strike survived filter: %.0f", inst.Strike)
		}
	}
}

// TestFilterByStrikeRange_ExactBoundaryInclusive verifies boundary instruments
// (strike == lower or upper bound) are included, not excluded.
func TestFilterByStrikeRange_ExactBoundaryInclusive(t *testing.T) {
	spot := 64000.0
	rangePct := 0.25
	lower := spot * (1 - rangePct) // 48000
	upper := spot * (1 + rangePct) // 80000
	insts := []gex.InstrumentGEXInput{makeInst(lower), makeInst(upper)}
	got := gex.FilterByStrikeRange(insts, spot, rangePct)
	if len(got) != 2 {
		t.Errorf("boundary strikes must be inclusive: expected 2, got %d", len(got))
	}
}

// TestFilterByStrikeRange_AllExcluded verifies that an empty slice is returned
// when every instrument is outside the range.
func TestFilterByStrikeRange_AllExcluded(t *testing.T) {
	insts := []gex.InstrumentGEXInput{makeInst(10000), makeInst(200000)}
	got := gex.FilterByStrikeRange(insts, 64000, 0.25)
	if len(got) != 0 {
		t.Errorf("expected empty result when all strikes OOB, got %d", len(got))
	}
}

// TestFilterByStrikeRange_EmptyInput verifies graceful handling of an empty slice.
func TestFilterByStrikeRange_EmptyInput(t *testing.T) {
	got := gex.FilterByStrikeRange(nil, 64000, 0.25)
	if len(got) != 0 {
		t.Errorf("nil input: expected empty result, got %d", len(got))
	}
}

// TestFilterByStrikeRange_NarrowRangeIsolatesATM verifies that a tight range
// (5%) keeps only near-the-money strikes and discards the rest.
func TestFilterByStrikeRange_NarrowRangeIsolatesATM(t *testing.T) {
	spot := 64000.0
	// Only 62000 and 65000 are within ±5% (60800–67200); 56000 and 73000 are not.
	insts := []gex.InstrumentGEXInput{
		makeInst(56000), // 12.5% below → excluded
		makeInst(62000), // 3.1% below → included
		makeInst(65000), // 1.6% above → included
		makeInst(73000), // 14.1% above → excluded
	}
	got := gex.FilterByStrikeRange(insts, spot, 0.05)
	if len(got) != 2 {
		t.Errorf("5%% range: expected 2 ATM instruments, got %d", len(got))
	}
}

// TestFilterByStrikeRange_GEXImpact verifies that filtering out deep-OTM puts
// with large OI changes the aggregate signed GEX in a directionally correct way.
// A deep-OTM put wall (below range) pulls GEX negative; excluding it raises the score.
func TestFilterByStrikeRange_GEXImpact(t *testing.T) {
	expiry := time.Now().AddDate(0, 0, 30)
	spot := 64000.0

	// Deep OTM put at 40000: huge OI, biases signed GEX negative
	deepPut := gex.InstrumentGEXInput{Strike: 40000, Expiry: expiry, OptionType: "put",
		Spot: spot, OpenInterest: 50000, MarkIV: 1.50}
	// Near-ATM call and put: moderate OI
	atmCall := gex.InstrumentGEXInput{Strike: 65000, Expiry: expiry, OptionType: "call",
		Spot: spot, OpenInterest: 500, MarkIV: 0.75}
	atmPut := gex.InstrumentGEXInput{Strike: 63000, Expiry: expiry, OptionType: "put",
		Spot: spot, OpenInterest: 500, MarkIV: 0.75}

	allInsts := []gex.InstrumentGEXInput{deepPut, atmCall, atmPut}

	strikesAll := gex.ComputeExpiryGEX(allInsts)
	scoreAll := 0.0
	for _, s := range strikesAll {
		scoreAll += s.GEXSigned
	}

	filtered := gex.FilterByStrikeRange(allInsts, spot, 0.25) // 48000–80000
	strikesFiltered := gex.ComputeExpiryGEX(filtered)
	scoreFiltered := 0.0
	for _, s := range strikesFiltered {
		scoreFiltered += s.GEXSigned
	}

	// Deep OTM put (40000) is below 48000 → excluded → score should be less negative
	if scoreFiltered <= scoreAll {
		t.Errorf("filtering deep OTM put should raise GEX score: unfiltered=%.2f filtered=%.2f",
			scoreAll, scoreFiltered)
	}
}

// ── ApplyRegimeHysteresis ─────────────────────────────────────────────────────

// TestApplyRegimeHysteresis_NoLastRegime verifies first call uses raw regime
// without any override (no previous state to carry forward).
func TestApplyRegimeHysteresis_NoLastRegime(t *testing.T) {
	got, overridden := gex.ApplyRegimeHysteresis("NEGATIVE/ACCELERATION", "", 63000, 64000, 0.01)
	if got != "NEGATIVE/ACCELERATION" {
		t.Errorf("first call: expected raw regime, got %q", got)
	}
	if overridden {
		t.Error("first call must not report an override")
	}
}

// TestApplyRegimeHysteresis_ZeroBand verifies that bandPct=0 always passes through
// the raw regime regardless of previous state (hysteresis disabled).
func TestApplyRegimeHysteresis_ZeroBand(t *testing.T) {
	got, overridden := gex.ApplyRegimeHysteresis("NEGATIVE/ACCELERATION", "POSITIVE/PINNING", 63000, 64000, 0)
	if got != "NEGATIVE/ACCELERATION" {
		t.Errorf("zero band: expected raw passthrough, got %q", got)
	}
	if overridden {
		t.Error("zero band must not override")
	}
}

// TestApplyRegimeHysteresis_NoFlip verifies that flip=0 (not found) disables hysteresis.
func TestApplyRegimeHysteresis_NoFlip(t *testing.T) {
	got, overridden := gex.ApplyRegimeHysteresis("NEGATIVE/ACCELERATION", "POSITIVE/PINNING", 63000, 0, 0.01)
	if got != "NEGATIVE/ACCELERATION" {
		t.Errorf("no flip: expected raw passthrough, got %q", got)
	}
	if overridden {
		t.Error("no flip must not override")
	}
}

// TestApplyRegimeHysteresis_SpotInBandStaysPositive verifies that when spot drops
// just below the flip but stays within the 1% band, POSITIVE regime is held.
// Scenario: flip=63500, band=1% → lower threshold=62865; spot=63200 stays above threshold.
func TestApplyRegimeHysteresis_SpotInBandStaysPositive(t *testing.T) {
	flip := 63500.0
	bandPct := 0.01
	// spot just below flip but above lower_band (63500*0.99 = 62865)
	spot := 63200.0 // raw regime = NEGATIVE, but in-band
	got, overridden := gex.ApplyRegimeHysteresis("NEGATIVE/ACCELERATION", "POSITIVE/PINNING", spot, flip, bandPct)
	if got != "POSITIVE/PINNING" {
		t.Errorf("in-band dip: expected POSITIVE/PINNING held, got %q", got)
	}
	if !overridden {
		t.Error("in-band dip: expected overridden=true")
	}
}

// TestApplyRegimeHysteresis_SpotInBandStaysNegative verifies that when spot rises
// just above the flip but stays within the 1% band, NEGATIVE regime is held.
// Scenario: flip=63500, band=1% → upper threshold=64135; spot=63800 stays below threshold.
func TestApplyRegimeHysteresis_SpotInBandStaysNegative(t *testing.T) {
	flip := 63500.0
	bandPct := 0.01
	// spot just above flip but below upper_band (63500*1.01 = 64135)
	spot := 63800.0 // raw regime = POSITIVE, but in-band
	got, overridden := gex.ApplyRegimeHysteresis("POSITIVE/PINNING", "NEGATIVE/ACCELERATION", spot, flip, bandPct)
	if got != "NEGATIVE/ACCELERATION" {
		t.Errorf("in-band bounce: expected NEGATIVE/ACCELERATION held, got %q", got)
	}
	if !overridden {
		t.Error("in-band bounce: expected overridden=true")
	}
}

// TestApplyRegimeHysteresis_ClearBreakBelowBand verifies that when spot drops
// clearly below the lower threshold the regime switches to NEGATIVE.
func TestApplyRegimeHysteresis_ClearBreakBelowBand(t *testing.T) {
	flip := 63500.0
	bandPct := 0.01
	spot := 62700.0 // clearly below 63500*0.99 = 62865
	got, overridden := gex.ApplyRegimeHysteresis("NEGATIVE/ACCELERATION", "POSITIVE/PINNING", spot, flip, bandPct)
	if got != "NEGATIVE/ACCELERATION" {
		t.Errorf("clear break below: expected NEGATIVE/ACCELERATION, got %q", got)
	}
	if overridden {
		t.Error("clear break below: overridden should be false (raw agrees with adjusted)")
	}
}

// TestApplyRegimeHysteresis_ClearBreakAboveBand verifies that when spot rises
// clearly above the upper threshold the regime switches to POSITIVE.
func TestApplyRegimeHysteresis_ClearBreakAboveBand(t *testing.T) {
	flip := 63500.0
	bandPct := 0.01
	spot := 64500.0 // clearly above 63500*1.01 = 64135
	got, overridden := gex.ApplyRegimeHysteresis("POSITIVE/PINNING", "NEGATIVE/ACCELERATION", spot, flip, bandPct)
	if got != "POSITIVE/PINNING" {
		t.Errorf("clear break above: expected POSITIVE/PINNING, got %q", got)
	}
	if overridden {
		t.Error("clear break above: overridden should be false (raw agrees with adjusted)")
	}
}

// TestApplyRegimeHysteresis_NoChangeWhenAlreadyAgrees verifies that when the raw
// regime matches the previous regime, overridden is false regardless of band.
func TestApplyRegimeHysteresis_NoChangeWhenAlreadyAgrees(t *testing.T) {
	got, overridden := gex.ApplyRegimeHysteresis("POSITIVE/PINNING", "POSITIVE/PINNING", 65000, 63500, 0.01)
	if got != "POSITIVE/PINNING" {
		t.Errorf("already agrees: expected POSITIVE/PINNING, got %q", got)
	}
	if overridden {
		t.Error("already agrees: overridden must be false when raw matches previous")
	}
}

// TestApplyRegimeHysteresis_OscillationSequence simulates the exact scenario from
// production logs: spot bounces between 63200 and 63800 around a flip at 63500.
// With a 1% band (lower=62865, upper=64135) the regime must stay NEGATIVE throughout.
func TestApplyRegimeHysteresis_OscillationSequence(t *testing.T) {
	flip := 63500.0
	bandPct := 0.01
	// Start: spot clearly below flip → NEGATIVE established on first call
	regime, _ := gex.ApplyRegimeHysteresis("NEGATIVE/ACCELERATION", "", 63200, flip, bandPct)
	if regime != "NEGATIVE/ACCELERATION" {
		t.Fatalf("initial: expected NEGATIVE/ACCELERATION, got %q", regime)
	}

	// Oscillation sequence: spot bounces above and below the flip but within the band
	spots := []float64{63200, 63650, 63400, 63750, 63300, 63600}
	for _, spot := range spots {
		rawRegime := "NEGATIVE/ACCELERATION"
		if spot > flip {
			rawRegime = "POSITIVE/PINNING"
		}
		regime, _ = gex.ApplyRegimeHysteresis(rawRegime, regime, spot, flip, bandPct)
		if regime != "NEGATIVE/ACCELERATION" {
			t.Errorf("spot=%.0f: expected NEGATIVE held, got %q (raw=%q)", spot, regime, rawRegime)
		}
	}

	// Genuine recovery: spot breaks clearly above upper_band (64135)
	rawRegime := "POSITIVE/PINNING"
	regime, overridden := gex.ApplyRegimeHysteresis(rawRegime, regime, 64500, flip, bandPct)
	if regime != "POSITIVE/PINNING" {
		t.Errorf("genuine recovery: expected POSITIVE/PINNING, got %q", regime)
	}
	if overridden {
		t.Error("genuine recovery: overridden must be false when raw and adjusted agree")
	}
}

// ── ConsolidateProfiles ───────────────────────────────────────────────────────

func TestConsolidateProfiles_WeightsApplied(t *testing.T) {
	expiry1 := time.Date(2026, 6, 26, 8, 0, 0, 0, time.UTC) // Friday weight=1.0
	expiry2 := time.Date(2026, 6, 22, 8, 0, 0, 0, time.UTC) // Monday weight=0.5

	profiles := []gex.ExpiryProfile{
		{
			Expiry:     expiry1,
			TotalOI:    100,
			TimeWeight: gex.ExpiryWeight(expiry1),
			Strikes: []gex.StrikeGEX{
				{Strike: 50000, CallOI: 100, PutOI: 0, GEXSigned: 1000},
			},
		},
		{
			Expiry:     expiry2,
			TotalOI:    100,
			TimeWeight: gex.ExpiryWeight(expiry2),
			Strikes: []gex.StrikeGEX{
				{Strike: 50000, CallOI: 0, PutOI: 100, GEXSigned: -500},
			},
		},
	}

	consolidated := gex.ConsolidateProfiles(profiles)
	if len(consolidated) != 1 {
		t.Fatalf("expected 1 consolidated strike, got %d", len(consolidated))
	}
	s := consolidated[0]
	if s.Strike != 50000 {
		t.Fatalf("expected strike 50000, got %v", s.Strike)
	}
	// Friday profile has base_weight=0.5, time_weight=1.0 → final=0.5
	// Monday profile has base_weight=0.5, time_weight=0.5 → final=0.25
	// weighted_gex = 1000*0.5 + (-500)*0.25 = 500 - 125 = 375
	expected := 1000.0*0.5 + (-500.0)*0.25
	if math.Abs(s.WeightedGEX-expected) > 0.01 {
		t.Errorf("expected weighted GEX ~%.2f, got %.4f", expected, s.WeightedGEX)
	}
}
