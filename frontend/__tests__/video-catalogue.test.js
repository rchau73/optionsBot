/**
 * @jest-environment node
 */
import fs from "node:fs";
import path from "node:path";
import glossary from "@/content/glossary.json";
import videos from "@/content/videos.json";
import { TIP_IDS, VIDEO_IDS, tipIdOf, validateEvent } from "@/lib/analytics/plan";
import { parseStoryboards } from "@/test/storyboards";

const REPO = path.join(process.cwd(), "..");
const SESSION = "0123456789abcdef0123456789abcdef";
const event = (name, props) => validateEvent({ event: name, session_id: SESSION, props });

/** Problems of the catalogue, as "id: what". */
export function catalogueProblems(entries) {
  return Object.entries(entries).flatMap(([id, v]) => {
    const out = [];
    const bad = (what) => out.push(`${id}: ${what}`);
    if (!/^video\.[a-z]+(_[a-z]+)*$/.test(id)) bad("id is not video.<letters and _>");
    for (const f of ["title", "task", "screen", "storyboard", "captions"]) if (!v[f] || typeof v[f] !== "string") bad(`no ${f}`);
    if (!(Number.isInteger(v.seconds) && v.seconds >= 30 && v.seconds <= 90)) bad(`${v.seconds} s is not 30-90`);
    return out;
  });
}

describe("video catalogue (#300)", () => {
  test("every entry has its fields and a 30-90 s length", () => {
    expect(catalogueProblems(videos)).toEqual([]);
  });

  // Scenario: Length rule
  test("an entry of 120 seconds fails, naming the entry", () => {
    expect(catalogueProblems({ "video.long": { ...videos["video.freeze"], seconds: 120 } })).toEqual(["video.long: 120 s is not 30-90"]);
  });

  test("every storyboard of #189 has an entry, with its length, and no entry lacks a storyboard", () => {
    const boards = parseStoryboards();
    expect(Object.keys(videos).sort()).toEqual(boards.map((b) => b.id).sort());
    for (const b of boards) expect([b.id, videos[b.id].seconds]).toEqual([b.id, b.length]);
  });

  test("each storyboard path exists and an unrecorded video may lack its captions file", () => {
    for (const [id, v] of Object.entries(videos)) {
      expect([id, fs.existsSync(path.join(REPO, v.storyboard))]).toEqual([id, true]);
      if (v.status !== "planned") expect([id, fs.existsSync(path.join(REPO, v.captions))]).toEqual([id, true]);
    }
  });

  test("a glossary key becomes a tip id of letters and _ only", () => {
    expect(tipIdOf("stop-loss")).toBe("tip.stop_loss");
    expect(tipIdOf("p&l %")).toBe("tip.p_l");
    expect(tipIdOf("gamma flip")).toBe("tip.gamma_flip");
    for (const key of Object.keys(glossary)) expect([key, /^tip\.[a-z]+(_[a-z]+)*$/.test(tipIdOf(key))]).toEqual([key, true]);
  });
});

describe("the collector checks ids against the catalogues (#300)", () => {
  // Scenario: Known video
  test("a known video id is accepted", () => {
    expect(VIDEO_IDS.has("video.read_summary")).toBe(true);
    expect(event("video_played", { video_id: "video.read_summary" }).ok).toBe(true);
  });

  // Scenario: Unknown id
  test("an unknown video id is refused whole", () => {
    const r = event("video_played", { video_id: "video.not_in_catalogue" });
    expect(r.ok).toBe(false);
    expect(r.error).toContain("video_id");
    expect(r.error).not.toContain("not_in_catalogue");
  });

  test("a known tip id is accepted and an unknown one refused", () => {
    expect(TIP_IDS.has("tip.gamma_flip")).toBe(true);
    expect(event("tip_opened", { tip_id: "tip.gamma_flip", screen: "monitor" }).ok).toBe(true);
    expect(event("tip_opened", { tip_id: "tip.no_such_term" }).ok).toBe(false);
    expect(event("help_resolved", { tip_id: "tip.gamma_flip", video_id: "video.nope", helped: true }).ok).toBe(false);
  });
});
