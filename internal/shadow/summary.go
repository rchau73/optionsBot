package shadow

import (
	"fmt"
	"sort"
	"time"
)

// Summary is GET /api/shadow-legs: the study's three metrics (#676) from the
// file's lines. Closed legs' P&L is what the record's close lines say; open
// legs' is at their last written mark, and stays apart.
type Summary struct {
	Since        *time.Time    `json:"since"` // the first line; null with none
	Lines        int           `json:"lines"`
	Skipped      int           `json:"skipped_lines"`
	Opened       int           `json:"opened"`
	OpenNow      int           `json:"open_now"`
	Closed       int           `json:"closed"`
	NoLeg        int           `json:"no_leg"`       // held legs with no strike to follow
	CoveragePct  *float64      `json:"coverage_pct"` // legs followed ÷ legs held; null with none
	Result       Result        `json:"result"`       // closed legs
	ByRule       []RuleSummary `json:"by_rule"`
	Weeks        []WeekSummary `json:"weeks"`
	Open         []Leg         `json:"open"`
	OpenPnLCoin  float64       `json:"open_pnl_coin"`
	ExtraPerpNow float64       `json:"extra_perp_now"` // coin: what the open legs would add to the perpetual
}

// Result sums closed legs.
type Result struct {
	PremiumCoin  float64 `json:"premium_coin"`
	PnLCoin      float64 `json:"pnl_coin"` // sold at the bid, before the perpetual
	PnLUSD       float64 `json:"pnl_usd"`
	PerpPnLCoin  float64 `json:"perp_pnl_coin"`
	PerpFeesCoin float64 `json:"perp_fees_coin"`
	NetPnLCoin   float64 `json:"net_pnl_coin"` // with the perpetual it adds
}

func (r *Result) add(l Leg) {
	r.PremiumCoin += l.Premium * l.Qty
	r.PnLCoin += l.PnLCoin
	r.PnLUSD += l.PnLUSD
	r.PerpPnLCoin += l.PerpPnLCoin
	r.PerpFeesCoin += l.PerpFeesCoin
	r.NetPnLCoin += l.NetPnLCoin
}

// RuleSummary is one holding rule's legs, by the rule that opened them.
type RuleSummary struct {
	Rule     string         `json:"rule"`
	Opened   int            `json:"opened"`
	Closed   int            `json:"closed"`
	ByReason map[string]int `json:"by_reason"`
	Result   Result         `json:"result"`
}

// WeekSummary counts the legs opened in one ISO week, by rule.
type WeekSummary struct {
	Week   string         `json:"week"` // 2026-W45
	Opened map[string]int `json:"opened"`
}

// Summarize builds the summary. recs are in file order; skipped is what Read
// could not parse. A pure function of the lines.
func Summarize(recs []Record, skipped int) Summary {
	out := Summary{Lines: len(recs), Skipped: skipped, ByRule: []RuleSummary{}, Weeks: []WeekSummary{}, Open: []Leg{}}
	rules := map[string]*RuleSummary{}
	weeks := map[string]map[string]int{}
	open := map[string]Leg{}
	unfollowed := map[string]bool{}
	rule := func(name string) *RuleSummary {
		if rules[name] == nil {
			rules[name] = &RuleSummary{Rule: name, ByReason: map[string]int{}}
		}
		return rules[name]
	}
	for i, r := range recs {
		if i == 0 {
			at := r.At
			out.Since = &at
		}
		switch r.Event {
		case EventOpen:
			out.Opened++
			rule(r.Rule).Opened++
			open[r.Key] = r.Leg
			delete(unfollowed, r.Key)
			y, w := r.At.UTC().ISOWeek()
			week := fmt.Sprintf("%d-W%02d", y, w)
			if weeks[week] == nil {
				weeks[week] = map[string]int{}
			}
			weeks[week][r.Rule]++
		case EventUpdate:
			open[r.Key] = r.Leg
		case EventClose:
			out.Closed++
			delete(open, r.Key)
			rs := rule(r.Rule)
			rs.Closed++
			rs.ByReason[r.ExitReason]++
			rs.Result.add(r.Leg)
			out.Result.add(r.Leg)
		case EventNoLeg:
			unfollowed[r.Key] = true
		}
	}
	out.NoLeg = len(unfollowed)
	if held := out.Opened + out.NoLeg; held > 0 {
		pct := float64(out.Opened) / float64(held) * 100
		out.CoveragePct = &pct
	}
	for _, l := range open {
		out.Open = append(out.Open, l)
		out.OpenPnLCoin += l.PnLCoin
		out.ExtraPerpNow += l.PerpDelta
	}
	out.OpenNow = len(out.Open)
	sort.Slice(out.Open, func(i, j int) bool { return out.Open[i].Key < out.Open[j].Key })
	for _, rs := range rules {
		out.ByRule = append(out.ByRule, *rs)
	}
	sort.Slice(out.ByRule, func(i, j int) bool { return out.ByRule[i].Rule < out.ByRule[j].Rule })
	for w, by := range weeks {
		out.Weeks = append(out.Weeks, WeekSummary{Week: w, Opened: by})
	}
	sort.Slice(out.Weeks, func(i, j int) bool { return out.Weeks[i].Week < out.Weeks[j].Week })
	return out
}
