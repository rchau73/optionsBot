package tests

import (
	"os"
	"regexp"
	"strings"
	"testing"
)

// The analytics event plan (docs/product/analytics/event-plan.md, #192) is a
// contract for the instrumentation stories: these tests read its tables and
// keep the privacy rule true as events are added.

const eventPlanPath = "../docs/product/analytics/event-plan.md"

// Words that, as a part of a property or envelope name, mean it could carry an
// amount, a size, a price or a secret.
var eventDenyWords = map[string]bool{
	"amount": true, "size": true, "qty": true, "quantity": true, "lots": true, "contracts": true,
	"price": true, "premium": true, "strike": true, "mark": true, "bid": true, "ask": true,
	"pnl": true, "profit": true, "loss": true, "usd": true, "btc": true, "eth": true, "coin": true,
	"balance": true, "equity": true, "margin": true, "notional": true, "fee": true, "fees": true,
	"im": true, "mm": true, "instrument": true, "position": true, "delta": true,
	"secret": true, "token": true, "password": true, "key": true, "salt": true,
	"email": true, "username": true, "ip": true, "client": true, "account": true,
	"message": true, "text": true, "url": true, "query": true,
}

// Property types that cannot hold free text or a money figure.
var eventSafeTypes = map[string]bool{"enum": true, "bool": true, "duration": true, "catalogue id": true}

// planTable returns the rows of the first Markdown table under the "## <n>."
// heading, each row's cells with backticks and spaces trimmed.
func planTable(t *testing.T, doc string, section string) [][]string {
	t.Helper()
	start := strings.Index(doc, "\n## "+section+".")
	if start < 0 {
		t.Fatalf("event plan has no section %q", section)
	}
	body := doc[start+1:]
	if next := strings.Index(body[3:], "\n## "); next >= 0 {
		body = body[:next+3]
	}
	var rows [][]string
	header := true
	for _, line := range strings.Split(body, "\n") {
		if !strings.HasPrefix(line, "|") {
			if len(rows) > 0 {
				break // the end of the first table
			}
			continue
		}
		if header {
			header = false // the column names
			continue
		}
		if strings.HasPrefix(line, "|---") {
			continue
		}
		var cells []string
		for _, c := range strings.Split(strings.Trim(line, "|"), "|") {
			cells = append(cells, strings.TrimSpace(c))
		}
		rows = append(rows, cells)
	}
	if len(rows) == 0 {
		t.Fatalf("section %q has no table rows", section)
	}
	return rows
}

var backticked = regexp.MustCompile("`([^`]+)`")

func codeNames(cell string) []string {
	var out []string
	for _, m := range backticked.FindAllStringSubmatch(cell, -1) {
		out = append(out, m[1])
	}
	return out
}

// deniedWord returns the word of name that is on the deny list, or "".
func deniedWord(name string) string {
	for _, w := range strings.Split(strings.ToLower(name), "_") {
		if eventDenyWords[w] {
			return w
		}
	}
	return ""
}

type eventPlan struct {
	questions map[string]bool
	events    map[string][]string // event → properties
	answers   map[string][]string // event → question ids
	props     map[string]string   // property → type
	envelope  []string
}

func loadEventPlan(t *testing.T) eventPlan {
	t.Helper()
	raw, err := os.ReadFile(eventPlanPath)
	if err != nil {
		t.Fatalf("read event plan: %v", err)
	}
	doc := string(raw)
	p := eventPlan{questions: map[string]bool{}, events: map[string][]string{}, answers: map[string][]string{}, props: map[string]string{}}
	for _, r := range planTable(t, doc, "1") {
		p.questions[r[0]] = true
	}
	for _, r := range planTable(t, doc, "2") {
		if len(r) != 4 {
			t.Fatalf("event row %v: want 4 cells", r)
		}
		name := codeNames(r[0])
		if len(name) != 1 {
			t.Fatalf("event row %v: want one backticked name", r)
		}
		p.events[name[0]] = codeNames(r[2])
		for _, q := range strings.Split(r[3], ",") {
			if q = strings.TrimSpace(q); q != "" {
				p.answers[name[0]] = append(p.answers[name[0]], q)
			}
		}
	}
	for _, r := range planTable(t, doc, "3") {
		p.envelope = append(p.envelope, codeNames(r[0])...)
	}
	for _, r := range planTable(t, doc, "4") {
		name := codeNames(r[0])
		if len(name) != 1 {
			t.Fatalf("property row %v: want one backticked name", r)
		}
		p.props[name[0]] = r[1]
	}
	return p
}

// Scenario: privacy by design — no event property can carry an amount, a
// size, a price or a secret, and each event maps to a question.
func TestEventPlan_PrivacyByDesign(t *testing.T) {
	p := loadEventPlan(t)
	if len(p.events) < 10 {
		t.Fatalf("only %d events parsed: the table format changed?", len(p.events))
	}
	for prop, typ := range p.props {
		if w := deniedWord(prop); w != "" {
			t.Errorf("property %q contains %q: it could carry an amount, size, price or secret", prop, w)
		}
		if !eventSafeTypes[typ] {
			t.Errorf("property %q has type %q: only %v cannot carry free text or money", prop, typ, eventSafeTypes)
		}
	}
	for _, f := range p.envelope {
		if w := deniedWord(f); w != "" {
			t.Errorf("envelope field %q contains %q", f, w)
		}
	}
	for ev, props := range p.events {
		for _, prop := range props {
			if _, ok := p.props[prop]; !ok {
				t.Errorf("event %q uses %q, which is not in the allow-list", ev, prop)
			}
		}
		if len(p.answers[ev]) == 0 {
			t.Errorf("event %q answers no question", ev)
		}
		for _, q := range p.answers[ev] {
			if !p.questions[q] {
				t.Errorf("event %q answers %q, which is not a question of §1", ev, q)
			}
		}
	}
}

// The deny check really catches the properties the plan rejects, so the
// scenario test above is not passing by accident.
func TestEventPlan_DenyCatchesMoneyAndSecrets(t *testing.T) {
	for _, bad := range []string{"premium_usd", "qty", "strike", "pnl", "margin_balance", "auth_token", "email", "instrument_name", "ip"} {
		if deniedWord(bad) == "" {
			t.Errorf("%q not denied", bad)
		}
	}
	for _, ok := range []string{"tip_id", "panel", "open_ms", "side", "outcome", "user_id", "session_id"} {
		if w := deniedWord(ok); w != "" {
			t.Errorf("%q denied for %q", ok, w)
		}
	}
}
