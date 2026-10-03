package tests

import (
	"encoding/json"
	"math"
	"os"
	"testing"
	"time"

	"optionsbot/internal/gex"
)

// scriptFixture is a real Deribit mainnet BTC book summary captured with
// public endpoints, plus what GestaoCarteira's deribit_tc_export_v3.py
// computed from exactly this data with its clock frozen at the capture time
// (its own functions: load_option_chain → filter_strikes_around_spot(0.15) →
// compute_gex_proxy → consolidate_levels → find_gamma_flip).
type scriptFixture struct {
	CapturedAtMS int64            `json:"captured_at_ms"`
	Summaries    []gex.SummaryRow `json:"summaries"`
	Expected     struct {
		Expiries  []string `json:"expiries"`
		Spot      float64  `json:"spot"`
		Score     float64  `json:"score"`
		GammaFlip float64  `json:"gamma_flip"`
		Regime    string   `json:"regime"`
		Strikes   int      `json:"strikes"`
	} `json:"expected_python"`
}

func loadGEXFixture(t *testing.T) scriptFixture {
	t.Helper()
	data, err := os.ReadFile("testdata/gex_mainnet_btc.json")
	if err != nil {
		t.Fatal(err)
	}
	var fx scriptFixture
	if err := json.Unmarshal(data, &fx); err != nil {
		t.Fatal(err)
	}
	return fx
}

func TestGEXBuild_MatchesGestaoCarteiraScript(t *testing.T) {
	fx := loadGEXFixture(t)
	now := time.UnixMilli(fx.CapturedAtMS)
	snap, st, err := gex.Build(fx.Summaries, now, gex.Params{Underlying: "BTC", NExpiries: 5, StrikeRangePct: 0.15, Method: gex.MethodScript})
	if err != nil {
		t.Fatal(err)
	}
	want := fx.Expected
	var got []string
	for _, e := range st.Expiries {
		got = append(got, e.Format("2006-01-02"))
	}
	if len(got) != len(want.Expiries) || got[0] != want.Expiries[0] || got[len(got)-1] != want.Expiries[len(want.Expiries)-1] {
		t.Errorf("expiries = %v, script used %v", got, want.Expiries)
	}
	if snap.Regime != want.Regime {
		t.Errorf("regime = %s, script says %s", snap.Regime, want.Regime)
	}
	if !snap.GammaFlipFound || math.Abs(snap.GammaFlip-want.GammaFlip) > 0.01 {
		t.Errorf("gamma flip = %.4f, script says %.4f", snap.GammaFlip, want.GammaFlip)
	}
	if math.Abs(snap.RegimeScore/want.Score-1) > 1e-9 {
		t.Errorf("score = %.6g, script says %.6g", snap.RegimeScore, want.Score)
	}
	if len(snap.Strikes) != want.Strikes || snap.Spot != want.Spot {
		t.Errorf("strikes %d spot %v, script %d / %v", len(snap.Strikes), snap.Spot, want.Strikes, want.Spot)
	}
}
