package tests

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"optionsbot/internal/compare"
	"optionsbot/internal/config"
)

// #641: the lab's run requests. The monitor queues a request file
// (frontend/lib/labrequests.js writes the shape of testdata/lab_request.json,
// which frontend/__tests__/lab-requests.test.js checks too); the runner
// (cmd/labrun) takes one at a time through cmd/compare -set … -neighbours.

var labNow = time.Date(2026, 11, 5, 1, 0, 0, 0, time.UTC)

const labReqID = "20261104T230000Z-a1b2c3d4"

// labQueue copies the fixture request into a new queue, with ids as given
// (each a copy of the fixture under its own id and time).
func labQueue(t *testing.T, ids ...string) (compare.LabRunOptions, []byte) {
	t.Helper()
	fixture, err := os.ReadFile("testdata/lab_request.json")
	if err != nil {
		t.Fatal(err)
	}
	root := t.TempDir()
	o := compare.LabRunOptions{QueueDir: filepath.Join(root, "compare", "requests"), OutDir: filepath.Join(root, "compare"), Base: "../config_{u}.yaml", Days: 7}
	if err := os.MkdirAll(o.QueueDir, 0o700); err != nil {
		t.Fatal(err)
	}
	for _, id := range ids {
		body := bytes.ReplaceAll(fixture, []byte(labReqID), []byte(id))
		if err := os.WriteFile(filepath.Join(o.QueueDir, id+".json"), body, 0o600); err != nil {
			t.Fatal(err)
		}
	}
	return o, fixture
}

func labRequest(t *testing.T, o compare.LabRunOptions, id string) compare.LabRequest {
	t.Helper()
	reqs, skipped, err := compare.ReadLabRequests(o.QueueDir)
	if err != nil || len(skipped) > 0 {
		t.Fatalf("read queue: %v, skipped %v", err, skipped)
	}
	for _, r := range reqs {
		if r.ID == id {
			return r
		}
	}
	t.Fatalf("no request %s", id)
	return compare.LabRequest{}
}

// fakeCompare writes a report folder like cmd/compare and prints its paths.
func fakeCompare(t *testing.T, o compare.LabRunOptions, reportID string) compare.CompareFunc {
	return func(_ context.Context, args []string) (string, error) {
		dir := filepath.Join(o.OutDir, reportID)
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(dir, "report.json"), []byte(`{"underlying":"BTC"}`), 0o644); err != nil {
			t.Fatal(err)
		}
		return "notes\n" + filepath.Join(dir, "report.md") + ", " + filepath.Join(dir, "report.json") + "\n", nil
	}
}

// Scenario: the runner finishes a request — running while cmd/compare runs,
// then done with the run id; cmd/compare gets the coin's shipped config, the
// proposed value through -set, -neighbours for it and the last 7 whole days.
func TestLabRunner_FinishesRequest(t *testing.T) {
	o, _ := labQueue(t, labReqID)
	var seen []string
	var during string
	done := fakeCompare(t, o, "20261105T010000Z")
	run := func(ctx context.Context, args []string) (string, error) {
		seen, during = args, labRequest(t, o, labReqID).Status
		return done(ctx, args)
	}
	sum, err := compare.RunLabQueue(context.Background(), o, func() time.Time { return labNow }, run)
	if err != nil {
		t.Fatal(err)
	}
	if during != compare.StatusRunning {
		t.Errorf("status while the comparison runs = %q, want running", during)
	}
	want := []string{"-base", "../config_btc.yaml", "-from", "2026-10-29", "-to", "2026-11-04", "-out", o.OutDir,
		"-set", "gamma_flip_buffer_sd=1.2", "-neighbours", "gamma_flip_buffer_sd"}
	if !slices.Equal(seen, want) {
		t.Errorf("cmd/compare args\n got %q\nwant %q", seen, want)
	}
	r := labRequest(t, o, labReqID)
	if r.Status != compare.StatusDone || r.RunID != "20261105T010000Z" || r.Reason != "" || !r.UpdatedAt.Equal(labNow) {
		t.Errorf("request after the run: %+v", r)
	}
	if sum != (compare.LabRunSummary{Done: 1}) {
		t.Errorf("summary %+v", sum)
	}
	if _, err := os.Stat(filepath.Join(o.QueueDir, compare.RunnerLock)); !os.IsNotExist(err) {
		t.Errorf("the lock must be released: %v", err)
	}
}

// Scenario: a failed run says so — failed with a short one-line reason, and
// the queue moves on to the next request.
func TestLabRunner_FailedRunSaysSo(t *testing.T) {
	first, second := "20261104T230000Z-00000001", "20261104T230500Z-00000002"
	o, _ := labQueue(t, first, second)
	done := fakeCompare(t, o, "20261105T010000Z")
	calls := 0
	run := func(ctx context.Context, args []string) (string, error) {
		calls++
		if calls == 1 {
			return "", errors.New("proposed /tmp/proposed-1.yaml: delta_drift_threshold must be at most 75 %\n" + strings.Repeat("x", 400))
		}
		return done(ctx, args)
	}
	sum, err := compare.RunLabQueue(context.Background(), o, func() time.Time { return labNow }, run)
	if err != nil {
		t.Fatal(err)
	}
	f := labRequest(t, o, first)
	if f.Status != compare.StatusFailed || f.RunID != "" || !strings.HasPrefix(f.Reason, "proposed /tmp/proposed-1.yaml: delta_drift_threshold") ||
		len([]rune(f.Reason)) > compare.ReasonMaxLen || strings.Contains(f.Reason, "\n") {
		t.Errorf("failed request: %+v", f)
	}
	if s := labRequest(t, o, second); s.Status != compare.StatusDone {
		t.Errorf("the queue must move on: %+v", s)
	}
	if sum != (compare.LabRunSummary{Done: 1, Failed: 1}) {
		t.Errorf("summary %+v", sum)
	}

	// A request that cannot run at all (no config for its coin, no report written) fails too.
	o, _ = labQueue(t, labReqID)
	body, _ := os.ReadFile(filepath.Join(o.QueueDir, labReqID+".json"))
	_ = os.WriteFile(filepath.Join(o.QueueDir, labReqID+".json"), bytes.Replace(body, []byte(`"BTC"`), []byte(`"SOL"`), 1), 0o600)
	if _, err := compare.RunLabQueue(context.Background(), o, func() time.Time { return labNow }, func(context.Context, []string) (string, error) {
		t.Error("cmd/compare must not run without a config")
		return "", nil
	}); err != nil {
		t.Fatal(err)
	}
	if r := labRequest(t, o, labReqID); r.Status != compare.StatusFailed || r.Reason != "no config for SOL (config_sol.yaml)" {
		t.Errorf("no config: %+v", r)
	}
	if _, err := compare.RunIDFromOutput("compare printed nothing useful"); err == nil {
		t.Error("output without a report path is an error")
	}
}

// Limits: a request older than a day is expired, never run; a request a
// stopped runner left running is failed; a second runner takes nothing.
func TestLabRunner_Limits(t *testing.T) {
	old := "20261103T230000Z-00000003"
	o, fixture := labQueue(t, old)
	stale := bytes.ReplaceAll(bytes.ReplaceAll(fixture, []byte(labReqID), []byte(old)), []byte("2026-11-04T23:00:00.000Z"), []byte("2026-11-03T23:00:00.000Z"))
	_ = os.WriteFile(filepath.Join(o.QueueDir, old+".json"), stale, 0o600)
	orphan := "20261104T220000Z-00000004"
	_ = os.WriteFile(filepath.Join(o.QueueDir, orphan+".json"),
		bytes.Replace(bytes.ReplaceAll(fixture, []byte(labReqID), []byte(orphan)), []byte(`"queued"`), []byte(`"running"`), 1), 0o600)
	sum, err := compare.RunLabQueue(context.Background(), o, func() time.Time { return labNow }, func(context.Context, []string) (string, error) {
		t.Error("nothing is queued to run")
		return "", nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if r := labRequest(t, o, old); r.Status != compare.StatusExpired || r.Reason == "" {
		t.Errorf("a day-old request: %+v", r)
	}
	if r := labRequest(t, o, orphan); r.Status != compare.StatusFailed || r.Reason != "the runner stopped before the run finished" {
		t.Errorf("an orphaned request: %+v", r)
	}
	if sum != (compare.LabRunSummary{Expired: 1, Orphaned: 1}) {
		t.Errorf("summary %+v", sum)
	}
	if compare.ExpireDue(compare.LabRequest{Status: compare.StatusQueued, RequestedAt: labNow.Add(-compare.RequestMaxAge)}, labNow) {
		t.Error("exactly a day old is not yet expired")
	}

	// At most one run at a time: a held lock leaves the queue alone; a lock
	// older than a day (a runner that died) is taken over.
	o, _ = labQueue(t, labReqID)
	lock := filepath.Join(o.QueueDir, compare.RunnerLock)
	if err := os.Mkdir(lock, 0o700); err != nil {
		t.Fatal(err)
	}
	busy := func() time.Time { return time.Now() }
	if _, err := compare.RunLabQueue(context.Background(), o, busy, fakeCompare(t, o, "20261105T010000Z")); !errors.Is(err, compare.ErrRunnerBusy) {
		t.Fatalf("held lock: err %v", err)
	}
	if r := labRequest(t, o, labReqID); r.Status != compare.StatusQueued {
		t.Errorf("a busy runner takes nothing: %+v", r)
	}
	later := func() time.Time { return time.Now().Add(compare.RequestMaxAge + time.Hour) }
	if _, err := compare.RunLabQueue(context.Background(), o, later, fakeCompare(t, o, "20261105T010000Z")); err != nil {
		t.Fatalf("stale lock: %v", err)
	}

	// A file that is not a request is skipped and named, never run.
	_ = os.WriteFile(filepath.Join(o.QueueDir, "20261104T230000Z-deadbeef.json"), []byte("{"), 0o600)
	_ = os.WriteFile(filepath.Join(o.QueueDir, "notes.txt"), []byte("x"), 0o600)
	if _, skipped, err := compare.ReadLabRequests(o.QueueDir); err != nil || !slices.Equal(skipped, []string{"20261104T230000Z-deadbeef"}) {
		t.Errorf("skipped %v, err %v", skipped, err)
	}
}

// The neighbours study one number at one place; a text, a bool or a whole
// list ("[]") has none. Data folders are passed only when they exist.
func TestLabRunner_CompareArgs(t *testing.T) {
	for _, c := range []struct {
		settings []compare.LabSetting
		want     string
	}{
		{[]compare.LabSetting{{Key: "gex_direction", Value: "bias"}, {Key: "roi_take_profit", Value: 0.6}}, "roi_take_profit"},
		{[]compare.LabSetting{{Key: "dte_delta_matrix[].dte", Value: 30.0}}, ""},
		{[]compare.LabSetting{{Key: "dte_delta_matrix[0].dte", Value: 30.0}}, "dte_delta_matrix[0].dte"},
		{[]compare.LabSetting{{Key: "backtest.margin_call_liquidation", Value: false}}, ""},
	} {
		if got := compare.NeighbourKey(c.settings); got != c.want {
			t.Errorf("%v: neighbour key %q, want %q", c.settings, got, c.want)
		}
	}
	r := compare.LabRequest{Underlying: "ETH", Settings: []compare.LabSetting{{Key: "gex_direction", Value: "bias"}}}
	o := compare.LabRunOptions{Base: "config_{u}.yaml", OutDir: "out", Replay: "data/{u}/data/market", Stress: "data/stress/{u}", Days: 1}
	args, err := compare.CompareArgs(r, o, labNow, func(p string) bool { return p != "data/eth/data/market" })
	if err != nil {
		t.Fatal(err)
	}
	want := []string{"-base", "config_eth.yaml", "-from", "2026-11-04", "-to", "2026-11-04", "-out", "out", "-set", `gex_direction="bias"`, "-stress", "data/stress/eth"}
	if !slices.Equal(args, want) {
		t.Errorf("args\n got %q\nwant %q", args, want)
	}
	for _, bad := range []compare.LabRequest{
		{Underlying: "../x", Settings: r.Settings},
		{Underlying: "BTC"},
		{Underlying: "BTC", Settings: []compare.LabSetting{{Key: "a=b -base x", Value: 1.0}}},
	} {
		if _, err := compare.CompareArgs(bad, o, labNow, func(string) bool { return true }); err == nil {
			t.Errorf("%+v must not run", bad)
		}
	}
}

// cmd/compare -set: the proposed config is a temporary copy of the base with
// only those values; the base file is untouched (nothing is applied).
func TestCompareSet_ProposedFromBase(t *testing.T) {
	t.Setenv("DERIBIT_ENV", "testnet")
	before, err := os.ReadFile("../config_btc.yaml")
	if err != nil {
		t.Fatal(err)
	}
	var sets []compare.Setting
	for _, s := range []string{"gamma_flip_buffer_sd=1.2", `gex_direction="sma"`, "dte_delta_matrix[].deltas[0]=0.17"} {
		one, err := compare.ParseSetting(s)
		if err != nil {
			t.Fatal(err)
		}
		sets = append(sets, one)
	}
	tmp, err := compare.WriteWith("../config_btc.yaml", sets)
	if err != nil {
		t.Fatal(err)
	}
	defer os.Remove(tmp)
	base, err := config.Load("../config_btc.yaml")
	if err != nil {
		t.Fatal(err)
	}
	proposed, err := config.Load(tmp)
	if err != nil {
		t.Fatal(err)
	}
	var keys []string
	for _, d := range compare.DiffConfigs(base, proposed) {
		keys = append(keys, d.Key)
	}
	slices.Sort(keys)
	want := []string{"gamma_flip_buffer_sd", "gex_direction"}
	for i := range base.DTEDeltaMatrix {
		if base.DTEDeltaMatrix[i].Deltas[0] != 0.17 {
			want = append(want, fmt.Sprintf("dte_delta_matrix[%d].deltas[0]", i))
		}
	}
	slices.Sort(want)
	if !slices.Equal(keys, want) {
		t.Errorf("differences %v, want %v", keys, want)
	}
	after, _ := os.ReadFile("../config_btc.yaml")
	if !bytes.Equal(before, after) {
		t.Error("the base config must not change")
	}
	if got := compare.SettingsLabel("config_btc.yaml", sets[:2]); got != "config_btc.yaml with gamma_flip_buffer_sd=1.2, gex_direction=sma" {
		t.Errorf("label %q", got)
	}

	for _, bad := range []string{"no_equals", "Bad Key=1", "a=[1, 2]", "a=", "dte_delta_matrix[9].dte=1"} {
		s, err := compare.ParseSetting(bad)
		if err == nil {
			_, err = compare.WriteWith("../config_btc.yaml", []compare.Setting{s})
		}
		if err == nil {
			t.Errorf("%q must be refused", bad)
		}
	}
}
