import { render, screen, within } from "@testing-library/react";
import ActivationCard, { ACTIVATION_TITLE } from "@/components/ActivationCard";
import { activationView, minutesShort } from "@/lib/analytics/usageView";
import { usageReport } from "@/test/usageReport";

// #620: the activation card of the admin Analytics page, on mock report JSON.

const card = (report) => {
  render(<ActivationCard report={report} />);
  return screen.getByRole("heading", { name: ACTIVATION_TITLE }).closest("section");
};

const withLatest = (latest) => {
  const r = usageReport();
  r.activation = { ...r.activation, weeks: [...r.activation.weeks.slice(0, -1), { week: "2026-W44", by_step: {}, ...latest }] };
  return r;
};

describe("Scenario: A new person reaches the answers in the first session", () => {
  test("8 of 12 activated, the share and the median minutes", () => {
    const c = card(usageReport());
    expect(within(c).getByText("8 of 12")).toBeInTheDocument();
    expect(within(c).getAllByText("67 %").length).toBeGreaterThan(0);
    expect(within(c).getAllByText("9 min").length).toBeGreaterThan(0);
    expect(within(c).queryByText(/too few to judge/)).toBeNull();
  });
});

describe("Scenario: Too few new people", () => {
  test("6 new people: too few to judge, no share, no alert", () => {
    const c = card(withLatest({ users: 6, overview: 4, activated: 3, note: "too few to judge" }));
    expect(within(c).getByText("3 of 6")).toBeInTheDocument();
    expect(within(c).getByText("too few to judge")).toBeInTheDocument();
    expect(within(c).getByText(/under 10: no share is given and no alert is raised/)).toBeInTheDocument();
    const w44 = within(c).getByRole("rowheader", { name: "2026-W44" }).closest("tr");
    expect(within(w44).getByText("too few")).toBeInTheDocument();
    expect(within(w44).queryByText(/%/)).toBeNull();
  });
});

describe("Scenario: Admin page", () => {
  test("the last 12 weeks: weeks with events in the table, newest first; weeks without events noted, never 0", () => {
    const c = card(usageReport());
    const rows = within(c).getAllByRole("rowheader").map((th) => th.textContent);
    expect(rows).toEqual(["2026-W44", "2026-W43", "2026-W42", "2026-W41", "2026-W40", "2026-W39"]);
    expect(within(c).getByText("Weeks with no events, not counted as zero: 2026-W33, 2026-W34, 2026-W35, 2026-W36, 2026-W37, 2026-W38")).toBeInTheDocument();
    expect(within(c).queryByRole("rowheader", { name: "2026-W35" })).toBeNull();
  });

  test("the report's own week without events says so instead of a 0", () => {
    const c = card(withLatest({ users: 0, overview: 0, activated: 0, no_events: true, note: "too few to judge" }));
    expect(within(c).getByText("No events in 2026-W44: nothing to count this week.")).toBeInTheDocument();
    expect(within(c).queryByText("0 of 0")).toBeNull();
  });

  test("a report written before #620 says the card is not in it", () => {
    const r = usageReport();
    delete r.activation;
    expect(activationView(r)).toBeNull();
    const c = card(r);
    expect(within(c).getByText("Not in this report (written before #620).")).toBeInTheDocument();
  });
});

describe("Scenario: Consent respected", () => {
  test("the card shows counts and shares only: no id, name or amount reaches it", () => {
    const r = withLatest({ users: 12, overview: 10, activated: 8, pct: 66.67, median_minutes: 9, user_id: "a0a0a0a0a0a0a0a0", user: "alice" });
    const c = card(r);
    expect(c.textContent).not.toMatch(/a0a0a0a0a0a0a0a0|alice/);
  });
});

test("minutesShort: whole minutes under an hour, hours above, — when absent", () => {
  expect(minutesShort(9)).toBe("9 min");
  expect(minutesShort(90)).toBe("1.5 h");
  expect(minutesShort(null)).toBe("—");
});
