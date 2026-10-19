package tests

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"optionsbot/internal/api"
	"optionsbot/internal/orders"
)

// The monitor names the person behind a manual close or rebalance (#171):
// {"actor": {"user": "bob", "role": "manager"}}, accepted only with the bearer
// token, logged and journaled by the bot.

var testActor = orders.Actor{User: "bob", Role: "manager"}

func TestActorOf(t *testing.T) {
	cases := []struct {
		in   *orders.Actor
		want orders.Actor
		ok   bool
	}{
		{nil, orders.Actor{User: "monitor"}, true},
		{&orders.Actor{User: "bob", Role: "manager"}, testActor, true},
		{&orders.Actor{User: "a.b_c-1", Role: "admin"}, orders.Actor{User: "a.b_c-1", Role: "admin"}, true},
		{&orders.Actor{User: "Bob", Role: "manager"}, orders.Actor{}, false},       // usernames are lower-case
		{&orders.Actor{User: "bo", Role: "manager"}, orders.Actor{}, false},        // too short
		{&orders.Actor{User: "bob\nWARN", Role: "manager"}, orders.Actor{}, false}, // no log injection
		{&orders.Actor{User: "bob", Role: "root"}, orders.Actor{}, false},
		{&orders.Actor{User: "bob"}, orders.Actor{}, false}, // a named person has a role
		{&orders.Actor{}, orders.Actor{}, false},
	}
	for _, c := range cases {
		got, err := api.ActorOf(c.in)
		if (err == nil) != c.ok || got != c.want {
			t.Errorf("ActorOf(%+v) = %+v, %v; want %+v ok=%v", c.in, got, err, c.want, c.ok)
		}
	}
	if s := testActor.String(); s != "bob (manager)" {
		t.Errorf("String() = %q", s)
	}
}

func TestAPI_ManualCloseForwardsTheActor(t *testing.T) {
	logger := orders.NewWriterLogger(io.Discard, 0)
	c := &fakeCloser{}
	h := api.New(staticView{sampleView()}, logger, api.WithManualClose(c, "s3cret")).Handler()
	post := func(body string) *httptest.ResponseRecorder {
		req := httptest.NewRequest(http.MethodPost, "/api/positions/close", strings.NewReader(body))
		req.Header.Set("Authorization", "Bearer s3cret")
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, req)
		return rec
	}

	if rec := post(`{"position_ids":["p1"],"actor":{"user":"bob","role":"root"}}`); rec.Code != http.StatusBadRequest || c.ids != nil {
		t.Fatalf("a malformed actor must be refused before the strategy: %d, ids %v", rec.Code, c.ids)
	}
	if rec := post(`{"position_ids":["p1"],"actor":{"user":"bob","role":"manager"}}`); rec.Code != http.StatusOK || c.by != testActor {
		t.Errorf("close by bob: %d, actor %+v", rec.Code, c.by)
	}
	if rec := post(`{"position_ids":["p1"]}`); rec.Code != http.StatusOK || c.by != (orders.Actor{User: "monitor"}) {
		t.Errorf("no actor → monitor: %d, actor %+v", rec.Code, c.by)
	}
}

func TestAPI_ManualRebalanceForwardsTheActor(t *testing.T) {
	logger := orders.NewWriterLogger(io.Discard, 0)
	r := &fakeRebalancer{}
	h := api.New(staticView{sampleView()}, logger, api.WithManualRebalance(r, "s3cret")).Handler()
	post := func(auth, body string) *httptest.ResponseRecorder {
		req := httptest.NewRequest(http.MethodPost, "/api/rebalance?dry_run=true", strings.NewReader(body))
		req.Header.Set("Authorization", auth)
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, req)
		return rec
	}

	if rec := post("Bearer wrong", `{"actor":{"user":"bob","role":"manager"}}`); rec.Code != http.StatusUnauthorized || r.calls != 0 {
		t.Fatalf("an actor without the token is refused: %d, %d calls", rec.Code, r.calls)
	}
	for _, bad := range []string{`{"actor":{"user":"x","role":"manager"}}`, `not json`} {
		if rec := post("Bearer s3cret", bad); rec.Code != http.StatusBadRequest || r.calls != 0 {
			t.Fatalf("%s: must be refused before the strategy: %d, %d calls", bad, rec.Code, r.calls)
		}
	}
	if rec := post("Bearer s3cret", `{"actor":{"user":"bob","role":"manager"}}`); rec.Code != http.StatusOK || r.by != testActor {
		t.Errorf("rebalance by bob: %d, actor %+v", rec.Code, r.by)
	}
	if rec := post("Bearer s3cret", ""); rec.Code != http.StatusOK || r.by != (orders.Actor{User: "monitor"}) {
		t.Errorf("empty body → monitor: %d, actor %+v", rec.Code, r.by)
	}
}

// The journal line of a manual close carries "actor", and the journal's
// trades (/api/trades, rebuilt at startup) keep it.
func TestJournal_ManualCloseRecordsTheActor(t *testing.T) {
	var buf bytes.Buffer
	logger := orders.NewWriterLogger(&buf, 0)
	pos := &orders.Position{ID: "p1", Instrument: "BTC-6NOV26-76000-P", OptionType: "put", Qty: 0.1,
		PremiumReceived: 0.002, EntryTime: time.Now().Add(-48 * time.Hour)}
	actor := testActor
	logger.LogClose(pos, orders.Fill{OrderID: "o1", Qty: 0.1, FillPrice: 0.01}, orders.TriggerManualClose, orders.TypeMarket,
		orders.EventContext{Detail: "manual close from the monitor by bob (manager)", Actor: &actor})

	var rec struct {
		Actor *orders.Actor `json:"actor"`
	}
	if err := json.Unmarshal(bytes.TrimSpace(buf.Bytes()), &rec); err != nil || rec.Actor == nil || *rec.Actor != testActor {
		t.Fatalf("journal line must carry actor bob (manager): %v %s", err, buf.String())
	}
	trades := logger.Trades(0, 10)
	if len(trades) != 1 || trades[0].Actor == nil || *trades[0].Actor != testActor {
		t.Errorf("trades must keep the actor: %+v", trades)
	}

	// Closes by a rule name nobody.
	buf.Reset()
	logger.LogClose(pos, orders.Fill{OrderID: "o2", Qty: 0.1, FillPrice: 0.01}, orders.TriggerStopLoss200Pct, orders.TypeMarket, orders.EventContext{})
	if strings.Contains(buf.String(), `"actor"`) {
		t.Errorf("a rule's close has no actor: %s", buf.String())
	}
}
