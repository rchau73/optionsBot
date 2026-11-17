package tests

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"reflect"
	"strings"
	"testing"
	"time"

	"optionsbot/internal/exchange"
	"optionsbot/internal/exchange/deribit"
	"optionsbot/internal/exchange/exchangetest"
	gw "optionsbot/internal/gateway"
	"optionsbot/internal/orders"
)

// deribitFake scripts mockDeribit in the conformance suite's terms (#819).
type deribitFake struct {
	m *mockDeribit
}

var deribitOpMethods = map[exchangetest.Op][]string{
	exchangetest.OpSubmit:        {"private/buy", "private/sell"},
	exchangetest.OpAmend:         {"private/edit"},
	exchangetest.OpCancel:        {"private/cancel"},
	exchangetest.OpCancelAll:     {"private/cancel_all_by_currency"},
	exchangetest.OpCancelByLabel: {"private/cancel_by_label"},
	exchangetest.OpSimulate:      {"private/simulate_portfolio"},
}

var deribitOpResults = map[exchangetest.Op]string{
	exchangetest.OpSubmit:        `{"order":{"order_id":"ord-1","average_price":0.0125,"filled_amount":0,"order_state":"open"},"trades":[]}`,
	exchangetest.OpCancelByLabel: `1`,
}

func (f *deribitFake) Script(op exchangetest.Op, o exchangetest.Outcome) {
	reply := mockReply{result: deribitOpResults[op]}
	switch o {
	case exchangetest.Reject:
		reply = mockReply{err: &gw.RPCError{Code: 10009, Message: "not_enough_funds"}}
	case exchangetest.NoAnswer:
		reply = mockReply{noReply: true}
	}
	for _, method := range deribitOpMethods[op] {
		f.m.on(method, func(gw.JSONRPCRequest) mockReply { return reply })
	}
}

func (f *deribitFake) SeedChain() {
	f.m.on("public/get_instruments", func(gw.JSONRPCRequest) mockReply {
		exp := time.Date(2030, 12, 27, 8, 0, 0, 0, time.UTC).UnixMilli()
		row := `{"instrument_name":"BTC-27DEC30-100000-%s","strike":100000,"expiration_timestamp":%d,"option_type":"%s","tick_size":0.0001,"min_trade_amount":0.1}`
		return mockReply{result: "[" + fmt.Sprintf(row, "C", exp, "call") + "," + fmt.Sprintf(row, "P", exp, "put") + "]"}
	})
}

func (f *deribitFake) Requests() []string {
	f.m.mu.Lock()
	defer f.m.mu.Unlock()
	var lines []string
	for _, req := range f.m.received {
		params, _ := json.Marshal(req.Params)
		lines = append(lines, req.Method+" "+string(params))
	}
	return lines
}

func (f *deribitFake) PrivateRequests() int {
	n := 0
	for _, line := range f.Requests() {
		if strings.HasPrefix(line, "private/") {
			n++
		}
	}
	return n
}

func (f *deribitFake) CancelledLabels() []string {
	var labels []string
	for _, req := range f.m.requests("private/cancel_by_label") {
		if p, ok := req.Params.(map[string]any); ok {
			if l, ok := p["label"].(string); ok {
				labels = append(labels, l)
			}
		}
	}
	return labels
}

// newDeribitSubject builds deribit.Adapter over the fake gateway.
func newDeribitSubject(t *testing.T) exchangetest.NewSubject {
	return func(o exchangetest.Options) exchangetest.Subject {
		m := newMockDeribit(t)
		if o.FailAuth {
			m.on("public/auth", func(gw.JSONRPCRequest) mockReply {
				return mockReply{result: `{"scope":"` + o.Scope + `"}`}
			})
		}
		cfg := testGatewayConfig()
		cfg.ClientID, cfg.ClientSecret = o.ClientID, o.ClientSecret
		cfg.Underlying = "BTC"
		cfg.IVPercentileWindow = 30
		opts := []gw.Option{gw.WithEndpoint(m.url())}
		if o.PublicOnly {
			opts = append(opts, gw.PublicOnly())
		}
		a := deribit.NewAdapter(cfg, gw.New(cfg, opts...))
		return exchangetest.Subject{
			Exchange:     a,
			Capabilities: exchange.DeribitCapabilities,
			Fake:         &deribitFake{m: m},
			Start:        a.Manager.Start,
			Close:        m.close,
		}
	}
}

// Scenario (#819): Deribit passes the suite — every contract case passes.
func TestExchangeConformance_DeribitPasses(t *testing.T) {
	exchangetest.Run(t, newDeribitSubject(t))
}

// Scenario (#819): the suite sends the requests of today — the suite's
// scripted cycle reaches the fake gateway the same through the adapter as
// through the executor, so Deribit's methods and their order did not change.
func TestExchangeConformance_DeribitRequestsUnchanged(t *testing.T) {
	run := func(build func(*gw.Gateway) exchange.Trading) []string {
		m := newMockDeribit(t)
		g := connectGateway(t, m, testGatewayConfig())
		m.mu.Lock()
		skip := len(m.received)
		m.mu.Unlock()
		exchangetest.RequestScript(context.Background(), build(g))
		return wireLines(t, m, skip)
	}
	before := run(func(g *gw.Gateway) exchange.Trading { return orders.NewExecutor(g) })
	after := run(func(g *gw.Gateway) exchange.Trading { return deribit.NewAdapter(testGatewayConfig(), g) })
	if len(before) < 8 || !reflect.DeepEqual(before, after) {
		t.Errorf("requests changed\nbefore %q\n after %q", before, after)
	}
}

// failRecorder collects what a case reports, so a test can prove it fails.
type failRecorder struct{ msgs []string }

func (r *failRecorder) Helper() {}
func (r *failRecorder) Errorf(format string, args ...any) {
	r.msgs = append(r.msgs, fmt.Sprintf(format, args...))
}

// brokenAmend claims AmendInPlace but its AmendOrder fails.
type brokenAmend struct{ exchange.Exchange }

func (brokenAmend) AmendOrder(context.Context, string, float64, float64) error {
	return errors.New("amend not implemented")
}

// Scenario (#819): a broken adapter is caught — Capabilities claim amend, but
// AmendOrder fails: the capability case fails and names the capability.
func TestExchangeConformance_BrokenCapabilityIsNamed(t *testing.T) {
	inner := newDeribitSubject(t)
	broken := func(o exchangetest.Options) exchangetest.Subject {
		s := inner(o)
		s.Exchange = brokenAmend{s.Exchange}
		return s
	}
	for _, c := range exchangetest.Cases() {
		if c.Name != "CapabilitiesMatchBehaviour" {
			continue
		}
		var r failRecorder
		c.Run(&r, broken)
		if len(r.msgs) != 1 || !strings.Contains(r.msgs[0], "capability AmendInPlace") {
			t.Errorf("want one failure naming AmendInPlace, got %q", r.msgs)
		}
	}
}

// Scenario (#819): an adapter that claims no amend yet amends is caught too.
func TestExchangeConformance_UnclaimedCapabilityMustBeRefused(t *testing.T) {
	inner := newDeribitSubject(t)
	noAmend := func(o exchangetest.Options) exchangetest.Subject {
		s := inner(o)
		s.Capabilities.AmendInPlace = false
		return s
	}
	for _, c := range exchangetest.Cases() {
		if c.Name != "CapabilitiesMatchBehaviour" {
			continue
		}
		var r failRecorder
		c.Run(&r, noAmend)
		if len(r.msgs) == 0 || !strings.Contains(r.msgs[0], "capability AmendInPlace") {
			t.Errorf("want a failure naming AmendInPlace, got %q", r.msgs)
		}
	}
}

// leakyAuth is an adapter whose Connect error carries the client ID.
type leakyAuth struct {
	exchange.Exchange
	id string
}

func (l leakyAuth) Connect(ctx context.Context) error {
	_ = l.Exchange.Connect(ctx)
	return errors.New("auth failed for client " + l.id)
}

// Scenario (#819): an error that carries a credential is caught.
func TestExchangeConformance_SecretInErrorIsCaught(t *testing.T) {
	inner := newDeribitSubject(t)
	leaky := func(o exchangetest.Options) exchangetest.Subject {
		s := inner(o)
		s.Exchange = leakyAuth{s.Exchange, o.ClientID}
		return s
	}
	for _, c := range exchangetest.Cases() {
		if c.Name != "NoSecretsInErrorsOrLogs" {
			continue
		}
		var r failRecorder
		c.Run(&r, leaky)
		if len(r.msgs) == 0 || !strings.Contains(r.msgs[0], "client ID") {
			t.Errorf("want a failure naming the client ID, got %q", r.msgs)
		}
	}
}
