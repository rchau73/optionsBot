package tests

import (
	"fmt"
	"math"
	"strings"
	"sync"
	"testing"
	"time"

	"optionsbot/internal/replay"
)

// The one-sided hedge study's rallies (#812, #813): the recordings since
// 10-07 only fall, so three synthetic rallies replay the hedge modes on a
// calls-only book in a bear trend under a negative regime (shedBooks, its
// call alone) — the case a calls-only book is at risk in. Paths are hourly
// from 10-07; trading starts on 10-10 (hour 72) at 100,000:
//   - grind: +1 % a day for 17 days (≈ +18 %);
//   - squeeze: +15 % in 48 hours, then flat;
//   - reversal: +15 % in 48 hours, back to 100,000 in the next 48, then flat.
// The history before is a 40-day fall of 0.6 % a day. MODEL: one flat 50 % IV, the regime and flip held as recorded. `go test
// ./tests -run TestReplayHedgeModeRallies -v` prints each table and when
// each mode first traded the perpetual and first sold a put (the trend's flip
// to bull, as the regime side then sells puts).

const rallyStart = 72 // hour trading starts (10-10 00:00)

func rallyPaths() map[string]func(h int) float64 {
	up := math.Log(1.15)
	return map[string]func(h int) float64{
		"grind": func(h int) float64 {
			return 100000 * math.Pow(1.01, math.Max(0, float64(h-rallyStart))/24)
		},
		"squeeze": func(h int) float64 {
			return 100000 * math.Exp(up*math.Min(1, math.Max(0, float64(h-rallyStart))/48))
		},
		"reversal": func(h int) float64 {
			x := float64(h - rallyStart)
			switch {
			case x <= 0:
				return 100000
			case x <= 48:
				return 100000 * math.Exp(up*x/48)
			case x <= 96:
				return 100000 * math.Exp(up*(96-x)/48)
			}
			return 100000
		},
	}
}

func TestReplayHedgeModeRallies(t *testing.T) {
	t.Parallel()
	for _, name := range []string{"grind", "squeeze", "reversal"} {
		path := rallyPaths()[name]
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			snaps, set := shedBooks(t, 20, path)
			set.Book = set.Book[:1] // the call alone
			// A clear fall before the recording (0.6 % a day for 40 days): the
			// bear trend holds until the rally breaks it, as the live books'
			// weeks of calls only. shedBooks' own history turns neutral within
			// hours.
			for i := range set.Closes {
				d := len(set.Closes) - i
				set.Closes[i].Close = 100000 * (1 + 0.006*float64(d))
				if d == 20 { // the swing high the trend reads
					set.Closes[i].Close += 5000
				}
			}
			var mu sync.Mutex
			firstPerp, firstPut := map[string]time.Time{}, map[string]time.Time{}
			set.Trace = func(at time.Time, line string) {
				mode, _, _ := strings.Cut(line, " ")
				mu.Lock()
				defer mu.Unlock()
				if strings.Contains(line, "perp ") && firstPerp[mode].IsZero() {
					firstPerp[mode] = at
				}
				if strings.Contains(line, "-P") && (strings.Contains(line, "entry") || strings.Contains(line, "repair")) && firstPut[mode].IsZero() {
					firstPut[mode] = at
				}
			}
			rows, err := replay.HedgeModeStudy(snaps, set, hedgeModes(t, "off,bot,onesided,trend"), []float64{1})
			if err != nil {
				t.Fatal(err)
			}
			if len(rows) != 4 {
				t.Fatalf("rows = %d, want 4 modes", len(rows))
			}
			if bot := hedgeRow(t, rows, "bot", 1); bot.HedgeTrades == 0 {
				t.Errorf("bot: no perp order in a %s; want the calls' short delta hedged", name)
			}
			var b strings.Builder
			fmt.Fprintf(&b, "%s: spot %.0f at the start, %.0f at hour %d, %.0f at the end\n", name, path(rallyStart), path(rallyStart+48), rallyStart+48, path(20*24-1))
			for _, m := range []string{"off", "bot", "onesided", "trend"} {
				fmt.Fprintf(&b, "  %-9s first perp %-16s first put %s\n", m, stamp(firstPerp[m]), stamp(firstPut[m]))
			}
			b.WriteString(replay.HedgeModeMarkdown("BTC", rows, nil, set.Cfg.MaxMMPct))
			t.Log("\n" + b.String())
		})
	}
}

func stamp(at time.Time) string {
	if at.IsZero() {
		return "never"
	}
	return at.UTC().Format("Jan 02 15:04")
}
