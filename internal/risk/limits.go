// Package risk decides how much of Deribit's margin the strategy may use.
//
// The rules (pure functions, no I/O):
//
//   - The initial-margin (IM) limit, as % of Deribit's margin balance, follows
//     the IV-percentile band of DVOL: rich premium (high DVOL) allows more,
//     calm markets (low DVOL, a big move may be coming) allow less.
//   - A confirmed negative gamma regime forces the lowest band, whatever DVOL
//     says: dealer hedging then amplifies moves against short options.
//   - Changes are confirmed on daily closes only: a band or regime must hold
//     for ConfirmDays consecutive UTC daily closes before the limit moves, so a
//     spike does not reshape the book. While a change is unconfirmed, new risk
//     is frozen.
//   - The maintenance-margin (MM) limit is fixed and acts immediately: it
//     measures distance to liquidation, which no DVOL level makes safer.
package risk

import (
	"fmt"
	"sort"
	"time"
)

// Band maps an IV percentile floor to the IM limit that applies from it up.
type Band struct {
	MinIVPct float64 `yaml:"min_iv_pct" json:"min_iv_pct"`
	MaxIMPct float64 `yaml:"max_im_pct" json:"max_im_pct"`
}

// Config is the policy. Bands may be given in any order.
type Config struct {
	Bands       []Band
	MaxMMPct    float64
	ConfirmDays int
	// UseRegime enables the gamma-regime rule (off when no GEX source exists,
	// e.g. in backtests).
	UseRegime bool
}

// SortedBands returns the bands from the highest IV floor to the lowest.
func (c Config) SortedBands() []Band {
	b := append([]Band(nil), c.Bands...)
	sort.Slice(b, func(i, j int) bool { return b[i].MinIVPct > b[j].MinIVPct })
	return b
}

// Validate checks the bands cover 0–100 and every limit is a percentage.
func (c Config) Validate() error {
	if len(c.Bands) == 0 {
		return fmt.Errorf("iv_margin_bands: at least one band is required")
	}
	b := c.SortedBands()
	if b[len(b)-1].MinIVPct != 0 {
		return fmt.Errorf("iv_margin_bands: the lowest band must start at min_iv_pct 0")
	}
	for i, band := range b {
		if band.MinIVPct < 0 || band.MinIVPct > 100 {
			return fmt.Errorf("iv_margin_bands: min_iv_pct %.1f must be in [0, 100]", band.MinIVPct)
		}
		if band.MaxIMPct <= 0 || band.MaxIMPct > 100 {
			return fmt.Errorf("iv_margin_bands: max_im_pct %.1f must be in (0, 100]", band.MaxIMPct)
		}
		if i > 0 && band.MinIVPct == b[i-1].MinIVPct {
			return fmt.Errorf("iv_margin_bands: duplicate min_iv_pct %.1f", band.MinIVPct)
		}
	}
	if c.MaxMMPct <= 0 || c.MaxMMPct >= 100 {
		return fmt.Errorf("max_mm_pct %.1f must be in (0, 100): Deribit liquidates at 100", c.MaxMMPct)
	}
	if c.ConfirmDays < 1 {
		return fmt.Errorf("iv_band_confirm_days %d must be at least 1", c.ConfirmDays)
	}
	return nil
}

// BandIndex returns the index (in SortedBands order) of the band ivPct falls in.
func (c Config) BandIndex(ivPct float64) int {
	b := c.SortedBands()
	for i, band := range b {
		if ivPct >= band.MinIVPct {
			return i
		}
	}
	return len(b) - 1
}

// Day is one observation: a completed UTC day's close, or the live value.
// A field that is not Known carries no evidence either way.
type Day struct {
	Date        time.Time // UTC midnight of the day
	IVPct       float64
	IVKnown     bool
	Negative    bool // gamma regime is negative (acceleration)
	RegimeKnown bool
}

// Pending is a change seen at daily closes but not yet confirmed.
type Pending struct {
	Rule string `json:"rule"` // "dvol_band" | "gamma_regime"
	To   string `json:"to"`   // e.g. "band ≥70" or "negative"
	Days int    `json:"days"` // consecutive daily closes so far
	Need int    `json:"need"`
}

// Status is the policy's decision for now.
type Status struct {
	LimitIMPct float64 `json:"limit_im_pct"`
	MaxMMPct   float64 `json:"max_mm_pct"`
	Reason     string  `json:"reason"`

	Band           *Band   `json:"band"` // confirmed DVOL band; nil when unknown
	BandLimitPct   float64 `json:"band_limit_pct"`
	LiveIVPct      float64 `json:"live_iv_pct"`
	LiveIVKnown    bool    `json:"live_iv_known"`
	RegimeUsed     bool    `json:"regime_used"`
	RegimeKnown    bool    `json:"regime_known"` // a regime has been confirmed
	RegimeNegative bool    `json:"regime_negative"`

	// Frozen blocks new risk (entries, upsizes); exits and repairs continue.
	Frozen       bool      `json:"frozen"`
	FreezeReason string    `json:"freeze_reason,omitempty"`
	Pending      []Pending `json:"pending"`
	// CanRebalance is true when the limit rests on confirmed data, so the book
	// may be resized toward it.
	CanRebalance bool `json:"can_rebalance"`
}

// confirmed is the outcome of walking one rule's daily closes.
type confirmed struct {
	value   int
	known   bool
	pending int // candidate value, valid when count > 0
	count   int
}

// confirm walks daily closes (oldest first) and returns the confirmed value:
// a new value is confirmed after `need` consecutive calendar days at that
// value. A missing day or an unknown close resets the streak but keeps the
// confirmed value.
func confirm(days []obs, need int) confirmed {
	var c confirmed
	var prev time.Time
	for _, d := range days {
		gap := !prev.IsZero() && !d.date.Equal(prev.AddDate(0, 0, 1))
		prev = d.date
		if gap || !d.known {
			c.count = 0
		}
		if !d.known {
			continue
		}
		if c.known && d.value == c.value {
			c.count = 0
			continue
		}
		if c.count > 0 && d.value == c.pending {
			c.count++
		} else {
			c.pending, c.count = d.value, 1
		}
		if c.count >= need {
			c.value, c.known, c.count = d.value, true, 0
		}
	}
	return c
}

type obs struct {
	date  time.Time
	value int
	known bool
}

// Evaluate applies the policy to the completed daily closes (oldest first,
// today excluded) and the live observation.
func Evaluate(cfg Config, closes []Day, live Day) Status {
	bands := cfg.SortedBands()
	lowest := len(bands) - 1
	st := Status{
		MaxMMPct: cfg.MaxMMPct, RegimeUsed: cfg.UseRegime,
		LiveIVPct: live.IVPct, LiveIVKnown: live.IVKnown, Pending: []Pending{},
	}

	ivObs := make([]obs, len(closes))
	rgObs := make([]obs, len(closes))
	for i, d := range closes {
		ivObs[i] = obs{d.Date, cfg.BandIndex(d.IVPct), d.IVKnown}
		rgObs[i] = obs{d.Date, boolInt(d.Negative), d.RegimeKnown}
	}
	iv := confirm(ivObs, cfg.ConfirmDays)
	band := lowest
	if iv.known {
		band = iv.value
		b := bands[band]
		st.Band = &b
	}
	st.BandLimitPct = bands[band].MaxIMPct
	if iv.count > 0 {
		st.Pending = append(st.Pending, Pending{Rule: "dvol_band", To: bandLabel(bands[iv.pending]), Days: iv.count, Need: cfg.ConfirmDays})
	}

	var freeze []string
	if iv.known && live.IVKnown && cfg.BandIndex(live.IVPct) != iv.value {
		freeze = append(freeze, fmt.Sprintf("DVOL moved to %s (IV percentile %.0f), not yet confirmed", bandLabel(bands[cfg.BandIndex(live.IVPct)]), live.IVPct))
	}

	st.CanRebalance = iv.known
	if cfg.UseRegime {
		rg := confirm(rgObs, cfg.ConfirmDays)
		st.RegimeKnown, st.RegimeNegative = rg.known, rg.known && rg.value == 1
		if rg.count > 0 {
			st.Pending = append(st.Pending, Pending{Rule: "gamma_regime", To: regimeLabel(rg.pending == 1), Days: rg.count, Need: cfg.ConfirmDays})
		}
		switch {
		case !rg.known:
			freeze = append(freeze, fmt.Sprintf("gamma regime not confirmed yet (needs %d daily closes)", cfg.ConfirmDays))
			st.CanRebalance = false
		case live.RegimeKnown && live.Negative != st.RegimeNegative:
			freeze = append(freeze, fmt.Sprintf("gamma regime turned %s, not yet confirmed", regimeLabel(live.Negative)))
		}
	}

	switch {
	case st.RegimeNegative:
		st.LimitIMPct = bands[lowest].MaxIMPct
		st.Reason = fmt.Sprintf("negative gamma regime (confirmed): lowest band %.0f%%; DVOL band would allow %.0f%%", st.LimitIMPct, st.BandLimitPct)
	case iv.known:
		st.LimitIMPct = st.BandLimitPct
		st.Reason = fmt.Sprintf("DVOL %s (confirmed)", bandLabel(bands[band]))
	default:
		st.LimitIMPct = bands[lowest].MaxIMPct
		st.Reason = "DVOL percentile unknown: lowest band"
	}
	if len(freeze) > 0 {
		st.Frozen = true
		st.FreezeReason = joinReasons(freeze)
	}
	return st
}

func bandLabel(b Band) string { return fmt.Sprintf("band ≥%.0f (IM %.0f%%)", b.MinIVPct, b.MaxIMPct) }

func regimeLabel(negative bool) string {
	if negative {
		return "negative"
	}
	return "positive/neutral"
}

func boolInt(b bool) int {
	if b {
		return 1
	}
	return 0
}

func joinReasons(r []string) string {
	out := r[0]
	for _, s := range r[1:] {
		out += "; " + s
	}
	return out
}

// Usage is the account's margin in one unit (USD totals under cross
// collateral, otherwise the underlying currency).
type Usage struct {
	IM, MM, MarginBalance float64
	Unit                  string
}

// Pct returns part as % of the margin balance; margin required with no
// collateral counts as fully used.
func (u Usage) Pct(part float64) float64 {
	switch {
	case u.MarginBalance > 0:
		return part / u.MarginBalance * 100
	case part > 0:
		return 100
	default:
		return 0
	}
}

func (u Usage) IMPct() float64 { return u.Pct(u.IM) }
func (u Usage) MMPct() float64 { return u.Pct(u.MM) }

// Headroom is how much more IM the limit allows (≤ 0: none).
func (u Usage) Headroom(limitIMPct float64) float64 {
	return limitIMPct/100*u.MarginBalance - u.IM
}

// Fits reports whether a post-trade usage stays within both limits.
func Fits(post Usage, limitIMPct, maxMMPct float64) bool {
	return post.IMPct() <= limitIMPct && post.MMPct() <= maxMMPct
}
