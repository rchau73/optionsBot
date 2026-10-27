package tests

import (
	"strings"
	"testing"
)

// cmd/monitor-demo -scenario good|mixed|drawdown (#203): demo data with
// losing months and a drawdown, served through the real API code.

type demoSummary struct {
	Holding struct {
		Nearest *struct {
			PutStrike  *float64 `json:"put_strike"`
			CallStrike *float64 `json:"call_strike"`
		} `json:"nearest"`
	} `json:"holding"`
	Income struct {
		Months []struct {
			Month        string  `json:"month"`
			RealisedCoin float64 `json:"realised_coin"`
			RealisedUSD  float64 `json:"realised_usd"`
			Closes       int     `json:"closes"`
		} `json:"months"`
		ThisMonth struct {
			RealisedCoin float64 `json:"realised_coin"`
		} `json:"this_month"`
	} `json:"income"`
	WorstCase struct {
		Available bool `json:"available"`
		Down      struct {
			LossCoin float64 `json:"loss_coin"`
		} `json:"down"`
		Up struct {
			LossCoin float64 `json:"loss_coin"`
		} `json:"up"`
	} `json:"worst_case"`
}

type demoStatus struct {
	Environment string `json:"environment"`
	Deployment  string `json:"deployment"`
	Version     string `json:"version"`
}

// Scenario: a losing month is served.
func TestMonitorDemo_MixedServesALosingMonth(t *testing.T) {
	get := demoAPI(t, "-scenario", "mixed")
	var s demoSummary
	get("/api/summary", &s)
	losing := 0
	for _, m := range s.Income.Months {
		if m.Closes > 0 && m.RealisedCoin < 0 && m.RealisedUSD < 0 {
			losing++
		}
	}
	if losing == 0 {
		t.Fatalf("no month with negative net income in coin and USD: %+v", s.Income.Months)
	}
	assertDemoMarker(t, get, "mixed")
}

// Scenario: drawdown — the worst case exceeds the latest month's income and
// an open leg is within 20 % of its stop mark.
func TestMonitorDemo_Drawdown(t *testing.T) {
	get := demoAPI(t, "-scenario", "drawdown")
	var s demoSummary
	get("/api/summary", &s)
	worst := max(s.WorstCase.Down.LossCoin, s.WorstCase.Up.LossCoin)
	if !s.WorstCase.Available || worst <= s.Income.ThisMonth.RealisedCoin {
		t.Errorf("worst case %.5f coin does not exceed this month's income %.5f", worst, s.Income.ThisMonth.RealisedCoin)
	}
	var positions struct {
		Strangles []struct {
			Legs []struct {
				Instrument   string  `json:"instrument"`
				Mark         float64 `json:"mark"`
				StopLossMark float64 `json:"stop_loss_mark"`
			} `json:"legs"`
		} `json:"strangles"`
	}
	get("/api/positions", &positions)
	near := false
	for _, st := range positions.Strangles {
		for _, l := range st.Legs {
			if l.StopLossMark > 0 && l.Mark < l.StopLossMark && l.Mark >= 0.8*l.StopLossMark {
				near = true
			}
		}
	}
	if !near {
		t.Errorf("no open leg within 20 %% of its stop mark: %+v", positions.Strangles)
	}
	assertDemoMarker(t, get, "drawdown")
}

// Scenario: good — steady income, no losing month; and clearly simulated.
func TestMonitorDemo_GoodIsSteadyAndMarked(t *testing.T) {
	get := demoAPI(t, "-scenario", "good")
	var s demoSummary
	get("/api/summary", &s)
	for _, m := range s.Income.Months {
		if m.Closes > 0 && m.RealisedCoin <= 0 {
			t.Errorf("month %s lost %.5f coin in the good scenario", m.Month, m.RealisedCoin)
		}
	}
	// The range bar's strikes (#320) come with the demo's nearest strangle.
	if n := s.Holding.Nearest; n == nil || n.PutStrike == nil || n.CallStrike == nil || *n.PutStrike >= *n.CallStrike {
		t.Errorf("nearest strangle %+v", n)
	}
	assertDemoMarker(t, get, "good")
}

// Scenario: default unchanged — no deployment marker added, still the demo.
func TestMonitorDemo_DefaultUnchanged(t *testing.T) {
	get := demoAPI(t)
	var st demoStatus
	get("/api/status", &st)
	if st.Deployment != "" || st.Version != "" || !strings.Contains(st.Environment, "demo") {
		t.Errorf("default demo status %+v", st)
	}
}

// Scenario: clearly simulated — /api/status reports the demo deployment.
func assertDemoMarker(t *testing.T, get func(string, any), scenario string) {
	t.Helper()
	var st demoStatus
	get("/api/status", &st)
	if st.Deployment != "demo" || st.Version != "scenario "+scenario || !strings.Contains(st.Environment, "demo") {
		t.Errorf("status %+v: want deployment demo, version %q, a demo environment", st, "scenario "+scenario)
	}
}
