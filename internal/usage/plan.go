// Package usage turns the monitor's analytics events
// (data/monitor/events.jsonl, written by frontend/lib/analytics/collector.js)
// into the weekly usage and cohort report (#295): active users, cohort
// retention, panel use, the 5-minute test, help use, the state-changing
// funnels, trust signals and the survey tally — questions Q1–Q8 of
// docs/product/analytics/event-plan.md. Pure except Load; the report holds
// counts and shares only, never a user_id or session_id.
package usage

import (
	"regexp"
	"slices"
)

// Rule is one property's rule from the plan's allow-list (§4).
type Rule struct {
	Type   string   // enum | bool | duration | catalogue id
	Values []string // enum values
	CapMs  int64    // duration cap
	Prefix string   // catalogue id prefix (tip, video)

	pattern *regexp.Regexp // catalogue id: "<prefix>.<words>", lower-case letters and _ only
}

const hourMs = 3600 * 1000

func enumOf(v ...string) Rule   { return Rule{Type: "enum", Values: v} }
func duration(capMs int64) Rule { return Rule{Type: "duration", CapMs: capMs} }
func catalogue(p string) Rule {
	return Rule{Type: "catalogue id", Prefix: p, pattern: regexp.MustCompile(`^` + p + `\.[a-z]+(_[a-z]+)*$`)}
}

var boolRule = Rule{Type: "bool"}

// Properties is the plan's §4 allow-list, the same as the collector's
// frontend/lib/analytics/plan.js (tests/usage_test.go checks both against the
// plan's tables).
var Properties = map[string]Rule{
	"entry_screen": enumOf("monitor", "login"),
	"screen":       enumOf("monitor", "login"),
	"viewport":     enumOf("phone", "tablet", "desktop"),
	"panel":        enumOf("account", "held", "others", "trades"),
	"open_ms":      duration(hourMs),
	"filter":       enumOf("bot", "strategy", "type", "moneyness", "group_by"),
	"level":        enumOf("strategy", "slot", "structure", "leg", "orders"),
	"section":      enumOf("positions", "held", "trades", "orders", "others"),
	"tip_id":       catalogue("tip"),
	"video_id":     catalogue("video"),
	"watched_ms":   duration(10 * 60 * 1000),
	"helped":       boolRule,
	"elapsed_ms":   duration(hourMs),
	"side":         enumOf("call", "put"),
	"allowed":      boolRule,
	"last_leg":     boolRule,
	"outcome":      enumOf("ok", "refused", "error", "timeout"),
	"source":       enumOf("bot_api", "account", "pnl_history"),
	"reason":       enumOf("user", "expired", "refresh_failed"),
	// The in-product surveys (docs/product/analytics/surveys.md, #297).
	"survey_id":       enumOf("nps", "answers", "exit"),
	"score":           enumOf("0", "1", "2", "3", "4", "5", "6", "7", "8", "9", "10"),
	"reason_code":     enumOf("value_unclear", "losses", "too_complex", "price", "trust", "switched", "steady_income", "easy_to_follow", "saves_time", "other"),
	"came_for":        enumOf(fourAnswers...),
	"hardest_to_find": enumOf(fourAnswers...),
	// The assistant panel's opener (#408).
	"entry": enumOf("menu", "why"),
	// The overview's switches (#404).
	"unit":  enumOf("coin", "usd"),
	"layer": enumOf("sma", "sma21", "swings", "flip", "strikes"),
}

// E3's four investor answers, plus "none of these" (surveys.md §2).
var fourAnswers = []string{"holding", "income", "worst_case", "last_action", "none"}

// Events is the plan's §2: event → the properties it may carry.
var Events = map[string][]string{
	"session_started":        {"entry_screen", "viewport"},
	"screen_viewed":          {"screen"},
	"panel_expanded":         {"panel"},
	"panel_collapsed":        {"panel", "open_ms"},
	"filter_changed":         {"filter"},
	"drill_opened":           {"level"},
	"details_section_jump":   {"section"},
	"details_trade_expand":   {},
	"tip_opened":             {"tip_id", "screen"},
	"video_played":           {"video_id"},
	"video_completed":        {"video_id", "watched_ms"},
	"help_resolved":          {"tip_id", "video_id", "helped"},
	"first_answers_seen":     {"elapsed_ms"},
	"manual_close_opened":    {"side", "allowed"},
	"manual_close_cancelled": {"side"},
	"manual_close_confirmed": {"side", "last_leg"},
	"manual_close_result":    {"side", "outcome"},
	"rebalance_previewed":    {"outcome"},
	"rebalance_confirmed":    {"outcome"},
	"data_stale_shown":       {"source"},
	"signed_out":             {"reason"},
	"survey_shown":           {"survey_id"},
	"survey_answered":        {"survey_id", "score", "reason_code", "came_for", "hardest_to_find"},
	"survey_dismissed":       {"survey_id"},
	"assistant_open":         {"entry"},
	"assistant_ask":          {"entry"},
	"assistant_flag":         {},
	"overview_unit_toggle":   {"unit"},
	"overview_layer_toggle":  {"layer"},
}

// The envelope's own rules (§3 and collector.js).
var (
	roles        = []string{"admin", "manager", "user"}
	environments = []string{"stable", "mvp"}
	userIDRe     = regexp.MustCompile(`^[0-9a-f]{16}$`)
	sessionIDRe  = regexp.MustCompile(`^[0-9a-f]{32}$`)
	versionRe    = regexp.MustCompile(`^[A-Za-z0-9._-]{1,40}$`)
)

// valueOK reports whether a decoded JSON value meets its rule.
func valueOK(r Rule, v any) bool {
	switch r.Type {
	case "enum":
		s, ok := v.(string)
		return ok && slices.Contains(r.Values, s)
	case "bool":
		_, ok := v.(bool)
		return ok
	case "duration":
		f, ok := v.(float64)
		return ok && f >= 0 && f == float64(int64(f)) && int64(f) <= r.CapMs
	case "catalogue id":
		s, ok := v.(string)
		return ok && len(s) <= 64 && r.pattern.MatchString(s)
	}
	return false
}
