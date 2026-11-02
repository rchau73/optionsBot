import fs from "node:fs";
import path from "node:path";
import { act, fireEvent, render, screen } from "@testing-library/react";
import { usePathname } from "next/navigation";
import AnalyticsConsentSettings from "@/components/AnalyticsConsentSettings";
import ConsentNotice from "@/components/ConsentNotice";
import ScreenTracker from "@/components/ScreenTracker";
import { CONSENT_COPY, consentStatus, noticeDue } from "@/lib/analytics/consent";
import { validateEvent } from "@/lib/analytics/plan";
import { resetTracking, track } from "@/lib/track";

// #577: the one-time analytics notice and the Account › Privacy switch,
// against the real lib/track.js and a fake fetch of the monitor's own routes.

jest.mock("next/navigation", () => ({ usePathname: jest.fn(() => "/"), useSearchParams: jest.fn(() => new URLSearchParams()) }));

const NOTE_NO = "Usage analytics is off for you: nothing more is collected. Events already stored stay until you ask for their deletion.";
const json = (status, body) => ({ ok: status >= 200 && status < 300, status, headers: { get: () => null }, json: async () => body });

/** fetch: GET /api/consent → the stored answer; POST /api/consent → stores it, or `postStatus`; POST /api/events → 204. */
function fakeFetch({ consent = null, postStatus = 200 } = {}) {
  const db = { consent };
  global.fetch = jest.fn(async (url, init = {}) => {
    if (url === "/api/consent" && (init.method ?? "GET") === "GET") return json(200, { consent: db.consent });
    if (url === "/api/consent") {
      if (postStatus !== 200) return json(postStatus, { error: "database is locked" });
      db.consent = JSON.parse(init.body).consent;
      return json(200, { consent: db.consent, note: db.consent ? "on" : NOTE_NO });
    }
    if (url === "/api/events") return json(204, null);
    throw new Error(`unexpected ${url}`);
  });
  return db;
}
const sentEvents = () => global.fetch.mock.calls.filter(([u]) => u === "/api/events").map(([, i]) => JSON.parse(i.body));
const consentPosts = () => global.fetch.mock.calls.filter(([u, i]) => u === "/api/consent" && i?.method === "POST").map(([, i]) => JSON.parse(i.body));

const page = () => (
  <>
    <ScreenTracker />
    <ConsentNotice />
  </>
);
async function renderAt(pathname, ui = page()) {
  usePathname.mockReturnValue(pathname);
  const view = render(ui);
  await act(async () => {});
  return view;
}
async function moveTo(view, pathname, ui = page()) {
  usePathname.mockReturnValue(pathname);
  view.rerender(ui);
  await act(async () => {});
}
const notice = () => screen.queryByRole("region", { name: CONSENT_COPY.en.title });
async function click(name) {
  fireEvent.click(screen.getByRole("button", { name }));
  await act(async () => {});
}

const realFetch = global.fetch;
beforeEach(() => {
  resetTracking();
  fakeFetch();
});
afterEach(() => {
  global.fetch = realFetch;
});

describe("Scenario: first sign-in asks once", () => {
  test("the Overview shows the notice with equal Yes and No, focused, and no event was sent", async () => {
    await renderAt("/");
    const card = notice();
    expect(card).toBeInTheDocument();
    expect(card).toHaveFocus();
    const yes = screen.getByRole("button", { name: "Yes" });
    const no = screen.getByRole("button", { name: "No" });
    expect(yes.className).toBe(no.className);
    for (const b of [yes, no]) {
      expect(b).not.toHaveAttribute("aria-pressed");
      expect(b.className).toContain("min-h-11");
    }
    expect(screen.queryByRole("dialog")).not.toBeInTheDocument();
    expect(screen.queryByRole("button", { name: /close/i })).not.toBeInTheDocument();
    expect(screen.getByRole("link", { name: "Change it in Account" })).toHaveAttribute("href", "/account");
    expect(card).toHaveTextContent("never amounts or positions");
    expect(sentEvents()).toEqual([]);
  });

  test.each(["/login", "/reset", "/reset?token=x"])("not shown on %s", async (pathname) => {
    await renderAt(pathname.split("?")[0]);
    expect(notice()).not.toBeInTheDocument();
  });

  test.each([
    ["already said yes", { consent: true }],
    ["already said no", { consent: false }],
  ])("%s: no notice", async (_, opts) => {
    fakeFetch(opts);
    await renderAt("/");
    expect(notice()).not.toBeInTheDocument();
  });

  test("an unreadable answer shows no notice and sends nothing", async () => {
    global.fetch = jest.fn(async (url) => (url === "/api/consent" ? json(500, { error: "x" }) : json(204, null)));
    await renderAt("/");
    expect(notice()).not.toBeInTheDocument();
    expect(global.fetch.mock.calls.some(([u]) => u === "/api/events")).toBe(false);
  });
});

describe("Scenario: yes starts tracking", () => {
  test("stores true, closes the notice for good and the next page sends screen_viewed", async () => {
    const db = fakeFetch();
    const view = await renderAt("/");
    await click("Yes");
    expect(consentPosts()).toEqual([{ consent: true }]);
    expect(db.consent).toBe(true);
    expect(notice()).not.toBeInTheDocument();
    const answered = sentEvents();
    expect(answered.map((e) => [e.event, e.props])).toEqual([
      ["consent_notice_shown", {}],
      ["consent_answered", { consent: "yes" }],
    ]);
    for (const e of answered) expect(validateEvent(e).ok).toBe(true);

    await moveTo(view, "/income");
    expect(sentEvents().filter((e) => e.event === "screen_viewed").map((e) => e.props)).toEqual([{ screen: "income" }]);

    view.unmount(); // a reload
    resetTracking();
    await renderAt("/");
    expect(notice()).not.toBeInTheDocument();
  });
});

describe("Scenario: no sends nothing", () => {
  test("stores false, sends no event and the notice does not return", async () => {
    const db = fakeFetch();
    const view = await renderAt("/");
    await click("No");
    expect(consentPosts()).toEqual([{ consent: false }]);
    expect(db.consent).toBe(false);
    expect(notice()).not.toBeInTheDocument();
    await moveTo(view, "/income");
    expect(track("screen_viewed", { screen: "income" })).toBe(false);
    expect(sentEvents()).toEqual([]);

    view.unmount();
    await renderAt("/");
    expect(notice()).not.toBeInTheDocument();
  });
});

describe("Scenario: save fails", () => {
  test.each([
    ["Yes", true],
    ["No", false],
  ])("%s that is not saved keeps the notice with Not saved and a retry, and sends nothing", async (name, value) => {
    fakeFetch({ postStatus: 500 });
    const view = await renderAt("/");
    await click(name);
    expect(notice()).toBeInTheDocument();
    expect(screen.getByRole("alert")).toHaveTextContent("Not saved");
    await moveTo(view, "/income");
    expect(sentEvents()).toEqual([]);

    fakeFetch(); // the server is back
    await click("Try again");
    expect(consentPosts()).toEqual([{ consent: value }]);
    expect(notice()).not.toBeInTheDocument();
  });
});

describe("Scenario: change of mind in Account", () => {
  test("a person who said yes switches it off: stored false, tracking stops, the note names the deletion path", async () => {
    const db = fakeFetch({ consent: true });
    await renderAt("/account", <AnalyticsConsentSettings />);
    const toggle = screen.getByRole("switch", { name: CONSENT_COPY.en.rowLabel });
    expect(toggle).toHaveAttribute("aria-checked", "true");
    expect(screen.getByText("Yes")).toBeInTheDocument();

    await act(async () => fireEvent.click(toggle));
    expect(db.consent).toBe(false);
    expect(toggle).toHaveAttribute("aria-checked", "false");
    expect(screen.getByText("No")).toBeInTheDocument();
    expect(track("screen_viewed", { screen: "account" })).toBe(false);
    expect(sentEvents()).toEqual([]);
    expect(screen.getByRole("status")).toHaveTextContent("Events already stored stay until you ask for their deletion.");
    expect(screen.getByRole("status")).toHaveTextContent("Delete my account");
  });

  test("not asked yet: switching it on stores true and starts tracking", async () => {
    const db = fakeFetch({ consent: null });
    await renderAt("/account", <AnalyticsConsentSettings />);
    expect(screen.getByText("Not asked yet")).toBeInTheDocument();
    await act(async () => fireEvent.click(screen.getByRole("switch")));
    expect(db.consent).toBe(true);
    expect(sentEvents().map((e) => [e.event, e.props])).toEqual([["consent_answered", { consent: "yes" }]]);
  });

  test("a failed change says Not saved and leaves the switch as it was", async () => {
    fakeFetch({ consent: false, postStatus: 500 });
    await renderAt("/account", <AnalyticsConsentSettings />);
    await act(async () => fireEvent.click(screen.getByRole("switch")));
    expect(screen.getByRole("alert")).toHaveTextContent("Not saved: database is locked");
    expect(screen.getByRole("switch")).toHaveAttribute("aria-checked", "false");
    expect(sentEvents()).toEqual([]);
  });

  test("an unreadable answer disables the switch and says unknown", async () => {
    global.fetch = jest.fn(async () => json(500, { error: "x" }));
    await renderAt("/account", <AnalyticsConsentSettings />);
    expect(screen.getByRole("switch")).toBeDisabled();
    expect(screen.getByText("unknown")).toBeInTheDocument();
  });
});

describe("the consent words (lib/analytics/consent.js)", () => {
  const SURVEYS = fs.readFileSync(path.join(__dirname, "..", "..", "docs", "product", "analytics", "surveys.md"), "utf8");

  test("EN and PT-BR carry the same texts", () => {
    const keys = (o) => Object.keys(o).sort();
    expect(keys(CONSENT_COPY.pt)).toEqual(keys(CONSENT_COPY.en));
    expect(keys(CONSENT_COPY.pt.status)).toEqual(keys(CONSENT_COPY.en.status));
    expect(CONSENT_COPY.pt.draft).toHaveLength(CONSENT_COPY.en.draft.length);
  });

  test("the draft text is surveys.md §6's, word for word", () => {
    expect(SURVEYS).toContain(`- EN: "${CONSENT_COPY.en.draft.join(" ")}"`);
    expect(SURVEYS).toContain(`- PT-BR: "${CONSENT_COPY.pt.draft.join(" ")}"`);
  });

  test.each([
    [null, "/", true],
    [null, "/account", true],
    [null, "/login", false],
    [null, "/reset", false],
    [true, "/", false],
    [false, "/", false],
    [undefined, "/", false],
  ])("noticeDue(%s, %s) is %s", (consent, pathname, want) => {
    expect(noticeDue(consent, pathname)).toBe(want);
  });

  test.each([
    [true, "Yes"],
    [false, "No"],
    [null, "Not asked yet"],
    [undefined, "unknown"],
  ])("status of %s is %s", (consent, want) => {
    expect(consentStatus(consent)).toBe(want);
  });
});
