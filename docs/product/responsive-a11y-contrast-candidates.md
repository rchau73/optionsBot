# Contrast candidates: the nearest passing colour for each failing pair (E12, #878)

A decision sheet for the designer. For each failing pair of the accessibility baseline (`responsive-a11y-baseline.md`), two or three colours that pass WCAG AA, found by changing one thing and keeping the rest, and what each does to the other pairs of the same token.
It changes no colour, token, class or component: the designer picks once the prototype (#654) is approved, and the fix is a later UI story.

**Generated** by `frontend/__tests__/contrast-candidates.test.js` from `KNOWN_FINDINGS` (`frontend/test/contrastPairs.js`) and `app/globals.css`; do not edit by hand.
When a finding is fixed or added the test fails and asks for `UPDATE_CONTRAST_SHEET=1 npm test -- contrast-candidates` from `frontend/`.

## How to read it

- **Bar:** 4.5:1 for body text, 3:1 for large text.
- **Options:** *text* changes the text colour's token; *background* the background's token; *tint* the opacity of a tinted background. Lighter and darker mix the colour toward white or black in 1 % steps, so the hue stays; the first step that passes is listed (the nearest).
- **A token is changed everywhere it is used.** When the text and its tint are the same token (`loss` on `loss/15`) both move, which is why a tint option can pass where a colour option does not. The second table shows each option's effect on every other pair of that token — a change to `loss` shows its effect on every loss figure.
- **Distance** is the straight-line distance in sRGB (0–441); **hue moved** is the angle from today's hue (0° = the same colour family).
- **Keep apart:** a loss candidate stays red and a gain stays green (a lighter or darker red never approaches `profit`); a filled-button red (#2, #6) that darkens `loss` or `live` moves them in lightness only, so a loss figure and a button stay told apart by size and shape as today — the designer decides whether a filled button gets a token of its own.

## Findings

### #2 `text-white on bg-live`

Seen in: Badge live. Now 3.76:1, AA needs 4.5:1.

| Option | Change | Colour | Ratio | Distance from now | Hue moved |
|---|---|---|---|---|---|
| A | darker `white` text, hue kept | `#1c1c1c` (now `#ffffff`) | 4.53:1 | 393 | 0° |
| B | darker `live` background, hue kept | `#d73d3d` (now `#ef4444`) | 4.54:1 | 26 | 0° |

Other pairs the same change touches (ratio now → with each option; ✗ is under the bar):

| Pair | Seen in | Now | A | B |
|---|---|---|---|---|
| `text-white on bg-live` | Badge live | 3.76 ✗ | 4.53 ✓ | 4.54 ✓ |
| `text-white on bg-action` | primary buttons (Save, Send) | 2.98 ✗ | 5.71 ✓ | same |
| `text-white on bg-loss` | Close / Delete confirm buttons | 3.67 ✗ | 4.64 ✓ | same |
| `text-white on bg-slate-700` | the menu's current page, selected tabs | 10.34 ✓ | 1.65 ✗ | same |
| `text-white on bg-slate-600` | selected chart range | 7.56 ✓ | 2.25 ✗ | same |
| `text-white on bg-sky-900` | the Ask button, survey choices | 9.47 ✓ | 1.80 ✗ | same |

### #3 `text-loss on bg-loss/15`

Seen in: Badge bad and form errors. Now 4.10:1, AA needs 4.5:1.

| Option | Change | Colour | Ratio | Distance from now | Hue moved |
|---|---|---|---|---|---|
| A | lighter `loss` text, hue kept | `#f55873` (now `#f43f5e`) | 4.52:1 | 33 | 0° |
| B | a weaker tint: `loss` at 6 % opacity instead of 15 % | 6 % opacity (now 15 %) | 4.51:1 | 9 points | 0° |

Other pairs the same change touches (ratio now → with each option; ✗ is under the bar):

| Pair | Seen in | Now | A | B |
|---|---|---|---|---|
| `text-loss on bg-surface` | page background (Filters, menu bar) | 5.13 ✓ | 5.85 ✓ | same |
| `text-loss on bg-panel` | panel body (every page) | 4.72 ✓ | 5.39 ✓ | same |
| `text-loss on bg-loss/10` | error banners (Monitor, Details) | 4.34 ✗ | 4.84 ✓ | same |
| `text-loss on bg-loss/15` | Badge bad and form errors | 4.10 ✗ | 4.52 ✓ | 4.51 ✓ |
| `text-loss on bg-loss/20` | request draft chip | 3.85 ✗ | 4.19 ✗ | same |
| `text-white on bg-loss` | Close / Delete confirm buttons | 3.67 ✗ | 3.22 ✗ | same |

### #6 `text-white on bg-loss`

Seen in: Close / Delete confirm buttons. Now 3.67:1, AA needs 4.5:1.

| Option | Change | Colour | Ratio | Distance from now | Hue moved |
|---|---|---|---|---|---|
| A | darker `white` text, hue kept | `#1c1c1c` (now `#ffffff`) | 4.64:1 | 393 | 0° |
| B | darker `loss` background, hue kept | `#d93854` (now `#f43f5e`) | 4.52:1 | 30 | 0° |

Other pairs the same change touches (ratio now → with each option; ✗ is under the bar):

| Pair | Seen in | Now | A | B |
|---|---|---|---|---|
| `text-white on bg-live` | Badge live | 3.76 ✗ | 4.53 ✓ | same |
| `text-white on bg-action` | primary buttons (Save, Send) | 2.98 ✗ | 5.71 ✓ | same |
| `text-white on bg-loss` | Close / Delete confirm buttons | 3.67 ✗ | 4.64 ✓ | 4.52 ✓ |
| `text-white on bg-slate-700` | the menu's current page, selected tabs | 10.34 ✓ | 1.65 ✗ | same |
| `text-white on bg-slate-600` | selected chart range | 7.56 ✓ | 2.25 ✗ | same |
| `text-white on bg-sky-900` | the Ask button, survey choices | 9.47 ✓ | 1.80 ✗ | same |
| `text-loss on bg-surface` | page background (Filters, menu bar) | 5.13 ✓ | same | 4.16 ✗ |
| `text-loss on bg-panel` | panel body (every page) | 4.72 ✓ | same | 3.83 ✗ |
| `text-loss on bg-loss/10` | error banners (Monitor, Details) | 4.34 ✗ | same | 3.57 ✗ |
| `text-loss on bg-loss/15` | Badge bad and form errors | 4.10 ✗ | same | 3.41 ✗ |
| `text-loss on bg-loss/20` | request draft chip | 3.85 ✗ | same | 3.24 ✗ |

### #7 `text-action on bg-action/20`

Seen in: the Rebalance button. Now 4.24:1, AA needs 4.5:1.

| Option | Change | Colour | Ratio | Distance from now | Hue moved |
|---|---|---|---|---|---|
| A | lighter `action` text, hue kept | `#8a94f8` (now `#818cf8`) | 4.53:1 | 12 | 0° |
| B | a weaker tint: `action` at 16 % opacity instead of 20 % | 16 % opacity (now 20 %) | 4.55:1 | 4 points | 0° |

Other pairs the same change touches (ratio now → with each option; ✗ is under the bar):

| Pair | Seen in | Now | A | B |
|---|---|---|---|---|
| `text-action on bg-surface` | page background (Filters, menu bar) | 6.31 ✓ | 6.88 ✓ | same |
| `text-action on bg-panel` | panel body (every page) | 5.81 ✓ | 6.34 ✓ | same |
| `text-action on bg-action/20` | the Rebalance button | 4.24 ✗ | 4.53 ✓ | 4.55 ✓ |
| `text-white on bg-action` | primary buttons (Save, Send) | 2.98 ✗ | 2.74 ✗ | same |

### #9 `text-white on bg-action`

Seen in: primary buttons (Save, Send). Now 2.98:1, AA needs 4.5:1.

| Option | Change | Colour | Ratio | Distance from now | Hue moved |
|---|---|---|---|---|---|
| A | darker `white` text, hue kept | `#2e2e2e` (now `#ffffff`) | 4.55:1 | 362 | 0° |
| B | darker `action` background, hue kept | `#666fc4` (now `#818cf8`) | 4.53:1 | 65 | 0° |

Other pairs the same change touches (ratio now → with each option; ✗ is under the bar):

| Pair | Seen in | Now | A | B |
|---|---|---|---|---|
| `text-white on bg-live` | Badge live | 3.76 ✗ | 3.61 ✗ | same |
| `text-white on bg-action` | primary buttons (Save, Send) | 2.98 ✗ | 4.55 ✓ | 4.53 ✓ |
| `text-white on bg-loss` | Close / Delete confirm buttons | 3.67 ✗ | 3.70 ✗ | same |
| `text-white on bg-slate-700` | the menu's current page, selected tabs | 10.34 ✓ | 1.31 ✗ | same |
| `text-white on bg-slate-600` | selected chart range | 7.56 ✓ | 1.80 ✗ | same |
| `text-white on bg-sky-900` | the Ask button, survey choices | 9.47 ✓ | 1.43 ✗ | same |
| `text-action on bg-surface` | page background (Filters, menu bar) | 6.31 ✓ | same | 4.16 ✗ |
| `text-action on bg-panel` | panel body (every page) | 5.81 ✓ | same | 3.83 ✗ |
| `text-action on bg-action/20` | the Rebalance button | 4.24 ✗ | same | 3.05 ✗ |

### #10 `text-loss on bg-loss/10`

Seen in: error banners (Monitor, Details). Now 4.34:1, AA needs 4.5:1.

| Option | Change | Colour | Ratio | Distance from now | Hue moved |
|---|---|---|---|---|---|
| A | lighter `loss` text, hue kept | `#f54966` (now `#f43f5e`) | 4.53:1 | 13 | 0° |
| B | a weaker tint: `loss` at 6 % opacity instead of 10 % | 6 % opacity (now 10 %) | 4.51:1 | 4 points | 0° |

Other pairs the same change touches (ratio now → with each option; ✗ is under the bar):

| Pair | Seen in | Now | A | B |
|---|---|---|---|---|
| `text-loss on bg-surface` | page background (Filters, menu bar) | 5.13 ✓ | 5.40 ✓ | same |
| `text-loss on bg-panel` | panel body (every page) | 4.72 ✓ | 4.98 ✓ | same |
| `text-loss on bg-loss/10` | error banners (Monitor, Details) | 4.34 ✗ | 4.53 ✓ | 4.51 ✓ |
| `text-loss on bg-loss/15` | Badge bad and form errors | 4.10 ✗ | 4.26 ✗ | same |
| `text-loss on bg-loss/20` | request draft chip | 3.85 ✗ | 3.98 ✗ | same |
| `text-white on bg-loss` | Close / Delete confirm buttons | 3.67 ✗ | 3.48 ✗ | same |

### #11 `text-loss on bg-loss/20`

Seen in: request draft chip. Now 3.85:1, AA needs 4.5:1.

| Option | Change | Colour | Ratio | Distance from now | Hue moved |
|---|---|---|---|---|---|
| A | lighter `loss` text, hue kept | `#f66981` (now `#f43f5e`) | 4.51:1 | 55 | 0° |
| B | a weaker tint: `loss` at 6 % opacity instead of 20 % | 6 % opacity (now 20 %) | 4.51:1 | 14 points | 0° |

Other pairs the same change touches (ratio now → with each option; ✗ is under the bar):

| Pair | Seen in | Now | A | B |
|---|---|---|---|---|
| `text-loss on bg-surface` | page background (Filters, menu bar) | 5.13 ✓ | 6.51 ✓ | same |
| `text-loss on bg-panel` | panel body (every page) | 4.72 ✓ | 5.99 ✓ | same |
| `text-loss on bg-loss/10` | error banners (Monitor, Details) | 4.34 ✗ | 5.29 ✓ | same |
| `text-loss on bg-loss/15` | Badge bad and form errors | 4.10 ✗ | 4.90 ✓ | same |
| `text-loss on bg-loss/20` | request draft chip | 3.85 ✗ | 4.51 ✓ | 4.51 ✓ |
| `text-white on bg-loss` | Close / Delete confirm buttons | 3.67 ✗ | 2.89 ✗ | same |

## Exempt

- #8 `text-slate-500 on bg-slate-800`: a disabled control, exempt from WCAG contrast; no candidates (see the baseline).
