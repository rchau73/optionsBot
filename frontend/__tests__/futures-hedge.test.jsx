import { fireEvent, render, screen } from "@testing-library/react";
import { FuturesHedgeDialog, FuturesHedgeView } from "@/components/FuturesHedge";

// The futures hedge experiment's card and switch (#657).

const hedge = (over = {}) => ({
  available: true,
  enabled: true,
  since: "2026-11-05T16:00:00Z",
  by: "roberto",
  kill_switch: false,
  halted: false,
  underlying: "BTC",
  instrument: "BTC-PERPETUAL",
  perp_usd: -17700,
  perp_delta_coin: -0.2305,
  options_delta_coin: 0.2305,
  net_delta_coin: 0,
  band_coin: 0.05785,
  orders_last_hour: 1,
  max_orders_per_hour: 12,
  shed_now: "",
  benchmark: { since: "2026-11-05T16:00:00Z", advantage_coin: -0.0012, advantage_usd: -92.1, hedge_pnl_coin: -0.0002, legs_coin: -0.001, real_trades: 1, virtual_trades: 0, legs: [], note: "An estimate." },
  ...over,
});

const preview = {
  underlying: "BTC",
  instrument: "BTC-PERPETUAL",
  perp_usd: -17700,
  unrealised_coin: -0.0004,
  unrealised_usd: -30.7,
  funding_coin: 0,
  close_fee_coin: 0.000115,
  close_fee_usd: 8.85,
  delta_now_coin: 0,
  delta_after_coin: 0.2305,
  im_pct_now: 18.3,
  im_pct_after: 19.9,
  mm_pct_now: 14.6,
  mm_pct_after: 15.9,
  limit_im_pct: 20,
  max_mm_pct: 35,
  scenarios: [
    { move_pct: -10, pnl_coin: -0.1003, pnl_usd: -6927 },
    { move_pct: -5, pnl_coin: -0.03, pnl_usd: -2400 },
    { move_pct: 5, pnl_coin: 0.012, pnl_usd: 900 },
    { move_pct: 10, pnl_coin: 0.01, pnl_usd: 800 },
  ],
  warnings: ["Without the hedge the options carry +0.2305 BTC of delta: a -10 % move would cost about 0.1003 BTC (6927 USD)."],
};

const reply = (status, body) => ({ ok: status < 300, status, json: async () => body });

describe("futures hedge card", () => {
  test("shows the mode, the book's delta and a loss in the benchmark with its sign", () => {
    render(<FuturesHedgeView bot="btc" data={{ hedge: hedge(), switchable: true }} onSwitch={jest.fn()} />);
    const card = screen.getByTestId("hedge-btc");
    expect(card).toHaveTextContent("ON");
    expect(card).toHaveTextContent("BTC-PERPETUAL holds the book's direction");
    expect(screen.getByTestId("hedge-benchmark-btc")).toHaveTextContent("−");
    expect(screen.getByTestId("hedge-benchmark-btc")).toHaveTextContent("The shed did better than the hedge.");
  });

  test("the switch is gray with its reason on a live bot or without futures", () => {
    const { rerender } = render(<FuturesHedgeView bot="btc" data={{ hedge: hedge(), switchable: false, switch_off_reason: "the futures hedge experiment is testnet-only" }} />);
    expect(screen.getByRole("button", { name: /Switch off/ })).toBeDisabled();
    expect(screen.getByTitle("the futures hedge experiment is testnet-only")).toBeInTheDocument();
    rerender(<FuturesHedgeView bot="btc" data={{ hedge: hedge({ available: false, enabled: false, unavailable: "this bot has no futures connection" }), switchable: true }} />);
    expect(screen.getByRole("button", { name: /Switch on/ })).toBeDisabled();
  });

  test("a halted hedge says why in plain words and that the shed is back, and the admin can still switch off", () => {
    const paused = hedge({
      halted: true,
      halt_reason: "Deribit is not taking orders (locked_by_admin), usually scheduled maintenance",
      not_holding: "Deribit is not taking orders (locked_by_admin), usually scheduled maintenance",
      recheck_at: "2026-11-05T17:10:00Z",
    });
    render(<FuturesHedgeView bot="btc" data={{ hedge: paused, switchable: true }} onSwitch={jest.fn()} />);
    const card = screen.getByTestId("hedge-btc");
    expect(card).toHaveTextContent("Paused");
    expect(card).toHaveTextContent("the GEX shed buys the threatened side back as usual");
    expect(card).not.toHaveTextContent("the GEX shed keeps its legs");
    expect(screen.getByTestId("hedge-halt-btc")).toHaveTextContent("usually scheduled maintenance");
    expect(screen.getByTestId("hedge-halt-btc")).toHaveTextContent("resumes by itself");
    expect(screen.getByRole("button", { name: /Switch off/ })).toBeEnabled();
  });

  test("a hedge that is not filling says why, without calling it paused", () => {
    render(<FuturesHedgeView bot="btc" data={{ hedge: hedge({ not_holding: "the perpetual has no price to trade at" }), switchable: true }} onSwitch={jest.fn()} />);
    expect(screen.getByTestId("hedge-btc")).toHaveTextContent("Not holding");
    expect(screen.getByTestId("hedge-halt-btc")).toHaveTextContent("Why: the perpetual has no price to trade at.");
  });

  test("a reason left over with the mode off shows nothing", () => {
    render(<FuturesHedgeView bot="btc" data={{ hedge: hedge({ enabled: false, not_holding: "stale" }), switchable: true }} onSwitch={jest.fn()} />);
    expect(screen.queryByTestId("hedge-halt-btc")).not.toBeInTheDocument();
  });

  test("the kill switch grays the switch, a paused hedge does not", () => {
    render(<FuturesHedgeView bot="btc" data={{ hedge: hedge({ kill_switch: true }), switchable: true }} />);
    expect(screen.getByRole("button", { name: /Switch off/ })).toBeDisabled();
    expect(screen.getByTitle("the kill switch has fired")).toBeInTheDocument();
  });

  test("says what the benchmark is priced at", () => {
    const b = { ...hedge().benchmark, pricing: "index", pricing_note: "benchmark priced at the index (testnet): fills and marks at the index price, funding left out" };
    render(<FuturesHedgeView bot="btc" data={{ hedge: hedge({ benchmark: b }), switchable: true }} />);
    expect(screen.getByTestId("hedge-benchmark-btc")).toHaveTextContent("enchmark priced at the index (testnet)");
  });

  test("opens the dialog for the other state", () => {
    const onSwitch = jest.fn();
    render(<FuturesHedgeView bot="btc" data={{ hedge: hedge({ enabled: false }), switchable: true }} onSwitch={onSwitch} />);
    fireEvent.click(screen.getByRole("button", { name: /Switch on/ }));
    expect(onSwitch).toHaveBeenCalledWith("on");
  });
});

describe("futures hedge switch", () => {
  const realFetch = global.fetch;
  afterEach(() => {
    global.fetch = realFetch;
  });

  test("switching off previews the close, the margin and the risk left, then closes only on confirmation", async () => {
    global.fetch = jest
      .fn()
      .mockResolvedValueOnce(reply(200, { hedge: hedge(), preview }))
      .mockResolvedValueOnce(reply(200, { enabled: false, closed_usd: 17700, detail: "futures hedge OFF by roberto" }));
    render(<FuturesHedgeDialog bot="btc" enable={false} hedge={hedge()} onClose={jest.fn()} />);

    const p = await screen.findByTestId("hedge-preview");
    expect(global.fetch).toHaveBeenCalledTimes(1);
    expect(global.fetch.mock.calls[0][0]).toBe("/api/bots/btc/hedge?preview=true");
    expect(p).toHaveTextContent("buys back $17,700 of BTC-PERPETUAL at market");
    expect(p).toHaveTextContent("18.3");
    expect(p).toHaveTextContent("19.9");
    expect(p).toHaveTextContent("−10 %");
    expect(p).toHaveTextContent("Without the hedge");

    fireEvent.click(screen.getByRole("button", { name: "Close the perpetual and switch off" }));
    await screen.findByText(/Done: futures hedge OFF by roberto/);
    expect(global.fetch).toHaveBeenLastCalledWith("/api/bots/btc/hedge", expect.objectContaining({ body: JSON.stringify({ enabled: false }) }));
  });

  test.each([
    ["the bot reports no preview", () => Promise.resolve(reply(200, { hedge: hedge(), preview_error: "exchange slow" })), "exchange slow"],
    ["the read fails", () => Promise.reject(new Error("network down")), "network down"],
  ])("when %s the admin can still switch off, warned", async (_, previewReply, why) => {
    global.fetch = jest
      .fn()
      .mockImplementationOnce(previewReply)
      .mockResolvedValueOnce(reply(200, { enabled: false, closed_usd: 17700, detail: "futures hedge OFF by roberto" }));
    render(<FuturesHedgeDialog bot="btc" enable={false} hedge={hedge()} onClose={jest.fn()} />);

    expect(await screen.findByText(/The preview is unavailable/)).toHaveTextContent(why);
    expect(screen.queryByTestId("hedge-preview")).not.toBeInTheDocument();
    fireEvent.click(screen.getByRole("button", { name: "Close the perpetual and switch off" }));
    await screen.findByText(/Done: futures hedge OFF by roberto/);
  });

  test("a refused switch says why and keeps the state", async () => {
    global.fetch = jest.fn().mockResolvedValueOnce(reply(409, { error: "the perpetual could not be closed: the experiment stays ON" }));
    render(<FuturesHedgeDialog bot="btc" enable hedge={hedge({ enabled: false })} onClose={jest.fn()} />);
    expect(screen.getByText(/the GEX shed no longer buys a side back/)).toBeInTheDocument();
    fireEvent.click(screen.getByRole("button", { name: "Switch on" }));
    await screen.findByText(/Not switched: the perpetual could not be closed/);
  });
});
