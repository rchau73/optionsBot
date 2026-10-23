package tests

import (
	"context"
	"encoding/json"
	"fmt"
	"math"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
	"time"
)

// The E3 usability test kit (docs/usability/e3-investor-monitor-test.md,
// #202) is followed by a moderator who has never seen the bot. These tests
// keep it runnable from the file alone, keep its answer key true to what
// cmd/monitor-demo serves, and keep participant data out of its templates.

const usabilityKit = "../docs/usability/e3-investor-monitor-test.md"

func readUsabilityKit(t *testing.T) string {
	t.Helper()
	b, err := os.ReadFile(usabilityKit)
	if err != nil {
		t.Fatalf("usability kit missing: %v", err)
	}
	return string(b)
}

// kitSection returns the text from the line starting with heading up to the
// next heading of the same level.
func kitSection(t *testing.T, doc, heading string) string {
	t.Helper()
	start := strings.Index(doc, "\n"+heading)
	if start < 0 {
		t.Fatalf("usability kit has no %q", heading)
	}
	body := doc[start+1:]
	level := heading[:strings.Index(heading, " ")+1] // "## " or "### "
	if next := strings.Index(body[len(heading):], "\n"+level); next >= 0 {
		body = body[:len(heading)+next]
	}
	return body
}

// Scenario: the kit runs without the author — it says how to start the demo
// and the monitor, asks the four questions in a fixed order, lists what the
// moderator must not say, and scores every question from an answer key.
func TestUsabilityKit_RunsWithoutTheAuthor(t *testing.T) {
	doc := readUsabilityKit(t)
	for _, want := range []string{
		"go run ./cmd/monitor-demo",
		"BOT_APIS=btc=http://127.0.0.1:8081 npm run dev",
		"npm ci",
		"AUTH_SECRET=",
		// A fresh user database per run: the first admin is created only
		// when the database is empty (frontend/lib/auth/service.js).
		`export AUTH_DB_PATH="$(mktemp -d)/auth.db"`,
		"1440 × 900",
		"390 × 844",
		"**The moderator must not:**",
		"5-minute timer",
		"**The round passes when at least 4 of the 5 participants pass.**",
		"Never ask how much.",
		"needs legal review first",
	} {
		if !strings.Contains(doc, want) {
			t.Errorf("the kit does not contain %q", want)
		}
	}
	if strings.Contains(doc, "> .env.local") {
		t.Error("the kit overwrites the moderator's frontend/.env.local")
	}
	for _, h := range []string{"## 1. When to run it", "## 2. Screener", "## 3. Setup", "## 4. Moderator script",
		"## 5. After the tasks", "## 6. Answer key", "## 7. Scoring", "## 8. Templates"} {
		kitSection(t, doc, h)
	}

	// The four questions, in the PRD's order.
	rows := planTable(t, doc, "4")
	want := []string{"what the bot holds", "making or losing money", "the worst case now", "why it did its last trade"}
	if len(rows) != len(want) {
		t.Fatalf("the moderator script has %d questions, want %d", len(rows), len(want))
	}
	for i, r := range rows {
		if r[0] != fmt.Sprintf("Q%d", i+1) || r[2] != want[i] {
			t.Errorf("question %d = %v, want Q%d measuring %q", i+1, r, i+1, want[i])
		}
		if !strings.HasPrefix(r[1], `"`) {
			t.Errorf("Q%d is not given word for word in quotes: %q", i+1, r[1])
		}
	}

	// Every question has a success / partial / fail key.
	key := kitSection(t, doc, "## 6. Answer key")
	for i := 1; i <= 4; i++ {
		q := kitSection(t, key, fmt.Sprintf("### Q%d ", i))
		for _, s := range []string{"| Success |", "| Partial |", "| Fail |"} {
			if !strings.Contains(q, s) {
				t.Errorf("Q%d's answer key has no %q row", i, s)
			}
		}
	}

	// The three after-task questions ask about what happened, never "would you".
	after := kitSection(t, doc, "## 5. After the tasks")
	qs := regexp.MustCompile(`(?m)^\d\. "(.+)"$`).FindAllStringSubmatch(after, -1)
	if len(qs) != 3 {
		t.Fatalf("after the tasks: %d questions, want 3", len(qs))
	}
	for _, q := range qs {
		if regexp.MustCompile(`(?i)\b(would|will you|do you think|imagine)\b`).MatchString(q[1]) {
			t.Errorf("after-task question asks about the future: %q", q[1])
		}
	}

	readme, err := os.ReadFile("../frontend/README.md")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(readme), "(../docs/usability/e3-investor-monitor-test.md)") {
		t.Error("frontend/README.md does not link the usability kit")
	}
}

// Scenario: privacy — the templates collect no names, contact details or
// amounts, and the private notes folder is gitignored.
func TestUsabilityKit_Privacy(t *testing.T) {
	doc := readUsabilityKit(t)
	begin, end := strings.Index(doc, "<!-- templates: begin -->"), strings.Index(doc, "<!-- templates: end -->")
	if begin < 0 || end < begin {
		t.Fatal("the kit has no templates: begin/end markers")
	}
	tpl := doc[begin:end]
	if !strings.Contains(tpl, "### Results table") || !strings.Contains(tpl, "### Synthesis") {
		t.Fatal("the templates must hold the results table and the synthesis")
	}
	personal := regexp.MustCompile(`(?i)\b(name|e-?mail|phone|contact|address|age|gender|employer|company|city|country|holdings?|balance|portfolio)\b`)
	if m := personal.FindString(tpl); m != "" {
		t.Errorf("the templates ask for personal data: %q", m)
	}
	email := regexp.MustCompile(`[A-Za-z0-9._%+-]+@[A-Za-z0-9.-]+\.[A-Za-z]{2,}`)
	money := regexp.MustCompile(`(?i)[$€£]\s?\d|\d\s?(usd|usdc|usdt|brl|eur|btc|eth)\b|\b(usd|btc|eth|amount)\b`)
	if m := email.FindString(tpl); m != "" {
		t.Errorf("the templates contain an e-mail address %q", m)
	}
	if m := money.FindString(tpl); m != "" {
		t.Errorf("the templates contain an amount or a currency %q", m)
	}
	if !strings.Contains(doc, "`data/usability/e3/<round>/`") {
		t.Error("the kit does not name the private notes folder data/usability/e3/<round>/")
	}
	ignore, err := os.ReadFile("../.gitignore")
	if err != nil {
		t.Fatal(err)
	}
	if !regexp.MustCompile(`(?m)^/data/$`).Match(ignore) {
		t.Fatal(".gitignore no longer ignores /data/: session notes would be committable")
	}
}

// demoAPI builds cmd/monitor-demo, starts it on a free local port with the
// kit's flags (none but the address) and returns a JSON getter.
func demoAPI(t *testing.T) func(path string, into any) {
	t.Helper()
	if testing.Short() {
		t.Skip("builds and runs cmd/monitor-demo")
	}
	bin := filepath.Join(t.TempDir(), "monitor-demo")
	if out, err := exec.Command("go", "build", "-o", bin, "../cmd/monitor-demo").CombinedOutput(); err != nil {
		t.Fatalf("build monitor-demo: %v\n%s", err, out)
	}
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("free port: %v", err)
	}
	addr := l.Addr().String()
	l.Close()

	ctx, cancel := context.WithCancel(context.Background())
	cmd := exec.CommandContext(ctx, bin, "-addr", addr)
	if err := cmd.Start(); err != nil {
		cancel()
		t.Fatalf("start monitor-demo: %v", err)
	}
	t.Cleanup(func() { cancel(); cmd.Wait() })

	client := &http.Client{Timeout: 5 * time.Second}
	get := func(path string, into any) {
		t.Helper()
		resp, err := client.Get("http://" + addr + path)
		if err != nil {
			t.Fatalf("GET %s: %v", path, err)
		}
		defer resp.Body.Close()
		if resp.StatusCode != http.StatusOK {
			t.Fatalf("GET %s: %s", path, resp.Status)
		}
		if err := json.NewDecoder(resp.Body).Decode(into); err != nil {
			t.Fatalf("GET %s: decode: %v", path, err)
		}
	}
	for deadline := time.Now().Add(20 * time.Second); ; {
		resp, err := client.Get("http://" + addr + "/api/health")
		if err == nil {
			resp.Body.Close()
			if resp.StatusCode == http.StatusOK {
				break
			}
		}
		if time.Now().After(deadline) {
			t.Fatalf("monitor-demo did not answer on %s: %v", addr, err)
		}
		time.Sleep(100 * time.Millisecond)
	}
	return get
}

// Scenario: the answer key matches the demo — every fixed answer in the key
// is in /api/status, /api/positions and /api/summary, and /api/events has the
// activity the Q4 fail case warns about.
func TestUsabilityKit_AnswerKeyMatchesTheDemo(t *testing.T) {
	doc := readUsabilityKit(t)
	key := kitSection(t, doc, "## 6. Answer key")
	get := demoAPI(t)

	var status struct {
		Underlying    string `json:"underlying"`
		Environment   string `json:"environment"`
		OpenStrangles int    `json:"open_strangles"`
		OpenLegs      int    `json:"open_legs"`
	}
	get("/api/status", &status)
	if status.Underlying != "BTC" || !strings.Contains(status.Environment, "demo") {
		t.Errorf("status: underlying %q environment %q, want BTC demo", status.Underlying, status.Environment)
	}
	if status.OpenStrangles != 3 || status.OpenLegs != 6 {
		t.Errorf("status: %d strangles %d legs, want 3 and 6 (Q1)", status.OpenStrangles, status.OpenLegs)
	}

	// Q1: 3 calls + 3 puts, all sold, 0.3 BTC each, at the key's strikes and expiries.
	var positions struct {
		Strangles []struct {
			Slot struct {
				DTE int `json:"dte"`
			} `json:"slot"`
			Legs []struct {
				OptionType string  `json:"option_type"`
				Side       string  `json:"side"`
				Strike     float64 `json:"strike"`
				DTE        float64 `json:"dte"`
				Qty        float64 `json:"qty"`
			} `json:"legs"`
		} `json:"strangles"`
	}
	get("/api/positions", &positions)
	strikes := map[int][2]float64{25: {110000, 90000}, 45: {114000, 87000}, 60: {116000, 84000}}
	if len(positions.Strangles) != 3 {
		t.Fatalf("positions: %d strangles, want 3", len(positions.Strangles))
	}
	for _, st := range positions.Strangles {
		want, ok := strikes[st.Slot.DTE]
		if !ok {
			t.Errorf("strangle in slot DTE %d, the key lists 25, 45 and 60", st.Slot.DTE)
			continue
		}
		if !strings.Contains(key, fmt.Sprintf("%s / %s (≈ %d days)", thousands(want[0]), thousands(want[1]), st.Slot.DTE)) {
			t.Errorf("the key does not list the %d-day strikes %v", st.Slot.DTE, want)
		}
		for _, l := range st.Legs {
			wantStrike := want[0]
			if l.OptionType == "put" {
				wantStrike = want[1]
			}
			if l.Side != "sell" || math.Abs(l.Qty-0.3) > 1e-9 || l.Strike != wantStrike {
				t.Errorf("DTE %d %s: side %s qty %v strike %v, want sell 0.3 at %v", st.Slot.DTE, l.OptionType, l.Side, l.Qty, l.Strike, wantStrike)
			}
			if l.DTE < float64(st.Slot.DTE)-1 || l.DTE > float64(st.Slot.DTE)+1 {
				t.Errorf("DTE %d %s expires in %.1f days, the key says about %d", st.Slot.DTE, l.OptionType, l.DTE, st.Slot.DTE)
			}
		}
	}

	var summary struct {
		Underlying string `json:"underlying"`
		Holding    struct {
			Strangles    int      `json:"strangles"`
			Calls        int      `json:"calls"`
			Puts         int      `json:"puts"`
			NotionalCoin float64  `json:"notional_coin"`
			NearestDTE   *float64 `json:"nearest_dte"`
		} `json:"holding"`
		Income struct {
			Months []struct {
				Month        string  `json:"month"`
				RealisedCoin float64 `json:"realised_coin"`
				RealisedUSD  float64 `json:"realised_usd"`
				Closes       int     `json:"closes"`
			} `json:"months"`
		} `json:"income"`
		WorstCase struct {
			Available bool    `json:"available"`
			Model     string  `json:"model"`
			MovePct   float64 `json:"move_pct"`
			Down      struct {
				LossUSD     float64  `json:"loss_usd"`
				LossCoin    float64  `json:"loss_coin"`
				PctOfEquity *float64 `json:"pct_of_equity"`
			} `json:"down"`
			Up struct {
				LossUSD     float64  `json:"loss_usd"`
				LossCoin    float64  `json:"loss_coin"`
				PctOfEquity *float64 `json:"pct_of_equity"`
			} `json:"up"`
		} `json:"worst_case"`
		LastAction *struct {
			Kind   string `json:"kind"`
			Reason string `json:"reason"`
		} `json:"last_action"`
	}
	get("/api/summary", &summary)
	h := summary.Holding
	if h.Strangles != 3 || h.Calls != 3 || h.Puts != 3 || math.Abs(h.NotionalCoin-1.8) > 1e-9 {
		t.Errorf("holding %+v, want 3 strangles, 3 calls, 3 puts, 1.8 BTC (Q1)", h)
	}
	if h.NearestDTE == nil || *h.NearestDTE < 24 || *h.NearestDTE > 25.5 {
		t.Errorf("nearest DTE %v, the key says 24–25 days", h.NearestDTE)
	}
	for _, want := range []string{"**3 calls and 3 puts, all sold**", "**0.3 BTC**", "**1.80 BTC sold**", "**25, 45 and 60 days**"} {
		if !strings.Contains(key, want) {
			t.Errorf("the Q1 key does not say %q", want)
		}
	}

	// Q2: every full past month is positive (the current month is last).
	months := summary.Income.Months
	if len(months) != 6 {
		t.Fatalf("income: %d months, want 6", len(months))
	}
	for _, m := range months[:len(months)-1] {
		if m.Closes == 0 || m.RealisedCoin <= 0 || m.RealisedUSD <= 0 {
			t.Errorf("month %s: %d closes, %v BTC, %v USD — the Q2 key says every full past month is positive", m.Month, m.Closes, m.RealisedCoin, m.RealisedUSD)
		}
	}
	if !strings.Contains(key, "Each **full past month is positive**") {
		t.Error("the Q2 key no longer says every full past month is positive")
	}

	// Q3: a modelled loss each way, as % of equity too, a move of about 7–9 %.
	wc := summary.WorstCase
	if !wc.Available || wc.Model == "" {
		t.Fatalf("worst case unavailable or unlabelled: %+v", wc)
	}
	if wc.MovePct < 6 || wc.MovePct > 10 {
		t.Errorf("worst-case move %.2f %%, the Q3 key says about 7–9 %%", wc.MovePct)
	}
	if wc.Down.LossUSD <= 0 || wc.Up.LossUSD <= 0 || wc.Down.LossCoin <= 0 || wc.Up.LossCoin <= 0 {
		t.Errorf("worst case down %+v up %+v: the Q3 key says each line is a loss", wc.Down, wc.Up)
	}
	if wc.Down.PctOfEquity == nil || wc.Up.PctOfEquity == nil {
		t.Error("worst case has no % of equity: the Q3 key says BTC, USD and % of equity")
	}
	if !strings.Contains(key, `**"This is a model, not a quote"**`) {
		t.Error("the Q3 key does not quote the model label")
	}

	// Q4: the last action's sentence is one the key lists (call or put).
	if summary.LastAction == nil {
		t.Fatal("no last action: the Q4 key expects one")
	}
	q4 := kitSection(t, key, "### Q4 ")
	sentences := regexp.MustCompile(`\| "([^"]+)" \|`).FindAllStringSubmatch(q4, -1)
	if len(sentences) < 5 {
		t.Fatalf("the Q4 key lists %d sentences, want at least 5", len(sentences))
	}
	listed := false
	for _, s := range sentences {
		r := summary.LastAction.Reason
		if r == s[1] || r == swapWord(s[1], "call", "put") || r == swapWord(s[1], "put", "call") {
			listed = true
		}
	}
	if !listed {
		t.Errorf("last action %q is not in the Q4 key", summary.LastAction.Reason)
	}

	// /api/events: the activity feed the Q4 fail case names fills within seconds.
	var events struct {
		Events []struct {
			Event string `json:"event"`
		} `json:"events"`
	}
	for deadline := time.Now().Add(60 * time.Second); ; time.Sleep(500 * time.Millisecond) {
		get("/api/events", &events)
		if len(events.Events) > 0 || time.Now().After(deadline) {
			break
		}
	}
	if len(events.Events) == 0 {
		t.Error("/api/events stayed empty for a minute: the Activity feed would be empty")
	}
}

// swapWord replaces the first "the <a>" with "the <b>" (call ↔ put in a
// plain sentence).
func swapWord(s, a, b string) string {
	return strings.Replace(s, "the "+a, "the "+b, 1)
}

// thousands formats 110000 as "110,000".
func thousands(v float64) string {
	n := fmt.Sprintf("%.0f", v)
	var out []byte
	for i := range n {
		if i > 0 && (len(n)-i)%3 == 0 {
			out = append(out, ',')
		}
		out = append(out, n[i])
	}
	return string(out)
}
