# Legal risk register

> **Not legal advice.** Every legal pitfall found so far, with the rule, our rating, the mitigation and what counsel must confirm. Likelihood (L) and impact (I): 1 low – 3 high; score = L × I. Owner: **PO** product owner · **PM** · **Dev** · **Counsel**. Updated by the legal-counsel skill; a mitigation that needs code becomes a ticket.

| # | Pitfall | Rule | L | I | Score | Mitigation | Owner | Counsel confirms |
|---|---|---|---|---|---|---|---|---|
| R1 | Operating customers' real-money accounts before authorisation | Lei 6.385/76 art. 27-E; Res. CVM 21/2021; MiFID art. 5; CEA (CTA) | 2 | 3 | **6** | Internal use only; licence model or authorised partner before any customer money | PO | L1, L4, L5 |
| R2 | Recruiting Brazilians for a foreign derivatives venue (referrals, onboarding, fees from the exchange) | CVM stop-order practice; Parecer CVM 33/2005 | 2 | 3 | **6** | Never refer, never take exchange fees, never call Deribit authorised in Brazil; the customer opens their own account | PO, PM | L17, B-list |
| R3 | Performance figures (testnet, replay, stress) used as a track record | CDC art. 37; CVM/ANBIMA practice; EU Del. Reg. 2017/565 art. 44; CFTC 4.41 | 2 | 2 | 4 | No figures in marketing; in-product labels (`drafts/avisos.md`) | PM | L7 |
| R4 | Liability waivers voided in consumer court; losses claimed from us | CDC art. 14, 25, 51 I | 2 | 3 | **6** | Define the service precisely; market risk disclosed and accepted; exclude third-party failures; reliable kill switch and journal as evidence | PO, Counsel | L20 |
| R5 | Retail customer without the knowledge or capacity to bear losses beyond the premium | Res. CVM 30/2021; MiFID art. 25; CDC art. 6 III | 2 | 3 | **6** | Questionnaire, risk statement and worst case before activation; experienced investors only; defined-risk default before retail | PM, Dev | L19 |
| R6 | API keys leaked (ours or customers') | LGPD art. 46–48; GDPR art. 32–34 | 1 | 3 | 3 | Trade-only keys, never withdrawal; encrypted at rest; never logged or sent to an AI provider; incident plan; pre-commit secret checks (exist) | Dev | L22 |
| R7 | Fee as % of capital or profit makes a licence look like management | Res. CVM 21; MiFID | 2 | 2 | 4 | Flat subscription per account | PO | L8 |
| R8 | AI answer read as personal advice or a promise of returns | Res. CVM 19/2021; MiFID A(5); CDC art. 37 | 2 | 2 | 4 | Assistant rules (no "should I", no forecasts), AI label, logs, flag-and-review | Dev, PM | L10–L12 |
| R9 | Data sent abroad without safeguards (cloud, AI, analytics) | LGPD art. 33; Res. CD/ANPD 19/2024; GDPR ch. V | 2 | 2 | 4 | Processor inventory; SCCs/DPAs; no personal data to AI providers beyond the question | Dev, Counsel | L13, L15 |
| R10 | Exchange bans third-party automation or the customer's residence | Exchange terms | 2 | 2 | 4 | Read terms per exchange; customer confirms; residence checks; clause on open positions | PM | L17, L18 |
| R11 | A customer's adviser acts as manager without registration | Res. CVM 19/21 | 1 | 3 | 3 | Managers are staff only (#185 recommendation) | PO | L24 |
| R12 | UK or US persons reached by marketing | FCA PS20/10 and promotions regime; CEA | 2 | 2 | 4 | Geofence; residence declaration; no ads targeting them | PM, Dev | L5, UK notes |
| R13 | Waiting list / fake door read as an offer | Res. CVM 160/2022; CDC art. 30 (offer binds) | 1 | 2 | 2 | Interest-only wording, no price for an investment service | PM | L9 |
| R14 | Missing records when a regulator, court or customer asks "why did it trade?" | Res. CVM 21 records; CDC burden of proof (art. 6 VIII) | 1 | 3 | 3 | The journal (every decision with reason and market) is append-only and kept ≥ 5 years; add integrity (hash chain) | Dev | L14 |
| R15 | Customer unaware of their tax duties on foreign crypto derivatives | Lei 14.754/2023; IN RFB 1.888/2019 | 2 | 1 | 2 | Terms say taxes are the customer's; annual statement from the journal | PM | B-list |
| R16 | 7-day withdrawal right after positions were opened | CDC art. 49 | 2 | 1 | 2 | Start trading only after the period, or the customer waives start-delay expressly and keeps market results; refund the fee | Counsel | B5 |
| R17 | Strategy change by us harms existing customers (unilateral change) | CDC art. 51 X, XIII | 1 | 2 | 2 | Notice before a rule change, option to stop; changelog per release | PM | L20 |
| R18 | Analytics or session replay capturing amounts or keys | LGPD art. 6 III (necessity); GDPR art. 5 | 1 | 2 | 2 | Event allow-list without amounts (`event-plan.md`) | Dev | L15 |
| R19 | Our own testnet/live trading framed as managing others' money if friends contribute capital | Res. CVM 21; Lei 7.492/86 | 1 | 3 | 3 | Never accept third-party money into our accounts | PO | L23 |
| R20 | Using a brand, logo or "Deribit partner" claim without permission | trademark; exchange terms | 1 | 1 | 1 | Name exchanges factually only | PM | — |
