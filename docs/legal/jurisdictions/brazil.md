# Brazil — regulatory study

> **Not legal advice.** Preparatory analysis by the legal-counsel skill for a licensed Brazilian lawyer (OAB) to confirm or correct. Confidence: **high** = stable text known well · **medium** = our reading of the text or of regulator practice · **low** = unsettled or from memory. **Verify** = the text or number may have changed after 2023 or we are not sure of the exact article. Facts F1–F11 and questions L1–L24: `docs/product/legal-brief.md`.

## 1. Summary for counsel

| Question | Our preliminary reading | Confidence |
|---|---|---|
| Are BTC/ETH options on Deribit *valores mobiliários*? | Yes, as derivatives: Lei 6.385/1976 art. 2º, VIII covers "outros contratos derivativos, independentemente dos ativos subjacentes" when publicly offered. The CVM has treated crypto derivatives offered to Brazilian residents as within its remit. | medium-high |
| Is running the strategy on a customer's account, with discretion (F4), regulated? | Yes: deciding trades in securities for someone else is *administração de carteiras de valores mobiliários* (Resolução CVM 21/2021), whether automated or not, and whatever the custody arrangement. Being paid or not doesn't change it (art. 27-E covers "ainda que a título gratuito"). | medium-high |
| Is it criminal to do it without registration? | Potentially: Lei 6.385/1976 art. 27-E (exercício irregular de cargo, profissão, atividade ou função) — detention 6 months to 2 years plus a fine. Operating as an unauthorised financial institution (Lei 7.492/1986 art. 16) is a further risk if the activity is read as intermediation. | medium |
| Is recommending a strategy or parameters regulated? | Professional, personalised recommendation is *consultoria de valores mobiliários* (Resolução CVM 19/2021). Generic, non-personalised analysis may be *análise* (Resolução CVM 20/2021). | medium |
| Is selling or licensing the software regulated? | Licensing software that the customer installs, configures and operates, where the customer decides to run it and the vendor has no access to the account, is generally outside the CVM perimeter. The CVM looks at substance: a vendor that operates the account, sets the parameters or promises results is back inside it. | medium-low (verify CVM practice on "robôs") |
| Is it a virtual-asset service (VASP) under Lei 14.478/2022? | Probably not as long as we never custody, exchange or transfer assets; the derivatives side sits with the CVM, not the Banco Central. The BCB's VASP rules (published 2025) must be read for "intermediação" and for services rendered to residents by foreign providers. | low (verify) |
| Can we solicit Brazilian residents to trade on Deribit? | High risk: the CVM has issued stop orders (*deliberações*) against foreign platforms offering derivatives to the Brazilian public without registration; a Brazilian company recruiting customers for one may be seen as irregular intermediation or participation in an irregular public offering. | medium |
| Consumer law? | Retail customers are consumers (CDC, Lei 8.078/1990; Súmula 297 STJ applies the CDC to financial institutions). Limitation-of-liability clauses are restricted (art. 25 and 51, I), information duties are strict (art. 6º, III; 31), online contracts carry a 7-day withdrawal right (art. 49). | high |
| Data protection? | LGPD (Lei 13.709/2018) applies fully; API keys and trading data are personal data linked to an identified person; financial data is not "sensitive" under art. 5º, II but needs strong security (art. 46). ANPD incident notification within 3 working days (Resolução CD/ANPD 15/2024 — verify). | high / medium |

**Recommended structure today (pending counsel):** no third-party money. If an offer is wanted before authorisation: a **software licence** (fallback of L1) — the customer runs it, holds the key, sets the parameters within published limits and switches it on and off; we never touch the account, never promise results, and show no live performance as a sales argument. **Full product path:** either (a) partner with a CVM-authorised *gestora* (administradora de carteiras) who becomes the regulated operator and contracts the software from us, or (b) register our own *gestora* under Resolução CVM 21/2021 (a *diretor responsável* with the CVM's qualification, compliance and risk officers, policies, records). Option (a) is faster and cheaper to start.

## 2. The instrument

- **Lei 6.385/1976, art. 2º** lists the *valores mobiliários*; incisos VII and VIII bring in "os contratos futuros, de opções e outros derivativos, cujos ativos subjacentes sejam valores mobiliários" and "outros contratos derivativos, independentemente dos ativos subjacentes". BTC/ETH options are derivatives, so the subject matter is in the CVM's competence when there is a public offer or a regulated service. *(high on the text; medium on its application to offshore crypto derivatives)*
- **Parecer de Orientação CVM 40/2022** sets the CVM's view of crypto-assets: a crypto-asset may or may not be a security depending on its characteristics, and the CVM supervises those that are. BTC and ETH themselves are generally not securities; **derivatives on them are**, through inciso VIII. *(medium; verify)*
- **Lei 14.478/2022** (crypto-assets framework) defines *ativo virtual* and excludes from it, among others, instruments already regulated as securities (art. 3º, parágrafo único — verify the inciso). That supports reading crypto derivatives as CVM matter, not BCB VASP matter. *(medium; verify)*

## 3. The activity

### 3.1 Portfolio management — Resolução CVM 21/2021

- *Administração de carteiras* = the professional management of securities portfolios, including the authority to decide on investments on behalf of the client (*gestão*). It requires prior CVM authorisation (pessoa física or jurídica) — **verify the article numbers**.
- **Automated management is still management.** Resolução CVM 21/2021 expressly accepts automated or algorithmic *gestão*; the *diretor responsável* remains responsible for the algorithm's decisions, its governance and its records. *(medium; verify the provision on "gestão por meio de algoritmos")*
- **Custody doesn't matter.** Our key being trade-only and the money staying in the customer's exchange account (F2, F3) helps on custody and VASP questions but does not take discretionary trading out of *administração de carteiras*.
- What registration requires (to plan option (b)): a Brazilian legal entity with a *diretor* responsible for management (certified: CGA/ANBIMA or CVM's experience route — verify) and one for compliance and risk; written policies (risk management, liquidity, personal trading, best execution, *rateio*, conflicts); a reference form (*formulário de referência*) updated yearly; record-keeping for at least 5 years; AML programme. **ANBIMA** adhesion is voluntary but common.

### 3.2 Advice and analysis — Resolução CVM 19/2021 and 20/2021

- *Consultoria*: professional, personalised orientation, recommendation and advice on securities. A manager (E9) recommending parameters to a customer, or an AI analysis tailored to a customer's account (L10), risks falling here. Robo-advisers are covered; the consultant remains responsible.
- *Análise*: reports and recommendations not tailored to a client. Educational content about how the strategy works (E11) is neither, if it doesn't recommend a decision.

### 3.3 Suitability — Resolução CVM 30/2021

Any regulated party (manager, consultant, distributor) must check that the product suits the client's profile (objectives, financial situation, knowledge). Short options with unlimited loss are, in practice, only suitable for qualified or experienced profiles. Even in a licence model, a suitability-style questionnaire and the worst case before activation are a strong defence. *(high on the duty; medium on its reach to a software vendor)*

### 3.4 Offering foreign services to Brazilian residents

- The CVM has repeatedly issued *Deliberações* (stop orders) against foreign brokers and platforms that offer derivatives (forex, CFDs, crypto) to the Brazilian public without registration. A Brazilian company that markets those platforms, onboards Brazilian customers to them, or receives a fee for it is exposed. *(medium; verify recent deliberations naming crypto-derivatives venues)*
- The Parecer de Orientação CVM 33/2005 sets the CVM's position on foreign intermediaries soliciting Brazilian residents. *(medium; verify it is still in force)*
- **Pitfall:** our product only works if the customer has a Deribit account. We must not open accounts for them, send them referral links for a fee, or describe Deribit as authorised in Brazil. Deribit's own terms decide whether Brazilian residents may hold an account (L17).

### 3.5 Public offering and marketing

- **Resolução CVM 160/2022** governs public offerings of securities; mass marketing of an investment opportunity can be read as an offer. Our waiting list and fake-door test (L9) must sell nothing: no price for an investment service, no returns, a clear "not available" notice.
- **Performance claims:** the CVM and ANBIMA expect past-performance figures to be labelled, net of fees, over consistent periods, with "rentabilidade passada não é garantia de rentabilidade futura". Testnet, backtest and stress results are hypothetical and must say so; best practice is not to use them as sales material at all before authorisation (L7). *(medium)*
- **Influencers / "finfluencers":** paid promotion of investment services by unregistered people is under CVM scrutiny. *(medium)*
- **CONAR** (advertising self-regulation) and CDC art. 37 (misleading advertising) apply to all ads.

## 4. Crypto-asset framework (Banco Central)

- **Lei 14.478/2022** created the *prestadora de serviços de ativos virtuais* (VASP): services of exchange between virtual assets and fiat, between virtual assets, transfer, custody or administration, and participation in services related to an issuer's offer (art. 5º — verify). **Decreto 11.563/2023** named the Banco Central as the competent authority.
- **Banco Central resolutions on VASPs** (published in 2025: authorisation, governance, conduct, and foreign-exchange treatment of crypto transactions — **verify numbers, dates and the transition schedule**). Read them for: whether "intermediação" or "administração" of virtual assets covers trading on a customer's account through an API key; whether foreign VASPs serving Brazilians must authorise; obligations of authorised VASPs regarding customers' transfers abroad.
- **Our position (to confirm):** we never exchange, transfer or hold assets; the trades are derivatives under the CVM. So we are outside the VASP perimeter. **Risk:** the regulator may read "administração de ativos virtuais" widely. *(low; verify)*
- **Criminal:** Lei 14.478/2022 added art. 171-A to the Código Penal (fraud with virtual assets) and made VASPs subject to Lei 9.613/1998 (AML). *(high on 171-A; medium on details)*

## 5. Anti-money laundering

- **Lei 9.613/1998** art. 9º lists the obliged persons (financial institutions, securities market participants including *administradores de carteira*, and now VASPs). A software licensor is not listed; a *gestora* is. *(medium)*
- If we become (or partner with) a regulated manager: KYC, PEP checks, sanctions screening, record of transactions, reporting to **COAF**, and an AML policy. **Resolução CVM 50/2021** sets AML rules for the securities market. *(medium; verify)*

## 6. Consumer law

- **CDC (Lei 8.078/1990)** applies to the relationship with retail users (art. 2º–3º; services remunerated). Key effects on our terms:
  - **Information:** clear, adequate information on risks (art. 6º, III; 31) — the risk disclosure must be prominent and in Portuguese.
  - **Liability:** suppliers answer for defects in the service regardless of fault (art. 14); clauses that "impossibilitem, exonerem ou atenuem" liability are void in consumer contracts (art. 51, I; art. 25). A software failure that closes a position badly cannot simply be disclaimed; we can define the service precisely (what the software does and does not do), allocate market risk to the customer (it is inherent in the product chosen) and exclude liability for the exchange and the market, but not for our own defects.
  - **Withdrawal:** contracts concluded outside the commercial establishment (online) have a 7-day withdrawal right (art. 49).
  - **Unilateral changes:** clauses allowing unilateral change of price or terms are abusive (art. 51, X, XIII); changes need notice and the right to terminate.
  - **Forum:** the consumer may sue at their domicile (art. 101, I); a foreign-forum clause won't hold against Brazilian consumers.
- **Decreto 7.962/2013** (e-commerce): identification of the supplier, clear summary of the contract, easy cancellation. *(high)*
- **Professional or qualified investors** may be outside the CDC if the use is not as final consumer — contested; don't rely on it. *(medium)*

## 7. Internet and data

- **Marco Civil da Internet (Lei 12.965/2014):** application providers must keep access logs (IP, date, time) for **6 months** under confidentiality (art. 15); privacy and consent rules for data (art. 7º). *(high)*
- **LGPD (Lei 13.709/2018):** see `docs/legal/data-protection.md`. Main points: legal basis per purpose (art. 7º); contract performance covers running the service; legitimate interest covers security logs (with a balancing test); consent for marketing and non-essential analytics; rights (art. 18); *encarregado* (art. 41; small processing agents have simplified rules — Resolução CD/ANPD 2/2022); international transfers (art. 33; standard contractual clauses — Resolução CD/ANPD 19/2024, verify); security (art. 46) and incident notification (art. 48; Resolução CD/ANPD 15/2024: 3 working days — verify); *RIPD* (art. 38) recommended given financial data at scale.

## 8. Tax (the customer's and ours)

- **Customers:** gains on crypto derivatives held through a foreign exchange are, since **Lei 14.754/2023**, generally taxed as *aplicações financeiras no exterior* at 15 % on annual results (verify the treatment of derivatives and of crypto held abroad). **IN RFB 1.888/2019** requires a resident who trades on a foreign exchange to report monthly transactions above a threshold (verify the threshold and whether derivatives are covered). Our terms must say taxes are the customer's, and the product should give them an annual statement from the journal (a product opportunity, E2/E3).
- **Us:** a software licence or SaaS is subject to ISS (municipal service tax; LC 116/2003 list, item 1.05 licensing or 1.03/1.04 — verify) plus federal taxes; a *gestora* pays ISS on management fees. Remittances abroad (cloud, AI provider) may carry withholding/CIDE — counsel/accountant to confirm.

## 9. Pitfalls specific to Brazil

1. **Running customers' accounts before authorisation** (art. 27-E, Lei 6.385/76) — even free, even testnet with real customers? Testnet uses fake money and no securities change hands; the risk is low but marketing it as "the strategy running on your account" before authorisation should still be cleared (L6).
2. **Recruiting Brazilians for a foreign platform** (CVM stop orders) — never refer, never earn from Deribit.
3. **Liability waivers that a court voids** (CDC art. 51) — write the service precisely instead.
4. **A fee by assets or results** (L8) — reinforces the reading of management and is incompatible with a pure licence. Flat subscription is the safest.
5. **Performance on the landing page** — hypothetical results presented as a track record.
6. **Manager role held by a customer's adviser** (L24) — the adviser needs their own CVM registration; we must not enable unregistered advisers.
7. **LGPD transfers** — cloud and AI providers abroad need transfer safeguards; API keys must never reach an AI provider.
8. **Taxes the customer doesn't know** — a source of complaints; inform and provide statements.

## 10. Questions for Brazilian counsel (in addition to L1, L2, L6–L9, L13–L16, L19–L24)

- B1. Does the CVM's practice on trading robots ("robôs") treat a licensed, customer-operated strategy as outside *administração de carteiras* if the vendor publishes default parameters and updates them remotely? What degree of vendor control tips it?
- B2. Would a partnership with an authorised *gestora* (we license software, they operate the accounts under their registration) be accepted by the CVM, and what must the contract between us contain (responsibility for the algorithm, access to records, change control)?
- B3. Do the Banco Central's VASP rules reach a provider that trades derivatives on a foreign exchange for a resident via API, with no custody?
- B4. What is the minimum disclosure and suitability process for a retail Brazilian customer of a short-options strategy, in the licence model and in the *gestora* model?
- B5. Is a 7-day withdrawal right compatible with a subscription where the strategy may have opened positions in those 7 days; what happens to them?
- B6. How should testnet results, replays of recorded books and stress tests be labelled, if they may be shown at all, to a Brazilian audience?
