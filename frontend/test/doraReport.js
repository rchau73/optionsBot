// A delivery report in the shape cmd/dora writes (internal/delivery,
// dora.json), for the Analytics page's Delivery (DORA) tests (#473). Written
// 2026-10-29 06:00 UTC: mvp recorded from 17 Oct, stable from its first
// release v0.3.0 (28 Oct 11:43). PRs: #458 (live on mvp 4 min after its
// merge, waiting for its soak), #358 (mvp 25 Oct 09:25, stable v0.3.0),
// #470 (its deploy rolled back, then live), #460 (soak over, no release yet),
// #455 (on stable), #400 (merged before recording).

const stat = (median, p90, n, band = "", note = "") => ({
  n,
  median_hours: median,
  p90_hours: p90,
  ...(band ? { band } : {}),
  ...(note ? { note } : {}),
});
const none = (note) => ({ n: 0, median_hours: null, p90_hours: null, note });

const notRecorded = (what) => ({
  recorded: false,
  note: `${what} not recorded`,
  deploys: 0,
  merge_to_live: none(`${what} not recorded`),
  commit_to_live: none(`${what} not recorded`),
  attempts: 0,
  failed: 0,
  failure_rate_pct: null,
  restore: none(`${what} not recorded`),
  unrestored: 0,
});

const mvpWeek = (deploys, perDay, lead, failed, attempts, restore = none("no failure")) => ({
  recorded: true,
  deploys,
  per_day: perDay,
  frequency_band: perDay >= 1 ? "elite" : "high",
  merge_to_live: lead,
  commit_to_live: lead,
  attempts,
  failed,
  failure_rate_pct: attempts ? (failed / attempts) * 100 : null,
  ...(attempts >= 3 ? { failure_band: failed / attempts <= 0.05 ? "elite" : failed / attempts <= 0.1 ? "high" : "low" } : {}),
  ...(attempts ? {} : { failure_note: "no deploys" }),
  restore,
  unrestored: 0,
});

const week = (name, from, to, mvp, stable, summary = "") => ({
  week: name,
  from,
  to,
  merged: 0,
  commit_to_merge: none("no sample"),
  mvp,
  stable,
  summary,
});

export const W43_SUMMARY =
  "**Delivery (DORA), week 2026-W43** (2026-10-19 – 2026-10-25 UTC)\n\n| | mvp | stable |\n|---|---|---|\n| Deployment frequency | 14 deploys (2.00 a day) · elite | release not recorded |\n";

/** The report; `over` replaces top-level fields. */
export function doraReport(over = {}) {
  return {
    generated: "2026-10-29T06:00:00Z",
    from: "2026-09-28T00:00:00Z",
    mvp_recorded_from: "2026-10-17T08:00:00Z",
    stable_recorded_from: "2026-10-28T11:43:00Z",
    weeks: [
      week("2026-W40", "2026-09-28T00:00:00Z", "2026-10-05T00:00:00Z", notRecorded("deploy"), notRecorded("release")),
      week("2026-W41", "2026-10-05T00:00:00Z", "2026-10-12T00:00:00Z", notRecorded("deploy"), notRecorded("release")),
      week("2026-W42", "2026-10-12T00:00:00Z", "2026-10-19T00:00:00Z", mvpWeek(2, 0.29, stat(0.1, 0.1, 2, "", "too few samples (2, need 3)"), 0, 2), notRecorded("release")),
      week(
        "2026-W43",
        "2026-10-19T00:00:00Z",
        "2026-10-26T00:00:00Z",
        mvpWeek(14, 2, stat(0.07, 0.2, 14, "elite"), 0, 14),
        notRecorded("release"),
        W43_SUMMARY,
      ),
      week(
        "2026-W44",
        "2026-10-26T00:00:00Z",
        "2026-11-02T00:00:00Z",
        mvpWeek(9, 3, stat(0.08, 0.67, 9, "elite"), 1, 10, stat(0.5, 0.5, 1, "", "too few samples (1, need 3)")),
        {
          recorded: true,
          note: "recorded from 2026-10-28 11:43 UTC",
          deploys: 1,
          per_week: 1,
          frequency_band: "high",
          merge_to_live: stat(77.4, 98, 3, "high"),
          commit_to_live: stat(90, 110, 3, "high"),
          attempts: 1,
          failed: 0,
          failure_rate_pct: 0,
          failure_note: "too few samples (1, need 3)",
          restore: none("no failure"),
          unrestored: 0,
        },
        "**Delivery (DORA), week 2026-W44** (in progress)\n",
      ),
    ],
    prs: [
      { number: 400, title: "Before recording", epic: "E1", branch: "agent/issue-390", commit: "4000000", first_commit: null, merged: "2026-10-10T12:00:00Z", mvp_live: null, mvp_note: "not recorded", stable_live: "2026-10-28T11:43:00Z", release: "v0.3.0" },
      { number: 455, title: "Scorecard weekly", epic: "E2", branch: "agent/issue-440", commit: "4550000", first_commit: "2026-10-23T20:00:00Z", merged: "2026-10-24T10:00:00Z", mvp_live: "2026-10-24T10:03:00Z", mvp_commit: "4550000", stable_live: "2026-10-28T11:43:00Z", release: "v0.3.0" },
      { number: 358, title: "Overview answers first", epic: "E3", branch: "agent/issue-290", commit: "3580000", first_commit: "2026-10-24T21:19:00Z", merged: "2026-10-25T09:19:19Z", mvp_live: "2026-10-25T09:25:00Z", mvp_commit: "3580000", stable_live: "2026-10-28T11:43:00Z", release: "v0.3.0" },
      { number: 460, title: "Split CLAUDE.md", epic: "", branch: "agent/issue-460", commit: "4600000", first_commit: "2026-10-25T22:00:00Z", merged: "2026-10-26T01:00:00Z", mvp_live: "2026-10-26T01:05:00Z", mvp_commit: "4600000", stable_live: null, stable_note: "not released yet" },
      { number: 470, title: "DORA metrics", epic: "E6", branch: "agent/issue-470", commit: "abc1234def", first_commit: "2026-10-26T20:00:00Z", merged: "2026-10-27T10:00:00Z", mvp_live: "2026-10-27T10:40:00Z", mvp_commit: "abc5678", stable_live: null, stable_note: "not released yet" },
      { number: 458, title: "Weekly usage report", epic: "E6", branch: "agent/issue-446", commit: "4580000", first_commit: "2026-10-28T20:00:00Z", merged: "2026-10-29T00:45:00Z", mvp_live: "2026-10-29T00:49:00Z", mvp_commit: "4580000", stable_live: null, stable_note: "not released yet" },
    ],
    releases: [{ tag: "v0.3.0", commit: "3580000", at: "2026-10-28T11:43:00Z", failed: false }],
    mvp_deploys: [
      { at: "2026-10-18T09:00:00Z", commit: "1000000" },
      { at: "2026-10-24T10:03:00Z", commit: "4550000" },
      { at: "2026-10-25T09:25:00Z", commit: "3580000" },
      { at: "2026-10-26T01:05:00Z", commit: "4600000" },
      { at: "2026-10-27T10:35:00Z", commit: "4600000" },
      { at: "2026-10-27T10:40:00Z", commit: "abc5678" },
      { at: "2026-10-29T00:49:00Z", commit: "4580000" },
    ],
    failures: [
      {
        id: "rollback/mvp/2026-10-27T10:05:00Z",
        kind: "rollback",
        env: "mvp",
        at: "2026-10-27T10:05:00Z",
        restored_at: "2026-10-27T10:35:00Z",
        restore_hours: 0.5,
        issue: 471,
        commit: "abc1234",
        detail: "deploy of abc1234 failed (#471), mvp rolled back to 4600000",
      },
    ],
    problems: [],
    ...over,
  };
}
