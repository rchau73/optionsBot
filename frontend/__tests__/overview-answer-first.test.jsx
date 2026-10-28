import { fireEvent, render, screen, waitFor, within } from "@testing-library/react";
import BandChart from "@/components/BandChart";
import { BotDashboard, NOTHING_HELD } from "@/components/BotDashboard";
import OverviewPnl from "@/components/OverviewPnl";
import OverviewStatus from "@/components/OverviewStatus";
import OverviewTiles from "@/components/OverviewTiles";
import StrangleRow from "@/components/StrangleRow";
import { bandChart, keptOf, nudgeLabels, rangeStrip, soldStrikes, strikeCorridor } from "@/lib/band";
import { coinDigits, earnedLines, fmtCoin, heldBackLines, heldSentence, marginLines, roomFooter, statusLine, usdFooter, worstLines } from "@/lib/tiles";
import { resetTracking, setAnalyticsConsent } from "@/lib/track";

// #404: the Overview, answer first — status line, four coin-first tiles, the
// P&L chart (Coin | USD), one band per bot with the sold-strike corridor and
// its strangle rows.

/** 60 daily candles; odd days fall (close < open). */
const candles = () =>
  Array.from({ length: 60 }, (_, i) => {
    const d = new Date(Date.UTC(2026, 7, 29 + i));
    const close = 90000 + i * 100;
    const open = i % 2 ? close + 150 : close - 150;
    return { date: d.toISOString().slice(0, 10), open, high: Math.max(open, close) + 300, low: Math.min(open, close) - 300, close };
  });

const status = (coin, extra = {}) => ({
  as_of: new Date().toISOString(),
  underlying: coin,
  environment: "testnet",
  market: { spot: 95900, gex_regime: "NEGATIVE/ACCELERATION", gamma_flip: 97000, dvol: 45.2 },
  flip_buffer_pct: 2,
  direction: "sma",
  trend: "bear",
  gex_as_of: new Date(Date.now() - 30_000).toISOString(),
  sma: { sma9: 95500, sma21: 94000, reason: "SMA9 > SMA21", candles: candles() },
  risk: { status: { limit_im_pct: 20, max_mm_pct: 50, frozen: false, regime_known: true, regime_negative: true }, im_pct: 14.2, mm_pct: 10, as_of: new Date().toISOString() },
  held_legs: [],
  ...extra,
});

const month = (m, coin, usd, closes = 2) => ({ month: m, realised_coin: coin, realised_usd: usd, closes });
const summary = (coin, k = 1) => ({
  as_of: "2026-10-27T15:00:00Z",
  underlying: coin,
  income: { coin, months: [month("2026-10", 0.004 * k, 410)], this_month: month("2026-10", 0.004 * k, 410, 1), unrealised_coin: -0.001 * k, unrealised_usd: -98.7 },
  worst_case: { available: true, move_pct: 7.9, coin, down: { loss_coin: 0.05 * k, loss_usd: 4935, pct_of_equity: 4.2 }, up: { loss_coin: 0.03 * k, loss_usd: 2961, pct_of_equity: 2.5 } },
  holding: { legs: 2 },
  last_action: { at: "2026-10-27T14:02:00Z", kind: "open" },
});

const leg = (type, strike) => ({ position_id: `${type}-${strike}`, instrument: `X-${strike}`, option_type: type, strike, qty: 1 });
const strangle = (id, extra = {}) => ({
  strangle_id: id,
  slot: { dte: 25, delta: 0.16 },
  expiry: "2026-11-27T08:00:00Z",
  dte: 31.4,
  spot: 95900,
  legs: [leg("put", 85000), leg("call", 110000)],
  missing: [],
  premium_coin: 0.01,
  unrealised_coin: 0.0062,
  unrealised_usd: 594.6,
  earned_pct: 62,
  break_even_low: 84000,
  break_even_high: 111000,
  ...extra,
});
const payoff = (strangles) => ({ as_of: new Date().toISOString(), underlying: "BTC", spot: 95900, strangles });

const bot = (name, coin, k = 1) => ({ name, stale: false, status: status(coin), summary: summary(coin, k), payoff: payoff([strangle(`${name}-a`)]) });

beforeEach(() => {
  window.localStorage.clear();
  resetTracking();
});

describe("Scenario: two bots, the four tiles coin first", () => {
  test("each tile shows each bot's value in its coin (BTC 5 decimals, ETH 4) and the USD sum only on the footer", () => {
    render(<OverviewTiles bots={[bot("btc", "BTC"), bot("eth", "ETH")]} />);
    const earned = screen.getByRole("region", { name: "Coins earned this month" });
    expect(within(within(earned).getByTestId("tile-line-btc")).getByText("+0.00400 BTC")).toBeInTheDocument();
    expect(within(within(earned).getByTestId("tile-line-eth")).getByText("+0.0040 ETH")).toBeInTheDocument();
    expect(earned).toHaveTextContent("≈ +$820 at today's prices");
    // USD only on the footer: no bot line carries a dollar figure, and the coins are never added.
    for (const tile of ["Coins earned this month", "Open now", "Worst case"]) {
      const region = screen.getByRole("region", { name: tile });
      for (const l of within(region).getAllByTestId(/^tile-line-/)) expect(l).not.toHaveTextContent("$");
      expect(region).not.toHaveTextContent(/\+0\.0080/);
    }
    expect(screen.getByRole("region", { name: "Open now" })).toHaveTextContent("−0.00100 BTC");
    const worst = screen.getByRole("region", { name: "Worst case" });
    expect(within(worst).getByTestId("tile-line-btc")).toHaveTextContent("−0.05000 BTC");
    expect(within(worst).getByTestId("tile-line-btc")).toHaveTextContent("−4.2% of account");
    expect(worst).toHaveTextContent("≈ −$9,870 at today's prices");
    const margin = screen.getByRole("region", { name: "Margin used" });
    expect(within(margin).getAllByRole("meter")).toHaveLength(2);
    expect(margin).toHaveTextContent("BTC 71% of limit");
    expect(margin).toHaveTextContent("room left: BTC 29% · ETH 29% of limit");
  });

  test("a bot without a summary says unknown with the reason and is named on the footer, never 0", () => {
    const eth = { ...bot("eth", "ETH"), summary: null, summaryError: "bot unreachable" };
    render(<OverviewTiles bots={[bot("btc", "BTC"), eth]} />);
    const earned = screen.getByRole("region", { name: "Coins earned this month" });
    expect(within(earned).getByTestId("tile-line-eth")).toHaveTextContent("unknown: bot unreachable");
    expect(earned).toHaveTextContent("≈ +$410 at today's prices · without ETH");
  });

  test("a loss is red with its sign, nothing hidden", () => {
    const b = bot("btc", "BTC");
    b.summary.income.this_month = month("2026-10", -0.002, -190, 3);
    render(<OverviewTiles bots={[b]} />);
    const v = within(screen.getByRole("region", { name: "Coins earned this month" })).getByText("−0.00200 BTC");
    expect(v).toHaveClass("text-loss");
  });

  test("loading shows skeleton tiles", () => {
    render(<OverviewTiles bots={[]} loading />);
    expect(screen.getByTestId("tiles-loading")).toHaveAttribute("aria-busy", "true");
  });
});

describe("Scenario: the P&L section's Coin | USD switch", () => {
  const realFetch = global.fetch;
  beforeEach(() => {
    global.fetch = jest.fn().mockImplementation(async (url) => ({
      ok: true,
      json: async () => ({
        points: String(url).includes("/btc/")
          ? [
              { t: "2026-10-26T12:00:00Z", realised: 0.01, total: 0.02, realised_usd: 900, total_usd: 1800 },
              { t: "2026-10-27T12:00:00Z", realised: 0.011, total: 0.0125, realised_usd: 1000, total_usd: 1200 },
            ]
          : [
              { t: "2026-10-26T12:00:00Z", realised: 0.1, total: 0.2, realised_usd: 300, total_usd: 600 },
              { t: "2026-10-27T12:00:00Z", realised: 0.1, total: 0.15, realised_usd: 300, total_usd: 450 },
            ],
      }),
    }));
  });
  afterEach(() => {
    global.fetch = realFetch;
  });

  test("Coin by default with one chart per bot, 1w; USD shows one chart for all bots; Coin returns one per bot", async () => {
    setAnalyticsConsent(true);
    render(<OverviewPnl names={["btc", "eth"]} units={{ btc: "BTC", eth: "ETH" }} />);
    const unit = screen.getByRole("group", { name: "P&L unit" });
    expect(within(unit).getByRole("button", { name: "Coin" })).toHaveAttribute("aria-pressed", "true");
    expect(within(screen.getByRole("group", { name: "P&L range" })).getByRole("button", { name: "1w" })).toHaveAttribute("aria-pressed", "true");
    await waitFor(() => expect(global.fetch).toHaveBeenCalledWith("/api/bots/btc/pnl/history?range=1w", expect.anything()));
    expect(await screen.findByText("now +0.01250 BTC · realised +0.01100 BTC")).toBeInTheDocument();
    expect(screen.getByText("now +0.1500 ETH · realised +0.1000 ETH")).toBeInTheDocument();
    expect(screen.getAllByRole("img")).toHaveLength(2);

    fireEvent.click(within(unit).getByRole("button", { name: "USD" }));
    expect(within(unit).getByRole("button", { name: "USD" })).toHaveAttribute("aria-pressed", "true");
    expect(screen.getAllByRole("img")).toHaveLength(1);
    expect(screen.getByText("All bots in USD")).toBeInTheDocument();
    expect(screen.getByText("now +$1,650 · realised +$1,300")).toBeInTheDocument();
    const sent = global.fetch.mock.calls.filter(([u]) => u === "/api/events").map(([, init]) => JSON.parse(init.body));
    expect(sent).toEqual([expect.objectContaining({ event: "overview_unit_toggle", props: { unit: "usd" } })]);

    fireEvent.click(within(unit).getByRole("button", { name: "Coin" }));
    expect(screen.getAllByRole("img")).toHaveLength(2);
  });

  test("the range switch offers 1d, 1w and 1m only", async () => {
    render(<OverviewPnl names={["btc"]} units={{ btc: "BTC" }} />);
    const range = screen.getByRole("group", { name: "P&L range" });
    expect(within(range).getAllByRole("button").map((b) => b.textContent)).toEqual(["1d", "1w", "1m"]);
    fireEvent.click(within(range).getByRole("button", { name: "1m" }));
    await waitFor(() => expect(global.fetch).toHaveBeenCalledWith("/api/bots/btc/pnl/history?range=1m", expect.anything()));
    await screen.findByText(/^now /);
  });
});

describe("Scenario: a strangle with no put", () => {
  test("its row shows the amber stub and the note with the held-leg reason", () => {
    const sp = strangle("x", {
      legs: [leg("call", 110000)],
      missing: [{ option_type: "put", rule: "regime_side", reason: "confirmed negative gamma regime in a bear trend: no new puts" }],
      break_even_low: null,
    });
    render(<StrangleRow sp={sp} coin="BTC" onOpen={() => {}} />);
    const row = screen.getByRole("button");
    expect(within(row).getByTestId("stub-put")).toHaveClass("hatch-warn");
    expect(within(row).queryByTestId("tick-put")).toBeNull();
    expect(within(row).getByTestId("tick-call")).toBeInTheDocument();
    const note = within(row).getByTestId("held-note");
    expect(note).toHaveClass("text-warn");
    expect(note).toHaveTextContent("no put held: repair waiting (confirmed negative gamma regime in a bear trend: no new puts)");
    expect(row.getAttribute("aria-label")).toContain("no put held");
  });

  test("a full strangle row: slot, expiry, days left, coin P&L with ≈ USD, distance to a break-even and premium kept", () => {
    const onOpen = jest.fn();
    render(<StrangleRow sp={strangle("a")} coin="BTC" onOpen={onOpen} />);
    const row = screen.getByRole("button");
    expect(row).toHaveTextContent("25 d slot");
    expect(row).toHaveTextContent("31 d left");
    expect(within(row).getByText("+0.00620 BTC")).toHaveClass("text-profit");
    expect(row).toHaveTextContent("≈ +$595");
    expect(row).toHaveTextContent("spot 12.4% from a break-even");
    expect(row).toHaveTextContent("62 % of the premium kept");
    expect(within(row).queryByTestId(/^stub-/)).toBeNull();
    expect(row).toHaveAttribute("aria-haspopup", "dialog");
    fireEvent.click(row);
    expect(onOpen).toHaveBeenCalledWith("a", row);
  });
});

describe("the bot band", () => {
  test("header: coin, spot, regime and trend chips, GEX age · DVOL and the margin bar", () => {
    render(<BotDashboard bot={bot("btc", "BTC")} onOpen={() => {}} />);
    const line = screen.getByTestId("regime-line-btc");
    expect(line).toHaveTextContent("$95,900");
    expect(within(line).getByText("NEGATIVE · confirmed")).toBeInTheDocument();
    expect(within(line).getByText("trend bear")).toBeInTheDocument();
    expect(within(line).getByText(/^GEX \d+s ago$/)).toBeInTheDocument();
    expect(line).toHaveTextContent("DVOL 45.2");
    expect(within(screen.getByTestId("band-margin-btc")).getByRole("meter", { name: "margin 71% of limit" })).toBeInTheDocument();
  });

  test("no strangles says what the bot does next", () => {
    const b = bot("btc", "BTC");
    b.payoff = payoff([]);
    render(<BotDashboard bot={b} onOpen={() => {}} />);
    expect(screen.getByRole("region", { name: "BTC · strangles" })).toHaveTextContent(NOTHING_HELD);
  });

  test("a stale bot is dimmed with its age; a bot that is down says so", () => {
    const stale = { ...bot("btc", "BTC"), stale: true };
    const { unmount } = render(<BotDashboard bot={stale} onOpen={() => {}} />);
    expect(screen.getByRole("region", { name: "BTC dashboard" })).toHaveClass("opacity-60");
    expect(screen.getByText(/^stale · updated /)).toBeInTheDocument();
    unmount();
    render(<BotDashboard bot={{ name: "eth", stale: true, error: "connection refused" }} onOpen={() => {}} />);
    expect(screen.getByRole("alert")).toHaveTextContent("ETH bot is down: connection refused");
  });
});

describe("the band's price chart", () => {
  const farPut = payoff([strangle("a", { legs: [leg("put", 70000), leg("call", 110000)] })]);

  test("60 candles, up green and down neutral grey; the corridor between the sold strikes; labelled strikes", () => {
    const { container } = render(<BandChart status={status("BTC")} payoff={farPut} coin="BTC" />);
    const shown = screen.getAllByTestId("candle");
    expect(shown).toHaveLength(60);
    expect(shown.filter((c) => c.dataset.up === "false")[0]).toHaveClass("fill-slate-500");
    expect(shown.filter((c) => c.dataset.up === "true")[0]).toHaveClass("fill-profit");
    expect(container.querySelector(".fill-loss")).toBeNull(); // red only for a loss
    expect(screen.getByTestId("strike-corridor")).toHaveAttribute("fill-opacity", "0.06");
    expect(screen.getAllByTestId("sold-strike")).toHaveLength(2);
    const words = [...container.querySelectorAll("text")].map((t) => t.textContent);
    expect(words).toEqual(expect.arrayContaining(["put 70,000 · 31 d", "call 110,000 · 31 d", "flip 97,000", "SMA9 95,500", "spot 95,900"]));
    const alt = screen.getByRole("img").getAttribute("aria-label");
    expect(alt).toContain("sold strikes from 70,000 to 110,000, spot inside");
  });

  test("the y-range covers every sold strike, and no two labels overlap", () => {
    const g = bandChart(status("BTC"), farPut);
    expect(g.domain[0]).toBeLessThan(70000);
    expect(g.domain[1]).toBeGreaterThan(110000);
    const ys = g.labels.map((l) => l.labelY).sort((a, b) => a - b);
    for (let i = 1; i < ys.length; i++) expect(ys[i] - ys[i - 1]).toBeGreaterThanOrEqual(g.box.labelGap - 1e-9);
  });

  test("the pills are real buttons with aria-pressed, all on; each toggles its layer and sends overview_layer_toggle", () => {
    setAnalyticsConsent(true);
    const realFetch = global.fetch;
    global.fetch = jest.fn().mockResolvedValue({ ok: true });
    render(<BandChart status={status("BTC")} payoff={farPut} coin="BTC" />);
    const pills = screen.getByRole("group", { name: "BTC chart layers" });
    expect(within(pills).getAllByRole("button").map((b) => [b.textContent, b.getAttribute("aria-pressed")])).toEqual([
      ["SMA9", "true"],
      ["Flip band", "true"],
      ["Sold strikes", "true"],
    ]);
    fireEvent.click(within(pills).getByRole("button", { name: "Sold strikes" }));
    expect(within(pills).getByRole("button", { name: "Sold strikes" })).toHaveAttribute("aria-pressed", "false");
    expect(screen.queryAllByTestId("sold-strike")).toHaveLength(0);
    expect(screen.queryByTestId("strike-corridor")).toBeNull();
    fireEvent.click(within(pills).getByRole("button", { name: "SMA9" }));
    expect(screen.queryByTestId("sma9")).toBeNull();
    fireEvent.click(within(pills).getByRole("button", { name: "Flip band" }));
    expect(screen.queryByTestId("flip-band")).toBeNull();
    const sent = global.fetch.mock.calls.map(([, init]) => JSON.parse(init.body).props.layer);
    expect(sent).toEqual(["strikes", "sma", "flip"]);
    global.fetch = realFetch;
  });

  test("without candles it says unknown, never a flat chart", () => {
    render(<BandChart status={status("BTC", { sma: {} })} payoff={farPut} coin="BTC" />);
    expect(screen.getByText(/Candles unknown/)).toBeInTheDocument();
  });
});

describe("Scenario: the status line and the held-back lines", () => {
  test("'Both trading · updated <time>' and one amber line per bot that holds something back", () => {
    const eth = bot("eth", "ETH");
    eth.status.held_legs = [
      { strangle_id: "s1", missing: "put", rule: "regime_side", trend: "bear", reason: "confirmed negative gamma regime in a bear trend: no new puts" },
      { strangle_id: "s2", missing: "put", rule: "regime_side", trend: "bear", reason: "same" },
      { strangle_id: "s3", missing: "call", rule: "repair_working", reason: "an order is working" },
    ];
    render(<OverviewStatus bots={[bot("btc", "BTC"), eth]} />);
    expect(screen.getByRole("status")).toHaveTextContent(/^Both trading · updated \d\d:\d\d$/);
    expect(screen.queryByTestId("held-back-btc")).toBeNull();
    const line = screen.getByTestId("held-back-eth");
    expect(line).toHaveTextContent("ETH sells no new puts: negative gamma regime in a bear trend");
    expect(line.textContent).not.toContain(";"); // one sentence per distinct hold
    expect(line).toHaveClass("text-warn");
  });

  test("a bot not running normally is named", () => {
    const b = bot("btc", "BTC");
    b.status.halted = true;
    expect(statusLine([b, bot("eth", "ETH")])).toMatchObject({ tone: "bad", text: expect.stringMatching(/^BTC halted: kill switch · ETH trading · updated/) });
    expect(statusLine([])).toEqual({ tone: "warn", text: "No bot answered yet" });
  });
});

describe("lib/tiles and lib/band", () => {
  test("coin decimals: BTC 5, ETH 4, others 4", () => {
    expect([coinDigits("BTC"), coinDigits("ETH"), coinDigits("SOL")]).toEqual([5, 4, 4]);
    expect(fmtCoin(-0.0123, "ETH")).toBe("−0.0123 ETH");
  });

  test("lines and footers", () => {
    const b = bot("btc", "BTC");
    expect(earnedLines([{ ...b, summary: { ...b.summary, income: { this_month: { closes: 0 } } } }])[0].text).toBe("not yet: no close this month");
    expect(worstLines([b])[0]).toMatchObject({ value: -0.05, usd: -4935, note: "−4.2% of account", tone: "loss" });
    expect(usdFooter([{ coin: "BTC", unknown: "unknown: x" }])).toBe("≈ USD unknown");
    expect(marginLines([{ name: "x", status: {} }])[0].unknown).toBe("unknown: no margin reading yet");
    expect(roomFooter([{ coin: "BTC", unknown: "u" }])).toBe("room left: BTC unknown of limit");
  });

  test("held sentences and a freeze", () => {
    expect(heldSentence("BTC", { missing: "call", rule: "gex_shed" })).toBe("BTC sells no new calls: the price is past the gamma flip (GEX shed)");
    expect(heldSentence("BTC", { missing: "put", rule: "weird", reason: "a reason" })).toBe("BTC holds back the put: a reason");
    const frozen = bot("btc", "BTC");
    frozen.status.risk.status = { frozen: true, freeze_reason: "DVOL moved band" };
    expect(heldBackLines([frozen])).toEqual([{ bot: "btc", text: "BTC opens nothing new: the expected swing changed and must hold for a few days" }]);
  });

  test("soldStrikes groups one strike's strangles; the corridor needs two prices", () => {
    const p = payoff([strangle("a"), strangle("b", { dte: 55.2, legs: [leg("put", 85000)] })]);
    expect(soldStrikes(p)).toEqual([
      { side: "put", price: 85000, label: "put 85,000 · 31/55 d" },
      { side: "call", price: 110000, label: "call 110,000 · 31 d" },
    ]);
    expect(strikeCorridor(soldStrikes(p))).toEqual({ low: 85000, high: 110000 });
    expect(strikeCorridor([{ price: 1 }])).toBeNull();
    expect(soldStrikes(null)).toEqual([]);
  });

  test("nudgeLabels pushes labels apart within the box, keeping their order", () => {
    const out = nudgeLabels([{ y: 50 }, { y: 52 }, { y: 51 }, { y: 200 }], 10, 0, 205);
    expect(out.map((l) => l.labelY)).toEqual([50, 70, 60, 200]);
    // Squeezed against the bottom: pushed back up.
    expect(nudgeLabels([{ y: 99 }, { y: 100 }], 10, 0, 100).map((l) => l.labelY)).toEqual([90, 100]);
  });

  test("rangeStrip: corridor, ticks, spot and distance; outside the corridor says past", () => {
    const r = rangeStrip(strangle("a"));
    expect(r.put).toBeLessThan(r.spot);
    expect(r.call).toBeGreaterThan(r.spot);
    expect(r.corridor.left).toBeLessThan(r.put);
    expect(r.inside).toBe(true);
    const out = rangeStrip(strangle("a", { spot: 83000 }));
    expect(out.inside).toBe(false);
    expect(out.distanceText).toBe("spot 1.2% past a break-even");
    expect(rangeStrip(strangle("a", { spot: 0 }))).toBeNull();
    const oneCall = rangeStrip(strangle("a", { legs: [leg("call", 110000)], break_even_low: null }));
    expect(oneCall.corridor.left).toBe(0);
    expect(oneCall.stubs).toEqual([{ side: "put", left: 0, width: 8 }]);
  });

  test("keptOf", () => {
    expect(keptOf(strangle("a"))).toEqual({ text: "62 % of the premium kept", bar: 62 });
    expect(keptOf(strangle("a", { earned_pct: -40 }))).toEqual({ text: "−40 % of the premium kept", bar: 0 });
    expect(keptOf(strangle("a", { earned_pct: null })).text).toMatch(/unknown/);
  });
});
