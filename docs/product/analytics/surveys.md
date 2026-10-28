# In-product surveys (E13)

**Status:** content and data rules (#251, #297). Nothing is on screen yet: the survey cards and the exit step wait for E13's prototype. The events they send are already in the event plan (`event-plan.md` §2 and §4) and the collector's allow-list (`frontend/lib/analytics/plan.js`), so the collector accepts them. The server side is built (#363): `POST /api/survey` stores an answer's codes and free text in `auth.db` beside the user and sends the codes-only event (§5). The screens wait for E13's prototype. **No survey ships before the legal review in §6.**

**Rule in one line:** every reason a person stays or leaves is recorded as a **code**, so the weekly report can count it. A survey never asks about amounts. It is never timed to flatter the score. Leaving is never delayed by a question.

Three surveys:

| Survey | `survey_id` | What it answers |
|---|---|---|
| NPS with one coded why | `nps` | Would people recommend the monitor, and the main reason for the score (event plan Q8) |
| The four answers | `answers` | Which of E3's four investor answers people come for, and which one is hardest to find (Q4, Q8) |
| Exit survey | `exit` | Why a person leaves (Q8; PRD §7 retention: every cancellation gets a coded reason) |

## 1. NPS and its why

**Wording** (one card, three steps; steps 2 and 3 are optional):

| Step | PT-BR | EN | Answer | Stored |
|---|---|---|---|---|
| 1. Score | "De 0 a 10, quanto você recomendaria este monitor a um amigo que investe?" | "From 0 to 10, how likely are you to recommend this monitor to a friend who invests?" | 0 … 10, one tap | `score` (event) |
| 2a. Why, score 9–10 (choice) | "O que fazemos de melhor?" | "What do we do best?" | one *what works* code of §4 | `reason_code` (event) |
| 2b. Why, score 0–8 (choice) | "O que devemos melhorar primeiro?" | "What should we improve first?" | one *to improve* code of §4 | `reason_code` (event) |
| 3. Anything else (text) | "Quer contar mais? (opcional)" | "Anything you want to add? (optional)" | free text, ≤ 500 characters | `auth.db` only (§5), never an event |

Step 2 asks one question by score (product owner on #164, 2026-10-28: for a high score the old list was all complaints, and "we should know what we are doing well too"). For **9–10** it asks what we do best and shows only the *what works* codes. For **0–8** it asks what to improve first and shows only the *to improve* codes. Both lists end with `other`. The weekly report counts the two lists apart, so what works is tracked like what doesn't.

**Trigger and frequency cap:**
- Shown only to a person who said yes to analytics (event plan §5). Without that consent nothing is shown, so nothing can be sent.
- **Where (product owner, 2026-10-28):** inside the assistant panel, as an optional card at the top of the chat, marked "From the team · optional · once a month". It is never a card on the monitor's pages, never a modal, never on `/login`, and never while a manual close or a rebalance dialog is open. While it is due, the Ask button carries a static "1" badge (it never moves or blinks), and the notification bell lists it under *Needs you*. The assistant works the same whether or not it is answered. Prototype: https://claude.ai/artifact/BB9X65gAbafTCJWdgusQdm (board "Monthly NPS in the assistant").
- **Never in the first 14 days** after the account is created. **Only after ≥ 3 sessions.**
- **At most once per 30 days per person** (product owner, 2026-10-28; was 90), counted from the day it was *shown*: the day the person first opens the panel with the card in it, whether answered or not. The badge alone does not count as shown. The date is kept on the user's row in `auth.db`, not in the browser, so another device does not show it again.
- **Not right after a loss-making week is shown:** the card is postponed to a later session if the latest full UTC week (Monday–Sunday) on the monitor's performance view was a net loss. The page reads that sign from `/api/performance?range=7d`. Only the sign is used, and it is never sent or stored. Apart from this rule, the timing never depends on results: it is not shown after a winning week on purpose, and not after a good trade.
- **Always dismissible:** a close button (×), "Not now" and Escape. Dismissing counts as shown for the 30-day cap, so a person who closes the card is not asked again the next day. **"Don't ask me again"** turns NPS off for that person (kept on their user row; they can turn it back on in Account › Privacy).
- **The score with monthly answers:** the same person may answer every month. A score over a window counts each person's latest answer in it, so frequent answerers do not weigh more.

**Events:** `survey_shown {survey_id: nps}` when the card appears; `survey_answered {survey_id: nps, score, reason_code}` when the person sends at least a score; `survey_dismissed {survey_id: nps}` when the card is closed without a score. A text from step 3 never goes into the event.

## 2. The four answers (two choice questions)

These are the "why" choice questions tied to E3's four investor answers: what it holds (`holding`), making or losing (`income`), worst case (`worst_case`), why the last trade (`last_action`).

**Wording** (one card, two optional questions):

| Question | PT-BR | EN | Answer | Stored |
|---|---|---|---|---|
| Came for | "O que você veio ver hoje?" | "What did you come to check today?" | one of: o que tenho / what it holds · se ganho ou perco / making or losing · o pior caso / the worst case · por que a última operação / why the last trade · nada disso / none of these | `came_for` (event) |
| Hardest to find | "O que foi mais difícil de achar?" | "What was hardest to find?" | the same five options | `hardest_to_find` (event) |

**Trigger and frequency cap:** the same consent, 14-day and ≥ 3-session rules as NPS. At most once per 30 days per person. Never in the same session as the NPS card, and never within 7 days after it. Each question is optional, and the card is always dismissible.

**Events:** `survey_shown {survey_id: answers}`; `survey_answered {survey_id: answers, came_for, hardest_to_find}` with what was answered; `survey_dismissed {survey_id: answers}`.

## 3. Exit survey

**When:** a person deletes their account, or switches a bot off for good. (Neither flow exists on the monitor yet. The exit survey is part of the story that builds each flow.)

**Frequency cap:** once per leaving, on that flow's final screen only. It is never asked again afterwards, and never as a reminder or an e-mail.

**Skipping it is one click, and it never delays or blocks leaving:**
- The question is on the same screen as the final confirmation, below the button that confirms leaving. It is never a step before that button.
- The confirm button works with the question unanswered. There is no "are you sure you don't want to tell us?" and no second prompt.
- "Pular / Skip" is one click and leaves at once. So does closing the screen.
- Leaving goes ahead even if the survey answer cannot be stored. The answer is sent without waiting, and a failure is never shown to the person.

**Wording** (one optional question, and a text box for `other`):

| Step | PT-BR | EN | Answer | Stored |
|---|---|---|---|---|
| 1. Reason (choice) | "Antes de sair: qual o principal motivo? (opcional)" | "Before you go: what is the main reason? (optional)" | one exit code of §4 | `reason_code` (event) |
| 2. Other (text) | shown only for `other`: "Conte em poucas palavras (opcional)" | "Tell us in a few words (optional)" | free text, ≤ 500 characters | `auth.db` only (§5), never an event |

The exit question shows the exit codes only, never the promoter codes.

**Events:** `survey_shown {survey_id: exit}`; `survey_answered {survey_id: exit, reason_code}`; `survey_dismissed {survey_id: exit}` on Skip or close. These events are sent only for a person who said yes to analytics. A person who said no still sees the question; their coded reason is not stored until §6 settles how (counsel question C3): `POST /api/survey` answers 403 for them and stores nothing, which the screen ignores (leaving goes ahead).

**Open point (account deletion):** deleting a user also deletes all their events in the same step (`AuthService.deleteUser`, event plan §5). As built today, an exit answer from a deleted account is therefore deleted with it, and so is its free text. The proposal for the build story, pending counsel (C4): the deletion step adds one to an anonymous weekly counter `(ISO week, reason_code)`, with no `user_id`, no session and no time finer than the week, and then deletes the user's events and text as now. Until then, only a bot switched off for good (the account stays) keeps its exit answer.

## 4. Reason codes

One enumeration, `reason_code`, in the event plan's allow-list. The weekly review reads the counts per code. The top codes become roadmap items.

| Code | PT-BR | EN | Asked in |
|---|---|---|---|
| `value_unclear` | não vejo o valor | I don't see the value | NPS (to improve), exit |
| `losses` | risco ou perdas | risk or losses | NPS (to improve), exit |
| `too_complex` | difícil de entender | too hard to understand | NPS (to improve), exit |
| `price` | preço | price | NPS (to improve), exit |
| `trust` | não confio no que vejo | I don't trust what I see | NPS (to improve), exit |
| `switched` | mudei para outro produto | switched to another product | exit |
| `hard_to_find` | difícil achar o que preciso | hard to find what I need | NPS (to improve) |
| `missing_feature` | falta algo que eu preciso | something I need is missing | NPS (to improve) |
| `steady_income` | renda constante | steady income | NPS (what works) |
| `easy_to_follow` | fácil de acompanhar | easy to follow | NPS (what works) |
| `saves_time` | me poupa tempo | saves me time | NPS (what works) |
| `clear_risk` | vejo o risco com clareza | I see the risk clearly | NPS (what works) |
| `explains_trades` | explica por que operou | it explains why it traded | NPS (what works) |
| `trust_numbers` | confio nos números | I trust the numbers | NPS (what works) |
| `helpful_answers` | o assistente responde minhas dúvidas | the assistant answers my questions | NPS (what works) |
| `other` | outro | other | NPS, exit (exit: opens the text box) |

The collector checks each property on its own. It does not refuse a promoter code on an exit answer. The weekly report counts such a line under `other` and logs it, rather than guessing.

## 5. Stored fields

**In analytics events** (`data/monitor/events.jsonl`, the event plan's envelope plus these properties):

| Event | Properties | New in the allow-list (#297) |
|---|---|---|
| `survey_shown` | `survey_id` | `survey_id` |
| `survey_answered` | `survey_id`, `score`, `reason_code`, `came_for`, `hardest_to_find` | `score`, `reason_code`, `came_for`, `hardest_to_find` |
| `survey_dismissed` | `survey_id` | — |

Every one is an enum: `survey_id` (`nps`, `answers`, `exit`), `score` (the strings `0` to `10`), `reason_code` (§4), and `came_for` and `hardest_to_find` (`holding`, `income`, `worst_case`, `last_action`, `none`). None can hold an amount, a secret or free text. `tests/event_plan_test.go` and `frontend/__tests__/events-plan.test.js` check this against the plan, and `tests/surveys_doc_test.go` checks this file against the plan. An event with any other property, or a value outside its enum, is refused whole (400, nothing stored).

**In `auth.db`, beside the user** (as built, #363):
- Each answer in the `survey_answers` table: the user's row id, `survey_id`, the codes (JSON of the survey's fields above), the free text of NPS step 3 or the exit `other` box (`free_text`, ≤ 500 characters, optional) and `created_at` (Unix seconds). Written by `POST /api/survey` (`frontend/README.md` "Analytics"), which checks the survey id, every code and the text against this file (`frontend/lib/analytics/surveys.js`: each survey's own codes — an exit answer with a promoter code is refused here, unlike the collector — and text only where §1 or §3 has a box) and refuses anything else whole (400, nothing stored). The `survey_answered` event goes through the collector's allow-list with the codes only. The text is never in an event or a log line.
- **Consent:** the route takes an answer only from a person who said yes to analytics, for every survey: §1 and §2 are shown only then, and §3 stores no exit reason of a person who said no until counsel answers C3.
- Deleted with the user in the same step (`AuthService.deleteUser`: `survey_answers_deleted`, or an ERROR log and `survey_answers_delete_failed`; rows left without a user are deleted when the monitor next starts). Exported with the person's events on an access request (`analytics-user.mjs export`, counts printed only). `cmd/usage` reads events only and never this table (`tests/usage_free_text_test.go`). Read by admins only, in the weekly review.
- Not built yet: a survey's last-shown date, per survey, for the frequency caps of §1–§3 (the screens' story).

**Never stored:** an amount, a position, the P&L sign used by the NPS timing rule, the text typed in any other field, the time spent on a question.

## 6. Data protection — **needs legal review**

> **Needs legal review (LGPD/GDPR).** Not legal advice. The legal brief (`docs/product/legal-brief.md`) and the data-protection programme (`docs/legal/data-protection.md` §2: surveys and the exit survey on consent, 24 months or anonymised) apply. The free text is personal data and goes in the privacy policy draft (`docs/legal/drafts/politica-privacidade.md`). **No survey ships before counsel answers the questions below.**

**Consent text** (draft, shown once with the first survey, *Yes* and *No* equally prominent):
- PT-BR: "Suas respostas nos ajudam a melhorar o monitor. Guardamos a nota e o motivo escolhido, nunca valores ou posições. Um texto que você escrever fica junto da sua conta e é apagado com ela. Você pode mudar de ideia a qualquer momento no menu da conta."
- EN: "Your answers help us improve the monitor. We keep the score and the reason you choose, never amounts or positions. Any text you write is kept with your account and deleted with it. You can change your mind at any time in the account menu."

**Retention and deletion** (proposal):
- Coded answers are analytics events, so the event plan's 13 months apply (`EVENT_RETENTION_DAYS`), and they are deleted with the user.
- Free text is kept 24 months (`data-protection.md` §4), or until the user is deleted, whichever comes first.
- A deletion or access request covers both: deleting the user removes the answers in the same step, and the admin CLI's export carries them with the events (`frontend/README.md` "Analytics", #363). The 24-month limit on free text is not enforced by a job yet.

**Questions for counsel:**
- **C1.** Is the analytics consent enough for the coded answers (they are analytics events), or do surveys need their own consent, given `data-protection.md` puts surveys on consent (LGPD art. 7º I, GDPR art. 6(1)(a))?
- **C2.** Can the free text be kept 24 months under that consent? Must the person be told before typing that it is stored with their account?
- **C3.** May the exit question be shown to a person who refused analytics? If so, may their coded reason be stored as an anonymous count (no `user_id`)?
- **C4.** Is an anonymous weekly count `(ISO week, reason_code)` kept after an account is deleted anonymous data under LGPD art. 12 / GDPR recital 26, so that it may outlive the deletion?
- **C5.** Does postponing NPS after a loss-making week raise any consumer-law (CDC) or marketing concern, if NPS figures are ever published?
- **C6.** What notice is needed in the terms or the privacy policy before the first survey, and in which languages?

## 7. How this file is kept true

- `tests/surveys_doc_test.go` checks that each survey has its wording, trigger, frequency cap and stored fields. It also checks that the exit survey says it is skippable in one click and never blocks leaving, that every stored event property is in the event plan's allow-list and holds no amount, secret or free text, and that §6 is marked needs legal review with questions for counsel.
- A new question or code goes into this file and into the event plan (§2 and §4) in the same PR. It then reaches `plan.js` and `internal/usage/plan.go`, which their tests keep equal to the plan.
