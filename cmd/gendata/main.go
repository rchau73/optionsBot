package main

import (
	"encoding/csv"
	"fmt"
	"math"
	"math/rand"
	"os"
	"strconv"
	"time"
)

func normCDF(x float64) float64 {
	return 0.5 * (1 + math.Erf(x/math.Sqrt2))
}

func normPDF(x float64) float64 {
	return math.Exp(-0.5*x*x) / math.Sqrt(2*math.Pi)
}

type greeks struct {
	price, delta, gamma, theta, vega, iv float64
}

func bsGreeks(S, K, T, r, sigma float64, optType string) greeks {
	if T <= 0 || sigma <= 0 {
		return greeks{}
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

	return greeks{math.Max(price, 0), delta, gamma, theta, vega, sigma}
}

func lastFriday(year, month int) time.Time {
	// First day of next month, then go back to last Friday
	var next time.Time
	if month == 12 {
		next = time.Date(year+1, 1, 1, 0, 0, 0, 0, time.UTC)
	} else {
		next = time.Date(year, time.Month(month+1), 1, 0, 0, 0, 0, time.UTC)
	}
	d := next.AddDate(0, 0, -1)
	for d.Weekday() != time.Friday {
		d = d.AddDate(0, 0, -1)
	}
	return d
}

func round2(v float64) float64 { return math.Round(v*100) / 100 }
func round4(v float64) float64 { return math.Round(v*10000) / 10000 }
func round6(v float64) float64 { return math.Round(v*1000000) / 1000000 }

func main() {
	rng := rand.New(rand.NewSource(42))

	// Build expiry list: last Friday of each month for 2025-2027
	var expiries []time.Time
	for y := 2025; y <= 2027; y++ {
		for m := 1; m <= 12; m++ {
			expiries = append(expiries, lastFriday(y, m))
		}
	}

	// Trading days 2026-01-01 to 2026-12-31
	start := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	end := time.Date(2026, 12, 31, 0, 0, 0, 0, time.UTC)
	var tradingDays []time.Time
	for d := start; !d.After(end); d = d.AddDate(0, 0, 1) {
		if d.Weekday() != time.Saturday && d.Weekday() != time.Sunday {
			tradingDays = append(tradingDays, d)
		}
	}

	if err := os.MkdirAll("data/historical", 0o755); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	f, err := os.Create("data/historical/options.csv")
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	defer f.Close()

	w := csv.NewWriter(f)
	_ = w.Write([]string{"date", "instrument", "underlying", "underlying_price", "strike", "expiry", "option_type", "bid", "ask", "mid", "delta", "gamma", "theta", "vega", "iv", "dvol_index"})

	const annualVol = 0.70
	const r = 0.05
	dailyVol := annualVol / math.Sqrt(252)
	S := 95000.0
	dvol := 70.0
	rows := 0

	for _, td := range tradingDays {
		S *= math.Exp((0-0.5*annualVol*annualVol)/252 + dailyVol*rng.NormFloat64())
		if S < 10000 {
			S = 10000
		}
		dvol += rng.NormFloat64() * 1.5
		if dvol < 30 {
			dvol = 30
		}
		if dvol > 120 {
			dvol = 120
		}
		sigma := dvol / 100.0

		// Pick up to 4 expiries with >= 21 DTE
		var activeExp []time.Time
		for _, e := range expiries {
			if int(e.Sub(td).Hours()/24) >= 21 {
				activeExp = append(activeExp, e)
				if len(activeExp) == 4 {
					break
				}
			}
		}

		baseStrike := math.Round(S/5000) * 5000
		for _, exp := range activeExp {
			T := exp.Sub(td).Hours() / 24 / 365
			expStr := exp.Format("02Jan06")
			// Uppercase month
			expLabel := fmt.Sprintf("%s%s%s", expStr[:2], uppercaseMonth(expStr[2:5]), expStr[5:])

			for i := -4; i <= 4; i++ {
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
					instrument := fmt.Sprintf("BTC-%s-%d-%s", expLabel, int(K), cp)

					_ = w.Write([]string{
						td.Format("2006-01-02"),
						instrument,
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

	w.Flush()
	fmt.Printf("wrote %d rows to data/historical/options.csv\n", rows)
}

func uppercaseMonth(s string) string {
	if len(s) == 0 {
		return s
	}
	b := []byte(s)
	if b[0] >= 'a' && b[0] <= 'z' {
		b[0] -= 32
	}
	return string(b)
}
