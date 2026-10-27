package usdcstudy

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"optionsbot/internal/config"
)

// Markdown writes the report: one table of every book × band, then each
// book's coverage and largest move. Counts, percentages and sizes only.
func Markdown(w io.Writer, r Report) error {
	const ts = "2006-01-02 15:04"
	var b strings.Builder
	fmt.Fprintf(&b, "# USDC-settled liquidity study\n\n")
	fmt.Fprintf(&b, "Window %s → %s UTC (end excluded). Recorded mainnet books, one snapshot an hour; missing hours are listed, never filled. "+
		"Bands: DTE as the bot counts it, legs with %.2f ≤ |Δ| ≤ %.2f; the strangle is the call and put nearest %.2f Δ on the band's expiry nearest its middle.\n\n",
		r.From.Format(ts), r.To.Format(ts), MinAbsDelta, MaxAbsDelta, TargetDelta)

	b.WriteString("| Book | Band | Quotes | One-sided % | Spread % mid: median / p90 (all) | Spread % mid: median / p90 (volatile) | Strangle hours | OI call / put (median) | Premium at bid % spot | Cap size (coin) | Cap size (USD) | Hours cap < 1 contract |\n")
	b.WriteString("|---|---|---:|---:|---|---|---:|---|---:|---:|---:|---:|\n")
	for _, bk := range r.Books {
		for _, bd := range bk.Bands {
			fmt.Fprintf(&b, "| %s | %s | %d | %s | %s / %s | %s / %s | %d | %s / %s | %s | %s | %s | %d |\n",
				bk.Underlying, bd.Band, bd.Quotes, pct(bd.OneSidedPct),
				pct(bd.SpreadAll.Median), pct(bd.SpreadAll.P90), pct(bd.SpreadVolatile.Median), pct(bd.SpreadVolatile.P90),
				bd.StrangleHours, num(bd.OICall), num(bd.OIPut), pct(bd.PremiumBidPct),
				num(bd.CapCoin), usd(bd.CapUSD), bd.CapZeroHours)
		}
	}

	b.WriteString("\n## Coverage and moves\n\n")
	b.WriteString("| Book | Settlement | Path | Snapshots | Missing hours | Missing days | Longest gap | Largest hourly move | Volatile hours (from) | Contract step |\n")
	b.WriteString("|---|---|---|---:|---:|---|---:|---|---|---:|\n")
	for _, bk := range r.Books {
		move := "—"
		if bk.LargestMove != nil {
			move = fmt.Sprintf("%+.2f %% at %s", bk.LargestMove.Pct, bk.LargestMove.At.Format(ts))
		}
		vol := "—"
		if bk.VolatileFromPct != nil {
			vol = fmt.Sprintf("%d (|move| ≥ %.2f %%)", bk.VolatileHours, *bk.VolatileFromPct)
		}
		days := "none"
		if len(bk.MissingDays) > 0 {
			days = strings.Join(bk.MissingDays, ", ")
		}
		fmt.Fprintf(&b, "| %s | %s | %s | %d | %.0f | %s | %.0f h | %s | %s | %s |\n",
			bk.Underlying, bk.Settlement, bk.Path, bk.Snapshots, bk.MissingHours, days, bk.LongestGapHours, move, vol, num(&bk.ContractSize))
	}
	if len(r.NotFound) > 0 {
		fmt.Fprintf(&b, "\nNot recorded (folder absent): %s\n", strings.Join(r.NotFound, ", "))
	}
	fmt.Fprintf(&b, "\nColumns: one-sided %% = quote-hours in the band with no bid, no ask or neither; spread = (ask − bid) ÷ mid, two-sided quotes only; "+
		"volatile = the hours with the largest %.0f %% of hourly spot moves (from the same books); OI in the coin; premium = call bid + put bid as %% of spot; "+
		"cap = %d %% of the smaller leg's open interest floored to the contract step (the bot's liquidity cap), USD at that hour's spot.\n",
		100*VolatileShare, config.FixedLiquidityCapOIPct)
	_, err := io.WriteString(w, b.String())
	return err
}

func pct(v *float64) string {
	if v == nil {
		return "—"
	}
	return strconv.FormatFloat(*v, 'f', 1, 64)
}

func num(v *float64) string {
	if v == nil {
		return "—"
	}
	return strconv.FormatFloat(*v, 'g', 6, 64)
}

func usd(v *float64) string {
	if v == nil {
		return "—"
	}
	return strconv.FormatFloat(*v, 'f', 0, 64)
}

// OutDir is the report folder of a run at now: <out>/<UTC time>.
func OutDir(out string, now time.Time) string {
	return filepath.Join(out, now.UTC().Format("20060102T150405Z"))
}

// Write writes report.md and report.json into dir and returns their paths.
func Write(r Report, dir string) (md, js string, err error) {
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", "", err
	}
	var buf bytes.Buffer
	if err := Markdown(&buf, r); err != nil {
		return "", "", err
	}
	md = filepath.Join(dir, "report.md")
	if err := os.WriteFile(md, buf.Bytes(), 0o644); err != nil {
		return "", "", err
	}
	data, err := json.MarshalIndent(r, "", "  ")
	if err != nil {
		return "", "", err
	}
	js = filepath.Join(dir, "report.json")
	return md, js, os.WriteFile(js, append(data, '\n'), 0o644)
}
