# Termos de Uso e Licença de Software — optionsBot

> **MINUTA para revisão por advogado(a) — não usar com usuários.** Preparada pela skill legal-counsel no modelo de **licença de software operado pelo próprio usuário** (fallback de L1 em `docs/legal/preliminary-answers.md`). `[ADVOGADO: …]` marca escolhas jurídicas; `[PREENCHER: …]` marca dados a completar. Versão [PREENCHER] · vigente desde [PREENCHER].

## 1. Quem somos e o que este documento é

1.1. Estes Termos regem o uso do software **optionsBot** e do seu painel de acompanhamento ("**Software**"), licenciado por [PREENCHER: razão social], CNPJ [PREENCHER], com sede em [PREENCHER] ("**nós**"), a você, pessoa física maior de 18 anos ou pessoa jurídica ("**você**").

1.2. Ao aceitar estes Termos você declara que leu e aceitou também o **Termo de Ciência de Riscos**, a **Política de Privacidade** e a **Autorização de Uso de Chave de API**, que fazem parte deste contrato.

## 2. O que o Software faz — e o que não faz

2.1. O Software executa, **na sua conta de corretora/exchange e com a chave de API que você cria e nos informa ou configura** ("**Conta**"), uma estratégia de opções publicada e documentada ("**Estratégia**"): venda de *strangles* (uma opção de compra e uma de venda fora do dinheiro) em BTC e ETH, com regras fixas de entrada, dimensionamento, encerramento e proteção descritas em [PREENCHER: link para a documentação pública da Estratégia].

2.2. **Você decide** se e quando ativar o Software, em qual Conta, com quanto capital (depositado por você na exchange), quais parâmetros usar dentro dos limites que o Software permite, e quando pausar ou encerrar. Você pode encerrar posições e revogar a chave diretamente na exchange a qualquer momento, sem depender de nós.

2.3. **Nós não**: (a) recebemos, guardamos, transferimos ou administramos seu dinheiro ou seus criptoativos; (b) temos permissão de saque na sua Conta; (c) fazemos recomendação personalizada de investimento; (d) prometemos ou garantimos rentabilidade; (e) somos corretora, gestora, consultora ou instituição autorizada pela CVM, pelo Banco Central ou por outro regulador [ADVOGADO: confirmar redação conforme o enquadramento final]; (f) temos relação comercial com a exchange que você usa, nem recebemos dela qualquer remuneração por você.

2.4. O Software toma decisões automáticas **segundo as regras da Estratégia que você escolheu ativar**. Cada decisão é registrada com seu motivo e o contexto de mercado no diário da sua Conta, que você pode consultar e exportar.

## 3. Elegibilidade

3.1. Você declara que: (a) tem capacidade civil; (b) reside em país em que o uso do Software e da exchange é permitido [PREENCHER: lista de países atendidos]; (c) a exchange aceita você como cliente e os termos dela permitem negociar por API com o Software; (d) tem conhecimento e experiência com derivativos e capacidade financeira para suportar perdas superiores aos prêmios recebidos, conforme o questionário de perfil que preencheu; (e) usa recursos próprios e lícitos.

3.2. Podemos recusar ou encerrar o acesso de quem não atenda a estes requisitos.

## 4. Conta no Software e segurança

4.1. Você é responsável pela confidencialidade das suas credenciais. Recomendamos e, quando disponível, exigimos autenticação em dois fatores.
4.2. A chave de API deve ter **somente permissão de negociação**, sem permissão de saque ou transferência. Nunca solicite nem aceite que alguém lhe peça uma chave com permissão de saque.

## 5. Licença

5.1. Concedemos a você licença **pessoal, não exclusiva, intransferível e revogável** de uso do Software durante a vigência da assinatura, para a(s) Conta(s) contratada(s).
5.2. É vedado: copiar, revender, sublicenciar, fazer engenharia reversa (salvo nos limites da Lei 9.609/1998), usar o Software para gerir recursos de terceiros sem autorização regulatória própria, ou contornar seus limites de risco.

## 6. Preço e pagamento

6.1. A assinatura custa [PREENCHER] por [mês/ano] por Conta conectada, cobrada antecipadamente por [PREENCHER: meio de pagamento]. **O preço não depende do seu patrimônio nem do seu resultado** [ADVOGADO: manter — ver L8].
6.2. Reajuste anual pelo [PREENCHER: índice], com aviso de 30 dias. Mudança de preço fora disso só com aviso prévio de 30 dias e direito de cancelar sem multa.
6.3. **Direito de arrependimento:** você pode desistir em até 7 dias da contratação, com reembolso integral da assinatura (CDC, art. 49). O Software só abre a primeira posição após esse prazo, salvo se você pedir expressamente o início imediato; nesse caso, os resultados de mercado das posições abertas pertencem a você e o reembolso abrange apenas o valor pago [ADVOGADO: validar — B5].

## 7. Mudanças na Estratégia e no Software

7.1. Podemos corrigir defeitos e melhorar o Software. **Mudanças nas regras da Estratégia** (limites de margem, regras de entrada e saída, proteções) serão comunicadas com antecedência mínima de [PREENCHER: 7] dias, com descrição do que muda e por quê; você pode pausar o Software ou cancelar sem multa antes da vigência.
7.2. Mudanças urgentes para reduzir risco (por exemplo, falha que possa causar perda) podem entrar em vigor de imediato, com comunicação em seguida.

## 8. A exchange

8.1. Sua relação com a exchange é **sua**, regida pelos termos dela. Prevalecem as regras da exchange sobre margem, liquidação, horários e interrupções.
8.2. Se a exchange suspender sua Conta, restringir negociação por API ou mudar seus termos, o Software pode deixar de operar; as posições abertas permanecem na sua Conta e você pode encerrá-las diretamente na exchange.

## 9. Riscos

9.1. Vender opções pode gerar **perdas muito superiores aos prêmios recebidos**, inclusive de todo o capital depositado na Conta. Movimentos bruscos, falta de liquidez, falhas da exchange ou da internet podem impedir ou atrasar as proteções da Estratégia. Detalhes no **Termo de Ciência de Riscos**, que você declara ter lido e compreendido.
9.2. Resultados passados, simulações, testes em ambiente de teste (*testnet*) e testes de estresse **não garantem resultados futuros**.

## 10. Responsabilidades

10.1. **Nós respondemos** por defeitos do Software que causem dano, na forma da lei, e nos comprometemos a: manter o Software funcionando com diligência; corrigir defeitos com prioridade; manter o diário de decisões íntegro e disponível; avisar você sobre incidentes relevantes.
10.2. **Não respondemos** por: (a) perdas decorrentes do comportamento do mercado quando o Software operou conforme as regras da Estratégia; (b) falhas, interrupções, liquidações ou decisões da exchange; (c) falhas de internet, energia ou de terceiros fora do nosso controle; (d) uso indevido das suas credenciais por terceiros a quem você as forneceu; (e) obrigações tributárias suas.
10.3. [ADVOGADO: limite de responsabilidade — válido para usuário profissional/pessoa jurídica (ex.: até o valor pago em 12 meses); para consumidor, cláusulas que exonerem ou atenuem a responsabilidade por defeito são nulas (CDC, art. 25 e 51, I) — redigir conforme o público-alvo.]

## 11. Tributos

Ganhos com derivativos de criptoativos podem ser tributáveis e sujeitos a declarações periódicas à Receita Federal (por exemplo, Lei 14.754/2023 e IN RFB 1.888/2019 [ADVOGADO: confirmar]). Essas obrigações são suas. O Software disponibiliza extrato anual das operações para apoiar sua declaração.

## 12. Dados pessoais

Tratamos seus dados conforme a **Política de Privacidade** e a LGPD. Sua chave de API é armazenada criptografada, nunca é exibida, registrada em logs ou compartilhada, e é apagada quando você a revoga ou encerra o contrato.

## 13. Vigência e cancelamento

13.1. Vigência mensal/anual com renovação automática, cancelável a qualquer momento no painel, com o mesmo número de passos da contratação.
13.2. Ao cancelar: o Software deixa de abrir novas posições imediatamente; você escolhe entre (a) o Software **encerrar** as posições abertas a mercado, ou (b) **deixá-las** na sua Conta para você administrar. Sem escolha em [PREENCHER: 24] horas, aplica-se (b) [ADVOGADO: definir padrão mais protetivo].
13.3. Seu diário e extratos ficam disponíveis para exportação por [PREENCHER: 90] dias e são guardados pelo prazo da Política de Privacidade.

## 14. Comunicações

Avisos por e-mail cadastrado e no painel. Atendimento: [PREENCHER: canal], resposta em até [PREENCHER] dias úteis.

## 15. Lei e foro

Lei brasileira. Foro do domicílio do consumidor; para pessoas jurídicas, [PREENCHER: comarca] [ADVOGADO: avaliar arbitragem para PJ].
