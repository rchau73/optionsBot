package tests

import (
	"fmt"
	"math"
	"testing"
	"time"

	"optionsbot/internal/marketdata"
	"optionsbot/internal/strategy"
)

// A coarse ETH-like grid (strikes 100 apart): the nearest strike to 0.16 on
// each side alone is call 0.145 / put 0.18 — +0.035 of delta per contract.
func ethGrid(exp time.Time) []*marketdata.Instrument {
	inst := func(typ string, strike, delta float64) *marketdata.Instrument {
		return &marketdata.Instrument{Name: fmt.Sprintf("ETH-%.0f-%s", strike, typ), Expiry: exp, OptionType: typ,
			Strike: strike, Mid: 0.01, Greeks: marketdata.Greeks{Delta: delta}}
	}
	return []*marketdata.Instrument{
		inst("call", 3000, 0.20), inst("call", 3100, 0.145), inst("call", 3200, 0.10),
		inst("put", 2500, -0.18), inst("put", 2400, -0.13), inst("put", 2300, -0.09),
	}
}

func TestSelectStrikePair_DeltasCancel(t *testing.T) {
	exp := time.Now().AddDate(0, 0, 25)
	call, put, err := strategy.SelectStrikePair(ethGrid(exp), exp, 0.16, 0.03)
	if err != nil {
		t.Fatal(err)
	}
	if call.Strike != 3100 || put.Strike != 2400 {
		t.Errorf("pair = %v C / %v P, want 3100 C (0.145) / 2400 P (0.13): net 0.015 instead of 0.035", call.Strike, put.Strike)
	}
	alone, _ := strategy.SelectStrike(ethGrid(exp), exp, "put", 0.16, 0.03)
	if alone.Strike != 2500 {
		t.Fatalf("setup: alone the nearest put is 2500 (0.18), got %v", alone.Strike)
	}
}

func TestSelectStrikePair_StaysInsideTheBand(t *testing.T) {
	exp := time.Now().AddDate(0, 0, 25)
	call, put, err := strategy.SelectStrikePair(ethGrid(exp), exp, 0.16, 0.03)
	if err != nil {
		t.Fatal(err)
	}
	for _, in := range []*marketdata.Instrument{call, put} {
		if d := math.Abs(math.Abs(in.Greeks.Delta) - 0.16); d > 0.03+1e-12 {
			t.Errorf("%s |Δ| %.3f is outside 0.16 ± 0.03", in.OptionType, in.Greeks.Delta)
		}
	}
	if _, _, err := strategy.SelectStrikePair(ethGrid(exp), exp, 0.16, 0.005); err == nil {
		t.Error("no strike within 0.16 ± 0.005: the pair must fail like a single leg does")
	}
}
