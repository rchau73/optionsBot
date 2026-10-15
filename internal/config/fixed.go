package config

// Settings fixed in code (2026-10-14). They were YAML keys that were never
// tuned after their study, so they only added things to manage; each keeps
// the value both shipped configs had. A config file that still sets one
// fails to load (fixedKeys), so nobody believes a changed value is in force.
// To make one tunable again, give its field back its yaml tag and drop it
// from ApplyFixed and fixedKeys. cmd/stress keeps flags for the rules, so a
// fixed value can still be simulated before it is changed.
const (
	// GEX
	FixedGEXMethod                = "script" // the reference script's rules (tests/gex_parity_test.go)
	FixedGEXStrikeRangePct        = 0.15     // strikes within ±15 % of each expiry's future, as the script
	FixedGammaRegimeBandPct       = 0.025    // nearest_flip method only
	FixedGEXConfirmSnapshots      = 2        // a shed, and a shed leg's repair, hold on 2 consecutive snapshots
	FixedGEXRepairReleaseHours    = 24       // …or the live regime non-negative this long
	FixedGEXBiasRepairCooldownHrs = 24       // bias: a shed leg waits this long (24 h beat 4 h, stress 2026-10-12)
	FixedGEXBiasFlipWindowMinutes = 60       // bias: median flip window (pre-expiry flip jumps, 2026-10-14)
	FixedGammaTrendLookbackDays   = 120      // SMA trend: daily closes kept
	FixedSwingPivotN              = 5        // SMA trend: bars on each side of a swing pivot
	FixedIVPercentileWindow       = 252      // DVOL days the IV percentile ranks against
	FixedIVBandConfirmDays        = 2        // margin band / regime changes confirmed on 2 daily closes
	FixedStopSpreadGuardPct       = 20       // a stop waits while the ask is > 20 % above mid…
	FixedStopSpreadMaxWaitMinutes = 5        // …at most 5 minutes
	FixedChurnMaxRoundTrips       = 3        // churn breaker: 3 buy-back/re-sell round trips…
	FixedChurnWindowMinutes       = 60       // …within 60 minutes pause the slot
	FixedRebalanceRetryMinutes    = 15       // a short rebalance complement retries after this…
	FixedRebalanceBackoffAfter    = 3        // …doubled from the 3rd unfilled rebalance pass in a row…
	FixedRebalanceRetryMaxMinutes = 60       // …up to 60 minutes (2026-10-14 BTC: 12 unfilled top-up orders in 1.5 h)
	FixedOrderFillTimeoutSec      = 90       // a resting sell is cancelled after this
	FixedOrderSlippagePct         = 0.05     // re-price up when the ask drifts this far above the limit
	FixedOrderMaxAdjustments      = 3        // at most this many re-prices up
	FixedEntryPriceFloor          = "mid"    // entries and top-ups step down to mid
	FixedRepairPriceFloor         = "bid"    // repairs cross to the bid (restore the hedge)
	FixedMaxDTEDeviation          = 10       // an expiry may be this many days from the slot's DTE
	FixedExpiryStretch            = 1.5      // or the nearest free one up to DTE × this
	FixedDeltaSlippage            = 0.03     // strike delta at most this far from the entry delta
	FixedMinTradeAmount           = 0.1      // order lot when an instrument has no minimum (Deribit's is read per option)
	FixedEvalIntervalMS           = 40000    // decision loop period
	FixedReportIntervalSec        = 60       // heartbeat and P&L line period
	FixedMarketRecordMinutes      = 60       // market history recording period
	FixedSpreadAlertThreshold     = 0.05     // journal warns on spreads above 5 %
	FixedHedgeReportThreshold     = 0.05     // hedge_report.json from this |net delta|
)

// fixedKeys are the YAML keys of the settings above, with their value as the
// load error shows it.
var fixedKeys = map[string]string{
	"target_dte":                     "use dte_delta_matrix",
	"entry_delta":                    "use dte_delta_matrix",
	"gex_method":                     "script",
	"gex_strike_range_pct":           "0.15",
	"gamma_regime_band_pct":          "0.025, nearest_flip only",
	"gex_shed_confirm_snapshots":     "2",
	"gex_repair_confirm_snapshots":   "2",
	"gex_repair_release_hours":       "24",
	"gex_bias_repair_cooldown_hours": "24",
	"gex_bias_flip_window_minutes":   "60",
	"gamma_trend_lookback_days":      "120",
	"swing_pivot_n":                  "5",
	"iv_percentile_window":           "252",
	"iv_band_confirm_days":           "2",
	"stop_spread_guard_pct":          "20",
	"stop_spread_max_wait_minutes":   "5",
	"churn_max_round_trips":          "3",
	"churn_window_minutes":           "60",
	"rebalance_retry_minutes":        "15",
	"order_fill_timeout_sec":         "90",
	"order_slippage_pct":             "0.05",
	"order_max_adjustments":          "3",
	"entry_price_floor":              "mid",
	"repair_price_floor":             "bid",
	"max_dte_deviation":              "10",
	"expiry_stretch":                 "1.5",
	"delta_slippage":                 "0.03",
	"min_trade_amount":               "0.1, Deribit's minimum is read per option",
	"eval_interval_ms":               "40000",
	"report_interval_sec":            "60",
	"market_record_minutes":          "60",
	"spread_alert_threshold":         "0.05",
	"hedge_report_threshold":         "0.05",
}

// ApplyFixed sets the settings fixed in code. Load calls it; a Config built
// in code (tests, sweeps) may call it too and then change what it studies.
func (c *Config) ApplyFixed() {
	c.TargetDTE, c.EntryDelta = nil, 0
	c.GEXMethod = FixedGEXMethod
	c.GEXStrikeRangePct = FixedGEXStrikeRangePct
	c.GammaRegimeBandPct = FixedGammaRegimeBandPct
	c.GEXShedConfirmSnapshots = FixedGEXConfirmSnapshots
	c.GEXRepairConfirmSnapshots = FixedGEXConfirmSnapshots
	c.GEXRepairReleaseHours = FixedGEXRepairReleaseHours
	c.GEXBiasRepairCooldownHours = FixedGEXBiasRepairCooldownHrs
	c.GEXBiasFlipWindowMinutes = FixedGEXBiasFlipWindowMinutes
	c.GammaTrendLookbackDays = FixedGammaTrendLookbackDays
	c.SwingPivotN = FixedSwingPivotN
	c.IVPercentileWindow = FixedIVPercentileWindow
	c.IVBandConfirmDays = FixedIVBandConfirmDays
	c.StopSpreadGuardPct = FixedStopSpreadGuardPct
	c.StopSpreadMaxWaitMin = FixedStopSpreadMaxWaitMinutes
	c.ChurnMaxRoundTrips = FixedChurnMaxRoundTrips
	c.ChurnWindowMinutes = FixedChurnWindowMinutes
	c.RebalanceRetryMinutes = FixedRebalanceRetryMinutes
	c.RebalanceBackoffAfter = FixedRebalanceBackoffAfter
	c.RebalanceRetryMaxMinutes = FixedRebalanceRetryMaxMinutes
	c.OrderFillTimeoutSec = FixedOrderFillTimeoutSec
	c.OrderSlippagePct = FixedOrderSlippagePct
	c.OrderMaxAdjustments = FixedOrderMaxAdjustments
	c.EntryPriceFloor = FixedEntryPriceFloor
	c.RepairPriceFloor = FixedRepairPriceFloor
	c.MaxDTEDeviation = FixedMaxDTEDeviation
	c.ExpiryStretch = FixedExpiryStretch
	c.DeltaSlippage = FixedDeltaSlippage
	c.MinTradeAmount = FixedMinTradeAmount
	c.EvalIntervalMS = FixedEvalIntervalMS
	c.ReportIntervalSec = FixedReportIntervalSec
	c.MarketRecordMinutes = FixedMarketRecordMinutes
	c.SpreadAlertThreshold = FixedSpreadAlertThreshold
	c.HedgeReportThreshold = FixedHedgeReportThreshold
}
