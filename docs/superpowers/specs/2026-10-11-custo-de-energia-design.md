# Custo de energia — especificação

Status: aprovado pelo autor para implementação ("sim, faça o custo", 2026-10-11); as decisões abaixo foram tomadas por Claude e podem ser corrigidas na revisão. Passo 3 do roadmap (`docs/superpowers/plans/2026-10-08-proximos-passos.md`).

## 1. Objetivo e sucesso

Hoje o simulador só calcula o custo da energia por horário (`sim.Tariff`: janela de ponta e dois preços) e o planejador não o usa: ele só garante prontidão e, por padrão, carrega o mais cedo possível. Este passo faz duas coisas:

1. **Medir a fatura de forma mais fiel ao Brasil** (energia por horário + demanda de potência), igual para o simulador e para os dados reais, em `simrun`, laboratório e `replay`.
2. **Deixar o planejador reduzir a fatura sem piorar a prontidão**: deslocar a carga para fora da ponta quando os ônibus têm folga e limitar o pico de demanda quando dá. Opcional e desligado por padrão.

Sucesso:
- Com o recurso desligado, **nenhum número existente muda** (README, golden, testes de propriedade) e `plan violations` segue 0.
- Com o recurso ligado, em cenários com chegadas na ponta, o custo de energia cai de forma mensurável e `ready%` não fica pior que com o recurso desligado (medido em todas as sementes dos testes, não só na média).
- O resultado é expresso em reais por noite e em uma **estimativa mensal** claramente rotulada, mais a economia contra `fifo` e `fifo-unplug`.
- O texto do README diz o que foi medido e o que depende de premissas (tarifa real da distribuidora).

## 2. Fora do escopo

Otimização global por programação linear; previsão de chegadas; bandeiras tarifárias e impostos; tarifa branca (grupo B); geração própria/bateria estacionária; contratação ótima de demanda; várias janelas intermediárias; dias úteis x fins de semana; fatura mensal real (o laboratório simula uma noite).

## 3. Modelo de fatura (decisões)

Estrutura do Grupo A brasileiro, parametrizável (os valores reais vêm da fatura da distribuidora; nada aqui é tarifa oficial):

- **Energia (R$/kWh)** com dois preços: ponta e fora da ponta. A janela de ponta pode **atravessar a meia-noite** (hoje `PriceAt` não trata isso: corrigir).
- **Demanda (R$/kW)**: cobrada pelo **maior valor médio de potência da rede em blocos de 15 minutos** alinhados ao relógio (`:00, :15, :30, :45`). Dois modos: **Azul** (um preço de demanda na ponta, outro fora da ponta, cada um sobre o maior bloco da sua janela) e **Verde** (um preço único sobre o maior bloco do período). Em ambos, o campo `DemandContractedKW` (0 = sem contrato) define a demanda cobrada: **com contrato**, paga-se o contratado mesmo que a garagem use menos (baixar o pico abaixo do contratado não reduz a fatura do mês; só evita ultrapassagem e pode justificar contratar menos no futuro); se o maior bloco passar de 105 % do contratado há **ultrapassagem**: o contratado mais o excedente acima do contratado a 2 vezes o preço de demanda (constantes nomeadas e documentadas; conferir com a distribuidora). **Sem contrato (0)**, a demanda cobrada é o maior bloco medido: serve de proxy do que seria contratado e é onde reduzir o pico aparece em reais.
- **Estimativa mensal**: `energia_da_noite × NightsPerMonth + demanda` (padrão `NightsPerMonth = 30`, a garagem opera todo dia). A demanda de uma noite é tratada como a demanda do mês (**proxy**: na fatura real vale o maior bloco do mês). Rótulos obrigatórios: "demanda (proxy mensal)" e "fatura mensal estimada".
- Preços padrão do gerador (já existentes): ponta 18:00–21:00, R$ 2,70 / 0,90 por kWh. Preços de demanda padrão: **zero** (compatível com hoje); o laboratório oferece um preset "Fatura com demanda" (valores ilustrativos, rotulados como tais).
- Tudo calculado por uma única biblioteca de fatura usada pelo simulador, pelo `replay` (para a linha "real" a partir de `potencia.csv` ou, estimado, das sessões) e pelo laboratório, para que real e simulado nunca divirjam por causa de duas implementações.

## 4. Planejador consciente de custo (decisões)

Nova política opcional em `planner.Config` (`Cost *CostPolicy`, nil = tudo como hoje; campo `omitempty` no registro de decisões para que o formato atual não mude):

- `CostPolicy{ClockAtZeroMin, PeakFromMin, PeakToMin int; DeferMarginMin float64 (padrão 45); DemandTargetKW float64 (0 = desligado)}`. O relógio vem da política; com a política nula o `Input`, o registro de decisões e os testes dourados não mudam.
- **Deslocar para fora da ponta.** Durante a ponta, um ônibus com necessidade fica em 0 kW se conseguir terminar depois da ponta com `DeferMarginMin` minutos de folga na potência máxima; caso contrário recebe potência como no planejador normal. A viabilidade conjunta usa a mesma ideia de `admit` (Moore-Hodgson) sobre o orçamento da garagem após a ponta (com a fração empírica `deferBudgetFrac = 0,6` e uma guarda de saturação); quem não couber é carregado já. O planejador replaneja a cada minuto, então erros de previsão se corrigem.
- **Escopo por construção (decisão da revisão da Tarefa 3).** Ônibus adiados continuam ocupando a tomada, e um modelo de fila de tomadas mostrou-se frágil (não monotônico). Por isso o adiamento só vale quando (a) a agenda de chegadas é conhecida (`CostPolicy.ScheduleKnown`; o simulador a fornece com `Scenario.AnnounceArrivals`, e então os `Input` e o registro de decisões passam a carregar também os ônibus ainda não chegados) e (b) há uma tomada saudável para cada ônibus presente ou anunciado que ainda não saiu. Fora disso a política não faz nada (comportamento idêntico ao planejador normal), em particular nas garagens com menos tomadas que ônibus (o cenário de rodízio do projeto) e sem agenda. Não há adiamento "às cegas".
- **Limitar a demanda (decisão da Tarefa 4).** Com `DemandTargetKW > 0`, o orçamento "normal" passa a ser `min(limite, alvo)` (aplicado antes do adiamento; o limite cheio continua sendo o do `Enforce`). Não depende de tomadas sobrando nem da agenda (funciona com e sem `ScheduleKnown`), mas só vale quando a prontidão não corre risco; caso contrário o planejador usa o limite cheio, com uma nota em `Plan.Notes`: (a) há ônibus aguardando carregador que ainda pode ser salvo (com a agenda, só se a folga dele for menor que 60 min), porque carregar mais devagar segura as tomadas por mais tempo; (b) algum ônibus carregando tem leitura de SoC não confiável; (c) sem agenda, ainda há carregador livre (a frota pode crescer); (d) os ônibus conhecidos (conectados, aguardando e, com agenda, anunciados) não terminam se a garagem entregar 85% do alvo (70% sem agenda) o tempo todo (teste de energia acumulada por instante de chegada e saída); (e) o plano com alvo deixa algum ônibus sem alcançar o alvo que o orçamento cheio salva ou ao menos alimenta com mais potência (o teste do enunciado, ampliado aos ônibus que o planejador dá por inviáveis, que o simulador ainda termina com a sobra de potência). Com a agenda anunciada, `ready%` nunca fica abaixo e a demanda nunca fica acima da mesma política sem alvo em nenhuma semente das geometrias testadas e de uma varredura aleatória; sem agenda, em garagens com vários ônibus por carregador, há limites fixados nos testes. A demanda só cai onde o alvo é viável (no cenário padrão de 50 ônibus/25 carregadores a 2000 kW, alvo de 1600 kW: -20%); alvo que a energia da noite não comporta não muda nada. Com o adiamento ligado, um alvo abaixo da carga que o adiamento concentra devolve parte da economia de energia (o README mostra os dois efeitos).
- **Garantias:** prontidão tem prioridade; a camada 0/1 de segurança e o verificador (`Enforce`) continuam valendo; sem leitura confiável de SoC o recurso não adia ninguém (comportamento conservador); com falhas de carregador ou queda de rede o adiamento é reavaliado a cada minuto.
- **Honestidade:** se as medições mostrarem que o recurso não economiza em algum perfil, o README diz isso.

## 5. Interfaces

- `internal/sim`: `Tariff` ganha campos novos (compatíveis: zero = comportamento atual); novas métricas em `Metrics`: `DemandKW` (maior bloco de 15 min), `DemandCostBRL`, `MonthlyBRL`; `CostBRL` continua sendo a energia da noite (nenhum número antigo muda).
- `cmd/simrun`: flags `-cost-aware`, `-demand-target KW`, `-demand-peak-price`, `-demand-offpeak-price`, `-demand-contracted KW`, `-demand-mode azul|verde`, `-arrival-from`/`-arrival-to` (janela de chegadas em hora do dia) e colunas `demand kW`, `demand R$/mês`, `monthly R$`.
- `internal/lab` e `web`: campos avançados equivalentes, preset "Chegadas na ponta (custo)", colunas novas na Comparação e economia contra `fifo`/`fifo-unplug`; texto de ajuda sobre o proxy mensal.
- `internal/realdata`: `garagem.csv` ganha colunas opcionais (`demanda_preco_ponta`, `demanda_preco_fora_ponta`, `demanda_contratada_kw`, `modo_demanda`); a linha "real" usa a mesma biblioteca de fatura; a janela de ponta que atravessa a meia-noite deixa de impedir a comparação de custo.

## 6. Testes e medições

- Biblioteca de fatura: casos calculados à mão (janela que atravessa a meia-noite, blocos de 15 min, Azul × Verde, ultrapassagem a 105 % e a 2×).
- Compatibilidade: com tarifa de demanda zero e política nula, todas as saídas existentes são idênticas (golden/hash dos testes, `simrun` antes e depois).
- Propriedades do planejador (todas as sementes de 4 perfis × 3 limites, chegadas na ponta e normais): zero violações; `ready%` com a política ≥ `ready%` sem ela; custo de energia com a política ≤ sem ela quando há chegadas na ponta; adiamento nunca deixa ônibus sem folga quando as leituras são confiáveis.
- Medição reportada no README: tabela antes/depois (energia R$/noite, demanda kW, mensal estimado, ready%) para os presets.

## 7. Decisões e custos se estiverem erradas

1. Dois modos de demanda (Azul/Verde) e ultrapassagem a 105 %/2× como constantes: se a distribuidora do autor for diferente, os números absolutos mudam, não a lógica.
2. Estimativa mensal = energia × 30 + demanda da noite: simplificação rotulada; o erro é tratar uma noite como o mês.
3. Política desligada por padrão e dentro de `Config` (não no `Input`): protege golden e registros; custa que quem usa o planejador em produção precise informar o relógio.
4. Heurística local em vez de otimização global: pode deixar economia na mesa; garante prontidão e é explicável.
