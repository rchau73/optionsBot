import { fireEvent, render, screen, waitFor, within } from "@testing-library/react";
import PositionsTable from "@/components/PositionsTable";
import { consolidateRows, groupRows, legRows, mergeManualClose } from "@/lib/monitor";
import { bot, positions, status } from "@/test/fixtures";

// The bot says per leg whether it may be closed by hand (regime-side block only).
const blocked = { allowed: false, reason: "manual close is allowed only while the regime-side block is on: the confirmed gamma regime is not negative" };
const allowedPut = (over = {}) => ({
  allowed: true,
  reason: "confirmed negative gamma regime in a bear trend: no new puts",
  after: "repair is held as after a stop-out",
  rebuilt: false,
  ...over,
});

function botWith(callMC, putMC) {
  const [call, put] = positions.strangles[0].legs;
  return bot({
    status: { ...status, trend: "bear", flip_buffer_pct: 2, market: { ...status.market, gamma_flip: 101000, spot_to_flip_pct: -1 }, risk: { status: { regime_negative: true, regime_known: true } } },
    positions: { ...positions, strangles: [{ ...positions.strangles[0], legs: [{ ...call, manual_close: callMC }, { ...put, bid: 0.012, ask: 0.014, manual_close: putMC }] }] },
  });
}

const renderTable = (b) => render(<PositionsTable groups={groupRows(consolidateRows(legRows([b])), "slot")} statuses={{ btc: b.status }} />);

describe("manual close", () => {
  const realFetch = global.fetch;
  afterEach(() => {
    global.fetch = realFetch;
  });

  // #405 Scenario: given a leg the manual-close rule does not allow, then no Close button is rendered.
  test("no Close button where the rule does not allow it; Close… outlined red with the bot's reason where it does", () => {
    renderTable(botWith(blocked, allowedPut()));
    expect(screen.queryByTestId("close-btc:45d · 0.16Δ:BTC-27DEC26-115000-C:sell")).toBeNull();
    const callRow = screen.getByTestId("leg-btc:45d · 0.16Δ:BTC-27DEC26-115000-C:sell");
    expect(within(callRow).queryByRole("button", { name: /Close/ })).toBeNull();
    const put = screen.getByTestId("close-btc:45d · 0.16Δ:BTC-27DEC26-88000-P:sell");
    expect(put).toHaveAccessibleName("Close…");
    expect(put).toBeEnabled();
    expect(put).toHaveClass("border-loss", "text-loss");
    expect(put).toHaveAttribute("title", `Close at market: ${allowedPut().reason}`);
  });

  test("no Close button on a bot that does not report the rule", () => {
    renderTable(botWith(undefined, undefined));
    expect(screen.queryAllByRole("button", { name: /Close/ })).toEqual([]);
  });

  test("confirmation shows the regime, GEX and what follows; confirm closes", async () => {
    const results = [{ position_id: "p-2", instrument: "BTC-27DEC26-88000-P", qty: 0.1, filled: 0.1 }];
    global.fetch = jest.fn().mockResolvedValue({ ok: true, status: 200, json: async () => ({ results }) });
    renderTable(botWith(blocked, allowedPut()));
    fireEvent.click(screen.getByTestId("close-btc:45d · 0.16Δ:BTC-27DEC26-88000-P:sell"));

    const dialog = screen.getByRole("dialog");
    expect(dialog).toHaveTextContent("Close BTC-27DEC26-88000-P × 0.1 at market?");
    expect(dialog).toHaveTextContent("confirmed NEGATIVE · live GEX POSITIVE/PINNING");
    expect(dialog).toHaveTextContent("$101,000 (spot -1.0%) · ±1σ $98,980–$103,020");
    expect(dialog).toHaveTextContent("Allowed: confirmed negative gamma regime in a bear trend: no new puts");
    expect(screen.getByTestId("close-after")).not.toHaveClass("text-loss");

    fireEvent.click(within(dialog).getByText("Buy back at market"));
    await waitFor(() => expect(screen.getByTestId("close-results")).toHaveTextContent("bought back 0.1 of 0.1"));
    expect(global.fetch).toHaveBeenCalledWith("/api/bots/btc/close", expect.objectContaining({ method: "POST", body: JSON.stringify({ position_ids: ["p-2"] }) }));
  });

  test("flagged red when the close would be rebuilt", () => {
    renderTable(botWith(blocked, allowedPut({ rebuilt: true, after: "this is the strangle's last leg: its slot becomes vacant" })));
    fireEvent.click(screen.getByTestId("close-btc:45d · 0.16Δ:BTC-27DEC26-88000-P:sell"));
    const after = screen.getByTestId("close-after");
    expect(after).toHaveClass("text-loss");
    expect(after).toHaveTextContent("Would be rebuilt: this is the strangle's last leg");
  });

  test("a merged row is closeable only if every position is", () => {
    expect(mergeManualClose(allowedPut(), blocked)).toBe(blocked);
    expect(mergeManualClose(allowedPut(), allowedPut({ rebuilt: true })).rebuilt).toBe(true);
    expect(mergeManualClose(null, allowedPut())).toBeNull();
    const [row] = consolidateRows([
      { bot: "btc", slot: "s", instrument: "I", side: "sell", qty: 1, entry: 1, positionIds: ["a"], manualClose: allowedPut() },
      { bot: "btc", slot: "s", instrument: "I", side: "sell", qty: 1, entry: 1, positionIds: ["b"], manualClose: allowedPut() },
    ]);
    expect(row.positionIds).toEqual(["a", "b"]);
    expect(row.manualClose.allowed).toBe(true);
  });
});
