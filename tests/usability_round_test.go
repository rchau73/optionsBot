package tests

import (
	"context"
	"io"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"
)

// scripts/usability_round.sh (#727) runs against a copy of the repository's
// layout with stub go, npm, npx and curl, so no demo or monitor really starts
// and the real frontend/.env.local is out of reach.

const roundEnvLocal = "MONITOR_API_TOKEN=real-from-env-local\n"

type roundFixture struct {
	root, log string
	env       []string
}

func writeExecutable(t *testing.T, path, body string) {
	t.Helper()
	if err := os.WriteFile(path, []byte("#!/bin/sh\n"+body), 0o755); err != nil {
		t.Fatal(err)
	}
}

func freePort(t *testing.T) string {
	t.Helper()
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("free port: %v", err)
	}
	defer l.Close()
	return strconv.Itoa(l.Addr().(*net.TCPAddr).Port)
}

func newRoundFixture(t *testing.T) roundFixture {
	t.Helper()
	root := t.TempDir()
	for _, d := range []string{"scripts", "frontend", "bin"} {
		if err := os.Mkdir(filepath.Join(root, d), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	script, err := os.ReadFile("../scripts/usability_round.sh")
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "scripts/usability_round.sh"), script, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "frontend/.env.local"), []byte(roundEnvLocal), 0o600); err != nil {
		t.Fatal(err)
	}
	log := filepath.Join(root, "calls.log")
	bin := filepath.Join(root, "bin")
	writeExecutable(t, filepath.Join(bin, "go"), `printf '#!/bin/sh\necho "demo $*" >> "$ROUND_LOG"\nexec sleep 300\n' > "$3"
chmod +x "$3"
echo "go $*" >> "$ROUND_LOG"
`)
	writeExecutable(t, filepath.Join(bin, "npm"), `echo "npm $* in $PWD" >> "$ROUND_LOG"
`)
	writeExecutable(t, filepath.Join(bin, "npx"), `{
  echo "npx $*"
  echo "db=$AUTH_DB_PATH"
  echo "secret=${#AUTH_SECRET}"
  echo "user=$ADMIN_USERNAME"
  echo "apis=$BOT_APIS"
  echo "tokens=[$MONITOR_API_TOKEN][$BOT_ADMIN_TOKEN][$ANTHROPIC_API_KEY]"
} >> "$ROUND_LOG"
exec sleep 300
`)
	writeExecutable(t, filepath.Join(bin, "curl"), "exit 0\n")
	env := append(os.Environ(),
		"PATH="+bin+":"+os.Getenv("PATH"),
		"ROUND_LOG="+log,
		"USABILITY_DEMO_PORT="+freePort(t),
		"USABILITY_MONITOR_PORT="+freePort(t),
		"MONITOR_API_TOKEN=real-monitor-token",
		"BOT_ADMIN_TOKEN=real-admin-token",
		"ANTHROPIC_API_KEY=real-api-key",
	)
	return roundFixture{root: root, log: log, env: env}
}

func waitForText(t *testing.T, path, want string) string {
	t.Helper()
	for deadline := time.Now().Add(20 * time.Second); ; time.Sleep(20 * time.Millisecond) {
		b, _ := os.ReadFile(path)
		if strings.Contains(string(b), want) {
			return string(b)
		}
		if time.Now().After(deadline) {
			t.Fatalf("%s never held %q; it holds:\n%s", path, want, b)
		}
	}
}

// Scenario: One command sets up a round.
func TestUsabilityRound_OneCommandSetsUpARound(t *testing.T) {
	f := newRoundFixture(t)
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()
	cmd := exec.CommandContext(ctx, "bash", filepath.Join(f.root, "scripts/usability_round.sh"), "mixed")
	cmd.Env = f.env
	out := filepath.Join(f.root, "out.txt")
	sink, err := os.Create(out)
	if err != nil {
		t.Fatal(err)
	}
	defer sink.Close()
	cmd.Stdout, cmd.Stderr = sink, sink
	stdin, err := cmd.StdinPipe()
	if err != nil {
		t.Fatal(err)
	}
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	waitForText(t, out, "Sign in:")
	calls := waitForText(t, f.log, "tokens=")

	for _, want := range []string{
		"npm ci in ",
		"-scenario mixed",
		"user=moderator",
		"tokens=[][][]", // the real tokens of the shell are blanked
		"secret=",
	} {
		if !strings.Contains(calls, want) {
			t.Errorf("setup calls do not contain %q:\n%s", want, calls)
		}
	}
	printed, _ := os.ReadFile(out)
	for _, want := range []string{"Scenario:  mixed", "Demo API:  http://127.0.0.1:", "Monitor:   http://localhost:", "Sign in:   http://localhost:"} {
		if !strings.Contains(string(printed), want) {
			t.Errorf("output does not contain %q:\n%s", want, printed)
		}
	}
	for _, secret := range []string{"real-monitor-token", "real-admin-token", "real-api-key"} {
		if strings.Contains(calls, secret) || strings.Contains(string(printed), secret) {
			t.Errorf("the round used or printed %s", secret)
		}
	}

	// A throw-away user database, outside the repository.
	var db string
	for _, line := range strings.Split(calls, "\n") {
		if v, ok := strings.CutPrefix(line, "db="); ok {
			db = v
		}
	}
	if db == "" || strings.HasPrefix(db, f.root) {
		t.Errorf("user database %q: want a throw-away path outside the repository", db)
	}

	// Between participants: a typed scenario restarts the demo, a wrong one is refused.
	if _, err := io.WriteString(stdin, "nonsense\ndrawdown\n"); err != nil {
		t.Fatal(err)
	}
	waitForText(t, f.log, "-scenario drawdown")
	waitForText(t, out, "Scenario:  drawdown")
	waitForText(t, out, "unknown scenario 'nonsense'")

	stdin.Close()
	if err := cmd.Wait(); err != nil {
		t.Fatalf("the round did not end cleanly: %v\n%s", err, printed)
	}

	if _, err := os.Stat(filepath.Dir(db)); !os.IsNotExist(err) {
		t.Errorf("the throw-away database folder %s survives the round: %v", filepath.Dir(db), err)
	}
	got, err := os.ReadFile(filepath.Join(f.root, "frontend/.env.local"))
	if err != nil || string(got) != roundEnvLocal {
		t.Errorf(".env.local changed: %q, %v", got, err)
	}
}

func TestUsabilityRound_DefaultScenarioPassesNoFlag(t *testing.T) {
	f := newRoundFixture(t)
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()
	cmd := exec.CommandContext(ctx, "bash", filepath.Join(f.root, "scripts/usability_round.sh"))
	cmd.Env = f.env
	stdin, err := cmd.StdinPipe()
	if err != nil {
		t.Fatal(err)
	}
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	waitForText(t, f.log, "tokens=")
	calls := waitForText(t, f.log, "demo -addr")
	if strings.Contains(calls, "-scenario") {
		t.Errorf("the default demo got a scenario flag:\n%s", calls)
	}
	stdin.Close()
	if err := cmd.Wait(); err != nil {
		t.Fatalf("the round did not end cleanly: %v", err)
	}
}

// Negative: nothing starts on a wrong scenario or on a busy port (a real bot's
// API on the demo port would be shown to the participant).
func TestUsabilityRound_RefusesBeforeStartingAnything(t *testing.T) {
	t.Run("unknown scenario", func(t *testing.T) {
		f := newRoundFixture(t)
		cmd := exec.Command("bash", filepath.Join(f.root, "scripts/usability_round.sh"), "good")
		cmd.Env = f.env
		out, err := cmd.CombinedOutput()
		if ee, ok := err.(*exec.ExitError); !ok || ee.ExitCode() != 2 {
			t.Fatalf("exit %v, want 2:\n%s", err, out)
		}
		if !strings.Contains(string(out), "unknown scenario 'good'") {
			t.Errorf("output: %s", out)
		}
		if _, err := os.Stat(f.log); !os.IsNotExist(err) {
			t.Error("something started on a wrong scenario")
		}
	})
	t.Run("busy demo port", func(t *testing.T) {
		f := newRoundFixture(t)
		l, err := net.Listen("tcp", "127.0.0.1:0")
		if err != nil {
			t.Fatal(err)
		}
		defer l.Close()
		port := strconv.Itoa(l.Addr().(*net.TCPAddr).Port)
		cmd := exec.Command("bash", filepath.Join(f.root, "scripts/usability_round.sh"))
		cmd.Env = append(f.env, "USABILITY_DEMO_PORT="+port)
		out, err := cmd.CombinedOutput()
		if err == nil || !strings.Contains(string(out), "port "+port+" is in use") {
			t.Fatalf("err %v, output: %s", err, out)
		}
		if _, err := os.Stat(f.log); !os.IsNotExist(err) {
			t.Error("something started on a busy port")
		}
	})
}
