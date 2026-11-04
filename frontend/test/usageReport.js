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
    // #607: followed 12 weeks; W32 has them all, W40 four, W44 none; W41 had no first session.
    cohorts: [
      {
        week: "2026-W32",
        from: "2026-08-03T00:00:00Z",
        users: 12,
        retained: [9, 8, 7, 6, 6, 6, 6, 6, 6, 6, 6, 6].map((n, i) => ({ offset: i + 1, users: n, pct: (100 * n) / 12 })),
        status: "flattening",
      },
      { week: "2026-W40", from: "2026-09-28T00:00:00Z", users: 10, retained: [1, 2, 3, 4].map((o) => ({ offset: o, users: 8 - o, pct: (100 * (8 - o)) / 10 })), status: "falling" },
      { week: "2026-W41", from: "2026-10-05T00:00:00Z", users: 0, retained: [1, 2, 3].map((o) => ({ offset: o, users: 0 })), status: "too few to judge" },
      { week: "2026-W44", from: "2026-10-26T00:00:00Z", users: 5, retained: [], status: "too few to judge" },
    ],
    cohort_rules: { min_users: 10, weeks: 3, step_pts: 5 },
    // #620: the last 12 weeks; W33–W38 had no events, W41 had events and no new person.
    activation: {
      window_hours: 24,
      min_users: 10,
      weeks: Array.from({ length: 12 }, (_, i) => {
        const week = `2026-W${String(33 + i).padStart(2, "0")}`;
        if (i < 6) return { week, users: 0, overview: 0, activated: 0, by_step: {}, no_events: true, note: "too few to judge" };
        const byWeek = {
          "2026-W39": { users: 11, overview: 9, activated: 5, pct: 45.45, median_minutes: 14 },
          "2026-W40": { users: 6, overview: 5, activated: 3, note: "too few to judge" },
          "2026-W41": { users: 0, overview: 0, activated: 0, note: "too few to judge" },
          "2026-W42": { users: 10, overview: 10, activated: 7, pct: 70, median_minutes: 6.5 },
          "2026-W43": { users: 13, overview: 11, activated: 9, pct: 69.23, median_minutes: 8 },
          "2026-W44": { users: 12, overview: 10, activated: 8, pct: 66.67, median_minutes: 9 },
        };
        return { week, by_step: { worst_case: 0, trade_reason: 0, help: 0 }, ...byWeek[week] };
      }),
    },
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
