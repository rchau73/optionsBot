package strategy

import (
	"fmt"
	"log/slog"
	"math"
	"time"

	"optionsbot/internal/orders"
	"optionsbot/internal/risk"
)

// The IM-over-limit alert (#166, the product owner's option B). Between
// rebalances nothing resizes the book: when money leaves the account (or a
// price move lifts IM) the book can sit over its IM limit until a confirmed
// limit change, a restart or the monitor's Rebalance button — and then the
// restart buys back at market (2026-10-17 16:04 BTC). This rule only tells:
// one im_over_limit journal line and WARN per episode, the episode shown in
// /api/status, im_over_limit_cleared when it ends. It never sends an order.
//
// The episode lives in memory: a restart in the middle of one journals it
// again once (one line per restart, by choice — simpler than rebuilding it
// from the journal, and a restart is worth a fresh reminder).

// IMOverLimitNext says whether an IM-over-limit episode is open after this
// reading. It opens when IM is more than bandPct points above the limit (the
// same band the startup rebalance leaves alone, #123) and stays open until IM
// is back at or under the limit, so a reading that wavers around the band's
// edge is one episode, not many. Pure.
func IMOverLimitNext(open bool, imPct, limitPct, bandPct float64) bool {
	if open {
		return imPct > limitPct
	}
	return imPct > limitPct+math.Max(bandPct, 0)
}

// IMOverLimitDetail is the im_over_limit journal detail: IM, the limit, the
// margin balance now and at the last rebalance or confirmed limit change
// (refBalance, 0 = none yet), and what to do. Pure.
func IMOverLimitDetail(u risk.Usage, limitPct, bandPct, refBalance float64) string {
	ref := "no rebalance since startup"
	if refBalance > 0 {
		ref = fmt.Sprintf("%s %s at the last rebalance or limit change (%+.1f%%)",
			fmtBalance(refBalance), u.Unit, (u.MarginBalance/refBalance-1)*100)
	}
	return fmt.Sprintf("IM %.1f%% over limit %.0f%% (+%g pp band) with no rebalance due · margin balance %s %s now, %s · no order sent: review with the Rebalance dry run in the monitor",
		u.IMPct(), limitPct, bandPct, fmtBalance(u.MarginBalance), u.Unit, ref)
}

func fmtBalance(v float64) string { return fmt.Sprintf("%.4g", v) }

// noteIMOverLimit opens or clears the episode for this cycle's margin and
// journals the change. Decision loop only.
func (s *Strategy) noteIMOverLimit(m marginState, now time.Time) {
	open := !s.imOverSince.IsZero()
	band := s.cfg.RebalanceStartupBandPct
	next := IMOverLimitNext(open, m.usage.IMPct(), m.status.LimitIMPct, band)
	switch {
	case next && !open:
		s.imOverSince = now
		detail := IMOverLimitDetail(m.usage, m.status.LimitIMPct, band, s.refBalance)
		slog.Warn("IM over the limit with no rebalance due: review with a dry run",
			"im_pct", fmt.Sprintf("%.2f", m.usage.IMPct()), "limit_im_pct", m.status.LimitIMPct, "band_pp", band,
			"margin_balance", m.usage.MarginBalance, "ref_margin_balance", s.refBalance, "unit", m.usage.Unit)
		rec := s.riskRecord(orders.RiskIMOverLimit, detail, m.status, m.usage, "")
		rec.RefMarginBalance = s.refBalance
		s.journal.LogRisk(rec)
	case !next && open:
		since := s.imOverSince
		s.imOverSince = time.Time{}
		detail := fmt.Sprintf("IM %.1f%% back at or under limit %.0f%% (over since %s)",
			m.usage.IMPct(), m.status.LimitIMPct, since.UTC().Format("2006-01-02 15:04"))
		slog.Info("IM back under the limit", "im_pct", fmt.Sprintf("%.2f", m.usage.IMPct()),
			"limit_im_pct", m.status.LimitIMPct, "over_since", since)
		s.logRisk(orders.RiskIMOverLimitDone, detail, m.status, m.usage, "")
	default:
		return
	}
	s.pub.mu.Lock()
	s.pub.risk.setIMOverLimit(s.imOverSince)
	s.pub.mu.Unlock()
}
