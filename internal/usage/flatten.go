package usage

// The cohort flattening check (#607): has a first-week cohort's retention
// curve stopped falling? PRD §7's retention target is "the curve flattens by
// month 3". Fixed in code; cmd/usage's -flat-* flags vary them to study them.
const (
	// FlatMinUsers: a cohort is judged only with at least this many users.
	FlatMinUsers = 10
	// FlatWeeks: how many of a cohort's last ended weeks are compared, each
	// with the week before it (so FlatWeeks+1 ended weeks are needed).
	FlatWeeks = 3
	// FlatStepPts: a week within this many percentage points of the week
	// before is flat; a fall of more is falling.
	FlatStepPts = 5.0
)

// The statuses of a cohort's curve.
const (
	StatusFlattening = "flattening" // each of the last FlatWeeks weeks within FlatStepPts of the one before
	StatusFalling    = "falling"    // one of them fell by more than FlatStepPts
	StatusRising     = "rising"     // none fell past FlatStepPts, but one rose by more: not flat yet
	StatusTooFew     = TooFew       // under FlatMinUsers users, or under FlatWeeks+1 ended weeks
)

// FlatRules are the thresholds of the flattening check.
type FlatRules struct {
	MinUsers int     `json:"min_users"` // FlatMinUsers
	Weeks    int     `json:"weeks"`     // FlatWeeks
	StepPts  float64 `json:"step_pts"`  // FlatStepPts
}

// DefaultFlatRules are the fixed rules.
func DefaultFlatRules() FlatRules {
	return FlatRules{MinUsers: FlatMinUsers, Weeks: FlatWeeks, StepPts: FlatStepPts}
}

// Flattening is the cohort's status on the fixed rules. Pure.
func Flattening(c Cohort) string {
	return FlatteningWith(c, DefaultFlatRules())
}

// FlatteningWith is the cohort's status on rules: only its ended weeks
// (Retained) are read, so a week not yet ended never counts as a fall to 0.
// A fall past StepPts wins over a rise. Pure.
func FlatteningWith(c Cohort, rules FlatRules) string {
	if c.Users < rules.MinUsers || rules.Weeks < 1 || len(c.Retained) < rules.Weeks+1 {
		return StatusTooFew
	}
	last := c.Retained[len(c.Retained)-rules.Weeks-1:]
	status := StatusFlattening
	for i := 1; i < len(last); i++ {
		if last[i].Pct == nil || last[i-1].Pct == nil {
			return StatusTooFew
		}
		step := *last[i].Pct - *last[i-1].Pct
		switch {
		case step < -rules.StepPts:
			return StatusFalling
		case step > rules.StepPts:
			status = StatusRising
		}
	}
	return status
}

// judgeCohorts sets every cohort's status on rules.
func judgeCohorts(cs []Cohort, rules FlatRules) {
	for i := range cs {
		cs[i].Status = FlatteningWith(cs[i], rules)
	}
}
