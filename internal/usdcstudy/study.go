// Package usdcstudy measures the liquidity of the recorded option books for
// the SOL/XRP decision (#117, #356): for each underlying with recorded books
// (BTC and ETH coin-settled, BTC_USDC, ETH_USDC, SOL_USDC, XRP_USDC) and each
// slot band, the spread, how often the book is one-sided or empty, the open
// interest behind a 0.16 Δ strangle, its premium at the bid and the size the
// bot's liquidity cap would allow. Missing hours are reported, never filled.
// Research only: it reads recorded files, never trading state, never Deribit,
// and reports counts, percentages and sizes.
package usdcstudy

import (
	"math"
	"sort"
	"strings"
	"time"

	"optionsbot/internal/config"
	"optionsbot/internal/coverage"
	"optionsbot/internal/marketdata"
	"optionsbot/internal/recorder"
)

// Band is a slot band: expiries with MinDTE ≤ DTE < MaxDTE (DTE as the bot
// counts it, marketdata.DaysToExpiry) and legs with MinAbsDelta ≤ |Δ| ≤
// MaxAbsDelta.
type Band struct {
	Name           string
	MinDTE, MaxDTE int
}

// Bands are the two slot bands of the study (the bot's slots sit in them).
var Bands = []Band{
	{Name: "18-40 DTE", MinDTE: 18, MaxDTE: 40},
	{Name: "40-60 DTE", MinDTE: 40, MaxDTE: 61}, // 60 included
}

const (
	MinAbsDelta = 0.10
	MaxAbsDelta = 0.22
	// TargetDelta is the strangle the open interest, premium and cap are
	// read on: the call and the put nearest it on one expiry.
	TargetDelta = 0.16
	// VolatileShare is the share of hours, by the largest spot move, whose
	// spreads are reported apart.
	VolatileShare = 0.10
	// maxMoveGap: a spot move is measured between snapshots at most this far
	// apart (an hour plus jitter); across a gap there is no hourly move.
	maxMoveGap = 90 * time.Minute
)

// ContractSize is the step Deribit sizes each book's options in (its minimum
// trade amount, in the coin): the liquidity cap is floored to it.
var ContractSize = map[string]float64{
	"BTC": 0.1, "ETH": 1,
	"BTC_USDC": 0.01, "ETH_USDC": 0.1,
	"SOL_USDC": 10, "XRP_USDC": 1000,
}

// Book is one underlying's recorded snapshots (recorder.ReadDir).
type Book struct {
	Underlying string // BTC, BTC_USDC, SOL_USDC…
	Path       string
	Snapshots  []recorder.Snapshot
}

// Report is the study of every book over [From, To).
type Report struct {
	From     time.Time    `json:"from"`
	To       time.Time    `json:"to"` // excluded
	Books    []BookReport `json:"books"`
	NotFound []string     `json:"not_found"` // dataset folders absent from the inputs
}

// BookReport is one underlying.
type BookReport struct {
	Underlying string `json:"underlying"`
	Path       string `json:"path"`
	Settlement string `json:"settlement"` // coin | USDC
	// Snapshots in the window and its coverage (hourly bins).
	Snapshots       int      `json:"snapshots"`
	MissingHours    float64  `json:"missing_hours"`
	MissingDays     []string `json:"missing_days"`
	LongestGapHours float64  `json:"longest_gap_hours"`
	// The window's largest hourly spot move, signed, and how many hours
	// count as volatile (the top VolatileShare of moves, at least one) with
	// the smallest move among them.
	LargestMove       *Move        `json:"largest_move"`
	VolatileHours     int          `json:"volatile_hours"`
	VolatileFromPct   *float64     `json:"volatile_from_pct"`
	ContractSize      float64      `json:"contract_size"`
	LiquidityCapOIPct float64      `json:"liquidity_cap_oi_pct"`
	Bands             []BandReport `json:"bands"`
}

// Move is a spot move between two consecutive hourly snapshots.
type Move struct {
	At  time.Time `json:"at"`  // the later snapshot
	Pct float64   `json:"pct"` // signed
}

// Stats is a median and a 90th percentile over N values (nil when N is 0).
type Stats struct {
	N      int      `json:"n"`
	Median *float64 `json:"median"`
	P90    *float64 `json:"p90"`
}

// BandReport is one slot band of one book.
type BandReport struct {
	Band string `json:"band"`
	// Quote-hours in the band (an option counted once per snapshot) and the
	// share with an empty side (no bid, no ask or neither).
	Quotes      int      `json:"quotes"`
	OneSidedPct *float64 `json:"one_sided_pct"`
	// Spread as % of the mid, two-sided quotes only: every hour and the
	// volatile hours.
	SpreadAll      Stats `json:"spread_all_pct"`
	SpreadVolatile Stats `json:"spread_volatile_pct"`
	// Hours with a 0.16 Δ strangle in the band (a call and a put on one
	// expiry), and its medians: open interest per leg (coin), premium at the
	// bid as % of spot (hours with both bids), and the liquidity cap's size
	// (10 % of the smaller leg's open interest, floored to ContractSize) in
	// the coin and in USD.
	StrangleHours int      `json:"strangle_hours"`
	OICall        *float64 `json:"oi_call_median"`
	OIPut         *float64 `json:"oi_put_median"`
	PremiumHours  int      `json:"premium_hours"`
	PremiumBidPct *float64 `json:"premium_bid_pct_of_spot_median"`
	CapCoin       *float64 `json:"cap_coin_median"`
	CapUSD        *float64 `json:"cap_usd_median"`
	CapZeroHours  int      `json:"cap_zero_hours"` // hours the cap allowed less than one contract
}

// Build studies books over [from, to). Pure.
func Build(books []Book, from, to time.Time, notFound []string) Report {
	r := Report{From: from.UTC(), To: to.UTC(), Books: []BookReport{}, NotFound: notFound}
	if r.NotFound == nil {
		r.NotFound = []string{}
	}
	for _, b := range books {
		r.Books = append(r.Books, buildBook(b, from, to))
	}
	return r
}

func buildBook(b Book, from, to time.Time) BookReport {
	var snaps []recorder.Snapshot
	for _, s := range b.Snapshots {
		if !s.At.Before(from) && s.At.Before(to) {
			snaps = append(snaps, s)
		}
	}
	sort.SliceStable(snaps, func(i, j int) bool { return snaps[i].At.Before(snaps[j].At) })

	times := make([]time.Time, len(snaps))
	for i, s := range snaps {
		times[i] = s.At
	}
	cov := coverage.Coverage(times, from, to, time.Duration(config.FixedMarketRecordMinutes)*time.Minute)
	out := BookReport{
		Underlying: b.Underlying, Path: b.Path, Settlement: settlement(b.Underlying),
		Snapshots: len(snaps), MissingHours: cov.MissingHours, MissingDays: cov.MissingDays,
		LongestGapHours: cov.LongestGapHours, ContractSize: ContractSize[b.Underlying],
		LiquidityCapOIPct: config.FixedLiquidityCapOIPct,
	}

	moves := hourlyMoves(snaps)
	volatile := map[time.Time]bool{}
	if len(moves) > 0 {
		sorted := append([]Move{}, moves...)
		sort.SliceStable(sorted, func(i, j int) bool { return math.Abs(sorted[i].Pct) > math.Abs(sorted[j].Pct) })
		largest := sorted[0]
		out.LargestMove = &largest
		n := max(1, int(math.Ceil(VolatileShare*float64(len(sorted)))))
		for _, m := range sorted[:n] {
			volatile[m.At] = true
		}
		out.VolatileHours = n
		out.VolatileFromPct = ptr(math.Abs(sorted[n-1].Pct))
	}

	for _, band := range Bands {
		out.Bands = append(out.Bands, buildBand(band, snaps, volatile, out.Settlement, out.ContractSize))
	}
	return out
}

func buildBand(band Band, snaps []recorder.Snapshot, volatile map[time.Time]bool, settle string, size float64) BandReport {
	r := BandReport{Band: band.Name}
	var oneSided int
	var spreadAll, spreadVol, oiCall, oiPut, prem, capCoin, capUSD []float64
	for _, s := range snaps {
		spot := s.Spot()
		for _, q := range s.Quotes {
			if !inBand(band, q, s.At) || math.Abs(q.Delta) < MinAbsDelta || math.Abs(q.Delta) > MaxAbsDelta {
				continue
			}
			r.Quotes++
			if q.Bid <= 0 || q.Ask <= 0 {
				oneSided++
				continue
			}
			sp := 100 * (q.Ask - q.Bid) / ((q.Ask + q.Bid) / 2)
			spreadAll = append(spreadAll, sp)
			if volatile[s.At] {
				spreadVol = append(spreadVol, sp)
			}
		}

		call, put, ok := strangle(band, s)
		if !ok {
			continue
		}
		r.StrangleHours++
		oiCall = append(oiCall, call.OpenInterest)
		oiPut = append(oiPut, put.OpenInterest)
		if call.Bid > 0 && put.Bid > 0 && spot > 0 {
			r.PremiumHours++
			p := 100 * (call.Bid + put.Bid) // coin-settled: prices are fractions of the coin
			if settle == "USDC" {
				p /= spot
			}
			prem = append(prem, p)
		}
		c := capSize(math.Min(call.OpenInterest, put.OpenInterest), size)
		if c <= 0 {
			r.CapZeroHours++
		}
		capCoin = append(capCoin, c)
		capUSD = append(capUSD, c*spot)
	}
	if r.Quotes > 0 {
		r.OneSidedPct = ptr(100 * float64(oneSided) / float64(r.Quotes))
	}
	r.SpreadAll, r.SpreadVolatile = stats(spreadAll), stats(spreadVol)
	r.OICall, r.OIPut = median(oiCall), median(oiPut)
	r.PremiumBidPct = median(prem)
	r.CapCoin, r.CapUSD = median(capCoin), median(capUSD)
	return r
}

// strangle is the snapshot's 0.16 Δ strangle in the band: on the band's
// expiry nearest its middle DTE that lists both types, the call and the put
// whose |Δ| is nearest TargetDelta, each within [MinAbsDelta, MaxAbsDelta].
func strangle(band Band, s recorder.Snapshot) (call, put recorder.Quote, ok bool) {
	type pair struct {
		call, put  recorder.Quote
		hasC, hasP bool
	}
	byExpiry := map[time.Time]*pair{}
	for _, q := range s.Quotes {
		ad := math.Abs(q.Delta)
		if !inBand(band, q, s.At) || ad < MinAbsDelta || ad > MaxAbsDelta {
			continue
		}
		p := byExpiry[q.Expiry]
		if p == nil {
			p = &pair{}
			byExpiry[q.Expiry] = p
		}
		switch {
		case q.OptionType == "call" && (!p.hasC || nearer(q, p.call)):
			p.call, p.hasC = q, true
		case q.OptionType == "put" && (!p.hasP || nearer(q, p.put)):
			p.put, p.hasP = q, true
		}
	}
	mid := float64(band.MinDTE+band.MaxDTE-1) / 2
	best, bestDist := time.Time{}, math.Inf(1)
	for exp, p := range byExpiry {
		if !p.hasC || !p.hasP {
			continue
		}
		d := math.Abs(float64(marketdata.DaysToExpiry(exp, s.At)) - mid)
		if d < bestDist || (d == bestDist && exp.Before(best)) {
			best, bestDist = exp, d
		}
	}
	if best.IsZero() {
		return recorder.Quote{}, recorder.Quote{}, false
	}
	return byExpiry[best].call, byExpiry[best].put, true
}

func nearer(a, b recorder.Quote) bool {
	da, db := math.Abs(math.Abs(a.Delta)-TargetDelta), math.Abs(math.Abs(b.Delta)-TargetDelta)
	return da < db || (da == db && a.Strike < b.Strike)
}

func inBand(band Band, q recorder.Quote, at time.Time) bool {
	dte := marketdata.DaysToExpiry(q.Expiry, at)
	return dte >= band.MinDTE && dte < band.MaxDTE
}

// capSize is the liquidity cap on open interest oi (coin): LiquidityCapOIPct
// % of it, floored to the contract step (unknown step: not floored).
func capSize(oi, step float64) float64 {
	c := oi * config.FixedLiquidityCapOIPct / 100
	if step <= 0 {
		return c
	}
	return math.Floor(c/step+1e-9) * step
}

// hourlyMoves are the spot moves between consecutive snapshots at most
// maxMoveGap apart, in %.
func hourlyMoves(snaps []recorder.Snapshot) []Move {
	var out []Move
	for i := 1; i < len(snaps); i++ {
		a, b := snaps[i-1], snaps[i]
		sa, sb := a.Spot(), b.Spot()
		if sa <= 0 || sb <= 0 || b.At.Sub(a.At) > maxMoveGap {
			continue
		}
		out = append(out, Move{At: b.At, Pct: 100 * (sb/sa - 1)})
	}
	return out
}

func settlement(underlying string) string {
	if strings.HasSuffix(underlying, "_USDC") {
		return "USDC"
	}
	return "coin"
}

func stats(v []float64) Stats {
	return Stats{N: len(v), Median: median(v), P90: percentile(v, 0.90)}
}

func median(v []float64) *float64 {
	if len(v) == 0 {
		return nil
	}
	s := append([]float64{}, v...)
	sort.Float64s(s)
	n := len(s)
	if n%2 == 1 {
		return ptr(s[n/2])
	}
	return ptr((s[n/2-1] + s[n/2]) / 2)
}

// percentile is the nearest-rank percentile p (0–1).
func percentile(v []float64, p float64) *float64 {
	if len(v) == 0 {
		return nil
	}
	s := append([]float64{}, v...)
	sort.Float64s(s)
	i := max(0, int(math.Ceil(p*float64(len(s))))-1)
	return ptr(s[i])
}

func ptr(v float64) *float64 { return &v }
