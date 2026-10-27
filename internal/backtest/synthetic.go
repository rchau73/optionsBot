package backtest

import (
	"encoding/csv"
	"fmt"
	"io"
	"math"
	"math/rand"
	"strconv"
	"time"
)

// Listing calendars of the synthetic data (SyntheticOptions.Calendar).
const (
	// CalendarMonthly: the first 4 monthlies at ≥ 21 DTE (the original data set).
	CalendarMonthly = "monthly"
	// CalendarDeribit: DeribitListedExpiries — weeklies, monthlies and
	// quarterlies, each listed until it expires (#83).
	CalendarDeribit = "deribit"
)

// SyntheticOptions drives WriteSynthetic.
type SyntheticOptions struct {
	From, To time.Time // first and last day (weekdays only are written)
	Calendar string    // CalendarMonthly (default) | CalendarDeribit
	Seed     int64     // 0 = 42, the original seed
}

// WriteSynthetic writes Black–Scholes priced BTC options in the
// HistoricalFeed CSV format (cmd/gendata): a lognormal spot path at 70 %
// annual vol from 95,000, DVOL a random walk in [30, 120], strikes every
// 5,000 around spot. Synthetic: it validates mechanics, not profitability.
func WriteSynthetic(out io.Writer, opt SyntheticOptions) (int, error) {
	switch opt.Calendar {
	case "":
		opt.Calendar = CalendarMonthly
	case CalendarMonthly, CalendarDeribit:
	default:
		return 0, fmt.Errorf("calendar %q: want %s or %s", opt.Calendar, CalendarMonthly, CalendarDeribit)
	}
	if opt.To.Before(opt.From) {
		return 0, fmt.Errorf("to %s before from %s", opt.To.Format("2006-01-02"), opt.From.Format("2006-01-02"))
	}
	if opt.Seed == 0 {
		opt.Seed = 42
	}
	// The deribit calendar lists expiries up to ~9 months out: a wider strike
	// grid keeps a 0.16-delta strike on the long ones.
	strikeSteps := 4
	if opt.Calendar == CalendarDeribit {
		strikeSteps = 10
	}
	rng := rand.New(rand.NewSource(opt.Seed))

	var monthlies []time.Time // midnight, as the original data set
	for y := opt.From.Year() - 1; y <= opt.To.Year()+1; y++ {
		for m := time.January; m <= time.December; m++ {
			e := LastFriday(y, m)
			monthlies = append(monthlies, time.Date(e.Year(), e.Month(), e.Day(), 0, 0, 0, 0, time.UTC))
		}
	}

	w := csv.NewWriter(out)
	_ = w.Write([]string{"date", "instrument", "underlying", "underlying_price", "strike", "expiry", "option_type", "bid", "ask", "mid", "delta", "gamma", "theta", "vega", "iv", "dvol_index"})

	const annualVol = 0.70
	const r = 0.05
	dailyVol := annualVol / math.Sqrt(252)
	S := 95000.0
	dvol := 70.0
	rows := 0

	for td := opt.From; !td.After(opt.To); td = td.AddDate(0, 0, 1) {
		if td.Weekday() == time.Saturday || td.Weekday() == time.Sunday {
			continue
		}
		S *= math.Exp((0-0.5*annualVol*annualVol)/252 + dailyVol*rng.NormFloat64())
		if S < 10000 {
			S = 10000
		}
		dvol = math.Min(math.Max(dvol+rng.NormFloat64()*1.5, 30), 120)
		sigma := dvol / 100.0

		baseStrike := math.Round(S/5000) * 5000
		for _, exp := range syntheticExpiries(opt.Calendar, td, monthlies) {
			T := exp.Sub(td).Hours() / 24 / 365
			expLabel := exp.Format("02Jan06") // the original data set's names, e.g. 27Feb26
			for i := -strikeSteps; i <= strikeSteps; i++ {
				K := baseStrike + float64(i)*5000
				if K <= 0 {
					continue
				}
				for _, optType := range []string{"call", "put"} {
					g := bsGreeks(S, K, T, r, sigma, optType)
					if g.price < 1.0 {
						continue
					}
					spread := math.Max(g.price*0.015, 5.0)
					bid := math.Max(g.price-spread/2, 0)
					ask := g.price + spread/2

					cp := "C"
					if optType == "put" {
						cp = "P"
					}
					_ = w.Write([]string{
						td.Format("2006-01-02"),
						fmt.Sprintf("BTC-%s-%d-%s", expLabel, int(K), cp),
						"BTC",
						strconv.FormatFloat(round2(S), 'f', 2, 64),
						strconv.Itoa(int(K)),
						exp.Format("2006-01-02"),
						optType,
						strconv.FormatFloat(round4(bid), 'f', 4, 64),
						strconv.FormatFloat(round4(ask), 'f', 4, 64),
						strconv.FormatFloat(round4(g.price), 'f', 4, 64),
						strconv.FormatFloat(round6(g.delta), 'f', 6, 64),
						strconv.FormatFloat(round6(g.gamma), 'f', 8, 64),
						strconv.FormatFloat(round6(g.theta), 'f', 6, 64),
						strconv.FormatFloat(round6(g.vega), 'f', 6, 64),
						strconv.FormatFloat(round4(g.iv), 'f', 4, 64),
						strconv.FormatFloat(round2(dvol), 'f', 2, 64),
					})
					rows++
				}
			}
		}
	}

	// csv.Writer errors are sticky: any failed Write above surfaces here.
	w.Flush()
	if err := w.Error(); err != nil {
		return rows, fmt.Errorf("write csv: %w", err)
	}
	return rows, nil
}

// syntheticExpiries is what the chain lists on td: CalendarMonthly the first 4
// monthlies at ≥ 21 DTE, CalendarDeribit DeribitListedExpiries (held legs
// keep a price down to the roll).
func syntheticExpiries(calendar string, td time.Time, monthlies []time.Time) []time.Time {
	if calendar == CalendarDeribit {
		return DeribitListedExpiries(td)
	}
	var out []time.Time
	for _, e := range monthlies {
		if int(e.Sub(td).Hours()/24) >= 21 {
			out = append(out, e)
			if len(out) == 4 {
				break
			}
		}
	}
	return out
}

type bsResult struct {
	price, delta, gamma, theta, vega, iv float64
}

func bsGreeks(S, K, T, r, sigma float64, optType string) bsResult {
	if T <= 0 || sigma <= 0 {
		return bsResult{}
	}
	d1 := (math.Log(S/K) + (r+0.5*sigma*sigma)*T) / (sigma * math.Sqrt(T))
	d2 := d1 - sigma*math.Sqrt(T)

	var price, delta float64
	if optType == "call" {
		price = S*normCDF(d1) - K*math.Exp(-r*T)*normCDF(d2)
		delta = normCDF(d1)
	} else {
		price = K*math.Exp(-r*T)*normCDF(-d2) - S*normCDF(-d1)
		delta = normCDF(d1) - 1
	}
	gamma := normPDF(d1) / (S * sigma * math.Sqrt(T))
	nd2 := d2
	if optType == "put" {
		nd2 = -d2
	}
	theta := (-(S*normPDF(d1)*sigma)/(2*math.Sqrt(T)) - r*K*math.Exp(-r*T)*normCDF(nd2)) / 365
	vega := S * normPDF(d1) * math.Sqrt(T) / 100
	return bsResult{math.Max(price, 0), delta, gamma, theta, vega, sigma}
}

func normCDF(x float64) float64 { return 0.5 * (1 + math.Erf(x/math.Sqrt2)) }
func normPDF(x float64) float64 { return math.Exp(-0.5*x*x) / math.Sqrt(2*math.Pi) }

func round2(v float64) float64 { return math.Round(v*100) / 100 }
func round4(v float64) float64 { return math.Round(v*10000) / 10000 }
func round6(v float64) float64 { return math.Round(v*1000000) / 1000000 }
