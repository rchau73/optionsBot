package deployeffect

import (
	"bufio"
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"sort"
	"strings"
	"time"

	"optionsbot/internal/orders"
)

// rawLine holds the journal fields the report reads; orders.OrderLog and
// orders.RiskRecord lines both decode into it.
type rawLine struct {
	Timestamp       time.Time       `json:"timestamp"`
	Event           string          `json:"event"`
	Instrument      string          `json:"instrument"`
	Slot            *orders.SlotRef `json:"slot"`
	OrderID         string          `json:"order_id"`
	TriggerReason   string          `json:"trigger_reason"`
	Detail          string          `json:"detail"`
	Change          string          `json:"change"`
	Qty             float64         `json:"qty"`
	Delta           float64         `json:"delta"`
	FillPrice       float64         `json:"fill_price"`
	PremiumReceived float64         `json:"premium_received"`
	Market          struct {
		Mid float64 `json:"mid"`
	} `json:"market"`
}

// wanted are the journal events the report uses; the rest (P&L ticks,
// skips, trend changes) are not decoded.
var wanted = map[string]bool{
	orders.EventSubmitted: true, orders.EventAmended: true, orders.EventCancelled: true,
	orders.EventFilled: true, orders.EventClosed: true, orders.EventReconciled: true, orders.EventRisk: true,
}

// ReadJournal reads the lines the report needs from a journal (orders.log),
// ordered by time. Unreadable lines (a torn write) are counted and skipped.
func ReadJournal(r io.Reader) (lines []Line, skipped int, err error) {
	br := bufio.NewReaderSize(r, 1<<20)
	for {
		raw, rerr := br.ReadBytes('\n')
		raw = bytes.TrimSpace(raw)
		if len(raw) > 0 {
			if l, ok, bad := decode(raw); bad {
				skipped++
			} else if ok {
				lines = append(lines, l)
			}
		}
		if errors.Is(rerr, io.EOF) {
			break
		}
		if rerr != nil {
			return lines, skipped, fmt.Errorf("read journal: %w", rerr)
		}
	}
	sort.SliceStable(lines, func(i, j int) bool { return lines[i].At.Before(lines[j].At) })
	return lines, skipped, nil
}

func decode(raw []byte) (l Line, ok, bad bool) {
	// Most lines are P&L ticks: check the event before decoding everything.
	if bytes.Contains(raw, []byte(`"event":"pnl"`)) {
		return Line{}, false, false
	}
	var head struct {
		Event string `json:"event"`
	}
	if err := json.Unmarshal(raw, &head); err != nil {
		return Line{}, false, true
	}
	if !wanted[head.Event] {
		return Line{}, false, false
	}
	var r rawLine
	if err := json.Unmarshal(raw, &r); err != nil || r.Timestamp.IsZero() {
		return Line{}, false, true
	}
	return Line{
		At: r.Timestamp, Event: r.Event, Instrument: r.Instrument, Slot: r.Slot, OrderID: r.OrderID,
		Trigger: r.TriggerReason, Detail: r.Detail, Change: r.Change, Qty: r.Qty, Delta: r.Delta,
		Mid: r.Market.Mid, FillPrice: r.FillPrice, PremiumReceived: r.PremiumReceived,
	}, true, false
}

// ReadJournalFile reads the journal at path.
func ReadJournalFile(path string) ([]Line, int, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, 0, fmt.Errorf("open journal %s: %w", path, err)
	}
	defer f.Close()
	return ReadJournal(f)
}

// ReadDeploys reads deploys.tsv (env_record: "<RFC 3339 time>\t<env>\t<commit>
// [\trollback]"), keeping the lines of env (all when env is empty).
// Unreadable lines are counted and skipped.
func ReadDeploys(r io.Reader, env string) (deploys []Deploy, skipped int, err error) {
	sc := bufio.NewScanner(r)
	for sc.Scan() {
		text := strings.TrimSpace(sc.Text())
		if text == "" {
			continue
		}
		f := strings.Split(text, "\t")
		if len(f) < 3 {
			skipped++
			continue
		}
		at, perr := time.Parse(time.RFC3339, f[0])
		if perr != nil {
			skipped++
			continue
		}
		if env != "" && f[1] != env {
			continue
		}
		deploys = append(deploys, Deploy{At: at, Commit: f[2], Rollback: len(f) > 3 && f[3] == "rollback"})
	}
	if err := sc.Err(); err != nil {
		return deploys, skipped, fmt.Errorf("read deploys: %w", err)
	}
	sort.SliceStable(deploys, func(i, j int) bool { return deploys[i].At.Before(deploys[j].At) })
	return deploys, skipped, nil
}

// ReadDeploysFile reads the deploy record at path.
func ReadDeploysFile(path, env string) ([]Deploy, int, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, 0, fmt.Errorf("open deploys %s: %w", path, err)
	}
	defer f.Close()
	return ReadDeploys(f, env)
}
