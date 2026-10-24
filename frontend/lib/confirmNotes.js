// Risk notes shown before a person confirms a manual close or a rebalance
// (#299). Content lives in content/confirm-notes.json: { action: { does,
// risks, follows, follows_last_leg? | dry_run?, terms, source } } — `terms`
// are the glossary keys the texts name, so each can link to its tip.
// Facts only, from the rules (manualclose.go, manualrebalance.go); never a
// figure, a forecast or advice. Draft wording: not for external users before
// counsel approves it (docs/legal/).
import notes from "@/content/confirm-notes.json";

export const MAX_NOTE = 280;

/** The fields of each action that hold a text shown to the person. */
export const TEXT_FIELDS = {
  manual_close: ["does", "risks", "follows", "follows_last_leg"],
  rebalance: ["does", "risks", "follows", "dry_run"],
};

/**
 * The note for an action: { action, does, risks, follows, terms, source },
 * or null for an unknown action. lastLeg (manual close): the leg is its
 * strangle's last, so the slot is re-entered. dryRun (rebalance): nothing is
 * sent, so the note says only what the preview does (risks, follows null).
 */
export function noteFor(action, { lastLeg = false, dryRun = false } = {}, catalogue = notes) {
  const n = Object.hasOwn(catalogue, action) ? catalogue[action] : null;
  if (!n) return null;
  const base = { action, terms: n.terms, source: n.source };
  if (action === "rebalance" && dryRun) return { ...base, does: n.dry_run, risks: null, follows: null };
  const follows = action === "manual_close" && lastLeg ? n.follows_last_leg : n.follows;
  return { ...base, does: n.does, risks: n.risks, follows };
}

export default notes;
