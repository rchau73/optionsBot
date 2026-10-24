package evidence

import (
	"fmt"
	"strings"
	"time"

	"optionsbot/internal/history"
)

// ResidualLabel names what the bridge's residual stands for: every change
// of the account's equity that the bot's own P&L does not explain.
const ResidualLabel = "not explained by the bot: transfers, positions the bot does not manage, funding, rounding"

// residualLabelUSD adds what moves a USD equity under cross collateral.
const residualLabelUSD = ResidualLabel + ", the USD value of the collateral as prices move"

// Bridge explains the week's equity change (#277): equity at the first and
// last P&L history point that carries it, the bot's net P&L between those
// two points in the same unit, and the residual the bot does not explain.
// Every figure is Deribit's equity as recorded on the history; nothing is
// recomputed or filled in.
type Bridge struct {
	Known       bool       `json:"known"`
	Note        string     `json:"note,omitempty"` // why it is not known
	Source      string     `json:"source"`
	Unit        string     `json:"unit,omitempty"` // USD under cross collateral, else the coin
	Start       *time.Time `json:"start,omitempty"`
	End         *time.Time `json:"end,omitempty"`
	EquityStart float64    `json:"equity_start"`
	EquityEnd   float64    `json:"equity_end"`
	Change      float64    `json:"change"`  // EquityEnd − EquityStart
	BotNet      float64    `json:"bot_net"` // the bot's net P&L over [Start, End], in Unit
	Residual    float64    `json:"residual"`
	ResidualPct float64    `json:"residual_pct"` // of EquityStart
	Label       string     `json:"residual_label,omitempty"`
}

// BuildBridge is the pure bridge over the points in [from, to). In a coin
// unit the bot's net P&L is the change of the cumulative total; in USD each
// step of the total is valued at its own point's spot (the last known spot
// when a point has none).
func BuildBridge(points []history.Point, from, to time.Time, source string) Bridge {
	b := Bridge{Source: source + " (equity and P&L recorded on each line)"}
	var in []history.Point
	first, last := -1, -1
	for _, p := range points {
		if p.T.Before(from) || !p.T.Before(to) {
			continue
		}
		if p.Equity != nil {
			if first < 0 {
				first = len(in)
			}
			last = len(in)
		}
		in = append(in, p)
	}
	switch {
	case first < 0:
		b.Note = "no equity recorded on the P&L history in the week (lines written before #277 carry none)"
		return b
	case first == last:
		b.Note = "only one P&L history point in the week carries equity"
		return b
	}
	a, z := in[first], in[last]
	if a.Unit != z.Unit {
		b.Note = fmt.Sprintf("the equity unit changed in the week (%s → %s): cross collateral was switched", a.Unit, z.Unit)
		return b
	}
	b.Unit = a.Unit
	if strings.EqualFold(b.Unit, "USD") {
		net, ok := netUSD(in[first : last+1])
		if !ok {
			b.Note = "no spot on the P&L history to value the bot's coin P&L in the equity's unit"
			return b
		}
		b.BotNet, b.Label = net, residualLabelUSD
	} else {
		b.BotNet, b.Label = z.Total-a.Total, ResidualLabel
	}
	b.Known = true
	b.Start, b.End = &a.T, &z.T
	b.EquityStart, b.EquityEnd = *a.Equity, *z.Equity
	b.Change = b.EquityEnd - b.EquityStart
	b.Residual = b.Change - b.BotNet
	if b.EquityStart > 0 {
		b.ResidualPct = b.Residual / b.EquityStart * 100
	}
	return b
}

// writeBridge writes the "Equity bridge" section of the Markdown report.
func writeBridge(p func(string, ...any), b Bridge) {
	p("## Equity bridge\n\nSource: %s.\n\n", b.Source)
	if !b.Known {
		p("Not available: %s.\n\n", b.Note)
		return
	}
	amt := func(v float64) string { return fmt.Sprintf("%+.4f %s", v, b.Unit) }
	if strings.EqualFold(b.Unit, "USD") {
		amt = func(v float64) string { return fmt.Sprintf("%+.0f USD", v) }
	}
	p("- %s → %s UTC: equity %s → %s, change **%s**\n", b.Start.UTC().Format("Jan 02 15:04"), b.End.UTC().Format("Jan 02 15:04"),
		strings.TrimPrefix(amt(b.EquityStart), "+"), strings.TrimPrefix(amt(b.EquityEnd), "+"), amt(b.Change))
	p("- the bot's net P&L (realised + unrealised, net of fees): %s\n", amt(b.BotNet))
	p("- **%s = %+.2f %% of the starting equity** — %s\n\n", amt(b.Residual), b.ResidualPct, b.Label)
}

// netUSD is Σ (total_i − total_{i−1}) × spot_i over the points: each step of
// the bot's coin P&L valued when it happened (a point without a spot takes
// the last one before it, or the span's first). False with no spot at all.
func netUSD(in []history.Point) (float64, bool) {
	spot := 0.0
	for _, p := range in {
		if p.Spot > 0 {
			spot = p.Spot
			break
		}
	}
	if spot <= 0 {
		return 0, false
	}
	net := 0.0
	for i := 1; i < len(in); i++ {
		if in[i].Spot > 0 {
			spot = in[i].Spot
		}
		net += (in[i].Total - in[i-1].Total) * spot
	}
	return net, true
}
