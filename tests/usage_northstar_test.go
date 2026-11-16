package tests

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"optionsbot/internal/usage"
)

// The usage report's north star (#801, PRD §7): environments with at least one
// strategy-running day (a bot's data/regime_history.jsonl) and at least one
// session_started in the 30 days ending with the week.

// northStarClone lays out a clone's data folder with one bot whose regime
// history has a line on each of days (plus a seeded line, a torn line and a
// P&L history with amounts the report must not read). Returns the data folder.
func northStarClone(t *testing.T, days ...time.Time) string {
	t.Helper()
	data := filepath.Join(t.TempDir(), "data")
	bot := filepath.Join(data, "btc", "data")
	if err := os.MkdirAll(bot, 0o755); err != nil {
		t.Fatal(err)
	}
	var lines []string
	if len(days) > 0 {
		seed := days[0].AddDate(0, 0, -1)
		lines = append(lines, `{"day":"`+seed.Format(time.RFC3339)+`","regime":"POSITIVE","seeded":true}`)
	}
	for _, d := range days {
		lines = append(lines, `{"day":"`+d.UTC().Truncate(24*time.Hour).Format(time.RFC3339)+`","regime":"NEGATIVE"}`)
	}
	lines = append(lines, `{"day":"2026-10-`) // torn by a crash
	if err := os.WriteFile(filepath.Join(bot, usage.RunningFile), []byte(strings.Join(lines, "\n")+"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	pnl := `{"t":"2026-10-20T00:00:00Z","total":0.4242,"total_usd":98765.43,"spot":101234.5}` + "\n"
	if err := os.WriteFile(filepath.Join(bot, "pnl_history.jsonl"), []byte(pnl), 0o644); err != nil {
		t.Fatal(err)
	}
	return data
}

// northStarVisits: people 1…n each start one session in env at at.
func northStarVisits(t *testing.T, env string, at time.Time, n int) []string {
	var lines []string
	for i := 1; i <= n; i++ {
		lines = append(lines, usageLine(t, "session_started", at.Add(time.Duration(i)*time.Minute), i, map[string]any{"environment": env}))
	}
	return lines
}

func northStarReport(t *testing.T, lines []string, running ...usage.Running) (usage.Report, string) {
	t.Helper()
	w := usageWeek(t, "2026-W43")
	r, paths, err := usage.RunWithRunning(writeUsageEvents(t, lines), w.From, w.To, w.Label, t.TempDir(),
		usage.DefaultRules(), usage.DefaultFlatRules(), usage.DropBaseWeeks, running)
	if err != nil {
		t.Fatal(err)
	}
	var written string
	for _, p := range paths {
		b, err := os.ReadFile(p)
		if err != nil {
			t.Fatal(err)
		}
		written += string(b)
	}
	if r.NorthStar == nil {
		t.Fatal("no north star in the report")
	}
	return r, written
}

func loadRunning(t *testing.T, env, data string) usage.Running {
	t.Helper()
	r, err := usage.LoadRunning(env, data)
	if err != nil {
		t.Fatal(err)
	}
	return r
}

// Scenario: an environment with strategy-running days and consented sessions in the last 30 days is one monthly active account.
func TestUsage_NorthStarRunningAndVisitedIsActive(t *testing.T) {
	w := usageWeek(t, "2026-W43")
	data := northStarClone(t, w.From.AddDate(0, 0, -10), w.From.Add(24*time.Hour))
	r, md := northStarReport(t, northStarVisits(t, "mvp", w.From.Add(30*time.Hour), 12), loadRunning(t, "", data))
	ns := r.NorthStar
	if ns.Active != 1 || ns.VisitsNotRunning != 0 || ns.RunningNoVisits != 0 || ns.Environments != 1 {
		t.Fatalf("north star = %+v, want mvp active", ns)
	}
	if ns.RunningDays["mvp"] != 2 {
		t.Fatalf("running days = %v, want 2 (the seeded and torn lines left out)", ns.RunningDays)
	}
	if ns.People != 12 || ns.Note != "" || ns.ActiveSharePct == nil || *ns.ActiveSharePct != 100 {
		t.Fatalf("people %d, note %q, share %v: want 12, judged, 100 %%", ns.People, ns.Note, ns.ActiveSharePct)
	}
	for _, want := range []string{"## North star: monthly active accounts with a strategy running", "| 1 | 0 | 0 | 1 | 12 | 100 % |",
		usage.RunningFile, "the 30 days from 2026-09-26 to 2026-10-26", "Active: mvp"} {
		if !strings.Contains(md, want) {
			t.Fatalf("report lacks %q:\n%s", want, md)
		}
	}
}

// Scenario: sessions and no strategy-running day → visits without a running strategy, not active.
func TestUsage_NorthStarVisitsWithoutRunning(t *testing.T) {
	w := usageWeek(t, "2026-W43")
	old := northStarClone(t, w.From.AddDate(0, 0, -40)) // before the 30 days
	r, md := northStarReport(t, northStarVisits(t, "mvp", w.From.Add(30*time.Hour), 3), loadRunning(t, "mvp", old))
	ns := r.NorthStar
	if ns.Active != 0 || ns.VisitsNotRunning != 1 || ns.RunningNoVisits != 0 || ns.VisitsNotRunningEnvs[0] != "mvp" {
		t.Fatalf("north star = %+v, want mvp under visits without a running strategy", ns)
	}
	if !strings.Contains(md, "visits without a running strategy: mvp") {
		t.Fatalf("markdown:\n%s", md)
	}
}

// Scenario: a running strategy and no consented session → running without visits, not active.
func TestUsage_NorthStarRunningWithoutVisits(t *testing.T) {
	w := usageWeek(t, "2026-W43")
	data := northStarClone(t, w.From.Add(48*time.Hour))
	// mvp's people visit; stable's bot runs and nobody opened stable's monitor.
	r, _ := northStarReport(t, northStarVisits(t, "mvp", w.From.Add(30*time.Hour), 2), loadRunning(t, "stable", data))
	ns := r.NorthStar
	if ns.Active != 0 || ns.RunningNoVisits != 1 || ns.RunningNoVisitsEnvs[0] != "stable" || ns.VisitsNotRunning != 1 {
		t.Fatalf("north star = %+v, want stable running without visits, mvp visits without running", ns)
	}
	// A week with no event at all still reports the running bot.
	r, md := northStarReport(t, northStarVisits(t, "mvp", w.From.AddDate(0, 0, -60), 1), loadRunning(t, "stable", data))
	if r.EventsInWeek != 0 || r.NorthStar.RunningNoVisits != 1 || !strings.Contains(md, "## North star") {
		t.Fatalf("no-events week: events %d, north star %+v:\n%s", r.EventsInWeek, r.NorthStar, md)
	}
}

// Scenario: fewer than 10 people in the window → too few to judge, no share; 10 → judged.
func TestUsage_NorthStarTooFewPeople(t *testing.T) {
	w := usageWeek(t, "2026-W43")
	data := northStarClone(t, w.From.Add(24*time.Hour))
	r, md := northStarReport(t, northStarVisits(t, "mvp", w.From.Add(30*time.Hour), 9), loadRunning(t, "mvp", data))
	if ns := r.NorthStar; ns.Note != usage.StatusTooFew || ns.ActiveSharePct != nil || ns.Active != 1 {
		t.Fatalf("9 people: %+v, want counted but too few to judge", ns)
	}
	if !strings.Contains(md, "— (too few to judge: 9 people with a visit, under 10)") || strings.Contains(md, `"active_share_pct"`) {
		t.Fatalf("9 people: the share is given:\n%s", md)
	}
	r, _ = northStarReport(t, northStarVisits(t, "mvp", w.From.Add(30*time.Hour), 10), loadRunning(t, "mvp", data))
	if ns := r.NorthStar; ns.Note != "" || ns.ActiveSharePct == nil {
		t.Fatalf("10 people: %+v, want a share", ns)
	}
}

// Scenario: the .md and .json carry counts only — no coin or USD amount, no user or session id.
func TestUsage_NorthStarCarriesNoAmount(t *testing.T) {
	w := usageWeek(t, "2026-W43")
	data := northStarClone(t, w.From.Add(24*time.Hour))
	_, written := northStarReport(t, northStarVisits(t, "mvp", w.From.Add(30*time.Hour), 11), loadRunning(t, "mvp", data))
	for _, amount := range []string{"0.4242", "98765", "101234", "total_usd", "USD", "BTC", "NEGATIVE", "POSITIVE"} {
		if strings.Contains(written, amount) {
			t.Fatalf("report holds %q from the bot's files", amount)
		}
	}
	for n := 0; n <= 11; n++ {
		uid, sid := usageUser(n)
		if strings.Contains(written, uid) || strings.Contains(written, sid) {
			t.Fatalf("user %d's id appears in the report", n)
		}
	}
}

// No bot folder is no running day, not an error; the default Run leaves the section out.
func TestUsage_NorthStarNoBotFiles(t *testing.T) {
	running := loadRunning(t, "mvp", filepath.Join(t.TempDir(), "none"))
	if len(running.Files) != 0 || len(running.Days) != 0 {
		t.Fatalf("running = %+v", running)
	}
	w := usageWeek(t, "2026-W43")
	r, _, err := usage.Run(writeUsageEvents(t, northStarVisits(t, "mvp", w.From.Add(time.Hour), 1)), w.From, w.To, w.Label, t.TempDir())
	if err != nil || r.NorthStar != nil {
		t.Fatalf("Run: err %v, north star %+v: want none", err, r.NorthStar)
	}
}
