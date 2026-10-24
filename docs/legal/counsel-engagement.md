# Engaging counsel — what to send, ask and get back

> **Not legal advice.** How to turn this pack into a written opinion quickly. Names, quotes, fees and the engagement letter go in the gitignored `data/product/legal/`, never here.

## 1. Who to hire

| Need | Profile | When |
|---|---|---|
| Brazil — capital markets and crypto | Firm or lawyer with **CVM** practice (portfolio managers, robo-advisers, foreign intermediaries) **and** crypto-asset regulation (Lei 14.478/2022, Banco Central VASP rules) | **First**: Brazil is the first market |
| Brazil — consumer, contracts, LGPD | Often the same firm (digital/consumer practice) | Same engagement |
| EU, US, UK | Only when a market is chosen (PRD §11); start with a short memo on L3–L5 | Later |
| Tax | Accountant/tax lawyer for our company (ISS, federal taxes, remittances) and customer statements | Before charging anyone |

## 2. Scope of work to request (Brazil, phase 1)

1. **Written opinion** on L1, L2, L6, L8, L9, L17 (Brazil), L19–L21, L23, L24 and B1–B6 (`jurisdictions/brazil.md` §10): enquadramento of (a) the hosted service, (b) the software licence, (c) the testnet trial.
2. **Structure recommendation**: licence now vs partnership with an authorised *gestora* vs our own registration; what each requires, the timeline and the residual risks.
3. **Review and approve the drafts** in `drafts/` for the chosen structure (terms, risk statement, privacy policy, API-key authorisation, testnet agreement, disclaimers).
4. **Marketing rules** (L7, L9): what the landing page, social media and the waiting list may say.
5. **LGPD**: confirm the data map and retention (`data-protection.md`), the need for an *encarregado*, and the transfer mechanism per provider.
6. **Compliance calendar**: obligations and dates once we operate (reports, renewals, records, incident duties).

## 3. What to send (all in this repository unless noted)

- `docs/product/legal-brief.md` (facts F1–F11, questions L1–L24) and `docs/legal/preliminary-answers.md`.
- `docs/legal/jurisdictions/brazil.md`, `risk-register.md`, `data-protection.md`, `audit-and-records.md`, `drafts/`.
- `docs/product/PRD.md` §1–3, §6, §11–12 and `OptionStrategy.md` (rules in plain words, §8 risk controls, §11 lessons).
- A 30-minute walk-through of the monitor on testnet: the journal and the reason for each trade, the kill switch, roles and audit (#170/#171), the worst-case view (E3 prototype).
- Deribit's current terms of use and API terms (download the version of the day; keep it in `data/product/legal/`).

## 4. What we need back

- One answer per question ID (L1–L24, B1–B6), each **yes / no / conditions**, with the rule relied on and the confidence, so we can update the PRD gates.
- Approved versions of the drafts (or redlines), versioned.
- A list of what must exist **before** the first external tester and **before** the first paying customer.

## 5. After the answers

The PM records each decision in PRD §11 and §12 by pull request, unblocks the gated epics (legal brief §4), and the legal-counsel skill updates `preliminary-answers.md` ("Counsel (date): …") and the risk register.
