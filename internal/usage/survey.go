package usage

import (
	"slices"
	"sort"
	"strconv"
)

// The survey tally (#342) over the survey events of #297
// (docs/product/analytics/surveys.md): NPS, its coded why, the four answers
// and the coded exit reasons. Only allow-listed codes are read; free text is
// never an event, so it cannot reach the report.

// NPSMinAnswers is the number of NPS answers below which no score is given.
const NPSMinAnswers = 10

// TopExitReasons is how many exit reasons the report lists first.
const TopExitReasons = 3

// NoSurveyAnswers is what the report says when the window has no survey event.
const NoSurveyAnswers = "no survey answers yet"

// OwnerLine follows the top exit reasons in the report.
const OwnerLine = "Each top reason needs an owner: file it as an E13 story."

// Codes each survey asks (surveys.md §4); an answer with another code is
// counted under "other" and in Miscoded. NPS asks one list by score (#446):
// what works for 9–10, what to improve for 0–8.
var (
	npsWhatWorks = []string{"steady_income", "easy_to_follow", "saves_time", "clear_risk", "explains_trades", "trust_numbers", "helpful_answers", "other"}
	npsToImprove = []string{"value_unclear", "losses", "too_complex", "price", "trust", "hard_to_find", "missing_feature", "other"}
	exitCodes    = []string{"value_unclear", "losses", "too_complex", "price", "trust", "switched", "other"}
)

// Survey is Q8: why people stay or leave, as counts of coded answers.
type Survey struct {
	Surveys       []SurveyFunnel `json:"surveys"`         // nps, answers, exit
	NPS           NPS            `json:"nps"`             // survey_answered{nps} with a score
	Why           map[string]int `json:"why"`             // NPS reason_code: why_promoters + why_others, kept for old readers
	WhyPromoters  map[string]int `json:"why_promoters"`   // NPS 9–10: what we do best (#446)
	WhyOthers     map[string]int `json:"why_others"`      // NPS 0–8: what to improve first (#446)
	CameFor       map[string]int `json:"came_for"`        // answers survey
	HardestToFind map[string]int `json:"hardest_to_find"` // answers survey
	ExitReasons   map[string]int `json:"exit_reasons"`    // exit reason_code
	TopExit       []CodeCount    `json:"top_exit"`        // the TopExitReasons most frequent, most first
	Miscoded      int            `json:"miscoded"`        // answers whose code that survey does not ask, counted under other
}

// SurveyFunnel is one survey's shown, answered and dismissed counts.
type SurveyFunnel struct {
	ID          string   `json:"survey_id"`
	Shown       int      `json:"shown"`
	Answered    int      `json:"answered"`
	Dismissed   int      `json:"dismissed"`
	ResponsePct *float64 `json:"response_pct,omitempty"` // answered ÷ shown; absent when never shown
}

// NPS is the score's bands and, from NPSMinAnswers answers, the score:
// % promoters (9–10) − % detractors (0–6). Each person counts once, by their
// latest answer in the window (surveys.md §1: the same person may answer
// every month), and so does their why.
type NPS struct {
	Answers    int      `json:"answers"`
	Promoters  int      `json:"promoters"`
	Passives   int      `json:"passives"`
	Detractors int      `json:"detractors"`
	Score      *float64 `json:"score,omitempty"`
	TooFew     bool     `json:"too_few,omitempty"` // fewer than NPSMinAnswers answers: no score
}

// CodeCount is one code and how many answers chose it.
type CodeCount struct {
	Code  string `json:"code"`
	Count int    `json:"count"`
}

// buildSurvey tallies the window's survey events; nil when there is none.
func buildSurvey(week []Event) *Survey {
	ids := Properties["survey_id"].Values
	s := &Survey{
		Why:           map[string]int{},
		WhyPromoters:  map[string]int{},
		WhyOthers:     map[string]int{},
		CameFor:       map[string]int{},
		HardestToFind: map[string]int{},
		ExitReasons:   map[string]int{},
	}
	funnels := map[string]*SurveyFunnel{}
	for _, id := range ids {
		funnels[id] = &SurveyFunnel{ID: id}
	}
	seen := false
	latestNPS := map[string]Event{} // user → their latest NPS answer in the window
	for _, e := range week {
		id, _ := e.Props["survey_id"].(string)
		f := funnels[id]
		if f == nil {
			continue
		}
		switch e.Name {
		case "survey_shown":
			f.Shown++
		case "survey_dismissed":
			f.Dismissed++
		case "survey_answered":
			f.Answered++
			if id != "nps" {
				s.answer(id, e.Props)
			} else if last, ok := latestNPS[e.UserID]; !ok || !e.TS.Before(last.TS) {
				latestNPS[e.UserID] = e
			}
		default:
			continue
		}
		seen = true
	}
	if !seen {
		return nil
	}
	for _, e := range latestNPS {
		s.answer("nps", e.Props)
	}
	for _, id := range ids {
		f := funnels[id]
		f.ResponsePct = pct(f.Answered, f.Shown)
		s.Surveys = append(s.Surveys, *f)
	}
	if s.NPS.Answers >= NPSMinAnswers {
		v := 100 * float64(s.NPS.Promoters-s.NPS.Detractors) / float64(s.NPS.Answers)
		s.NPS.Score = &v
	} else {
		s.NPS.TooFew = true
	}
	s.TopExit = topCodes(s.ExitReasons, TopExitReasons)
	return s
}

// answer counts one survey_answered by its survey.
func (s *Survey) answer(id string, p map[string]any) {
	switch id {
	case "nps":
		promoter := false
		if sc, ok := p["score"].(string); ok {
			n, _ := strconv.Atoi(sc) // the allow-list holds "0" … "10" only
			s.NPS.Answers++
			switch {
			case n >= 9:
				s.NPS.Promoters++
				promoter = true
			case n >= 7:
				s.NPS.Passives++
			default:
				s.NPS.Detractors++
			}
		}
		// Each band was asked its own list (#446); a code of the other list counts under other.
		band, asked := s.WhyOthers, npsToImprove
		if promoter {
			band, asked = s.WhyPromoters, npsWhatWorks
		}
		if c := s.code(band, asked, p["reason_code"]); c != "" {
			s.Why[c]++
		}
	case "exit":
		s.code(s.ExitReasons, exitCodes, p["reason_code"])
	case "answers":
		if v, ok := p["came_for"].(string); ok {
			s.CameFor[v]++
		}
		if v, ok := p["hardest_to_find"].(string); ok {
			s.HardestToFind[v]++
		}
	}
}

// code counts a reason code the survey asks; another code is counted under
// "other" and in Miscoded, never guessed. → the code counted; "" (nothing
// counted) when there is none.
func (s *Survey) code(m map[string]int, asked []string, v any) string {
	c, ok := v.(string)
	if !ok {
		return ""
	}
	if !slices.Contains(asked, c) {
		s.Miscoded++
		c = "other"
	}
	m[c]++
	return c
}

// topCodes is the n most frequent codes, most first, ties by code.
func topCodes(m map[string]int, n int) []CodeCount {
	out := []CodeCount{}
	for k, v := range m {
		out = append(out, CodeCount{k, v})
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Count != out[j].Count {
			return out[i].Count > out[j].Count
		}
		return out[i].Code < out[j].Code
	})
	if len(out) > n {
		out = out[:n]
	}
	return out
}
