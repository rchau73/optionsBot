# Avisos (disclaimers) — textos curtos para produto, site e IA

> **MINUTA para revisão por advogado(a).** Textos prontos para os lugares onde aparecem. PT-BR primeiro; EN entre parênteses para telas em inglês. Fonte das regras: `docs/legal/preliminary-answers.md` (L7, L9, L11, L12).

## Rodapé de todas as telas
**PT:** "optionsBot é um software. Não é corretora, gestora nem consultoria de investimentos e não faz recomendação personalizada. Vender opções pode gerar perdas maiores que os prêmios recebidos, inclusive de todo o capital. Resultados passados, simulações e testes não garantem resultados futuros."
**EN:** "optionsBot is software. It is not a broker, asset manager or investment adviser and gives no personal recommendation. Selling options can lose more than the premiums received, up to all the capital. Past results, simulations and tests do not guarantee future results."

## Ambiente de teste (cabeçalho do painel em testnet)
**PT:** "TESTNET · dinheiro fictício. Preços e liquidez não são os do mercado real; estes números mostram o funcionamento das regras, não desempenho."
**EN:** "TESTNET · practice money. Prices and liquidity are not the real market's; these figures show how the rules work, not performance."

## Números de desempenho (qualquer gráfico ou tabela de resultado)
- Testnet: "Resultado em ambiente de testes com dinheiro fictício. Não é desempenho real."
- Simulação/replay/estresse: "Resultado **hipotético**, calculado com regras aplicadas a dados passados. Não reflete custos, atrasos e liquidez reais por completo e não garante resultados futuros."
- Real (somente após autorização — L7): "Resultado passado, líquido de taxas, período [de–até]. Rentabilidade passada não é garantia de rentabilidade futura."

## Pior caso (card "Worst case")
"Estimativa por modelo, com os preços de hoje. Movimentos maiores podem acontecer e a perda real pode ser maior. Não é um limite."

## Lista de espera / página inicial (L9)
**PT:** "Ainda não oferecemos este serviço. Esta lista registra seu interesse para avisarmos quando e onde ele estiver disponível; não é oferta, não há preço e não aceitamos dinheiro. Disponibilidade sujeita a autorização regulatória."
**EN:** "This service is not offered yet. This list records your interest so we can tell you when and where it becomes available; it is not an offer, there is no price and we accept no money. Availability is subject to regulatory approval."

## Assistente de IA (E10, L11–L12)
**Aviso fixo:** "Você está conversando com um assistente de IA. Ele explica como o optionsBot funciona e por que o robô tomou cada decisão, citando a fonte. Não dá recomendação de investimento, não prevê preços nem resultados e pode errar — confira a fonte citada."
**Regras de resposta (implementar no assistente):** recusar "devo comprar/vender/ativar…?", previsões de preço e de retorno, e comparações de adequação pessoal ("isso é bom para mim?"), respondendo: "Não posso recomendar o que fazer com o seu dinheiro. Posso explicar como a regra funciona e o que ela fez na sua conta." Sempre citar a fonte. Registrar pergunta e resposta para revisão.

## Análise de IA no laboratório de estratégia (E9, L10)
"Análise gerada por IA a partir dos números deste teste. Resultados de backtest são hipotéticos. Não é recomendação." (Visível apenas para a equipe até a resposta de L10/L24.)

## Fechamento manual e rebalanceamento (painel)
"Esta ação envia ordens a mercado na sua conta e é registrada com seu nome. O resultado pode ser pior que o preço exibido em mercados rápidos."
