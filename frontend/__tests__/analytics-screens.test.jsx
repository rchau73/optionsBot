import { act, fireEvent, render, screen, within } from "@testing-library/react";
import { usePathname } from "next/navigation";
import AssistantProvider from "@/components/AssistantProvider";
import ScreenTracker from "@/components/ScreenTracker";
import { MenuNav } from "@/components/MenuBar";
import { validateEvent } from "@/lib/analytics/plan";
import { answerOf, pagesWithoutScreen, screenOf } from "@/lib/analytics/screens";
import { menuItems } from "@/lib/menu";
import { resetTracking } from "@/lib/track";

// Every monitor page and the assistant panel are counted, after consent only
// (#424, docs/product/analytics/event-plan.md §2 and §4). The real lib/track.js
// against a fake fetch of the monitor's own routes.

jest.mock("next/navigation", () => ({ usePathname: jest.fn(), useSearchParams: jest.fn(() => new URLSearchParams()) }));

const json = (status, body) => ({ ok: status >= 200 && status < 300, status, headers: { get: () => null }, json: async () => body });

const QUESTION = "When does the bot buy a leg back?";

/** fetch: GET /api/consent → consent; /api/assistant → state or the reply; POST /api/events → 204. */
function fakeFetch({ consent = true, outcome = "answered" } = {}) {
  global.fetch = jest.fn(async (url, init = {}) => {
    if (url === "/api/consent") return json(200, { consent });
    if (url === "/api/events") return json(204, null);
    if (url === "/api/assistant" && (init.method ?? "GET") === "GET") return json(200, { enabled: true, remaining: 30 });
    if (url === "/api/assistant") {
      const answer = outcome === "dont_know" ? "I don't know." : "A leg is bought back when its stop-loss fires.";
      return json(200, { id: "answer-id-77", answer, citations: [], outcome, ai_generated: true, remaining: 29 });
    }
    throw new Error(`unexpected ${url}`);
  });
}
const sentEvents = () => global.fetch.mock.calls.filter(([u]) => u === "/api/events").map(([, i]) => JSON.parse(i.body));

async function renderAt(pathname, ui = null) {
  usePathname.mockReturnValue(pathname);
  const view = render(
    <AssistantProvider>
      <ScreenTracker />
      {ui}
    </AssistantProvider>,
  );
  await act(async () => {});
  return view;
}

async function ask() {
  await renderAt("/", <MenuNav pathname="/" bot="btc" />);
  fireEvent.click(screen.getByRole("button", { name: "Ask" }));
  const panel = screen.getByRole("complementary", { name: "Assistant" });
  fireEvent.change(within(panel).getByLabelText("Your question"), { target: { value: QUESTION } });
  fireEvent.keyDown(within(panel).getByLabelText("Your question"), { key: "Enter" });
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

describe("Scenario: a consented person opens the Income page", () => {
  test("one screen_viewed with screen=income, accepted by the collector's allow-list", async () => {
    await renderAt("/income");
    const views = sentEvents().filter((e) => e.event === "screen_viewed");
    expect(views.map((e) => e.props)).toEqual([{ screen: "income" }]);
    expect(validateEvent(views[0]).ok).toBe(true);
  });

  test("moving to another page sends that page's view", async () => {
    const view = await renderAt("/income");
    usePathname.mockReturnValue("/worst-case");
    view.rerender(
      <AssistantProvider>
        <ScreenTracker />
      </AssistantProvider>,
    );
    await act(async () => {});
    expect(sentEvents().map((e) => e.props.screen)).toEqual(["income", "worst_case"]);
  });

  test("a page outside the menu (admin) sends no view", async () => {
    await renderAt("/admin/users");
    expect(sentEvents()).toEqual([]);
  });
});

describe("Scenario: a person who has not consented", () => {
  test.each([
    ["said no", false],
    ["never asked", null],
  ])("%s: opening a page or asking the assistant sends no event", async (_, consent) => {
    fakeFetch({ consent });
    await ask();
    expect(await screen.findByText("A leg is bought back when its stop-loss fires.")).toBeInTheDocument();
    expect(global.fetch.mock.calls.some(([u]) => u === "/api/events")).toBe(false);
  });

  test("a failed consent read sends nothing either", async () => {
    global.fetch = jest.fn(async (url) => (url === "/api/consent" ? json(500, { error: "x" }) : json(204, null)));
    await renderAt("/income");
    expect(global.fetch.mock.calls.some(([u]) => u === "/api/events")).toBe(false);
  });
});

describe("Scenario: the assistant answered \"I don't know\"", () => {
  test("assistant_answered carries answer=dont_know and no question or answer text", async () => {
    fakeFetch({ outcome: "dont_know" });
    await ask();
    const events = sentEvents();
    expect(events.map((e) => e.event)).toEqual(["screen_viewed", "assistant_open", "assistant_ask", "assistant_answered"]);
    expect(events[3].props).toEqual({ answer: "dont_know" });
    for (const e of events) expect(validateEvent(e).ok).toBe(true);
    const raw = JSON.stringify(events);
    expect(raw).not.toContain(QUESTION);
    expect(raw).not.toContain("I don't know");
    expect(raw).not.toContain("answer-id-77");
  });

  test.each([
    ["answered", "answered"],
    ["dont_know", "dont_know"],
    ["no_citation", "dont_know"],
    ["refused", "refused"],
    ["rejected", "fallback"],
    ["needs_info", "answered"],
    ["draft", "answered"],
    ["model_error", null],
    [undefined, null],
    ["toString", null],
  ])("the route's outcome %s is the reply kind %s", (outcome, kind) => {
    expect(answerOf(outcome)).toBe(kind);
  });
});

describe("Scenario: a page in menu.js with no screen value in the event plan", () => {
  test("every page of the menu has its screen in the plan", () => {
    expect(pagesWithoutScreen()).toEqual([]);
  });

  test("a page added without a screen value is named", () => {
    const items = [...menuItems("/"), { key: "tax-report", label: "Tax report", href: "/tax-report", current: false }];
    expect(pagesWithoutScreen(items)).toEqual(["Tax report"]);
  });

  test.each([
    ["/", "overview"],
    ["/income", "income"],
    ["/worst-case", "worst_case"],
    ["/positions", "positions"],
    ["/how-it-works", "how_it_works"],
    ["/activity", "activity"],
    ["/account", "account"],
    ["/details", "details"],
    ["/login", null],
    ["/admin/users", null],
  ])("%s is the screen %s", (pathname, want) => {
    expect(screenOf(pathname)).toBe(want);
  });
});
