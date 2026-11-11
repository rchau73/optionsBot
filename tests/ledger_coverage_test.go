package tests

import (
	"fmt"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"testing"
)

// Run ledger coverage (#722): every agent script under scripts/agents/ writes a
// start and an end line to agent_runs.jsonl (lib.sh run_begin / run_end; the
// EXIT trap lib.sh installs ends a run that crashes), or it is on ledgerExempt
// with the reason it writes none. Every agent name a script passes to run_begin
// is one runs.py accepts (make_line refuses any other, and the line is skipped)
// and is documented in the run-ledger doc and the folder's CLAUDE.md. It reads
// the scripts' code with the pipe guard's shellCode, so a comment never counts.

// ledgerExempt lists the scripts that write no run, each with its reason.
var ledgerExempt = map[string]string{
	"branch_work.sh":    "a helper dev_agent.sh runs inside its own dev run",
	"install.sh":        "a person runs it once to schedule the jobs",
	"lib.sh":            "the library that defines run_begin and run_end, sourced by every agent",
	"limit_lib.sh":      "a library (budget and limits), sourced by the tick and the agents",
	"rescue_commits.sh": "a helper dev_agent.sh runs inside its own dev run",
	"setup_github.sh":   "a person runs it once to set up the labels and the board",
	"sweep_runs.sh":     "it closes the ledger's dead runs itself, under the agent-runs mutex",
	"tick.sh":           "the scheduler: each script it starts opens its own run, a tick with no work writes nothing",
}

var (
	ledgerBegin    = regexp.MustCompile(`\brun_begin\s+([a-z_]+)`)
	ledgerEnd      = regexp.MustCompile(`\brun_end\s`)
	ledgerSourced  = regexp.MustCompile(`^\s*(?:source|\.)\s.*\blib\.sh\b`)
	ledgerAgentsPy = regexp.MustCompile(`(?s)\nAGENTS = \(([^)]*)\)`)
	ledgerQuoted   = regexp.MustCompile(`"([a-z_]+)"`)
)

// ledgerCode is a script's code without its comments.
func ledgerCode(text string) string {
	var b strings.Builder
	for _, line := range strings.Split(text, "\n") {
		code, _ := shellCode(line)
		b.WriteString(code + "\n")
	}
	return b.String()
}

// ledgerProblems lists what breaks the rule: scripts maps a file name to its
// text, docs a document's name to its text, known the names runs.py accepts.
func ledgerProblems(scripts map[string]string, exempt map[string]string, docs map[string]string, known map[string]bool) []string {
	var problems []string
	agents := map[string]string{} // agent → the first script that names it
	names := make([]string, 0, len(scripts))
	for name := range scripts {
		names = append(names, name)
	}
	sort.Strings(names)
	for _, name := range names {
		code := ledgerCode(scripts[name])
		begins := ledgerBegin.FindAllStringSubmatch(code, -1)
		for _, m := range begins {
			if _, seen := agents[m[1]]; !seen {
				agents[m[1]] = name
			}
		}
		if _, ok := exempt[name]; ok {
			if len(begins) > 0 {
				problems = append(problems, fmt.Sprintf("%s is on ledgerExempt but calls run_begin: take it off the list", name))
			}
			continue
		}
		if len(begins) == 0 {
			problems = append(problems, fmt.Sprintf("%s: no run_begin — open a run, or add it to ledgerExempt with a reason", name))
		}
		if !ledgerEnd.MatchString(code) {
			problems = append(problems, fmt.Sprintf("%s: no run_end — end the run with its outcome, or add it to ledgerExempt with a reason", name))
		}
		sourced := false
		for _, line := range strings.Split(scripts[name], "\n") {
			sourced = sourced || ledgerSourced.MatchString(line)
		}
		if !sourced {
			problems = append(problems, fmt.Sprintf("%s: does not source lib.sh (run_begin, run_end and the EXIT trap)", name))
		}
	}
	exemptNames := make([]string, 0, len(exempt))
	for name := range exempt {
		exemptNames = append(exemptNames, name)
	}
	sort.Strings(exemptNames)
	for _, name := range exemptNames {
		if _, ok := scripts[name]; !ok {
			problems = append(problems, fmt.Sprintf("ledgerExempt names %s, which no longer exists", name))
		}
		if strings.TrimSpace(exempt[name]) == "" {
			problems = append(problems, fmt.Sprintf("ledgerExempt: %s has no reason", name))
		}
	}
	agentNames := make([]string, 0, len(agents))
	for agent := range agents {
		agentNames = append(agentNames, agent)
	}
	sort.Strings(agentNames)
	docNames := make([]string, 0, len(docs))
	for doc := range docs {
		docNames = append(docNames, doc)
	}
	sort.Strings(docNames)
	for _, agent := range agentNames {
		if !known[agent] {
			problems = append(problems, fmt.Sprintf("agent %s (%s) is not in runs.py AGENTS: its lines are refused", agent, agents[agent]))
		}
		for _, doc := range docNames {
			if !strings.Contains(docs[doc], "`"+agent+"`") {
				problems = append(problems, fmt.Sprintf("agent %s (%s) is not documented in %s", agent, agents[agent], doc))
			}
		}
	}
	return problems
}

// ledgerBullet is the Run ledger bullet of scripts/agents/CLAUDE.md, up to the next top-level bullet.
func ledgerBullet(t *testing.T, text string) string {
	t.Helper()
	start := strings.Index(text, "\n- Run ledger (")
	if start < 0 {
		t.Fatal("no Run ledger bullet in scripts/agents/CLAUDE.md")
	}
	rest := text[start+1:]
	if end := strings.Index(rest[1:], "\n- "); end >= 0 {
		rest = rest[:end+1]
	}
	return rest
}

// ledgerRepo reads the repository's agent scripts, docs and runs.py AGENTS.
func ledgerRepo(t *testing.T) (scripts, docs map[string]string, known map[string]bool) {
	t.Helper()
	files, err := filepath.Glob("../scripts/agents/*.sh")
	if err != nil {
		t.Fatal(err)
	}
	scripts = map[string]string{}
	for _, f := range files {
		scripts[filepath.Base(f)] = readFile(t, f)
	}
	docs = map[string]string{
		"docs/agents/run-ledger.md":                    readFile(t, "../docs/agents/run-ledger.md"),
		"scripts/agents/CLAUDE.md (Run ledger bullet)": ledgerBullet(t, readFile(t, "../scripts/agents/CLAUDE.md")),
	}
	m := ledgerAgentsPy.FindStringSubmatch(readFile(t, "../scripts/agents/runs.py"))
	if m == nil {
		t.Fatal("no AGENTS tuple in scripts/agents/runs.py")
	}
	known = map[string]bool{}
	for _, q := range ledgerQuoted.FindAllStringSubmatch(m[1], -1) {
		known[q[1]] = true
	}
	return scripts, docs, known
}

const ledgerLib = "set -euo pipefail\nsource \"$(dirname \"${BASH_SOURCE[0]}\")/lib.sh\"\n"

func ledgerFixture() (scripts, docs map[string]string, known map[string]bool) {
	scripts = map[string]string{
		"lib.sh":  "run_begin() { :; }\n",
		"good.sh": ledgerLib + "run_begin good \"\" \"\" \"$dir\"\nrun_end done \"x\"\n",
	}
	docs = map[string]string{"run-ledger.md": "`good`", "CLAUDE.md": "`good`"}
	return scripts, docs, map[string]bool{"good": true}
}

func ledgerWant(t *testing.T, problems []string, want ...string) {
	t.Helper()
	if len(problems) != len(want) {
		t.Fatalf("problems = %q, want %d: %q", problems, len(want), want)
	}
	for i, w := range want {
		if !strings.Contains(problems[i], w) {
			t.Errorf("problem %d = %q, want it to contain %q", i, problems[i], w)
		}
	}
}

// Scenario: A new agent script without ledger calls — the test fails and names the script.
func TestLedgerCoverage_NewScriptWithoutLedgerCalls(t *testing.T) {
	exempt := map[string]string{"lib.sh": "the library"}
	for _, c := range []struct {
		name, script string
		want         []string
	}{
		{"neither", ledgerLib + "gh issue comment 1 --body x\n", []string{"new.sh: no run_begin", "new.sh: no run_end"}},
		{"no run_end", ledgerLib + "run_begin good\n", []string{"new.sh: no run_end"}},
		{"no run_begin", ledgerLib + "run_end done\n", []string{"new.sh: no run_begin"}},
		{"only in a comment", ledgerLib + "# run_begin good / run_end done\n", []string{"new.sh: no run_begin", "new.sh: no run_end"}},
		{"lib.sh not sourced", "run_begin good\nrun_end done\n", []string{"new.sh: does not source lib.sh"}},
	} {
		t.Run(c.name, func(t *testing.T) {
			scripts, docs, known := ledgerFixture()
			scripts["new.sh"] = c.script
			ledgerWant(t, ledgerProblems(scripts, exempt, docs, known), c.want...)
		})
	}
}

// Scenario: An exempt script — it passes, and fails if the file no longer exists
// (or has no reason, or writes runs after all).
func TestLedgerCoverage_ExemptScript(t *testing.T) {
	scripts, docs, known := ledgerFixture()
	scripts["helper.sh"] = "echo helper\n"
	exempt := map[string]string{"lib.sh": "the library", "helper.sh": "a helper run inside a run"}
	ledgerWant(t, ledgerProblems(scripts, exempt, docs, known))

	delete(scripts, "helper.sh")
	ledgerWant(t, ledgerProblems(scripts, exempt, docs, known), "ledgerExempt names helper.sh, which no longer exists")

	scripts["helper.sh"] = "echo helper\n"
	exempt["helper.sh"] = " "
	ledgerWant(t, ledgerProblems(scripts, exempt, docs, known), "ledgerExempt: helper.sh has no reason")

	exempt["helper.sh"] = "a helper"
	scripts["helper.sh"] = ledgerLib + "run_begin good\nrun_end done\n"
	ledgerWant(t, ledgerProblems(scripts, exempt, docs, known), "helper.sh is on ledgerExempt but calls run_begin")
}

// Scenario: An undocumented agent name — the test fails and names the agent.
func TestLedgerCoverage_UndocumentedAgent(t *testing.T) {
	scripts, docs, known := ledgerFixture()
	scripts["other.sh"] = ledgerLib + "run_begin other 12\nrun_end done\n"
	known["other"] = true
	docs["CLAUDE.md"] += " `other`"
	ledgerWant(t, ledgerProblems(scripts, map[string]string{"lib.sh": "lib"}, docs, known),
		"agent other (other.sh) is not documented in run-ledger.md")

	delete(known, "other")
	docs["run-ledger.md"] += " `other`"
	ledgerWant(t, ledgerProblems(scripts, map[string]string{"lib.sh": "lib"}, docs, known),
		"agent other (other.sh) is not in runs.py AGENTS")

	docs["run-ledger.md"] = "`good` other" // a bare word is no documented name
	known["other"] = true
	ledgerWant(t, ledgerProblems(scripts, map[string]string{"lib.sh": "lib"}, docs, known),
		"agent other (other.sh) is not documented in run-ledger.md")
}

// Scenario: Existing scripts — the scripts on main pass, the gaps found fixed in #722.
func TestLedgerCoverage_RepositoryScripts(t *testing.T) {
	scripts, docs, known := ledgerRepo(t)
	if len(scripts) < 20 || len(known) < 15 {
		t.Fatalf("read %d scripts and %d agent names: wrong folder?", len(scripts), len(known))
	}
	for _, p := range ledgerProblems(scripts, ledgerExempt, docs, known) {
		t.Error(p)
	}
}

// Scenario: Ticks that do no work — each script opens its run only after its
// early exits (the done-marker, the due check, the lock, nothing to move).
func TestLedgerCoverage_IdleTicksWriteNoRun(t *testing.T) {
	for script, before := range map[string][]string{
		"metrics.sh":  {`[[ -f "$report" ]] && exit 0`, "take_lock metrics || exit 0"},
		"evidence.sh": {`[[ -f "$done_marker" ]] && exit 0`, "take_lock evidence || exit 0"},
		"go_no_go.sh": {`[[ -f "$done_marker" ]] && exit 0`, "take_lock go_no_go || exit 0"},
		"stuck.sh":    {`python3 "$STUCK_PY" due "$state" || exit 0`, "take_lock stuck || exit 0"},
		"requeue.sh":  {"take_lock requeue || exit 0", `[[ -n "$nums" ]] || exit 0`, `[[ -n "$n" ]] || continue`},
	} {
		lines := strings.Split(readFile(t, "../scripts/agents/"+script), "\n")
		begin := -1
		for i, line := range lines {
			if code, _ := shellCode(line); begin < 0 && ledgerBegin.MatchString(code) {
				begin = i
			}
		}
		if begin < 0 {
			t.Errorf("%s: no run_begin", script)
			continue
		}
		for _, exit := range before {
			at := -1
			for i, line := range lines {
				if at < 0 && strings.Contains(line, exit) {
					at = i
				}
			}
			if at < 0 || at > begin {
				t.Errorf("%s: %q must come before run_begin (line %d, run_begin on line %d)", script, exit, at+1, begin+1)
			}
		}
	}
}
