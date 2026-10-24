import notes, { MAX_NOTE, TEXT_FIELDS, noteFor } from "@/lib/confirmNotes";
import glossary, { normalise } from "@/lib/glossary";
import { markdownAnchors } from "@/test/glossarySources";

const actions = Object.keys(TEXT_FIELDS);
const texts = (action) => TEXT_FIELDS[action].map((f) => [f, notes[action][f]]);

// The same words the bot's plain-words catalogue refuses (tests/api_explain_test.go
// TestAPIExplain_NoAdvice) plus the glossary's return promises.
const DENY = [
  "should", "recommend", "advise", "suggest", "expect", "forecast", "predict", "guarantee",
  "will earn", "will make", "will gain", "will return", "consider", "you ", "your ", "best time", "opportunit",
];
const PROMISE = /guarantee|risk[- ]free|no risk|can(not|'t) lose|sure (profit|win|income)|always (profit|win|earn)|will earn|safe income/i;

describe("confirm notes (#299)", () => {
  test("the catalogue holds exactly the two actions and their fields", () => {
    expect(Object.keys(notes).sort()).toEqual([...actions].sort());
    for (const action of actions) {
      expect(Object.keys(notes[action]).sort()).toEqual([...TEXT_FIELDS[action], "terms", "source"].sort());
    }
  });

  test("every field is present, each at most 280 characters", () => {
    for (const action of actions) {
      for (const [field, text] of texts(action)) {
        expect([action, field, typeof text]).toEqual([action, field, "string"]);
        expect([action, field, text.trim().length > 0 && text.length <= MAX_NOTE]).toEqual([action, field, true]);
      }
    }
  });

  // Scenario: No figure and no promise.
  test("no figure and no promise: no digit with %, USD, BTC or ETH, no deny-listed word", () => {
    for (const action of actions) {
      for (const [field, text] of texts(action)) {
        expect([action, field, /\d\s*(%|USD|BTC|ETH)/i.test(text)]).toEqual([action, field, false]);
        // The notes never state a figure at all: settings are named, not quoted.
        expect([action, field, /\d/.test(text)]).toEqual([action, field, false]);
        const lower = ` ${text.toLowerCase()} `;
        expect([action, field, DENY.filter((w) => lower.includes(w))]).toEqual([action, field, []]);
        expect([action, field, PROMISE.test(text)]).toEqual([action, field, false]);
      }
    }
  });

  test("the deny-list catches a figure and a forecast", () => {
    expect(/\d\s*(%|USD|BTC|ETH)/i.test("costs 20 % of margin")).toBe(true);
    expect(/\d\s*(%|USD|BTC|ETH)/i.test("about 0.5 BTC")).toBe(true);
    expect(DENY.some((w) => " the price is expected to fall ".includes(w))).toBe(true);
  });

  // Scenario: Grounded wording.
  test("grounded wording: every term named is a glossary key and appears in the note", () => {
    for (const action of actions) {
      const all = normalise(texts(action).map(([, t]) => t).join(" "));
      expect(notes[action].terms.length).toBeGreaterThan(0);
      for (const term of notes[action].terms) {
        expect([action, term, Object.hasOwn(glossary, term)]).toEqual([action, term, true]);
        expect([action, term, all.includes(normalise(term))]).toEqual([action, term, true]);
      }
    }
  });

  test("grounded wording: every setting named in a note is a glossary key", () => {
    for (const action of actions) {
      for (const [field, text] of texts(action)) {
        for (const key of text.match(/\b[a-z]+(?:_[a-z]+)+\b/g) ?? []) {
          expect([action, field, key, Object.hasOwn(glossary, key)]).toEqual([action, field, key, true]);
          expect([action, field, key, notes[action].terms.includes(key)]).toEqual([action, field, key, true]);
        }
      }
    }
  });

  test("grounded wording: every source anchor exists in OptionStrategy.md or CLAUDE.md", () => {
    for (const action of actions) {
      const [file, anchor] = notes[action].source.split("#");
      expect([action, ["OptionStrategy.md", "CLAUDE.md"].includes(file)]).toEqual([action, true]);
      expect([action, markdownAnchors(file).has(anchor)]).toEqual([action, true]);
    }
  });

  test("the facts the rules state are in the notes", () => {
    const mc = notes.manual_close;
    expect(mc.does).toMatch(/at market/);
    expect(mc.does).toMatch(/regime side/);
    expect(mc.follows).toMatch(/repair_cooldown_hours/);
    const rb = notes.rebalance;
    expect(rb.does).toMatch(/IM limit/);
    expect(rb.does).toMatch(/sells more/);
    expect(rb.does).toMatch(/buys back/);
    expect(rb.risks).toMatch(/liquidity cap/);
    expect(rb.risks).toMatch(/max_leg_size_multiple/);
    expect(rb.follows).toMatch(/retried later/);
    expect(rb.dry_run).toMatch(/sends nothing/);
  });
});

describe("noteFor", () => {
  // Scenario: Last leg of a strangle.
  test("last leg of a strangle: the slot is re-entered by a fresh entry and the side is held from repair", () => {
    const n = noteFor("manual_close", { lastLeg: true });
    expect(n.follows).toBe(notes.manual_close.follows_last_leg);
    expect(n.follows).toMatch(/slot becomes empty/);
    expect(n.follows).toMatch(/fresh entry/);
    expect(n.follows).toMatch(/held from repair/);
    expect(n.does).toBe(notes.manual_close.does);
    expect(n.risks).toBe(notes.manual_close.risks);
  });

  test("a leg that is not the last: the side is held, the other leg stays", () => {
    const n = noteFor("manual_close");
    expect(n).toEqual({
      action: "manual_close",
      does: notes.manual_close.does,
      risks: notes.manual_close.risks,
      follows: notes.manual_close.follows,
      terms: notes.manual_close.terms,
      source: notes.manual_close.source,
    });
    expect(n.follows).toMatch(/stop hold/);
  });

  test("rebalance: the real pass, and the dry run that sends nothing", () => {
    expect(noteFor("rebalance").does).toBe(notes.rebalance.does);
    expect(noteFor("rebalance").follows).toBe(notes.rebalance.follows);
    const dry = noteFor("rebalance", { dryRun: true });
    expect(dry.does).toBe(notes.rebalance.dry_run);
    expect([dry.risks, dry.follows]).toEqual([null, null]);
  });

  test("flags of the other action are ignored", () => {
    expect(noteFor("rebalance", { lastLeg: true })).toEqual(noteFor("rebalance"));
    expect(noteFor("manual_close", { dryRun: true })).toEqual(noteFor("manual_close"));
  });

  test("unknown action → null, never a guess", () => {
    expect(noteFor("kill_switch")).toBeNull();
    expect(noteFor(undefined)).toBeNull();
    expect(noteFor("toString")).toBeNull();
  });
});
