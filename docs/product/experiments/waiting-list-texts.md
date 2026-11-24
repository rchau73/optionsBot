# Waiting-list texts for counsel (E5)

> **Needs legal review: not to be sent or published.** Every text below is a draft (#882). None goes to anyone outside the team before counsel has reviewed it against `docs/product/legal-brief.md` and the product owner has approved it. Not legal advice. Nothing here is built or sent.

**What this is.** Every text the fake-door waiting list sends or shows beyond the page itself (`docs/product/landing-copy-draft.md`), in English and Brazilian Portuguese, and one sheet (§2) that says which legal-brief question each text answers, so counsel can review the whole door in one pass.

**Rules these texts keep** (`tests/landing_copy_test.go` checks them, naming the text that breaks one):

- No price, amount or figure; the only number allowed is the age limit 18.
- No return, yield, APY, profit or performance figure, and no word that promises one ("guaranteed", "garantido").
- Sentences of 25 words or fewer, plain words.
- The confirmation e-mail says that nothing is available yet and carries the risk line of the page footer (C9).
- Controller name and contact stay as `[FILL]` (§8 question 6 of `docs/product/experiments/fake-door-landing.md`): a person fills them. No name, e-mail address or amount enters the repository.
- The tool that sends them is not chosen yet (the product owner's decision, after counsel answers §8 questions 1 and 2).

## 1. Texts

### T1. Double opt-in confirmation e-mail

*Sent once, right after the form, to the address given. Needs legal review (§8 question 7: is it marketing, and what must it say?).*

**EN**

> Subject: Please confirm your place on the optionsBot waiting list
>
> You asked to join the optionsBot waiting list. Please confirm by opening this link: [CONFIRM LINK].
>
> Nothing is available yet. This is not an offer, there is no price and we accept no money.
> We are only recording your interest, to tell you when and where a trial becomes available.
> Options can lose more than the premium collected.
> If you did not ask for this, ignore this message and we keep nothing after your link expires.
> You can leave the list or ask to see or delete your data at any time: [LEAVE LINK].
> Controller: [FILL]. Contact: [FILL].

**PT-BR**

> Assunto: Confirme seu lugar na lista de espera do optionsBot
>
> Você pediu para entrar na lista de espera do optionsBot. Confirme abrindo este link: [LINK DE CONFIRMAÇÃO].
>
> Nada está disponível ainda. Isto não é uma oferta, não há preço e não aceitamos dinheiro.
> Só registramos seu interesse, para avisar quando e onde um teste ficar disponível.
> Opções podem perder mais do que o prêmio recebido.
> Se você não pediu isto, ignore esta mensagem e não guardamos nada depois que o link expirar.
> Você pode sair da lista ou pedir para ver ou apagar seus dados a qualquer momento: [LINK PARA SAIR].
> Controlador: [FILL]. Contato: [FILL].

### T2. "You are on the list" line

*Shown after the link in T1 is opened. It repeats the page's C6 and may replace it.*

**EN**

> Thank you, you are on the list. Nothing is available yet, and we will never ask you for money or for your exchange keys through this list. We will write only when a trial exists where you live.

**PT-BR**

> Obrigado, você está na lista. Nada está disponível ainda, e nunca pediremos dinheiro nem suas chaves da corretora por esta lista. Escreveremos só quando existir um teste onde você mora.

### T3. Leave-the-list confirmation

*Shown and sent when someone uses the leave link.*

**EN**

> You have left the waiting list. We will not write to you again about it. We delete your e-mail, country and answers now. We keep only the record that you agreed and then left, as the law requires. Controller: [FILL]. Contact: [FILL].

**PT-BR**

> Você saiu da lista de espera. Não escreveremos mais sobre isso. Apagamos agora seu e-mail, seu país e suas respostas. Guardamos só o registro de que você aceitou e depois saiu, como a lei exige. Controlador: [FILL]. Contato: [FILL].

### T4. Reply to a request to see or delete data

*Sent to a request made through the link in every e-mail (the rights of §8: access, deletion, correction, leaving). Needs legal review of the deadline and of what we must keep.*

**EN**

> We received your request about your data on the optionsBot waiting list. We will answer within the time the law allows: [FILL DEADLINE]. To see your data, we send you your e-mail, country, optional answers, and the date of your consent. To delete it, we erase all of it except the consent record, which we must keep. We use it for nothing else, and we share it with nobody for marketing. Controller: [FILL]. Contact: [FILL].

**PT-BR**

> Recebemos seu pedido sobre seus dados na lista de espera do optionsBot. Responderemos no prazo que a lei permite: [FILL PRAZO]. Para ver seus dados, enviamos seu e-mail, seu país, suas respostas opcionais e a data do seu consentimento. Para apagar, removemos tudo, exceto o registro de consentimento, que devemos guardar. Não usamos os dados para mais nada e não os compartilhamos com ninguém para marketing. Controlador: [FILL]. Contato: [FILL].

### T5. "Not available in your country" line

*Shown instead of the form for a self-declared United States residence. It repeats the page's C7.*

**EN**

> Sorry, this list is not open to people living in the United States. We keep nothing from this visit.

**PT-BR**

> Desculpe, esta lista não está aberta a quem mora nos Estados Unidos. Não guardamos nada desta visita.

## 2. Hand-off sheet

Every user-facing text of the fake door against the legal-brief questions it answers, and the fallback that may ship meanwhile. "Source" is where the text lives.

| ID | Text | Source | Legal | Fallback that may ship meanwhile |
|---|---|---|---|---|
| C1 | Headline | `landing-copy-draft.md` §1 | L1, L7, L19 | Describe the mechanics only, without "income". |
| C2 | Sub-head | `landing-copy-draft.md` §1 | L1, L7, L9, L19 | The licence model's wording (the customer runs the software). |
| C3 | Not-available notice | `landing-copy-draft.md` §1 | L9, L23 | None needed: it is the fallback. |
| C4 | Trial line | `landing-copy-draft.md` §1 | L6, L7, L9 | Remove it and describe the waiting list only. |
| C5 | Call to action | `landing-copy-draft.md` §1 | L9 | "Tell me when it exists". |
| C6 | After sign-up | `landing-copy-draft.md` §1 | L9, L13 | T2. |
| C7 | US exclusion | `landing-copy-draft.md` §1 | L5, L17 | T5. |
| C8 | Consent line | `landing-copy-draft.md` §1 and `fake-door-landing.md` §8 | L13, L15, L5 | Two unticked boxes: "tell me" and "count interest". |
| C9 | Footer, the risk line | `landing-copy-draft.md` §1 | L1, L7, L19, L20 | None: it stays. |
| V1 | A reason for every trade | `landing-copy-draft.md` §2 | L1, L4, L11 | Drop the value points; keep the notice. |
| V2 | The worst case before every risk | `landing-copy-draft.md` §2 | L7, L19 | Drop the value points; keep the notice. |
| V3 | It never touches your funds | `landing-copy-draft.md` §2 | L21, L17, L18, L20 | Drop the value points; keep the notice. |
| T1 | Confirmation e-mail | §1 above | L9, L13, L15, L19 | Send no e-mail: collect nothing until counsel answers. |
| T2 | "You are on the list" line | §1 above | L9, L13 | Show C6. |
| T3 | Leave-the-list confirmation | §1 above | L13, L15 | Answer by hand, from the same words. |
| T4 | Data-request reply | §1 above | L13, L14, L15 | Answer by hand, from the same words. |
| T5 | Not available in your country | §1 above | L5, L17 | Show C7. |

### Open questions of `fake-door-landing.md` §8, each pointing to its text

| Question | Asks | Text | Legal |
|---|---|---|---|
| 1 | Is consent the basis, and is one box enough? | C8, T2 | L13 |
| 2 | Where may the list be stored, with which processors? | T1, T3, T4 | L13, L15 |
| 3 | How long is the list and the consent record kept? | T3, T4 | L13, L14 |
| 4 | Is page-load counting without cookies allowed? | C3 | L15 |
| 5 | Does the country question need its own notice? | C8, T5 | L5, L13 |
| 6 | Who is the named controller and the contact? | T1, T3, T4 | L13 |
| 7 | Does the confirmation e-mail count as marketing, and what must it say? | T1 | L9, L13 |
