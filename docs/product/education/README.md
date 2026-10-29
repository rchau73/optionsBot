# In-product education (E11)

Index of the education material.
- `video-storyboards.md`: the how-to videos, one `video.<id>` section each.
- `comprehension-test.md`: the protocol that measures whether the tips and videos work.
- `empty-states.md`: the copy of every monitor panel with no data.
- `tips.json`: a plain-words tip for every tunable setting (#492): meaning, unit, what raising it does to risk and to income, a video id where one fits.
  - Core-policy settings say a change needs the product owner.
  - Keys are the catalogue's (`config.Tunables()`), in alphabetical order.
  - Glossary terms are their own tips (`short` and `risk_income` in `frontend/content/glossary.json`).
  - `tests/tips_coverage_test.go` fails on a tunable or glossary term with no tip, a sentence over 25 words, a jargon word with no glossary entry, or a promised return; it names the key or term.
  - No screen reads the file yet; the monitor will, once the epic's prototype is approved.
