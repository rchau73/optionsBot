package perf

import (
	"time"

	"optionsbot/internal/history"
)

// MarginApprox labels how the margin used is measured: the history records
// IM as a % of the margin balance, not the balance itself, so the balance at
// the window start stands for every line (#278).
const MarginApprox = "IM used ≈ IM % × margin balance at the window start (an approximation until the history records the balance on every line)"

// MarginReturn is the PRD's engine-phase north star (#278): net P&L in coin,
// net of fees, per unit of initial margin used. It rises only when the bot
// earns more for the margin it ties up.
type MarginReturn struct {
	Known  bool
	Reason string // why it is not known; empty when Known

	NetPnL    float64 // coin, the change of the total over the window
	Equity    float64 // coin, margin balance at the window start
	MeanIMPct float64 // time-weighted mean IM ÷ margin balance, %
	MeanIM    float64 // coin, MeanIMPct × Equity

	ReturnPct     float64 // NetPnL ÷ MeanIM, in %
	Return30dPct  float64 // ReturnPct × 30 ÷ days the net covers (a 30-day rate, not a forecast)
	SpanHours     float64 // time the net covers: the 30-day rate's base, downtime included
	MeasuredHours float64 // time covered by points carrying im_pct (the mean IM's base)
}

// ReturnOnMargin computes the return on margin over [from, to] (inclusive,
// like Summarize). Each point carrying im_pct stands for the time until the
// next point, at most history.MaxSampleGap and never past to — the weighting
// of history.Utilisation — so a gap with no point is not filled in. Net P&L
// is Summarize's: the total's change from the first point to the last, so the
// 30-day rate is scaled by that span (to the end of the last point's sample),
// downtime included — like the replay's whole window — not by the measured
// hours. equity ≤ 0 means unknown.
func ReturnOnMargin(points []history.Point, equity float64, from, to time.Time) MarginReturn {
	if to.Before(from) {
		return MarginReturn{Equity: equity, Reason: "empty window"}
	}
	in := window(points, from, to)
	if len(in) == 0 {
		return MarginReturn{Equity: equity, Reason: "no P&L history point in the window"}
	}
	net := in[len(in)-1].Total - in[0].Total

	// sampleEnd is when point i stops standing for the book.
	sampleEnd := func(i int) time.Time {
		end := in[i].T.Add(history.MaxSampleGap)
		if i+1 < len(in) && in[i+1].T.Before(end) {
			end = in[i+1].T
		}
		if end.After(to) {
			end = to
		}
		return end
	}

	var measured time.Duration
	var weighted float64 // Σ im_pct × seconds
	for i, p := range in {
		if p.IMPct == nil {
			continue
		}
		d := sampleEnd(i).Sub(p.T)
		if d <= 0 {
			continue
		}
		measured += d
		weighted += *p.IMPct * d.Seconds()
	}
	if measured <= 0 {
		return MarginReturn{NetPnL: net, Equity: equity, Reason: "no point carries im_pct"}
	}
	span := sampleEnd(len(in) - 1).Sub(in[0].T)
	r := ReturnOnMarginOf(net, weighted/measured.Seconds(), equity, span.Hours())
	r.MeasuredHours = measured.Hours()
	return r
}

// ReturnOnMarginOf is the return on margin from its parts: net P&L (coin),
// mean IM in % of equity, equity (coin) and the hours the net covers (both
// SpanHours and MeasuredHours). The replay uses it with its own net, average
// IM and window.
func ReturnOnMarginOf(net, meanIMPct, equity, hours float64) MarginReturn {
	r := MarginReturn{NetPnL: net, Equity: equity, MeanIMPct: meanIMPct, SpanHours: hours, MeasuredHours: hours}
	switch {
	case equity <= 0:
		r.Reason = "margin balance at the window start unknown"
	case meanIMPct <= 0:
		r.Reason = "mean IM used is zero"
	case hours <= 0:
		r.Reason = "no time measured"
	default:
		r.Known = true
		r.MeanIM = meanIMPct / 100 * equity
		r.ReturnPct = pct(net, r.MeanIM)
		r.Return30dPct = r.ReturnPct * 30 / (hours / 24)
	}
	return r
}
