package tests

import (
	"os"
	"regexp"
	"strings"
	"testing"
	"time"
)

// cmd/monitor-demo -steady (#896): the phone check compares the Details trade
// log with a committed baseline, and a random roll put wider rows with real
// instrument names on top of it 30–60 s into a run. With -steady the trade
// log keeps its seeded demo-call / demo-put rows; -tick runs the simulation
// fast so a few seconds cover hundreds of ticks.

// demoRolledTrades returns the instruments of the demo's trades that were
// not seeded (a roll's close and re-open name a real-looking instrument).
func demoRolledTrades(get func(string, any)) []string {
	var body struct {
		Trades []struct {
			Instrument string `json:"instrument"`
		} `json:"trades"`
	}
	get("/api/trades?limit=5000", &body)
	var rolled []string
	for _, tr := range body.Trades {
		if !strings.HasPrefix(tr.Instrument, "demo-") {
			rolled = append(rolled, tr.Instrument)
		}
	}
	return rolled
}

// Scenario: the demo started -steady with a 5 ms tick runs ~300 ticks (about
// 12 rolls without the flag) and its trade log still holds only the seeded rows.
func TestMonitorDemo_SteadyKeepsTheSeededTradeLog(t *testing.T) {
	get := demoAPI(t, "-steady", "-tick", "5ms")
	time.Sleep(1500 * time.Millisecond)
	if rolled := demoRolledTrades(get); len(rolled) > 0 {
		t.Fatalf("-steady demo rolled legs into the trade log: %v", rolled)
	}
	// Orders sent still come at random, so the Activity feed moves.
	var events struct {
		Events []struct {
			Event string `json:"event"`
		} `json:"events"`
	}
	get("/api/events", &events)
	if len(events.Events) == 0 {
		t.Fatal("-steady demo journaled no events: the Activity feed would be empty")
	}
}

// Scenario: without -steady the same fast demo rolls a leg within a few
// seconds — the control that shows the check above can see a roll.
func TestMonitorDemo_WithoutSteadyRollsLegs(t *testing.T) {
	get := demoAPI(t, "-tick", "5ms")
	for deadline := time.Now().Add(10 * time.Second); ; {
		if len(demoRolledTrades(get)) > 0 {
			return
		}
		if time.Now().After(deadline) {
			t.Fatal("demo without -steady rolled no leg in 10 s of 5 ms ticks")
		}
		time.Sleep(100 * time.Millisecond)
	}
}

// Scenario: the phone check starts both demo bots with -steady, so a baseline
// recorded on one run holds on the next.
func TestMonitorDemo_PhoneCheckRunsTheDemoSteady(t *testing.T) {
	src, err := os.ReadFile("../frontend/e2e/playwright.config.mjs")
	if err != nil {
		t.Fatal(err)
	}
	cmd := regexp.MustCompile("go run ./cmd/monitor-demo[^`]*").Find(src)
	if cmd == nil {
		t.Fatal("playwright.config.mjs no longer starts cmd/monitor-demo")
	}
	if !strings.Contains(string(cmd), "-steady") {
		t.Fatalf("the phone check starts the demo without -steady: %s", cmd)
	}
}
