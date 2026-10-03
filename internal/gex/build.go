package gex

import (
	"fmt"
	"sort"
	"strings"
	"time"

	"optionsbot/internal/marketdata"
)

// GEX methods: how the gamma flip and regime are derived from the profile.
const (
	// MethodScript reproduces GestaoCarteira's deribit_tc_export_v3.py: the
	// nearest expiries of the whole chain, strikes around each expiry's own
	// underlying price, regime = sign of the summed weighted GEX, flip = the
	// lowest zero crossing. No hysteresis.
	MethodScript = "script"
	// MethodNearestFlip is the bot's original rule: expiries with open
	// interest and IV, strikes around the latest underlying price, flip = the
	// crossing nearest spot, regime = spot vs flip, with hysteresis.
	MethodNearestFlip = "nearest_flip"
)

// SummaryRow is one instrument of public/get_book_summary_by_currency.
type SummaryRow struct {
	InstrumentName  string  `json:"instrument_name"`
	OpenInterest    float64 `json:"open_interest"`
	UnderlyingPrice float64 `json:"underlying_price"` // that expiry's future
	MarkIV          float64 `json:"mark_iv"`          // percent, e.g. 45.2
}

// Params configure Build.
type Params struct {
	Underlying     string
	NExpiries      int     // nearest expiries consolidated (default 5)
	StrikeRangePct float64 // keep strikes within ±this of spot; 0 = all
	Method         string  // MethodScript (default) or MethodNearestFlip
}

// BuildStats says what went into a snapshot, for logging.
type BuildStats struct {
	Expiries         []time.Time
	InstrumentsTotal int
	InstrumentsUsed  int // after the strike filter
	Unparsed         int
}

type parsedRow struct {
	SummaryRow
	expiry  time.Time
	strike  float64
	optType string
}

// Build computes the GEX snapshot from a book summary at time now. It is
// pure (no I/O, no clock), so the same input always gives the same answer.
func Build(rows []SummaryRow, now time.Time, p Params) (Snapshot, BuildStats, error) {
	if p.NExpiries <= 0 {
		p.NExpiries = 5
	}
	if p.Method == "" {
		p.Method = MethodScript
	}
	var st BuildStats

	var parsed []parsedRow
	latestSpot := 0.0 // last row with a price, in API order (nearest_flip)
	for _, r := range rows {
		und, exp, strike, typ, err := marketdata.ParseOptionName(r.InstrumentName)
		if err != nil || !strings.EqualFold(und, p.Underlying) {
			st.Unparsed++
			continue
		}
		if r.UnderlyingPrice > 0 {
			latestSpot = r.UnderlyingPrice
		}
		parsed = append(parsed, parsedRow{SummaryRow: r, expiry: exp.Add(marketdata.SettlementHourUTC), strike: strike, optType: typ})
	}
	// The script sorts the chain by expiry, strike, type ("call" < "put").
	sort.SliceStable(parsed, func(i, j int) bool {
		a, b := parsed[i], parsed[j]
		if !a.expiry.Equal(b.expiry) {
			return a.expiry.Before(b.expiry)
		}
		if a.strike != b.strike {
			return a.strike < b.strike
		}
		return a.optType < b.optType
	})

	script := p.Method == MethodScript
	byExpiry := map[time.Time][]parsedRow{}
	var expiries []time.Time
	for _, r := range parsed {
		if !script && (r.OpenInterest <= 0 || r.MarkIV <= 0) {
			continue
		}
		if _, ok := byExpiry[r.expiry]; !ok {
			expiries = append(expiries, r.expiry)
		}
		byExpiry[r.expiry] = append(byExpiry[r.expiry], r)
	}
	if len(expiries) > p.NExpiries {
		expiries = expiries[:p.NExpiries]
	}
	if len(expiries) == 0 {
		return Snapshot{}, st, fmt.Errorf("no %s options with data", p.Underlying)
	}
	st.Expiries = expiries

	profiles := make([]ExpiryProfile, 0, len(expiries))
	snapSpot := latestSpot
	for _, exp := range expiries {
		rs := byExpiry[exp]
		spot := latestSpot
		if script {
			spot = firstPrice(rs) // the script filters around each expiry's own future
			snapSpot = spot       // and reports the last expiry's
		}
		insts := make([]InstrumentGEXInput, 0, len(rs))
		for _, r := range rs {
			rowSpot := r.UnderlyingPrice
			if !script && rowSpot <= 0 {
				rowSpot = latestSpot
			}
			insts = append(insts, InstrumentGEXInput{
				Instrument: r.InstrumentName, Strike: r.strike, Expiry: exp, OptionType: r.optType,
				Spot: rowSpot, OpenInterest: r.OpenInterest, MarkIV: r.MarkIV / 100,
			})
		}
		st.InstrumentsTotal += len(insts)
		insts = FilterByStrikeRange(insts, spot, p.StrikeRangePct)
		st.InstrumentsUsed += len(insts)

		strikes := ComputeExpiryGEXAt(insts, now)
		totalOI := 0.0
		for _, s := range strikes {
			totalOI += s.CallOI + s.PutOI
		}
		profiles = append(profiles, ExpiryProfile{
			Expiry: exp, Label: exp.Format("2006-01-02"), TotalOI: totalOI,
			TimeWeight: ExpiryWeight(exp), Strikes: strikes,
		})
	}

	snap := BuildSnapshotWith(ConsolidateProfiles(profiles), snapSpot, p.Method)
	snap.ComputedAt = now
	return snap, st, nil
}

func firstPrice(rs []parsedRow) float64 {
	for _, r := range rs {
		if r.UnderlyingPrice > 0 {
			return r.UnderlyingPrice
		}
	}
	return 0
}
