package tests

import (
	"errors"
	"math"
	"os/exec"
	"strings"
	"testing"
	"time"

	"optionsbot/internal/api"
	"optionsbot/internal/orders"
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

// cmd/monitor-demo -scenario hedged (#827): the default book with the futures
// hedge on, served through the real API code.

type hedgedMove struct {
	Spot        float64  `json:"spot"`
	LossCoin    float64  `json:"loss_coin"`
	LossUSD     float64  `json:"loss_usd"`
	PctOfEquity *float64 `json:"pct_of_equity"`
}

type hedgedSummary struct {
	Holding struct {
		Strangles int `json:"strangles"`
		Legs      int `json:"legs"`
	} `json:"holding"`
	WorstCase struct {
		Available bool       `json:"available"`
		Model     string     `json:"model"`
		Down      hedgedMove `json:"down"`
		Up        hedgedMove `json:"up"`
		Hedge     *struct {
			SizeUSD     float64    `json:"size_usd"`
			AvgPrice    float64    `json:"avg_price"`
			OptionsDown hedgedMove `json:"options_down"`
			OptionsUp   hedgedMove `json:"options_up"`
		} `json:"hedge"`
	} `json:"worst_case"`
	LastAction *struct {
		Kind       string  `json:"kind"`
		Instrument string  `json:"instrument"`
		Rule       string  `json:"rule"`
		Reason     string  `json:"reason"`
		Side       string  `json:"side"`
		SizeUSD    float64 `json:"size_usd"`
	} `json:"last_action"`
}

// Scenario: the hedged demo starts and says what it is.
func TestMonitorDemo_HedgedStartsAndSaysWhatItIs(t *testing.T) {
	get := demoAPI(t, "-scenario", "hedged")
	assertDemoMarker(t, get, "hedged")
	var h struct {
		Hedge struct {
			Enabled    bool    `json:"enabled"`
			Testnet    bool    `json:"testnet"`
			Instrument string  `json:"instrument"`
			PerpUSD    float64 `json:"perp_usd"`
			PerpAvg    float64 `json:"perp_avg_price"`
		} `json:"hedge"`
		Switchable bool `json:"switchable"`
	}
	get("/api/hedge", &h)
	if !h.Hedge.Enabled || !h.Hedge.Testnet || h.Hedge.Instrument != "BTC-PERPETUAL" || h.Hedge.PerpUSD == 0 || h.Hedge.PerpAvg <= 0 || h.Switchable {
		t.Errorf("/api/hedge %+v: want the hedge on, testnet, a BTC-PERPETUAL position, not switchable", h)
	}
	var s hedgedSummary
	get("/api/summary", &s)
	if s.WorstCase.Hedge == nil || s.WorstCase.Hedge.SizeUSD == 0 {
		t.Fatalf("/api/summary has no perpetual in its worst case: %+v", s.WorstCase)
	}
	// The perpetual is no fourth strangle.
	if s.Holding.Strangles != 3 || s.Holding.Legs != 6 {
		t.Errorf("holding %+v, want the default demo's 3 strangles and 6 legs", s.Holding)
	}
}

// Scenario: an unknown scenario is refused.
func TestMonitorDemo_UnknownScenarioIsRefused(t *testing.T) {
	out, err := exec.Command(buildDemo(t), "-scenario", "nothing").CombinedOutput()
	var exit *exec.ExitError
	if !errors.As(err, &exit) || exit.ExitCode() != 2 {
		t.Fatalf("-scenario nothing: err %v, want exit status 2\n%s", err, out)
	}
	for _, name := range []string{"good", "mixed", "drawdown", "hedged"} {
		if !strings.Contains(string(out), name) {
			t.Errorf("the error %q does not list %s", out, name)
		}
	}
}

// Scenario: the worst case includes the hedge — read a few times while the
// demo runs (spot and the perpetual move).
func TestMonitorDemo_HedgedWorstCaseIncludesTheHedge(t *testing.T) {
	get := demoAPI(t, "-scenario", "hedged")
	for i := 0; i < 4; i++ {
		if i > 0 {
			time.Sleep(2 * time.Second)
		}
		var s hedgedSummary
		get("/api/summary", &s)
		wc := s.WorstCase
		if !wc.Available || wc.Hedge == nil || !strings.Contains(wc.Model, "perpetual") {
			t.Fatalf("read %d: worst case %+v: want one with the perpetual, labelled", i, wc)
		}
		h := wc.Hedge
		t.Logf("read %d: perpetual %.0f USD at %.0f; down %.5f (options %.5f), up %.5f (options %.5f) BTC",
			i, h.SizeUSD, h.AvgPrice, wc.Down.LossCoin, h.OptionsDown.LossCoin, wc.Up.LossCoin, h.OptionsUp.LossCoin)
		for _, m := range []struct {
			name          string
			hedged, alone hedgedMove
		}{{"down", wc.Down, h.OptionsDown}, {"up", wc.Up, h.OptionsUp}} {
			if m.hedged.LossCoin <= 0 || m.hedged.LossUSD <= 0 || m.hedged.PctOfEquity == nil || *m.hedged.PctOfEquity <= 0 {
				t.Errorf("read %d %s: %+v, want a loss in coin, USD and %% of equity", i, m.name, m.hedged)
			}
			// The figure is the options alone plus the hedge's share at the moved price.
			share := h.SizeUSD * (1/h.AvgPrice - 1/m.hedged.Spot)
			if want := m.alone.LossCoin - share; math.Abs(m.hedged.LossCoin-want) > 1e-9 {
				t.Errorf("read %d %s: %.9f BTC, want the options' %.9f less the perpetual's %.9f", i, m.name, m.hedged.LossCoin, m.alone.LossCoin, share)
			}
			if want := m.alone.LossUSD - share*m.hedged.Spot; math.Abs(m.hedged.LossUSD-want) > 1e-6 {
				t.Errorf("read %d %s: %.6f USD, want %.6f", i, m.name, m.hedged.LossUSD, want)
			}
		}
		// The key's note: the hedged figure is smaller than the options alone.
		if hedged, alone := max(wc.Down.LossCoin, wc.Up.LossCoin), max(h.OptionsDown.LossCoin, h.OptionsUp.LossCoin); hedged >= alone {
			t.Errorf("read %d: the larger hedged loss %.5f BTC is not smaller than the options alone %.5f BTC", i, hedged, alone)
		}
	}
}

// Scenario: the Last action can be a hedge trade — the hedge sells the
// perpetual at start, after the last option trade.
func TestMonitorDemo_HedgedLastActionIsAHedgeTrade(t *testing.T) {
	get := demoAPI(t, "-scenario", "hedged")
	want, _ := api.ExplainHedge(orders.HedgeFill)
	for deadline := time.Now().Add(30 * time.Second); ; time.Sleep(250 * time.Millisecond) {
		var s hedgedSummary
		get("/api/summary", &s)
		if a := s.LastAction; a != nil && a.Kind == "hedge" {
			if a.Instrument != "BTC-PERPETUAL" || a.Rule != orders.HedgeFill || a.Reason != want || (a.Side != "sell" && a.Side != "buy") || a.SizeUSD <= 0 {
				t.Errorf("hedge last action %+v: want a BTC-PERPETUAL hedge_fill, sold or bought, with the hedge sentence", a)
			}
			return
		}
		if time.Now().After(deadline) {
			t.Fatalf("no hedge trade as the last action within 30 s: %+v", s.LastAction)
		}
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
