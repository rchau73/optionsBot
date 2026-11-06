import { act, fireEvent, render, screen, within } from "@testing-library/react";
import Monitor from "@/components/Monitor";
import { FIRST_VISIT } from "@/components/SinceLastVisit";
import { useMonitor } from "@/hooks/useMonitor";
import { formatDateTime } from "@/lib/format";
import { DISMISS_KEY, VISIT_AFTER_MS, closeWords, compareWorst, parseVisit, sinceLastVisit, visitKey, visitRecord, warningsOf } from "@/lib/sincevisit";
import { track } from "@/lib/track";

// #618 (epic E3): "Since you last looked" — the Overview card that compares
// this browser's last visit (localStorage only) with now: the closes and their
// reasons, their result net of fees, the worst case then vs now, a warning
// that began meanwhile.

jest.mock("@/hooks/useMonitor", () => ({ useMonitor: jest.fn() }));
jest.mock("@/components/OverviewPnl", () => () => null);
jest.mock("@/lib/track", () => ({ track: jest.fn(() => true) }));

const NOW = Date.parse("2026-11-04T12:00:00Z");
const HOUR = 3_600_000;
const iso = (ms) => new Date(ms).toISOString();

const worstCase = (coin, usd) => ({ available: true, move_pct: 7.9, down: { loss_coin: coin, loss_usd: usd, pct_of_equity: 4 }, up: { loss_coin: coin / 2, loss_usd: usd / 2, pct_of_equity: 2 } });

function makeBot({ worst = worstCase(0.05, 4935), legs = 2, status = {}, ...extra } = {}) {
  return {
    name: "btc",
    stale: false,
    updatedAt: NOW,
    status: { as_of: iso(NOW), underlying: "BTC", environment: "testnet", market: { spot: 95900 }, risk: { im_pct: 10 }, held_legs: [], ...status },
    summary: {
      as_of: iso(NOW),
      underlying: "BTC",
      income: { this_month: { closes: 2, realised_coin: 0.004, realised_usd: 410 }, unrealised_coin: -0.001, unrealised_usd: -98 },
      worst_case: worst,
      holding: { strangles: legs ? 1 : 0, legs },
      last_action: null,
    },
    payoff: { as_of: iso(NOW), underlying: "BTC", spot: 95900, strangles: [] },
    ...extra,
  };
}

const close = (seq, hoursAgo, reason, pnl, pnlUSD) => ({ seq, at: iso(NOW - hoursAgo * HOUR), kind: "close", instrument: `BTC-27NOV26-${100000 + seq}-C`, qty: 1, reason, pnl, pnl_usd: pnlUSD });
const open = (seq, hoursAgo) => ({ seq, at: iso(NOW - hoursAgo * HOUR), kind: "open", instrument: "BTC-27NOV26-85000-P", qty: 1, reason: "entry" });

const storeVisit = (hoursAgo, extra = {}) =>
  window.localStorage.setItem(visitKey("btc"), JSON.stringify({ at: iso(NOW - hoursAgo * HOUR), worst: { coin: 0.03, usd: 2961 }, warnings: [], ...extra }));

function renderOverview({ bot = makeBot(), trades = [], hedgeEvents = [] } = {}) {
  useMonitor.mockReturnValue({ names: [bot.name], bots: [bot], feed: [], history: [], trades: { btc: trades }, tradesComplete: { btc: true }, hedgeEvents: { btc: hedgeEvents }, error: null, pollMs: 1000, polledAt: NOW });
  render(<Monitor />);
  return screen.getByTestId("desktop-monitor");
}
const card = (page) => within(page).getByRole("region", { name: "Since you last looked" });

beforeEach(() => {
  jest.useFakeTimers();
  jest.setSystemTime(NOW);
  window.localStorage.clear();
  track.mockClear();
});
afterEach(() => jest.useRealTimers());

describe("Scenario: a return after a profitable day", () => {
  test("the net result in coin and USD with its sign, and both closes with their reasons in plain words", () => {
    storeVisit(20);
    const page = renderOverview({ trades: [close(1, 30, "stop_loss", -0.01, -900), close(2, 6, "roi_target", 0.003, 290), close(3, 2, "dte_rollout", 0.001, 95)] });
    const c = card(page);
    expect(c).toHaveTextContent(`BTC · since ${formatDateTime(iso(NOW - 20 * HOUR))} (20h ago)`);
    expect(c).toHaveTextContent("Closed since then, net of fees: +0.00400 BTC · +$385");
    expect(within(c).getByText("+0.00400 BTC")).toHaveClass("text-profit");
    const rows = within(c).getAllByRole("listitem");
    expect(rows).toHaveLength(2); // the stop-loss came before the visit
    expect(rows[0]).toHaveTextContent("time roll (expiry near)");
    expect(rows[1]).toHaveTextContent("take profit");
    expect(within(c).getByRole("link", { name: "All in Activity →" })).toHaveAttribute("href", "/activity?bot=btc");
  });
});

describe("Scenario: a loss is as visible as a gain", () => {
  const SIZE = /^(text-(xs|sm|base|lg|xl|[2-9]xl)|font-\w+)$/;
  const sizeAndWeight = (el) => {
    const out = [];
    for (let n = el; n && n !== document.body; n = n.parentElement) {
      for (const kind of ["text-", "font-"]) {
        const own = [...n.classList].find((x) => SIZE.test(x) && x.startsWith(kind));
        if (own && !out.some((x) => x.startsWith(kind))) out.push(own);
      }
    }
    return out.sort();
  };

  test("the figure has a minus sign, the unit and the ▼ loss mark, at the size and weight of a gain", () => {
    storeVisit(20);
    const gain = within(card(renderOverview({ trades: [close(1, 2, "roi_target", 0.004, 400)] }))).getAllByText("+0.00400 BTC")[0];
    const gainLook = sizeAndWeight(gain);
    document.body.innerHTML = "";
    const c = card(renderOverview({ trades: [close(1, 2, "stop_loss", -0.004, -400)] }));
    const loss = within(c).getAllByText("−0.00400 BTC")[0];
    expect(loss).toHaveClass("text-loss");
    expect(sizeAndWeight(loss)).toEqual(gainLook);
    expect(within(loss.parentElement).getByLabelText("loss")).toHaveTextContent("▼ loss");
    expect(c).toHaveTextContent("Closed since then, net of fees: ▼ loss−0.00400 BTC · ▼ loss−$400");
  });
});

describe("the futures hedge's lines (#679)", () => {
  const hedgeLine = (seq, hoursAgo, data) => ({ seq, event: "hedge", at: iso(NOW - hoursAgo * HOUR), data, plain: "The bot's own sentence." });
  const fill = (seq, hoursAgo) => hedgeLine(seq, hoursAgo, { change: "hedge_fill", side: "sell", amount_usd: 300, filled_usd: 300, price: 75000, options_delta: 0.5, perp_delta: -0.08, band_coin: 0.3 });
  const unavailable = (seq, hoursAgo) => hedgeLine(seq, hoursAgo, { change: "hedge_unavailable", options_delta: 0.4 });
  const rows = (c) => within(within(c).getByTestId("since-hedge")).getAllByRole("listitem");

  test("Scenario: hedge unavailable: the card says the hedge cannot trade and the GEX shed is back, marked by more than colour", () => {
    storeVisit(5);
    const c = card(renderOverview({ hedgeEvents: [unavailable(1, 2)] }));
    expect(c).toHaveTextContent("Futures hedge:");
    expect(c).toHaveTextContent("⚠ The futures hedge cannot trade, so the GEX shed is back on");
    expect(c).not.toHaveTextContent("Nothing happened");
  });

  test("a fill reads as the Activity page does, and a line before the visit is not listed", () => {
    storeVisit(5);
    const c = card(renderOverview({ hedgeEvents: [fill(1, 6), fill(2, 1)] }));
    expect(rows(c)).toHaveLength(1);
    expect(rows(c)[0]).toHaveTextContent("Sold 300 USD of the perpetual to bring the book's coin delta back inside its band");
    expect(rows(c)[0]).not.toHaveTextContent("⚠");
  });

  test("a close of the perpetual shows its result, a loss with its mark", () => {
    storeVisit(5);
    const closed = hedgeLine(1, 1, { change: "hedge_closed", side: "buy", amount_usd: 300, filled_usd: 300, price: 75000, realised_coin: -0.0012 });
    const row = rows(card(renderOverview({ hedgeEvents: [closed] })))[0];
    expect(row).toHaveTextContent("▼ loss−0.00120 BTC");
    expect(within(row).getByText("−0.00120 BTC")).toHaveClass("text-loss");
  });

  test("a change of protection stays on the card under a run of trades; the rest are counted", () => {
    storeVisit(20);
    const c = card(renderOverview({ hedgeEvents: [unavailable(1, 10), ...[2, 3, 4, 5, 6, 7].map((i) => fill(i, 9 - i))] }));
    expect(rows(c)).toHaveLength(5);
    expect(rows(c)[0]).toHaveTextContent("⚠ The futures hedge cannot trade");
    expect(c).toHaveTextContent("and 2 more.");
  });

  test("a line of a change the monitor does not know keeps the bot's sentence", () => {
    storeVisit(5);
    const c = card(renderOverview({ hedgeEvents: [hedgeLine(1, 1, { change: "hedge_new" })] }));
    expect(rows(c)[0]).toHaveTextContent("The bot's own sentence.");
  });

  test("the lines show while the closes load, and stay readable on a phone", () => {
    storeVisit(5);
    useMonitor.mockReturnValue({ names: ["btc"], bots: [makeBot()], trades: { btc: [] }, tradesComplete: { btc: false }, hedgeEvents: { btc: [unavailable(1, 1)] }, error: null, pollMs: 1000, polledAt: NOW });
    const { container } = render(<Monitor />);
    const c = within(container.querySelector(".sm\\:hidden")).getByRole("region", { name: "Since you last looked" });
    expect(c).toHaveTextContent("Loading the closes…");
    expect(rows(c)[0]).toHaveClass("min-w-0", "break-words");
    expect(c.outerHTML).not.toMatch(/w-\[\d+px\]|whitespace-nowrap/);
  });
});

describe("Scenario: nothing happened", () => {
  test('"Nothing happened since <time>" and no zero for an unknown figure', () => {
    storeVisit(5, { worst: null });
    const c = card(renderOverview({ trades: [close(1, 30, "roi_target", 0.004, 400)] }));
    expect(c).toHaveTextContent(`Nothing happened since ${formatDateTime(iso(NOW - 5 * HOUR))}.`);
    expect(c).toHaveTextContent("Worst case then vs now unknown: not recorded at your last visit.");
    expect(c).not.toHaveTextContent(/0\.00000|\$0\b/);
  });

  test("the closes not all loaded yet: says so instead of nothing", () => {
    storeVisit(5);
    const bot = makeBot();
    useMonitor.mockReturnValue({ names: ["btc"], bots: [bot], trades: { btc: [] }, tradesComplete: { btc: false }, error: null, pollMs: 1000, polledAt: NOW });
    render(<Monitor />);
    const c = card(screen.getByTestId("desktop-monitor"));
    expect(c).toHaveTextContent("Loading the closes…");
    expect(c).not.toHaveTextContent("Nothing happened");
  });

  test("a bot that does not answer: the closes are unknown with the reason", () => {
    storeVisit(5);
    const c = card(renderOverview({ bot: makeBot({ summary: null, error: "bot unreachable" }) }));
    expect(c).toHaveTextContent("Closes unknown: bot unreachable.");
    expect(c).toHaveTextContent("Worst case then vs now unknown: bot unreachable.");
  });
});

describe("Scenario: first visit", () => {
  test("no card, and the one-line explanation once", () => {
    const page = renderOverview();
    expect(within(page).queryByRole("region", { name: "Since you last looked" })).toBeNull();
    expect(within(page).getAllByText(FIRST_VISIT)).toHaveLength(1);
  });

  test("an unreadable stored visit counts as none", () => {
    window.localStorage.setItem(visitKey("btc"), "{not json");
    const page = renderOverview();
    expect(within(page).getByText(FIRST_VISIT)).toBeInTheDocument();
  });
});

describe("Scenario: the card writes nothing outside the browser", () => {
  test("the visit is stored after 10 s visible; no request carries it and no analytics event is sent", () => {
    const fetchSpy = jest.fn(() => Promise.resolve({ ok: true, json: () => Promise.resolve({}) }));
    global.fetch = fetchSpy;
    storeVisit(20);
    const page = renderOverview({ trades: [close(1, 2, "roi_target", 0.004, 400)] });
    act(() => jest.advanceTimersByTime(VISIT_AFTER_MS - 1));
    expect(parseVisit(window.localStorage.getItem(visitKey("btc"))).at).toBe(NOW - 20 * HOUR);
    act(() => jest.advanceTimersByTime(1));
    // The card keeps comparing with the visit it opened with.
    expect(card(page)).toHaveTextContent("20h ago");
    const stored = window.localStorage.getItem(visitKey("btc"));
    expect(parseVisit(stored).at).toBe(NOW + VISIT_AFTER_MS);
    expect(JSON.parse(stored)).toEqual({ at: iso(NOW + VISIT_AFTER_MS), worst: { coin: 0.05, usd: 4935 }, warnings: [] });
    const sent = JSON.stringify(fetchSpy.mock.calls);
    expect(sent).not.toContain(iso(NOW - 20 * HOUR));
    expect(sent).not.toContain(iso(NOW + VISIT_AFTER_MS));
    expect(track).not.toHaveBeenCalled();
    delete global.fetch;
  });

  test("hidden time does not count toward the 10 s", () => {
    renderOverview();
    const setVisibility = (state) => {
      Object.defineProperty(document, "visibilityState", { configurable: true, get: () => state });
      act(() => document.dispatchEvent(new Event("visibilitychange")));
    };
    act(() => jest.advanceTimersByTime(4000));
    setVisibility("hidden");
    act(() => jest.advanceTimersByTime(60_000));
    setVisibility("visible");
    act(() => jest.advanceTimersByTime(5000));
    expect(window.localStorage.getItem(visitKey("btc"))).toBeNull();
    act(() => jest.advanceTimersByTime(1000));
    expect(parseVisit(window.localStorage.getItem(visitKey("btc"))).at).toBe(NOW + 70_000);
    delete document.visibilityState;
  });

  test("dismissed: hidden until the next visit, also after a reload", () => {
    storeVisit(20);
    const page = renderOverview();
    fireEvent.click(within(card(page)).getByRole("button", { name: "Hide until my next visit" }));
    expect(within(page).queryByRole("region", { name: "Since you last looked" })).toBeNull();
    expect(window.localStorage.getItem(DISMISS_KEY)).toBe(`btc@${NOW - 20 * HOUR}`);
    document.body.innerHTML = "";
    expect(within(renderOverview()).queryByRole("region", { name: "Since you last looked" })).toBeNull();
    // The next visit (a newer stored time) shows the card again.
    document.body.innerHTML = "";
    storeVisit(1);
    expect(card(renderOverview())).toBeInTheDocument();
  });
});

describe("Scenario: a phone", () => {
  test("the phone's card has 44 px buttons and links and nothing of a fixed width", () => {
    storeVisit(20);
    useMonitor.mockReturnValue({ names: ["btc"], bots: [makeBot()], trades: { btc: [close(1, 2, "roi_target", 0.004, 400)] }, tradesComplete: { btc: true }, error: null, pollMs: 1000, polledAt: NOW });
    const { container } = render(<Monitor />);
    const phone = container.querySelector(".sm\\:hidden");
    const c = within(phone).getByRole("region", { name: "Since you last looked" });
    for (const el of within(c).getAllByRole("button").concat(within(c).getAllByRole("link"))) expect(el).toHaveClass("min-h-11");
    expect(within(c).getByRole("button")).toHaveClass("min-w-11");
    expect(c.outerHTML).not.toMatch(/w-\[\d+px\]|whitespace-nowrap/);
    expect(within(c).getByText(/bought back 1 ×/)).toHaveClass("break-all");
    expect(c).toHaveClass("min-w-0");
  });
});

describe("the card's other states", () => {
  test("more than five closes: five rows, how many more, and the Activity link", () => {
    storeVisit(20);
    const trades = [1, 2, 3, 4, 5, 6, 7].map((i) => close(i, 10 - i, "roi_target", 0.001, 100));
    const c = card(renderOverview({ trades: [open(0, 12), ...trades] }));
    expect(within(c).getAllByRole("listitem")).toHaveLength(5);
    expect(c).toHaveTextContent("7 closes:");
    expect(c).toHaveTextContent("and 2 more.");
    expect(c).toHaveTextContent("1 leg sold since then.");
    expect(within(c).getByRole("link", { name: "All in Activity →" })).toBeInTheDocument();
  });

  test("away more than 30 days: the window is capped and says so", () => {
    storeVisit(40 * 24);
    const c = card(renderOverview({ trades: [close(1, 35 * 24, "stop_loss", -0.01, -900), close(2, 24, "roi_target", 0.002, 200)] }));
    expect(c).toHaveTextContent("You were away more than 30 days: this covers the last 30 days only.");
    expect(c).toHaveTextContent("(30 d ago)");
    expect(within(c).getAllByRole("listitem")).toHaveLength(1);
  });

  test("a warning that began since the last visit is pointed at once; one already there is not", () => {
    storeVisit(3, { warnings: ["IM over limit"] });
    const c = card(renderOverview({ bot: makeBot({ status: { halted: true, risk: { im_over_limit: true } } }) }));
    const pointers = within(c).getAllByRole("status");
    expect(pointers).toHaveLength(1);
    expect(pointers[0]).toHaveTextContent("⚠ HALTED began since then: the kill switch fired and trading is halted. See the header.");
    expect(c).not.toHaveTextContent("Nothing happened");
  });

  test("the worst case then vs now: larger, with both figures in coin and USD", () => {
    storeVisit(3);
    expect(within(card(renderOverview())).getByTestId("since-worst")).toHaveTextContent(
      "Worst case (one very bad day): larger, then −0.03000 BTC (≈ −$2,961), now −0.05000 BTC (≈ −$4,935).",
    );
  });
});

describe("the helpers in lib/sincevisit.js", () => {
  test("compareWorst: larger, smaller, about the same, nothing open, unknown", () => {
    const at = (coin) => ({ coin, usd: coin * 100000 });
    expect(compareWorst(at(0.03), at(0.05)).verdict).toBe("larger");
    expect(compareWorst(at(0.05), at(0.03)).verdict).toBe("smaller");
    expect(compareWorst(at(0.05), at(0.0525)).verdict).toBe("about the same");
    expect(compareWorst({ none: true }, at(0.01)).verdict).toBe("larger");
    expect(compareWorst(at(0.01), { none: true }).verdict).toBe("smaller");
    expect(compareWorst({ none: true }, { none: true })).toEqual({ none: true });
    expect(compareWorst(null, at(0.01))).toEqual({ unknown: "not recorded at your last visit" });
    expect(compareWorst(at(0.01), { unknown: "not computed yet" })).toEqual({ unknown: "not computed yet" });
  });

  test("visitRecord: nothing before the summary; nothing open recorded as such", () => {
    expect(visitRecord({ name: "btc", summary: null }, NOW)).toBeNull();
    expect(visitRecord(makeBot({ legs: 0 }), NOW)).toEqual({ at: iso(NOW), worst: { none: true }, warnings: [] });
    expect(visitRecord(makeBot({ worst: { available: false, reason: "no DVOL" } }), NOW).worst).toBeNull();
  });

  test("warningsOf, closeWords and parseVisit", () => {
    expect(warningsOf({ stale: true, status: {} })).toEqual(["STALE"]);
    expect(warningsOf({ stale: true, error: "down", status: { halted: true } })).toEqual(["HALTED", "OFFLINE"]);
    expect(closeWords("delta_exit")).toBe("exit line (delta reached its limit)");
    expect(closeWords("delta_drift")).toBe("roll (delta drifted)");
    expect(closeWords("margin_mm_limit")).toBe("mm limit cut");
    expect(parseVisit(null)).toBeNull();
    expect(parseVisit('{"at":"never"}')).toBeNull();
    expect(parseVisit('{"at":"2026-11-04T12:00:00Z"}')).toEqual({ at: NOW, worst: null, warnings: [] });
  });

  test("sinceLastVisit: a close at the visit's exact time is not counted; a missing pnl counts as 0", () => {
    const visit = { at: NOW - HOUR, worst: null, warnings: [] };
    const v = sinceLastVisit({ bot: makeBot(), trades: [close(1, 1, "roi_target", 0.01, 1000), { ...close(2, 0.5, "roi_target"), pnl: undefined, pnl_usd: undefined }], visit, now: NOW });
    expect(v.closes.map((c) => c.seq)).toEqual([2]);
    expect(v.result).toEqual({ coin: 0, usd: 0 });
  });
});
