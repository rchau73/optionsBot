# Consent — recording and data use (E5 interviews)

**Status:** draft, **needs legal review** before the first interview with someone outside the team (PRD §11, data protection). It states the LGPD (Brazil) and GDPR (EU) basics as we understand them; it is not legal advice.

**Where data goes:** recordings, notes, names and contacts stay only in `data/product/interviews/` (gitignored, never committed, never pasted into an issue, a pull request, a chat with an AI tool outside our own machine or a shared drive without access control). Public documents — `synthesis.md` filled in, the PRD — carry only anonymous IDs (`P01`, `P02`…), no names and no amounts.

## 1. What we tell the participant (read before recording)

Send this in writing before the call and read the short version at the start of it. Ask for a clear "yes" and note it, with the date, in `data/product/interviews/`.

> Thank you for talking to us. This is research: we want to learn how people who hold crypto have tried to earn on it. We are not selling anything and nothing we say is investment advice.
>
> - **Recording:** may we record the audio of this call? It is only so we can take accurate notes. You can say no and we will take written notes instead, or ask us to stop recording at any time.
> - **What we keep:** the recording, our notes, your name and how to contact you. We never ask how much you hold, and if you mention an amount we leave it out of anything we share.
> - **Who sees it:** only the people running this research. What we share inside the team or publish is anonymous: a code instead of your name, no amounts, no detail that could identify you.
> - **How long:** recordings are deleted within 90 days, once the notes are written; notes and contact details within 12 months, or sooner if you ask.
> - **Your rights:** you can ask to see, correct or delete what we hold about you, or withdraw this consent, at any time, by writing to the contact we gave you. Withdrawing changes nothing else.
> - **Waiting list:** at the end we may describe an idea and offer a waiting list. Joining is optional and separate from this interview; it is the only reason we would contact you again.
>
> Is that OK with you, and may we record?

## 2. What we do with the data

| Item | Purpose | Lawful basis (to confirm) | Kept |
|---|---|---|---|
| Recording | accurate notes | consent | ≤ 90 days, then deleted |
| Notes and per-interview sheet | synthesis (`synthesis.md`) | consent | ≤ 12 months |
| Name and contact | scheduling, follow-up questions | consent | ≤ 12 months, or until the participant asks |
| Waiting-list contact (fake door) | telling them when a trial exists | separate consent, given at the end of the call | until they leave the list |

- Store recordings and notes encrypted at rest (an encrypted disk counts); no copies on personal phones or shared cloud folders without access control.
- A transcription or AI tool may process a recording only if it keeps the data private (no training on it) — check its terms first and note the tool used.
- On a deletion request: delete the recording, notes and contact, keep only the anonymous coded lines already in a published synthesis, and note the date of deletion.

## 3. Open points for the legal review

- Is consent the right basis under LGPD and GDPR for research interviews, and is a verbal "yes" on a recording enough, or is a written one needed?
- Transfers abroad: participants and the team may be in different countries (Brazil, EU, elsewhere); what do we need for that?
- Is a 90-day / 12-month retention reasonable, and who is the named controller and contact for requests?
- Does the fake-door description at the end of the call count as marketing, and does it need its own wording or opt-in?
