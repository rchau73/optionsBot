package tests

import (
	"testing"

	"optionsbot/internal/orders"
)

func TestStepRounding(t *testing.T) {
	tests := []struct {
		name string
		fn   func(v, step float64) float64
		v    float64
		step float64
		want float64
	}{
		{"round removes float residue", orders.RoundToStep, 107 * 0.0001, 0.0001, 0.0107},
		{"round to nearest half tick up", orders.RoundToStep, 0.00526, 0.0005, 0.0055},
		{"round to nearest half tick down", orders.RoundToStep, 0.00524, 0.0005, 0.005},
		{"ceil moves to next tick", orders.CeilToStep, 0.0046, 0.0005, 0.005},
		{"ceil keeps exact tick", orders.CeilToStep, 0.005, 0.0005, 0.005},
		{"floor quantity to lot", orders.FloorToStep, 0.35, 0.1, 0.3},
		{"floor keeps exact lot", orders.FloorToStep, 0.3, 0.1, 0.3},
		{"floor below one lot is zero", orders.FloorToStep, 0.09, 0.1, 0},
		{"floor survives division error 0.3/0.1", orders.FloorToStep, 0.1 + 0.2, 0.1, 0.3},
		{"ceil survives division error 0.7/0.1", orders.CeilToStep, 0.7, 0.1, 0.7},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := tc.fn(tc.v, tc.step); got != tc.want {
				t.Errorf("got %v, want %v", got, tc.want)
			}
		})
	}
}

func TestRoundToTick_UsesDefaultTick(t *testing.T) {
	if got := orders.RoundToTick(0.012345); got != 0.0123 {
		t.Errorf("RoundToTick(0.012345) = %v, want 0.0123", got)
	}
}
