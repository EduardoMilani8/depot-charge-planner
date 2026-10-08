# Próximos passos do depot-charge-planner (roadmap)

Status: planos de alto nível, escritos em 2026-10-08 depois que o laboratório web ficou pronto. Cada passo ganha, antes de ser implementado, a sua própria especificação e o seu plano detalhado (o mesmo ciclo dos subprojetos 1 e 2). Só o passo 1 já tem especificação: `docs/superpowers/specs/2026-10-08-validacao-dados-reais-design.md`.

## Ponto de partida

Hoje o projeto responde: "o planejador se comporta bem nos cenários que nós inventamos". Os cenários são sintéticos e usam premissas de dados públicos que nunca foram conferidas com uma operadora. A premissa mais pesada é que os operadores executam os rodízios recomendados (a maior parte do ganho do planejador sobre as referências depende dela). O custo de energia já existe no simulador (`sim.Tariff`, `Metrics.CostBRL`), mas o planejador só o mede, não o otimiza.

Ordem recomendada: **1 → 3 → 2 → 4 → 5**. O 1 testa se estamos certos; o 3 transforma o resultado em reais; o 2 reduz o risco de um piloto; o 4 e o 5 são ferramentas de produto e de prospecção.

| # | Passo | Pergunta que responde | Depende de | Tamanho |
|---|-------|-----------------------|------------|---------|
| 1 | Validar com dados reais | "Isto vale para uma garagem de verdade?" | conseguir dados de uma operadora | médio |
| 2 | Modo sombra | "O que o planejador faria no lugar do que a operação fez, ao vivo?" | 1 (formato de dados) | médio/grande |
| 3 | Custo de energia | "Quantos reais economizamos?" | nada (pode começar já) | médio |
| 4 | Ferramenta de dimensionamento | "Quantos carregadores e quantos kW esta garagem precisa?" | nada | pequeno |
| 5 | Tela para o pátio | "Os operadores conseguem seguir os rodízios?" | 2 (para ficar ao vivo) | médio |

---

## Plano 1 — Validar com dados reais

**Situação:** implementado (aguardando dados reais para validar). Veja `README.md` ("Validação com dados reais") e `docs/dados-reais/README.md`.

**Objetivo.** Importar um ou mais dias reais de uma garagem e rodar o planejador e as referências sobre as mesmas condições, comparando com o que de fato aconteceu.

**Por que primeiro.** Tudo o mais se apoia em premissas ainda não validadas. Se o planejador ganhar dos números reais, temos o melhor argumento comercial; se perder, descobrimos onde antes de investir mais.

**O que já existe.** `sim.Scenario`, os cinco controladores, o laboratório web, `Metrics`.

**Escopo.**
- Kit de pedido de dados para a operadora (`docs/dados-reais/`): o que pedir, versão mínima e ideal, modelos de planilha, cuidados com LGPD e anonimização.
- Pacote `internal/realdata`: lê planilhas CSV (ônibus, carregadores, garagem, sessões de carga reais e, se houver, potência), valida com mensagens em português e linha do erro, e monta um `sim.Scenario` por noite mais o desfecho real.
- Comando `cmd/replay` e aba "Dados reais" no laboratório: importar, rodar os controladores sobre cada noite e mostrar a coluna "real" ao lado.
- Relatório por ônibus: saiu pronto na realidade? e com o planejador, com e sem rodízios?

**Etapas.** (1) Kit de dados e formato (feito junto com a especificação). (2) Leitor e validação com testes. (3) Montagem do cenário e do desfecho real, com a mesma definição de "pronto" do simulador. (4) `cmd/replay`. (5) Aba no laboratório. (6) Exemplo fictício e documentação.

**Pronto quando.** Um conjunto de planilhas no formato do kit produz a tabela "real × planejador × referências" no `replay` e no laboratório, com avisos claros do que foi estimado; erros de planilha apontam arquivo, linha e coluna.

**Riscos.** Conseguir os dados (depende de relacionamento, não de código); qualidade dos dados (horários sem o SoC de saída; sensores diferentes); comparação contrafactual (a operação real fez escolhas que o planejador não vê). Mitigação: declarar premissas no relatório, mostrar planejador com e sem rodízios, começar por dados mínimos.

---

## Plano 2 — Modo sombra

**Objetivo.** O planejador lê o estado de uma garagem em tempo real (ou quase) e mostra o que faria, sem comandar nada; depois comparamos com o que a operação fez.

**Por que.** Reduz o risco para um cliente aceitar um piloto: nada é controlado, só observado.

**O que já existe.** `internal/gateway.Gateway` (interface de leitura de carregadores e escrita de potência), `planner.Plan`, `sim.Recorder` e o registro de decisões (`decisionlog`).

**Escopo.**
- Entrada de estado: primeiro por arquivos (a pasta de planilhas do Plano 1 atualizada a cada N minutos), depois, se houver cliente piloto, um adaptador de leitura (OCPP ou o que a garagem tiver). Fora de escopo na primeira versão: escrever nos carregadores.
- Laço de recomendação: a cada ciclo, montar `planner.Input` do estado lido, chamar o planejador e registrar a decisão e os rodízios sugeridos.
- Painel (reaproveita o laboratório): "agora o planejador recomendaria isto; a operação fez isto".
- Relatório do dia seguinte: ônibus que teriam saído prontos, energia e custo contra o real.

**Etapas.** (1) Especificação com o formato de estado de entrada e a política de falha (dado atrasado, sensor ausente, relógio errado). (2) Adaptador de arquivos + laço + registro. (3) Painel de recomendação. (4) Relatório diário. (5) Adaptador de leitura ao vivo, só com um cliente piloto definido.

**Pronto quando.** Uma noite inteira de dados reais passa pelo laço, sem escrever em nenhum carregador, e produz o relatório; o laço sobrevive a dado ausente e atrasado sem parar.

**Riscos.** Segurança (a garagem precisa confiar que não escrevemos nada: o modo sombra tem de ser somente leitura por construção); latência e relógio; acesso a telemetria depende do fabricante.

---

## Plano 3 — Custo de energia

**Objetivo.** Além de "ônibus pronto", minimizar a fatura de energia (carregar fora da ponta, achatar o pico de demanda), sem nunca sacrificar a prontidão.

**Por que.** Um número em reais convence mais do que porcentagem de ônibus prontos.

**O que já existe.** `sim.Tariff` (janela de ponta e dois preços), `Metrics.CostBRL`, a opção de achatar o pico no planejador (`planner.Config`, descrita em `types.go`: prontidão primeiro, achatamento depois).

**Escopo.**
- Modelo tarifário mais fiel ao Brasil: preço por horário (ponta / fora da ponta / intermediário), demanda máxima medida no mês (R$/kW), demanda contratada e ultrapassagem. Parametrizável por arquivo.
- Planejador: usar a folga que sobra depois de garantir prontidão para deslocar energia para horários baratos e reduzir o pico, com um parâmetro de agressividade.
- Métricas novas: custo de energia, custo de demanda, economia contra `fifo` e contra `fifo-unplug`.
- Laboratório: coluna e gráfico de custo, e cenários com tarifa editável.

**Etapas.** (1) Especificação do modelo tarifário e do que o planejador pode mexer. (2) Modelo tarifário e métricas no simulador (sem mudar o planejador; os números do README não mudam). (3) Estratégia de deslocamento no planejador atrás de uma opção desligada por padrão. (4) Testes de que a prontidão não piora e o custo não sobe. (5) Exibir no laboratório.

**Pronto quando.** Em cenários com folga, o custo cai de forma mensurável sem reduzir `ready%`; com a opção desligada nada muda; `plan violations` segue 0.

**Riscos.** Tarifas reais são complicadas e variam por distribuidora e contrato (conferir com a operadora e com a tarifa vigente da distribuidora); otimização pode interagir com o anti-vaivém dos rodízios.

---

## Plano 4 — Ferramenta de dimensionamento

**Objetivo.** Responder "quantos carregadores e que limite de potência uma garagem com N ônibus precisa para fechar a prontidão em X%?".

**Por que.** Serve para prospecção antes de existir piloto e é uma extensão pequena do laboratório.

**O que já existe.** `sim.Compare` (varreduras em paralelo), `/api/compare`, validação de limites.

**Escopo.**
- Varredura de duas dimensões (carregadores × limite de potência, opcionalmente potência por carregador) com sementes, para um controlador escolhido (padrão: planejador) e perfil de falha.
- Saída: mapa de calor de `ready%` e a menor combinação que atinge a meta, mais a comparação com `fifo-unplug` (quanto o planejador "economiza" em infraestrutura).
- Nova aba "Dimensionamento" no laboratório e opção `-sweep` no `simrun`.

**Etapas.** (1) Especificação (resolução da grade, limite de tempo, como mostrar incerteza). (2) Função de varredura em `internal/sim` com limite de tamanho. (3) Endpoint e aba com mapa de calor acessível (sem depender só de cor). (4) Documentação com um exemplo.

**Pronto quando.** Para um cenário padrão, a aba mostra o mapa, indica a menor combinação que atinge a meta e a execução termina dentro do prazo do servidor (60 s) para grades razoáveis.

**Riscos.** Custo computacional (grade grande × sementes); tentação de vender o resultado como dimensionamento real sendo que os dados ainda são sintéticos: o texto na tela deve dizer isso até o Plano 1 validar.

---

## Plano 5 — Tela para o pátio

**Objetivo.** Uma tela simples que mostra aos operadores o que fazer agora ("troque o ônibus B012 de lugar com o B007"), para testar se as pessoas conseguem seguir os rodízios.

**Por que.** A maior parte do ganho do planejador depende de operadores executarem os rodízios. É preciso medir se isso funciona na prática.

**O que já existe.** `planner.Plan.Swaps`, o painel de decisão do laboratório, o registro de decisões.

**Escopo.**
- Tela para celular/tablet: lista curta e priorizada de ações com prazo ("faça nos próximos 10 minutos"), confirmação de "feito" ou "não deu", vaga/carregador em destaque.
- Registro das confirmações (para medir a aderência real e alimentar o simulador no lugar da premissa "seguem todos").
- Funciona sobre o modo sombra (Plano 2) e, antes disso, em modo demonstração sobre uma execução simulada.

**Etapas.** (1) Entrevista curta com operadores sobre como recebem instruções hoje (rádio, papel, aplicativo). (2) Especificação da tela e do registro de aderência. (3) Modo demonstração. (4) Integração com o modo sombra. (5) Parâmetro de aderência no simulador (`FollowSwaps` passa a aceitar uma fração).

**Pronto quando.** Em demonstração, um operador consegue ler e confirmar as ações sem treinamento; o simulador aceita "70% dos rodízios são seguidos" e o laboratório mostra o efeito.

**Riscos.** Adoção (a melhor tela é inútil se o pátio não a usa); rodízio mal feito pode ser pior do que não fazer; segurança do pátio (instruções erradas movem ônibus de verdade).
