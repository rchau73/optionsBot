// Keyword (BM25) search over the help index (#191): the JavaScript twin of
// internal/helpkb/search.go (#259), so the monitor finds the passages a
// question needs without calling the bot. Same tokens, stems, constants and
// "I don't know" rule; __tests__/assistant-search.test.js scores it on the Go
// tests' question set against the same baseline. Pure.

const BM25_K1 = 1.2;
const BM25_B = 0.75;
// A passage whose title has exactly the question's words is about that term.
const TITLE_MATCH_BOOST = 2.0;

/** The share of the question's weight the best passage must hold, else "I don't know". */
export const DEFAULT_NO_ANSWER = 0.5;

const TOKEN_RE = /[\p{L}\p{N}]+(?:['’.][\p{L}\p{N}]+)*/gu;

const STOP_WORDS = new Set(
  `a an the and or but if then so as of to in on at by for from with without into
  about over under is are was were be been being am do does did done doing has have had having
  it its this that these those there here which who whom whose what when where why how
  i me my we our you your he she they them their his her can could would should will shall may might must
  not no than too very just also any some all each more most such own same only
  happen happens happened tell explain mean means meaning does work works`.split(/\s+/),
);

// A deliberately small English stemmer: plurals and -ing/-ed, a doubled final
// consonant undone ("stopped", "stopping" → "stop").
function stem(w) {
  if (/[0-9]/.test(w)) return w;
  let cut;
  if (w.length > 4 && w.endsWith("ies")) return `${w.slice(0, -3)}y`;
  if (w.endsWith("sses")) return w.slice(0, -2);
  if (w.length > 5 && w.endsWith("ing")) cut = w.slice(0, -3);
  else if (w.length > 4 && w.endsWith("ed")) cut = w.slice(0, -2);
  else if (w.length > 3 && w.endsWith("s") && !w.endsWith("ss") && !w.endsWith("us")) return w.slice(0, -1);
  else return w;
  const n = cut.length;
  if (n >= 2 && cut[n - 1] === cut[n - 2] && !"lsz".includes(cut[n - 1])) cut = cut.slice(0, -1);
  return cut;
}

/** Lower-cased words of a text, stop words dropped, each stemmed ("0.30" and "leg's" stay whole). */
export function tokens(text) {
  const out = [];
  for (const [m] of String(text ?? "").toLowerCase().matchAll(TOKEN_RE)) {
    let w = m;
    if (w.endsWith("'s")) w = w.slice(0, -2);
    if (w.endsWith("’s")) w = w.slice(0, -2);
    if (STOP_WORDS.has(w)) continue;
    out.push(stem(w));
  }
  return out;
}

const unique = (ts) => [...new Set(ts)];

function sameTerms(a, b) {
  if (a.length === 0 || a.length !== b.length) return false;
  const set = new Set(a);
  return b.every((t) => set.has(t));
}

/** A BM25 index over passages { id, source, title, text }; the title counts twice. */
export class HelpIndex {
  constructor(passages) {
    this.docs = passages;
    this.df = new Map();
    this.tf = [];
    this.lens = [];
    this.titles = [];
    let total = 0;
    for (const p of passages) {
      const toks = tokens(`${p.title} ${p.title} ${p.text}`);
      const tf = new Map();
      for (const t of toks) tf.set(t, (tf.get(t) ?? 0) + 1);
      for (const t of tf.keys()) this.df.set(t, (this.df.get(t) ?? 0) + 1);
      this.titles.push(unique(tokens(p.title)));
      this.tf.push(tf);
      this.lens.push(toks.length);
      total += toks.length;
    }
    this.avgDL = passages.length > 0 ? total / passages.length : 0;
  }

  idf(term) {
    const n = this.docs.length;
    const df = this.df.get(term) ?? 0;
    return Math.log(1 + (n - df + 0.5) / (df + 0.5));
  }

  /** The k best passages for the question, best first: { passage, score, coverage }. */
  search(question, k) {
    const terms = unique(tokens(question));
    if (terms.length === 0 || k <= 0) return [];
    let weight = 0;
    for (const t of terms) weight += this.idf(t);
    const hits = [];
    this.tf.forEach((tf, i) => {
      let score = 0;
      let found = 0;
      for (const t of terms) {
        const f = tf.get(t) ?? 0;
        if (f === 0) continue;
        const idf = this.idf(t);
        found += idf;
        const norm = 1 - BM25_B + (BM25_B * this.lens[i]) / this.avgDL;
        score += (idf * f * (BM25_K1 + 1)) / (f + BM25_K1 * norm);
      }
      if (sameTerms(this.titles[i], terms)) score *= TITLE_MATCH_BOOST;
      if (score > 0) hits.push({ passage: this.docs[i], score, coverage: found / weight });
    });
    hits.sort((a, b) => b.score - a.score);
    return hits.slice(0, k);
  }
}

/** True when the help does not answer: nothing matched, or the best passage holds under `threshold` of the question's weight. */
export function noAnswer(hits, threshold = DEFAULT_NO_ANSWER) {
  return hits.length === 0 || hits[0].coverage < threshold;
}
