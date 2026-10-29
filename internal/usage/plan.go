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
// Properties and Events are kept in alphabetical order (#465), so two PRs that
// each add one land on different lines (tests/sorted_lists_test.go).
var Properties = map[string]Rule{
	"allowed": boolRule,
	// The kind of the assistant's reply, never its text (#424).
	"answer":     enumOf(AssistantAnswers...),
	"came_for":   enumOf(fourAnswers...),
	"elapsed_ms": duration(hourMs),
	// The assistant panel's opener (#408).
	"entry":           enumOf("menu", "why"),
	"entry_screen":    enumOf("monitor", "login"),
	"filter":          enumOf("bot", "strategy", "type", "moneyness", "group_by"),
	"hardest_to_find": enumOf(fourAnswers...),
	"helped":          boolRule,
	// The kind of the bell's item followed (#447), never its text.
	"kind":     enumOf("survey", "request_needs_info", "bot_event", "request_status", "whats_new"),
	"last_leg": boolRule,
	// The overview's switches (#404): layer and unit.
	"layer":       enumOf("sma", "sma21", "swings", "flip", "strikes"),
	"level":       enumOf("strategy", "slot", "structure", "leg", "orders"),
	"open_ms":     duration(hourMs),
	"outcome":     enumOf("ok", "refused", "error", "timeout"),
	"panel":       enumOf("account", "held", "others", "trades"),
	"reason":      enumOf("user", "expired", "refresh_failed"),
	"reason_code": enumOf("value_unclear", "losses", "too_complex", "price", "trust", "switched", "steady_income", "easy_to_follow", "saves_time", "clear_risk", "explains_trades", "trust_numbers", "helpful_answers", "hard_to_find", "missing_feature", "other"),
	"score":       enumOf("0", "1", "2", "3", "4", "5", "6", "7", "8", "9", "10"),
	"screen":      enumOf(Screens...),
	"section":     enumOf("positions", "held", "trades", "orders", "others"),
	"side":        enumOf("call", "put"),
	"source":      enumOf("bot_api", "account", "pnl_history"),
	// The in-product surveys (docs/product/analytics/surveys.md, #297; also
	// score, reason_code, came_for, hardest_to_find).
	"survey_id": enumOf("nps", "answers", "exit"),
	"tip_id":    catalogue("tip"),
	// The admin Analytics page's topic (#473).
	"topic":      enumOf("usage", "delivery"),
	"unit":       enumOf("coin", "usd"),
	"video_id":   catalogue("video"),
	"viewport":   enumOf("phone", "tablet", "desktop"),
	"watched_ms": duration(10 * 60 * 1000),
}

// Screens is one value per page of the monitor's menu (frontend/lib/menu.js)
// in menu order, plus the sign-in page (#424) and the admin Analytics page
// (#445: whether the admin opens it on Mondays).
var Screens = []string{"overview", "income", "worst_case", "positions", "how_it_works", "activity", "account", "details", "login", "analytics"}

// AssistantAnswers is the kind of an assistant reply (#424): a cited answer,
// "I don't know", the refusal, the safe fallback.
var AssistantAnswers = []string{"answered", "dont_know", "refused", "fallback"}

// E3's four investor answers, plus "none of these" (surveys.md §2).
var fourAnswers = []string{"holding", "income", "worst_case", "last_action", "none"}

// Events is the plan's §2: event → the properties it may carry.
var Events = map[string][]string{
	"assistant_answered":     {"answer"},
	"assistant_ask":          {"entry"},
	"assistant_flag":         {},
	"assistant_open":         {"entry"},
	"data_stale_shown":       {"source"},
	"details_section_jump":   {"section"},
	"details_trade_expand":   {},
	"drill_opened":           {"level"},
	"filter_changed":         {"filter"},
	"first_answers_seen":     {"elapsed_ms"},
	"help_resolved":          {"tip_id", "video_id", "helped"},
	"manual_close_cancelled": {"side"},
	"manual_close_confirmed": {"side", "last_leg"},
	"manual_close_opened":    {"side", "allowed"},
	"manual_close_result":    {"side", "outcome"},
	"notification_followed":  {"kind"},
	"notifications_opened":   {},
	"overview_layer_toggle":  {"layer"},
	"overview_unit_toggle":   {"unit"},
	"panel_collapsed":        {"panel", "open_ms"},
	"panel_expanded":         {"panel"},
	"rebalance_confirmed":    {"outcome"},
	"rebalance_previewed":    {"outcome"},
	"screen_viewed":          {"screen", "topic"},
	"session_started":        {"entry_screen", "viewport"},
	"signed_out":             {"reason"},
	"survey_answered":        {"survey_id", "score", "reason_code", "came_for", "hardest_to_find"},
	"survey_dismissed":       {"survey_id"},
	"survey_shown":           {"survey_id"},
	"tip_opened":             {"tip_id", "screen"},
	"video_completed":        {"video_id", "watched_ms"},
	"video_played":           {"video_id"},
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
