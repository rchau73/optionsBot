// Command usdcstudy is the repeatable liquidity report for the SOL/XRP
// decision (#117, #356): for each recorded book (BTC/ETH coin-settled,
// BTC_USDC, ETH_USDC, SOL_USDC, XRP_USDC) and slot band, spread, one-sided
// share, open interest, premium and the size the liquidity cap would allow.
// Research tool: it reads recorded files only — no Deribit call, no trading
// state — and writes counts, percentages and sizes.
//
//	go run ./cmd/usdcstudy -dir data/btc/data -from 2026-10-24 -to 2026-11-07
//	go run ./cmd/usdcstudy -dir data/btc,data/eth -from 2026-10-24 -to 2026-11-07
package main

import (
	"flag"
	"fmt"
	"os"
	"strings"
	"time"

	"optionsbot/internal/usdcstudy"
)

func main() {
	dir := flag.String("dir", "", "a bot's folder (data/btc) or its data directory (data/btc/data); several comma-separated (data/btc,data/eth)")
	from := flag.String("from", "", "first day, YYYY-MM-DD")
	to := flag.String("to", "", "last day, YYYY-MM-DD (included)")
	out := flag.String("out", "data/usdcstudy", "reports folder: <out>/<UTC time>/report.md|json")
	flag.Parse()

	if err := run(*dir, *from, *to, *out); err != nil {
		fmt.Fprintln(os.Stderr, "usdcstudy:", err)
		os.Exit(1)
	}
}

func run(dirList, fromStr, toStr, out string) error {
	if dirList == "" {
		return fmt.Errorf("-dir is required")
	}
	from, err := time.Parse(time.DateOnly, fromStr)
	if err != nil {
		return fmt.Errorf("-from %q: want YYYY-MM-DD", fromStr)
	}
	last, err := time.Parse(time.DateOnly, toStr)
	if err != nil {
		return fmt.Errorf("-to %q: want YYYY-MM-DD", toStr)
	}
	if last.Before(from) {
		return fmt.Errorf("-to %s is before -from %s", toStr, fromStr)
	}
	to := last.AddDate(0, 0, 1) // the day is included
	books, notFound, err := usdcstudy.Read(strings.Split(dirList, ","), from, to)
	if err != nil {
		return err
	}
	if len(books) == 0 {
		return fmt.Errorf("no recorded books under %s", dirList)
	}
	md, js, err := usdcstudy.Write(usdcstudy.Build(books, from, to, notFound), usdcstudy.OutDir(out, time.Now()))
	if err != nil {
		return err
	}
	fmt.Printf("%s, %s\n", md, js)
	return nil
}
