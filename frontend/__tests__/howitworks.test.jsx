import { render, screen, within } from "@testing-library/react";
import HowItWorks from "@/components/HowItWorks";
import { HowItWorksScreen } from "@/components/HowItWorksView";
import { BotSummary } from "@/components/InvestorSummary";
import { breakEvens, cooldownWords, exampleLabel, payoffAt, payoffPoints, strangleFigures } from "@/lib/howitworks";
import { findTerm } from "@/lib/glossary";

// The shape GET /api/summary serves (internal/api/summary.go + howitworks.go, #322).
const fixture = (overrides = {}) => ({
  as_of: "2026-10-27T12:00:00Z",
  underlying: "BTC",
  environment: "testnet",
  spot: 101234,
  stop_loss_multiple: 2,
  delta_exit_threshold: 0.3,
  repair_cooldown_hours: 72,
  holding: { strangles: 1, legs: 2, calls: 1, puts: 1, coin: "BTC" },
  income: { coin: "BTC", months: [] },
  worst_case: { available: false, reason: "x", scenarios: [] },
  last_action: null,
  strangle: {
    strangle_id: "st-1",
    expiry: "2026-11-27T08:00:00Z",
    spot: 101234,
    put: { instrument: "BTC-27NOV26-87000-P", strike: 87000, qty: 1, premium_coin: 0.0115, exit_line: 92750 },
    call: { instrument: "BTC-27NOV26-117000-C", strike: 117000, qty: 1, premium_coin: 0.0085, exit_line: 107300 },
    premium_coin: 0.02,
    break_even_low: 85294.12,
    break_even_high: 119387.76,
    exit_lines: true,
    exit_lines_model: "model: …",
  },
  ...overrides,
});

const picture = (name) => screen.getByRole("region", { name });

describe("Scenario: live numbers", () => {
  test("the three pictures use that strangle's strikes, premiums, expiry and today's price", () => {
    render(<HowItWorks summary={fixture()} />);
    expect(screen.getByRole("heading", { level: 1 })).toHaveTextContent("How your BTC earns, in three pictures");
    expect(screen.queryByText(/^Example:/)).toBeNull();

    const legs = picture("One position, two legs");
    expect(legs).toHaveTextContent("Leg 1 · the floor$87,000");
    expect(legs).toHaveTextContent("it promises to buy BTC at $87,000 if BTC is below that on 2026-11-27");
    expect(legs).toHaveTextContent("paid now +0.0115 BTC");
    expect(legs).toHaveTextContent("Leg 2 · the ceiling$117,000");
    expect(legs).toHaveTextContent("it promises to sell BTC at $117,000 if BTC is above that on 2026-11-27");
    expect(legs).toHaveTextContent("paid now +0.0085 BTC");
    expect(legs).toHaveTextContent("BTC today $101,234");
    expect(legs).toHaveTextContent("+0.0200 BTC paid today");

    const payoff = picture("What you end with on 2026-11-27, for each BTC price");
    expect(payoff).toHaveTextContent("Break-evens at expiry: $85,294 and $119,388");
    expect(payoff).toHaveTextContent("doesn't wait until 2026-11-27");

    const def = picture("How the bot defends you before the loss zone");
    expect(def).toHaveTextContent("Exit lines (delta 0.30): $92,750 on the floor's side and $107,300 on the ceiling's side");
  });

  test("the stop multiple, cooldown and delta exit come from the config", () => {
    render(<HowItWorks summary={fixture({ stop_loss_multiple: 3, repair_cooldown_hours: 48, delta_exit_threshold: 0.35 })} />);
    const def = picture("How the bot defends you before the loss zone");
    expect(def).toHaveTextContent("A leg that now costs 3 times what it was paid is bought back at once");
    expect(def).toHaveTextContent("it waits (2 days after a stop)");
    expect(def).toHaveTextContent("Exit lines (delta 0.35)");
    expect(screen.getByText(/The exit line is a delta of 0\.35\./)).toBeInTheDocument();
  });

  test("twice and 3 days for the shipped config", () => {
    render(<HowItWorks summary={fixture()} />);
    const def = picture("How the bot defends you before the loss zone");
    expect(def).toHaveTextContent("costs twice what it was paid");
    expect(def).toHaveTextContent("it waits (3 days after a stop)");
    for (const card of ["Calm · take profit", "Near a side · exit line", "Sudden jump · stop-loss", "After · wait, then repair"]) {
      expect(within(def).getByRole("heading", { name: new RegExp(card) })).toBeInTheDocument();
    }
  });

  test("exit lines not computable: the band without prices, and why", () => {
    const s = fixture();
    s.strangle = { ...s.strangle, exit_lines: false, exit_lines_reason: "no implied volatility" };
    s.strangle.put = { ...s.strangle.put, exit_line: null };
    s.strangle.call = { ...s.strangle.call, exit_line: null };
    render(<HowItWorks summary={s} />);
    const def = picture("How the bot defends you before the loss zone");
    expect(def).toHaveTextContent("Exit lines move with the market (unknown: no implied volatility)");
    expect(def).not.toHaveTextContent("$92,750");
    expect(within(def).getByRole("img")).toHaveAttribute("aria-label", expect.stringContaining("Exit lines move with the market."));
  });
});

describe("Scenario: nothing held", () => {
  const noZeros = (el) => expect(el).not.toHaveTextContent(/\$0\b|[+−]?0\.0000 BTC|NaN|—/);

  test("the page shows a labelled example, never zeros", () => {
    const s = fixture({ holding: { strangles: 0, legs: 0, calls: 0, puts: 0, coin: "BTC" }, strangle: null });
    const { container } = render(<HowItWorks summary={s} />);
    expect(screen.getByText("Example: a BTC strangle like the ones the bot sells.")).toBeInTheDocument();
    expect(screen.getByText(/The bot holds nothing right now/)).toBeInTheDocument();
    expect(picture("One position, two legs")).toHaveTextContent("$88,000");
    noZeros(container);
  });

  test("an ETH bot gets an ETH example; a one-legged strangle says why", () => {
    const s = fixture({ underlying: "ETH", holding: { strangles: 1, legs: 1, coin: "ETH" }, strangle: null });
    const { container } = render(<HowItWorks summary={s} />);
    expect(screen.getByText("Example: an ETH strangle like the ones the bot sells.")).toBeInTheDocument();
    expect(screen.getByText(/one leg only/)).toBeInTheDocument();
    expect(screen.getByRole("heading", { level: 1 })).toHaveTextContent("How your ETH earns");
    noZeros(container);
  });

  test("an older bot without the config values still reads", () => {
    const s = fixture({ stop_loss_multiple: undefined, repair_cooldown_hours: undefined, delta_exit_threshold: undefined, strangle: null });
    const { container } = render(<HowItWorks summary={s} />);
    expect(container).toHaveTextContent("reaches its stop-loss limit");
    expect(container).toHaveTextContent("a set time after a stop");
    expect(container).not.toHaveTextContent("undefined");
  });
});

describe("Scenario: accessible pictures", () => {
  test("each picture has a text alternative", () => {
    render(<HowItWorks summary={fixture()} />);
    const payoff = within(picture("What you end with on 2026-11-27, for each BTC price")).getByRole("img");
    expect(payoff).toHaveAttribute(
      "aria-label",
      "What the BTC strangle pays at expiry, for each BTC price: between $87,000 and $117,000 you keep all +0.0200 BTC; " +
        "it falls to zero at $85,294 and $119,388 (the break-evens); beyond them the loss grows, with no fixed limit. Today BTC is $101,234.",
    );
    const line = within(picture("How the bot defends you before the loss zone")).getByRole("img");
    expect(line.getAttribute("aria-label")).toMatch(/floor \$87,000, ceiling \$117,000, today \$101,234\. Exit lines \(delta 0\.30\) at \$92,750 .* \$107,300/);
    // Picture 1 is text: strikes and payments are read as they are.
    expect(picture("One position, two legs")).toHaveTextContent("Leg 1 · the floor");
  });

  test("the loss zone is marked by a pattern, not colour alone", () => {
    const { container } = render(<HowItWorks summary={fixture()} />);
    const losses = container.querySelectorAll('[data-zone="loss"]');
    expect(losses.length).toBe(3); // the payoff's loss area and both sides of the price line
    for (const el of losses) expect(el.getAttribute("fill")).toMatch(/^url\(#.+-hatch\)$/);
    const earn = container.querySelector('[data-zone="earn"]');
    expect(earn.getAttribute("fill")).toBeNull();
    expect(screen.getByText(/you lose \(hatched\)/)).toBeInTheDocument();
    expect(screen.getAllByText(/loss grows/).length).toBeGreaterThanOrEqual(2);
  });
});

describe("the glossary", () => {
  test("six cards from the monitor's glossary (one source of words)", () => {
    render(<HowItWorks summary={fixture()} />);
    const words = screen.getByRole("region", { name: "Words you'll see in Details (pro)" });
    const terms = within(words).getAllByRole("term").map((t) => t.textContent);
    expect(terms).toEqual(["leg", "strike", "premium", "delta", "expiry", "margin"]);
    for (const t of terms) expect(words).toHaveTextContent(findTerm(t).short);
    expect(words).toHaveTextContent("The exit line is a delta of 0.30.");
  });
});

describe("the page shell", () => {
  test("views nav, testnet badge and the footer; no video button", () => {
    render(<HowItWorksScreen bot="btc" summary={fixture()} />);
    expect(screen.getByRole("link", { name: "← Overview" })).toHaveAttribute("href", "/?bot=btc");
    expect(within(screen.getByRole("navigation", { name: "Views" })).getByText("How it works")).toHaveAttribute("aria-current", "page");
    expect(screen.getByText("TESTNET")).toBeInTheDocument();
    expect(screen.getByText(/not investment advice/)).toBeInTheDocument();
    expect(screen.queryByText(/video/i)).toBeNull();
  });

  test("loading and failure say so, never zeros", () => {
    const { rerender } = render(<HowItWorksScreen bot="btc" summary={null} />);
    expect(screen.getByText(/Not yet: loading/)).toBeInTheDocument();
    rerender(<HowItWorksScreen bot="btc" summary={null} error="bot btc is unreachable" />);
    expect(screen.getByRole("alert")).toHaveTextContent("Unknown: bot btc is unreachable");
  });

  test("a failed poll keeps the last figures and says they are not updating", () => {
    render(<HowItWorksScreen bot="btc" summary={fixture()} error="timeout" />);
    expect(screen.getByRole("alert")).toHaveTextContent("Not updating: timeout");
    expect(picture("One position, two legs")).toHaveTextContent("$87,000");
  });

  test("no bot chosen: links to each bot's page", () => {
    render(<HowItWorksScreen bot={null} names={["btc", "eth"]} summary={null} />);
    expect(screen.getByRole("link", { name: "ETH" })).toHaveAttribute("href", "/how-it-works?bot=eth");
  });

  test("the summary block's card links to this page, held or not", () => {
    const { rerender } = render(<BotSummary bot={{ name: "btc", summary: fixture() }} />);
    expect(screen.getByRole("link", { name: /How it works/ })).toHaveAttribute("href", "/how-it-works?bot=btc");
    rerender(<BotSummary bot={{ name: "eth", summary: fixture({ holding: { legs: 0 } }) }} />);
    expect(screen.getByRole("link", { name: /How it works/ })).toHaveAttribute("href", "/how-it-works?bot=eth");
  });
});

describe("lib/howitworks", () => {
  test("break-evens: the coin-settled payoff there is zero", () => {
    const put = { strike: 95000, qty: 1 };
    const call = { strike: 105000, qty: 1 };
    const { low, high } = breakEvens(put, call, 0.03);
    const fig = { put, call, premium: 0.03 };
    expect(Math.abs(payoffAt(fig, low))).toBeLessThan(1e-12);
    expect(Math.abs(payoffAt(fig, high))).toBeLessThan(1e-12);
    expect(payoffAt(fig, 100000)).toBeCloseTo(0.03, 12);
    expect(breakEvens(put, { strike: 105000, qty: 0.02 }, 0.03).high).toBeNull();
  });

  test("the example's figures are consistent: exit lines between the strikes and today", () => {
    for (const coin of ["BTC", "ETH"]) {
      const f = strangleFigures({ underlying: coin, holding: { legs: 0 }, strangle: null });
      expect(f.live).toBe(false);
      expect(f.put.strike).toBeLessThan(f.put.exit);
      expect(f.put.exit).toBeLessThan(f.spot);
      expect(f.spot).toBeLessThan(f.call.exit);
      expect(f.call.exit).toBeLessThan(f.call.strike);
      expect(f.low).toBeLessThan(f.put.strike);
      expect(f.high).toBeGreaterThan(f.call.strike);
    }
    expect(strangleFigures(null).note).toMatch(/No answer from the bot yet/);
    expect(strangleFigures({ underlying: "SOL", strangle: null }).coin).toBe("BTC");
  });

  test("payoff points span both break-evens and dip below zero on each side", () => {
    const f = strangleFigures(fixture());
    const pts = payoffPoints(f);
    expect(pts[0].s).toBeLessThan(f.low);
    expect(pts[pts.length - 1].s).toBeGreaterThan(f.high);
    expect(pts[0].y).toBeLessThan(0);
    expect(pts[pts.length - 1].y).toBeLessThan(0);
    expect(Math.max(...pts.map((p) => p.y))).toBeCloseTo(0.02, 12);
  });

  test("cooldownWords and exampleLabel", () => {
    expect(cooldownWords(72)).toBe("3 days");
    expect(cooldownWords(24)).toBe("1 day");
    expect(cooldownWords(36)).toBe("36 hours");
    expect(cooldownWords(0)).toBeNull();
    expect(cooldownWords(undefined)).toBeNull();
    expect(exampleLabel("BTC")).toBe("Example: a BTC strangle like the ones the bot sells");
    expect(exampleLabel("ETH")).toBe("Example: an ETH strangle like the ones the bot sells");
  });
});
