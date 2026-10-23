package tests

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// scripts/check_shell.sh (#205): bash -n and shellcheck on every script, and
// a reason on every disable directive. The shellcheck cases skip where it is
// not installed; CI installs it and runs the script with REQUIRE_SHELLCHECK=1.

// runCheckShell runs the check from the repository root with extra env vars
// and returns its exit status (0 = ok) and output.
func runCheckShell(t *testing.T, env []string, files ...string) (int, string) {
	t.Helper()
	if _, err := exec.LookPath("bash"); err != nil {
		t.Skip("bash not installed")
	}
	cmd := exec.Command("bash", append([]string{"scripts/check_shell.sh"}, files...)...)
	cmd.Dir = ".."
	cmd.Env = append(os.Environ(), env...)
	out, err := cmd.CombinedOutput()
	if ee, ok := err.(*exec.ExitError); ok {
		return ee.ExitCode(), string(out)
	}
	if err != nil {
		t.Fatalf("run check_shell.sh: %v", err)
	}
	return 0, string(out)
}

// writeScript writes a bash script into a temp dir and returns its path.
func writeScript(t *testing.T, name, body string) string {
	t.Helper()
	p := filepath.Join(t.TempDir(), name)
	if err := os.WriteFile(p, []byte("#!/usr/bin/env bash\n"+body), 0o644); err != nil {
		t.Fatal(err)
	}
	return p
}

func requireShellcheck(t *testing.T) {
	t.Helper()
	if _, err := exec.LookPath("shellcheck"); err != nil {
		t.Skip("shellcheck not installed (CI installs it)")
	}
}

// Scenario: main is clean — every script in the repo passes (bash -n, disable
// reasons, and shellcheck at warning level where it is installed).
func TestShellScripts_MainIsClean(t *testing.T) {
	code, out := runCheckShell(t, nil)
	if code != 0 {
		t.Fatalf("check_shell.sh failed on the repo (exit %d):\n%s", code, out)
	}
	if !strings.Contains(out, "script(s) ok") {
		t.Errorf("no ok line:\n%s", out)
	}
}

// Scenario: a broken script fails CI — an unquoted command substitution
// (SC2046, warning) fails and names the file and line.
func TestShellScripts_BrokenScriptFailsWithFileAndLine(t *testing.T) {
	requireShellcheck(t)
	p := writeScript(t, "broken.sh", "set -euo pipefail\ntouch $(echo a b)\n")
	code, out := runCheckShell(t, nil, p)
	if code == 0 {
		t.Fatalf("expected a failure:\n%s", out)
	}
	// Line 3: writeScript puts the shebang on line 1.
	if !strings.Contains(out, p+":3:") || !strings.Contains(out, "SC2046") {
		t.Errorf("want %s:3: and SC2046 in the output:\n%s", p, out)
	}
}

// A syntax error fails whether or not shellcheck is installed.
func TestShellScripts_SyntaxErrorFails(t *testing.T) {
	p := writeScript(t, "syntax.sh", "if true; then\n  echo x\n")
	code, out := runCheckShell(t, []string{"SHELLCHECK=/nonexistent/shellcheck"}, p)
	if code == 0 || !strings.Contains(out, p) {
		t.Fatalf("expected a syntax failure naming %s (exit %d):\n%s", p, code, out)
	}
}

// Scenario: every disable is justified — a bare disable fails, one with its
// reason on the same line passes.
func TestShellScripts_EveryDisableIsJustified(t *testing.T) {
	noSC := []string{"SHELLCHECK=/nonexistent/shellcheck"}
	bare := writeScript(t, "bare.sh", "# shellcheck disable=SC2034\nunused=1\n")
	code, out := runCheckShell(t, noSC, bare)
	if code == 0 || !strings.Contains(out, bare+":2:") {
		t.Fatalf("a bare disable must fail and name %s:2 (exit %d):\n%s", bare, code, out)
	}
	why := writeScript(t, "why.sh", "# shellcheck disable=SC2034 # read by the caller\nunused=1\n")
	if code, out := runCheckShell(t, noSC, why); code != 0 {
		t.Fatalf("a disable with its reason must pass (exit %d):\n%s", code, out)
	}
}

// Without shellcheck, make check skips that step with a message; CI
// (REQUIRE_SHELLCHECK=1) fails instead of skipping.
func TestShellScripts_MissingShellcheck(t *testing.T) {
	p := writeScript(t, "ok.sh", "set -euo pipefail\necho \"ok\"\n")
	noSC := "SHELLCHECK=/nonexistent/shellcheck"
	code, out := runCheckShell(t, []string{noSC}, p)
	if code != 0 || !strings.Contains(out, "skipped") {
		t.Fatalf("want a skip message and exit 0 (exit %d):\n%s", code, out)
	}
	code, out = runCheckShell(t, []string{noSC, "REQUIRE_SHELLCHECK=1"}, p)
	if code == 0 {
		t.Fatalf("REQUIRE_SHELLCHECK=1 without shellcheck must fail:\n%s", out)
	}
}
