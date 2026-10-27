// Reads docs/product/education/video-storyboards.md (#337) into sections, so a
// test can check that every video has what it needs to be recorded.
import fs from "node:fs";
import path from "node:path";

const REPO = path.join(__dirname, "..", "..");

export const STORYBOARDS = "docs/product/education/video-storyboards.md";

export const ID = /^video\.[a-z]+(_[a-z]+)*$/;

/** The videos of the file: id, and the fields each section holds (missing ones are null or empty). */
export function parseStoryboards(text = fs.readFileSync(path.join(REPO, STORYBOARDS), "utf8")) {
  return text
    .split(/^## (?=video\.)/m)
    .slice(1)
    .map((block) => {
      const field = (name) => block.match(new RegExp(`^- \\*\\*${name}:\\*\\* (.+)$`, "m"))?.[1].trim() ?? null;
      const shotsBlock = block.match(/### Shots\n([\s\S]*?)(?=\n### |\n---|$)/)?.[1] ?? "";
      const captionsBlock = block.match(/### Captions\n([\s\S]*?)(?=\n---|$)/)?.[1] ?? "";
      const length = field("Length")?.match(/^(\d+) s$/)?.[1];
      return {
        id: block.split("\n")[0].trim(),
        task: field("Task"),
        length: length ? Number(length) : null,
        screen: field("Screen"),
        terms: [...(field("Terms") ?? "").matchAll(/`([^`]+)`/g)].map((m) => m[1]),
        risk: field("Risk sentence"),
        source: field("Source")?.replace(/`/g, "") ?? null,
        shots: [...shotsBlock.matchAll(/^(\d+)\. On screen: (.+?) Say: "([^"]+)"$/gm)].map((m) => ({ n: Number(m[1]), screen: m[2], say: m[3] })),
        shotLines: (shotsBlock.match(/^\d+\. .*$/gm) ?? []).length,
        captions: [...captionsBlock.matchAll(/^- \[(\d\d):(\d\d)-(\d\d):(\d\d)\] (.+)$/gm)].map((m) => ({
          start: Number(m[1]) * 60 + Number(m[2]),
          end: Number(m[3]) * 60 + Number(m[4]),
          text: m[5],
        })),
      };
    });
}

export const words = (text) => String(text).trim().split(/\s+/).filter(Boolean);

/** Problems of one video, as "id: what" (empty when it can be recorded). */
export function videoProblems(v) {
  const out = [];
  const bad = (what) => out.push(`${v.id}: ${what}`);
  if (!ID.test(v.id)) bad("id is not video.<letters and _>");
  for (const f of ["task", "screen", "risk", "source"]) if (!v[f]) bad(`no ${f}`);
  if (!(v.length >= 30 && v.length <= 90)) bad(`length ${v.length} s is not 30-90`);
  if (v.terms.length === 0) bad("no glossary terms");
  if (v.shots.length < 3) bad("fewer than 3 numbered shots");
  if (v.shots.length !== v.shotLines) bad("a numbered line is not 'On screen: … Say: \"…\"'");
  v.shots.forEach((s, i) => s.n !== i + 1 && bad(`shot ${s.n} is out of order`));
  const spoken = v.shots.flatMap((s) => words(s.say));
  if (spoken.length > 200) bad(`${spoken.length} spoken words, over 200`);
  const pace = spoken.length / (v.length || 1);
  if (pace < 1.5 || pace > 3) bad(`pace ${pace.toFixed(1)} words/s is not 1.5-3`);
  if (v.captions.length === 0) bad("no captions");
  if (words(v.captions.map((c) => c.text).join(" ")).join(" ") !== spoken.join(" ")) bad("captions are not the spoken text");
  let at = 0;
  for (const c of v.captions) {
    if (c.start < at || c.end <= c.start) bad(`caption time ${c.start}-${c.end} is out of order`);
    at = c.end;
  }
  if (at > v.length) bad(`captions run to ${at} s, past ${v.length} s`);
  // No amount: a number followed by a coin, a currency or a %.
  const amount = /\d[\d,.]*\s*(btc|eth|usd|%|\$)|[$]\s*\d/i;
  if (amount.test(`${v.shots.map((s) => s.say).join(" ")} ${v.risk}`)) bad("an amount in a spoken line or the risk sentence");
  return out;
}
