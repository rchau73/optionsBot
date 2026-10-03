package strategy

import (
	"fmt"
	"math"
	"sort"
	"time"

	"optionsbot/internal/marketdata"
)

// SelectExpiry returns the nearest expiry whose days-to-expiry, counted from
// now, falls within [max(targetDTE-maxDeviationDays, rolloutDTE+1), targetDTE+maxDeviationDays].
// rolloutDTE is a hard lower bound: an entry at or below it would roll on the
// very next cycle. now is a parameter (not time.Now) so the backtest can pass
// the simulated date. Returns false when no expiry qualifies — normal
// mid-month, not an error.
func SelectExpiry(instruments []*marketdata.Instrument, now time.Time, targetDTE, maxDeviationDays, rolloutDTE int) (time.Time, bool) {
	lo, hi := marketdata.ExpiryWindow(targetDTE, maxDeviationDays, rolloutDTE)
	return marketdata.NearestExpiry(instruments, now, lo, hi, nil)
}

// SelectStrike picks the OTM strike with |delta| closest to targetDelta for the given
// expiry and option type. When maxDeltaSlippage > 0, only candidates within
// [targetDelta - maxDeltaSlippage, targetDelta + maxDeltaSlippage] are eligible;
// if none qualify, an error is returned. When maxDeltaSlippage == 0, the closest
// candidate is returned regardless of distance.
func SelectStrike(instruments []*marketdata.Instrument, expiry time.Time, optType string, targetDelta, maxDeltaSlippage float64) (*marketdata.Instrument, error) {
	var candidates []*marketdata.Instrument
	totalForExpiry, noData, itm := 0, 0, 0
	for _, inst := range instruments {
		if !inst.Expiry.Equal(expiry) || inst.OptionType != optType {
			continue
		}
		totalForExpiry++
		if inst.Mid == 0 {
			noData++
			continue
		}
		// OTM filter: calls have delta < 0.5, puts have delta > -0.5
		absDelta := math.Abs(inst.Greeks.Delta)
		if absDelta > 0.5 {
			itm++
			continue
		}
		candidates = append(candidates, inst)
	}
	if len(candidates) == 0 {
		if totalForExpiry == 0 {
			return nil, fmt.Errorf("no %s instruments found for expiry %s (not in instrument chain)",
				optType, expiry.Format("2006-01-02"))
		}
		return nil, fmt.Errorf("no OTM %s candidates for expiry %s: %d instruments found, %d have no ticker data (Mid=0, not subscribed), %d ITM",
			optType, expiry.Format("2006-01-02"), totalForExpiry, noData, itm)
	}

	sort.Slice(candidates, func(i, j int) bool {
		di := math.Abs(math.Abs(candidates[i].Greeks.Delta) - targetDelta)
		dj := math.Abs(math.Abs(candidates[j].Greeks.Delta) - targetDelta)
		return di < dj
	})

	best := candidates[0]
	if maxDeltaSlippage > 0 {
		bestAbsDelta := math.Abs(best.Greeks.Delta)
		if math.Abs(bestAbsDelta-targetDelta) > maxDeltaSlippage {
			return nil, fmt.Errorf("no %s strike within delta slippage for expiry %s: target=%.4f slippage=%.4f closest=%.4f (%s)",
				optType, expiry.Format("2006-01-02"), targetDelta, maxDeltaSlippage, bestAbsDelta, best.Name)
		}
	}
	return best, nil
}

// SelectExpiryFallback is SelectExpiry restricted to expiries not in
// occupiedExpiries. It is used when the primary expiry already holds a
// strangle at the same delta.
func SelectExpiryFallback(
	instruments []*marketdata.Instrument,
	now time.Time,
	targetDTE, maxDeviationDays, rolloutDTE int,
	occupiedExpiries map[time.Time]bool,
) (time.Time, bool) {
	lo, hi := marketdata.ExpiryWindow(targetDTE, maxDeviationDays, rolloutDTE)
	return marketdata.NearestExpiry(instruments, now, lo, hi, occupiedExpiries)
}

// AvailableExpiries returns sorted unique expiries across all instruments.
func AvailableExpiries(instruments []*marketdata.Instrument) []time.Time {
	seen := map[time.Time]struct{}{}
	for _, inst := range instruments {
		seen[inst.Expiry] = struct{}{}
	}
	out := make([]time.Time, 0, len(seen))
	for exp := range seen {
		out = append(out, exp)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Before(out[j]) })
	return out
}

// NextMonthlyExpiry returns the last trading day of the next calendar month after ref.
func NextMonthlyExpiry(ref time.Time, available []time.Time) (time.Time, bool) {
	// Last trading day of next month: last Friday on or before the last day of the month
	nextMonth := time.Date(ref.Year(), ref.Month()+1, 1, 0, 0, 0, 0, time.UTC)
	lastDay := nextMonth.AddDate(0, 1, -1) // last calendar day of that month
	// Walk back to Friday
	for lastDay.Weekday() != time.Friday {
		lastDay = lastDay.AddDate(0, 0, -1)
	}
	// Find the closest available expiry to lastDay
	best := time.Time{}
	bestDiff := math.MaxFloat64
	for _, exp := range available {
		diff := math.Abs(exp.Sub(lastDay).Hours())
		if diff < bestDiff {
			bestDiff = diff
			best = exp
		}
	}
	return best, !best.IsZero()
}
