# In-product education (E11)

Index of the education material.
- `video-storyboards.md`: the how-to videos, one `video.<id>` section each.
- `comprehension-test.md`: the protocol that measures whether the tips and videos work.
- `empty-states.md`: the copy of every monitor panel with no data.
- `tips.json`: a plain-words tip for every tunable setting (#492): meaning, unit, what raising it does to risk and to income, a video id where one fits.
  - Core-policy settings say a change needs the product owner.
  - Keys are the catalogue's (`config.Tunables()`), in alphabetical order.
  - Glossary terms are their own tips (`short` and `risk_income` in `frontend/content/glossary.json`).
  - The futures hedge's terms (coin delta, perpetual, inverse perpetual, hedge band, hedge vs shed, hedge unavailable, kill switch perpetual) are glossary tips; `TestTips_EveryHedgeTermHasATip` (#681) names a hedge term with no tip or no "testnet" in it. A new hedge term goes in its `hedgeTerms` list.
  - `tests/tips_coverage_test.go` fails on a tunable or glossary term with no tip, a sentence over 25 words, a jargon word with no glossary entry, or a promised return; it names the key or term.
  - The monitor serves it since #588: `GET /api/help/tips` (`frontend/docs/api-routes.md`) and `frontend/lib/tips.js`. No screen uses the route yet; the first one comes after the epic's prototype is approved.
  - The monitor image is built from `frontend/` alone, so it reads a copy, `frontend/content/tips.json`.
    After editing this file run `cp docs/product/education/tips.json frontend/content/tips.json`; `frontend/__tests__/tips-route.test.js` fails on a stale copy.
- `videos.json`: the manifest of the how-to videos (#611), one entry per `video.<id>` section of the storyboards, keys in alphabetical order.
  - Each entry has `captions` (path), `duration_sec` (planned, at most 90), `id`, `status` (`storyboard` or `recorded`), `task` (one task), `tips` (the tip keys that link it) and `title`. No file URL while a video is unrecorded.
  - `captions/<id>.vtt`: one caption file per video, built from the storyboard's captions.
  - `tests/videos_coverage_test.go` fails on a storyboard with no entry, an entry with no storyboard, a tip whose video id is missing or does not list it, and a caption sentence over 25 words, with a jargon word with no glossary entry or a promised return; it names the id.
  - The monitor serves it: `GET /api/help/videos` (`frontend/docs/api-routes.md`) and `frontend/lib/videos.js`. No screen uses it yet.
  - The image reads a copy: after editing run `cp docs/product/education/videos.json frontend/content/videos.json`; `frontend/__tests__/videos-route.test.js` fails on a stale copy.

## Measuring use (#642)

The events `tip_opened`, `video_played`, `video_completed` and `help_resolved` (analytics event plan §2) carry catalogue ids only, never an amount or text, and follow the E13 consent: with analytics off nothing is sent.
Their ids must exist in `tips.json` and `videos.json` (the collector refuses others).
The weekly usage report's "Education" section reads them: tips and videos per active person, the share of resolved help from tips and videos against the assistant, and the tip keys to rewrite.
With no events it says "No data yet". No screen emits the events until the first tip or video screen ships.
