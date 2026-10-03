package config

import (
	"errors"
	"fmt"
	"os"
	"strconv"

	"gopkg.in/yaml.v3"
)

// DTEDeltaEntry pairs a target DTE with one or more entry deltas.
// Each (DTE, delta) combination becomes an independent strangle slot.
type DTEDeltaEntry struct {
	DTE    int       `yaml:"dte"`
	Deltas []float64 `yaml:"deltas"`
}

// StrangleSlot is the expanded unit of work: one (DTE, delta) pair.
type StrangleSlot struct {
	TargetDTE  int
	EntryDelta float64
}

// Config holds all runtime configuration.
// Strategy/logic fields are loaded from config.yaml.
// Platform/connectivity fields are loaded from .env.
type Config struct {
	// ── Loaded from config.yaml ───────────────────────────────────────────────
	Underlying     string          `yaml:"underlying"`
	StrategyID     string          `yaml:"strategy_id"` // names the strategy in orders.log and order labels
	DTEDeltaMatrix []DTEDeltaEntry `yaml:"dte_delta_matrix"`
	// Legacy fields — kept for backward-compatible configs and backtest scenario sweeps.
	// Ignored by Slots() when dte_delta_matrix is set.
	TargetDTE           []int   `yaml:"target_dte"`
	EntryDelta          float64 `yaml:"entry_delta"`
	RolloutDTE          int     `yaml:"rollout_dte"`
	DeltaDriftThreshold float64 `yaml:"delta_drift_threshold"`
	ROITakeProfit       float64 `yaml:"roi_take_profit"`
	StopLossMultiplier  float64 `yaml:"stop_loss_multiplier"`

	GammaTrendLookbackDays int     `yaml:"gamma_trend_lookback_days"`
	SwingPivotN            int     `yaml:"swing_pivot_n"`
	GammaRegimeBandPct     float64 `yaml:"gamma_regime_band_pct"`
	GEXStrikeRangePct      float64 `yaml:"gex_strike_range_pct"`
	IVPercentileWindow     int     `yaml:"iv_percentile_window"`
	HedgeReportThreshold   float64 `yaml:"hedge_report_threshold"`
	MaxMarginPct           float64 `yaml:"max_margin_pct"`
	Leverage               float64 `yaml:"leverage"`
	MaxLeverage            float64 `yaml:"max_leverage"`
	SpreadAlertThreshold   float64 `yaml:"spread_alert_threshold"`

	EvalIntervalMS      int     `yaml:"eval_interval_ms"`
	ReportIntervalSec   int     `yaml:"report_interval_sec"` // heartbeat + P&L journal period
	MaxDTEDeviation     int     `yaml:"max_dte_deviation"`
	DeltaSlippage       float64 `yaml:"delta_slippage"`
	MinTradeAmount      float64 `yaml:"min_trade_amount"`
	MinPremiumBTC       float64 `yaml:"min_premium_btc"`
	OrderFillTimeoutSec int     `yaml:"order_fill_timeout_sec"`
	OrderSlippagePct    float64 `yaml:"order_slippage_pct"`
	OrderMaxAdjustments int     `yaml:"order_max_adjustments"`

	Backtest Backtest `yaml:"backtest"`

	// ── Loaded from .env ──────────────────────────────────────────────────────
	Environment  string // "testnet" | "live" — from DERIBIT_ENV
	ClientID     string
	ClientSecret string
	RateLimit    RateLimitConfig
	Retry        RetryConfig
	Circuit      CircuitConfig
	Heartbeat    HeartbeatConfig
	// APIAddr is where the read-only monitor API listens (BOT_API_ADDR);
	// empty disables it. Use 127.0.0.1:<port> locally, :<port> in Docker
	// on the private compose network only.
	APIAddr string
}

type Backtest struct {
	FillModel             string  `yaml:"fill_model"`
	SlippagePct           float64 `yaml:"slippage_pct"`
	LimitFillRule         string  `yaml:"limit_fill_rule"`
	CommissionPerContract float64 `yaml:"commission_per_contract"`
	MarginCallLiquidation bool    `yaml:"margin_call_liquidation"`
}

type RateLimitConfig struct {
	WsNonMatchRPS    float64
	WsMatchRPS       float64
	MaxSubscriptions int
	SafetyFactor     float64
}

type RetryConfig struct {
	InitialMS  int
	Multiplier float64
	MaxMS      int
	MaxRetries int
}

type CircuitConfig struct {
	Threshold int
	OpenSec   int
}

type HeartbeatConfig struct {
	IntervalSec            int
	ReconnectMaxAttempts   int
	ReconnectBackoffBaseMS int
}

func (c *Config) WSEndpoint() string {
	if c.Environment == "live" {
		return "wss://www.deribit.com/ws/api/v2"
	}
	return "wss://test.deribit.com/ws/api/v2"
}

// IsLive reports whether the bot trades real capital (DERIBIT_ENV=live).
func (c *Config) IsLive() bool {
	return c.Environment == "live"
}

// RequireCredentials checks that API credentials are present. Only live and
// testnet trading need them; a backtest runs without any.
func (c *Config) RequireCredentials() error {
	if c.ClientID == "" || c.ClientSecret == "" {
		return errors.New("DERIBIT_CLIENT_ID and DERIBIT_CLIENT_SECRET must be set in .env")
	}
	return nil
}

// Validate rejects strategy settings that would make the bot misbehave
// silently, so a typo fails at startup instead of in the market.
func (c *Config) Validate() error {
	if c.Underlying == "" {
		return errors.New("underlying is required (BTC or ETH)")
	}
	slots := c.Slots()
	if len(slots) == 0 {
		return errors.New("no strangle slots: set dte_delta_matrix")
	}
	for _, sl := range slots {
		if sl.EntryDelta <= 0 || sl.EntryDelta >= 0.5 {
			return fmt.Errorf("slot %d DTE: entry delta %.2f must be in (0, 0.5) — an OTM option", sl.TargetDTE, sl.EntryDelta)
		}
		if sl.TargetDTE <= c.RolloutDTE {
			return fmt.Errorf("slot %d DTE is at or below rollout_dte %d: it would roll immediately", sl.TargetDTE, c.RolloutDTE)
		}
	}
	if c.MaxMarginPct <= 0 || c.MaxMarginPct > 1 {
		return fmt.Errorf("max_margin_pct %.2f must be in (0, 1]", c.MaxMarginPct)
	}
	if c.StopLossMultiplier <= 0 {
		return fmt.Errorf("stop_loss_multiplier %.2f must be positive", c.StopLossMultiplier)
	}
	if c.Environment != "testnet" && c.Environment != "live" {
		return fmt.Errorf("DERIBIT_ENV %q must be testnet or live", c.Environment)
	}
	return nil
}

// Slots returns the expanded list of (DTE, delta) strangle slots.
// When dte_delta_matrix is configured it is used; otherwise falls back to the
// legacy target_dte × entry_delta cross-product (one delta for all DTEs).
func (c *Config) Slots() []StrangleSlot {
	if len(c.DTEDeltaMatrix) > 0 {
		var slots []StrangleSlot
		for _, e := range c.DTEDeltaMatrix {
			for _, d := range e.Deltas {
				slots = append(slots, StrangleSlot{TargetDTE: e.DTE, EntryDelta: d})
			}
		}
		return slots
	}
	var slots []StrangleSlot
	for _, dte := range c.TargetDTE {
		slots = append(slots, StrangleSlot{TargetDTE: dte, EntryDelta: c.EntryDelta})
	}
	return slots
}

func Load(path string) (*Config, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, fmt.Errorf("open config: %w", err)
	}
	defer f.Close()

	var cfg Config
	if err := yaml.NewDecoder(f).Decode(&cfg); err != nil {
		return nil, fmt.Errorf("decode config: %w", err)
	}

	// ── Platform: credentials (from .env; checked by RequireCredentials) ─────
	cfg.ClientID = os.Getenv("DERIBIT_CLIENT_ID")
	cfg.ClientSecret = os.Getenv("DERIBIT_CLIENT_SECRET")

	// ── Platform: environment (from .env, defaults to testnet) ────────────────
	cfg.Environment = os.Getenv("DERIBIT_ENV")
	if cfg.Environment == "" {
		cfg.Environment = "testnet"
	}

	// ── Logic param defaults (config.yaml is authoritative; these are fallbacks)
	if cfg.EvalIntervalMS <= 0 {
		cfg.EvalIntervalMS = 35000
	}
	if cfg.ReportIntervalSec <= 0 {
		cfg.ReportIntervalSec = 60
	}
	if cfg.MaxDTEDeviation <= 0 {
		cfg.MaxDTEDeviation = 2
	}
	if cfg.MinTradeAmount <= 0 {
		cfg.MinTradeAmount = 0.05
	}
	if cfg.OrderFillTimeoutSec <= 0 {
		cfg.OrderFillTimeoutSec = 90
	}
	// MinPremiumBTC == 0 disables the floor (accept any premium).
	if cfg.OrderSlippagePct <= 0 {
		cfg.OrderSlippagePct = 0.05
	}
	if cfg.OrderMaxAdjustments <= 0 {
		cfg.OrderMaxAdjustments = 3
	}
	if cfg.SwingPivotN <= 0 {
		cfg.SwingPivotN = 3
	}
	if cfg.GammaRegimeBandPct <= 0 {
		cfg.GammaRegimeBandPct = 0.01 // 1% band around gamma flip
	}
	// GEXStrikeRangePct: 0 disables the filter (all strikes included).
	// config.yaml is the authority; no default override so users can explicitly disable.
	// DeltaSlippage == 0 is valid: disables the tolerance filter entirely.

	// ── Leverage defaults and guardrails ──────────────────────────────────────
	if cfg.MaxLeverage <= 0 {
		cfg.MaxLeverage = 10.0
	}
	if cfg.Leverage <= 0 {
		cfg.Leverage = 1.0
	}
	if cfg.Leverage > cfg.MaxLeverage {
		return nil, fmt.Errorf("leverage %.2f exceeds max_leverage %.2f", cfg.Leverage, cfg.MaxLeverage)
	}

	// ── Platform: rate limits (from .env) ─────────────────────────────────────
	cfg.RateLimit = RateLimitConfig{
		WsNonMatchRPS:    envFloat("DERIBIT_RATE_WS_NONMATCH_RPS", 20),
		WsMatchRPS:       envFloat("DERIBIT_RATE_WS_MATCH_RPS", 8),
		MaxSubscriptions: envInt("DERIBIT_RATE_MAX_SUBSCRIPTIONS", 1000),
		SafetyFactor:     envFloat("DERIBIT_RATE_SAFETY_FACTOR", 0.80),
	}

	// ── Platform: retry & backoff (from .env) ─────────────────────────────────
	cfg.Retry = RetryConfig{
		InitialMS:  envInt("DERIBIT_BACKOFF_INITIAL_MS", 500),
		Multiplier: envFloat("DERIBIT_BACKOFF_MULTIPLIER", 2.0),
		MaxMS:      envInt("DERIBIT_BACKOFF_MAX_MS", 30000),
		MaxRetries: envInt("DERIBIT_BACKOFF_MAX_RETRIES", 6),
	}

	// ── Platform: circuit breaker (from .env) ─────────────────────────────────
	cfg.Circuit = CircuitConfig{
		Threshold: envInt("DERIBIT_CIRCUIT_BREAKER_THRESHOLD", 5),
		OpenSec:   envInt("DERIBIT_CIRCUIT_BREAKER_OPEN_SEC", 60),
	}

	// ── Platform: monitor API (from env) ──────────────────────────────────────
	cfg.APIAddr = os.Getenv("BOT_API_ADDR")

	// ── Platform: heartbeat & reconnect (from .env) ───────────────────────────
	cfg.Heartbeat = HeartbeatConfig{
		IntervalSec:            envInt("DERIBIT_HEARTBEAT_INTERVAL_SEC", 15),
		ReconnectMaxAttempts:   envInt("DERIBIT_RECONNECT_MAX_ATTEMPTS", 10),
		ReconnectBackoffBaseMS: envInt("DERIBIT_RECONNECT_BACKOFF_BASE_MS", 1000),
	}

	if err := cfg.Validate(); err != nil {
		return nil, fmt.Errorf("invalid config: %w", err)
	}
	return &cfg, nil
}

func envFloat(key string, def float64) float64 {
	v := os.Getenv(key)
	if v == "" {
		return def
	}
	f, err := strconv.ParseFloat(v, 64)
	if err != nil {
		return def
	}
	return f
}

func envInt(key string, def int) int {
	v := os.Getenv(key)
	if v == "" {
		return def
	}
	i, err := strconv.Atoi(v)
	if err != nil {
		return def
	}
	return i
}
