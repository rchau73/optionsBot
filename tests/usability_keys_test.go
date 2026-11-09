package tests

import (
	"fmt"
	"math"
	"regexp"
	"slices"
	"sort"
	"strconv"
	"strings"
	"testing"
	"time"
)

// The E3 kit's answer keys quote figures in fenced `key` blocks (#727), one per
// scenario and question. These tests compare every quoted figure with what
// cmd/monitor-demo serves, so a round is never scored against a stale key.

// usabilityKey is one fenced `key` block of the kit.
type usabilityKey struct {
	scenario string
	question string
	fields   map[string]string
}

var keyBlock = regexp.MustCompile("(?ms)^```key\n(.*?)^```$")

// parseUsabilityKeys reads every `key` block of the kit.
func parseUsabilityKeys(doc string) ([]usabilityKey, error) {
	var keys []usabilityKey
	for _, m := range keyBlock.FindAllStringSubmatch(doc, -1) {
		k := usabilityKey{fields: map[string]string{}}
		for _, line := range strings.Split(strings.TrimSpace(m[1]), "\n") {
			name, value, ok := strings.Cut(line, ":")
			if !ok {
				return nil, fmt.Errorf("key block line %q is not name: value", line)
			}
			name, value = strings.TrimSpace(name), strings.TrimSpace(value)
			switch name {
			case "scenario":
				k.scenario = value
			case "question":
				k.question = value
			default:
				k.fields[name] = value
			}
		}
		if k.scenario == "" || k.question == "" {
			return nil, fmt.Errorf("key block without scenario or question: %q", m[1])
		}
		keys = append(keys, k)
	}
	return keys, nil
}

// demoFacts is what the demo serves, in the terms the keys quote.
type demoFacts struct {
	strangles, openLegs, calls, puts, months int
	side, qtyPerOption, strikes              string
	notionalCoin                             float64
	resultSign, losingMonthsAgo              string
	openLossLeg                              string
	openLossMarkMultiple, openLossStopMult   float64
	worstCaseSign                            string
	worstCaseMovePct                         float64
	worstExceedsMonthlyIncome                bool
	lastActionKind                           string
	reasonInQ4Table                          bool
}

// keyFields reads each field a key may quote: a number is compared with the
// key's number or low-high range, anything else as text.
var keyFields = map[string]func(demoFacts) any{
	"strangles":                         func(f demoFacts) any { return float64(f.strangles) },
	"open_legs":                         func(f demoFacts) any { return float64(f.openLegs) },
	"calls":                             func(f demoFacts) any { return float64(f.calls) },
	"puts":                              func(f demoFacts) any { return float64(f.puts) },
	"side":                              func(f demoFacts) any { return f.side },
	"qty_per_option":                    func(f demoFacts) any { return f.qtyPerOption },
	"notional_coin":                     func(f demoFacts) any { return f.notionalCoin },
	"strikes":                           func(f demoFacts) any { return f.strikes },
	"months":                            func(f demoFacts) any { return float64(f.months) },
	"result_sign":                       func(f demoFacts) any { return f.resultSign },
	"losing_months_ago":                 func(f demoFacts) any { return f.losingMonthsAgo },
	"open_loss_leg":                     func(f demoFacts) any { return f.openLossLeg },
	"open_loss_mark_multiple":           func(f demoFacts) any { return f.openLossMarkMultiple },
	"open_loss_stop_multiple":           func(f demoFacts) any { return f.openLossStopMult },
	"worst_case_sign":                   func(f demoFacts) any { return f.worstCaseSign },
	"worst_case_move_pct":               func(f demoFacts) any { return f.worstCaseMovePct },
	"worst_case_exceeds_monthly_income": func(f demoFacts) any { return strconv.FormatBool(f.worstExceedsMonthlyIncome) },
	"last_action_kinds":                 func(f demoFacts) any { return f.lastActionKind },
	"last_action_reason_in_q4_table":    func(f demoFacts) any { return strconv.FormatBool(f.reasonInQ4Table) },
}

// requiredKeyFields are the figures every scenario's key must quote per
// question, so deleting a line cannot silently stop a check.
var requiredKeyFields = map[string][]string{
	"Q1": {"strangles", "open_legs", "calls", "puts", "side", "qty_per_option", "notional_coin", "strikes"},
	"Q2": {"months", "result_sign", "losing_months_ago"},
	"Q3": {"worst_case_sign", "worst_case_move_pct"},
	"Q4": {"last_action_kinds", "last_action_reason_in_q4_table"},
}

var usabilityScenarios = []string{"default", "mixed", "drawdown"}

// keyMatches compares one quoted value with the demo's.
func keyMatches(want string, got any) bool {
	n, isNumber := got.(float64)
	if !isNumber {
		if field, ok := got.(string); ok && strings.Contains(want, "|") {
			return slices.Contains(strings.Split(strings.ReplaceAll(want, " ", ""), "|"), field)
		}
		return want == got
	}
	low, high, isRange := strings.Cut(want, "-")
	lo, errLo := strconv.ParseFloat(low, 64)
	if errLo != nil {
		return false
	}
	if !isRange {
		return math.Abs(n-lo) < 1e-9
	}
	hi, errHi := strconv.ParseFloat(high, 64)
	return errHi == nil && n >= lo && n <= hi
}

// compareUsabilityKey lists each way the key differs from the demo, naming the
// scenario and the question.
func compareUsabilityKey(k usabilityKey, f demoFacts) []string {
	var problems []string
	report := func(format string, args ...any) {
		problems = append(problems, fmt.Sprintf("scenario %s %s: ", k.scenario, k.question)+fmt.Sprintf(format, args...))
	}
	for _, name := range requiredKeyFields[k.question] {
		if _, ok := k.fields[name]; !ok {
			report("the key does not quote %s", name)
		}
	}
	names := make([]string, 0, len(k.fields))
	for name := range k.fields {
		names = append(names, name)
	}
	sort.Strings(names)
	for _, name := range names {
		read, ok := keyFields[name]
		if !ok {
			report("unknown field %s", name)
			continue
		}
		if got := read(f); !keyMatches(k.fields[name], got) {
			report("%s: the key says %s, the demo has %v", name, k.fields[name], got)
		}
	}
	return problems
}

// missingUsabilityKeys lists every scenario and question without exactly one key.
func missingUsabilityKeys(keys []usabilityKey) []string {
	seen := map[string]int{}
	for _, k := range keys {
		seen[k.scenario+" "+k.question]++
	}
	var problems []string
	for _, s := range usabilityScenarios {
		for _, q := range []string{"Q1", "Q2", "Q3", "Q4"} {
			if n := seen[s+" "+q]; n != 1 {
				problems = append(problems, fmt.Sprintf("scenario %s %s: %d key blocks, want 1", s, q, n))
			}
		}
	}
	return problems
}

// fetchDemoFacts reads the facts the keys quote from a running demo.
func fetchDemoFacts(t *testing.T, get func(string, any), q4Reasons []string, now time.Time) demoFacts {
	t.Helper()
	var positions struct {
		Strangles []struct {
			Slot struct {
				DTE int `json:"dte"`
			} `json:"slot"`
			Legs []struct {
				OptionType    string  `json:"option_type"`
				Side          string  `json:"side"`
				Strike        float64 `json:"strike"`
				Qty           float64 `json:"qty"`
				EntryPrice    float64 `json:"entry_price"`
				Mark          float64 `json:"mark"`
				UnrealisedPnL float64 `json:"unrealised_pnl"`
				StopLossMark  float64 `json:"stop_loss_mark"`
			} `json:"legs"`
		} `json:"strangles"`
	}
	get("/api/positions", &positions)
	var status struct {
		OpenStrangles int `json:"open_strangles"`
		OpenLegs      int `json:"open_legs"`
	}
	get("/api/status", &status)
	var summary struct {
		Holding struct {
			Calls        int     `json:"calls"`
			Puts         int     `json:"puts"`
			NotionalCoin float64 `json:"notional_coin"`
		} `json:"holding"`
		Income struct {
			Months []struct {
				Month        string  `json:"month"`
				RealisedCoin float64 `json:"realised_coin"`
				RealisedUSD  float64 `json:"realised_usd"`
			} `json:"months"`
		} `json:"income"`
		WorstCase struct {
			MovePct float64 `json:"move_pct"`
			Down    struct {
				LossCoin float64 `json:"loss_coin"`
			} `json:"down"`
			Up struct {
				LossCoin float64 `json:"loss_coin"`
			} `json:"up"`
		} `json:"worst_case"`
		LastAction *struct {
			Kind   string `json:"kind"`
			Reason string `json:"reason"`
		} `json:"last_action"`
	}
	get("/api/summary", &summary)

	f := demoFacts{
		strangles: status.OpenStrangles, openLegs: status.OpenLegs,
		calls: summary.Holding.Calls, puts: summary.Holding.Puts,
		notionalCoin: summary.Holding.NotionalCoin, months: len(summary.Income.Months),
		worstCaseMovePct: summary.WorstCase.MovePct,
	}

	sides, sizes := map[string]bool{}, map[string]bool{}
	var strikes []string
	for _, st := range positions.Strangles {
		var call, put float64
		for _, l := range st.Legs {
			sides[l.Side] = true
			sizes[strconv.FormatFloat(l.Qty, 'f', -1, 64)] = true
			if l.OptionType == "call" {
				call = l.Strike
			} else {
				put = l.Strike
			}
			if l.UnrealisedPnL < 0 && l.EntryPrice > 0 && l.Mark >= 2*l.EntryPrice {
				f.openLossLeg = fmt.Sprintf("%dd %s %.0f", st.Slot.DTE, l.OptionType, l.Strike)
				f.openLossMarkMultiple = math.Round(l.Mark/l.EntryPrice*10) / 10
				f.openLossStopMult = math.Round(l.StopLossMark/l.EntryPrice*10) / 10
			}
		}
		strikes = append(strikes, fmt.Sprintf("%d=%.0f/%.0f", st.Slot.DTE, call, put))
	}
	sort.Strings(strikes)
	f.side = strings.Join(sortedSet(sides), ",")
	f.qtyPerOption = strings.Join(sortedSet(sizes), ",")
	f.strikes = strings.Join(strikes, " ")

	worst := math.Max(summary.WorstCase.Down.LossCoin, summary.WorstCase.Up.LossCoin)
	f.worstCaseSign = "gain"
	if summary.WorstCase.Down.LossCoin > 0 && summary.WorstCase.Up.LossCoin > 0 {
		f.worstCaseSign = "loss"
	}

	f.worstExceedsMonthlyIncome = true
	var losing []string
	for i, m := range summary.Income.Months {
		if worst <= m.RealisedCoin {
			f.worstExceedsMonthlyIncome = false
		}
		// The last month is the current one: the keys say to read it off the screen.
		if i == len(summary.Income.Months)-1 || (m.RealisedCoin > 0 && m.RealisedUSD > 0) {
			continue
		}
		at, err := time.Parse("2006-01", m.Month)
		if err != nil {
			t.Fatalf("month %q: %v", m.Month, err)
		}
		ago := (now.Year()-at.Year())*12 + int(now.Month()-at.Month())
		losing = append(losing, strconv.Itoa(ago))
	}
	f.losingMonthsAgo, f.resultSign = "none", "positive"
	if len(losing) > 0 {
		f.losingMonthsAgo, f.resultSign = strings.Join(losing, ","), fmt.Sprintf("%d_losing_month", len(losing))
	}

	if summary.LastAction != nil {
		f.lastActionKind = summary.LastAction.Kind
		for _, s := range q4Reasons {
			r := summary.LastAction.Reason
			if r == s || r == swapWord(s, "call", "put") || r == swapWord(s, "put", "call") {
				f.reasonInQ4Table = true
			}
		}
	}
	return f
}

func sortedSet(set map[string]bool) []string {
	out := make([]string, 0, len(set))
	for v := range set {
		out = append(out, v)
	}
	sort.Strings(out)
	return out
}

// q4Sentences are the plain sentences of the section 6 Q4 table.
func q4Sentences(t *testing.T, doc string) []string {
	t.Helper()
	q4 := kitSection(t, kitSection(t, doc, "## 6. Answer key"), "### Q4 ")
	var out []string
	for _, m := range regexp.MustCompile(`\| "([^"]+)" \|`).FindAllStringSubmatch(q4, -1) {
		out = append(out, m[1])
	}
	if len(out) < 5 {
		t.Fatalf("the Q4 key lists %d sentences, want at least 5", len(out))
	}
	return out
}

// Scenario: Keys match the demo.
func TestUsabilityKeys_KeysMatchTheDemo(t *testing.T) {
	doc := readUsabilityKit(t)
	keys, err := parseUsabilityKeys(doc)
	if err != nil {
		t.Fatal(err)
	}
	for _, p := range missingUsabilityKeys(keys) {
		t.Error(p)
	}
	reasons := q4Sentences(t, doc)
	for _, scenario := range usabilityScenarios {
		t.Run(scenario, func(t *testing.T) {
			var args []string
			if scenario != "default" {
				args = []string{"-scenario", scenario}
			}
			facts := fetchDemoFacts(t, demoAPI(t, args...), reasons, time.Now().UTC())
			for _, k := range keys {
				if k.scenario != scenario {
					continue
				}
				for _, p := range compareUsabilityKey(k, facts) {
					t.Error(p)
				}
			}
		})
	}
}

// Scenario: A stale key is caught.
func TestUsabilityKeys_AStaleKeyIsCaught(t *testing.T) {
	facts := demoFacts{worstCaseSign: "loss", worstCaseMovePct: 8.2, worstExceedsMonthlyIncome: true, lastActionKind: "close"}
	worstCase := func(move string) map[string]string {
		return map[string]string{"worst_case_sign": "loss", "worst_case_move_pct": move}
	}
	for _, tc := range []struct {
		name   string
		key    usabilityKey
		wanted []string // each problem must contain these; none = no problem
	}{
		{"matching range", usabilityKey{"drawdown", "Q3", worstCase("7-9")}, nil},
		{"range boundary", usabilityKey{"drawdown", "Q3", worstCase("8.2-9")}, nil},
		{"stale worst case", usabilityKey{"drawdown", "Q3", worstCase("20-25")},
			[]string{"scenario drawdown Q3", "worst_case_move_pct", "20-25", "8.2"}},
		{"just past the range", usabilityKey{"mixed", "Q3", worstCase("8.3-9")},
			[]string{"scenario mixed Q3", "worst_case_move_pct"}},
		{"not a number", usabilityKey{"default", "Q3", worstCase("about 8")},
			[]string{"scenario default Q3", "worst_case_move_pct"}},
		{"a field the demo does not have", usabilityKey{"default", "Q3",
			map[string]string{"worst_case_sign": "loss", "worst_case_move_pct": "7-9", "worst_case_usd": "100"}},
			[]string{"scenario default Q3", "unknown field worst_case_usd"}},
		{"a figure removed from the key", usabilityKey{"mixed", "Q3", map[string]string{"worst_case_sign": "loss"}},
			[]string{"scenario mixed Q3", "does not quote worst_case_move_pct"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			problems := compareUsabilityKey(tc.key, facts)
			if len(tc.wanted) == 0 {
				if len(problems) != 0 {
					t.Fatalf("unexpected problems: %v", problems)
				}
				return
			}
			if len(problems) != 1 {
				t.Fatalf("problems = %v, want one", problems)
			}
			for _, want := range tc.wanted {
				if !strings.Contains(problems[0], want) {
					t.Errorf("problem %q does not name %q", problems[0], want)
				}
			}
		})
	}
}

// A block removed or duplicated, or one that cannot be read, fails the key test.
func TestUsabilityKeys_EveryScenarioAndQuestionHasOneKey(t *testing.T) {
	var keys []usabilityKey
	for _, s := range usabilityScenarios {
		for _, q := range []string{"Q1", "Q2", "Q3", "Q4"} {
			keys = append(keys, usabilityKey{scenario: s, question: q})
		}
	}
	if p := missingUsabilityKeys(keys); len(p) != 0 {
		t.Fatalf("complete set reported: %v", p)
	}
	if p := missingUsabilityKeys(keys[1:]); len(p) != 1 || !strings.Contains(p[0], "scenario default Q1: 0") {
		t.Errorf("a removed block: %v", p)
	}
	if p := missingUsabilityKeys(append(keys, keys[0])); len(p) != 1 || !strings.Contains(p[0], "scenario default Q1: 2") {
		t.Errorf("a duplicated block: %v", p)
	}
	for _, bad := range []string{"```key\nscenario: mixed\n```\n", "```key\nno colon here\n```\n"} {
		if _, err := parseUsabilityKeys(bad); err == nil {
			t.Errorf("no error for %q", bad)
		}
	}
}
