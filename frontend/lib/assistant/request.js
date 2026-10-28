// The request check (#409, E10): the filter in front of bug and enhancement
// requests. A question that reads as a report goes to the model's request
// check (model.js checkRequest), which returns a structured verdict; this file
// checks that verdict against the passages and journal facts the model was
// given and decides the outcome — needs_info (the checklist, no draft),
// answered (the help or the journal explains it) or draft (a card the person
// may send). The product owner's rule: a draft only when it makes sense and
// carries data or examples. Pure: no model, no file, no network.
import { REASON_UNGROUNDED_NUMBER, REASON_UNKNOWN_CITATION, check } from "./guard";
import { CHECKLIST, FIELDS } from "@/lib/assistantRequests";

export const REQUEST_NEEDS_INFO = "needs_info";
export const REQUEST_ANSWERED = "answered";
export const REQUEST_DRAFT = "draft";
/** The verdict is not a report (kind none): the question is answered as any other. */
export const REQUEST_NONE = "none";

// Words that make a question read as a report. "should" only when it is not
// the person asking for advice ("should I…", "Should we…"), which the refusal handles.
const REPORT_PATTERNS = [
  /\bbugs?\b/i,
  /\bwrong\b/i,
  /\bbroken\b/i,
  /\bincorrect\b/i,
  /\b(doesn't|does not|didn't|did not|isn't|is not) (work|show|load|update|match)/i,
  /\bnot (working|showing|loading|updating)\b/i,
  /\bcan you add\b/i,
  /\bplease add\b/i,
  /\bfeature request\b/i,
  /\bshould\b(?! (i|we)\b)/i,
];

/** True when the question reads as a bug report or a wish: it then runs the request check. */
export function looksLikeReport(question) {
  const q = String(question ?? "").trim();
  if (/^should\b/i.test(q)) return false;
  return REPORT_PATTERNS.some((re) => re.test(q));
}

const known = (c, passages) => passages.some((p) => p.id === c || p.source === c);

/**
 * The evidence items of a verdict, each checked: cited from a given passage,
 * and every number in its text found in that passage (the answer guard's
 * ungrounded_number rule). → { good, details } — details name each item the
 * guard rejects.
 */
export function checkEvidence(evidence, given) {
  const good = [];
  const details = [];
  // "2026-10-27T14:00:00Z" in a journal title is 14:00 to a person: the
  // guard reads no number glued to the "T", so the time is spelt out first.
  const spell = (s) => String(s ?? "").replace(/(\d{4}-\d{2}-\d{2})T(\d{2}:\d{2})/g, "$1 $2");
  const passages = given.map((p) => ({ ...p, title: spell(p.title), text: spell(p.text) }));
  for (const e of evidence ?? []) {
    if (!e?.text) continue;
    if (!e.citation || !known(e.citation, passages)) {
      details.push(`${REASON_UNKNOWN_CITATION}: ${e.citation || "(none)"}`);
      continue;
    }
    const v = check({ text: e.text, citations: [e.citation] }, passages);
    const bad = v.details.filter((d) => d.startsWith(`${REASON_UNGROUNDED_NUMBER}:`));
    if (bad.length) details.push(...bad);
    else good.push({ text: e.text, citation: e.citation });
  }
  return { good, details };
}

/** "Positions · btc · 2026-10-27 14:00", the where of a verdict as one line. */
function whereLine(where) {
  return [where?.page, where?.bot, where?.time].filter(Boolean).join(" · ");
}

/** The draft card of a verified verdict: the kind's fields, the checked evidence, the steps. */
function draftOf(verdict, evidence) {
  const draft = { kind: verdict.kind };
  for (const [key] of FIELDS[verdict.kind]) draft[key] = key === "where" ? whereLine(verdict.where) : verdict[key] ?? "";
  if (!draft.title) draft.title = verdict.what.slice(0, 80);
  draft.evidence = evidence;
  draft.steps = verdict.steps ?? [];
  return draft;
}

/**
 * Decides a request check's outcome from the model's verdict (model.js
 * parseRequestReply, citations already resolved to passage ids) and the
 * passages it was given. → { outcome, checklist: [{key, label, ok}], draft
 * (null unless one may be offered), explained_by, details }:
 *   - kind none → REQUEST_NONE (not a report);
 *   - a given passage explains it (explained_by) → answered; an enhancement
 *     with what and evidence may still be offered as a draft;
 *   - what, where and evidence all present → draft;
 *   - else needs_info.
 * Evidence is at least one item cited from a given passage, or at least two
 * reproducible steps; one item whose numbers its source does not hold rejects
 * the draft (needs_info, the evidence item unticked, the guard's details kept).
 */
export function checkRequest(verdict, passages) {
  if (!verdict || !["bug", "enhancement"].includes(verdict.kind)) {
    return { outcome: REQUEST_NONE, checklist: [], draft: null, explained_by: [], details: [] };
  }
  const explainedBy = [...new Set((verdict.explained_by ?? []).filter((c) => known(c, passages)))];
  const { good, details } = checkEvidence(verdict.evidence, passages);
  const ok = {
    what: Boolean(verdict.what),
    where: Boolean(whereLine(verdict.where)),
    evidence: details.length === 0 && (good.length > 0 || (verdict.steps ?? []).length >= 2),
    not_explained: explainedBy.length === 0,
  };
  const checklist = CHECKLIST.map((item) => ({ ...item, ok: ok[item.key] }));
  const draftable = ok.what && ok.where && ok.evidence;

  let outcome = REQUEST_NEEDS_INFO;
  let draft = null;
  if (!ok.not_explained) {
    outcome = REQUEST_ANSWERED;
    if (verdict.kind === "enhancement" && draftable) draft = draftOf(verdict, good);
  } else if (draftable) {
    outcome = REQUEST_DRAFT;
    draft = draftOf(verdict, good);
  }
  return { outcome, checklist, draft, explained_by: explainedBy, details };
}

/** The checklist of a report there was nothing to check against (no passage, no journal): nothing explains it, the rest is missing. */
export function emptyChecklist() {
  return CHECKLIST.map((item) => ({ ...item, ok: item.key === "not_explained" }));
}
