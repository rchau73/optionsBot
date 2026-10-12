import { render, screen, within } from "@testing-library/react";
import HeldLegs from "@/components/HeldLegs";
import { distanceToLevel, heldRows, holdRule, scriptBias } from "@/lib/held";
import { bot, status } from "@/test/fixtures";

// 2026-10-11: spot above the flip in a confirmed negative regime with a bear
// trend — the puts were held by the regime-side rule, not by a GEX anchor.
const regimeSide = {
  strangle_id: "st-1",
  slot: { dte: 30, delta: 0.16 },
  missing: "put",
  open_leg: "ETH-30OCT26-2800-C",
  rule: "regime_side",
  reason: "confirmed negative gamma regime in a bear trend: no new puts",
  trend: "bear",
  confirmed_negative: true,
  live_regime: "NEGATIVE/ACCELERATION",
  spot: 2545,
  live_flip: 2476,
  buffer_pct: 2.6,
  above_flip_level: 2540.4,
};
const shed = {
  ...regimeSide,
  strangle_id: "st-2",
  rule: "gex_shed",
  reason: "shed by GEX at flip 2697",
  anchor: { shed_at: "2026-10-06T23:02:00Z", flip: 2697, buffer_pct: 2.6, level: 2767, above: 0, need: 2, release_hours: 24 },
};
const withHeld = (held) => bot({ name: "eth", status: { ...status, held_legs: held } });

describe("held legs helpers", () => {
  test("distance to a release level", () => {
    expect(distanceToLevel(100, 102)).toBeCloseTo(2);
    expect(distanceToLevel(100, 98)).toBeCloseTo(-2);
    expect(distanceToLevel(100, 0)).toBeNull();
    expect(distanceToLevel(undefined, 100)).toBeNull();
  });

  test("script bias only in a negative regime", () => {
    expect(scriptBias("NEGATIVE/ACCELERATION", 2545, 2476)).toBe("up");
    expect(scriptBias("NEGATIVE/ACCELERATION", 2400, 2476)).toBe("down");
    expect(scriptBias("POSITIVE/PINNING", 2545, 2476)).toBeNull();
    expect(scriptBias("NEGATIVE/ACCELERATION", 2545, 0)).toBeNull();
  });

  test("rule labels, unknown rules shown as given", () => {
    expect(holdRule("regime_side").label).toBe("regime side");
    expect(holdRule("").label).toBe("repairing");
    expect(holdRule("new_rule").label).toBe("new_rule");
  });

  test("rows across bots, missing held_legs is no rows", () => {
    const rows = heldRows([withHeld([regimeSide, shed]), bot()]);
    expect(rows).toHaveLength(2);
    expect(rows[0].bias).toBe("up");
    expect(rows[0].toAboveFlipPct).toBeLessThan(0); // spot already above the live level
    expect(rows[1].toAnchorPct).toBeGreaterThan(0); // the anchor is higher
  });
});

describe("HeldLegs", () => {
  test("shows the rule, the regime and both release levels side by side", () => {
    render(<HeldLegs bots={[withHeld([regimeSide, shed])]} />);
    const row = screen.getByTestId("held-eth:st-1:put");
    expect(within(row).getByText("regime side")).toBeInTheDocument();
    expect(within(row).getByText(/no new puts/)).toBeInTheDocument();
    expect(within(row).getByText(/accel\. up/)).toBeInTheDocument();
    expect(within(row).getByText("not shed by GEX")).toBeInTheDocument();

    const anchored = screen.getByTestId("held-eth:st-2:put");
    expect(within(anchored).getByText("GEX anchor")).toBeInTheDocument();
    expect(within(anchored).getByText(/flip \$2,697 · 0\/2 snapshots · or 24 h non-negative/)).toBeInTheDocument();
  });

  test("empty state", () => {
    render(<HeldLegs bots={[bot()]} />);
    expect(screen.getByText("Every strangle has both legs.")).toBeInTheDocument();
  });
});
