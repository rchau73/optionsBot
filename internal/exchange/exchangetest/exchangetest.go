// Package exchangetest is the conformance suite every exchange adapter must
// pass (E8, #819): what docs/exchange-adapter.md §3 and §5 ask of an
// adapter, written as cases that see it through exchange.Exchange only.
//
// An adapter's test builds its adapter over a scripted fake transport and
// calls Run:
//
//	exchangetest.Run(t, func(o exchangetest.Options) exchangetest.Subject { … })
//
// The suite never names a request. The Fake the adapter's test supplies
// answers in the exchange's own wire format, so the same cases run on any
// exchange. It is a package of test helpers (it imports "testing" to report,
// and no production code imports it).
package exchangetest

import (
	"bytes"
	"context"
	"fmt"
	"log/slog"
	"strings"
	"sync"
	"testing"
	"time"

	"optionsbot/internal/exchange"
	"optionsbot/internal/orders"
)

// Op is a request kind the suite asks the fake to script.
type Op string

const (
	OpSubmit        Op = "submit"
	OpAmend         Op = "amend"
	OpCancel        Op = "cancel"
	OpCancelAll     Op = "cancel_all"
	OpCancelByLabel Op = "cancel_by_label"
	OpSimulate      Op = "simulate"
)

// Outcome is how the fake answers a scripted request.
type Outcome int

const (
	Answer   Outcome = iota // a valid success reply
	Reject                  // the exchange refuses: a definite "not placed"
	NoAnswer                // the request is swallowed: the caller times out
)

// Fake is the scripted transport under an adapter.
type Fake interface {
	// Script sets how every later request of op is answered.
	Script(op Op, o Outcome)
	// SeedChain makes the exchange serve an option chain of at least two
	// instruments (a call and a put) for Subject.Start to load.
	SeedChain()
	// Requests lists what the exchange received, in order, one line each.
	Requests() []string
	// PrivateRequests counts the requests that need credentials.
	PrivateRequests() int
	// CancelledLabels lists the labels the cancel-by-label requests carried.
	CancelledLabels() []string
}

// Options says how the suite wants an adapter built.
type Options struct {
	PublicOnly bool // a connection with no credentials (market data only)
	FailAuth   bool // the exchange refuses the credentials below
	// Credentials the adapter is configured with, and a scope the exchange
	// answers with when authentication fails: none may reach an error or a log.
	ClientID, ClientSecret, Scope string
}

// Subject is one adapter under test, not yet connected.
type Subject struct {
	Exchange     exchange.Exchange
	Capabilities exchange.Capabilities
	Fake         Fake
	// Start loads the option chain (the market-data manager's start); nil
	// when Connect does.
	Start func(ctx context.Context) error
	// Close releases the fake; may be nil.
	Close func()
}

// NewSubject builds an adapter over a fresh fake. It must create the
// adapter's logger (slog.Default) while it runs: the no-secrets case
// captures it.
type NewSubject func(o Options) Subject

// TB is the part of testing.TB the cases use; *testing.T satisfies it, and a
// test can pass a recorder to prove that a case fails.
type TB interface {
	Helper()
	Errorf(format string, args ...any)
}

// Case is one contract case.
type Case struct {
	Name string
	Run  func(t TB, newSubject NewSubject)
}

// Cases lists the contract, in the order Run executes it.
func Cases() []Case {
	return []Case{
		{"SubmitOutcomes", submitOutcomes},
		{"TimeoutIsMaybePlacedAndCancelledByLabel", timeoutIsMaybePlaced},
		{"CapabilitiesMatchBehaviour", capabilitiesMatchBehaviour},
		{"InstrumentFieldsFilled", instrumentFieldsFilled},
		{"PerpetualNameStable", perpetualNameStable},
		{"PublicOnlyRefusesPrivateCalls", publicOnlyRefusesPrivate},
		{"NoSecretsInErrorsOrLogs", noSecretsInErrorsOrLogs},
	}
}

// Run executes every case as a subtest.
func Run(t *testing.T, newSubject NewSubject) {
	t.Helper()
	for _, c := range Cases() {
		t.Run(c.Name, func(t *testing.T) { c.Run(t, newSubject) })
	}
}

// open builds a subject and connects it; ok is false when it could not.
func open(t TB, newSubject NewSubject, o Options) (s Subject, ok bool) {
	t.Helper()
	s = newSubject(o)
	if err := s.Exchange.Connect(context.Background()); err != nil {
		t.Errorf("Connect: %v", err)
		s.shutdown()
		return s, false
	}
	return s, true
}

func (s Subject) shutdown() {
	if s.Exchange != nil {
		_ = s.Exchange.Close()
	}
	if s.Close != nil {
		s.Close()
	}
}

// testOrder is the order every case submits: a sold put with a label.
func testOrder(label string) orders.Order {
	return orders.Order{Instrument: "BTC-27DEC30-90000-P", Direction: "sell", OrderType: "limit",
		Qty: 0.2, LimitPrice: 0.0125, TickSize: 0.0005, Label: label}
}

// RequestScript is the cycle of order, account and margin calls the
// adapter's own test replays through the adapter and through the code it
// wraps, to prove that the requests and their order did not change.
func RequestScript(ctx context.Context, ex exchange.Trading) {
	order := testOrder("s1-30d-0.15-entry-x")
	_, _ = ex.Submit(ctx, order)
	_, _ = ex.GetOrderState(ctx, "ord-1")
	_ = ex.AmendOrder(ctx, "ord-1", 0.2, 0.012)
	_ = ex.Cancel(ctx, "ord-1")
	_, _ = ex.CancelByLabel(ctx, "BTC", order.Label)
	_, _ = ex.GetPositions(ctx, "BTC")
	_, _ = ex.GetAccountSummary(ctx, "BTC")
	_, _ = ex.SimulatePortfolio(ctx, "BTC", map[string]float64{order.Instrument: -0.2})
}

// submitOutcomes: every submit ends in exactly one of placed, not placed or
// maybe placed (docs/exchange-adapter.md §5 "Not placed errors").
func submitOutcomes(t TB, newSubject NewSubject) {
	t.Helper()
	s, ok := open(t, newSubject, Options{})
	if !ok {
		return
	}
	defer s.shutdown()
	ctx := context.Background()

	s.Fake.Script(OpSubmit, Answer)
	fill, err := s.Exchange.Submit(ctx, testOrder("conf-placed"))
	switch {
	case err != nil:
		t.Errorf("submit the exchange accepted: error %v", err)
	case orders.MaybePlaced(err):
		t.Errorf("submit the exchange accepted is reported maybe placed")
	case fill.OrderID == "":
		t.Errorf("submit the exchange accepted returned no order ID")
	}

	s.Fake.Script(OpSubmit, Reject)
	if _, err := s.Exchange.Submit(ctx, testOrder("conf-rejected")); err == nil {
		t.Errorf("submit the exchange rejected returned no error")
	} else if orders.MaybePlaced(err) {
		t.Errorf("a rejection is a definite 'not placed', but the error maps to 'maybe placed': %v", err)
	}
}

// timeoutIsMaybePlaced: a submit that gets no answer may be on the book, so
// it is "maybe placed", never "not placed", and the order is found by its
// label and cancelled.
func timeoutIsMaybePlaced(t TB, newSubject NewSubject) {
	t.Helper()
	s, ok := open(t, newSubject, Options{})
	if !ok {
		return
	}
	defer s.shutdown()
	const label = "conf-timeout-label"

	s.Fake.Script(OpSubmit, NoAnswer)
	ctx, cancel := context.WithTimeout(context.Background(), 300*time.Millisecond)
	defer cancel()
	_, err := s.Exchange.Submit(ctx, testOrder(label))
	if err == nil {
		t.Errorf("submit with no answer returned no error")
		return
	}
	if !orders.MaybePlaced(err) {
		t.Errorf("a timeout must map to 'maybe placed', never 'not placed': %v", err)
	}

	if !s.Capabilities.CancelByLabel {
		return // the capabilities case checks that it is refused
	}
	s.Fake.Script(OpCancelByLabel, Answer)
	if _, err := s.Exchange.CancelByLabel(context.Background(), "BTC", label); err != nil {
		t.Errorf("cancelling the maybe-placed order by label: %v", err)
	}
	found := false
	for _, l := range s.Fake.CancelledLabels() {
		found = found || l == label
	}
	if !found {
		t.Errorf("the cancel did not carry the order's label %q (got %q)", label, s.Fake.CancelledLabels())
	}
}

// capabilityCheck is one capability and the call that proves it.
type capabilityCheck struct {
	name string
	has  func(exchange.Capabilities) bool
	op   Op
	call func(ctx context.Context, ex exchange.Exchange) error
}

var capabilityChecks = []capabilityCheck{
	{"AmendInPlace", func(c exchange.Capabilities) bool { return c.AmendInPlace }, OpAmend,
		func(ctx context.Context, ex exchange.Exchange) error { return ex.AmendOrder(ctx, "ord-1", 0.2, 0.012) }},
	{"CancelByLabel", func(c exchange.Capabilities) bool { return c.CancelByLabel }, OpCancelByLabel,
		func(ctx context.Context, ex exchange.Exchange) error {
			_, err := ex.CancelByLabel(ctx, "BTC", "conf-label")
			return err
		}},
	{"CancelAllByCurrency", func(c exchange.Capabilities) bool { return c.CancelAllByCurrency }, OpCancelAll,
		func(ctx context.Context, ex exchange.Exchange) error { return ex.CancelAllOrders(ctx, "BTC") }},
	{"MarginSimulator", func(c exchange.Capabilities) bool { return c.MarginSimulator }, OpSimulate,
		func(ctx context.Context, ex exchange.Exchange) error {
			_, err := ex.SimulatePortfolio(ctx, "BTC", map[string]float64{"BTC-27DEC30-90000-P": -0.2})
			return err
		}},
}

// capabilitiesMatchBehaviour: a capability the adapter claims works; one it
// does not claim is refused with an error and sends nothing — never
// silently emulated (§5 "No amend in place").
func capabilitiesMatchBehaviour(t TB, newSubject NewSubject) {
	t.Helper()
	s, ok := open(t, newSubject, Options{})
	if !ok {
		return
	}
	defer s.shutdown()
	ctx := context.Background()

	for _, c := range capabilityChecks {
		s.Fake.Script(c.op, Answer)
		before := len(s.Fake.Requests())
		err := c.call(ctx, s.Exchange)
		sent := len(s.Fake.Requests()) - before
		switch {
		case c.has(s.Capabilities) && err != nil:
			t.Errorf("capability %s: Capabilities claims it, but the call failed: %v", c.name, err)
		case !c.has(s.Capabilities) && err == nil:
			t.Errorf("capability %s: Capabilities says false, but the call succeeded (emulated?)", c.name)
		case !c.has(s.Capabilities) && sent > 0:
			t.Errorf("capability %s: Capabilities says false, but the call sent %d request(s): it must be refused, not emulated", c.name, sent)
		}
	}
	if !s.Capabilities.VolIndex && s.Exchange.DVOL() != 0 {
		t.Errorf("capability VolIndex: Capabilities says false, but DVOL() is %v", s.Exchange.DVOL())
	}
}

// instrumentFieldsFilled: every chain instrument carries Expiry, Strike and
// OptionType, so the strategy never parses a name, and InstrumentFromName
// agrees for a name outside the chain.
func instrumentFieldsFilled(t TB, newSubject NewSubject) {
	t.Helper()
	s, ok := open(t, newSubject, Options{})
	if !ok {
		return
	}
	defer s.shutdown()
	s.Fake.SeedChain()
	if s.Start != nil {
		if err := s.Start(context.Background()); err != nil {
			t.Errorf("loading the option chain: %v", err)
			return
		}
	}
	all := s.Exchange.AllInstruments()
	if len(all) == 0 {
		t.Errorf("the chain is empty after loading")
		return
	}
	for _, in := range all {
		if in.Expiry.IsZero() || in.Strike <= 0 || (in.OptionType != "call" && in.OptionType != "put") {
			t.Errorf("instrument %s: expiry %v, strike %v, option type %q: all three must be filled", in.Name, in.Expiry, in.Strike, in.OptionType)
		}
		got, err := s.Exchange.InstrumentFromName(in.Name)
		if err != nil {
			t.Errorf("InstrumentFromName(%s): %v", in.Name, err)
			continue
		}
		// A name carries the expiry's date, not its time of day.
		if !sameUTCDay(got.Expiry, in.Expiry) || got.Strike != in.Strike || got.OptionType != in.OptionType {
			t.Errorf("InstrumentFromName(%s) = %v %v %q, but the chain holds %v %v %q", in.Name,
				got.Expiry, got.Strike, got.OptionType, in.Expiry, in.Strike, in.OptionType)
		}
	}
}

func sameUTCDay(a, b time.Time) bool {
	ay, am, ad := a.UTC().Date()
	by, bm, bd := b.UTC().Date()
	return ay == by && am == bm && ad == bd
}

// perpetualNameStable: one name per currency, the same on every call, a
// different one for another currency, and no request to get it.
func perpetualNameStable(t TB, newSubject NewSubject) {
	t.Helper()
	s, ok := open(t, newSubject, Options{})
	if !ok {
		return
	}
	defer s.shutdown()
	before := len(s.Fake.Requests())
	btc, eth := s.Exchange.PerpetualName("BTC"), s.Exchange.PerpetualName("ETH")
	if btc == "" || eth == "" {
		t.Errorf("PerpetualName is empty: BTC %q, ETH %q", btc, eth)
	}
	if btc == eth {
		t.Errorf("PerpetualName(BTC) and PerpetualName(ETH) are both %q", btc)
	}
	if again := s.Exchange.PerpetualName("BTC"); again != btc {
		t.Errorf("PerpetualName(BTC) changed between calls: %q then %q", btc, again)
	}
	if n := len(s.Fake.Requests()) - before; n != 0 {
		t.Errorf("PerpetualName sent %d request(s): a name is not a lookup", n)
	}
}

// publicOnlyRefusesPrivate: a connection with no credentials refuses every
// private call, and sends none.
func publicOnlyRefusesPrivate(t TB, newSubject NewSubject) {
	t.Helper()
	s, ok := open(t, newSubject, Options{PublicOnly: true})
	if !ok {
		return
	}
	defer s.shutdown()
	ctx := context.Background()
	refused := map[string]error{}
	_, refused["Submit"] = s.Exchange.Submit(ctx, testOrder("conf-public"))
	_, refused["GetAccountSummary"] = s.Exchange.GetAccountSummary(ctx, "BTC")
	_, refused["GetPositions"] = s.Exchange.GetPositions(ctx, "BTC")
	refused["CancelAllOrders"] = s.Exchange.CancelAllOrders(ctx, "BTC")
	for name, err := range refused {
		if err == nil {
			t.Errorf("public-only connection: %s succeeded, it must refuse private calls", name)
		}
	}
	if n := s.Fake.PrivateRequests(); n != 0 {
		t.Errorf("public-only connection sent %d private request(s)", n)
	}
}

// syncBuffer is a log sink the adapter's goroutines may write concurrently.
type syncBuffer struct {
	mu  sync.Mutex
	buf bytes.Buffer
}

func (b *syncBuffer) Write(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.Write(p)
}

func (b *syncBuffer) String() string {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.String()
}

// noSecretsInErrorsOrLogs: a failing authentication puts no key, client ID
// or raw scope in any error or log line.
func noSecretsInErrorsOrLogs(t TB, newSubject NewSubject) {
	t.Helper()
	o := Options{FailAuth: true, ClientID: "conf-client-id-7f3a", ClientSecret: "conf-secret-91bc", Scope: "conf-scope-trade:read_write"}
	var logs syncBuffer
	prev := slog.Default()
	slog.SetDefault(slog.New(slog.NewTextHandler(&logs, &slog.HandlerOptions{Level: slog.LevelDebug})))
	defer slog.SetDefault(prev)

	s := newSubject(o)
	defer s.shutdown()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	err := s.Exchange.Connect(ctx)
	if err == nil {
		t.Errorf("Connect succeeded although the exchange refused the credentials")
	}
	s.Fake.Script(OpSubmit, Reject)
	_, submitErr := s.Exchange.Submit(ctx, testOrder("conf-secret-check"))
	time.Sleep(50 * time.Millisecond) // let the adapter's goroutines log

	lines := map[string]string{"log": logs.String()}
	if err != nil {
		lines["Connect error"] = err.Error()
	}
	if submitErr != nil {
		lines["Submit error"] = submitErr.Error()
	}
	for where, text := range lines {
		for what, secret := range map[string]string{"client ID": o.ClientID, "client secret": o.ClientSecret, "scope": o.Scope} {
			if strings.Contains(text, secret) {
				t.Errorf("the %s appears in the %s: %s", what, where, fmt.Sprintf("%.200q", text))
			}
		}
	}
}
