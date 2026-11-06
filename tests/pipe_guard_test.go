package tests

import (
	"fmt"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"testing"

	"gopkg.in/yaml.v3"
)

// The pipe guard (#683, after #659): a pipeline reports only its last command's
// status, so `check | tee log` passes while the check fails. This scan reads the
// workflows' run: blocks, scripts/**/*.sh and .githooks/* and refuses a pipeline
// whose first command can fail and that runs into a filter, unless one holds:
//   - pipefail is on: `set -o pipefail` (or -euo) above it in the same block or
//     file, or the step runs under `shell: bash`, which GitHub starts with
//     -eo pipefail (a step with no shell gets bash -e, without it);
//   - it carries `# pipe-ok: <why>` on its own line or the line before;
//   - its first command is a pure producer (pipeProducers).
//
// It is a line scan, not a shell parser. A quoted string spanning lines or a
// heredoc can read as a pipeline: a pipe-ok line settles that false alarm. Each
// `set` line counts wherever it sits, even inside an if.

var (
	// Commands that only print what they are given.
	pipeProducers = map[string]bool{"cat": true, "echo": true, "printf": true}
	// The commands whose exit status replaces the failure on their left. Any other
	// right side (`| while read`, `| python3 …`) decides for itself and is not flagged.
	pipeFilters = map[string]bool{
		"awk": true, "cut": true, "grep": true, "head": true, "jq": true, "sed": true,
		"sort": true, "tail": true, "tee": true, "tr": true, "uniq": true, "wc": true,
	}
	// Words that can precede a pipeline's first command.
	shellPrefixes = map[string]bool{
		"!": true, "do": true, "elif": true, "else": true, "if": true, "then": true,
		"time": true, "until": true, "while": true, "{": true,
	}
	// Sourced, never run: they take pipefail from their caller's shell, so setting
	// it here is not needed and each `source` of one must be under pipefail.
	pipefailLibraries = map[string]bool{"env_lib.sh": true, "lib.sh": true, "limit_lib.sh": true}

	pipefailOn     = regexp.MustCompile(`^\s*set\s.*-[a-zA-Z]*o\s+pipefail\b`)
	pipefailOff    = regexp.MustCompile(`^\s*set\s.*\+[a-zA-Z]*o\s+pipefail\b`)
	pipeOK         = regexp.MustCompile(`#\s*pipe-ok:\s*\S`)
	shellSeparator = regexp.MustCompile("&&|\\|\\||\\$\\(|[;()`]")
	pipeStage      = regexp.MustCompile(`\|&?`)
	shellAssign    = regexp.MustCompile(`^\w+=`)
	sourceLine     = regexp.MustCompile(`^\s*(?:source|\.)\s.*?([\w-]+\.sh)\b`)
)

// pipeHit is one pipeline that can hide a failure.
type pipeHit struct {
	file     string
	line     int
	pipeline string
}

func (h pipeHit) String() string {
	return fmt.Sprintf("%s:%d: %s", h.file, h.line, h.pipeline)
}

// shellCode splits a line into its code and its comment. The code drops quoted
// text, where a | is no pipe, but keeps a $(...) inside double quotes, where it is.
func shellCode(line string) (code, comment string) {
	var b strings.Builder
	var open []byte // the double quotes and parentheses open here, innermost last
	top := func() byte {
		if len(open) == 0 {
			return 0
		}
		return open[len(open)-1]
	}
	for i := 0; i < len(line); i++ {
		c := line[i]
		switch {
		case c == '\\':
			if i == len(line)-1 {
				b.WriteByte(c) // a line continuation, for pipelineContinues
			}
			i++
		case top() == '"':
			if c == '"' {
				open = open[:len(open)-1]
			} else if strings.HasPrefix(line[i:], "$(") {
				open = append(open, '(')
				b.WriteString("$(")
				i++
			}
		case c == '\'':
			end := strings.IndexByte(line[i+1:], '\'')
			if end < 0 {
				return b.String(), ""
			}
			i += end + 1
		case c == '"':
			open = append(open, '"')
		case c == '#' && (i == 0 || line[i-1] == ' ' || line[i-1] == '\t'):
			return b.String(), line[i:]
		default:
			if c == '(' {
				open = append(open, '(')
			} else if c == ')' && top() == '(' {
				open = open[:len(open)-1]
			}
			b.WriteByte(c)
		}
	}
	return b.String(), ""
}

// pipelineContinues says the next line belongs to this one.
func pipelineContinues(code string) bool {
	code = strings.TrimSpace(code)
	return strings.HasSuffix(code, `\`) || (strings.HasSuffix(code, "|") && !strings.HasSuffix(code, "||"))
}

// shellCommand is a stage's command word; "" or a closing word (done, }) means the
// left side is a compound whose commands the scan cannot see, so it is no producer.
func shellCommand(stage string) string {
	for _, word := range strings.Fields(stage) {
		if !shellPrefixes[word] && !shellAssign.MatchString(word) {
			return word
		}
	}
	return ""
}

// unsafePipeline reads one command between separators.
func unsafePipeline(segment string) bool {
	stages := pipeStage.Split(segment, -1)
	if len(stages) < 2 || pipeProducers[shellCommand(stages[0])] {
		return false
	}
	for _, stage := range stages[1:] {
		if pipeFilters[shellCommand(stage)] {
			return true
		}
	}
	return false
}

// scanShell lists the unsafe pipelines of a block whose first line is firstLine
// in file, given whether pipefail is on when it starts. Sourcing a library with
// pipefail off is a hit too: every pipeline in the library would run without it.
func scanShell(file, text string, firstLine int, pipefail bool) []pipeHit {
	var hits []pipeHit
	lines := strings.Split(text, "\n")
	for i := 0; i < len(lines); i++ {
		start := i
		code, comment := shellCode(lines[i])
		explained := pipeOK.MatchString(comment)
		if start > 0 {
			_, above := shellCode(lines[start-1])
			explained = explained || pipeOK.MatchString(above)
		}
		for pipelineContinues(code) && i+1 < len(lines) {
			i++
			more, moreComment := shellCode(lines[i])
			code = strings.TrimSuffix(strings.TrimSpace(code), `\`) + " " + more
			explained = explained || pipeOK.MatchString(moreComment)
		}
		unsafe := false
		if m := sourceLine.FindStringSubmatch(lines[start]); m != nil {
			unsafe = pipefailLibraries[m[1]] && !pipefail
		}
		for _, segment := range shellSeparator.Split(code, -1) {
			switch {
			case pipefailOn.MatchString(segment):
				pipefail = true
			case pipefailOff.MatchString(segment):
				pipefail = false
			case !pipefail && unsafePipeline(segment):
				unsafe = true
			}
		}
		if unsafe && !explained {
			pipeline := make([]string, 0, i-start+1)
			for _, l := range lines[start : i+1] {
				pipeline = append(pipeline, strings.TrimSpace(l))
			}
			hits = append(hits, pipeHit{file, firstLine + start, strings.Join(pipeline, " ")})
		}
	}
	return hits
}

// scanScript scans a whole file; a library starts under its caller's pipefail.
func scanScript(file, text string) []pipeHit {
	return scanShell(file, text, 1, pipefailLibraries[filepath.Base(file)])
}

type pipeDefaults struct {
	Run struct {
		Shell string `yaml:"shell"`
	} `yaml:"run"`
}

type pipeWorkflow struct {
	Defaults pipeDefaults `yaml:"defaults"`
	Jobs     map[string]struct {
		Defaults pipeDefaults `yaml:"defaults"`
		Steps    []struct {
			Shell string    `yaml:"shell"`
			Run   yaml.Node `yaml:"run"`
		} `yaml:"steps"`
	} `yaml:"jobs"`
}

// scanWorkflow scans every run: block that does not run under `shell: bash`,
// a step's own shell winning over its job's default, that over the workflow's.
func scanWorkflow(file, text string) ([]pipeHit, error) {
	var w pipeWorkflow
	if err := yaml.Unmarshal([]byte(text), &w); err != nil {
		return nil, fmt.Errorf("parse %s: %w", file, err)
	}
	var hits []pipeHit
	for _, job := range w.Jobs {
		for _, step := range job.Steps {
			shell := step.Shell
			for _, fallback := range []string{job.Defaults.Run.Shell, w.Defaults.Run.Shell} {
				if shell == "" {
					shell = fallback
				}
			}
			if shell == "bash" || step.Run.Value == "" {
				continue
			}
			firstLine := step.Run.Line
			if step.Run.Style&(yaml.LiteralStyle|yaml.FoldedStyle) != 0 {
				firstLine++ // the node starts at the `|`, the text on the next line
			}
			hits = append(hits, scanShell(file, step.Run.Value, firstLine, false)...)
		}
	}
	sort.Slice(hits, func(i, j int) bool { return hits[i].line < hits[j].line })
	return hits, nil
}

// repoPipeFiles lists what the guard reads, relative to the repository root.
func repoPipeFiles(t *testing.T) (workflows, scripts []string) {
	t.Helper()
	workflows, err := filepath.Glob("../.github/workflows/*.y*ml")
	if err != nil {
		t.Fatal(err)
	}
	hooks, err := filepath.Glob("../.githooks/*")
	if err != nil {
		t.Fatal(err)
	}
	scripts = hooks
	err = filepath.WalkDir("../scripts", func(p string, d fs.DirEntry, err error) error {
		if err == nil && !d.IsDir() && strings.HasSuffix(p, ".sh") {
			scripts = append(scripts, p)
		}
		return err
	})
	if err != nil {
		t.Fatal(err)
	}
	return workflows, scripts
}

func workflowStep(extra, body string) string {
	var b strings.Builder
	b.WriteString("jobs:\n  a:\n    runs-on: ubuntu-latest\n" + extra + "    steps:\n      - run: |\n")
	for _, l := range strings.Split(body, "\n") {
		b.WriteString("          " + l + "\n")
	}
	return b.String()
}

// Scenario: a check piped into tee without pipefail is refused — the failure
// names the file, the line and the pipeline.
func TestPipeGuard_CheckIntoTeeIsRefused(t *testing.T) {
	yml := workflowStep("", "echo start\nbash scripts/check_cover.sh | tee cover.txt")
	hits, err := scanWorkflow(".github/workflows/x.yml", yml)
	if err != nil {
		t.Fatal(err)
	}
	if len(hits) != 1 {
		t.Fatalf("hits = %v, want one", hits)
	}
	want := ".github/workflows/x.yml:7: bash scripts/check_cover.sh | tee cover.txt"
	if got := hits[0].String(); got != want {
		t.Errorf("hit = %q, want %q", got, want)
	}
}

// Scenarios: the same step with pipefail passes; a step under shell: bash passes
// (its own, its job's or the workflow's default, and not another shell's).
func TestPipeGuard_WorkflowShells(t *testing.T) {
	const check = "bash scripts/check_cover.sh | tee cover.txt"
	step := func(line string) string {
		return "jobs:\n  a:\n    steps:\n      - " + line + "\n        run: " + check + "\n"
	}
	for _, c := range []struct {
		name string
		yml  string
		want int
	}{
		{"set -o pipefail first", workflowStep("", "set -o pipefail\n"+check), 0},
		{"set -euo pipefail first", workflowStep("", "set -euo pipefail\n"+check), 0},
		{"set -e alone is not enough", workflowStep("", "set -e\n"+check), 1},
		{"pipefail after the pipeline is too late", workflowStep("", check+"\nset -o pipefail"), 1},
		{"pipefail in another step does not count", workflowStep("", "set -o pipefail") + "      - run: " + check + "\n", 1},
		{"a step's shell: bash", step("shell: bash"), 0},
		{"a step's shell: sh", step("shell: sh"), 1},
		{"a job's default", workflowStep("    defaults:\n      run:\n        shell: bash\n", check), 0},
		{"a workflow's default", "defaults:\n  run:\n    shell: bash\n" + workflowStep("", check), 0},
		{"a step's shell overrides the default", "defaults:\n  run:\n    shell: bash\n" + step("shell: sh"), 1},
		{"a default with no shell", workflowStep("    defaults:\n      run:\n        working-directory: frontend\n", check), 1},
	} {
		t.Run(c.name, func(t *testing.T) {
			hits, err := scanWorkflow("x.yml", c.yml)
			if err != nil {
				t.Fatal(err)
			}
			if len(hits) != c.want {
				t.Errorf("hits = %v, want %d", hits, c.want)
			}
		})
	}
}

// A one-line `run: cmd` is on the line of its key, a block on the line after.
func TestPipeGuard_PlainRunLine(t *testing.T) {
	hits, err := scanWorkflow("x.yml", "jobs:\n  a:\n    steps:\n      - run: go vet ./... | tee v.txt\n")
	if err != nil {
		t.Fatal(err)
	}
	if len(hits) != 1 || hits[0].line != 4 {
		t.Errorf("hits = %v, want one on line 4", hits)
	}
}

// Scenarios: an explained exception passes; a producer-only pipeline is not
// flagged. The table also holds what a line scan could get wrong either way.
func TestPipeGuard_Pipelines(t *testing.T) {
	for _, c := range []struct {
		name   string
		script string
		want   int // the line of the hit, 0 for none
	}{
		{"go test into tee", "go test ./tests/... | tee t.log", 1},
		{"into grep", "gofmt -l . | grep foo", 1},
		{"into head after a filter", "shellcheck x.sh | sed s/a/b/ | head -5", 1},
		{"|& is a pipe", "go vet ./... |& tee v.log", 1},
		{"in a substitution", `x=$(python3 scripts/a.py | tail -1)`, 1},
		{"in a quoted substitution", `x="$(python3 scripts/a.py | tail -1)"`, 1},
		{"after an assignment prefix", "GOFLAGS=-v go vet ./... | tee v.log", 1},
		{"after if", "if go vet ./... | grep -q x; then echo y; fi", 1},
		{"after a separator", "true; go vet ./... | tee v.log", 1},
		{"a loop's output", "for a in b; do check $a; done | tee v.log", 1},
		{"a group's output", "{ check_a; check_b; } | sort", 1},
		{"a subshell's output", "(check_a) | sort", 1},
		{"continued with a backslash", "echo a\ngo vet ./... \\\n  | tee v.log", 2},
		{"continued after the pipe", "x=$(go vet ./... |\n  tee v.log)", 1},
		{"a comment after the continuing pipe", "go vet ./... | # why\n  tee v.log", 1},

		{"echo into tee", `echo "a=b" | tee -a "$GITHUB_OUTPUT"`, 0},
		{"printf into a chain", `printf '%s' "$a" | sed s/a/b/ | tr -d x`, 0},
		{"cat into grep", "cat files.txt | grep x", 0},
		{"a producer in if", `if printf '%s' "$a" | grep -qF b; then echo c; fi`, 0},
		{"a right side that decides for itself", "grep x .env | while read -r y; do echo $y; done", 0},
		{"or is no pipe", "go vet ./... || tee v.log", 0},
		{"or then and", "check || grep x y && sed z", 0},
		{"a | in single quotes", `jq '.[] | head' x.json`, 0},
		{"a | in double quotes", `go vet "a | tee b"`, 0},
		{"an apostrophe in a comment", "echo a # it's | tee", 0},
		{"a pipeline in a comment", "# go vet ./... | tee v.log", 0},
		{"a case pattern", "case $a in .env|*.key) echo b ;; esac", 0},

		{"pipe-ok on the line", "go vet ./... | grep -q x # pipe-ok: grep -q is the condition", 0},
		{"pipe-ok on the line before", "# pipe-ok: grep -q is the condition\ngo vet ./... | grep -q x", 0},
		{"pipe-ok on a continuing line", "go vet ./... | # pipe-ok: why\n  tee v.log", 0},
		{"pipe-ok with no reason", "go vet ./... | grep -q x # pipe-ok:", 1},
		{"pipe-ok two lines above", "# pipe-ok: why\necho a\ngo vet ./... | grep -q x", 3},
		{"pipe-ok inside a string", `go vet "# pipe-ok: why" | grep -q x`, 1},

		{"set -euo pipefail", "set -euo pipefail\ngo vet ./... | tee v.log", 0},
		{"set -o pipefail with a comment", "set -o pipefail # why\ngo vet ./... | tee v.log", 0},
		{"on one line", "set -o pipefail; go vet ./... | tee v.log", 0},
		{"set -e -o pipefail", "set -e -o pipefail\ngo vet ./... | tee v.log", 0},
		{"a commented set", "# set -o pipefail\ngo vet ./... | tee v.log", 2},
		{"a quoted set", "echo 'set -o pipefail'\ngo vet ./... | tee v.log", 2},
		{"turned off again", "set -o pipefail\nset +o pipefail\ngo vet ./... | tee v.log", 3},
	} {
		t.Run(c.name, func(t *testing.T) {
			hits := scanScript("x.sh", c.script)
			switch {
			case c.want == 0 && len(hits) != 0:
				t.Errorf("hits = %v, want none", hits)
			case c.want != 0 && (len(hits) != 1 || hits[0].line != c.want):
				t.Errorf("hits = %v, want one on line %d", hits, c.want)
			}
		})
	}
}

// A library inherits pipefail, so every source of one must be under it.
func TestPipeGuard_Libraries(t *testing.T) {
	const pipeline = "go vet ./... | tee v.log\n"
	if hits := scanScript("scripts/agents/lib.sh", pipeline); len(hits) != 0 {
		t.Errorf("a library's own pipeline: hits = %v, want none", hits)
	}
	if hits := scanScript("scripts/agents/other.sh", pipeline); len(hits) != 1 {
		t.Errorf("a script outside the list: hits = %v, want one", hits)
	}
	for _, c := range []struct {
		name   string
		script string
		want   int
	}{
		{"under pipefail", "set -euo pipefail\nsource \"$(dirname \"$0\")/lib.sh\"\n", 0},
		{"before pipefail", "source \"$(dirname \"$0\")/lib.sh\"\nset -euo pipefail\n", 1},
		{"with a dot", ". scripts/env_lib.sh\n", 1},
		{"with a comment", "source \"$d/limit_lib.sh\" # loads agents.env\n", 1},
		{"explained", "source scripts/env_lib.sh # pipe-ok: only reads a constant\n", 0},
		{"another file", "source scripts/settings.sh\n", 0},
	} {
		t.Run(c.name, func(t *testing.T) {
			if hits := scanScript("x.sh", c.script); len(hits) != c.want {
				t.Errorf("hits = %v, want %d", hits, c.want)
			}
		})
	}
}

// Scenario: the repository is clean — no unexplained unsafe pipeline in the
// workflows, scripts/** and .githooks/*.
func TestPipeGuard_RepositoryIsClean(t *testing.T) {
	workflows, scripts := repoPipeFiles(t)
	if len(workflows) == 0 || len(scripts) < 30 {
		t.Fatalf("the guard read %d workflows and %d scripts: wrong folder?", len(workflows), len(scripts))
	}
	var hits []pipeHit
	for _, f := range workflows {
		got, err := scanWorkflow(strings.TrimPrefix(f, "../"), readFile(t, f))
		if err != nil {
			t.Fatal(err)
		}
		hits = append(hits, got...)
	}
	for _, f := range scripts {
		hits = append(hits, scanScript(strings.TrimPrefix(f, "../"), readFile(t, f))...)
	}
	for _, h := range hits {
		t.Errorf("%s\n  the pipe hides the left command's failure: set pipefail, or explain it with # pipe-ok: <why>", h)
	}
}

// The ci.yml step that picks the jobs is the guard's own first catch: a crash of
// checkfail.py behind tee would run no job and pass.
func TestPipeGuard_ChangesStepFailsWhenSuitesCrash(t *testing.T) {
	var step string
	for _, s := range readCIWorkflow(t).Jobs["changes"].Steps {
		if strings.Contains(s.Run, "checkfail.py suites") {
			step = s.Run
		}
	}
	if step == "" {
		t.Fatal("no checkfail.py suites step in the changes job of ci.yml")
	}
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "python3"), []byte("#!/bin/sh\nexit 3\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	stepFile := filepath.Join(dir, "step.sh")
	writeFile(t, stepFile, step)

	for _, args := range [][]string{
		{"init", "-q"},
		{"-c", "user.name=t", "-c", "user.email=t@example.com", "commit", "-q", "--allow-empty", "-m", "base"},
	} {
		cmd := exec.Command("git", args...)
		cmd.Dir = dir
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v\n%s", args, err, out)
		}
	}
	cmd := exec.Command("bash", "-e", stepFile) // as GitHub runs a step with no shell
	cmd.Dir = dir
	cmd.Env = append(os.Environ(), "EVENT=pull_request", "BASE=HEAD", "HEAD=HEAD",
		"PATH="+dir+string(os.PathListSeparator)+os.Getenv("PATH"), "GITHUB_OUTPUT="+filepath.Join(dir, "output"))
	if out, err := cmd.CombinedOutput(); err == nil {
		t.Errorf("the step passed with checkfail.py failing:\n%s", out)
	}
}

// The hook's secret check fails closed: it runs under /bin/sh, which may have no
// pipefail, so a git diff that fails stops the commit by itself.
func TestPipeGuard_PreCommitSecretCheck(t *testing.T) {
	hook, err := filepath.Abs("../.githooks/pre-commit")
	if err != nil {
		t.Fatal(err)
	}
	for _, c := range []struct {
		name     string
		staged   string
		badGit   bool
		wantPass bool
	}{
		{"a clean change passes", "harmless\n", false, true},
		{"the secret's value is refused", "fake-token-value\n", false, false},
		{"a failing git diff is refused", "harmless\n", true, false},
	} {
		t.Run(c.name, func(t *testing.T) {
			dir := t.TempDir()
			writeFile(t, filepath.Join(dir, ".env"), "FAKE_TOKEN=fake-token-value\n")
			writeFile(t, filepath.Join(dir, "a.txt"), c.staged)
			for _, args := range [][]string{{"init", "-q"}, {"add", "a.txt"}} {
				cmd := exec.Command("git", args...)
				cmd.Dir = dir
				if out, err := cmd.CombinedOutput(); err != nil {
					t.Fatalf("git %v: %v\n%s", args, err, out)
				}
			}
			cmd := exec.Command("sh", hook)
			cmd.Dir = dir
			if c.badGit {
				cmd.Env = append(os.Environ(), "GIT_DIR="+filepath.Join(dir, "no-such-repo"))
			}
			out, err := cmd.CombinedOutput()
			if passed := err == nil; passed != c.wantPass {
				t.Errorf("passed = %v (%v), want %v:\n%s", passed, err, c.wantPass, out)
			}
		})
	}
}
