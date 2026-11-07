package assistantreview

import (
	"fmt"
	"strings"
)

// Candidate is one line of golden_candidates.jsonl: a question of the review
// list in the format of docs/product/assistant/golden.jsonl, with Category,
// ExpectedBehaviour and ExpectedSources left empty for a person to fill in.
// Nothing is ever added to the golden set itself.
type Candidate struct {
	ID                string   `json:"id"`
	Category          string   `json:"category"`
	Question          string   `json:"question"`
	ExpectedBehaviour string   `json:"expected_behaviour"`
	ExpectedSources   []string `json:"expected_sources"`
	MustSay           []string `json:"must_say"`
	MustNotSay        []string `json:"must_not_say"`
	// AnswerID is the review log's answer the question comes from.
	AnswerID string `json:"answer_id"`
}

// questionKey is a question's words, case and spacing aside.
func questionKey(q string) string {
	return strings.ToLower(strings.Join(strings.Fields(q), " "))
}

// Candidates are the review list's questions, each once, in the review's order.
// A carried row was offered in its own week, and a question whose answer was
// closed as golden_case is already a golden case (#704): neither is offered.
func Candidates(r Report) []Candidate {
	seen := map[string]bool{}
	out := []Candidate{}
	for _, it := range r.Review {
		key := questionKey(it.Question)
		if key == "" || seen[key] || it.Carried || r.goldenDone[key] {
			continue
		}
		seen[key] = true
		out = append(out, Candidate{
			ID:       fmt.Sprintf("cand-%s-%02d", r.Week, len(out)+1),
			Question: strings.TrimSpace(it.Question), AnswerID: it.ID,
			ExpectedSources: []string{}, MustSay: []string{}, MustNotSay: []string{},
		})
	}
	return out
}
