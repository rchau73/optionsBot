package tests

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/joho/godotenv"
	"gopkg.in/yaml.v3"

	"optionsbot/internal/config"
	"optionsbot/internal/hedge"
)

// The SOL and XRP bots on testnet (#837): their shipped configs, their
// services in docker-compose.yml (on their own accounts, only with
// BOT_LINEAR=1) and a SOL entry through Strategy.Run.

// Scenario: a SOL bot opens a strangle on testnet — with the shipped
// config_sol.yaml (slots 30 and 50), 5,000 USDC on its own account and no
// hedge mode file yet (a first start), the vacant 50-DTE slot sells a call and
// a put of whole 10-SOL contracts at USDC prices, and the hedge is on.
func TestLinearBots_SOLBotOpensAStrangleOnTestnet(t *testing.T) {
	shipped, err := loadWithDummyCreds(t, filepath.Join("..", "config_sol.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	// Two slots share 20 % of 5,000 USDC: 500 USDC each; one 10-SOL contract
	// a leg pair takes 333 USDC of IM (imPerUnit is per 0.1 SOL).
	f := newLinearFixture(t, "SOL", 200, 220, 180, wholeStrike, 10, 1.10, 1.30, 500/(1.5*100))
	f.cfg.DTEDeltaMatrix, f.cfg.MinPremiumBTC = shipped.DTEDeltaMatrix, shipped.MinPremiumBTC
	f.cfg.MaxLegSizeMultiple = shipped.MaxLegSizeMultiple
	f.exch.perpSuffix = "_USDC-PERPETUAL"
	f.futures = &fakeFutures{ex: f.exch, bid: 199.9, ask: 200.1, index: 200, contract: 0.1}
	f.hedgeAt = filepath.Join(t.TempDir(), "hedge_mode.json")
	f.startRun()

	eventually(t, 2*time.Second, "both legs submitted", func() bool { return len(f.exch.sells()) == 2 })
	for _, o := range f.exch.sells() {
		if o.Qty < 10 || o.Qty/10 != float64(int(o.Qty/10)) {
			t.Errorf("%s qty = %v, want whole 10-SOL contracts (at least one)", o.Instrument, o.Qty)
		}
		if !strings.HasPrefix(o.Instrument, "SOL_USDC-") || o.LimitPrice < 1.10 || o.LimitPrice > 1.30 {
			t.Errorf("%s at %v, want a SOL_USDC option priced inside its 1.10–1.30 USDC book", o.Instrument, o.LimitPrice)
		}
	}
	if s := f.exch.sells(); len(s) == 2 && s[0].Instrument == s[1].Instrument {
		t.Errorf("a call and a put, got %s twice", s[0].Instrument)
	}
	eventually(t, 2*time.Second, "the hedge is on", func() bool { return f.strat.HedgeState().Enabled })
	st, err := hedge.LoadMode(f.hedgeAt)
	if err != nil || !st.Enabled || !st.Ledger.Real.Linear {
		t.Errorf("the mode file says ON on the linear perpetual: %+v, %v", st, err)
	}
}

// The shipped SOL and XRP configs: slots 30 and 50 DTE at 0.16 Δ, each
// slot's ±10-day window (max_dte_deviation) holding Deribit's month-end
// expiries, the premium floor in USDC, everything else as BTC's.
func TestLinearBots_ShippedConfigs(t *testing.T) {
	btc, err := loadWithDummyCreds(t, filepath.Join("..", "config_btc.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	for coin, floor := range map[string]float64{"SOL": 0.07, "XRP": 0.001} {
		t.Run(coin, func(t *testing.T) {
			cfg, err := loadWithDummyCreds(t, filepath.Join("..", "config_"+strings.ToLower(coin)+".yaml"))
			if err != nil {
				t.Fatal(err)
			}
			if cfg.Underlying != coin || !config.IsLinear(cfg.Underlying) || cfg.PremiumUnit() != "USDC" {
				t.Errorf("underlying %q (linear %v, premium in %s), want %s in USDC", cfg.Underlying, config.IsLinear(cfg.Underlying), cfg.PremiumUnit(), coin)
			}
			slots := cfg.Slots()
			if len(slots) != 2 || slots[0].TargetDTE != 30 || slots[1].TargetDTE != 50 || slots[0].EntryDelta != 0.16 || slots[1].EntryDelta != 0.16 {
				t.Errorf("slots %+v, want 30 and 50 DTE at 0.16", slots)
			}
			if cfg.MaxDTEDeviation != 10 {
				t.Errorf("max_dte_deviation %d: the 20–40 and 40–60 windows assume 10", cfg.MaxDTEDeviation)
			}
			if cfg.MinPremiumBTC != floor {
				t.Errorf("min_premium %v USDC, want %v (~0.1 %% of spot)", cfg.MinPremiumBTC, floor)
			}
			// Every other tuned value is BTC's (the direction source is mvp's sma).
			same := *cfg
			same.Underlying, same.DTEDeltaMatrix, same.MinPremiumBTC, same.GEXDirection = btc.Underlying, btc.DTEDeltaMatrix, btc.MinPremiumBTC, btc.GEXDirection
			if cfg.GEXDirection != "sma" {
				t.Errorf("gex_direction %q, want sma (as mvp's BTC)", cfg.GEXDirection)
			}
			a, b := string(mustJSON(t, btc)), string(mustJSON(t, &same))
			if a != b {
				t.Errorf("config_%s.yaml differs from config_btc.yaml beyond underlying, slots, min_premium and direction:\n btc %s\n %s %s",
					strings.ToLower(coin), a, strings.ToLower(coin), b)
			}
		})
	}
}

type composeFile struct {
	Services map[string]struct {
		Profiles    []string  `yaml:"profiles"`
		Volumes     []string  `yaml:"volumes"`
		Environment yaml.Node `yaml:"environment"`
		DependsOn   []string  `yaml:"depends_on"`
	} `yaml:"services"`
}

// environment is a service's environment as name → raw value (a mapping or a
// "NAME=value" list, the merge key's anchor left out).
func environment(t *testing.T, n yaml.Node) map[string]string {
	t.Helper()
	out := map[string]string{}
	switch n.Kind {
	case yaml.MappingNode:
		for i := 0; i+1 < len(n.Content); i += 2 {
			out[n.Content[i].Value] = n.Content[i+1].Value
		}
	case yaml.SequenceNode:
		for _, c := range n.Content {
			k, v, _ := strings.Cut(c.Value, "=")
			out[k] = v
		}
	}
	return out
}

func readCompose(t *testing.T) composeFile {
	t.Helper()
	raw, err := os.ReadFile(filepath.Join("..", "docker-compose.yml"))
	if err != nil {
		t.Fatal(err)
	}
	var c composeFile
	if err := yaml.Unmarshal(raw, &c); err != nil {
		t.Fatal(err)
	}
	return c
}

// bot-sol and bot-xrp: only in the "linear" profile (BOT_LINEAR=1), their
// own config, data folder and API port, and DERIBIT_CLIENT_ID/SECRET from
// their own SOL_/XRP_ variables; the monitor lists them only through
// MONITOR_LINEAR_APIS and does not wait for them. BTC and ETH are unchanged.
func TestLinearBots_ComposeServices(t *testing.T) {
	c := readCompose(t)
	for coin, port := range map[string]string{"sol": ":8083", "xrp": ":8084"} {
		s, ok := c.Services["bot-"+coin]
		if !ok {
			t.Fatalf("no bot-%s service", coin)
		}
		if len(s.Profiles) != 1 || s.Profiles[0] != "linear" {
			t.Errorf("bot-%s profiles %v, want [linear]: it runs only with BOT_LINEAR=1", coin, s.Profiles)
		}
		vols := strings.Join(s.Volumes, " ")
		if !strings.Contains(vols, "./config_"+coin+".yaml:/app/config.yaml:ro") || !strings.Contains(vols, "./data/"+coin+":/app/run") {
			t.Errorf("bot-%s volumes %v, want its own config and data folder", coin, s.Volumes)
		}
		env := environment(t, s.Environment)
		up := strings.ToUpper(coin)
		if env["BOT_API_ADDR"] != port || env["DERIBIT_CLIENT_ID"] != "${"+up+"_DERIBIT_CLIENT_ID:-}" ||
			env["DERIBIT_CLIENT_SECRET"] != "${"+up+"_DERIBIT_CLIENT_SECRET:-}" {
			t.Errorf("bot-%s environment %v, want API %s and its own %s_DERIBIT_CLIENT_ID/SECRET", coin, env, port, up)
		}
	}
	for _, name := range []string{"bot-btc", "bot-eth"} {
		s := c.Services[name]
		env := environment(t, s.Environment)
		if len(s.Profiles) != 0 || env["DERIBIT_CLIENT_ID"] != "" || env["DERIBIT_CLIENT_SECRET"] != "" {
			t.Errorf("%s changed: profiles %v, credentials overridden %v", name, s.Profiles, env)
		}
	}
	mon := c.Services["monitor"]
	apis := environment(t, mon.Environment)["BOT_APIS"]
	if apis != "btc=http://bot-btc:8081,eth=http://bot-eth:8082${MONITOR_LINEAR_APIS:-}" {
		t.Errorf("monitor BOT_APIS = %q", apis)
	}
	for _, d := range mon.DependsOn {
		if d == "bot-sol" || d == "bot-xrp" {
			t.Errorf("the monitor depends on %s, which runs only in the linear profile", d)
		}
	}
}

// What makes "its own account" hold in the container: compose sets
// DERIBIT_CLIENT_ID from SOL_DERIBIT_CLIENT_ID (empty when missing), and the
// bot's --env-file never overrides a variable already set, so a SOL bot
// without its keys refuses to trade instead of using the BTC/ETH account.
func TestLinearBots_EmptyOwnKeyNeverFallsBackToTheSharedAccount(t *testing.T) {
	envFile := filepath.Join(t.TempDir(), ".env")
	if err := os.WriteFile(envFile, []byte("DERIBIT_CLIENT_ID=btc-eth-account\nDERIBIT_CLIENT_SECRET=btc-eth-secret\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("DERIBIT_CLIENT_ID", "")
	t.Setenv("DERIBIT_CLIENT_SECRET", "")
	if err := godotenv.Load(envFile); err != nil {
		t.Fatal(err)
	}
	cfg, err := config.Load(filepath.Join("..", "config_sol.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	if cfg.ClientID != "" {
		t.Fatalf("client id %q: the shared .env must not fill an empty SOL key", cfg.ClientID)
	}
	if err := cfg.RequireCredentials(); err == nil {
		t.Error("a SOL bot without its own keys must refuse to trade")
	}

	// Its own key, set by compose, wins over the shared .env.
	t.Setenv("DERIBIT_CLIENT_ID", "sol-account")
	t.Setenv("DERIBIT_CLIENT_SECRET", "sol-secret")
	if err := godotenv.Load(envFile); err != nil {
		t.Fatal(err)
	}
	if cfg, err = config.Load(filepath.Join("..", "config_sol.yaml")); err != nil || cfg.ClientID != "sol-account" {
		t.Errorf("client id %q (%v), want sol-account", cfg.ClientID, err)
	}
}
