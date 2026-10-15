package config

import (
	"errors"
	"fmt"
	"io"
	"os"
	"strconv"
	"strings"

	"gopkg.in/yaml.v3"

	"optionsbot/internal/risk"
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
// Strategy/logic fields are loaded from config.yaml; those tagged yaml:"-"
// are fixed in code (fixed.go, ApplyFixed) and only tests and sweeps vary them.
// Platform/connectivity fields are loaded from .env.
type Config struct {
	// ── Loaded from config.yaml ───────────────────────────────────────────────
	Underlying     string          `yaml:"underlying"`
	StrategyID     string          `yaml:"strategy_id"` // names the strategy in orders.log and order labels
	DTEDeltaMatrix []DTEDeltaEntry `yaml:"dte_delta_matrix"`
	// Legacy fields — set in code by backtest scenario sweeps only.
	// Ignored by Slots() when dte_delta_matrix is set.
	TargetDTE  []int   `yaml:"-"`
	EntryDelta float64 `yaml:"-"`
	RolloutDTE int     `yaml:"rollout_dte"`
	// RepairCooldownHours: a leg closed by a stop-loss is re-sold no sooner
	// than this, and only when entries are not frozen and the confirmed gamma
	// regime is not negative.
	RepairCooldownHours int `yaml:"repair_cooldown_hours"`
	// RebalanceRetryMinutes: when a rebalance complement times out short of
	// its size, the rebalance runs again after this long (it would otherwise
	// wait for the next confirmed limit change or a restart).
	RebalanceRetryMinutes int `yaml:"-"`
	// RebalanceBackoffAfter / RebalanceRetryMaxMinutes: from this many
	// complements in a row that filled nothing, the retry wait doubles per
	// unfilled complement (15 → 30 → 60 min), up to the max. A fill or a limit
	// change resets it.
	RebalanceBackoffAfter    int `yaml:"-"`
	RebalanceRetryMaxMinutes int `yaml:"-"`
	// ChurnMaxRoundTrips / ChurnWindowMinutes: a slot that buys back and
	// re-sells this many times within the window is paused for the window
	// (no entries, repairs, upsizes or leg balancing; exits still run).
	ChurnMaxRoundTrips int `yaml:"-"`
	// MarketRecordMinutes: every this many minutes the bot records every
	// option of its underlying (mainnet quotes) to data/market/ for
	// backtests and stress tests; negative → off.
	MarketRecordMinutes int `yaml:"-"`
	ChurnWindowMinutes  int `yaml:"-"`
	// MaxLegSizeMultiple caps a new leg at this × the slot's normal size (its
	// share ÷ the strangle's standalone margin per lot).
	MaxLegSizeMultiple  float64 `yaml:"max_leg_size_multiple"`
	DeltaDriftThreshold float64 `yaml:"delta_drift_threshold"`
	ROITakeProfit       float64 `yaml:"roi_take_profit"`
	StopLossMultiplier  float64 `yaml:"stop_loss_multiplier"`
	// StopSpreadGuardPct: a stop-loss waits while the leg's ask is more than
	// this % above its mid (an emptied book: a flash wick) and fires once the
	// spread is back under it, re-checked each cycle — at most
	// StopSpreadMaxWaitMin minutes, then at market. −1 = off.
	StopSpreadGuardPct   float64 `yaml:"-"`
	StopSpreadMaxWaitMin int     `yaml:"-"`
	// DeltaExitThreshold: a short leg whose |delta| reaches this is bought
	// back and held like a stopped leg (must be above every entry delta).
	DeltaExitThreshold float64 `yaml:"delta_exit_threshold"`

	GammaTrendLookbackDays int     `yaml:"-"`
	SwingPivotN            int     `yaml:"-"`
	GammaRegimeBandPct     float64 `yaml:"-"`
	// GammaFlipBufferSD: a leg is shed only once spot is this many daily
	// standard deviations (DVOL ÷ √365) below the gamma flip.
	GammaFlipBufferSD float64 `yaml:"gamma_flip_buffer_sd"`
	// GEXShedConfirmSnapshots: a leg is shed only once the signal has held
	// for this many consecutive GEX snapshots (one per minute; 1 = at once).
	GEXShedConfirmSnapshots int `yaml:"-"`
	// GEXRepairConfirmSnapshots: a GEX-shed leg is re-sold once spot is above
	// the flip it was shed at plus its buffer on this many consecutive
	// snapshots, or once the live regime has been non-negative for
	// GEXRepairReleaseHours.
	GEXRepairConfirmSnapshots int `yaml:"-"`
	GEXRepairReleaseHours     int `yaml:"-"`
	// GEXDirection: the side to avoid in a negative regime — "sma" (swing
	// levels and SMAs of daily closes) or "bias" (the reference script's
	// ACCELERATION BIAS: spot vs flip ± buffer). Under "bias" a shed leg is
	// re-sold GEXBiasRepairCooldownHours after the shed, once the bias has
	// left its side on GEXRepairConfirmSnapshots consecutive snapshots.
	GEXDirection               string `yaml:"gex_direction"`
	GEXBiasRepairCooldownHours int    `yaml:"-"`
	// GEXBiasFlipWindowMinutes: the bias holds only when it points the same
	// way against the live flip and the median flip of this many minutes
	// (strategy.StableBiasDir) — the flip jumps before daily expiries.
	// Negative → the live flip alone.
	GEXBiasFlipWindowMinutes int     `yaml:"-"`
	GEXStrikeRangePct        float64 `yaml:"-"`
	GEXMethod                string  `yaml:"-"` // script (default) | nearest_flip
	IVPercentileWindow       int     `yaml:"-"`
	HedgeReportThreshold     float64 `yaml:"-"`
	// Margin policy (see internal/risk): IM limit by IV-percentile band,
	// fixed MM limit, band/regime changes confirmed on daily closes.
	IVMarginBands        []risk.Band `yaml:"iv_margin_bands"`
	MaxMMPct             float64     `yaml:"max_mm_pct"`
	IVBandConfirmDays    int         `yaml:"-"`
	SpreadAlertThreshold float64     `yaml:"-"`

	EvalIntervalMS    int `yaml:"-"`
	ReportIntervalSec int `yaml:"-"` // heartbeat + P&L journal period
	MaxDTEDeviation   int `yaml:"-"`
	// ExpiryStretch: when a slot's own expiry is held by another slot, it may
	// use the nearest free expiry up to target DTE × this; otherwise it waits.
	ExpiryStretch       float64 `yaml:"-"`
	DeltaSlippage       float64 `yaml:"-"`
	MinTradeAmount      float64 `yaml:"-"`
	MinPremiumBTC       float64 `yaml:"min_premium"`
	OrderFillTimeoutSec int     `yaml:"-"`
	OrderSlippagePct    float64 `yaml:"-"`
	// EntryPriceFloor / RepairPriceFloor: how far a resting sell steps down
	// from the ask before order_fill_timeout_sec — "ask", "mid" or "bid".
	// Entries and rebalance upsizes default to mid; repairs, which restore a
	// one-sided strangle's hedge, to the bid.
	EntryPriceFloor     string `yaml:"-"`
	RepairPriceFloor    string `yaml:"-"`
	OrderMaxAdjustments int    `yaml:"-"`

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
	// AccountPollSec is how often the account/collateral summary is polled
	// for the monitor (BOT_ACCOUNT_POLL_SEC, default 10).
	AccountPollSec int
	// AdminToken (BOT_ADMIN_TOKEN) authorises the monitor's manual closes
	// (POST /api/positions/close); empty = off. Testnet only.
	AdminToken string
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

// Deribit WebSocket endpoints.
const (
	MainnetWSEndpoint = "wss://www.deribit.com/ws/api/v2"
	TestnetWSEndpoint = "wss://test.deribit.com/ws/api/v2"
)

// WSEndpoint is where the bot trades: testnet unless DERIBIT_ENV=live.
func (c *Config) WSEndpoint() string {
	if c.Environment == "live" {
		return MainnetWSEndpoint
	}
	return TestnetWSEndpoint
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
	if err := c.RiskPolicy(false).Validate(); err != nil {
		return err
	}
	if c.MaxLegSizeMultiple < 1 {
		return fmt.Errorf("max_leg_size_multiple %.2f must be at least 1", c.MaxLegSizeMultiple)
	}
	if c.RepairCooldownHours < 0 {
		return fmt.Errorf("repair_cooldown_hours %d must not be negative", c.RepairCooldownHours)
	}
	if c.DeltaDriftThreshold < 0 {
		return fmt.Errorf("delta_drift_threshold %.2f must not be negative", c.DeltaDriftThreshold)
	}
	for _, sl := range slots {
		// A leg sold near the drift threshold falls below it with the first
		// small move, is rolled, re-sold and rolled again — paying spread
		// each time (a simulation with 0.10 entries and drift 0.10 rolled 436
		// times in 4 months). Keep the threshold well below every entry delta.
		if c.DeltaDriftThreshold > MaxDriftToEntryRatio*sl.EntryDelta+1e-9 {
			return fmt.Errorf("delta_drift_threshold %.2f is too close to slot %d DTE's entry delta %.2f (max %.0f%% of it, %.3f): a new leg would be rolled by the first small move, over and over",
				c.DeltaDriftThreshold, sl.TargetDTE, sl.EntryDelta, MaxDriftToEntryRatio*100, MaxDriftToEntryRatio*sl.EntryDelta)
		}
		if c.DeltaExitThreshold <= sl.EntryDelta || c.DeltaExitThreshold >= 1 {
			return fmt.Errorf("delta_exit_threshold %.2f must be above every entry delta (slot %d DTE at %.2f) and below 1 — a new leg would be exited at once",
				c.DeltaExitThreshold, sl.TargetDTE, sl.EntryDelta)
		}
	}
	if c.StopLossMultiplier <= 0 {
		return fmt.Errorf("stop_loss_multiplier %.2f must be positive", c.StopLossMultiplier)
	}
	if c.GammaFlipBufferSD < 0 || c.GammaFlipBufferSD > 3 {
		return fmt.Errorf("gamma_flip_buffer_sd %.2f must be between 0 and 3 daily standard deviations", c.GammaFlipBufferSD)
	}
	if c.GEXDirection != "sma" && c.GEXDirection != "bias" {
		return fmt.Errorf("gex_direction %q must be sma or bias", c.GEXDirection)
	}
	if c.Environment != "testnet" && c.Environment != "live" {
		return fmt.Errorf("DERIBIT_ENV %q must be testnet or live", c.Environment)
	}
	return nil
}

// MaxDriftToEntryRatio caps delta_drift_threshold at this fraction of the
// smallest entry delta (0.16 entries allow up to 0.12).
const MaxDriftToEntryRatio = 0.75

// DefaultIVMarginBands is the margin policy used when config sets none:
// rich premium allows more initial margin, calm markets less.
var DefaultIVMarginBands = []risk.Band{
	{MinIVPct: 70, MaxIMPct: 50},
	{MinIVPct: 30, MaxIMPct: 35},
	{MinIVPct: 0, MaxIMPct: 20},
}

// RiskPolicy returns the margin policy; useRegime enables the gamma rule.
// Unset fields take the defaults Load applies, so a Config built in code
// (tests, sweeps) gets the same policy as one loaded from YAML.
func (c *Config) RiskPolicy(useRegime bool) risk.Config {
	p := risk.Config{Bands: c.IVMarginBands, MaxMMPct: c.MaxMMPct, ConfirmDays: c.IVBandConfirmDays, UseRegime: useRegime}
	if len(p.Bands) == 0 {
		p.Bands = DefaultIVMarginBands
	}
	if p.MaxMMPct == 0 {
		p.MaxMMPct = DefaultMaxMMPct
	}
	if p.ConfirmDays == 0 {
		p.ConfirmDays = DefaultIVBandConfirmDays
	}
	return p
}

// Margin policy defaults.
const (
	DefaultMaxMMPct          = 35
	DefaultIVBandConfirmDays = 2
)

// removedKeys were replaced by the margin policy. Loading a config that still
// sets them fails loudly, so nobody believes an old cap is still enforced.
var removedKeys = map[string]string{
	"max_margin_pct": "iv_margin_bands",
	"leverage":       "iv_margin_bands",
	"max_leverage":   "iv_margin_bands and max_mm_pct",
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

	raw, err := io.ReadAll(f)
	if err != nil {
		return nil, fmt.Errorf("read config: %w", err)
	}
	var keys map[string]any
	if err := yaml.Unmarshal(raw, &keys); err != nil {
		return nil, fmt.Errorf("decode config: %w", err)
	}
	for old, repl := range removedKeys {
		if _, ok := keys[old]; ok {
			return nil, fmt.Errorf("config key %q was removed: margin is now limited by %s (as %% of Deribit's margin balance) — see README", old, repl)
		}
	}
	if _, ok := keys["min_premium_btc"]; ok {
		return nil, errors.New(`config key "min_premium_btc" was renamed min_premium (in the underlying's coin)`)
	}
	for key := range keys {
		if v, ok := fixedKeys[key]; ok {
			return nil, fmt.Errorf("config key %q is fixed in code now (%s): remove it from the file — see README \"Configuration\"", key, v)
		}
	}
	var cfg Config
	if err := yaml.Unmarshal(raw, &cfg); err != nil {
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
	if cfg.MaxLegSizeMultiple == 0 {
		cfg.MaxLegSizeMultiple = 2
	}
	if cfg.RepairCooldownHours == 0 {
		cfg.RepairCooldownHours = 72
	}
	if cfg.DeltaExitThreshold == 0 {
		cfg.DeltaExitThreshold = 0.30
	}
	if cfg.GammaFlipBufferSD == 0 {
		cfg.GammaFlipBufferSD = 1
	}
	if cfg.GEXDirection == "" {
		cfg.GEXDirection = "sma"
	}
	// MinPremiumBTC == 0 disables the floor (accept any premium).
	cfg.ApplyFixed()

	// ── Margin policy defaults (validated in Validate) ────────────────────────
	if len(cfg.IVMarginBands) == 0 {
		cfg.IVMarginBands = append([]risk.Band(nil), DefaultIVMarginBands...)
	}
	if cfg.MaxMMPct == 0 {
		cfg.MaxMMPct = DefaultMaxMMPct
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
	cfg.AdminToken = strings.TrimSpace(os.Getenv("BOT_ADMIN_TOKEN"))
	cfg.AccountPollSec = envInt("BOT_ACCOUNT_POLL_SEC", 10)
	if cfg.AccountPollSec < 2 {
		cfg.AccountPollSec = 2 // keep the request rate negligible
	}

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
