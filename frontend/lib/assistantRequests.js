// Assistant requests (#409, E10): the wording and limits the browser and the
// server share — the request check's four items, the draft card's notice, a
// request's fields and their sizes, the review statuses. No file access here,
// so the panel imports it; lib/assistant/request.js and requests.js use it on
// the server.

/** The request check's four items, in the order the checklist shows them. */
export const CHECKLIST = [
  { key: "what", label: "What: the behaviour you saw, or what you wish for" },
  { key: "where", label: "Where: the page, the bot and the time" },
  { key: "evidence", label: "Evidence: a number, a journal entry, a P&L history point or the steps to see it again" },
  { key: "not_explained", label: "Not explained: no help passage or journal entry already explains it" },
];

/** The text of a needs_info reply: the checklist under it names what is missing. */
export const NEEDS_INFO_TEXT = "To draft a request I need the items below that are not ticked. Tell me more and ask again.";
/** The text above a draft card. */
export const DRAFT_TEXT = "Here is a draft request from what you wrote and what the bot recorded. Nothing is sent until you press Send for review.";

export const DRAFT_HEADING = "Draft request · you send it";
export const DRAFT_NOTICE =
  "Goes to a private review queue, not to the public repository. Before anything is made public, amounts, account details and personal data are removed.";

/** "Sent as R-0012. Follow it in My requests." */
export function sentText(id) {
  return `Sent as ${id}. Follow it in My requests.`;
}

export const KINDS = ["bug", "enhancement"];
export const KIND_LABELS = { bug: "Bug", enhancement: "Enhancement" };

// Sizes a sent request may have (the route refuses more).
export const MAX_TITLE = 120;
export const MAX_FIELD = 1000;
export const MAX_ITEMS = 10; // evidence items, steps
/** Requests a person may send per UTC day. */
export const REQUESTS_PER_DAY = 10;

/** The text fields of a draft by kind, with their labels, in the card's order (evidence and steps are lists). */
export const FIELDS = {
  bug: [
    ["title", "Title"],
    ["what", "What"],
    ["where", "Where"],
    ["expected", "Expected"],
  ],
  enhancement: [
    ["title", "Title"],
    ["what", "What"],
    ["where", "Where"],
    ["who", "Who"],
    ["why", "Why"],
    ["example", "Example"],
  ],
};

/** Review statuses (requests_status.jsonl, written by the PM agent) and how My requests names them. */
export const STATUS_LABELS = {
  in_review: "In review",
  filed: "Filed",
  fixed: "Fixed",
  not_planned: "Not planned",
  not_a_bug: "Not a bug",
  duplicate: "Duplicate",
  needs_info: "Needs info",
};
export const STATUSES = Object.keys(STATUS_LABELS);

export function statusLabel(status) {
  return STATUS_LABELS[status] ?? STATUS_LABELS.in_review;
}

/**
 * A draft's fields as a request body sends them: the kind's text fields
 * trimmed and cut to their size, evidence [{text, citation}] and steps
 * without empty items. The card's edits go through it.
 */
export function requestFields(kind, draft) {
  const out = {};
  for (const [key] of FIELDS[kind] ?? []) {
    const v = typeof draft?.[key] === "string" ? draft[key].trim() : "";
    out[key] = v.slice(0, key === "title" ? MAX_TITLE : MAX_FIELD);
  }
  out.evidence = (Array.isArray(draft?.evidence) ? draft.evidence : [])
    .map((e) => ({ text: String(e?.text ?? "").trim().slice(0, MAX_FIELD), citation: String(e?.citation ?? "") }))
    .filter((e) => e.text)
    .slice(0, MAX_ITEMS);
  out.steps = (Array.isArray(draft?.steps) ? draft.steps : [])
    .map((s) => String(s ?? "").trim().slice(0, MAX_FIELD))
    .filter(Boolean)
    .slice(0, MAX_ITEMS);
  return out;
}
