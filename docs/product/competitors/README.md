# Competitor map — method

How epic E5 maps the market of PRD §4 (#304). This page is public; the research is not. Each competitor is one YAML file in the gitignored `data/product/competitors/`, checked by `scripts/product/competitors.py` (usage in `scripts/product/README.md`). The PRD quotes only the checker's `--summary`.

## Why this shape

Positioning and pricing must never rest on a remembered or invented number. So every fact carries **where it was seen** (`source_url`) and **when** (`seen_on`, `YYYY-MM-DD`); the checker refuses an entry with a fact missing either, and lists every fact seen more than **90 days** ago as stale, to be re-checked before it is quoted.

## Categories (PRD §4)

| `category` | What it covers |
|---|---|
| `exchange_yield` | exchange-native structured yield: dual investment, "earn" products |
| `defi_vault` | DeFi option vaults |
| `copy_trading` | copy-trading and signal platforms |
| `api_automation` | automation platforms that trade the customer's account with an API key |
| `option_analytics` | option analytics tools (no execution) |

A competitor goes in the one category that best describes how its customers use it; a second product of the same company is a second file.

## Fields

A **fact** is a mapping with its content plus `source_url` and `seen_on`. Lists (strengths, tiers…) are mappings **by name**, since the parser has no YAML lists. The fictional `scripts/product/example_competitor.yaml` shows every field.

| Field | Content |
|---|---|
| `name`, `segment` | text: the company/product, and who it sells to |
| `category` | one of the five above |
| `pricing` | fact: `value_metric` (what the price scales with) and `tiers`, each tier a fact with `price`, `currency`, `period` |
| `positioning` | fact: `text` — its own promise, in its words |
| `strengths`, `weaknesses`, `recent_moves` | optional, facts by name with `text` |
| `praise`, `complaints` | optional, what customers praise or hate — facts by name with `text`, sourced to the review, forum or interview note |
| `risk_shown` | fact: `value` yes / partly / no — is the risk shown next to the yield? |
| `against_positioning` | PRD §5's four claims, each a fact with `value` yes / partly / no: `explains_trades` (the reason for every trade), `worst_case_before_risk`, `non_custodial` (never touches the customer's funds), `public_track_record` (full, net of fees) |

Write `"no"` and `"yes"` as plain words; quote text that contains ` #`. An unknown field is refused, so a typo cannot hide a missing one.

## Method

1. **Who fills it:** a person or the PM in a session with web tools. Never from memory: if a fact cannot be found with a source, leave it out.
2. **Sources:** the competitor's own page (pricing, terms, help centre) for prices and claims; public reviews, forums and app stores for praise and complaints; an interview note in `data/product/` for what our interviewees said (`source_url` then names the private file). Prefer an archived copy for prices that change.
3. **Dates:** `seen_on` is the day the fact was read at its source, not the day it was typed.
4. **Re-check:** run the checker before any pricing or positioning decision; re-read every stale fact or drop it.
5. **Judgement fields** (`risk_shown`, `against_positioning`) cite the page they were judged from; "partly" means shown but not beside the yield, or only in the small print.

## What the PRD may quote

Only the output of `competitors.py --summary`: per category, how many competitors, how many show the risk beside the yield, and for each positioning claim whether it is **open** (no competitor of the category makes it) or **contested** (how many do, fully or partly), plus the count of stale facts. It holds no competitor name, price, URL or file path, so it can go into PRD §4 by a PM pull request. Prices, names of private sources and the entries themselves stay in `data/product/`.
