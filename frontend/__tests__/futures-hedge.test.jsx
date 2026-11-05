import { fireEvent, render, screen } from "@testing-library/react";
import { FuturesHedgeDialog, FuturesHedgeView } from "@/components/FuturesHedge";

// The futures hedge experiment's card and switch (#657).

const hedge = (over = {}) => ({
  available: true,
  enabled: true,
  since: "2026-11-05T16:00:00Z",
  by: "roberto",
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

  test("a refused switch says why and keeps the state", async () => {
    global.fetch = jest.fn().mockResolvedValueOnce(reply(409, { error: "the perpetual could not be closed: the experiment stays ON" }));
    render(<FuturesHedgeDialog bot="btc" enable hedge={hedge({ enabled: false })} onClose={jest.fn()} />);
    expect(screen.getByText(/the GEX shed no longer buys a side back/)).toBeInTheDocument();
    fireEvent.click(screen.getByRole("button", { name: "Switch on" }));
    await screen.findByText(/Not switched: the perpetual could not be closed/);
  });
});
