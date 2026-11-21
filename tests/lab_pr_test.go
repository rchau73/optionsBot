package tests

import (
	"encoding/json"
	"errors"
	"go/parser"
	"go/token"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"optionsbot/internal/compare"
	"optionsbot/internal/config"
)

// #862: cmd/labpr turns a ready lab candidate (cmd/labapply's
// COMPARE_DIR/candidates/<id>/, the shape of testdata/lab_pr_candidate.json)
// into one draft pull request on branch lab/<id>; git and gh go through an
// injected runner, faked here (no network).

// prSetup writes the fixture candidate (changed by edit), its diff.md, its
// config.candidate.yaml (when ready) and the run's report into a new COMPARE_DIR.
func prSetup(t *testing.T, edit func(*compare.CandidateStatus)) string {
	t.Helper()
	var st compare.CandidateStatus
	if err := json.Unmarshal(mustRead(t, "testdata/lab_pr_candidate.json"), &st); err != nil {
		t.Fatal(err)
	}
	if edit != nil {
		edit(&st)
	}
	dir := filepath.Join(t.TempDir(), "compare")
	cand := filepath.Join(dir, "candidates", candID)
	body, _ := json.MarshalIndent(st, "", "  ")
	candWrite(t, filepath.Join(cand, compare.CandidateStatusFile), body)
	candWrite(t, filepath.Join(cand, compare.CandidateDiffFile), []byte(compare.CandidateMarkdown(st)))
	if st.Status == compare.CandidateReady {
		var sets []compare.Setting
		for _, c := range st.Changes {
			s, err := compare.ParseSetting(c.Key + "=" + c.New)
			if err != nil {
				t.Fatal(err)
			}
			sets = append(sets, s)
		}
		edited, err := compare.EditConfigText(mustRead(t, filepath.Join("..", st.Config)), sets)
		if err != nil {
			t.Fatal(err)
		}
		candWrite(t, filepath.Join(cand, compare.CandidateConfigFile), edited)
	}
	candWrite(t, filepath.Join(dir, candID, "report.json"), mustRead(t, "testdata/lab_candidate_report.json"))
	return dir
}

// fakeRunner answers git and gh as GitHub and a clone would, and records
// every command.
type fakeRunner struct {
	t         *testing.T
	repo      string            // where the worktree's configs are copied from
	list      string            // gh pr list's answer
	extra     []string          // more paths git status reports changed
	generated map[string]bool   // paths git check-attr marks generated
	wt        string            // the worktree, once added
	calls     [][]string        // dir, name, args…
	overrides map[string]string // config name → text the worktree holds instead
}

func (f *fakeRunner) Run(dir, name string, args ...string) (string, error) {
	f.calls = append(f.calls, append([]string{dir, name}, args...))
	line := name + " " + strings.Join(args, " ")
	switch {
	case strings.HasPrefix(line, "gh pr list"):
		if f.list == "" {
			return "[]\n", nil
		}
		return f.list, nil
	case strings.HasPrefix(line, "git worktree add"):
		f.wt = args[len(args)-2]
		for _, c := range shippedConfigs {
			body := mustRead(f.t, filepath.Join(f.repo, c))
			if o, ok := f.overrides[c]; ok {
				body = []byte(o)
			}
			candWrite(f.t, filepath.Join(f.wt, c), body)
		}
		return "", nil
	case strings.HasPrefix(line, "git status"):
		var out strings.Builder
		for _, c := range shippedConfigs {
			if string(mustRead(f.t, filepath.Join(f.wt, c))) != string(mustRead(f.t, filepath.Join(f.repo, c))) {
				out.WriteString(" M " + c + "\n")
			}
		}
		for _, p := range f.extra {
			out.WriteString(" M " + p + "\n")
		}
		return out.String(), nil
	case strings.HasPrefix(line, "git check-attr"):
		p := args[len(args)-1]
		if f.generated[p] {
			return p + ": linguist-generated: set\n", nil
		}
		return p + ": linguist-generated: unspecified\n", nil
	case strings.HasPrefix(line, "gh pr create"):
		return "https://github.com/example/optionsbot/pull/901\n", nil
	}
	return "", nil
}

func (f *fakeRunner) ran(prefix string) [][]string {
	var out [][]string
	for _, c := range f.calls {
		if strings.HasPrefix(strings.Join(c[1:], " "), prefix) {
			out = append(out, c)
		}
	}
	return out
}

func newFake(t *testing.T) *fakeRunner {
	return &fakeRunner{t: t, repo: "..", generated: map[string]bool{"tests/testdata/effective_config_btc.json": true}}
}

func prOptions(t *testing.T, dir string) compare.LabPROptions {
	return compare.LabPROptions{ID: candID, CompareDir: dir, RepoDir: "..", WorkDir: t.TempDir()}
}

// The shipped configs are never written by the tool (it writes the worktree).
func snapshotConfigs(t *testing.T) func() {
	before := map[string]string{}
	for _, c := range shippedConfigs {
		before[c] = string(mustRead(t, filepath.Join("..", c)))
	}
	return func() {
		for _, c := range shippedConfigs {
			if string(mustRead(t, filepath.Join("..", c))) != before[c] {
				t.Errorf("%s changed", c)
			}
		}
	}
}

// Scenario: A ready candidate opens one draft PR.
func TestLabPR_ReadyCandidateOpensOneDraftPR(t *testing.T) {
	check := snapshotConfigs(t)
	defer check()
	f := newFake(t)
	f.extra = []string{"tests/testdata/effective_config_btc.json"}
	var out strings.Builder
	o := prOptions(t, prSetup(t, nil))
	res, err := compare.RunLabPR(o, f, &out)
	if err != nil {
		t.Fatal(err)
	}
	if res.State != "opened" || res.Branch != "lab/"+candID || res.URL != "https://github.com/example/optionsbot/pull/901" {
		t.Fatalf("outcome %+v", res)
	}
	want := []string{"config_btc.yaml", "config_btc.mvp.yaml", "tests/testdata/effective_config_btc.json"}
	if strings.Join(res.Files, ",") != strings.Join(want, ",") {
		t.Fatalf("files %v, want %v", res.Files, want)
	}
	for _, c := range []string{"config_btc.yaml", "config_btc.mvp.yaml"} {
		lines := changedLines(t, mustRead(t, filepath.Join("..", c)), mustRead(t, filepath.Join(f.wt, c)))
		if len(lines) != 2 || !strings.HasPrefix(lines[0], "gamma_flip_buffer_sd:      1.2  # shed") || strings.TrimSpace(lines[1]) != "slippage_pct:             0.002" {
			t.Errorf("%s changed lines %q: want only the two recorded values", c, lines)
		}
	}
	if string(mustRead(t, filepath.Join(f.wt, "config_eth.yaml"))) != string(mustRead(t, "../config_eth.yaml")) {
		t.Error("config_eth.yaml changed")
	}
	creates := f.ran("gh pr create")
	if len(creates) != 1 {
		t.Fatalf("gh pr create ran %d times", len(creates))
	}
	args := strings.Join(creates[0][2:], " ")
	for _, w := range []string{"--draft", "--base main", "--head lab/" + candID} {
		if !strings.Contains(args, w) {
			t.Errorf("gh pr create %q lacks %q", args, w)
		}
	}
	body := string(mustRead(t, filepath.Join(o.WorkDir, "body.md")))
	for _, w := range []string{"`" + candID + "`", "| `gamma_flip_buffer_sd` | Gamma flip buffer | 1.5 | 1.2 |", "| `backtest.slippage_pct` | Backtest market slippage | 0.001 | 0.002 |",
		"## Headline numbers: backtested, not a forecast",
		"backtest: return 1.20 % → 1.10 %, worst week -0.80 % → -0.90 %, largest drawdown 1.10 % → 1.30 %",
		"replay of recorded books: return 0.50 % → 0.45 %, worst week -0.40 % → -0.50 %, largest drawdown 1.00 % → 1.50 %",
		"## Fragility", "no fragile spot near the proposed value", "## Risks", "past data only", "Product owner's approval: #900",
		"`" + compare.ManagerHash("maria") + "`", compare.LabPRMergeLine} {
		if !strings.Contains(body, w) {
			t.Errorf("body lacks %q:\n%s", w, body)
		}
	}
	// Nothing merged, nothing pushed to main, nothing restarted or ordered.
	pushes := f.ran("git push")
	if len(pushes) != 1 || pushes[0][len(pushes[0])-1] != "HEAD:refs/heads/lab/"+candID {
		t.Fatalf("pushes %v: want one, of the lab branch only", pushes)
	}
	for _, c := range f.calls {
		joined := strings.Join(c[1:], " ")
		if c[1] == "git" && c[2] == "commit" {
			joined = "git commit" // its message says "the merge is the approval"
		}
		for _, banned := range []string{"merge", "refs/heads/main", "docker", "deploy", "envctl", "promote", "kill"} {
			if strings.Contains(joined, banned) {
				t.Errorf("command %q contains %q", joined, banned)
			}
		}
		if c[1] != "git" && c[1] != "gh" && !(c[1] == "bash" && c[2] == "scripts/regen.sh") {
			t.Errorf("unexpected command %q", joined)
		}
	}
	if len(f.ran("git worktree remove")) != 1 {
		t.Error("the worktree is not removed")
	}
}

// Scenario: A refused or missing candidate opens nothing.
func TestLabPR_RefusedOrMissingCandidateOpensNothing(t *testing.T) {
	cases := map[string]struct {
		dir  func(t *testing.T) string
		want string
	}{
		"refused": {func(t *testing.T) string {
			return prSetup(t, func(st *compare.CandidateStatus) {
				st.Status, st.Candidate, st.Reasons = compare.CandidateRefused, "", []string{"backtest.slippage_pct: 0.9 must be at most 0.05"}
			})
		}, "must be at most 0.05"},
		"no candidate": {func(t *testing.T) string { return t.TempDir() }, "run cmd/labapply -id " + candID + " first"},
		"no diff": {func(t *testing.T) string {
			d := prSetup(t, nil)
			if err := os.Remove(filepath.Join(d, "candidates", candID, compare.CandidateDiffFile)); err != nil {
				t.Fatal(err)
			}
			return d
		}, "no diff.md"},
		"core policy without approval": {func(t *testing.T) string {
			return prSetup(t, func(st *compare.CandidateStatus) { st.ProductOwnerApproval = "" })
		}, "gamma_flip_buffer_sd: needs_product_owner"},
		"shipped changed since": {func(t *testing.T) string {
			d := prSetup(t, nil)
			p := filepath.Join(d, "candidates", candID, compare.CandidateConfigFile)
			candWrite(t, p, append(mustRead(t, p), []byte("# edited\n")...))
			return d
		}, "changed since the candidate was built"},
		"other run": {func(t *testing.T) string {
			return prSetup(t, func(st *compare.CandidateStatus) { st.RunID = "20261101T000000Z" })
		}, "not \"" + candID + "\""},
	}
	for name, c := range cases {
		t.Run(name, func(t *testing.T) {
			f := newFake(t)
			_, err := compare.RunLabPR(prOptions(t, c.dir(t)), f, &strings.Builder{})
			var refused *compare.LabPRRefusal
			if !errors.As(err, &refused) || !strings.Contains(err.Error(), c.want) {
				t.Fatalf("error %v: want a refusal naming %q", err, c.want)
			}
			if len(f.calls) != 0 {
				t.Fatalf("commands ran: %v", f.calls)
			}
		})
	}
	t.Run("bad id", func(t *testing.T) {
		f := newFake(t)
		o := prOptions(t, prSetup(t, nil))
		o.ID = "main"
		if _, err := compare.RunLabPR(o, f, &strings.Builder{}); err == nil || len(f.calls) != 0 {
			t.Fatalf("id main: error %v, calls %v", err, f.calls)
		}
	})
}

// The command exits 2 with the reasons on a refused candidate, 0 on a dry run.
func TestLabPR_CommandExitCodes(t *testing.T) {
	bin := filepath.Join(t.TempDir(), "labpr")
	if out, err := exec.Command("go", "build", "-o", bin, "../cmd/labpr").CombinedOutput(); err != nil {
		t.Fatalf("build labpr: %v\n%s", err, out)
	}
	run := func(args ...string) (string, int) {
		out, err := exec.Command(bin, args...).CombinedOutput()
		var ee *exec.ExitError
		if errors.As(err, &ee) {
			return string(out), ee.ExitCode()
		} else if err != nil {
			t.Fatal(err)
		}
		return string(out), 0
	}
	if out, code := run("-id", candID, "-compare", t.TempDir(), "-repo", ".."); code != 2 || !strings.Contains(out, "run cmd/labapply") {
		t.Fatalf("missing candidate: exit %d\n%s", code, out)
	}
	if out, code := run("-id", candID, "-compare", prSetup(t, nil), "-repo", "..", "-dry-run"); code != 0 || !strings.Contains(out, "branch: lab/"+candID) {
		t.Fatalf("dry run: exit %d\n%s", code, out)
	}
}

// Scenario: Running it twice does not duplicate.
func TestLabPR_RunningTwiceDoesNotDuplicate(t *testing.T) {
	dir := prSetup(t, nil)
	for state, want := range map[string]string{"OPEN": "open", "MERGED": "merged"} {
		f := newFake(t)
		f.list = `[{"number":901,"url":"https://github.com/example/optionsbot/pull/901","state":"` + state + `"}]`
		var out strings.Builder
		res, err := compare.RunLabPR(prOptions(t, dir), f, &out)
		if err != nil || res.State != want || res.URL != "https://github.com/example/optionsbot/pull/901" {
			t.Fatalf("%s: outcome %+v, error %v", state, res, err)
		}
		if len(f.calls) != 1 || !strings.Contains(out.String(), "pull/901") {
			t.Fatalf("%s: calls %v, output %q: want only the list and the PR reported", state, f.calls, out.String())
		}
	}
	f := newFake(t)
	f.list = `[{"number":901,"url":"https://github.com/example/optionsbot/pull/901","state":"CLOSED"}]`
	_, err := compare.RunLabPR(prOptions(t, dir), f, &strings.Builder{})
	var refused *compare.LabPRRefusal
	if !errors.As(err, &refused) || len(f.ran("gh pr create")) != 0 {
		t.Fatalf("closed PR: error %v, calls %v", err, f.calls)
	}
}

// Scenario: Dry run touches nothing.
func TestLabPR_DryRunTouchesNothing(t *testing.T) {
	check := snapshotConfigs(t)
	defer check()
	dir := prSetup(t, nil)
	before := map[string]string{}
	filepath.Walk(dir, func(p string, info os.FileInfo, err error) error {
		if err == nil && !info.IsDir() {
			before[p] = string(mustRead(t, p))
		}
		return nil
	})
	f := newFake(t)
	var out strings.Builder
	o := compare.LabPROptions{ID: candID, CompareDir: dir, RepoDir: "..", DryRun: true}
	res, err := compare.RunLabPR(o, f, &out)
	if err != nil || res.State != "dry-run" {
		t.Fatalf("outcome %+v, error %v", res, err)
	}
	if len(f.calls) != 0 {
		t.Fatalf("commands ran: %v", f.calls)
	}
	for _, w := range []string{"branch: lab/" + candID, "--- a/config_btc.yaml", "--- a/config_btc.mvp.yaml",
		"-gamma_flip_buffer_sd:      1.5", "+gamma_flip_buffer_sd:      1.2", "+  slippage_pct:             0.002",
		"backtested, not a forecast", compare.LabPRMergeLine} {
		if !strings.Contains(out.String(), w) {
			t.Errorf("dry run lacks %q:\n%s", w, out.String())
		}
	}
	after := 0
	filepath.Walk(dir, func(p string, info os.FileInfo, err error) error {
		if err == nil && !info.IsDir() {
			after++
			if string(mustRead(t, p)) != before[p] {
				t.Errorf("%s changed", p)
			}
		}
		return nil
	})
	if after != len(before) {
		t.Errorf("files %d → %d", len(before), after)
	}
}

// Scenario: Nothing private reaches the public repository.
func TestLabPR_NothingPrivateInBody(t *testing.T) {
	plan, err := compare.PlanLabPR(prSetup(t, nil), "..", candID)
	if err != nil {
		t.Fatal(err)
	}
	text := plan.Title + "\n" + plan.Body
	for _, banned := range []string{"maria", "Maria", "token", "USD", "$", "account id", "client_id", "net_usd", "1200", "1100"} {
		if strings.Contains(text, banned) {
			t.Errorf("PR text holds %q:\n%s", banned, text)
		}
	}
	if !strings.Contains(text, "manager `"+compare.ManagerHash("maria")+"`") || len(compare.ManagerHash("maria")) != 8 {
		t.Errorf("the requester is not the 8-character hash:\n%s", text)
	}
	if strings.Contains(text, "analysis") {
		t.Errorf("the AI analysis reached the body (withheld until L10):\n%s", text)
	}
	for _, c := range []struct {
		text string
		want bool
	}{{"fees 250 USD", true}, {"cost $100", true}, {"0.5 BTC of premium", true}, {"by maria at noon", true},
		{"return 1.10 % (BTC)", false}, {"mariana's note", false}, {"worst week -0.90 %", false}} {
		if got := len(compare.LabPRPrivacyReasons(c.text, "maria")) > 0; got != c.want {
			t.Errorf("%q: refused %v, want %v", c.text, got, c.want)
		}
	}
	// A candidate whose setting title carries the person's name is refused.
	_, err = compare.PlanLabPR(prSetup(t, func(st *compare.CandidateStatus) { st.Changes[0].Title = "maria's slippage" }), "..", candID)
	if err == nil || !strings.Contains(err.Error(), "names the person") {
		t.Fatalf("a body naming the person: %v", err)
	}
	// The package code runs no process and calls no network or exchange; only
	// the command runs git and gh.
	for file, banned := range map[string][]string{
		"../internal/compare/labpr.go": {"net", "os/exec", "optionsbot/internal/gateway", "optionsbot/internal/exchange", "optionsbot/internal/strategy", "optionsbot/internal/aianalysis"},
		"../cmd/labpr/main.go":         {"net", "optionsbot/internal/gateway", "optionsbot/internal/exchange", "optionsbot/internal/strategy", "optionsbot/internal/aianalysis"},
	} {
		f, err := parser.ParseFile(token.NewFileSet(), file, nil, parser.ImportsOnly)
		if err != nil {
			t.Fatal(err)
		}
		for _, imp := range f.Imports {
			p, _ := strconv.Unquote(imp.Path.Value)
			for _, b := range banned {
				if p == b || strings.HasPrefix(p, b+"/") {
					t.Errorf("%s imports %s", file, p)
				}
			}
		}
	}
}

// Scenario: Both shipped configs stay in step.
func TestLabPR_BothShippedConfigsStayInStep(t *testing.T) {
	plan, err := compare.PlanLabPR(prSetup(t, nil), "..", candID)
	if err != nil {
		t.Fatal(err)
	}
	if len(plan.Files) != 2 || plan.Files[0].Path != "config_btc.yaml" || plan.Files[1].Path != "config_btc.mvp.yaml" {
		t.Fatalf("files %v", plan.Files)
	}
	// TestConfig_MVPBTCDiffersOnlyInDirection's check on the branch's files.
	dir := t.TempDir()
	var loaded []*config.Config
	for _, f := range plan.Files {
		p := filepath.Join(dir, f.Path)
		candWrite(t, p, f.New)
		cfg, err := config.Load(p)
		if err != nil {
			t.Fatal(err)
		}
		loaded = append(loaded, cfg)
	}
	if loaded[0].GammaFlipBufferSD != 1.2 || loaded[1].GammaFlipBufferSD != 1.2 {
		t.Fatalf("gamma_flip_buffer_sd %v / %v", loaded[0].GammaFlipBufferSD, loaded[1].GammaFlipBufferSD)
	}
	loaded[1].GEXDirection = loaded[0].GEXDirection
	a, _ := json.Marshal(loaded[0])
	b, _ := json.Marshal(loaded[1])
	if string(a) != string(b) {
		t.Errorf("the twins differ in more than gex_direction:\n%s\n%s", a, b)
	}

	// Starting from mvp's file, the stable twin gets the change too.
	mvp, err := compare.PlanLabPR(prSetup(t, func(st *compare.CandidateStatus) { st.Config = "config_btc.mvp.yaml" }), "..", candID)
	if err != nil || len(mvp.Files) != 2 || mvp.Files[1].Path != "config_btc.yaml" {
		t.Fatalf("mvp first: %v, %v", mvp.Files, err)
	}
	// gex_direction is the one key the twins hold differently: only the named file.
	gex, err := compare.PlanLabPR(prSetup(t, func(st *compare.CandidateStatus) {
		st.Config = "config_btc.mvp.yaml"
		st.Changes = []compare.CandidateChange{{Key: "gex_direction", Title: "Direction source", Old: "sma", New: "bias", CorePolicy: true, Policy: "gex"}}
	}), "..", candID)
	if err != nil || len(gex.Files) != 1 || gex.Files[0].Path != "config_btc.mvp.yaml" {
		t.Fatalf("gex_direction: %v, %v", gex.Files, err)
	}
	// ETH has no twin.
	eth, err := compare.PlanLabPR(prSetup(t, func(st *compare.CandidateStatus) { st.Config, st.Underlying = "config_eth.yaml", "ETH" }), "..", candID)
	if err != nil || len(eth.Files) != 1 {
		t.Fatalf("eth: %v, %v", eth.Files, err)
	}
	// A twin out of step is refused, not overwritten.
	repo := t.TempDir()
	for _, c := range shippedConfigs {
		body := string(mustRead(t, filepath.Join("..", c)))
		if c == "config_btc.mvp.yaml" {
			body = strings.Replace(body, "gamma_flip_buffer_sd:      1.5", "gamma_flip_buffer_sd:      1.4", 1)
		}
		candWrite(t, filepath.Join(repo, c), []byte(body))
	}
	_, err = compare.PlanLabPR(prSetup(t, nil), repo, candID)
	var refused *compare.LabPRRefusal
	if !errors.As(err, &refused) || !strings.Contains(err.Error(), "config_btc.mvp.yaml holds 1.4") {
		t.Fatalf("twin out of step: %v", err)
	}
	// Only the configs and generated files are committed.
	f := newFake(t)
	f.extra = []string{"internal/strategy/limits.go"}
	if _, err := compare.RunLabPR(prOptions(t, prSetup(t, nil)), f, &strings.Builder{}); err == nil || len(f.ran("git commit")) != 0 {
		t.Fatalf("an unexpected file: error %v, commits %v", err, f.ran("git commit"))
	}
}
