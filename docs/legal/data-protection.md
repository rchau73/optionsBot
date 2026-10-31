# Data protection programme (LGPD · GDPR · CCPA)

> **Not legal advice.** Preparatory programme by the legal-counsel skill; counsel and the future *encarregado* (DPO) confirm. Based on the planned data (legal brief F10) and the analytics plan (`docs/product/analytics/event-plan.md`).

## 1. Roles

- **Controller (*controlador*):** the optionsBot company [FILL: legal name, CNPJ] for accounts, the monitor, analytics and support.
- **Processors (*operadores*):** cloud/hosting [FILL], email [FILL], AI provider for E9/E10 [FILL], error monitoring [FILL]. Each needs a data processing agreement (DPA) with security and transfer clauses.
- **The exchange** (Deribit) is an independent controller of the customer's exchange account; we receive trade and margin data through the customer's key.
- ***Encarregado* / DPO:** LGPD art. 41 requires one; small processing agents may have simplified duties (Res. CD/ANPD 2/2022 — verify whether we qualify once we process financial data at scale). GDPR art. 37 requires a DPO only for large-scale monitoring; an EU representative (art. 27) is needed if we target EU users without an EU establishment.

## 2. Data map and legal bases

| Data | Purpose | LGPD basis (art. 7º) | GDPR basis (art. 6) | Retention (§4) |
|---|---|---|---|---|
| Account: username, email, password hash, role | Sign-in, roles, security | V contract | (b) contract | contract + 5 y |
| Sign-in events, IP, failed attempts, lockouts | Security, fraud prevention, Marco Civil logs | II legal obligation (logs) / IX legitimate interest | (c) / (f) | 6 months (access logs), security incidents 5 y |
| Audit log (who closed or rebalanced what) | Accountability, disputes | V contract / IX legitimate interest / VI exercise of rights | (b) / (f) | contract + 5 y |
| Exchange API key (trade-only), encrypted | Run the service | V contract | (b) | until revoked or contract end; deleted at once after |
| Positions, orders, journal, P&L | Run and explain the service; customer's record | V contract / VI exercise of rights | (b) / (f) | contract + 5 y |
| Residence, investor-knowledge questionnaire | Eligibility, suitability-style protection | II legal obligation (if regulated) / V contract | (c) / (b) | contract + 5 y |
| Product analytics (no amounts, no secrets) | Improve the product | IX legitimate interest (Brazil, with balancing test) or I consent | (a) consent (non-essential storage, ePrivacy) | 13 months |
| Inactivity count (#569: people who visited in the 4 weeks before and not this week), from the same consented analytics events | Improve the product; ask why before people leave | as product analytics: only people who said yes, no new data collected | (a) consent | as product analytics; the weekly report holds counts only, never a user id |
| Surveys, exit survey | Understand needs and churn | I consent | (a) | 24 months, or anonymised |
| Support messages, assistant questions and answers | Support; review of AI answers | V contract / IX | (b) / (f) | 24 months |
| Marketing email, waiting list | Contact about the product | I consent | (a) | until withdrawn |

**Not collected:** government IDs (unless a regulated partner requires KYC — then they collect it), payment card data (the payment provider holds it), withdrawal-enabled keys, data of minors (service for 18+ only).

## 3. Principles applied

Purpose limitation and necessity (LGPD art. 6º; GDPR art. 5): analytics events carry no amounts, sizes, prices or secrets; API keys never appear in logs, the journal, analytics, error reports or prompts to an AI provider; the assistant receives only the question and the user's own journal entries it needs.

## 4. Retention

| Category | Period | Why |
|---|---|---|
| Access logs (IP, date, time) | 6 months | Marco Civil art. 15 |
| Trading records and journal | 5 years after the contract ends | evidence for consumer claims (CDC art. 27: 5-year limitation), tax; CVM records if regulated (verify period) |
| Account and audit log | 5 years after the contract ends | same |
| API key | deleted when revoked or at contract end | no further purpose |
| Analytics | 13 months, then aggregated | proportionality |
| Marketing consent records | while consent lasts + 5 years | proof of consent |

## 5. Data-subject rights

Access, correction, anonymisation/deletion, portability, information on sharing, revocation of consent (LGPD art. 18; GDPR art. 15–21). Channel: [FILL: privacy email]; answer within 15 days (LGPD art. 19, II for the full report — verify) / one month (GDPR art. 12(3)). Deletion respects the retention duties in §4. Portability: the journal and P&L export (CSV/JSON) already exist in the monitor's data.

## 6. Security measures (LGPD art. 46; GDPR art. 32)

Hashed passwords (argon2id), short sessions with revocation, lockout (#170); roles enforced on the server (#171); keys encrypted at rest with a key outside the database (E8); least-privilege trade-only keys; TLS; secrets never in git (pre-commit hook and gitleaks in CI); backups encrypted; access to production limited and logged; dependency and vulnerability scanning in CI.

## 7. Incident response

1. Detect and contain (revoke keys, rotate secrets, stop the bots with the kill switch if trading is at risk).
2. Assess risk to data subjects (keys, financial data → high).
3. **Notify:** ANPD and data subjects when the incident may cause relevant risk or damage — **3 working days** (Res. CD/ANPD 15/2024, verify); EU supervisory authority within **72 h** (GDPR art. 33) and data subjects without undue delay if high risk (art. 34); US states per their breach laws.
4. Record every incident (even unnotified) with facts, effects and remedies (GDPR art. 33(5); Res. CD/ANPD 15/2024 record duty).
5. Tell customers to revoke and recreate their exchange keys; the exchange's own security controls (IP whitelist, no withdrawal scope) limit the damage.

## 8. DPIA / RIPD

Recommended before the first external user: systematic processing of financial behaviour, automated decisions taken for the user, a new technology (AI assistant). Template: describe processing → necessity and proportionality → risks → measures (§6) → residual risk → sign-off by the *encarregado*.

## 9. International transfers

Hosting, email or AI providers outside Brazil or the EEA: LGPD art. 33 (standard contractual clauses adopted by Res. CD/ANPD 19/2024 — verify, deadline for adoption in existing contracts) and GDPR ch. V (adequacy, SCCs 2021/914). Keep a transfer register [FILL].

## 10. California and other US states

Only relevant if US persons are served (currently excluded, L5). CCPA/CPRA applies above revenue or volume thresholds (verify current thresholds); provide "notice at collection" and honour access/deletion/opt-out if it ever applies.
