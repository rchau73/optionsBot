// Server-side only: the What's new notes of the bell (#447). One short file
// per promoted release, docs/product/releases/<tag>.md, written by a person
// (scripts/promote.sh does not write it):
//
//   ---
//   date: 2026-10-30
//   title: Notifications in the menu bar
//   ---
//   One or two plain sentences on what changed for the person.
//
// A file without a release tag name, a date or a title is not listed.
import fs from "node:fs";
import path from "node:path";

/** The longest body kept: a note is a short paragraph, never a changelog. */
export const MAX_NOTE_BODY = 600;

const TAG_FILE = /^(v\d+\.\d+\.\d+)\.md$/;
const DATE = /^\d{4}-\d{2}-\d{2}$/;

/** RELEASE_NOTES_DIR (compose mounts the folder), default docs/product/releases beside frontend/. */
export function releasesDir(raw = process.env.RELEASE_NOTES_DIR) {
  return raw || path.join(process.cwd(), "..", "docs", "product", "releases");
}

/** One note's text → { tag, date (ISO), title, body }, or null when it is not a note. */
export function parseReleaseNote(file, text) {
  const tag = TAG_FILE.exec(file)?.[1];
  const m = /^---\n([\s\S]*?)\n---\n?([\s\S]*)$/.exec(String(text ?? "").replace(/\r\n/g, "\n"));
  if (!tag || !m) return null;
  const head = Object.fromEntries(
    m[1]
      .split("\n")
      .map((l) => /^(\w+):\s*(.*)$/.exec(l.trim()))
      .filter(Boolean)
      .map(([, k, v]) => [k, v.trim()]),
  );
  const body = m[2].trim().replace(/\s+/g, " ");
  if (!DATE.test(head.date ?? "") || !head.title || !body) return null;
  const date = new Date(`${head.date}T00:00:00Z`);
  if (Number.isNaN(date.getTime())) return null;
  return { tag, date: date.toISOString(), title: head.title, body: body.slice(0, MAX_NOTE_BODY) };
}

/** Every note in dir, newest first; [] when the folder does not exist. */
export function readReleaseNotes(dir = releasesDir()) {
  let files;
  try {
    files = fs.readdirSync(dir);
  } catch {
    return [];
  }
  const notes = [];
  for (const file of files) {
    if (!TAG_FILE.test(file)) continue;
    try {
      const note = parseReleaseNote(file, fs.readFileSync(path.join(dir, file), "utf8"));
      if (note) notes.push(note);
    } catch {
      // an unreadable file: not listed
    }
  }
  return notes.sort((a, b) => b.date.localeCompare(a.date));
}
