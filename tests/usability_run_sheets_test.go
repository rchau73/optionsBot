package tests

import (
	"os"
	"regexp"
	"strings"
	"testing"
)

// The loss and hedge rounds' run sheets (#881) are followed by a moderator
// who has never seen the bot. They must stay true to the demo's scenarios,
// to scripts/usability_round.sh and to the kit section they score with, and
// they must hold no participant data.

type runSheet struct {
	file    string
	kitHead string            // the kit heading the sheet scores with
	assign  map[string]string // participant code → scenario
}

var runSheets = []runSheet{
	{
		file:    "../docs/usability/round-2-loss-run-sheet.md",
		kitHead: "## 6b.",
		assign:  map[string]string{"P1": "mixed", "P2": "drawdown", "P3": "mixed", "P4": "drawdown", "P5": "mixed"},
	},
	{
		file:    "../docs/usability/round-3-hedge-run-sheet.md",
		kitHead: "## 6c.",
		assign:  map[string]string{"P1": "hedged", "P2": "hedged", "P3": "hedged", "P4": "hedged", "P5": "hedged"},
	},
}

// sheetDemoScenarios reads the scenario names cmd/monitor-demo accepts.
func sheetDemoScenarios(t *testing.T) map[string]bool {
	t.Helper()
	b, err := os.ReadFile("../cmd/monitor-demo/main.go")
	if err != nil {
		t.Fatalf("monitor-demo source missing: %v", err)
	}
	out := map[string]bool{}
	for _, m := range regexp.MustCompile(`(?m)^\s*scenario\w+\s*=\s*"(\w+)"`).FindAllStringSubmatch(string(b), -1) {
		out[m[1]] = true
	}
	if len(out) == 0 {
		t.Fatal("no scenario found in cmd/monitor-demo/main.go")
	}
	return out
}

// scriptScenarios reads the scenarios valid_scenario of the round script accepts.
func scriptScenarios(t *testing.T, script string) map[string]bool {
	t.Helper()
	m := regexp.MustCompile(`(?m)^\s*([a-z|]+)\) return 0`).FindStringSubmatch(script)
	if m == nil {
		t.Fatal("scripts/usability_round.sh has no valid_scenario list")
	}
	out := map[string]bool{}
	for _, s := range strings.Split(m[1], "|") {
		out[s] = true
	}
	return out
}

// kitAnchor builds the GitHub anchor of the kit heading starting with prefix.
func kitAnchor(t *testing.T, kit, prefix string) string {
	t.Helper()
	for _, line := range strings.Split(kit, "\n") {
		if strings.HasPrefix(line, prefix) {
			a := strings.ToLower(strings.TrimLeft(line, "# "))
			a = regexp.MustCompile(`[^a-z0-9 -]`).ReplaceAllString(a, "")
			return strings.ReplaceAll(a, " ", "-")
		}
	}
	t.Fatalf("usability kit has no heading %q", prefix)
	return ""
}

func TestUsabilityRunSheets_TrueToScriptDemoAndKit(t *testing.T) {
	kit := readUsabilityKit(t)
	sb, err := os.ReadFile("../scripts/usability_round.sh")
	if err != nil {
		t.Fatalf("scripts/usability_round.sh missing: %v", err)
	}
	script := string(sb)
	demo := sheetDemoScenarios(t)
	inScript := scriptScenarios(t, script)
	row := regexp.MustCompile("(?m)^\\| (P\\d(?: to P\\d)?) \\| `(\\w+)` \\|")
	cmd := regexp.MustCompile(`scripts/usability_round\.sh(?: ([a-z]+))?`)

	for _, sh := range runSheets {
		b, err := os.ReadFile(sh.file)
		if err != nil {
			t.Errorf("%s: run sheet missing: %v", sh.file, err)
			continue
		}
		doc := string(b)

		// Who sees which scenario.
		got := map[string]string{}
		for _, m := range row.FindAllStringSubmatch(doc, -1) {
			if m[1] == "P1 to P5" {
				for _, p := range []string{"P1", "P2", "P3", "P4", "P5"} {
					got[p] = m[2]
				}
			} else {
				got[m[1]] = m[2]
			}
		}
		for p, want := range sh.assign {
			if got[p] != want {
				t.Errorf("%s: %s is on scenario %q, want %q", sh.file, p, got[p], want)
			}
		}
		for p, s := range got {
			if !demo[s] {
				t.Errorf("%s: %s names scenario %q that cmd/monitor-demo does not have", sh.file, p, s)
			}
			if !inScript[s] {
				t.Errorf("%s: %s names scenario %q that scripts/usability_round.sh does not accept", sh.file, p, s)
			}
		}

		// Every command it quotes exists in the script.
		cmds := cmd.FindAllStringSubmatch(doc, -1)
		if len(cmds) == 0 {
			t.Errorf("%s: quotes no scripts/usability_round.sh command", sh.file)
		}
		for _, m := range cmds {
			if m[1] != "" && !inScript[m[1]] {
				t.Errorf("%s: quotes scenario %q the script does not accept", sh.file, m[1])
			}
		}
		for _, want := range []string{"python3 scripts/product/score_usability.py", "--findings"} {
			if !strings.Contains(doc, want) {
				t.Errorf("%s: scoring command lacks %q", sh.file, want)
			}
		}
		if _, err := os.Stat("../scripts/product/score_usability.py"); err != nil {
			t.Errorf("%s: scorer missing: %v", sh.file, err)
		}

		// Links to the kit: the section it scores with and the timer script.
		for _, a := range []string{kitAnchor(t, kit, sh.kitHead), kitAnchor(t, kit, "## 4.")} {
			if !strings.Contains(doc, "e3-investor-monitor-test.md#"+a) {
				t.Errorf("%s: does not link to the kit's #%s", sh.file, a)
			}
		}
		if strings.Contains(doc, "Thank you for helping") {
			t.Errorf("%s: copies the kit's moderator script; link section 4 instead", sh.file)
		}
		if !strings.Contains(doc, "Triage") {
			t.Errorf("%s: does not say findings go to Triage issue drafts", sh.file)
		}

		// Privacy: no e-mail, phone number or money amount.
		for name, re := range map[string]*regexp.Regexp{
			"e-mail": regexp.MustCompile(`[\w.+-]+@[\w-]+\.[\w.]+`),
			"phone":  regexp.MustCompile(`\+?\d[\d ()-]{8,}\d`),
			"amount": regexp.MustCompile(`(?i)[$€£]\s?\d|R\$|\d[\d.,]*\s?(usd|usdc|eur|brl|dollars?|euros?|reais)\b`),
		} {
			if m := re.FindString(doc); m != "" {
				t.Errorf("%s: holds a %s (%q)", sh.file, name, m)
			}
		}
	}
}
