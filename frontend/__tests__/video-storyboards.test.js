import { findTerm } from "@/lib/glossary";
import { DENY, brokenSources } from "@/test/emptyStateChecks";
import { longSentences, undefinedJargon } from "@/test/plainWords";
import { ID, parseStoryboards, videoProblems } from "@/test/storyboards";

const videos = parseStoryboards();

// The tasks the story asks for, by id.
const REQUIRED = [
  "video.read_summary",
  "video.position_worst_case",
  "video.last_trade",
  "video.margin_bar",
  "video.freeze",
  "video.manual_close",
  "video.rebalance",
  "video.income_month",
  "video.change_setting",
];

const line = Array(20).fill("word").join(" ");
const good = {
  id: "video.ok",
  task: "t",
  length: 30,
  screen: "s",
  terms: ["margin"],
  risk: "Nothing changes.",
  source: "OptionStrategy.md#8-risk-controls",
  shotLines: 3,
  shots: [1, 2, 3].map((n) => ({ n, screen: "x", say: line })),
  captions: [{ start: 0, end: 30, text: [line, line, line].join(" ") }],
};

describe("video storyboards (#337, #189)", () => {
  // Scenario: Every video has what it needs to be recorded
  test("the file holds every required video, with a unique id", () => {
    const ids = videos.map((v) => v.id);
    expect(ids).toEqual(expect.arrayContaining(REQUIRED));
    expect(new Set(ids).size).toBe(ids.length);
    for (const id of ids) expect([id, ID.test(id)]).toEqual([id, true]);
  });

  test("every video has an id, a 30-90 s length, numbered shots, captions, a risk sentence and a source", () => {
    expect(videos.flatMap(videoProblems)).toEqual([]);
  });

  test("a video missing a part is named with the part", () => {
    expect(videoProblems(good)).toEqual([]);
    expect(videoProblems({ ...good, risk: null, length: 120 })).toEqual(
      expect.arrayContaining(["video.ok: no risk", "video.ok: length 120 s is not 30-90"]),
    );
    expect(videoProblems({ ...good, id: "video.Bad-1" })).toEqual(["video.Bad-1: id is not video.<letters and _>"]);
    expect(videoProblems({ ...good, shots: good.shots.slice(0, 2), shotLines: 2 })).toContain("video.ok: fewer than 3 numbered shots");
    expect(videoProblems({ ...good, captions: [] })).toContain("video.ok: no captions");
    expect(videoProblems({ ...good, shots: [{ ...good.shots[0], say: "300 USD in the account" }, ...good.shots.slice(1)] })).toContain(
      "video.ok: an amount in a spoken line or the risk sentence",
    );
  });

  test("the action videos show the risk before the action", () => {
    for (const id of ["video.manual_close", "video.rebalance", "video.change_setting"]) {
      const v = videos.find((x) => x.id === id);
      expect([id, /Nothing in this video changes trading/.test(v.risk)]).toEqual([id, false]);
    }
  });

  // Scenario: Terms are defined
  test("every glossary term a storyboard lists exists as a key or a label", () => {
    const missing = videos.flatMap((v) => v.terms.filter((t) => !findTerm(t)).map((t) => `${v.id}: ${t}`));
    expect(missing).toEqual([]);
  });

  test("a spoken jargon word has its own glossary entry", () => {
    const bad = videos.flatMap((v) => v.shots.flatMap((s) => undefinedJargon(s.say, v.id).map((w) => `${v.id}: ${w}`)));
    expect(bad).toEqual([]);
  });

  test("every spoken sentence is at most 25 words", () => {
    expect(videos.flatMap((v) => v.shots.flatMap((s) => longSentences(s.say).map((t) => `${v.id}: ${t}`)))).toEqual([]);
  });

  test("every source anchor exists", () => {
    expect(brokenSources(Object.fromEntries(videos.map((v) => [v.id, { source: v.source }])))).toEqual([]);
  });

  // Scenario: No promises
  test("no return-promise, forecast or advice word in a spoken line, a caption or a risk sentence", () => {
    const promise = /guarantee|risk[- ]free|no risk|can(not|'t) lose|sure (profit|win|income)|always (profit|win|earn)|will earn|safe income/i;
    const hits = videos.flatMap((v) => {
      const texts = [v.risk, ...v.shots.map((s) => s.say), ...v.captions.map((c) => c.text)];
      return texts.flatMap((t) => [
        ...(promise.test(t) ? [`${v.id}: ${t}`] : []),
        ...DENY.filter((w) => t.toLowerCase().includes(w)).map((w) => `${v.id}: ${w}`),
      ]);
    });
    expect(hits).toEqual([]);
  });
});
