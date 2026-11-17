package compare

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"
)

// The lab's usage report (#824, epic E9, cmd/labstats): how the lab is used
// and whether its runs finish, from its own files only — the request queue
// (COMPARE_DIR/requests/), the recorded changes (applied/), their candidates'
// status (candidates/<id>/candidate.json) and whether a done run's analysis
// was written (<run id>/analysis.json). It reads ids, statuses and times:
// no setting value, amount, account detail or name reaches the report; a
// requester is a short hash. Read-only.

// LabStatsDays is the default window: the last 28 days.
const LabStatsDays = 28

// The targets, assumptions to confirm after the first four weeks (#824).
const (
	LabTargetDonePct       = 90.0
	LabTargetMedianMinutes = 30.0
)

// LabNoRequests is the report of a window without a request.
const LabNoRequests = "no requests in the window"

// LabUsageFiles is what ReadLabUsage found in COMPARE_DIR.
type LabUsageFiles struct {
	Requests          []LabRequest
	Unreadable        int             // request files that cannot be read or parsed
	Applied           map[string]bool // run ids with a recorded change (applied/<id>.json)
	AppliedUnreadable int             // applied/ files whose name is not a run id
	CorePolicyRefused map[string]bool // run ids whose candidate was refused for the product owner
	Analysed          map[string]bool // run ids with an analysis written beside their report
}

// ReadLabUsage reads the lab's files under compareDir. Missing folders are an
// empty lab; a request file that cannot be read is counted, never skipped
// silently.
func ReadLabUsage(compareDir string) (LabUsageFiles, error) {
	f := LabUsageFiles{Applied: map[string]bool{}, CorePolicyRefused: map[string]bool{}, Analysed: map[string]bool{}}
	reqs, skipped, err := ReadLabRequests(filepath.Join(compareDir, "requests"))
	if err != nil {
		return f, fmt.Errorf("read the request queue: %w", err)
	}
	f.Requests, f.Unreadable = reqs, len(skipped)
	entries, err := os.ReadDir(filepath.Join(compareDir, "applied"))
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		return f, fmt.Errorf("read the recorded changes: %w", err)
	}
	for _, e := range entries {
		id, ok := strings.CutSuffix(e.Name(), ".json")
		if !ok {
			continue
		}
		if !runIDRe.MatchString(id) {
			f.AppliedUnreadable++
			continue
		}
		f.Applied[id] = true
		if refusedForProductOwner(filepath.Join(compareDir, "candidates", id, CandidateStatusFile)) {
			f.CorePolicyRefused[id] = true
		}
	}
	for _, r := range reqs {
		if r.Status != StatusDone || !runIDRe.MatchString(r.RunID) {
			continue
		}
		if _, err := os.Stat(filepath.Join(compareDir, r.RunID, "analysis.json")); err == nil {
			f.Analysed[r.RunID] = true
		}
	}
	return f, nil
}

// refusedForProductOwner says whether a candidate.json says refused as a core
// policy without the product owner's approval; a missing or bad file says no.
func refusedForProductOwner(path string) bool {
	raw, err := os.ReadFile(path)
	if err != nil {
		return false
	}
	var st struct {
		Status            string `json:"status"`
		NeedsProductOwner bool   `json:"needs_product_owner"`
	}
	return json.Unmarshal(raw, &st) == nil && st.Status == CandidateRefused && st.NeedsProductOwner
}

// LabStats is the report: counts, shares and minutes only.
type LabStats struct {
	From              time.Time         `json:"from"`
	To                time.Time         `json:"to"`
	Days              int               `json:"days"`
	Requests          int               `json:"requests"`
	Done              int               `json:"done"`
	Failed            int               `json:"failed"`
	Expired           int               `json:"expired"`
	Open              int               `json:"open"` // queued or running
	DonePct           float64           `json:"done_pct"`
	FailedPct         float64           `json:"failed_pct"`
	ExpiredPct        float64           `json:"expired_pct"`
	Unreadable        int               `json:"unreadable"`
	AppliedUnreadable int               `json:"applied_unreadable"`
	MedianMinutes     *float64          `json:"median_minutes_to_done"` // nil: no done run
	Managers          int               `json:"managers"`
	PerManagerWeek    []LabManagerWeek  `json:"per_manager_week"`
	Recorded          int               `json:"recorded_changes"`
	RecordedPctOfDone float64           `json:"recorded_pct_of_done"`
	CorePolicyRefused int               `json:"core_policy_refused"`
	AnalysesWritten   int               `json:"analyses_written"`
	Targets           []LabTargetResult `json:"targets"`
}

// LabManagerWeek is one manager's requests in one ISO week, under a short hash.
type LabManagerWeek struct {
	Week     string `json:"week"`
	Manager  string `json:"manager"`
	Requests int    `json:"requests"`
}

// LabTargetResult is one target; Met is nil when there is nothing to judge.
type LabTargetResult struct {
	Name string `json:"name"`
	Want string `json:"want"`
	Met  *bool  `json:"met"`
}

// ManagerHash is a requester as 8 hex characters of its SHA-256: the same
// person is the same hash across weeks, never a name.
func ManagerHash(requester string) string {
	sum := sha256.Sum256([]byte(requester))
	return hex.EncodeToString(sum[:])[:8]
}

// ComputeLabStats is the report of the requests made in the days before now.
func ComputeLabStats(f LabUsageFiles, now time.Time, days int) LabStats {
	if days < 1 {
		days = LabStatsDays
	}
	to := now.UTC()
	s := LabStats{From: to.AddDate(0, 0, -days), To: to, Days: days, Unreadable: f.Unreadable,
		AppliedUnreadable: f.AppliedUnreadable, PerManagerWeek: []LabManagerWeek{}}
	var minutes []float64
	perWeek := map[LabManagerWeek]int{}
	managers := map[string]bool{}
	for _, r := range f.Requests {
		at := r.RequestedAt.UTC()
		if !at.After(s.From) || at.After(to) {
			continue
		}
		s.Requests++
		switch r.Status {
		case StatusDone:
			s.Done++
			minutes = append(minutes, r.UpdatedAt.Sub(r.RequestedAt).Minutes())
			if f.Applied[r.RunID] {
				s.Recorded++
				if f.CorePolicyRefused[r.RunID] {
					s.CorePolicyRefused++
				}
			}
			if f.Analysed[r.RunID] && r.Note == "" {
				s.AnalysesWritten++
			}
		case StatusFailed:
			s.Failed++
		case StatusExpired:
			s.Expired++
		default:
			s.Open++
		}
		y, w := at.ISOWeek()
		h := ManagerHash(r.Requester)
		managers[h] = true
		perWeek[LabManagerWeek{Week: fmt.Sprintf("%d-W%02d", y, w), Manager: h}]++
	}
	s.Managers = len(managers)
	for k, n := range perWeek {
		k.Requests = n
		s.PerManagerWeek = append(s.PerManagerWeek, k)
	}
	sort.Slice(s.PerManagerWeek, func(i, j int) bool {
		a, b := s.PerManagerWeek[i], s.PerManagerWeek[j]
		if a.Week != b.Week {
			return a.Week < b.Week
		}
		if a.Requests != b.Requests {
			return a.Requests > b.Requests
		}
		return a.Manager < b.Manager
	})
	s.DonePct, s.FailedPct, s.ExpiredPct = pctOf(s.Done, s.Requests), pctOf(s.Failed, s.Requests), pctOf(s.Expired, s.Requests)
	s.RecordedPctOfDone = pctOf(s.Recorded, s.Done)
	if len(minutes) > 0 {
		m := math.Round(median(minutes)*10) / 10
		s.MedianMinutes = &m
	}
	s.Targets = labTargets(s)
	return s
}

func labTargets(s LabStats) []LabTargetResult {
	done := LabTargetResult{Name: "done share", Want: ">= " + num(LabTargetDonePct) + " %"}
	if s.Requests > 0 {
		met := s.DonePct >= LabTargetDonePct
		done.Met = &met
	}
	fast := LabTargetResult{Name: "median time to a finished run", Want: "<= " + num(LabTargetMedianMinutes) + " min"}
	if s.MedianMinutes != nil {
		met := *s.MedianMinutes <= LabTargetMedianMinutes
		fast.Met = &met
	}
	return []LabTargetResult{done, fast}
}

func pctOf(n, of int) float64 {
	if of == 0 {
		return 0
	}
	return math.Round(float64(n)/float64(of)*1000) / 10
}

// num is a figure without trailing zeros: 50, 33.3.
func num(x float64) string { return strconv.FormatFloat(x, 'f', -1, 64) }

// LabStatsMarkdown is the report as a short Markdown section.
func LabStatsMarkdown(s LabStats) string {
	var b strings.Builder
	fmt.Fprintf(&b, "## Lab usage\n\nRequests made %s – %s UTC (%d days).\n\n",
		s.From.Format("2006-01-02 15:04"), s.To.Format("2006-01-02 15:04"), s.Days)
	if s.Requests == 0 {
		b.WriteString(LabNoRequests + ".\n")
		writeUnreadable(&b, s)
		return b.String()
	}
	medianText := "no finished run"
	if s.MedianMinutes != nil {
		medianText = num(*s.MedianMinutes) + " min"
	}
	b.WriteString("| Figure | Value |\n|---|---|\n")
	fmt.Fprintf(&b, "| Requests | %d |\n", s.Requests)
	fmt.Fprintf(&b, "| Done | %d (%s %%) |\n", s.Done, num(s.DonePct))
	fmt.Fprintf(&b, "| Failed | %d (%s %%) |\n", s.Failed, num(s.FailedPct))
	fmt.Fprintf(&b, "| Expired | %d (%s %%) |\n", s.Expired, num(s.ExpiredPct))
	fmt.Fprintf(&b, "| Still queued or running | %d |\n", s.Open)
	fmt.Fprintf(&b, "| Median time to a finished run | %s |\n", medianText)
	fmt.Fprintf(&b, "| Managers | %d |\n", s.Managers)
	fmt.Fprintf(&b, "| Done runs with a recorded change | %d of %d (%s %%) |\n", s.Recorded, s.Done, num(s.RecordedPctOfDone))
	fmt.Fprintf(&b, "| Recorded changes refused as a core policy | %d |\n", s.CorePolicyRefused)
	fmt.Fprintf(&b, "| AI analyses written (withheld until L10) | %d |\n", s.AnalysesWritten)
	b.WriteString("\n| Week | Manager | Requests |\n|---|---|---|\n")
	for _, m := range s.PerManagerWeek {
		fmt.Fprintf(&b, "| %s | %s | %d |\n", m.Week, m.Manager, m.Requests)
	}
	b.WriteString("\nTargets (assumptions to confirm after the first four weeks):\n\n")
	for _, t := range s.Targets {
		state := "too early to judge"
		if t.Met != nil && *t.Met {
			state = "met"
		} else if t.Met != nil {
			state = "not met"
		}
		fmt.Fprintf(&b, "- %s %s: %s\n", t.Name, t.Want, state)
	}
	writeUnreadable(&b, s)
	return b.String()
}

func writeUnreadable(b *strings.Builder, s LabStats) {
	if s.Unreadable > 0 {
		fmt.Fprintf(b, "\nUnreadable request files (counted in no figure above): %d.\n", s.Unreadable)
	}
	if s.AppliedUnreadable > 0 {
		fmt.Fprintf(b, "\nRecorded-change files not named by a run id: %d.\n", s.AppliedUnreadable)
	}
}
