**DRAFT for counsel (legal brief L5, L7, L9): do not send any text below to anyone until counsel approves it in writing.**

# Outreach kit: one invitation per channel (E5 survey)

**Status:** draft, **needs legal review** (marketing wording to third parties). Publishing the survey and choosing the channels stay with the product owner (#178). Part of the E5 discovery kit: `outreach.md` (the invitation) → `consent.md` (the form's first screen) → `screener.md` (who counts) → `survey.md` (the questions) → `synthesis.md`.

**Where answers go:** answers, names and contacts stay only in `data/product/interviews/` (gitignored). This file holds no name, no contact, no price and no answer.

## 1. Channel codes

The survey link carries the channel code, so `survey_tally.py` can split the qualified answers by channel (its `CHANNEL` column, "Quota progress"). The code is exactly the value the form stores in `CHANNEL`: lowercase, short, never a person's name.

| Code | Link suffix | Where it is sent |
|---|---|---|
| `community` | `?channel=community` | a public crypto or long-term-holder community the product owner belongs to, with the moderators' permission |
| `direct-message` | `?channel=direct-message` | a one-to-one message to a person the product owner already knows who fits `screener.md` §1 |
| `referral` | `?channel=referral` | a person who passes the invitation on to someone they know who fits |
| `waiting-list` | `?channel=waiting-list` | people who already asked, on their own, to hear from us |

Each text below has `<survey link>` where the form's address goes, with the suffix of its row added. The consent text is the form's first screen, `consent.md` §1.

## 2. What every text says

- Who we look for: someone who has held BTC or ETH through at least one full market cycle (`screener.md` §1).
- How long: about 6 minutes (`survey.md`), a written form, nothing else.
- What the answers are for: product research only.
- Where to read how the data is used before answering: the consent text.
- What the product is not: one honest sentence, in each language.

## 3. community

### English

> Hi. We are researching how people who hold BTC or ETH for the long term have tried to earn on their coins. If you have held through at least one full market cycle, we would value 6 minutes of your time: a short written form, no calls.
>
> Your answers are used for product research only. Read how we handle them first: [consent text](consent.md). Form: <survey link>?channel=community
>
> To be clear: this is not investment advice, it promises no returns, and we are not selling anything yet.

### Português (PT-BR)

> Olá. Estamos pesquisando como quem guarda BTC ou ETH a longo prazo já tentou ter renda com suas moedas. Se você segurou pelo menos um ciclo completo de mercado, gostaríamos de 6 minutos do seu tempo: um formulário escrito curto, sem ligações.
>
> As respostas servem só para pesquisa de produto. Veja antes como tratamos os dados: [texto de consentimento](consent.md). Formulário: <survey link>?channel=community
>
> Para ficar claro: isto não é recomendação de investimento, não promete retorno algum e ainda não vendemos nada.

## 4. direct-message

### English

> Hi, I am researching how long-term BTC or ETH holders have tried to earn on their coins, and I thought of you. If you have held through at least one full market cycle, would you fill in a written form of about 6 minutes?
>
> Answers are used for product research only; how we handle them is here: [consent text](consent.md). Form: <survey link>?channel=direct-message
>
> This is not investment advice, it promises no returns, and nothing is for sale yet.

### Português (PT-BR)

> Oi, estou pesquisando como quem guarda BTC ou ETH a longo prazo já tentou ter renda com suas moedas e lembrei de você. Se você segurou pelo menos um ciclo completo de mercado, pode preencher um formulário escrito de cerca de 6 minutos?
>
> As respostas servem só para pesquisa de produto; o tratamento dos dados está aqui: [texto de consentimento](consent.md). Formulário: <survey link>?channel=direct-message
>
> Isto não é recomendação de investimento, não promete retorno algum e ainda não há nada à venda.

## 5. referral

### English

> A friend of ours is researching how long-term BTC or ETH holders have tried to earn on their coins. If you have held through at least one full market cycle, the form takes about 6 minutes and is written only. If you know someone else who fits, you are welcome to pass this on.
>
> Answers are used for product research only: [consent text](consent.md). Form: <survey link>?channel=referral
>
> This is not investment advice, it promises no returns, and nothing is for sale yet.

### Português (PT-BR)

> Um conhecido nosso está pesquisando como quem guarda BTC ou ETH a longo prazo já tentou ter renda com suas moedas. Se você segurou pelo menos um ciclo completo de mercado, o formulário leva cerca de 6 minutos e é só escrito. Se conhece alguém que também se encaixa, pode repassar este convite.
>
> As respostas servem só para pesquisa de produto: [texto de consentimento](consent.md). Formulário: <survey link>?channel=referral
>
> Isto não é recomendação de investimento, não promete retorno algum e ainda não há nada à venda.

## 6. waiting-list

### English

> You asked to hear from us, so here is a short request. We are researching how long-term BTC or ETH holders have tried to earn on their coins. If you have held through at least one full market cycle, would you fill in a written form of about 6 minutes?
>
> Answers are used for product research only: [consent text](consent.md). Form: <survey link>?channel=waiting-list
>
> This is not investment advice, it promises no returns, and nothing is for sale yet.

### Português (PT-BR)

> Você pediu para receber notícias nossas, então vai um pedido curto. Estamos pesquisando como quem guarda BTC ou ETH a longo prazo já tentou ter renda com suas moedas. Se você segurou pelo menos um ciclo completo de mercado, pode preencher um formulário escrito de cerca de 6 minutos?
>
> As respostas servem só para pesquisa de produto: [texto de consentimento](consent.md). Formulário: <survey link>?channel=waiting-list
>
> Isto não é recomendação de investimento, não promete retorno algum e ainda não há nada à venda.

## 7. Rules for any change to this kit

- No performance or yield figure, no promise, no urgency and no incentive (`tests/outreach_kit_test.go` keeps a banned-phrase list in English and PT-BR).
- A new channel needs a row in §1, both texts, and a code the tally accepts.
