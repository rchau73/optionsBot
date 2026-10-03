package tests

import (
	"context"
	"encoding/json"
	"errors"
	"math"
	"strings"
	"testing"
	"time"

	"optionsbot/internal/config"
	gw "optionsbot/internal/gateway"
	"optionsbot/internal/gex"
	"optionsbot/internal/marketdata"
)

// ── Public-only market-data gateway (mainnet GEX while trading on testnet) ──

func TestGateway_PublicOnlyNeverAuthenticatesOrSendsPrivate(t *testing.T) {
	m := newMockDeribit(t)
	cfg := testGatewayConfig()
	cfg.ClientID, cfg.ClientSecret = "must-not-leak", "must-not-leak"
	g := gw.New(cfg, gw.WithEndpoint(m.url()), gw.PublicOnly(), gw.WithName("mainnet-public"))
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(func() { cancel(); g.Close() })
	if err := g.Connect(ctx); err != nil {
		t.Fatalf("connect: %v", err)
	}

	if _, err := g.Call(ctx, "public/get_book_summary_by_currency", map[string]any{"currency": "BTC"}, gw.PriorityLow); err != nil {
		t.Fatalf("public calls must work: %v", err)
	}
	_, err := g.Call(ctx, "private/get_account_summary", map[string]any{"currency": "BTC"}, gw.PriorityLow)
	if !errors.Is(err, gw.ErrPrivateOnPublic) {
		t.Errorf("private call on a public-only gateway: err = %v", err)
	}
	if n := m.count("public/auth"); n != 0 {
		t.Errorf("a public-only gateway must never authenticate, got %d auth calls", n)
	}
	if n := m.count("private/get_account_summary"); n != 0 {
		t.Error("the refused private call must never reach the exchange")
	}
	m.mu.Lock()
	sent, _ := json.Marshal(m.received)
	m.mu.Unlock()
	if strings.Contains(string(sent), "must-not-leak") {
		t.Fatal("credentials were sent over the public connection")
	}
}

func TestConfig_TradingEndpointVsMarketData(t *testing.T) {
	if (&config.Config{Environment: "testnet"}).WSEndpoint() != config.TestnetWSEndpoint ||
		(&config.Config{Environment: "live"}).WSEndpoint() != config.MainnetWSEndpoint {
		t.Error("trading endpoint follows DERIBIT_ENV")
	}
	if !strings.Contains(config.MainnetWSEndpoint, "www.deribit.com") {
		t.Error("market data must come from mainnet")
	}
}

// ── Option names ─────────────────────────────────────────────────────────────

func TestParseOptionName_OneAndTwoDigitDays(t *testing.T) {
	cases := map[string]struct {
		day    int
		strike float64
		typ    string
	}{
		"BTC-4OCT26-84000-C":  {4, 84000, "call"}, // daily expiries use one digit
		"BTC-27NOV26-74000-P": {27, 74000, "put"},
	}
	for name, want := range cases {
		und, exp, strike, typ, err := marketdata.ParseOptionName(name)
		if err != nil || und != "BTC" || exp.Day() != want.day || exp.Year() != 2026 || strike != want.strike || typ != want.typ {
			t.Errorf("%s → %s %v %v %s %v", name, und, exp, strike, typ, err)
		}
	}
	for _, bad := range []string{"BTC-PERPETUAL", "BTC-4OCT26-X-C", "BTC-4OCT26-84000-Z", "BTC-40OCT26-1-C"} {
		if _, _, _, _, err := marketdata.ParseOptionName(bad); err == nil {
			t.Errorf("%s must not parse", bad)
		}
	}
}

// ── Script vs original method on the same real data ──────────────────────────

func TestGEXBuild_NearestFlipMethodOnRealData(t *testing.T) {
	fx := loadGEXFixture(t)
	now := time.UnixMilli(fx.CapturedAtMS)
	script, _, _ := gex.Build(fx.Summaries, now, gex.Params{Underlying: "BTC", StrikeRangePct: 0.15, Method: gex.MethodScript})
	nearest, st, err := gex.Build(fx.Summaries, now, gex.Params{Underlying: "BTC", StrikeRangePct: 0.15, Method: gex.MethodNearestFlip})
	if err != nil || len(st.Expiries) != 5 {
		t.Fatalf("nearest_flip build: %v, %d expiries", err, len(st.Expiries))
	}
	// Mainnet has several crossings: the script takes the lowest, the original
	// method the one nearest spot. Both agree the market is pinning today.
	if nearest.GammaFlip <= script.GammaFlip || math.Abs(nearest.GammaFlip-nearest.Spot) > math.Abs(script.GammaFlip-nearest.Spot) {
		t.Errorf("nearest flip %.0f should be closer to spot %.0f than the lowest crossing %.0f", nearest.GammaFlip, nearest.Spot, script.GammaFlip)
	}
	if nearest.Regime != "POSITIVE/PINNING" || script.Regime != "POSITIVE/PINNING" {
		t.Errorf("regimes: script %s, nearest_flip %s", script.Regime, nearest.Regime)
	}
}

func TestGEXBuild_UnknownUnderlyingIsAnError(t *testing.T) {
	fx := loadGEXFixture(t)
	if _, _, err := gex.Build(fx.Summaries, time.UnixMilli(fx.CapturedAtMS), gex.Params{Underlying: "ETH"}); err == nil {
		t.Error("a BTC book has no ETH options")
	}
}

func TestConfigLoad_GEXMethod(t *testing.T) {
	cfg, err := loadWithDummyCreds(t, writeTempConfig(t, ""))
	if err != nil || cfg.GEXMethod != gex.MethodScript {
		t.Errorf("default gex_method = %q (%v), want script", cfg.GEXMethod, err)
	}
	if _, err := loadWithDummyCreds(t, writeTempConfig(t, "gex_method: magic\n")); err == nil || !strings.Contains(err.Error(), "gex_method") {
		t.Errorf("an unknown method must be rejected, got %v", err)
	}
}
