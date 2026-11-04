package usage

import (
	"sort"
	"time"
)

// FollowUpWindow is how long after a tip opens an assistant question in the
// same session still counts as its follow-up (#642).
const FollowUpWindow = 10 * time.Minute

// TopTips is how many tip keys each rewrite-candidate list names.
const TopTips = 5

// TipCount is one tip key and how many times it was opened.
type TipCount struct {
	Tip   string `json:"tip"`
	Count int    `json:"count"`
}

// Education is E11's measure (#642): are tips and videos used, and do they
// resolve the question before the assistant is needed. Ids and counts only.
type Education struct {
	NoData bool   `json:"no_data,omitempty"` // no education event in the window: no rate below is invented
	Why    string `json:"why,omitempty"`

	ActivePeople          int        `json:"active_people"`
	TipsOpened            int        `json:"tips_opened"`
	VideosPlayed          int        `json:"videos_played"`
	VideosCompleted       int        `json:"videos_completed"`
	TipsPerPerson         *float64   `json:"tips_per_person,omitempty"`
	VideosPerPerson       *float64   `json:"videos_per_person,omitempty"`
	VideoCompletionPct    *float64   `json:"video_completion_pct,omitempty"`
	TipVideoAnswers       int        `json:"tip_video_answers"` // help_resolved
	TipVideoResolved      int        `json:"tip_video_resolved"`
	AssistantAnswers      int        `json:"assistant_answers"` // assistant_helpful
	AssistantResolved     int        `json:"assistant_resolved"`
	ResolvedByTipVideoPct *float64   `json:"resolved_by_tip_video_pct,omitempty"` // of all resolved help: tips and videos against the assistant
	NoFollowUp            []TipCount `json:"no_follow_up,omitempty"`              // opened with no assistant question after
	FollowedByQuestion    []TipCount `json:"followed_by_question,omitempty"`      // opened, then an assistant question: rewrite candidates
}

const educationNoData = "no tip, video or help answer was recorded in the window (the first tip or video screen sends them)."

func buildEducation(week []Event, active int) *Education {
	e := &Education{ActivePeople: active}
	// asks[session] = times of assistant_ask, ascending (week is sorted).
	asks := map[string][]time.Time{}
	for _, ev := range week {
		if ev.Name == "assistant_ask" {
			asks[ev.SessionID] = append(asks[ev.SessionID], ev.TS)
		}
	}
	plain, followed := map[string]int{}, map[string]int{}
	for _, ev := range week {
		switch ev.Name {
		case "tip_opened":
			e.TipsOpened++
			key, _ := ev.Props["tip_id"].(string)
			if key == "" {
				continue
			}
			if askedWithin(asks[ev.SessionID], ev.TS) {
				followed[key]++
			} else {
				plain[key]++
			}
		case "video_played":
			e.VideosPlayed++
		case "video_completed":
			e.VideosCompleted++
		case "help_resolved":
			if v, ok := ev.Props["helped"].(bool); ok {
				e.TipVideoAnswers++
				if v {
					e.TipVideoResolved++
				}
			}
		case "assistant_helpful":
			if v, ok := ev.Props["helped"].(bool); ok {
				e.AssistantAnswers++
				if v {
					e.AssistantResolved++
				}
			}
		}
	}
	if e.TipsOpened+e.VideosPlayed+e.VideosCompleted+e.TipVideoAnswers+e.AssistantAnswers == 0 {
		e.NoData, e.Why = true, educationNoData
		return e
	}
	e.TipsPerPerson = ratio(e.TipsOpened, active)
	e.VideosPerPerson = ratio(e.VideosPlayed, active)
	e.VideoCompletionPct = pct(e.VideosCompleted, e.VideosPlayed)
	e.ResolvedByTipVideoPct = pct(e.TipVideoResolved, e.TipVideoResolved+e.AssistantResolved)
	e.NoFollowUp = topTips(plain)
	e.FollowedByQuestion = topTips(followed)
	return e
}

// askedWithin reports whether one of the ascending times is in [at, at+FollowUpWindow].
func askedWithin(times []time.Time, at time.Time) bool {
	i := sort.Search(len(times), func(i int) bool { return !times[i].Before(at) })
	return i < len(times) && !times[i].After(at.Add(FollowUpWindow))
}

func topTips(m map[string]int) []TipCount {
	out := make([]TipCount, 0, len(m))
	for k, n := range m {
		out = append(out, TipCount{Tip: k, Count: n})
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Count != out[j].Count {
			return out[i].Count > out[j].Count
		}
		return out[i].Tip < out[j].Tip
	})
	if len(out) > TopTips {
		out = out[:TopTips]
	}
	return out
}

func ratio(n, of int) *float64 {
	if of == 0 {
		return nil
	}
	v := float64(n) / float64(of)
	return &v
}
