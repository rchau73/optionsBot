package config

import (
	"fmt"
	"strconv"
	"strings"
)

// The tunable-parameter catalogue (#255): every setting a config file may
// hold, in plain words, with the range Validate accepts, the core policy it
// belongs to and the tools that can test a change before it is made. It is
// the shared base for the settings comparison (cmd/compare), a future form
// and parameter tips. docs/parameters.md is generated from it
// (tests/config_catalogue_test.go, UPDATE_GOLDEN=1). A new tuned key needs an
// entry here. The texts are a first draft, not user-facing until a prototype
// is approved.

// Core-policy areas a setting belongs to (CLAUDE.md "Trading rules"). A
// change to the default of a setting in any area but PolicyNone needs the
// product owner (PRD §3).
const (
	PolicyEntry  = "entry"
	PolicyExits  = "exits"
	PolicyRepair = "repair"
	PolicyMargin = "margin"
	PolicyGEX    = "gex"
	PolicySizing = "sizing"
	PolicyNone   = "none"
)

// Kinds of value a Tunable takes.
const (
	KindNumber  = "number"  // a decimal
	KindInteger = "integer" // a whole number
	KindEnum    = "enum"    // one of Values, checked at load
	KindText    = "text"    // free text, not checked against a list
	KindBool    = "bool"
)

// Tunable is one setting of a config file.
type Tunable struct {
	// Key is the YAML path: "backtest.fill_model"; "[]" stands for every
	// element of a list ("dte_delta_matrix[].deltas[]").
	Key   string
	Title string
	// Plain says what it does and how it moves risk and income, in one or
	// two sentences, without a promise of returns.
	Plain string
	Unit  string
	Kind  string
	// Min and Max are the values Validate accepts on their own (inclusive),
	// nil where it checks no such bound; Step is the input granularity, so
	// Min − Step and Max + Step are rejected.
	Min, Max *float64
	Step     float64
	Values   []string // KindEnum: every accepted value
	// Default is what Load uses when the key is absent (and, where said, 0).
	Default string
	// CrossRules are Validate's constraints involving other keys.
	CrossRules []string
	Policy     string
	// VariedBy names the tools that can test a change of it.
	VariedBy []string
}

// Tool names used in VariedBy.
const (
	toolCompare = "cmd/compare (two configs: backtest, replay, stress)"
	toolSweep   = "backtest --sweep"
	toolLadders = "backtest --sweep-ladders"
	toolBTOnly  = "backtest"
)

func num(v float64) *float64 { return &v }

// Tunables returns the catalogue in the order of the shipped config files.
func Tunables() []Tunable {
	return []Tunable{
		{
			Key: "underlying", Title: "Coin", Kind: KindText, Policy: PolicyNone,
			Plain: "The coin this bot trades (BTC or ETH). There is one config file per coin; it names the bot rather than tunes it.",
		},
		{
			Key: "strategy_id", Title: "Strategy name", Kind: KindText, Policy: PolicyNone,
			Plain: "Names the strategy in the journal and in order labels. The shipped files leave it unset; it does not change any trading rule.",
		},
		{
			Key: "dte_delta_matrix[].dte", Title: "Slot days to expiry", Kind: KindInteger, Unit: "days", Step: 1,
			Policy: PolicyEntry,
			Plain: "Each slot sells one strangle on an expiry about this many days away. Longer: more premium per trade and larger moves to sit through; " +
				"shorter: faster decay, more rolls and fees.",
			CrossRules: []string{"above rollout_dte, or a new strangle would be rolled at once"},
			VariedBy:   []string{toolSweep, toolLadders, "cmd/stress -slots", toolCompare},
		},
		{
			Key: "dte_delta_matrix[].deltas[]", Title: "Slot entry delta", Kind: KindNumber, Unit: "delta (absolute)",
			Min: num(0.01), Max: num(0.49), Step: 0.01, Policy: PolicyEntry,
			Plain: "How far from the price each leg is sold, as option delta (0.16 is about one standard deviation away). " +
				"Higher: more premium, and moves reach the strikes more often; lower: less premium, fewer stops.",
			CrossRules: []string{"delta_drift_threshold at most 75 % of it", "delta_exit_threshold above it"},
			VariedBy:   []string{toolSweep, "cmd/stress -slots DTE:delta, -slotdelta", toolCompare},
		},
		{
			Key: "rollout_dte", Title: "Roll at days to expiry", Kind: KindInteger, Unit: "days", Step: 1,
			Policy: PolicyExits,
			Plain: "A strangle is bought back when its expiry is this close, and its slot opens a new one. " +
				"Higher: leaves the riskier last weeks earlier and keeps less of the time decay.",
			CrossRules: []string{"below every slot's dte"},
			VariedBy:   []string{toolSweep, toolCompare},
		},
		{
			Key: "delta_drift_threshold", Title: "Roll when delta falls below", Kind: KindNumber, Unit: "delta (absolute)",
			Min: num(0), Step: 0.01, Policy: PolicyExits,
			Plain: "A leg whose |delta| falls below this has little premium left: it is bought back and re-sold nearer the entry delta. " +
				"Higher: rolls sooner and more often, paying spread and fees for fresher premium.",
			CrossRules: []string{"at most 75 % of every entry delta (config.MaxDriftToEntryRatio)"},
			VariedBy:   []string{"cmd/stress -drift", toolCompare},
		},
		{
			Key: "roi_take_profit", Title: "Take profit at", Kind: KindNumber, Unit: "fraction of premium",
			Policy: PolicyExits,
			Plain: "A leg is bought back once it has earned this share of its premium (0.50 = half) and is re-sold. " +
				"Lower: takes gains sooner and trades more; higher: holds longer for the rest, exposed to moves meanwhile.",
			VariedBy: []string{toolSweep, "cmd/stress -tp", toolCompare},
		},
		{
			Key: "stop_loss_multiplier", Title: "Stop-loss at", Kind: KindNumber, Unit: "× premium",
			Min: num(0.1), Step: 0.1, Policy: PolicyExits,
			Plain: "A leg is bought back at market when its loss reaches this multiple of its premium (2 = twice the premium). " +
				"Lower: cuts losses earlier with more stops on noise; higher: fewer stops, larger losses when one fires.",
			VariedBy: []string{toolSweep, toolCompare},
		},
		{
			Key: "delta_exit_threshold", Title: "Delta exit at", Kind: KindNumber, Unit: "delta (absolute)",
			Max: num(0.99), Step: 0.01, Default: "0.30 (also when 0)", Policy: PolicyExits,
			Plain: "A leg whose |delta| reaches this is bought back before the stop-loss and re-sold only after the repair cooldown. " +
				"Lower: exits earlier on smaller moves; higher: waits longer, closer to the stop.",
			CrossRules: []string{"above every entry delta"},
			VariedBy:   []string{"cmd/stress -breach", toolCompare},
		},
		{
			Key: "repair_cooldown_hours", Title: "Repair cooldown", Kind: KindInteger, Unit: "hours",
			Min: num(0), Step: 1, Default: "72 (also when 0)", Policy: PolicyRepair,
			Plain: "After a stop-loss, delta exit or manual close, the leg is re-sold no sooner than this, and only while entries are not frozen " +
				"and the gamma regime is not negative. Longer: less re-selling into a move, more time one-sided.",
			VariedBy: []string{toolCompare},
		},
		{
			Key: "gamma_flip_buffer_sd", Title: "Gamma flip buffer", Kind: KindNumber, Unit: "daily standard deviations",
			Min: num(0), Max: num(3), Step: 0.1, Default: "1 (also when 0)", Policy: PolicyGEX,
			Plain: "In a negative gamma regime a leg is shed only once spot is this many daily standard deviations beyond the gamma flip. " +
				"Higher: fewer sheds and more exposure in a falling market; lower: sheds sooner and more often.",
			VariedBy: []string{"cmd/stress -flipbufsd", toolCompare},
		},
		{
			Key: "gex_direction", Title: "Direction source", Kind: KindEnum, Values: []string{"sma", "bias"},
			Default: "sma", Policy: PolicyGEX,
			Plain: "Which side to avoid in a negative gamma regime: sma (the trend of daily moving averages) or bias " +
				"(spot against the gamma flip, as the reference script). It decides which legs are shed and when they are re-sold.",
			VariedBy: []string{"cmd/stress -direction", toolCompare},
		},
		{
			Key: "iv_margin_bands[].min_iv_pct", Title: "Margin band start", Kind: KindNumber, Unit: "DVOL percentile",
			Min: num(0), Max: num(100), Step: 1, Default: "bands 70 / 30 / 0 at 50 / 35 / 20 %", Policy: PolicyMargin,
			Plain: "Where a margin band starts, as the percentile of today's DVOL in the last year. " +
				"With max_im_pct it sets how much initial margin the bot may use when volatility is rich or calm.",
			CrossRules: []string{"the lowest band starts at 0", "no two bands start at the same percentile"},
			VariedBy:   []string{"cmd/stress -bands (three bands)", toolCompare},
		},
		{
			Key: "iv_margin_bands[].max_im_pct", Title: "Margin band limit", Kind: KindNumber, Unit: "% of margin balance",
			Min: num(1), Max: num(100), Step: 1, Default: "bands 70 / 30 / 0 at 50 / 35 / 20 %", Policy: PolicyMargin,
			Plain: "The most initial margin the bot may use in that band. Higher: larger positions and more premium, and larger losses in a move. " +
				"A confirmed negative gamma regime uses the lowest band.",
			VariedBy: []string{"cmd/stress -bands (three bands)", toolCompare},
		},
		{
			Key: "max_mm_pct", Title: "Maintenance-margin limit", Kind: KindNumber, Unit: "% of margin balance",
			Min: num(1), Max: num(99), Step: 1, Default: "35 (also when 0)", Policy: PolicyMargin,
			Plain: "Above this maintenance margin the bot reduces positions at market at once and opens nothing new; Deribit liquidates at 100. " +
				"Higher: more room before forced cuts, less before liquidation.",
			VariedBy: []string{toolCompare},
		},
		{
			Key: "max_leg_size_multiple", Title: "Leg size cap", Kind: KindNumber, Unit: "× normal size",
			Min: num(1), Step: 0.1, Default: "2 (also when 0)", Policy: PolicySizing,
			Plain: "A new leg is at most this many times its slot's normal size (its margin share ÷ the strangle's own margin per lot), " +
				"so offsets in the book cannot inflate a size. Higher: larger legs when the book offsets them.",
			VariedBy: []string{"cmd/stress -cap", toolCompare},
		},
		{
			Key: "min_premium", Title: "Minimum premium", Kind: KindNumber, Unit: "coin per contract",
			Policy: PolicyEntry,
			Plain: "The least premium a leg must bring at the lowest price its order can step down to; a leg under it is not sold, and 0 accepts any. " +
				"Higher: skips thin strikes, less income when premiums are low.",
			VariedBy: []string{toolCompare},
		},
		{
			Key: "backtest.fill_model", Title: "Backtest fill model", Kind: KindText, Policy: PolicyNone,
			Plain:    "Backtest only: mid, bid_ask or slippage. The simulator does not read it today; the trading bot never does.",
			VariedBy: []string{toolBTOnly},
		},
		{
			Key: "backtest.slippage_pct", Title: "Backtest market slippage", Kind: KindNumber, Unit: "fraction of mid",
			Policy:   PolicyNone,
			Plain:    "Backtest only: a simulated market order fills this fraction of the mid worse than the mid. No effect on trading.",
			VariedBy: []string{toolBTOnly},
		},
		{
			Key: "backtest.limit_fill_rule", Title: "Backtest limit fills", Kind: KindText, Policy: PolicyNone,
			Plain:    "Backtest only: next_tick fills a simulated limit order on the next tick; any other value fills it at once. No effect on trading.",
			VariedBy: []string{toolBTOnly},
		},
		{
			Key: "backtest.commission_per_contract", Title: "Backtest fee rate", Kind: KindNumber, Unit: "fraction of the underlying",
			Policy:   PolicyNone,
			Plain:    "Backtest only: the fee per contract as a fraction of the underlying (0.0003 = Deribit's 0.03 %), capped at 12.5 % of the price. No effect on trading.",
			VariedBy: []string{toolBTOnly},
		},
		{
			Key: "backtest.margin_call_liquidation", Title: "Backtest margin-call liquidation", Kind: KindBool, Policy: PolicyNone,
			Plain:    "Backtest only. The simulator does not read it today; the trading bot never does.",
			VariedBy: []string{toolBTOnly},
		},
	}
}

// FixedKeys returns the YAML keys fixed in code (fixed.go); a catalogue entry
// must never be one.
func FixedKeys() []string {
	out := make([]string, 0, len(fixedKeys))
	for k := range fixedKeys {
		out = append(out, k)
	}
	return out
}

// CatalogueMarkdown renders the catalogue as docs/parameters.md.
func CatalogueMarkdown(ts []Tunable) string {
	var b strings.Builder
	b.WriteString("# Tunable parameters\n\n")
	b.WriteString("<!-- Generated from internal/config/catalogue.go: go run ./cmd/paramdocs. " +
		"Do not edit by hand; tests/config_catalogue_test.go fails until it is regenerated. -->\n\n")
	b.WriteString("Every setting a `config_*.yaml` file may hold, in plain words. Everything else is fixed in code " +
		"(`internal/config/fixed.go`); a file that sets a fixed key fails to load. The range is what `Validate` accepts when the bot " +
		"loads the file; \"not checked\" means it accepts any value. The texts are a first draft (#255).\n\n")
	b.WriteString("**Core policies.** A setting whose policy is not `none` belongs to a core policy of the strategy: " +
		"**a change to its default needs the product owner** (PRD §3). Test a change first with the tools listed under \"Tested by\".\n\n")
	b.WriteString("| Key | Title | Policy | Range | Unit |\n|---|---|---|---|---|\n")
	for _, t := range ts {
		fmt.Fprintf(&b, "| `%s` | %s | %s | %s | %s |\n", t.Key, t.Title, t.Policy, rangeText(t), dash(t.Unit))
	}
	for _, t := range ts {
		fmt.Fprintf(&b, "\n## `%s` — %s\n\n%s\n\n", t.Key, t.Title, t.Plain)
		fmt.Fprintf(&b, "- **Range:** %s\n", rangeText(t))
		if t.Unit != "" {
			fmt.Fprintf(&b, "- **Unit:** %s\n", t.Unit)
		}
		if t.Default != "" {
			fmt.Fprintf(&b, "- **When absent:** %s\n", t.Default)
		}
		for _, r := range t.CrossRules {
			fmt.Fprintf(&b, "- **Also:** %s\n", r)
		}
		if t.Policy == PolicyNone {
			b.WriteString("- **Policy:** none\n")
		} else {
			fmt.Fprintf(&b, "- **Policy:** %s — a change to its default needs the product owner\n", t.Policy)
		}
		if len(t.VariedBy) > 0 {
			fmt.Fprintf(&b, "- **Tested by:** %s\n", strings.Join(t.VariedBy, "; "))
		}
	}
	return b.String()
}

func rangeText(t Tunable) string {
	f := func(v float64) string { return strconv.FormatFloat(v, 'g', -1, 64) }
	switch {
	case t.Kind == KindEnum:
		return strings.Join(t.Values, " \\| ")
	case t.Kind == KindBool:
		return "true \\| false"
	case t.Min != nil && t.Max != nil:
		return fmt.Sprintf("%s to %s (step %s)", f(*t.Min), f(*t.Max), f(t.Step))
	case t.Min != nil:
		return fmt.Sprintf("≥ %s (step %s)", f(*t.Min), f(t.Step))
	case t.Max != nil:
		return fmt.Sprintf("≤ %s (step %s)", f(*t.Max), f(t.Step))
	case len(t.CrossRules) > 0:
		return "see \"Also\""
	}
	return "not checked"
}

func dash(s string) string {
	if s == "" {
		return "—"
	}
	return s
}
