# What's new notes

One short note per promoted release, shown once as a *What's new* item of the monitor's notification bell (#447, `frontend/lib/releases.js`).
A person writes it when a release is promoted to stable; `scripts/promote.sh` does not write it.

The file is `<tag>.md` (for example `v0.3.0.md`; any other name is not listed):

```markdown
---
date: 2026-10-30
title: Notifications in the menu bar
---
One or two plain sentences on what changed for the person using the monitor.
```

- `date` is the promotion day (UTC, `YYYY-MM-DD`); the bell lists a note for 90 days from it.
- `title` is one short line; the body is at most 600 characters, plain words, no amounts, nothing promotional and no "act now".
- A note without a date, a title or a body is not listed.

The monitor reads this folder at `RELEASE_NOTES_DIR` (compose mounts it read-only), default `../docs/product/releases` from `frontend/`.
