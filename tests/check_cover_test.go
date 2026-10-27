package tests

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// scripts/check_cover.sh (#330): the coverage floor. The tests feed it a
// synthetic profile and a baseline in a temp dir (COVER_BASELINE), so they
// never run the suite's coverage themselves.

// coverBlock is one profile line: a package, how many statements, covered or not.
type coverBlock struct {
	pkg     string
	stmts   int
	covered bool
}

// writeProfile writes a coverage profile with one block per entry; each block
// gets its own line range so no two collide.
func writeProfile(t *testing.T, dir string, blocks []coverBlock) string {
	t.Helper()
	var b strings.Builder
	b.WriteString("mode: atomic\n")
	for i, bl := range blocks {
		count := 0
		if bl.covered {
			count = 3
		}
		fmt.Fprintf(&b, "optionsbot/internal/%s/file.go:%d.1,%d.2 %d %d\n", bl.pkg, i+1, i+1, bl.stmts, count)
	}
	p := filepath.Join(dir, "coverage.out")
	if err := os.WriteFile(p, []byte(b.String()), 0o644); err != nil {
		t.Fatal(err)
	}
	return p
}

// runCheckCover runs the check from the repository root against the baseline
// file given and returns its exit status and output.
func runCheckCover(t *testing.T, baseline string, args ...string) (int, string) {
	t.Helper()
	if _, err := exec.LookPath("bash"); err != nil {
		t.Skip("bash not installed")
	}
	cmd := exec.Command("bash", append([]string{"scripts/check_cover.sh"}, args...)...)
	cmd.Dir = ".."
	cmd.Env = append(os.Environ(), "COVER_BASELINE="+baseline)
	out, err := cmd.CombinedOutput()
	if ee, ok := err.(*exec.ExitError); ok {
		return ee.ExitCode(), string(out)
	}
	if err != nil {
		t.Fatalf("run check_cover.sh: %v", err)
	}
	return 0, string(out)
}

func writeFile(t *testing.T, p, body string) {
	t.Helper()
	if err := os.WriteFile(p, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
}

func readFile(t *testing.T, p string) string {
	t.Helper()
	b, err := os.ReadFile(p)
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

// coverBaseline writes a baseline total and its per-package figures.
func coverBaseline(t *testing.T, dir, total, packages string) string {
	t.Helper()
	p := filepath.Join(dir, "cover_baseline.txt")
	writeFile(t, p, total+"\n")
	writeFile(t, filepath.Join(dir, "cover_baseline_packages.txt"), packages)
	return p
}

const coverPackages71 = "optionsbot/internal/risk 83.3\noptionsbot/internal/strategy 53.5\n"

// Scenario: Coverage at or above the baseline — baseline 71.0, total 71.4:
// passes and suggests raising the baseline.
func TestCheckCover_AtOrAboveBaselinePassesAndSuggestsRaise(t *testing.T) {
	dir := t.TempDir()
	base := coverBaseline(t, dir, "71.0", coverPackages71)
	// risk 500/600 covered, strategy 214/400 → 714/1000 = 71.4 %.
	prof := writeProfile(t, dir, []coverBlock{
		{"risk", 500, true}, {"risk", 100, false},
		{"strategy", 214, true}, {"strategy", 186, false},
	})
	code, out := runCheckCover(t, base, "--profile", prof)
	if code != 0 {
		t.Fatalf("want a pass (exit %d):\n%s", code, out)
	}
	if !strings.Contains(out, "71.4%") || !strings.Contains(out, "--update") {
		t.Errorf("want the total and a hint to raise the baseline:\n%s", out)
	}
}

// Scenario: Coverage falls — baseline 71.0, total 69.8: fails and lists the
// packages whose coverage fell (strategy), not the one that held (risk).
func TestCheckCover_FallFailsAndListsPackages(t *testing.T) {
	dir := t.TempDir()
	base := coverBaseline(t, dir, "71.0", coverPackages71)
	// risk 500/600 (83.3, held), strategy 198/400 (49.5, fell) → 69.8 %.
	prof := writeProfile(t, dir, []coverBlock{
		{"risk", 500, true}, {"risk", 100, false},
		{"strategy", 198, true}, {"strategy", 202, false},
	})
	code, out := runCheckCover(t, base, "--profile", prof)
	if code == 0 {
		t.Fatalf("want a failure:\n%s", out)
	}
	if !strings.Contains(out, "69.8%") || !strings.Contains(out, "optionsbot/internal/strategy") {
		t.Errorf("want the total and the strategy package listed:\n%s", out)
	}
	if strings.Contains(out, "optionsbot/internal/risk") {
		t.Errorf("risk held its coverage and must not be listed:\n%s", out)
	}
}

// Within the tolerance (0.2 points) under the baseline still passes; just past
// it fails — the edge is compared in tenths, not floats.
func TestCheckCover_Tolerance(t *testing.T) {
	for _, tc := range []struct {
		covered int // of 1000 statements
		pass    bool
	}{
		{710, true}, {709, true}, {708, true}, {707, false},
	} {
		dir := t.TempDir()
		base := coverBaseline(t, dir, "71.0", coverPackages71)
		prof := writeProfile(t, dir, []coverBlock{
			{"risk", tc.covered, true}, {"risk", 1000 - tc.covered, false},
		})
		code, out := runCheckCover(t, base, "--profile", prof)
		if (code == 0) != tc.pass {
			t.Errorf("total %.1f: pass=%v want %v:\n%s", float64(tc.covered)/10, code == 0, tc.pass, out)
		}
	}
}

// A block counted by several test binaries appears more than once in a merged
// profile: its statements count once, covered when any run hit it.
func TestCheckCover_DuplicateBlocksCountOnce(t *testing.T) {
	dir := t.TempDir()
	base := filepath.Join(dir, "cover_baseline.txt")
	prof := filepath.Join(dir, "coverage.out")
	writeFile(t, prof, "mode: atomic\n"+
		"optionsbot/internal/risk/a.go:1.1,2.2 3 0\n"+
		"optionsbot/internal/risk/a.go:1.1,2.2 3 1\n"+
		"optionsbot/internal/risk/a.go:3.1,4.2 1 0\n")
	if code, out := runCheckCover(t, base, "--update", "--profile", prof); code != 0 {
		t.Fatalf("update failed (exit %d):\n%s", code, out)
	}
	if got := readFile(t, base); got != "75.0\n" {
		t.Errorf("baseline = %q, want 75.0 (3 of 4 statements)", got)
	}
}

// Scenario: Baseline update is deliberate — --update writes the new total and
// nothing else in the baseline file; the profile is left as it was.
func TestCheckCover_UpdateWritesOnlyTheNewTotal(t *testing.T) {
	dir := t.TempDir()
	base := coverBaseline(t, dir, "71.0", coverPackages71)
	prof := writeProfile(t, dir, []coverBlock{
		{"risk", 500, true}, {"risk", 100, false},
		{"strategy", 214, true}, {"strategy", 186, false},
	})
	before := readFile(t, prof)
	code, out := runCheckCover(t, base, "--update", "--profile", prof)
	if code != 0 {
		t.Fatalf("update failed (exit %d):\n%s", code, out)
	}
	if got := readFile(t, base); got != "71.4\n" {
		t.Errorf("baseline = %q, want exactly \"71.4\\n\"", got)
	}
	if got := readFile(t, filepath.Join(dir, "cover_baseline_packages.txt")); got != "optionsbot/internal/risk 83.3\noptionsbot/internal/strategy 53.5\n" {
		t.Errorf("per-package baseline = %q", got)
	}
	if readFile(t, prof) != before {
		t.Error("the profile changed")
	}
	// The new baseline passes at once, with no raise hint.
	code, out = runCheckCover(t, base, "--profile", prof)
	if code != 0 || strings.Contains(out, "--update") {
		t.Errorf("after update: exit %d, want a plain pass:\n%s", code, out)
	}
}

// A missing or malformed baseline, or an empty profile, is an error — never a pass.
func TestCheckCover_BadInputsFail(t *testing.T) {
	dir := t.TempDir()
	prof := writeProfile(t, dir, []coverBlock{{"risk", 10, true}})
	if code, out := runCheckCover(t, filepath.Join(dir, "none.txt"), "--profile", prof); code == 0 {
		t.Errorf("missing baseline must fail:\n%s", out)
	}
	bad := filepath.Join(dir, "bad.txt")
	writeFile(t, bad, "71.04\n")
	if code, out := runCheckCover(t, bad, "--profile", prof); code == 0 {
		t.Errorf("a baseline with two decimals must fail:\n%s", out)
	}
	empty := filepath.Join(dir, "empty.out")
	writeFile(t, empty, "mode: atomic\n")
	if code, out := runCheckCover(t, coverBaseline(t, dir, "71.0", ""), "--profile", empty); code == 0 {
		t.Errorf("a profile with no statements must fail:\n%s", out)
	}
}

// The repository's baseline is one number to one decimal.
func TestCheckCover_RepoBaselineIsOneNumber(t *testing.T) {
	got := strings.TrimSpace(readFile(t, "../scripts/cover_baseline.txt"))
	var f float64
	if _, err := fmt.Sscanf(got, "%f", &f); err != nil || fmt.Sprintf("%.1f", f) != got || f <= 0 || f > 100 {
		t.Errorf("scripts/cover_baseline.txt = %q, want one number to one decimal", got)
	}
}
