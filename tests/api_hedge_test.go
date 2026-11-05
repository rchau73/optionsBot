package tests

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"optionsbot/internal/api"
	"optionsbot/internal/hedge"
	"optionsbot/internal/orders"
	"optionsbot/internal/strategy"
)

// GET/POST /api/hedge (#657).

type fakeHedgeController struct {
	enabled   bool
	switches  []bool
	by        []orders.Actor
	switchErr error
	previews  int
}

func (c *fakeHedgeController) HedgeState() strategy.HedgeView {
	return strategy.HedgeView{Available: true, Enabled: c.enabled, Instrument: testPerp, PerpUSD: -17700}
}

func (c *fakeHedgeController) HedgePreview(context.Context) (strategy.HedgePreview, error) {
	c.previews++
	return strategy.HedgePreview{Preview: hedge.Preview{DeltaNow: 0, DeltaAfter: 0.2305}, Warnings: []string{}}, nil
}

func (c *fakeHedgeController) SetFuturesHedge(_ context.Context, enable bool, by orders.Actor) (strategy.HedgeSwitchResult, error) {
	if c.switchErr != nil {
		return strategy.HedgeSwitchResult{}, c.switchErr
	}
	c.switches, c.by = append(c.switches, enable), append(c.by, by)
	c.enabled = enable
	return strategy.HedgeSwitchResult{Enabled: enable, Detail: "ok"}, nil
}

func hedgeServer(c *fakeHedgeController, token string, live bool) http.Handler {
	return api.New(staticView{sampleView()}, orders.NewWriterLogger(io.Discard, 0), api.WithFuturesHedge(c, token, live)).Handler()
}

func hedgeCall(h http.Handler, method, path, token, body string) *httptest.ResponseRecorder {
	req := httptest.NewRequest(method, path, strings.NewReader(body))
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	return rec
}

func TestAPIHedge_StateAndPreview(t *testing.T) {
	c := &fakeHedgeController{enabled: true}
	h := hedgeServer(c, "secret", false)
	rec := hedgeCall(h, http.MethodGet, "/api/hedge", "", "")
	var out struct {
		Hedge      strategy.HedgeView     `json:"hedge"`
		Switchable bool                   `json:"switchable"`
		Preview    *strategy.HedgePreview `json:"preview"`
	}
	if rec.Code != http.StatusOK || json.Unmarshal(rec.Body.Bytes(), &out) != nil || !out.Hedge.Enabled || !out.Switchable || out.Preview != nil {
		t.Fatalf("GET /api/hedge → %d %s", rec.Code, rec.Body)
	}
	if c.previews != 0 {
		t.Error("the plain state reads nothing from the exchange")
	}
	rec = hedgeCall(h, http.MethodGet, "/api/hedge?preview=true", "", "")
	_ = json.Unmarshal(rec.Body.Bytes(), &out)
	if out.Preview == nil || out.Preview.DeltaAfter != 0.2305 {
		t.Fatalf("preview → %s", rec.Body)
	}
	hedgeCall(h, http.MethodGet, "/api/hedge?preview=true", "", "")
	if c.previews != 1 {
		t.Errorf("previews are cached: %d exchange reads, want 1", c.previews)
	}
	if rec := hedgeCall(h, http.MethodGet, "/api/hedge?preview=maybe", "", ""); rec.Code != http.StatusBadRequest {
		t.Errorf("bad preview flag → %d", rec.Code)
	}
}

func TestAPIHedge_SwitchNeedsTokenBodyAndTestnet(t *testing.T) {
	c := &fakeHedgeController{}
	h := hedgeServer(c, "secret", false)
	if rec := hedgeCall(h, http.MethodPost, "/api/hedge", "", `{"enabled":true}`); rec.Code != http.StatusUnauthorized {
		t.Errorf("no token → %d", rec.Code)
	}
	if rec := hedgeCall(h, http.MethodPost, "/api/hedge", "secret", `{}`); rec.Code != http.StatusBadRequest {
		t.Errorf("no enabled field → %d", rec.Code)
	}
	if rec := hedgeCall(h, http.MethodPost, "/api/hedge", "secret", `{"enabled":true,"actor":{"user":"X!","role":"admin"}}`); rec.Code != http.StatusBadRequest {
		t.Errorf("bad actor → %d", rec.Code)
	}
	rec := hedgeCall(h, http.MethodPost, "/api/hedge", "secret", `{"enabled":true,"actor":{"user":"roberto","role":"admin"}}`)
	if rec.Code != http.StatusOK || len(c.switches) != 1 || !c.switches[0] || c.by[0].User != "roberto" {
		t.Fatalf("switch on → %d %s %+v", rec.Code, rec.Body, c.by)
	}
	c.switchErr = errors.New("the perpetual could not be closed: the experiment stays ON")
	if rec := hedgeCall(h, http.MethodPost, "/api/hedge", "secret", `{"enabled":false}`); rec.Code != http.StatusConflict {
		t.Errorf("a refused switch → %d, want 409", rec.Code)
	}

	live := hedgeServer(&fakeHedgeController{}, "secret", true)
	if rec := hedgeCall(live, http.MethodPost, "/api/hedge", "secret", `{"enabled":true}`); rec.Code != http.StatusForbidden {
		t.Errorf("live → %d, want 403 (testnet-only)", rec.Code)
	}
	noToken := hedgeServer(&fakeHedgeController{}, "", false)
	// Only GET is registered without the admin token.
	if rec := hedgeCall(noToken, http.MethodPost, "/api/hedge", "", `{"enabled":true}`); rec.Code != http.StatusMethodNotAllowed {
		t.Errorf("no admin token configured → %d, want 405 (no POST route)", rec.Code)
	}
}
