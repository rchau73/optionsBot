# United States — regulatory study

> **Not legal advice.** Preparatory analysis by the legal-counsel skill for licensed US counsel. Confidence: **high / medium / low**; **verify** = may have changed after 2023 or the exact provision is uncertain.

## 1. Summary for counsel

| Question | Preliminary reading | Confidence |
|---|---|---|
| What are BTC/ETH options? | BTC and ETH are treated by the CFTC as **commodities**; options on them are **commodity options / commodity interests** under the Commodity Exchange Act (CEA). | high (BTC), medium (ETH) |
| Is the hosted service regulated? | Directing a client's commodity-interest account or advising for compensation is acting as a **Commodity Trading Advisor (CTA)** (CEA §1a(12)), requiring CFTC registration and NFA membership unless exempt. | medium-high |
| Exemptions? | CFTC Reg. 4.14(a)(10): advice to ≤ 15 persons in 12 months without holding out to the public — **verify current status and conditions**; 4.14(a)(9): advice not tailored to particular clients (e.g. publications, possibly software sold generally). | low-medium (verify) |
| Securities law? | If the instruments are commodities, the Advisers Act is not triggered by them; SEC jurisdiction would arise only for crypto assets that are securities. | medium |
| Can US persons use Deribit? | Deribit's terms have excluded US persons (verify current terms after Coinbase's acquisition of Deribit in 2025 — verify). Offering US persons access to an unregistered foreign derivatives venue is itself a CFTC enforcement risk. | medium (verify) |
| Privacy? | No federal general privacy law; state laws (California CCPA/CPRA and others) apply above thresholds; GLBA if a financial institution. | high |
| Advertising? | FTC Act §5 (unfair/deceptive); for CTAs, NFA Rule 2-29 (communications with the public; hypothetical performance must carry the prescribed disclaimer — CFTC Reg. 4.41). | medium |

**Recommendation:** exclude US persons from the hosted service, the testnet trial and the waiting list until US counsel opines; geofence and ask residence at signup (L5 fallback). *(high as a conservative position)*

## 2. Questions for US counsel (beyond L5, L7, L17)

- US1. Is licensing configurable trading software that the customer runs on their own account, without personalised advice, within an exemption from CTA registration (4.14(a)(9) or otherwise)?
- US2. Does the CFTC consider facilitating US persons' trading on a non-US crypto-derivatives venue as aiding a violation, even via software?
- US3. Which state privacy laws apply at a small scale (thresholds), and do any require a privacy notice before the first user?
