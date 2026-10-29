import { act, fireEvent, render, screen, within } from "@testing-library/react";
import DeliveryAnalytics, { DeliveryView, WILL_APPEAR } from "@/components/DeliveryAnalytics";
import AnalyticsTopics from "@/components/AnalyticsTopics";
import { fetchDeliveryReport } from "@/lib/api";
import { deliveryTiles, durationText, filterRows, headline, prRows, sortRows, stableText, utcText, weeklySummary } from "@/lib/analytics/deliveryView";
import { W43_SUMMARY, doraReport } from "@/test/doraReport";

// #473: the Analytics page's Delivery (DORA) topic, on mock dora.json (test/doraReport.js).

jest.mock("@/lib/api", () => ({ fetchDeliveryReport: jest.fn() }));

const answer = (report = doraReport()) => ({ report, file: report ? "dora.json" : null, written_at: report ? "2026-10-29T06:00:05Z" : null });
const view = (env = "stable", data = answer(), error = null) => render(<DeliveryView env={env} data={data} error={error} />);
const tile = (name) => screen.getByRole("region", { name });
const rowOf = (n) => screen.getByRole("button", { name: new RegExp(`#${n}$`) }).closest("tr");

describe("Scenario: dora.json with 4 weeks → this week's four tiles with bands and four weeks of bars", () => {
  test("stable: frequency, lead time, NO FAILURE; weeks before recording say n/r, never 0", () => {
    view("stable");
    expect(within(tile("Deployment frequency")).getByText("1.00 a week")).toBeInTheDocument();
    expect(within(tile("Deployment frequency")).getByText("HIGH")).toBeInTheDocument();
    expect(within(tile("Lead time")).getByText("3.2 d", { selector: "p" })).toBeInTheDocument();
    expect(within(tile("Change failure rate")).getByText("NO FAILURE")).toBeInTheDocument();
    expect(within(tile("Time to restore")).getByText("NO FAILURE")).toBeInTheDocument();
    const bars = within(tile("Deployment frequency")).getByRole("img");
    expect(bars).toHaveAccessibleName("Deployment frequency, last 4 weeks: 2026-W41 n/r, 2026-W42 n/r, 2026-W43 n/r, 2026-W44 1");
    const dimmed = bars.querySelectorAll(".opacity-40");
    expect(dimmed).toHaveLength(3);
    for (const d of dimmed) expect(d).toHaveTextContent("n/r");
    expect(within(tile("Deployment frequency")).getByText(/^What this counts: New releases a week/)).toBeInTheDocument();
  });

  test("mvp: ELITE frequency and lead time, the failure rate banded, a restore of 30 min with too few samples is NOT YET", () => {
    view("mvp");
    expect(within(tile("Deployment frequency")).getByText("3.00 a day")).toBeInTheDocument();
    expect(within(tile("Deployment frequency")).getByText("ELITE")).toBeInTheDocument();
    expect(within(tile("Lead time")).getByText("5 min", { selector: "p" })).toBeInTheDocument();
    expect(within(tile("Lead time")).getByRole("img")).toHaveAccessibleName("Lead time, last 4 weeks: 2026-W41 n/r, 2026-W42 6 min, 2026-W43 4 min, 2026-W44 5 min");
    expect(screen.getByRole("heading", { name: "mvp · test · week 2026-W44 (so far)" })).toBeInTheDocument();
  });

  test("the headline says how long a change takes to reach mvp and stable", () => {
    view("stable");
    expect(screen.getByText(headline(doraReport()))).toBeInTheDocument();
    expect(headline(doraReport())).toBe("Every change reaches mvp in minutes. Stable waits about 4 days for the 3-day soak and a release.");
    expect(headline(doraReport({ prs: [] }))).toBe(
      "No change has a recorded mvp deploy yet. Nothing has reached stable through a release yet: a release needs the 3-day soak on mvp.",
    );
  });
});

describe("Scenario: PR #458 merged 00:45, live on mvp 00:49", () => {
  test("its row shows 29 Oct 00:49 · +4 min and waiting · soak ends 1 Nov 00:49, LIVE ON MVP", () => {
    view();
    const row = rowOf(458);
    expect(row).toHaveTextContent("29 Oct 00:45");
    expect(within(row).getByText("29 Oct 00:49 · +4 min")).toBeInTheDocument();
    expect(within(row).getByText("waiting · soak ends 1 Nov 00:49")).toBeInTheDocument();
    expect(within(row).getByText("LIVE ON MVP")).toBeInTheDocument();
  });

  test("a soak already over waits for the next release; a PR before recording says so", () => {
    view();
    expect(within(rowOf(460)).getByText("waiting · soak over, waits for the next release")).toBeInTheDocument();
    expect(within(rowOf(400)).getByText("not recorded")).toBeInTheDocument();
    expect(within(rowOf(400)).getByText("ON STABLE")).toBeInTheDocument();
  });
});

describe("Scenario: Delivery opens on stable; PR #358 opened", () => {
  test("the switch reads stable · production | mvp · test, stable marked, links keep the topic", () => {
    view("stable");
    const envs = screen.getByRole("navigation", { name: "Environment" });
    expect(within(envs).getByRole("link", { name: "stable · production" })).toHaveAttribute("aria-current", "page");
    expect(within(envs).getByRole("link", { name: "mvp · test" })).toHaveAttribute("href", "/admin/analytics?topic=delivery&env=mvp");
    for (const l of within(envs).getAllByRole("link")) expect(l).toHaveClass("min-h-11");
  });

  test("its dots sit at 25 Oct 09:25 on mvp and 28 Oct 11:43 (v0.3.0) on stable, and the line says so", () => {
    const { container } = view();
    expect(screen.getByText("Open a PR in What went live to place it on the lanes.")).toBeInTheDocument();
    fireEvent.click(screen.getByRole("button", { name: /#358$/ }));
    expect(screen.getByRole("button", { name: /#358$/ })).toHaveAttribute("aria-expanded", "true");
    const mvp = container.querySelector('[data-dot="mvp"]');
    const stable = container.querySelector('[data-dot="stable"]');
    expect([mvp.dataset.at, mvp.dataset.filled]).toEqual(["2026-10-25T09:25:00Z", "true"]);
    expect([stable.dataset.at, stable.dataset.filled]).toEqual(["2026-10-28T11:43:00Z", "true"]);
    expect(screen.getByText("#358 went live on mvp 25 Oct 09:25 and on stable 28 Oct 11:43 (v0.3.0).")).toBeInTheDocument();
    expect(screen.getByRole("img", { name: /^stable · production: 1 release in the last 8 days; v0\.3\.0 28 Oct 11:43; #358 live 28 Oct 11:43$/ })).toBeInTheDocument();
  });

  test("a PR still waiting has a hollow dot at the right edge of stable", () => {
    const { container } = view();
    fireEvent.click(screen.getByRole("button", { name: /#458$/ }));
    const stable = container.querySelector('[data-dot="stable"]');
    expect([stable.dataset.filled, stable.style.left]).toEqual(["false", "100%"]);
    expect(screen.getByText("#458 went live on mvp 29 Oct 00:49; stable: waiting · soak ends 1 Nov 00:49.")).toBeInTheDocument();
    // mvp lane: a tick per deploy of the last 8 days (the 18 Oct one is older).
    expect(container.querySelectorAll('[data-mark="deploy"]')).toHaveLength(6);
  });

  test("an opened row shows where the time went and its links; opening it again closes it", () => {
    view();
    fireEvent.click(screen.getByRole("button", { name: /#358$/ }));
    expect(screen.getByRole("img", { name: "#358, where the time went: First commit → merge 12.0 h, Merge → live on mvp 6 min, mvp → live on stable 3.1 d" })).toBeInTheDocument();
    expect(screen.getByRole("link", { name: "PR #358 on GitHub" })).toHaveAttribute("href", "https://github.com/rchau73/optionsBot/pull/358");
    expect(screen.getByRole("link", { name: "Activity page: look from 25 Oct 08:25 to 26 Oct 09:25 UTC" })).toHaveAttribute(
      "href",
      "/activity?bot=btc&from=2026-10-25T08%3A25%3A00.000Z&to=2026-10-26T09%3A25%3A00.000Z",
    );
    expect(screen.getByText(/P&L chart with this PR marked: later/)).toBeInTheDocument();
    fireEvent.click(screen.getByRole("button", { name: /#358$/ }));
    expect(screen.queryByRole("link", { name: "PR #358 on GitHub" })).toBeNull();
  });
});

describe("Scenario: a rolled-back deploy", () => {
  test("its PR shows ROLLED BACK, the failure tile counts it once and time to restore shows the minutes", () => {
    view("mvp");
    expect(within(rowOf(470)).getByText("ROLLED BACK")).toBeInTheDocument();
    expect(within(tile("Change failure rate")).getByText("10 %", { selector: "p" })).toBeInTheDocument();
    expect(within(tile("Change failure rate")).getByText("1 of 10 deploys failed")).toBeInTheDocument();
    expect(within(tile("Time to restore")).getByText("30 min", { selector: "p" })).toBeInTheDocument();
    expect(within(tile("Time to restore")).getByText("1 restored, p90 30 min")).toBeInTheDocument();
    expect(within(tile("Time to restore")).getByText("NOT YET")).toBeInTheDocument();
  });

  test("a PR reverted later is ROLLED BACK too; a regression alone is not", () => {
    const r = doraReport();
    r.prs[1] = { ...r.prs[1], failures: ["revert/#456/mvp"] };
    r.prs[2] = { ...r.prs[2], failures: ["regression/#500/mvp"] };
    r.failures.push({ id: "revert/#456/mvp", kind: "revert", env: "mvp", at: "2026-10-24T12:00:00Z" });
    r.failures.push({ id: "regression/#500/mvp", kind: "regression", env: "mvp", at: "2026-10-26T12:00:00Z", detail: "#358 caused regression #500" });
    const rows = Object.fromEntries(prRows(r).map((x) => [x.number, x.result]));
    expect([rows[455], rows[358]]).toEqual(["ROLLED BACK", "ON STABLE"]);
  });
});

describe("Scenario: no dora.json, loading and error", () => {
  test("the empty state says what will appear and that #470 makes it; no figure is drawn", () => {
    view("stable", answer(null));
    expect(screen.getByRole("heading", { name: "No delivery report yet" })).toBeInTheDocument();
    expect(screen.getByText(/runs by itself once a day .*\(dora\.sh, #470\): nothing to\s+set up/)).toBeInTheDocument();
    for (const s of WILL_APPEAR) expect(screen.getByText(s)).toBeInTheDocument();
    expect(screen.queryByRole("img")).toBeNull();
    expect(screen.queryByRole("region", { name: "Deployment frequency" })).toBeNull();
    expect(screen.queryByRole("button", { name: "Weekly summary" })).toBeNull();
  });

  test("loading, then the route's error", () => {
    const { rerender } = view("stable", null);
    expect(screen.getByText("Loading the delivery report…")).toBeInTheDocument();
    rerender(<DeliveryView env="stable" data={null} error="403" />);
    expect(screen.getByRole("alert")).toHaveTextContent("The delivery report could not be loaded: 403");
  });

  test("the fetching component shows the report once the route answers", async () => {
    fetchDeliveryReport.mockResolvedValueOnce(answer());
    render(<DeliveryAnalytics env="stable" />);
    await act(async () => {});
    expect(screen.getByRole("region", { name: "Lead time" })).toBeInTheDocument();
  });

  test("a report with no week yet: the headline and the table, no tiles", () => {
    view("stable", answer(doraReport({ weeks: [] })));
    expect(screen.getByText("stable · production · no week in the report", { exact: false })).toBeInTheDocument();
    expect(screen.queryByRole("region", { name: "Lead time" })).toBeNull();
    expect(deliveryTiles(doraReport({ weeks: [] }), "stable")).toBeNull();
  });
});

describe("What went live: filters, search, sort", () => {
  const rows = prRows(doraReport());
  const numbers = (rs) => rs.map((r) => r.number);

  test("newest merge first by default; sortable by a column, a missing time last", () => {
    expect(numbers(sortRows(rows))).toEqual([458, 470, 460, 358, 455, 400]);
    // Equal times: the higher PR number first.
    expect(numbers(sortRows(rows, "stable", "asc"))).toEqual([455, 400, 358, 470, 460, 458]);
    expect(numbers(sortRows(rows, "number", "asc"))).toEqual([358, 400, 455, 458, 460, 470]);
  });

  test("All, On mvp only, On stable; search by PR, epic or date", () => {
    expect(numbers(filterRows(rows, "mvp"))).toEqual([460, 470, 458]);
    expect(numbers(filterRows(rows, "stable")).sort()).toEqual([358, 400, 455]);
    expect(numbers(filterRows(rows, "all", "#458"))).toEqual([458]);
    expect(numbers(filterRows(rows, "all", "e6")).sort()).toEqual([458, 470]);
    expect(numbers(filterRows(rows, "all", "2026-10-25")).sort()).toEqual([358]);
    expect(numbers(filterRows(rows, "all", "28 Oct")).sort()).toEqual([358, 400, 455]);
  });

  test("the table: filter buttons pressed, search, a header click sorts and says so", () => {
    view();
    const table = screen.getByRole("table");
    const firstPr = () => within(table).getAllByRole("button", { name: /#\d+$/ })[0].textContent;
    expect(firstPr()).toMatch(/#458$/);
    fireEvent.click(screen.getByRole("button", { name: "On stable" }));
    expect(screen.getByRole("button", { name: "On stable" })).toHaveAttribute("aria-pressed", "true");
    expect(screen.getByText("3 of 6 PRs")).toBeInTheDocument();
    fireEvent.click(screen.getByRole("button", { name: "All" }));
    fireEvent.change(screen.getByLabelText("Search by PR, epic or date"), { target: { value: "nothing like this" } });
    expect(screen.getByText("No PR matches this filter and search.")).toBeInTheDocument();
    fireEvent.change(screen.getByLabelText("Search by PR, epic or date"), { target: { value: "" } });
    fireEvent.click(within(table).getByRole("button", { name: /^PR/ }));
    expect(within(table).getByRole("columnheader", { name: /^PR/ })).toHaveAttribute("aria-sort", "descending");
    expect(firstPr()).toMatch(/#470$/);
    expect(table.querySelector("thead")).toHaveClass("sticky");
  });
});

describe("Weekly summary and Copy link", () => {
  test("Weekly summary shows the text dora.sh posts for the last week that ended", () => {
    view();
    const button = screen.getByRole("button", { name: "Weekly summary" });
    expect(button).toHaveAttribute("aria-expanded", "false");
    fireEvent.click(button);
    const panel = screen.getByRole("region", { name: "Weekly summary" });
    expect(panel).toHaveTextContent("week 2026-W43");
    expect(panel.querySelector("pre").textContent).toBe(W43_SUMMARY);
    expect(weeklySummary(doraReport({ weeks: doraReport().weeks.slice(-1) }))).toBeNull();
  });

  test("Copy link copies this view's address", async () => {
    const writeText = jest.fn().mockResolvedValue();
    Object.assign(navigator, { clipboard: { writeText } });
    view("mvp");
    await act(async () => fireEvent.click(screen.getByRole("button", { name: "Copy link" })));
    expect(writeText).toHaveBeenCalledWith(`${window.location.origin}/admin/analytics?topic=delivery&env=mvp`);
    expect(screen.getByText("Link copied.")).toBeInTheDocument();
    writeText.mockRejectedValueOnce(new Error("denied"));
    await act(async () => fireEvent.click(screen.getByRole("button", { name: "Copy link" })));
    expect(screen.getByText("Could not copy: use the address bar.")).toBeInTheDocument();
  });
});

describe("topic tabs and formats", () => {
  test("Usage · Delivery (DORA), the current one marked; Delivery's link has no env (stable)", () => {
    render(<AnalyticsTopics topic="delivery" />);
    const tabs = screen.getByRole("navigation", { name: "Topic" });
    expect(within(tabs).getAllByRole("link").map((l) => l.textContent)).toEqual(["Usage", "Delivery (DORA)"]);
    expect(within(tabs).getByRole("link", { name: "Delivery (DORA)" })).toHaveAttribute("aria-current", "page");
    expect(within(tabs).getByRole("link", { name: "Delivery (DORA)" })).toHaveAttribute("href", "/admin/analytics?topic=delivery");
    expect(within(tabs).getByRole("link", { name: "Usage" })).toHaveAttribute("href", "/admin/analytics");
  });

  test("UTC times and durations as cmd/dora prints them; missing values are —, never 0", () => {
    expect(utcText("2026-11-01T00:49:00Z")).toBe("1 Nov 00:49");
    expect(utcText(null)).toBe("—");
    expect([durationText(0.0667), durationText(5.24), durationText(77.4), durationText(null)]).toEqual(["4 min", "5.2 h", "3.2 d", "—"]);
    expect(stableText({ merged: "2026-10-29T00:45:00Z", stable_note: "not released yet" }, 0)).toBe("not released yet");
  });
});
