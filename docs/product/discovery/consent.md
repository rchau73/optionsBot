# Consent — data use (E5 survey)

**Status:** draft, **needs legal review** before the survey goes to anyone outside the team (PRD §11, data protection). It states the LGPD (Brazil) and GDPR (EU) basics as we understand them; it is not legal advice.

**Where data goes:** the survey answers, the contacts left for the waiting list and every export stay only in `data/product/interviews/` (gitignored, never committed, never pasted into an issue, a pull request, an AI tool outside our own machine or a shared drive without access control). The one outside service that holds them is the form tool, under the rules of §2, until the survey closes. Public documents — `synthesis.md` filled in, the PRD — carry only anonymous IDs (`P01`, `P02`…), no names and no amounts.

No calls and no recordings: the survey is a written form only (`survey.md`).

## 1. What the participant reads (the form's first screen)

They tick "I agree" to continue; the form keeps the tick with the date.

> Thank you for taking part. This is research: we want to learn how people who hold crypto have tried to earn on it. It takes about 6 minutes. We are not selling anything and nothing here is investment advice.
>
> - **What we keep:** your answers. We never ask your name or how much you hold; if you write an amount we leave it out of anything we share.
> - **Who sees it:** only the people running this research. What we share inside the team or publish is anonymous: a code instead of you, no amounts, no detail that could identify you.
> - **How long:** answers are kept for 12 months at most, or less if you ask.
> - **Your rights:** you can ask to see, correct or delete your answers, or withdraw this consent, at any time, by writing to the contact on this page. Withdrawing changes nothing else.
> - **Waiting list:** at the end we describe an idea and offer a waiting list. Joining is optional and separate from this survey; the e-mail you leave there is the only way we would contact you again.
>
> ☐ I agree and I am 18 or older.

## 2. What we do with the data

| Item | Purpose | Lawful basis (to confirm) | Kept |
|---|---|---|---|
| Survey answers | synthesis (`synthesis.md`) | consent | in the form tool until the survey closes, then in `data/product/interviews/` ≤ 12 months |
| Waiting-list e-mail (F1) | telling them when a trial exists | separate consent, given at F1 | until they leave the list |

- Pick a form tool whose terms keep the answers private (no training on them, no sale), limit access to the people running the research, and turn off collecting sign-ins, e-mails and IP addresses by default; note the tool used.
- When the survey closes, export the answers, then delete them from the form tool.
- Store exports encrypted at rest (an encrypted disk counts); no copies on personal phones.
- On a deletion request: delete the answers and the e-mail, keep only anonymous lines already in a published synthesis, and note the date of deletion.

## 3. Open points for the legal review

- Is consent the right basis under LGPD and GDPR for a research survey, and is a ticked box enough?
- Transfers abroad: the form tool, participants and the team may be in different countries (Brazil, EU, elsewhere); what do we need for that?
- Is 12 months reasonable, and who is the named controller and contact for requests?
- Does the fake-door text at the end count as marketing, and does it need its own wording or opt-in?
