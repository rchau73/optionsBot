package backtest

import "optionsbot/internal/config"

// Scenario defines a parameter combination for a sweep run.
type Scenario struct {
	Name          string
	EntryDelta    float64
	TargetDTE     []int
	RolloutDTE    int
	StopLossMulti float64
	ROITakeProfit float64
}

// DefaultScenarios returns the standard parameter sweep grid.
func DefaultScenarios() []Scenario {
	return []Scenario{
		{
			Name:       "base_16delta_45_60_90",
			EntryDelta: 0.16, TargetDTE: []int{45, 60, 90},
			RolloutDTE: 19, StopLossMulti: 2.0, ROITakeProfit: 0.50,
		},
		{
			Name:       "tight_10delta_45_60_90",
			EntryDelta: 0.10, TargetDTE: []int{45, 60, 90},
			RolloutDTE: 19, StopLossMulti: 2.0, ROITakeProfit: 0.50,
		},
		{
			Name:       "wide_20delta_45_60_90",
			EntryDelta: 0.20, TargetDTE: []int{45, 60, 90},
			RolloutDTE: 19, StopLossMulti: 2.0, ROITakeProfit: 0.50,
		},
		{
			Name:       "base_16delta_30_45_60",
			EntryDelta: 0.16, TargetDTE: []int{30, 45, 60},
			RolloutDTE: 14, StopLossMulti: 2.0, ROITakeProfit: 0.50,
		},
		{
			Name:       "high_roi_16delta_45_60_90",
			EntryDelta: 0.16, TargetDTE: []int{45, 60, 90},
			RolloutDTE: 19, StopLossMulti: 3.0, ROITakeProfit: 0.75,
		},
	}
}

// Apply returns a copy of cfg with this scenario's parameters. The slots are
// rebuilt as a dte_delta_matrix, because Slots() ignores the legacy
// target_dte/entry_delta fields whenever a matrix is configured.
func (sc Scenario) Apply(cfg *config.Config) *config.Config {
	out := *cfg
	out.DTEDeltaMatrix = make([]config.DTEDeltaEntry, len(sc.TargetDTE))
	for i, dte := range sc.TargetDTE {
		out.DTEDeltaMatrix[i] = config.DTEDeltaEntry{DTE: dte, Deltas: []float64{sc.EntryDelta}}
	}
	out.EntryDelta = sc.EntryDelta
	out.TargetDTE = sc.TargetDTE
	out.RolloutDTE = sc.RolloutDTE
	out.StopLossMultiplier = sc.StopLossMulti
	out.ROITakeProfit = sc.ROITakeProfit
	return &out
}
