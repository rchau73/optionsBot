package tests

import (
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"testing"

	"gopkg.in/yaml.v3"
)

// CI picks its jobs with scripts/agents/checkfail.py suites (#472): the
// `changes` job prints one output per suite, and every job but the always-on
// ones runs only when its suite's output is true. A suite renamed on one side
// would skip a job silently.

type ciWorkflow struct {
	On   map[string]any `yaml:"on"`
	Jobs map[string]struct {
		Name    string            `yaml:"name"`
		If      string            `yaml:"if"`
		Outputs map[string]string `yaml:"outputs"`
	} `yaml:"jobs"`
}

func readCIWorkflow(t *testing.T) ciWorkflow {
	t.Helper()
	b, err := os.ReadFile(filepath.Join("..", ".github", "workflows", "ci.yml"))
	if err != nil {
		t.Fatal(err)
	}
	var w ciWorkflow
	if err := yaml.Unmarshal(b, &w); err != nil {
		t.Fatalf("ci.yml does not parse: %v", err)
	}
	return w
}

func checkfailSuites(t *testing.T) []string {
	t.Helper()
	b, err := os.ReadFile(filepath.Join("..", "scripts", "agents", "checkfail.py"))
	if err != nil {
		t.Fatal(err)
	}
	m := regexp.MustCompile(`(?m)^SUITES = \(([^)]*)\)`).FindSubmatch(b)
	if m == nil {
		t.Fatal("no SUITES = (...) in checkfail.py")
	}
	var out []string
	for _, q := range regexp.MustCompile(`"(\w+)"`).FindAllSubmatch(m[1], -1) {
		out = append(out, string(q[1]))
	}
	sort.Strings(out)
	return out
}

func TestCIWorkflow_JobsRunByTheSuitesCheckfailPicks(t *testing.T) {
	w := readCIWorkflow(t)
	suites := checkfailSuites(t)
	var outputs []string
	for k := range w.Jobs["changes"].Outputs {
		outputs = append(outputs, k)
	}
	sort.Strings(outputs)
	if strings.Join(outputs, ",") != strings.Join(suites, ",") {
		t.Fatalf("changes outputs %v, checkfail.py SUITES %v", outputs, suites)
	}
	// Always on: the filter itself, the secret scan, the required check (it
	// reads every job's result).
	always := map[string]bool{"changes": true, "secrets": true, "test": true}
	gate := regexp.MustCompile(`needs\.changes\.outputs\.(\w+) == 'true'`)
	for id, job := range w.Jobs {
		if always[id] {
			continue
		}
		m := gate.FindStringSubmatch(job.If)
		if m == nil {
			t.Errorf("job %s runs on every change: gate it on a checkfail.py suite (if: %q)", id, job.If)
			continue
		}
		if _, ok := w.Jobs["changes"].Outputs[m[1]]; !ok {
			t.Errorf("job %s is gated on %q, which the changes job does not output", id, m[1])
		}
	}
}

// The monitor's Node.js major is pinned in three places that move together
// (#522): the Docker image it runs in, CI's frontend job and package.json's
// engines. minFrontendNode is the oldest supported LTS (Node 20 reached end of
// life on 2026-04-30).
const minFrontendNode = 24

func TestCIWorkflow_FrontendNodeVersionPinnedTogether(t *testing.T) {
	read := func(parts ...string) string {
		b, err := os.ReadFile(filepath.Join(append([]string{".."}, parts...)...))
		if err != nil {
			t.Fatal(err)
		}
		return string(b)
	}
	majors := map[string][]string{
		"frontend/Dockerfile FROM": regexpAll(`(?m)^FROM node:(\d+)`, read("frontend", "Dockerfile")),
		"ci.yml node-version":      regexpAll(`(?m)^\s*node-version:\s*['"]?(\d+)`, read(".github", "workflows", "ci.yml")),
		"package.json engines":     regexpAll(`"node":\s*">=(\d+)`, read("frontend", "package.json")),
	}
	want := strconv.Itoa(minFrontendNode)
	for where, got := range majors {
		if len(got) == 0 {
			t.Errorf("%s: no Node version found", where)
		}
		for _, v := range got {
			if v != want {
				t.Errorf("%s pins Node %s, want %s: the Dockerfile, ci.yml and package.json move together", where, v, want)
			}
		}
	}
}

func regexpAll(pattern, s string) []string {
	var out []string
	for _, m := range regexp.MustCompile(pattern).FindAllStringSubmatch(s, -1) {
		out = append(out, m[1])
	}
	return out
}

func TestCIWorkflow_RequiredCheckAndNightlyRun(t *testing.T) {
	w := readCIWorkflow(t)
	// The PR gate re-runs a red CI by this name (checkfail.py AGGREGATOR).
	if got := w.Jobs["test"].Name; got != "Lint, test & coverage" {
		t.Errorf("required check renamed to %q: update setup_github.sh and checkfail.py AGGREGATOR", got)
	}
	if got := w.Jobs["test"].If; got != "always()" {
		t.Errorf("the required check must always run, got if: %q", got)
	}
	// A docs-only change runs the doc-wide checks checkfail.py names.
	ci, _ := os.ReadFile(filepath.Join("..", ".github", "workflows", "ci.yml"))
	cf, _ := os.ReadFile(filepath.Join("..", "scripts", "agents", "checkfail.py"))
	const docRun = `^(TestClaudeDocs|TestDocs_)`
	if !strings.Contains(string(ci), "-run '"+docRun+"'") || !strings.Contains(string(cf), `DOC_TESTS_RUN = "`+docRun+`"`) {
		t.Errorf("ci.yml's doc-check step and checkfail.py DOC_TESTS_RUN must both run %s", docRun)
	}
	// A path filter skips jobs on pull requests; main runs every job on each
	// push and nightly, so no suite runs less often there.
	for _, ev := range []string{"push", "schedule", "pull_request"} {
		if _, ok := w.On[ev]; !ok {
			t.Errorf("ci.yml does not run on %s", ev)
		}
	}
}
