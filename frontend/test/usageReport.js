// A weekly usage report in the shape cmd/usage writes (internal/usage/report.go),
// for the Analytics page's tests (#445). Counts only, as the real file.

/** The report of `week` with `active` people; `over` replaces top-level fields. */
export function usageReport(week = "2026-W44", { active = 24, ...over } = {}) {
  return {
    week,
    from: "2026-10-26T00:00:00Z",
    to: "2026-11-02T00:00:00Z",
    source: { file: "/home/someone/env/data/monitor/events.jsonl", missing: false, lines: 420, accepted: 410, rejected: { property: 10 }, rejected_total: 10 },
    events_in_week: 180,
    active: {
      days: ["26", "27", "28", "29", "30", "31"].map((d, i) => ({ day: `2026-10-${d}`, users: 3 + i, by_role: {}, by_environment: {} })).concat([
        { day: "2026-11-01", users: 2, by_role: {}, by_environment: {} },
      ]),
      week: { users: active, by_role: { admin: 1, manager: 3, user: active - 4 }, by_environment: { stable: active } },
      month: { users: 31, by_role: {}, by_environment: {} },
      month_from: "2026-10-03T00:00:00Z",
    },
    cohorts: [
      { week: "2026-W40", from: "2026-09-28T00:00:00Z", users: 10, retained: [1, 2, 3, 4].map((o) => ({ offset: o, users: 8 - o, pct: (100 * (8 - o)) / 10 })) },
      { week: "2026-W44", from: "2026-10-26T00:00:00Z", users: 5, retained: [] },
    ],
    lapsed: { users: 6, by_role: { admin: 0, manager: 1, user: 5 }, weeks_away: { 1: 3, 2: 2, 3: 0, "4+": 1 }, base: 30, share_pct: 20 },
    panels: {
      opens: { account: 4, held: 0, others: 2, trades: 7 },
      never_opened: ["held"],
      drill: {},
      filters: {},
      screens: { overview: 60, income: 12, worst_case: 0, positions: 30, how_it_works: 3, activity: 9, account: 5, details: 8, login: 0, analytics: 1 },
    },
    first_answers: { first_sessions: 6, with_answers: 4, pct: 66.67, median_elapsed_ms: 150000 },
    help: { tips: { margin: 3 }, videos_played: { intro: 2 }, videos_completed: { intro: 1 }, answers: 4, helped: 3, helped_pct: 75 },
    assistant: { opened: { menu: 9, why: 2 }, asked: { menu: 7, why: 2 }, answers: { answered: 6, dont_know: 2, refused: 1, fallback: 0 }, replies: 9, answered_pct: 66.7, flagged: 2 },
    manual_close: { opened: 4, opened_allowed: 3, opened_not_allowed: 1, cancelled: 1, confirmed: 2, confirmed_last_leg: 0, results: { ok: 2 } },
    rebalance: { previewed: { ok: 3 }, confirmed: { ok: 1 } },
    trust: { stale: { bot: 2 }, signed_out: { expired: 1 } },
    survey: {
      surveys: [{ survey_id: "nps", shown: 8, answered: 7, dismissed: 1, response_pct: 87.5 }],
      nps: { answers: 7, promoters: 4, passives: 2, detractors: 1, too_few: true },
      why: { steady_income: 3, clear_risk: 1, hard_to_find: 2 },
      why_best: { steady_income: 3, clear_risk: 1 },
      why_improve: { hard_to_find: 2 },
      came_for: {},
      hardest_to_find: {},
      exit_reasons: {},
      top_exit: [],
      miscoded: 0,
    },
    drops: {
      week,
      rules: { CountPct: 30, FunnelPts: 20, MinBase: 10 },
      weeks: [],
      missing: [],
      drops: [{ metric: "active_users", kind: "count", current: 24, base: 40, change_pct: -40 }],
      skipped: [],
    },
    ...over,
  };
}
