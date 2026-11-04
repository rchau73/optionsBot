package compare

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"time"

	"optionsbot/internal/config"
)

// The lab's run requests (#641, epic E9): the monitor's POST /api/lab/requests
// writes one small file per request into a private queue folder
// (data/compare/requests/<id>.json, frontend/lib/labrequests.js); the runner
// (cmd/labrun, scripts/lab_runner.sh) takes one queued request at a time and
// runs cmd/compare with -set for each proposed value and -neighbours for the
// changed number, then writes the status back: running, then done (with the
// report's id) or failed (with a short reason). A request still queued a day
// after it was made is expired. Nothing here writes a config or applies a
// setting: the proposed config is cmd/compare's temporary copy.

// Request statuses.
const (
	StatusQueued  = "queued"
	StatusRunning = "running"
	StatusDone    = "done"
	StatusFailed  = "failed"
	StatusExpired = "expired"
)

// RequestMaxAge is how long a request may wait in the queue before it is
// expired; also how old the runner's lock may be before a crashed runner's
// lock is taken over.
const RequestMaxAge = 24 * time.Hour

// ReasonMaxLen caps a failed request's reason: one short line.
const ReasonMaxLen = 200

// RunnerLock is the lock folder in the queue: at most one run at a time.
const RunnerLock = ".runner.lock"

// ErrRunnerBusy says another runner holds the lock.
var ErrRunnerBusy = errors.New("another lab run is in progress")

// requestIDRe is a request id: its UTC time and a random suffix
// (20261104T230000Z-a1b2c3d4), and so its file name.
var requestIDRe = regexp.MustCompile(`^\d{8}T\d{6}Z-[0-9a-f]{8}$`)

// underlyingRe is an underlying as the monitor writes it (BTC, ETH).
var underlyingRe = regexp.MustCompile(`^[A-Z]{2,10}$`)

// LabRequest is one request file. It holds no secret and no amount: the
// person's username, the time, the underlying and the proposed values.
type LabRequest struct {
	ID          string       `json:"id"`
	Requester   string       `json:"requester"`
	RequestedAt time.Time    `json:"requested_at"`
	Underlying  string       `json:"underlying"`
	Settings    []LabSetting `json:"settings"`
	Status      string       `json:"status"`
	UpdatedAt   time.Time    `json:"updated_at"`
	RunID       string       `json:"run_id,omitempty"` // the report folder under data/compare, once done
	Reason      string       `json:"reason,omitempty"` // why it failed or expired
}

// LabSetting is one proposed value of a request.
type LabSetting struct {
	Key   string `json:"key"`
	Value any    `json:"value"`
}

// LabRunOptions are where the runner reads and writes. Paths may hold {u},
// the underlying in lower case (config_{u}.yaml).
type LabRunOptions struct {
	QueueDir string // data/compare/requests
	OutDir   string // data/compare: cmd/compare's -out
	Base     string // config_{u}.yaml
	Replay   string // data/{u}/data/market: passed only when it exists
	Stress   string // data/stress/{u}: passed only when it exists
	CSV      string // backtest data; "" = cmd/compare's default
	Days     int    // the window: the last Days whole UTC days
}

// CompareFunc runs cmd/compare with args and returns its standard output;
// an error carries a short reason.
type CompareFunc func(ctx context.Context, args []string) (string, error)

// LabRunSummary counts what one runner pass did.
type LabRunSummary struct {
	Done, Failed, Expired, Orphaned int
}

// ReadLabRequests reads every request file of dir, oldest first. A file that
// is not a request id or does not parse is skipped and named in skipped
// (ids only). A missing dir is an empty queue.
func ReadLabRequests(dir string) (reqs []LabRequest, skipped []string, err error) {
	entries, err := os.ReadDir(dir)
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil, nil
	}
	if err != nil {
		return nil, nil, err
	}
	for _, e := range entries {
		id, ok := strings.CutSuffix(e.Name(), ".json")
		if !ok || !requestIDRe.MatchString(id) {
			continue
		}
		body, err := os.ReadFile(filepath.Join(dir, e.Name()))
		var r LabRequest
		if err == nil {
			err = json.Unmarshal(body, &r)
		}
		if err != nil || r.ID != id {
			skipped = append(skipped, id)
			continue
		}
		reqs = append(reqs, r)
	}
	sort.Slice(reqs, func(i, j int) bool { return reqs[i].ID < reqs[j].ID })
	return reqs, skipped, nil
}

// SaveLabRequest writes r to its file through a temporary file and a rename,
// so the monitor never reads half a request.
func SaveLabRequest(dir string, r LabRequest) error {
	if !requestIDRe.MatchString(r.ID) {
		return fmt.Errorf("request id %q is not a request id", r.ID)
	}
	data, err := json.MarshalIndent(r, "", "  ")
	if err != nil {
		return err
	}
	file := filepath.Join(dir, r.ID+".json")
	tmp := file + ".tmp"
	if err := os.WriteFile(tmp, append(data, '\n'), 0o600); err != nil {
		return err
	}
	return os.Rename(tmp, file)
}

// ExpireDue reports whether a queued request has waited longer than RequestMaxAge.
func ExpireDue(r LabRequest, now time.Time) bool {
	return r.Status == StatusQueued && now.Sub(r.RequestedAt) > RequestMaxAge
}

// NeighbourKey is the setting -neighbours studies: the first proposed value
// that is a number of a number setting at one place (no "[]": a neighbour
// moves one value). "" when there is none.
func NeighbourKey(settings []LabSetting) string {
	for _, s := range settings {
		if _, ok := s.Value.(float64); !ok || strings.Contains(s.Key, "[]") {
			continue
		}
		if t, ok := TunableFor(s.Key); ok && (t.Kind == config.KindNumber || t.Kind == config.KindInteger) {
			return s.Key
		}
	}
	return ""
}

// LabWindow is the run's window: the last days whole UTC days, ending yesterday.
func LabWindow(now time.Time, days int) (from, to time.Time) {
	if days < 1 {
		days = 1
	}
	today := now.UTC().Truncate(24 * time.Hour)
	return today.AddDate(0, 0, -days), today.AddDate(0, 0, -1)
}

// CompareArgs are cmd/compare's arguments for r, or an error saying why the
// request cannot run. exists says whether a data folder is there.
func CompareArgs(r LabRequest, o LabRunOptions, now time.Time, exists func(string) bool) ([]string, error) {
	if !underlyingRe.MatchString(r.Underlying) {
		return nil, fmt.Errorf("unknown underlying %q", r.Underlying)
	}
	if len(r.Settings) == 0 {
		return nil, errors.New("no proposed value")
	}
	u := strings.ToLower(r.Underlying)
	fill := func(p string) string { return strings.ReplaceAll(p, "{u}", u) }
	base := fill(o.Base)
	if !exists(base) {
		return nil, fmt.Errorf("no config for %s (%s)", r.Underlying, filepath.Base(base))
	}
	from, to := LabWindow(now, o.Days)
	args := []string{"-base", base, "-from", from.Format(time.DateOnly), "-to", to.Format(time.DateOnly), "-out", o.OutDir}
	for _, s := range r.Settings {
		if !SettingKeyRe.MatchString(s.Key) {
			return nil, fmt.Errorf("setting %q is not a setting key", s.Key)
		}
		v, err := json.Marshal(s.Value) // 1.2, true, "bias": cmd/compare reads it as YAML
		if err != nil {
			return nil, fmt.Errorf("setting %s: %w", s.Key, err)
		}
		args = append(args, "-set", s.Key+"="+string(v))
	}
	if k := NeighbourKey(r.Settings); k != "" {
		args = append(args, "-neighbours", k)
	}
	if o.CSV != "" {
		args = append(args, "-csv", o.CSV)
	}
	for _, d := range []struct{ flag, path string }{{"-replay", o.Replay}, {"-stress", o.Stress}} {
		if d.path != "" && exists(fill(d.path)) {
			args = append(args, d.flag, fill(d.path))
		}
	}
	return args, nil
}

// reportIDRe is a report folder's name (OutDir).
var reportIDRe = regexp.MustCompile(`^\d{8}T\d{6}Z$`)

// RunIDFromOutput reads the report's id from cmd/compare's output (its last
// line: "<out>/<id>/report.md, <out>/<id>/report.json").
func RunIDFromOutput(stdout string) (string, error) {
	lines := strings.Split(strings.TrimSpace(stdout), "\n")
	last := strings.TrimSpace(lines[len(lines)-1])
	_, js, ok := strings.Cut(last, ", ")
	id := filepath.Base(filepath.Dir(strings.TrimSpace(js)))
	if !ok || !reportIDRe.MatchString(id) {
		return "", errors.New("the comparison wrote no report")
	}
	return id, nil
}

// ShortReason is err as one line of at most ReasonMaxLen characters.
func ShortReason(err error) string {
	s := strings.Join(strings.Fields(err.Error()), " ")
	if r := []rune(s); len(r) > ReasonMaxLen {
		s = string(r[:ReasonMaxLen-1]) + "…"
	}
	return s
}

// RunLabQueue is one runner pass: it takes the lock (ErrRunnerBusy when
// another runner holds it), marks a request a stopped runner left running as
// failed, expires the requests that waited too long, then runs the queued
// ones oldest first, one at a time, until none is left. A failed run is
// marked failed and the queue moves on.
func RunLabQueue(ctx context.Context, o LabRunOptions, now func() time.Time, run CompareFunc) (LabRunSummary, error) {
	var sum LabRunSummary
	unlock, err := lockQueue(o.QueueDir, now())
	if err != nil {
		return sum, err
	}
	defer unlock()
	exists := func(p string) bool { _, err := os.Stat(p); return err == nil }
	for {
		reqs, _, err := ReadLabRequests(o.QueueDir)
		if err != nil {
			return sum, err
		}
		var next *LabRequest
		for i := range reqs {
			r := &reqs[i]
			switch {
			case r.Status == StatusRunning: // the lock is ours: a runner stopped mid-run
				sum.Orphaned++
				if err := finish(o.QueueDir, r, now(), StatusFailed, "", "the runner stopped before the run finished"); err != nil {
					return sum, err
				}
			case ExpireDue(*r, now()):
				sum.Expired++
				if err := finish(o.QueueDir, r, now(), StatusExpired, "", "not run within a day of the request"); err != nil {
					return sum, err
				}
			case r.Status == StatusQueued && next == nil:
				next = r
			}
		}
		if next == nil {
			return sum, nil
		}
		if err := finish(o.QueueDir, next, now(), StatusRunning, "", ""); err != nil {
			return sum, err
		}
		args, err := CompareArgs(*next, o, now(), exists)
		var stdout string
		if err == nil {
			stdout, err = run(ctx, args)
		}
		var id string
		if err == nil {
			id, err = RunIDFromOutput(stdout)
		}
		if err != nil {
			sum.Failed++
			if err := finish(o.QueueDir, next, now(), StatusFailed, "", ShortReason(err)); err != nil {
				return sum, err
			}
			continue
		}
		sum.Done++
		if err := finish(o.QueueDir, next, now(), StatusDone, id, ""); err != nil {
			return sum, err
		}
	}
}

func finish(dir string, r *LabRequest, now time.Time, status, runID, reason string) error {
	r.Status, r.UpdatedAt, r.RunID, r.Reason = status, now.UTC(), runID, reason
	return SaveLabRequest(dir, *r)
}

// lockQueue takes the queue's lock folder; a lock older than RequestMaxAge
// (a runner that died) is taken over.
func lockQueue(dir string, now time.Time) (func(), error) {
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return nil, err
	}
	lock := filepath.Join(dir, RunnerLock)
	err := os.Mkdir(lock, 0o700)
	if errors.Is(err, os.ErrExist) {
		if fi, serr := os.Stat(lock); serr == nil && now.Sub(fi.ModTime()) > RequestMaxAge {
			if rerr := os.Remove(lock); rerr != nil {
				return nil, rerr
			}
			err = os.Mkdir(lock, 0o700)
		}
	}
	if errors.Is(err, os.ErrExist) {
		return nil, ErrRunnerBusy
	}
	if err != nil {
		return nil, err
	}
	return func() { os.Remove(lock) }, nil
}
