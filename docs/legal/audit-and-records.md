# Audit, records and controls

> **Not legal advice.** What a regulator, auditor, court or customer would ask us to show, what we already have, and the gaps. Prepared by the legal-counsel skill.

## 1. What we must be able to show

| Question someone will ask | Evidence today | Gap |
|---|---|---|
| Why did the software trade (or not) at that moment? | `orders.log` journal: every decision with its rule, numbers and market snapshot (CLAUDE.md "Journal rule") | Integrity: add a hash chain or periodic signed digest so entries can't be altered unnoticed |
| Who changed a parameter or closed a leg? | Audit per person to the bot journal (#171) | Retain 5 years; export for a customer on request |
| Which version of the rules ran on which date? | Git tags, deploy history (`deploys.tsv`), PRs with reviews | Publish a customer-facing changelog of rule changes |
| Were the customer's limits respected (margin, size)? | Margin policy journal entries, `pnl_history.jsonl` | A per-customer monthly statement |
| Did the customer accept the risks and terms? | — | Store the accepted version, date, IP and questionnaire answers (E7) |
| Was the customer eligible (residence, knowledge)? | — | Signup checks and records (E7/E8) |
| Were secrets protected? | Pre-commit hook, gitleaks in CI, `.env` separation | Key encryption at rest (E8); access log of production |
| Were incidents handled? | — | Incident register (`data-protection.md` §7) |

## 2. Records and periods (to confirm with counsel)

- **Trading and journal:** ≥ 5 years after the relationship (CDC limitation; CVM if regulated — Res. CVM 21 requires keeping records for at least 5 years, verify article).
- **Access logs:** 6 months (Marco Civil art. 15).
- **Tax and accounting:** 5 years (CTN art. 173–174) — the company's books.
- **AML (if regulated):** 5 years (Lei 9.613/1998 art. 10, verify).

## 3. Controls an auditor would test (when we are regulated or a partner's supplier)

- **Change management:** every change through a PR with CI and review (exists); trading-rule changes approved by the product owner (exists, PRD §12); stress/replay evidence in the PR (exists for E1).
- **Model governance:** a documented strategy (`OptionStrategy.md`), versioned parameters (`config_*.yaml`, `fixed.go`), back-tests and stress before changes (E9), a kill switch.
- **Segregation:** agents can't merge code, deploy live or trade (AGENTIC.md); a person merges.
- **Access:** roles (E7), MFA for admins (later), production access list.
- **Business continuity:** reconnect, watchdog, restart policy, reconcile at startup (exists); documented recovery.
- **Third parties:** exchange, cloud, AI provider due diligence and contracts.

## 4. Certifications to consider later

SOC 2 Type II or ISO/IEC 27001 when selling to partners (a *gestora* will ask for security evidence); not needed before the first customer.
