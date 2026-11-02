import { render, screen, within } from "@testing-library/react";
import UsageAnalytics, { AnalyticsView, COUNTS_ONLY, SETUP_STEPS } from "@/components/UsageAnalytics";
import { fetchUsageReport } from "@/lib/api/admin";
import { attentionItems, dropText } from "@/lib/analytics/usageView";
import { usageReport } from "@/test/usageReport";

// #445: the admin Analytics page's states, on mock report JSON (no real events).

jest.mock("@/lib/api/admin",() => ({ fetchUsageReport: jest.fn() }));

const answer = (week = "2026-W44", over = {}) => ({
  off: false,
  env: "stable",
  envs: ["stable", "mvp"],
  current_week: "2026-W45",
  weeks: ["2026-W43", "2026-W44"],
  week,
  report: usageReport(week),
  file: `${week}.json`,
  written_at: "2026-11-02T06:00:00Z",
  consent: { yes: 24, no: 3, unasked: 5 },
  ...over,
});

const view = (data, error = null) => render(<AnalyticsView env="stable" data={data} error={error} />);

describe("Scenario: admin sees the latest week", () => {
  test("W44 with its attention items and eight cards; the counts-only line in the header", () => {
    view(answer());
    expect(screen.getByText(COUNTS_ONLY)).toBeInTheDocument();
    expect(screen.getByText("2026-W44", { selector: "span" })).toBeInTheDocument();
    const attention = screen.getByRole("heading", { name: "Needs your attention" }).closest("section");
    const items = within(attention).getAllByRole("listitem").map((li) => li.textContent);
    expect(items).toEqual([
      "DROPActive people: 24, mean of the weeks before 40.0 (−40 %)",
      "FLAG2 assistant answers flagged: see the weekly assistant review, data/monitor/assistant_reviews/2026-W44.md",
      "NEVERScreens never opened: worst_case",
      "NEVERPanels never opened: held",
    ]);
    for (const q of ["Q1", "Q2", "Q3", "Q4", "Q5", "Q6", "Q7", "Q8"]) {
      expect(screen.getByRole("heading", { name: new RegExp(`^${q} · `) })).toBeInTheDocument();
    }
    expect(screen.queryByText(/Small sample/)).toBeNull();
  });

  test("week arrows: older goes to W43, newer is disabled on the latest; the env switch marks stable", () => {
    view(answer());
    expect(screen.getByRole("link", { name: "Older week: 2026-W43" })).toHaveAttribute("href", "/admin/analytics?env=stable&week=2026-W43");
    expect(screen.getByLabelText("Newer week: none")).toHaveAttribute("aria-disabled", "true");
    const envs = screen.getByRole("navigation", { name: "Environment" });
    expect(within(envs).getByRole("link", { name: "stable" })).toHaveAttribute("aria-current", "page");
    expect(within(envs).getByRole("link", { name: "mvp" })).toHaveAttribute("href", "/admin/analytics?env=mvp");
    for (const l of [...within(envs).getAllByRole("link"), screen.getByRole("link", { name: /Older week/ })]) expect(l).toHaveClass("min-h-11");
  });

  test("Q8 counts what we do best (9–10) and what to improve first (0–8) apart; NPS not scored under 10 answers", () => {
    view(answer());
    const q8 = screen.getByRole("heading", { name: /^Q8 · / }).closest("section");
    expect(within(q8).getByText("Not scored: 7 of 10 answers")).toBeInTheDocument();
    const best = within(q8).getByRole("table", { name: "What we do best (9–10)" });
    const improve = within(q8).getByRole("table", { name: "What to improve first (0–8)" });
    expect(within(best).getByText("steady_income")).toBeInTheDocument();
    expect(within(best).queryByText("hard_to_find")).toBeNull();
    expect(within(improve).getByText("hard_to_find")).toBeInTheDocument();
  });

  test("Q1's day chart is an image with a text summary; Q2 shows — for weeks not ended", () => {
    view(answer());
    expect(screen.getByRole("img", { name: /^Active people per day: 2026-10-26 3, / })).toBeInTheDocument();
    const q2 = screen.getByRole("heading", { name: /^Q2 · / }).closest("section");
    const w44 = within(q2).getByRole("rowheader", { name: "2026-W44" }).closest("tr");
    expect(within(w44).getAllByText("—")).toHaveLength(4);
    expect(within(q2).getAllByRole("columnheader").map((c) => c.textContent)).toEqual(["First week", "People", "+1", "+2", "+3", "+4"]);
  });

  test("the data behind the page: file, time, events, consent counts and this week not reported yet", () => {
    view(answer());
    const data = screen.getByRole("heading", { name: "Data behind this page" }).closest("section");
    expect(data).toHaveTextContent("2026-W44.json");
    expect(data).toHaveTextContent("Events accepted: 410 (180 in the week); lines rejected: 10.");
    expect(data).toHaveTextContent("Analytics answers: 24 yes, 3 no, 5 not asked yet.");
    expect(data).toHaveTextContent("This week (2026-W45) isn't in a report yet");
  });

  test("nothing dropped → one OK line", () => {
    const r = usageReport("2026-W44", {
      drops: { drops: [] },
      assistant: { ...usageReport().assistant, flagged: 0 },
      panels: { ...usageReport().panels, never_opened: [], screens: { overview: 1, login: 0 } },
    });
    view(answer("2026-W44", { report: r }));
    const attention = screen.getByRole("heading", { name: "Needs your attention" }).closest("section");
    expect(within(attention).getByText(/Nothing dropped/)).toBeInTheDocument();
    expect(within(attention).getByText("OK")).toBeInTheDocument();
    expect(within(attention).queryByRole("listitem")).toBeNull();
  });
});

describe("Scenario: small sample", () => {
  test("7 active people → the small-sample note", () => {
    view(answer("2026-W44", { report: usageReport("2026-W44", { active: 7 }) }));
    expect(screen.getByText(/Small sample: 7 active people this week, under 10/)).toBeInTheDocument();
  });
});

describe("Scenario: analytics off", () => {
  test("the setup checklist and no figure", () => {
    view({ off: true, env: "stable", envs: ["stable", "mvp"], current_week: "2026-W45" });
    const steps = screen.getAllByRole("listitem").map((li) => li.textContent);
    expect(steps).toEqual(SETUP_STEPS);
    expect(screen.queryByRole("heading", { name: /^Q1/ })).toBeNull();
    expect(screen.queryByText("Needs your attention")).toBeNull();
    expect(screen.queryByRole("link", { name: /week/ })).toBeNull();
  });
});

describe("the other states", () => {
  test("no events in the week", () => {
    const r = { ...usageReport("2026-W44"), events_in_week: 0, no_events: "No events were recorded in the week." };
    view(answer("2026-W44", { report: r }));
    expect(screen.getByText(/No events in 2026-W44\. No events were recorded/)).toBeInTheDocument();
    expect(screen.queryByRole("heading", { name: /^Q1/ })).toBeNull();
  });

  test("report missing for that week", () => {
    view(answer("2026-W40", { report: null, file: null, missing: true }));
    expect(screen.getByText(/No report for 2026-W40/)).toBeInTheDocument();
    expect(screen.queryByRole("heading", { name: /^Q1/ })).toBeNull();
  });

  test("no report yet in the environment", () => {
    view(answer(null, { weeks: [], report: null, file: null }));
    expect(screen.getByText(/No usage report in stable yet/)).toBeInTheDocument();
  });

  test("loading, then error", () => {
    const { unmount } = view(null);
    expect(screen.getByText("Loading the usage report…")).toBeInTheDocument();
    unmount();
    view(null, "500");
    expect(screen.getByRole("alert")).toHaveTextContent("The usage report could not be loaded: 500");
  });

  test("UsageAnalytics asks the route for the env and week of the URL and shows the answer", async () => {
    fetchUsageReport.mockResolvedValue(answer());
    render(<UsageAnalytics env="stable" week="2026-W44" />);
    expect(screen.getByText("Loading the usage report…")).toBeInTheDocument();
    expect(await screen.findByRole("heading", { name: "Needs your attention" })).toBeInTheDocument();
    expect(fetchUsageReport).toHaveBeenCalledWith("stable", "2026-W44", expect.any(AbortSignal));
  });

  test("a failed route shows the error", async () => {
    fetchUsageReport.mockRejectedValue(new Error("the usage report could not be read"));
    render(<UsageAnalytics env="mvp" />);
    expect(await screen.findByRole("alert")).toHaveTextContent("could not be read");
  });
});

describe("Scenario: the admin page shows who stopped coming back (#569)", () => {
  const card = () => screen.getByRole("heading", { name: "Stopped coming back" }).closest("section");
  const lapsedDrop = { metric: "lapsed_share", kind: "share", current: 45, base: 15, change_pts: 30, starts: 40, base_starts: 120 };

  test("the counts by weeks away and by role, with an OK text tag while the share did not rise", () => {
    view(answer());
    const c = card();
    expect(c).toHaveTextContent("People who visited in the last 4 weeks and did not visit this week.");
    expect(within(c).getByText("6 of 30")).toBeInTheDocument();
    expect(within(c).getByText("20 %")).toBeInTheDocument();
    expect(within(c).getByText("OK")).toBeInTheDocument();
    const away = within(c).getByRole("table", { name: "By weeks since the last visit" });
    expect(within(away).getAllByRole("row").slice(1).map((r) => r.textContent)).toEqual(["1 week3", "2 weeks2", "3 weeks0", "4 weeks or more1"]);
    const roles = within(c).getByRole("table", { name: "By role at the last visit" });
    expect(within(roles).getAllByRole("row").slice(1).map((r) => r.textContent)).toEqual(["user5", "manager1"]);
    // In the cards' grid, after Q2.
    const headings = screen.getAllByRole("heading").map((h) => h.textContent);
    expect(headings.indexOf("Stopped coming back")).toBe(headings.findIndex((h) => h.startsWith("Q2 · ")) + 1);
  });

  test("a rising share: DROP in Needs your attention, and the card moves right under it", () => {
    const r = usageReport("2026-W44", { drops: { drops: [lapsedDrop] } });
    view(answer("2026-W44", { report: r }));
    const attention = screen.getByRole("heading", { name: "Needs your attention" }).closest("section");
    expect(within(attention).getAllByRole("listitem")[0]).toHaveTextContent(
      "DROPPeople who stopped coming back: 45 % of 40 people, was 15 % of 120 (+30 pts)",
    );
    expect(within(card()).getByText("DROP")).toBeInTheDocument();
    expect(screen.getAllByRole("heading", { name: "Stopped coming back" })).toHaveLength(1);
    const headings = screen.getAllByRole("heading").map((h) => h.textContent);
    expect(headings.indexOf("Stopped coming back")).toBe(headings.indexOf("Needs your attention") + 1);
  });

  test("a small base: the note, no share and no tag", () => {
    const r = usageReport("2026-W44", { lapsed: { users: 3, by_role: { user: 3 }, weeks_away: { 1: 3 }, base: 7, note: "too few to judge" } });
    view(answer("2026-W44", { report: r }));
    const c = card();
    expect(c).toHaveTextContent("Small sample: 7 people visited in those weeks, under 10. No share is given and no drop is raised.");
    expect(within(c).getByText("—")).toBeInTheDocument();
    expect(within(c).queryByText("OK")).toBeNull();
    expect(within(c).queryByText("DROP")).toBeNull();
  });

  test("no one lapsed: the empty line, no tables", () => {
    const r = usageReport("2026-W44", { lapsed: { users: 0, by_role: { user: 0 }, weeks_away: {}, base: 30, share_pct: 0 } });
    view(answer("2026-W44", { report: r }));
    expect(card()).toHaveTextContent("No one stopped coming back: everyone who visited in those weeks came back this week.");
    expect(within(card()).queryByRole("table")).toBeNull();
  });

  test("a report written before #569 says so; a week with no events shows the no-events message and no card", () => {
    const { unmount } = view(answer("2026-W44", { report: usageReport("2026-W44", { lapsed: undefined }) }));
    expect(card()).toHaveTextContent("Not in this report (written before #569).");
    unmount();
    const r = { ...usageReport("2026-W44"), lapsed: undefined, events_in_week: 0, no_events: "No events were recorded in the week." };
    view(answer("2026-W44", { report: r }));
    expect(screen.getByText(/No events in 2026-W44/)).toBeInTheDocument();
    expect(screen.queryByRole("heading", { name: "Stopped coming back" })).toBeNull();
  });
});

describe("Must not: no id, username, amount or free text", () => {
  test("the page shows none of the report's host path, and no id-like field exists to show", () => {
    const { container } = view(answer());
    expect(container.textContent).not.toMatch(/user_id|session_id|someone/);
  });

  test("funnel drops read in points", () => {
    expect(dropText({ metric: "manual_close", kind: "funnel", current: 50, base: 80, change_pts: -30, starts: 10, base_starts: 20 })).toBe(
      "Manual-close funnel: 50 % of 10 starts, was 80 % of 20 (−30 pts)",
    );
    expect(attentionItems({ events_in_week: 0 })).toEqual([]);
  });
});
