import fs from "fs";
import path from "path";
import { fireEvent, render, screen, within } from "@testing-library/react";
import IdleNote from "@/components/IdleNote";
import PhoneOverview from "@/components/PhoneOverview";
import { IDLE_FALLBACK, IDLE_WORDS, idleLine } from "@/lib/idleWords";
import { longSentences, undefinedJargon } from "@/test/plainWords";

// #781 (epic E3): why the bot is not trading, one sentence per reason of
// /api/summary's idle note. The reasons are read from the Go source: every
// strategy.Skip* constant and api.IdleHalted.

jest.mock("@/components/RegimeChart", () => () => null);

const goDir = (...parts) => path.join(process.cwd(), "..", ...parts);
function goReasons() {
  const out = new Set();
  const dir = goDir("internal", "strategy");
  for (const f of fs.readdirSync(dir).filter((n) => n.endsWith(".go"))) {
    for (const m of fs.readFileSync(path.join(dir, f), "utf8").matchAll(/^\s*(?:const\s+)?Skip[A-Z]\w*\s*=\s*"(\w+)"/gm)) out.add(m[1]);
  }
  const halted = fs.readFileSync(goDir("internal", "api", "idle.go"), "utf8").match(/IdleHalted = "(\w+)"/);
  if (halted) out.add(halted[1]);
  return [...out].sort();
}

describe("Scenario: a skip reason that has no sentence in the map", () => {
  test("every journal skip reason and the kill switch have a sentence", () => {
    const reasons = goReasons();
    expect(reasons).toEqual(expect.arrayContaining(["risk_frozen", "gex_wait", "no_free_expiry", "repair_held", "regime_side", "entry_wait_topup", "halted"]));
    const missing = reasons.filter((r) => !IDLE_WORDS[r]);
    expect(missing).toEqual([]);
  });

  test("the map has no sentence for a reason the bot no longer journals", () => {
    expect(Object.keys(IDLE_WORDS).sort()).toEqual(goReasons());
  });

  test("an unknown reason falls back to the waiting sentence", () => {
    expect(idleLine({ reason: "something_new" })).toEqual({ text: IDLE_FALLBACK, act: null, blocked: false, known: false });
    expect(idleLine(null)).toBeNull();
  });
});

describe("the sentences are plain words", () => {
  test.each(Object.entries(IDLE_WORDS))("%s: short sentences, no undefined jargon, no promise", (reason, w) => {
    const all = `${w.text} ${w.act ?? ""}`;
    expect([reason, longSentences(all)]).toEqual([reason, []]);
    expect([reason, undefinedJargon(all, reason)]).toEqual([reason, []]);
    expect([reason, /will (resume|trade|restart)|soon|guarantee/i.test(all)]).toEqual([reason, false]);
  });

  test("only the kill switch asks a person to act", () => {
    expect(idleLine({ reason: "halted" })).toMatchObject({ blocked: true, act: expect.stringMatching(/^A person must restart/) });
    expect(idleLine({ reason: "gex_wait" }).act).toBe("You need to do nothing.");
  });
});

describe("Scenario: a phone 360 px wide", () => {
  const bot = {
    name: "btc",
    status: { underlying: "BTC", environment: "testnet" },
    summary: {
      underlying: "BTC",
      holding: { strangles: 0, legs: 0, coin: "BTC" },
      income: { coin: "BTC", months: [], this_month: { month: "2026-11", realised_coin: 0, realised_usd: 0, closes: 0 }, unrealised_coin: 0, unrealised_usd: 0 },
      worst_case: { available: true, down: { loss_coin: 0, loss_usd: 0 }, up: { loss_coin: 0, loss_usd: 0 } },
      last_action: { at: "2026-11-13T10:00:00Z", kind: "close", instrument: "BTC-27NOV26-110000-C", qty: 1, rule: "take_profit", reason: "Bought back the call." },
      idle: { reason: "risk_frozen", blocked: true, held: false, seq: 7, at: "2026-11-14T09:00:00Z", plain: "No new strangle: new risk is paused.", detail: "regime NEGATIVE (1/2 closes)" },
    },
  };

  test("the idle explanation wraps instead of scrolling and keeps a 44 px Why?", () => {
    window.innerWidth = 360;
    render(<PhoneOverview bots={[bot]} selected="btc" />);
    const card = screen.getByRole("region", { name: "What it holds" });
    const note = within(card).getByTestId("idle-note-btc");
    expect(within(note).getByText(/^Not opening new positions/)).toHaveClass("break-words");
    for (const el of note.querySelectorAll("*")) expect(el.className).not.toMatch(/\b(w-\[\d{3,}px\]|min-w-\[|whitespace-nowrap|overflow-x)/);
    const why = within(note).getByRole("button", { name: "Why?" });
    expect(why).toHaveClass("min-h-11", "min-w-11");
    expect(within(note).getByRole("link", { name: "All actions, explained →" })).toHaveClass("min-h-11");
    fireEvent.click(why);
    expect(within(note).getByTestId("idle-why")).toHaveClass("break-words");
  });

  test("a line with neither journal line nor numbers has no Why?, only Activity", () => {
    render(<IdleNote bot={{ name: "eth", summary: { idle: { reason: "risk_frozen", blocked: true, seq: 0 } } }} />);
    expect(screen.queryByRole("button", { name: "Why?" })).toBeNull();
    expect(screen.getByRole("link", { name: "All actions, explained →" })).toHaveAttribute("href", "/activity?bot=eth");
  });
});
