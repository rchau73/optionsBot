package strategy

import (
	"time"

	"optionsbot/internal/orders"
)

// Why a one-legged strangle's missing leg is not re-sold. repairGate checks
// the per-strangle rules in repair's order; heldLegViews adds the cycle-wide
// gates in front of repair. Repair and the monitor share the chain, so the
// monitor never shows a reason the bot does not apply.
const (
	HoldMMBreach      = "mm_breach"         // maintenance margin at or above max_mm_pct
	HoldStopDeferred  = "stop_deferred"     // a stop-loss waits for the spread
	HoldGEXWait       = "gex_wait"          // no fresh GEX snapshot
	HoldRepairWorking = "repair_working"    // a repair order is on the book
	HoldUnconfirmed   = "unconfirmed_order" // an order with an unknown outcome in the slot
	HoldChurn         = "churn_paused"      // churn breaker
	HoldGEXShedding   = "gex_shedding"      // the live GEX signal sheds this side
	HoldRolloutWindow = "rollout_window"    // the open leg is about to roll
	HoldRegimeSide    = "regime_side"       // confirmed negative regime, side of the trend
	HoldGEXShed       = "gex_shed"          // shed by GEX, waiting on its anchor
	HoldStopped       = "stopped"           // stopped out or closed by hand, RepairBlockReason
)

// repairGate returns the first rule holding the repair of st's missing leg,
// or "" when repair may sell it now. observe counts the GEX snapshot on a
// shed leg's anchor; only repair passes true, so reading the gate for the
// monitor never changes what repair sees.
func (s *Strategy) repairGate(st *orders.Strangle, present *orders.Position, missing string,
	dec GammaDecision, m marginState, now time.Time, observe bool) (rule, reason string) {
	key := makeSlotKey(st.TargetDTE, st.EntryDelta)
	sideKey := stopKey(st.ID, missing)
	switch {
	case s.repairPending(st.ID):
		return HoldRepairWorking, "a repair order is working"
	case s.slotUnconfirmed(key):
		return HoldUnconfirmed, "an order with an unknown outcome is being cancelled in this slot"
	case s.churnPaused(key, now):
		return HoldChurn, "churn breaker: slot paused until " + s.churnUntil[key].UTC().Format("Jan 2 15:04 UTC")
	case dec.Sheds(missing):
		return HoldGEXShedding, "GEX is shedding " + missing + "s"
	case present.DTE() <= s.cfg.RolloutDTE:
		return HoldRolloutWindow, "the open leg is inside the rollout window"
	}
	if reason := RegimeSideBlockReason(missing, dec.TrendDir, m.status); reason != "" {
		return HoldRegimeSide, reason
	}
	if a, shed := s.shedAnchors[sideKey]; shed {
		if s.cfg.GEXDirection == DirectionBias { // counted for every anchor in repairIncompleteStrangles
			if reason := BiasRepairBlockReason(a, missing, now, s.biasRepairCooldown(), s.cfg.GEXRepairConfirmSnapshots); reason != "" {
				return HoldGEXShed, reason
			}
		} else {
			if observe {
				a.Observe(dec.Spot, dec.SnapshotAt)
			}
			if reason := ShedRepairBlockReason(a, now, dec.NonNegativeSince,
				s.cfg.GEXRepairConfirmSnapshots, s.gexRepairRelease()); reason != "" {
				return HoldGEXShed, reason
			}
		}
	}
	if at, stopped := s.stopped[sideKey]; stopped {
		if reason := RepairBlockReason(at, now, s.repairCooldown(), m.status); reason != "" {
			return HoldStopped, reason
		}
	}
	return "", ""
}

func (s *Strategy) gexRepairRelease() time.Duration {
	return time.Duration(s.cfg.GEXRepairReleaseHours) * time.Hour
}

func (s *Strategy) biasRepairCooldown() time.Duration {
	return time.Duration(s.cfg.GEXBiasRepairCooldownHours) * time.Hour
}

func (s *Strategy) repairCooldown() time.Duration {
	return time.Duration(s.cfg.RepairCooldownHours) * time.Hour
}

// HeldLegView is a strangle's missing leg and what holds its repair, with
// the figures each release condition is measured against.
type HeldLegView struct {
	StrangleID string         `json:"strangle_id"`
	Slot       orders.SlotRef `json:"slot"`
	Missing    string         `json:"missing"`  // call | put
	OpenLeg    string         `json:"open_leg"` // the leg still held
	Expiry     time.Time      `json:"expiry"`
	Rule       string         `json:"rule"` // Hold* constant; "" = repair may sell it this cycle
	Reason     string         `json:"reason"`

	Trend             string  `json:"trend"` // the direction the rules use
	SMATrend          string  `json:"sma_trend"`
	Bias              string  `json:"gex_bias"`
	ConfirmedNegative bool    `json:"confirmed_negative"` // the margin policy's confirmed regime
	LiveRegime        string  `json:"live_regime"`
	Spot              float64 `json:"spot"`      // the GEX snapshot's spot
	LiveFlip          float64 `json:"live_flip"` // 0 = no flip
	BufferPct         float64 `json:"buffer_pct"`
	// AboveFlipLevel is the live flip × (1 + buffer): where the shed's anchor
	// rule would release a leg shed at today's flip. Shown for comparison.
	AboveFlipLevel float64 `json:"above_flip_level"`

	Anchor           *AnchorView `json:"anchor,omitempty"`             // shed by GEX
	StoppedAt        *time.Time  `json:"stopped_at,omitempty"`         // stopped out or closed by hand
	CooldownUntil    *time.Time  `json:"cooldown_until,omitempty"`     // StoppedAt + repair_cooldown_hours
	NonNegativeSince *time.Time  `json:"non_negative_since,omitempty"` // live regime non-negative since
}

// AnchorView is the flip a GEX-shed leg was shed at and how far its release
// is. Mode "flip": spot back above Level on Need snapshots, or ReleaseHours
// of non-negative regime. Mode "bias": after CooldownUntil, the bias off the
// shed side on Need snapshots (Clear so far).
type AnchorView struct {
	Mode          string     `json:"mode"` // flip | bias
	CooldownUntil *time.Time `json:"cooldown_until,omitempty"`
	Clear         int        `json:"clear"`
	ShedAt        time.Time  `json:"shed_at"`
	Flip          float64    `json:"flip"`
	BufferPct     float64    `json:"buffer_pct"`
	Level         float64    `json:"level"`         // spot must close above this …
	Above         int        `json:"above"`         // … on this many snapshots so far …
	Need          int        `json:"need"`          // … of these
	ReleaseHours  int        `json:"release_hours"` // or the live regime non-negative this long
}

// heldLegViews describes every one-legged strangle's missing leg. Call on
// the Run goroutine (it reads loop-owned maps), after repair.
func (s *Strategy) heldLegViews(dec GammaDecision, m marginState, now time.Time) []HeldLegView {
	out := make([]HeldLegView, 0)
	for _, st := range s.state.AllStrangles() {
		present, missing := s.liveLegs(st)
		if present == nil || missing == "" {
			continue
		}
		hv := HeldLegView{
			StrangleID: st.ID, Slot: orders.SlotRef{DTE: st.TargetDTE, Delta: st.EntryDelta},
			Missing: missing, OpenLeg: present.Instrument, Expiry: present.Expiry,
			Trend: dec.Trend, SMATrend: dec.SMATrend, Bias: dec.Bias, ConfirmedNegative: m.status.RegimeUsed && m.status.RegimeNegative,
			LiveRegime: dec.Regime, Spot: dec.Spot, BufferPct: dec.FlipBufferPct,
		}
		if dec.GammaFlipFound {
			hv.LiveFlip = dec.GammaFlip
			hv.AboveFlipLevel = dec.GammaFlip * (1 + dec.FlipBufferPct/100)
		}
		if !dec.NonNegativeSince.IsZero() {
			t := dec.NonNegativeSince
			hv.NonNegativeSince = &t
		}
		key := stopKey(st.ID, missing)
		if a, ok := s.shedAnchors[key]; ok {
			hv.Anchor = &AnchorView{Mode: "flip", ShedAt: a.At, Flip: a.Flip, BufferPct: a.BufferPct, Level: a.Level(),
				Above: a.Above, Need: s.cfg.GEXRepairConfirmSnapshots, ReleaseHours: s.cfg.GEXRepairReleaseHours}
			if s.cfg.GEXDirection == DirectionBias {
				until := a.At.Add(s.biasRepairCooldown())
				hv.Anchor.Mode, hv.Anchor.CooldownUntil, hv.Anchor.Clear = "bias", &until, a.Clear
			}
		}
		if at, ok := s.stopped[key]; ok {
			until := at.Add(s.repairCooldown())
			hv.StoppedAt, hv.CooldownUntil = &at, &until
		}
		switch {
		case m.mmBreached():
			hv.Rule, hv.Reason = HoldMMBreach, "maintenance margin at or above the limit"
		case s.stopsDeferred:
			hv.Rule, hv.Reason = HoldStopDeferred, "a stop-loss is waiting for the spread"
		case m.gexWait != "":
			hv.Rule, hv.Reason = HoldGEXWait, m.gexWait
		default:
			hv.Rule, hv.Reason = s.repairGate(st, present, missing, dec, m, now, false)
		}
		out = append(out, hv)
	}
	return out
}

// recordHeldLegs publishes heldLegViews for the monitor. Call on the Run goroutine.
func (s *Strategy) recordHeldLegs(dec GammaDecision, m marginState) {
	held := s.heldLegViews(dec, m, time.Now())
	s.pub.mu.Lock()
	defer s.pub.mu.Unlock()
	s.pub.held = held
}
