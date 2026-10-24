# European Union — regulatory study

> **Not legal advice.** Preparatory analysis by the legal-counsel skill for a licensed EU lawyer. Confidence: **high / medium / low**; **verify** = may have changed after 2023 or the exact article is uncertain. Questions L1–L24: `docs/product/legal-brief.md`.

## 1. Summary for counsel

| Question | Preliminary reading | Confidence |
|---|---|---|
| Are BTC/ETH options financial instruments? | Yes: MiFID II Annex I Section C covers options on commodities and "other" underlyings (C(10)), and ESMA has stated that crypto-asset derivatives are financial instruments. They are therefore **outside MiCA** (MiCA art. 2(4)(a) excludes crypto-assets that qualify as financial instruments). | high (MiCA exclusion) / medium (classification) |
| Is the hosted service regulated? | Discretionary trading for a client is **portfolio management** (MiFID II Annex I Section A(4)); personal recommendations are **investment advice** (A(5)). Both need authorisation as an investment firm (art. 5). | medium-high |
| Does trading through the customer's own key change it? | No: the test is discretion over a client's portfolio, not who holds the money. | medium |
| Reverse solicitation? | MiFID II art. 42 allows a third-country firm to serve an EU retail client only at the client's own exclusive initiative; ESMA reads it narrowly and any marketing in the EU defeats it. Not a business model. | high |
| Retail protections? | Appropriateness/suitability (art. 25), product governance and target market (art. 16(3), 24(2)), costs disclosure, best execution, record-keeping and telephone/electronic communications recording (art. 16(7)). | high |
| Data? | GDPR fully; ePrivacy (cookies/analytics) via national laws; transfers outside the EEA need adequacy or SCCs. | high |
| AI? | AI Act (Reg. 2024/1689): a help chatbot has transparency duties (art. 50 — users must know they interact with AI; applies from 2 Aug 2026 — verify); credit scoring is high-risk, investment advice tools are not listed as high-risk per se. | medium |
| Operational resilience? | DORA (Reg. 2022/2554) applies to authorised financial entities and their critical ICT providers; we would fall under it only once authorised or as a critical ICT provider to one. | medium |

**Recommendation:** no EU customers for any hosted service until authorised or partnered with an EU investment firm. A pure software licence (customer operates it) is generally outside MiFID if the vendor gives no personal recommendation and exercises no discretion — confirm per member state (some regulators, e.g. BaFin, look closely at "trading bots"). *(medium)*

## 2. Points to brief counsel on

- **MiCA (Reg. 2023/1114):** relevant only for spot crypto services; our product doesn't exchange or custody crypto. If a future product touches spot (e.g. hedging with BTC), MiCA's CASP regime (art. 59 ff., portfolio management on crypto-assets art. 81 — verify) applies.
- **PRIIPs (Reg. 1286/2014):** a KID is required when packaged retail products are made available to EU retail investors; exchange-traded options sold by the venue may need one — Deribit's matter, but marketing its options to EU retail touches it. *(low; verify)*
- **ESMA product intervention:** binary options banned and CFDs restricted for retail (now national measures). Options are not CFDs, but national regulators may restrict crypto derivatives for retail. *(medium; verify per state)*
- **Marketing:** MiFID art. 24(3) fair, clear, not misleading; past performance rules in Delegated Reg. 2017/565 art. 44 (at least 5 years or since inception, 12-month periods, "past performance is not a reliable indicator", net of fees; simulated past performance only under strict conditions; forecasts not based on simulated past performance). Testnet figures are neither past nor valid simulated performance. *(high on the article; medium on application)*
- **GDPR specifics:** controller established outside the EU but targeting EU users must appoint an EU representative (art. 27); DPIA likely (art. 35: systematic processing of financial behaviour); breach notification within 72 h (art. 33); data subject rights (art. 15–22), including the right not to be subject to solely automated decisions with legal or similarly significant effects (art. 22) — trading decisions are made **for** the user at their request under contract, which art. 22(2)(a) allows with safeguards. *(medium)*
- **ePrivacy:** non-essential cookies and analytics need prior consent; first-party, strictly-necessary or audience-measurement exemptions vary by member state (CNIL allows some measurement without consent). *(medium)*

## 3. Questions for EU counsel (beyond L3, L4, L7, L12, L13–L16, L19)

- EU1. Is a software licence of a configurable options strategy, with remote parameter updates by the vendor, "portfolio management" or "investment advice" in [target member state]?
- EU2. Which member state is the best home for an authorisation (or partner) given a small team, and what capital and staff does a portfolio-management licence need (MiFID/IFR: initial capital typically €75k–€150k for firms not dealing on own account — verify)?
- EU3. Do we need a GDPR art. 27 representative before the first EU user, including testnet trial users?
