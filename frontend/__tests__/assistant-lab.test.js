/**
 * @jest-environment node
 */
import fs from "node:fs";
import os from "node:os";
import path from "node:path";
import { POST as ask } from "@/app/api/assistant/route";
import { goldenInputs } from "@/lib/assistant/goldenrun";
import { check } from "@/lib/assistant/guard";
import { readAnswers } from "@/lib/assistant/log";
import { REFUSAL } from "@/lib/assistant/phrases";
import { labRuns, setAssistantModel } from "@/lib/assistant/server";
import { LAB_RUNS, asksAboutLab, labPassages } from "@/lib/assistant/service";
import { L10_ANALYSIS_GATE_OPEN } from "@/lib/lab";
import { signedIn, useTestAuth } from "@/test/auth";

// The assistant answers "what did my last comparison show?" from the lab's
// run list (#742): one lab:<run id>/headline passage per latest run, one
// lab:<request id>/status per request not run yet, lab:none without any —
// for managers and admins only, percentages only, the analysis never quoted.
// The fixtures are the golden set's lab cases: invented figures. A fake
// model: no test calls the model API.

const golden = fs
  .readFileSync(path.join(process.cwd(), "..", "docs", "product", "assistant", "golden.jsonl"), "utf8")
  .split("\n")
  .filter((l) => l.trim())
  .map((l) => JSON.parse(l));
const byId = Object.fromEntries(golden.map((q) => [q.id, q]));
const LAB_IDS = ["lab-analysis-withheld", "lab-last-run", "lab-no-run", "lab-run-failed"];
const [withheld, lastRun, noRun, failed] = LAB_IDS.map((id) => byId[id]);
const RUN = "20261108T090000Z";
const REQUEST = "20261109T100000Z-1a2b3c4d";
const ADVICE = byId["adv-apply-lab-change"].question;

/** A report.json whose run row is the golden lab-last-run fixture's. */
const report = {
  underlying: "BTC",
  diffs: [{ key: "stop_loss_multiplier", base: "2", proposed: "2.5" }],
  backtest: {
    ran: true,
    equity_usd: 100000,
    base: { return_pct: 1.2, worst_week_pct: -0.8, max_drawdown_pct: 1.5, fees_usd: 300, net_usd: 1200 },
    proposed: { return_pct: 1.8, worst_week_pct: -1.1, max_drawdown_pct: 2.1, fees_usd: 400, net_usd: 1800 },
  },
  replay: { ran: false },
  stress: { ran: false },
  neighbours: { cliffs: [] },
};

let root, dir, file, logs;
beforeEach(() => {
  root = fs.mkdtempSync(path.join(os.tmpdir(), "assistant-lab-"));
  dir = path.join(root, "compare");
  fs.mkdirSync(dir);
  file = path.join(root, "assistant.jsonl");
  process.env.COMPARE_DIR = dir;
  process.env.ASSISTANT_LOG_PATH = file;
  logs = [];
  jest.spyOn(console, "error").mockImplementation((l) => logs.push(l));
  jest.spyOn(console, "log").mockImplementation((l) => logs.push(l));
  useTestAuth();
});
afterEach(() => {
  setAssistantModel(undefined);
  jest.restoreAllMocks();
  fs.rmSync(root, { recursive: true, force: true });
  delete process.env.COMPARE_DIR;
  delete process.env.ASSISTANT_LOG_PATH;
});

const putRun = (id, content = report) => {
  fs.mkdirSync(path.join(dir, id), { recursive: true });
  fs.writeFileSync(path.join(dir, id, "report.json"), JSON.stringify(content));
};
const putRequest = (r) => {
  fs.mkdirSync(path.join(dir, "requests"), { recursive: true });
  fs.writeFileSync(path.join(dir, "requests", `${r.id}.json`), JSON.stringify(r));
};

/** A model that always replies `reply` and records what it was sent. */
function fakeModel(reply) {
  const m = { model: "fake", calls: [], ask: async (args) => (m.calls.push(args), structuredClone(reply)) };
  return m;
}

/** POST /api/assistant as `role`, with no bot: the answer, what the model was sent and the review log's line. */
async function askAs(role, question, reply) {
  const model = fakeModel(reply);
  setAssistantModel(model);
  const { cookie } = await signedIn(`${role}1`, role);
  const res = await ask(
    new Request("http://monitor/api/assistant", {
      method: "POST",
      headers: { origin: "http://monitor", host: "monitor", "content-type": "application/json", cookie },
      body: JSON.stringify({ question }),
    }),
  );
  const sent = model.calls[0]?.passages ?? [];
  return { body: await res.json(), sent, ids: sent.map((p) => p.id), line: [...readAnswers(file).values()].at(-1) };
}

describe("the lab passages", () => {
  test("a finished run: what changed, return, worst week, drawdown and fees for current and proposed, as percentages, labelled backtested", () => {
    const [p] = labPassages(lastRun.lab);
    expect(p).toMatchObject({ id: `lab:${RUN}/headline`, source: `lab:${RUN}` });
    expect(p.text).toBe(
      `Comparison run ${RUN} (finished 8 Nov 09:00 UTC), the newest comparison listed, BTC: ` +
        "backtested, not a forecast: simulated on past data, not a promise of future results. " +
        "Settings changed: stop_loss_multiplier from 2 to 2.5. " +
        "Backtest on past prices: current settings: return +1.2%, worst week −0.8%, max drawdown 1.5%, fees 0.30% of the starting equity. " +
        "proposed settings: return +1.8%, worst week −1.1%, max drawdown 2.1%, fees 0.40% of the starting equity. " +
        "Replay of recorded order books: did not run. Stress test: did not run. Fragile spots found near the proposed values: 0. " +
        "The worst week and the max drawdown are the risk beside each return. The written analysis is not available yet.",
    );
  });

  test("built from the run list as /api/lab/runs reads it: the same passage as the golden fixture", () => {
    putRun(RUN);
    expect(labRuns({ role: "manager" })).toEqual(labPassages(lastRun.lab));
  });

  test("a config that did not complete, windows and a missing figure", () => {
    const row = {
      ...lastRun.lab.runs[0],
      backtest: { base: lastRun.lab.runs[0].backtest.base, proposed: null },
      replay: { base: { return_pct: 0.5, worst_week_pct: null, max_drawdown_pct: 1, fees_pct: 0.2 }, proposed: null },
      windows: { backtest: { run: 5, proposed_wins: 3, base_wins: 2 }, replay: { run: 0, proposed_wins: 0, base_wins: 0 } },
      cliffs: null,
    };
    const { text } = labPassages({ runs: [row] })[0];
    expect(text).toContain("proposed settings: no figures: this run did not complete.");
    expect(text).toContain("current settings: return +0.5%, worst week unknown, max drawdown 1.0%, fees 0.20% of the starting equity.");
    expect(text).toContain("The proposed settings did better in 3 of 5 backtest windows.");
    expect(text).not.toContain("Fragile spots");
  });

  test.each([
    ["queued", {}, "queued: waiting for the runner"],
    ["running", {}, "running now"],
    ["failed", { reason: "the backtest found no recorded data for the week" }, "failed: the backtest found no recorded data for the week"],
    ["failed", { reason: null }, "failed: no reason recorded"],
    ["expired", {}, "expired: it did not run within a day of the request"],
  ])("a request %s is answered with its status and reason, no figure", (status, patch, want) => {
    const r = { ...failed.lab.requests[0], status, ...patch };
    const [p] = labPassages({ requests: [r] });
    expect(p).toMatchObject({ id: `lab:${REQUEST}/status`, source: `lab:${REQUEST}` });
    expect(p.text).toBe(`Comparison request ${REQUEST} (asked 9 Nov 10:00 UTC), the newest comparison listed, BTC, settings min_premium to 0.002: ${want}. It has no figures.`);
  });

  test("the newest of runs and requests says so, once; at most LAB_RUNS runs", () => {
    const ps = labPassages(failed.lab);
    expect(ps.map((p) => p.id)).toEqual(["lab:20261101T090000Z/headline", `lab:${REQUEST}/status`]);
    expect(ps.filter((p) => p.text.includes("the newest comparison listed")).map((p) => p.id)).toEqual([`lab:${REQUEST}/status`]);
    const runs = ["20261105T000000Z", "20261104T000000Z", "20261103T000000Z", "20261102T000000Z"].map((id) => ({ ...lastRun.lab.runs[0], id, time: `${id.slice(0, 4)}-${id.slice(4, 6)}-${id.slice(6, 8)}T00:00:00Z` }));
    expect(labPassages({ runs })).toHaveLength(LAB_RUNS);
  });

  test("no amount reaches a passage: the report's USD figures stay out", () => {
    putRun(RUN);
    const text = labRuns({ role: "admin" }).map((p) => p.text).join(" ");
    for (const amount of ["100000", "100,000", "1200", "1800", "300", "400", "$"]) expect(text).not.toContain(amount);
  });
});

describe("Scenario: a manager asks about the last run", () => {
  test("the answer gives the figures for current and proposed as percentages, says backtested and cites lab:<run id>", async () => {
    putRun(RUN);
    const { body, ids } = await askAs("manager", lastRun.question, lastRun.lab.answer);
    expect(ids).toContain(`lab:${RUN}/headline`);
    expect(body).toMatchObject({ outcome: "answered", answer: lastRun.lab.answer.text });
    for (const s of ["backtested", "+1.2%", "+1.8%", "−0.8%", "−1.1%", "1.5%", "2.1%", "0.30%", "0.40%"]) expect(body.answer).toContain(s);
    expect(body.citations.map((c) => c.id)).toEqual([`lab:${RUN}/headline`]);
  });

  test("an admin gets the same passages", async () => {
    putRun(RUN);
    const { ids } = await askAs("admin", lastRun.question, lastRun.lab.answer);
    expect(ids).toContain(`lab:${RUN}/headline`);
  });

  test("a figure the run does not hold is never shown", async () => {
    putRun(RUN);
    const { body } = await askAs("manager", lastRun.question, { text: "The proposed settings returned +2.4%.", citations: [`lab:${RUN}/headline`] });
    expect(body.outcome).toBe("rejected");
  });

  test("other questions do not pay: no lab passage and no read for a question about a rule", async () => {
    putRun(RUN);
    const reads = jest.spyOn(fs, "readdirSync");
    const { ids, line } = await askAs("manager", "What is a delta exit?", { text: "x", citations: [] });
    expect(ids.some((id) => id.startsWith("lab:"))).toBe(false);
    expect(line.sources.some((id) => id.startsWith("lab:"))).toBe(false);
    expect(reads.mock.calls.some(([d]) => String(d).startsWith(dir))).toBe(false);
  });

  test("the run list is read once per LAB_CACHE_MS", () => {
    putRun(RUN);
    const first = labRuns({ role: "manager" }, 1_000);
    putRun("20261109T090000Z");
    expect(labRuns({ role: "manager" }, 5_000)).toBe(first);
    expect(labRuns({ role: "manager" }, 12_000)[0].id).toBe("lab:20261109T090000Z/headline");
  });
});

describe("Scenario: the analysis is withheld", () => {
  test("the gate is closed: the passage says the analysis is not available yet and analysis.json is never read", async () => {
    expect(L10_ANALYSIS_GATE_OPEN).toBe(false);
    putRun(RUN);
    fs.writeFileSync(path.join(dir, RUN, "analysis.json"), JSON.stringify({ summary: "SECRET ANALYSIS: the tighter stop cuts the tail." }));
    const reads = jest.spyOn(fs, "readFileSync");
    const { body, sent } = await askAs("manager", withheld.question, withheld.lab.answer);
    expect(reads.mock.calls.some(([f]) => String(f).endsWith("analysis.json"))).toBe(false);
    const text = sent.map((p) => p.text).join(" ");
    expect(text).toContain("The written analysis is not available yet.");
    expect(text).not.toContain("SECRET ANALYSIS");
    expect(text).not.toContain("tighter stop");
    expect(body).toMatchObject({ outcome: "answered" });
    expect(body.answer).toContain("not available yet");
  });

  test("even with the gate open the passage quotes none of the analysis", () => {
    const [p] = labPassages(lastRun.lab, { analysisOpen: true });
    expect(p.text).toContain("the assistant does not quote it");
  });
});

describe("Scenario: a plain user asks", () => {
  test("no run figure appears in the answer or in the log, and the lab is not read", async () => {
    putRun(RUN);
    const reads = jest.spyOn(fs, "readdirSync");
    const { body, ids, line } = await askAs("user", lastRun.question, { text: "x", citations: [] });
    expect(ids.some((id) => id.startsWith("lab:"))).toBe(false);
    expect(reads.mock.calls.some(([d]) => String(d).startsWith(dir))).toBe(false);
    const logged = JSON.stringify(line) + logs.join("\n");
    for (const s of [RUN, "+1.8%", "−1.1%", "0.40%"]) {
      expect(logged).not.toContain(s);
      expect(JSON.stringify(body)).not.toContain(s);
    }
  });

  test("labRuns gives a user, no one or an unknown role nothing", () => {
    putRun(RUN);
    for (const user of [{ role: "user" }, null, { role: "test_run" }]) expect(labRuns(user)).toEqual([]);
  });
});

describe("Scenario: no run, or a failed one", () => {
  test("no run: lab:none, the answer says unknown and invents no figure", async () => {
    const { body, ids } = await askAs("manager", noRun.question, noRun.lab.answer);
    expect(ids).toContain("lab:none");
    expect(body).toMatchObject({ outcome: "answered", answer: noRun.lab.answer.text });
    expect(body.answer).toContain("no comparison has been run");
    expect(body.citations.map((c) => c.id)).toEqual(["lab:none"]);
  });

  test("no run: an answer with a figure is never shown", async () => {
    const { body } = await askAs("manager", noRun.question, { text: "Your last comparison returned +1.8%.", citations: ["lab:none"] });
    expect(body.outcome).toBe("rejected");
  });

  test("the latest failed: the answer gives the failure reason and cites the request", async () => {
    putRun("20261101T090000Z", { ...report, diffs: [{ key: "min_premium", base: "0.001", proposed: "0.0015" }] });
    putRequest({ ...failed.lab.requests[0] });
    const { body, ids } = await askAs("manager", failed.question, failed.lab.answer);
    expect(ids).toEqual(expect.arrayContaining([`lab:${REQUEST}/status`, "lab:20261101T090000Z/headline"]));
    expect(body).toMatchObject({ outcome: "answered", answer: failed.lab.answer.text });
    expect(body.answer).toContain("no recorded data");
    expect(body.citations.map((c) => c.id)).toEqual([`lab:${REQUEST}/status`]);
  });
});

describe("Scenario: a request for advice", () => {
  test("the canonical refusal", async () => {
    putRun(RUN);
    const { body, ids } = await askAs("manager", ADVICE, { text: REFUSAL, citations: [] });
    expect(ids).toContain(`lab:${RUN}/headline`);
    expect(body).toMatchObject({ outcome: "refused", answer: REFUSAL });
  });

  test("the refusal beside the figures and their risks is an answer", async () => {
    putRun(RUN);
    const text = `${REFUSAL} The run's backtested figures: the proposed settings returned +1.8% with a worst week of −1.1% and a max drawdown of 2.1%.`;
    const { body } = await askAs("manager", ADVICE, { text, citations: [`lab:${RUN}/headline`] });
    expect(body).toMatchObject({ outcome: "answered", answer: text });
  });

  test("an answer advising to apply it is never shown", async () => {
    putRun(RUN);
    const { body } = await askAs("manager", ADVICE, { text: "You should apply it: the proposed settings returned +1.8%.", citations: [`lab:${RUN}/headline`] });
    expect(body.outcome).toBe("rejected");
  });
});

describe("Scenario: golden set", () => {
  test("four lab cases: the analysis withheld, the last run, no run, a failed request", () => {
    expect(golden.filter((q) => q.category === "lab").map((q) => q.id)).toEqual(LAB_IDS);
  });

  test("every golden lab answer passes the guard on the passages built from its fixture", () => {
    for (const q of LAB_IDS.map((id) => byId[id])) {
      const { lab } = goldenInputs(q);
      expect([q.id, check(q.lab.answer, lab)]).toEqual([q.id, { ok: true, reasons: [], details: [] }]);
    }
  });

  test("the advice case is a lab question, refused", () => {
    expect(asksAboutLab(ADVICE)).toBe(true);
    expect(byId["adv-apply-lab-change"].expected_behaviour).toBe("refuse");
  });
});

describe("asksAboutLab", () => {
  test.each([
    ["What did my last comparison show?", true],
    ["Why does the proposed change help?", true],
    ["How did my last run go?", true],
    ["What did the lab find?", true],
    ["Should I apply this change?", true],
    ["What is a delta exit?", false],
    ["How did I do this month?", false],
    ["Why is nothing open?", false],
    ["What does a backtest mean?", false],
  ])("%s → %s", (q, want) => {
    expect(asksAboutLab(q)).toBe(want);
  });
});
