import { act, fireEvent, render, screen, waitFor, within } from "@testing-library/react";
import AssistantProvider from "@/components/AssistantProvider";
import { MenuNav } from "@/components/MenuBar";
import TradeHistory from "@/components/TradeHistory";
import { ActivityView } from "@/components/ActivityPage";
import { track } from "@/lib/track";
import { MAX_QUESTION as ROUTE_MAX_QUESTION } from "@/lib/assistant/service";
import {
  DOCS_BASE,
  EXAMPLES,
  FOOT_JOURNAL,
  FOOT_REFUSED,
  FOOT_SOURCES,
  MAX_QUESTION,
  OFF_TEXT,
  REFUSAL,
  DONT_KNOW,
  citationHref,
  footLine,
  journalSeqOf,
  legOf,
  limitText,
  whyQuestion,
} from "@/lib/assistantPanel";

// The assistant panel (#408): the menu bar's Ask, the panel's states, cited
// answers as chips, Flag, and Why? on trade rows — against a fake fetch of
// the monitor's own /api/assistant routes.

jest.mock("@/lib/track", () => ({ track: jest.fn() }));

const TRADE = {
  bot: "btc",
  seq: 41,
  kind: "close",
  at: "2026-10-27T13:00:00Z",
  instrument: "BTC-27NOV26-60000-P",
  slot: { dte: 30, delta: 0.16 },
  qty: 0.5,
  price: 0.01,
  reason: "stop_loss",
  plain: "Bought back the put: its stop-loss fired.",
  pnl: -0.002,
  pnl_usd: -130,
  unit: "BTC",
};

const ANSWER = {
  id: "a1",
  answer: "A leg is bought back when its stop-loss fires.",
  citations: [
    { id: "OptionStrategy.md#7-exit-rules", source: "OptionStrategy.md#7-exit-rules", title: "7. Exit rules" },
    { id: "glossary:stop-loss", source: "glossary:stop-loss", title: "Stop-loss" },
    { id: "journal:btc/41", source: "journal:btc", title: "btc journal: closed at 2026-10-27T13:00:00Z" },
  ],
  outcome: "answered",
  ai_generated: true,
  remaining: 29,
};

const json = (status, body, headers = {}) => ({
  ok: status >= 200 && status < 300,
  status,
  headers: { get: (k) => headers[k.toLowerCase()] ?? null },
  json: async () => body,
});

/** fetch: GET /api/assistant → state; POST /api/assistant → ask(); POST …/flag → flag(). */
function fakeFetch({ state = json(200, { enabled: true, remaining: 30, per_hour: 30 }), ask = () => json(200, ANSWER), flag = () => json(204, null) } = {}) {
  global.fetch = jest.fn(async (url, init = {}) => {
    if (url === "/api/assistant" && (init.method ?? "GET") === "GET") return state;
    if (url === "/api/assistant") return ask(JSON.parse(init.body));
    if (url === "/api/assistant/flag") return flag(JSON.parse(init.body));
    throw new Error(`unexpected ${url}`);
  });
}
const posts = (url) => global.fetch.mock.calls.filter(([u, i]) => u === url && i?.method === "POST").map(([, i]) => JSON.parse(i.body));

/** Renders inside the provider and lets its state request (GET /api/assistant) settle. */
async function renderIn(ui) {
  render(<AssistantProvider>{ui}</AssistantProvider>);
  await act(async () => {});
}
// The floating Ask (#419) asks about the page's bot, read from ?bot=.
const renderMenu = () => {
  window.history.replaceState(null, "", "/?bot=btc");
  return renderIn(<MenuNav pathname="/" bot="btc" />);
};
const askButton = () => screen.getByRole("button", { name: "Ask" });
const panel = () => screen.getByRole("complementary", { name: "Assistant" });

async function openPanel() {
  await renderMenu();
  fireEvent.click(askButton());
  return panel();
}

async function askQuestion(text = "When does the bot buy a leg back?") {
  fireEvent.change(screen.getByLabelText("Your question"), { target: { value: text } });
  fireEvent.keyDown(screen.getByLabelText("Your question"), { key: "Enter" });
}

const realFetch = global.fetch;
const realMatchMedia = window.matchMedia;
beforeEach(() => {
  track.mockClear();
  fakeFetch();
});
afterEach(() => {
  global.fetch = realFetch;
  window.matchMedia = realMatchMedia;
});

describe("Scenario: Ask opens the panel; Esc closes it and returns focus", () => {
  test("the panel opens with the examples, and Esc closes it and focuses Ask again", async () => {
    const p = await openPanel();
    expect(within(p).getByRole("heading", { name: "Assistant" })).toBeInTheDocument();
    expect(within(p).getByText("AI · answers from the docs and your journal")).toBeInTheDocument();
    expect(within(p).getByRole("tab", { name: "Ask" })).toHaveAttribute("aria-selected", "true");
    expect(within(p).queryByRole("tab", { name: "My requests" })).toBeNull();
    for (const q of EXAMPLES) expect(within(p).getByRole("button", { name: q })).toBeInTheDocument();
    expect(within(p).getByRole("log")).toHaveAttribute("aria-live", "polite");
    expect(within(p).getByText("Answers can be wrong: check the linked source. Not investment advice.")).toBeInTheDocument();
    expect(await within(p).findByText("30 of 30 questions left this hour")).toBeInTheDocument();
    expect(track).toHaveBeenCalledWith("assistant_open", { entry: "menu" });

    fireEvent.keyDown(window, { key: "Escape" });
    expect(screen.queryByRole("complementary", { name: "Assistant" })).toBeNull();
    await waitFor(() => expect(askButton()).toHaveFocus());
  });

  test("× closes it too", async () => {
    const p = await openPanel();
    fireEvent.click(within(p).getByRole("button", { name: "Close the assistant" }));
    expect(screen.queryByRole("complementary", { name: "Assistant" })).toBeNull();
  });

  test("on a phone the panel is a full-screen dialog that keeps the focus inside", async () => {
    window.matchMedia = jest.fn(() => ({ matches: true, addEventListener: jest.fn(), removeEventListener: jest.fn() }));
    await renderMenu();
    fireEvent.click(askButton());
    const dialog = screen.getByRole("dialog", { name: "Assistant" });
    expect(dialog).toHaveAttribute("aria-modal", "true");
    expect(dialog).toHaveClass("fixed", "inset-0", "sm:w-[400px]");
    const close = within(dialog).getByRole("button", { name: "Close the assistant" });
    close.focus();
    fireEvent.keyDown(window, { key: "Tab", shiftKey: true });
    // Shift+Tab from the first control goes to the last, never out of the sheet.
    expect(dialog.contains(document.activeElement)).toBe(true);
    expect(document.activeElement).not.toBe(close);
  });

  test("an example question is asked with the page's bot", async () => {
    const p = await openPanel();
    fireEvent.click(within(p).getByRole("button", { name: EXAMPLES[1] }));
    await within(p).findByText(ANSWER.answer);
    expect(posts("/api/assistant")).toEqual([{ question: EXAMPLES[1], bot: "btc" }]);
    expect(track).toHaveBeenCalledWith("assistant_ask", { entry: "menu" });
  });

  test("the conversation stays while the panel is closed and opened again", async () => {
    const p = await openPanel();
    await askQuestion();
    await within(p).findByText(ANSWER.answer);
    fireEvent.click(within(p).getByRole("button", { name: "Close the assistant" }));
    fireEvent.click(askButton());
    expect(within(panel()).getByText(ANSWER.answer)).toBeInTheDocument();
  });
});

describe("Scenario: citations render as chips linking to their source", () => {
  test("each citation is a chip; the answer shows its foot line and the questions left", async () => {
    const p = await openPanel();
    await askQuestion();
    // The thinking skeleton while the answer is on its way, then the answer.
    const answer = await within(p).findByTestId("assistant-answer");
    expect(within(answer).getByText(ANSWER.answer)).toHaveClass("whitespace-pre-line");
    const chips = within(within(answer).getByRole("list", { name: "Sources" })).getAllByRole("link");
    expect(chips.map((c) => [c.textContent, c.getAttribute("href")])).toEqual([
      ["7. Exit rules", `${DOCS_BASE}/OptionStrategy.md#7-exit-rules`],
      ["Stop-loss", `${DOCS_BASE}/OptionStrategy.md#4-how-a-trade-can-end`],
      ["btc journal: closed at 2026-10-27T13:00:00Z", "/?bot=btc&view=details#trade-btc-41"],
    ]);
    expect(chips[0]).toHaveAttribute("target", "_blank");
    expect(within(answer).getByText(FOOT_SOURCES)).toBeInTheDocument();
    expect(within(p).getByText("29 of 30 questions left this hour")).toBeInTheDocument();
  });

  test("Shift+Enter adds a line instead of sending; the question is capped at 500 characters", async () => {
    const p = await openPanel();
    const box = within(p).getByLabelText("Your question");
    expect(box).toHaveAttribute("maxLength", String(MAX_QUESTION));
    fireEvent.change(box, { target: { value: "first" } });
    fireEvent.keyDown(box, { key: "Enter", shiftKey: true });
    expect(posts("/api/assistant")).toEqual([]);
  });

  test("a refusal shows as written with its follow-ups", async () => {
    fakeFetch({ ask: () => json(200, { id: "a2", answer: REFUSAL, citations: [], outcome: "refused", remaining: 28 }) });
    const p = await openPanel();
    await askQuestion("Should I sell more puts?");
    expect(await within(p).findByText(REFUSAL)).toBeInTheDocument();
    expect(within(p).getByRole("link", { name: "Open Worst case" })).toHaveAttribute("href", "/worst-case?bot=btc");
    expect(within(p).getByText(FOOT_REFUSED)).toBeInTheDocument();
    fireEvent.click(within(p).getByRole("button", { name: "How does sizing work?" }));
    await waitFor(() => expect(posts("/api/assistant").at(-1)).toEqual({ question: "How does sizing work?", bot: "btc" }));
  });

  test("I don't know is shown plainly, with How it works", async () => {
    fakeFetch({ ask: () => json(200, { id: "a3", answer: DONT_KNOW, citations: [], outcome: "dont_know", remaining: 28 }) });
    const p = await openPanel();
    await askQuestion("zxqv");
    expect(await within(p).findByText(DONT_KNOW)).toBeInTheDocument();
    expect(within(p).getByRole("link", { name: "How it works" })).toHaveAttribute("href", "/how-it-works?bot=btc");
  });
});

describe("Scenario: the assistant is off (503)", () => {
  test("the Ask button is dimmed and says why; the panel says it too", async () => {
    fakeFetch({ state: json(503, { enabled: false, error: "the assistant is off: ANTHROPIC_API_KEY is not set" }) });
    await renderMenu();
    expect(askButton()).toHaveAttribute("title", OFF_TEXT);
    expect(askButton()).toHaveClass("opacity-50");
    fireEvent.click(askButton());
    expect(within(panel()).getByText(OFF_TEXT)).toBeInTheDocument();
    expect(within(panel()).getByLabelText("Your question")).toBeDisabled();
  });

  test("a 503 answer to a question turns the assistant off", async () => {
    fakeFetch({ ask: () => json(503, { error: "the assistant is off: ANTHROPIC_API_KEY is not set" }) });
    const p = await openPanel();
    await askQuestion();
    expect(await within(p).findByText(OFF_TEXT)).toBeInTheDocument();
    expect(askButton()).toHaveAttribute("title", OFF_TEXT);
  });
});

describe("Scenario: 30 questions an hour (429)", () => {
  test("the next allowed time is shown and none are left", async () => {
    fakeFetch({ ask: () => json(429, { error: "too many questions: try again later", retry_after: 600, remaining: 0 }, { "retry-after": "600" }) });
    const p = await openPanel();
    const before = Date.now();
    await askQuestion();
    const text = await within(p).findByText(/^30 questions an hour: next at \d\d:\d\d$/);
    expect(text.textContent).toBe(limitText(600, before));
    expect(within(p).getByText("0 of 30 questions left this hour")).toBeInTheDocument();
  });
});

describe("Scenario: an error offers a retry", () => {
  test("Retry asks the same question again", async () => {
    let calls = 0;
    fakeFetch({ ask: () => (++calls === 1 ? json(502, { id: "x", error: "the assistant did not answer: try again later" }) : json(200, ANSWER)) });
    const p = await openPanel();
    await askQuestion("What is a stop-loss?");
    const alert = await within(p).findByRole("alert");
    expect(alert).toHaveTextContent("try again later");
    fireEvent.click(within(alert).getByRole("button", { name: "Retry" }));
    await within(p).findByText(ANSWER.answer);
    expect(posts("/api/assistant").map((b) => b.question)).toEqual(["What is a stop-loss?", "What is a stop-loss?"]);
  });
});

describe("Scenario: Flag", () => {
  test("Flag calls /api/assistant/flag with the answer id and shows Flagged", async () => {
    const p = await openPanel();
    await askQuestion();
    const answer = await within(p).findByTestId("assistant-answer");
    fireEvent.click(within(answer).getByRole("button", { name: "Flag" }));
    expect(await within(answer).findByRole("button", { name: "Flagged" })).toBeDisabled();
    expect(posts("/api/assistant/flag")).toEqual([{ id: "a1" }]);
    expect(track).toHaveBeenCalledWith("assistant_flag", {});
  });

  test("a refused flag says so and stays a Flag button", async () => {
    fakeFetch({ flag: () => json(403, { error: "only the person who asked, a manager or an admin may flag this answer" }) });
    const p = await openPanel();
    await askQuestion();
    const answer = await within(p).findByTestId("assistant-answer");
    fireEvent.click(within(answer).getByRole("button", { name: "Flag" }));
    expect(await within(answer).findByText(/Not flagged: only the person who asked/)).toBeInTheDocument();
    expect(within(answer).getByRole("button", { name: "Flag" })).toBeEnabled();
  });

  test("👍 is a toggle button", async () => {
    const p = await openPanel();
    await askQuestion();
    const like = await within(p).findByRole("button", { name: "Helpful" });
    fireEvent.click(like);
    expect(like).toHaveAttribute("aria-pressed", "true");
  });
});

describe("Scenario: Why? on a trade row", () => {
  test("in the Details trade log it opens the panel and asks about that trade with bot and seq", async () => {
    await renderIn(<TradeHistory trades={[TRADE]} />);
    fireEvent.click(screen.getByRole("button", { name: /^Why\?/ }));
    const p = panel();
    const question = whyQuestion(TRADE);
    expect(question).toMatch(/^Why did BTC close the put BTC-27NOV26-60000-P on .+\?$/);
    expect(within(p).getByText(question)).toBeInTheDocument();
    await within(p).findByText(ANSWER.answer);
    expect(posts("/api/assistant")).toEqual([{ question, bot: "btc", seq: 41 }]);
    expect(track).toHaveBeenCalledWith("assistant_open", { entry: "why" });
    expect(track).toHaveBeenCalledWith("assistant_ask", { entry: "why" });
    // The journal chip opens that trade's row in Details.
    expect(document.getElementById("trade-btc-41")).not.toBeNull();
  });

  test("on the Activity page an open or close has Why?, a wait does not", async () => {
    const events = [
      { seq: 50, event: "filled", at: "2026-10-27T12:00:00Z", plain: "Sold the call: entry.", data: { instrument: "BTC-27NOV26-90000-C" } },
      { seq: 51, event: "skipped", at: "2026-10-27T12:05:00Z", plain: "No new strangle: it waits.", data: { skip_reason: "gex_wait" } },
    ];
    await renderIn(<ActivityView bot="btc" events={events} now={Date.parse("2026-10-27T15:00:00Z")} />);
    const whys = screen.getAllByRole("button", { name: /^Why\?/ });
    expect(whys).toHaveLength(1);
    fireEvent.click(whys[0]);
    await within(panel()).findByText(ANSWER.answer);
    expect(posts("/api/assistant")[0]).toMatchObject({ bot: "btc", seq: 50, question: expect.stringMatching(/^Why did BTC open the call BTC-27NOV26-90000-C on /) });
  });

  test("outside the assistant's provider there is no Why? and no Ask", () => {
    render(<TradeHistory trades={[TRADE]} />);
    expect(screen.queryByRole("button", { name: /^Why\?/ })).toBeNull();
    render(<MenuNav pathname="/" bot="btc" />);
    expect(screen.queryByRole("button", { name: "Ask" })).toBeNull();
  });
});

describe("lib/assistantPanel", () => {
  test("the panel's question cap is the route's", () => {
    expect(MAX_QUESTION).toBe(ROUTE_MAX_QUESTION);
  });

  test("legOf and journalSeqOf read the names", () => {
    expect(legOf("BTC-27NOV26-60000-P")).toBe("put");
    expect(legOf("ETH-27NOV26-4000-C")).toBe("call");
    expect(legOf(undefined)).toBeNull();
    expect(journalSeqOf("journal:btc/41")).toBe(41);
    expect(journalSeqOf("glossary:stop-loss")).toBeNull();
  });

  test("whyQuestion without an instrument still asks", () => {
    expect(whyQuestion({ bot: "eth", kind: "open", at: "2026-10-27T13:00:00Z" })).toMatch(/^Why did ETH open a leg on /);
  });

  test.each([
    [{ source: "docs/parameters.md#delta_exit_threshold" }, { href: `${DOCS_BASE}/docs/parameters.md#delta_exit_threshold`, external: true }],
    [{ source: "glossary:no-such-term" }, null],
    [{ id: "explain:closed:stop_loss", source: "explain:closed:stop_loss" }, { href: "/activity?bot=btc", external: false }],
    [{ id: "journal:eth", source: "journal:eth" }, { href: "/?bot=eth&view=details#details", external: false }],
    [{ source: "somewhere" }, null],
  ])("citationHref(%j)", (citation, want) => {
    expect(citationHref(citation, "btc")).toEqual(want);
  });

  test("footLine: journal facts, cited sources, refusals", () => {
    expect(footLine({ outcome: "answered", citations: [{ source: "journal:btc" }] })).toBe(FOOT_JOURNAL);
    expect(footLine({ outcome: "answered", citations: [{ source: "journal:btc" }, { source: "glossary:x" }] })).toBe(FOOT_SOURCES);
    expect(footLine({ outcome: "rejected" })).toBe(FOOT_REFUSED);
    expect(footLine({ outcome: "dont_know" })).toBe(FOOT_SOURCES);
  });

  test("limitText names the next time in the display zone", () => {
    expect(limitText(60, Date.parse("2026-10-27T13:00:00Z"))).toBe("30 questions an hour: next at 10:01");
  });
});
