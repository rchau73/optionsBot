import { fireEvent, render, screen, waitFor } from "@testing-library/react";
import { RebalanceButton, RebalanceModal } from "@/components/Rebalance";

const row = (over = {}) => ({ bot: "eth", frozen: false, freezeReason: null, error: null, ...over });

const report = (over = {}) => ({
  dry_run: true,
  done: true,
  limit_im_pct: 20,
  im_pct: 6.5,
  share_per_slot: 91.6,
  margin_balance: 916.04,
  unit: "ETH",
  actions: [
    { strangle_id: "s1", slot: "50d/0.16Δ", action: "top_up", call: "ETH-27NOV26-3050-C", put: "ETH-27NOV26-1900-P", qty: 100, target_qty: 300, detail: "sell 100 of each leg" },
    { strangle_id: "s2", slot: "30d/0.16Δ", action: "skipped", qty: 0, target_qty: 0, detail: "inside the rollout window" },
  ],
  ...over,
});

const reply = (status, body) => ({ ok: status < 300, status, json: async () => body });

describe("rebalance button", () => {
  test("gray with the reason while frozen or without margin data", () => {
    const { rerender } = render(<RebalanceButton row={row({ frozen: true, freezeReason: "DVOL moved" })} onOpen={jest.fn()} />);
    expect(screen.getByRole("button", { name: "Rebalance" })).toBeDisabled();
    expect(screen.getByTitle(/new risk frozen: DVOL moved/)).toBeInTheDocument();
    rerender(<RebalanceButton row={row({ error: "timeout" })} onOpen={jest.fn()} />);
    expect(screen.getByRole("button", { name: "Rebalance" })).toBeDisabled();
  });

  test("opens the dialog when allowed", () => {
    const onOpen = jest.fn();
    render(<RebalanceButton row={row()} onOpen={onOpen} />);
    fireEvent.click(screen.getByRole("button", { name: "Rebalance" }));
    expect(onOpen).toHaveBeenCalledWith(row());
  });
});

describe("rebalance dialog", () => {
  const realFetch = global.fetch;
  afterEach(() => {
    global.fetch = realFetch;
  });

  test("previews with a dry run, then runs only on confirmation", async () => {
    global.fetch = jest
      .fn()
      .mockResolvedValueOnce(reply(200, report()))
      .mockResolvedValueOnce(reply(200, report({ dry_run: false })));
    render(<RebalanceModal row={row()} onClose={jest.fn()} />);

    await screen.findByText(/Would send 1 order/);
    expect(global.fetch).toHaveBeenCalledTimes(1);
    expect(global.fetch).toHaveBeenCalledWith("/api/bots/eth/rebalance", expect.objectContaining({ body: JSON.stringify({ dry_run: true }) }));
    expect(screen.getByTestId("rebalance-actions")).toHaveTextContent("ETH-27NOV26-3050-C + ETH-27NOV26-1900-P × 100");

    fireEvent.click(screen.getByRole("button", { name: "Run rebalance" }));
    await screen.findByText(/Done\./);
    expect(global.fetch).toHaveBeenLastCalledWith("/api/bots/eth/rebalance", expect.objectContaining({ body: JSON.stringify({ dry_run: false }) }));
  });

  test("nothing to send: the run button stays disabled", async () => {
    global.fetch = jest.fn().mockResolvedValue(reply(200, report({ actions: [] })));
    render(<RebalanceModal row={row()} onClose={jest.fn()} />);
    await screen.findByText(/Nothing to send/);
    expect(screen.getByRole("button", { name: "Run rebalance" })).toBeDisabled();
  });

  test("shows the bot's refusal", async () => {
    global.fetch = jest.fn().mockResolvedValue(reply(409, { error: "rebalance refused: new risk frozen: DVOL moved" }));
    render(<RebalanceModal row={row()} onClose={jest.fn()} />);
    await waitFor(() => expect(screen.getByText(/Preview refused: rebalance refused: new risk frozen/)).toBeInTheDocument());
    expect(screen.queryByRole("button", { name: "Run rebalance" })).not.toBeInTheDocument();
  });
});
