# depot-charge-planner

Planejador de recarga para garagens de ônibus elétricos: decide quanta potência cada carregador entrega a cada ônibus para que o máximo de ônibus saia com a carga necessária, respeitando o limite da garagem e continuando a funcionar quando a infraestrutura falha.

Status: em desenvolvimento (planejador + simulador). Design: `docs/superpowers/specs/`. Plano: `docs/superpowers/plans/`.

## Uso rápido

    go test -race ./...
    go run ./cmd/simrun -profile severe -seeds 20
    go run ./cmd/simrun -profile severe -seeds 20 -follow-swaps=false

Opções do `simrun`: `-buses`, `-chargers`, `-limit` (kW, até 1e7), `-profile none|mild|severe|random`, `-seeds`, `-follow-swaps` (padrão `true`), `-log decisions.jsonl`.
Colunas: `ready%` (principal), `plan violations` (deve ser 0), `overshoot min` (potência física acima do limite).

`plan violations` conta os minutos em que a potência comandada passou do limite (deve ser 0). `overshoot min` conta os minutos em que a potência física passou do limite: pode ser diferente de zero mesmo com `plan violations` 0, porque os comandos só fazem efeito um minuto depois e uma queda repentina do limite (perfis `mild`, `severe` e `random`) pode causar excesso por cerca de um minuto mesmo com um plano correto. Como o planner usa toda a potência disponível, ele tem mais minutos de excesso que os baselines nessas quedas (por exemplo 17 contra 8 no perfil `severe` a 2000 kW).

Controladores comparados:

- `fifo`: carrega por ordem de chegada, na potência máxima, até esgotar o limite. Ônibus carregado continua no carregador até sair.
- `edf`: igual, por ordem de saída (quem sai primeiro carrega primeiro).
- `fifo-unplug`: `fifo` mais a rotina manual de hoje: quando há ônibus esperando, operadores desconectam um ônibus que já atingiu o alvo (pela mesma leitura de SoC que os controladores recebem, com as falhas) e ligam o que espera, com os mesmos 5 minutos de manobra de um rodízio.
- `safe`: só a camada 0 do planner (divisão igual do limite entre os ônibus conectados, sem ler SoC). É o perfil de último recurso, medido sozinho; não precisa superar os baselines.
- `planner`: camadas 0 e 1 com rodízio. O planner recomenda rodízios (um ônibus esperando assume o carregador de um ônibus que já atingiu o alvo) só quando isso aumenta o número de ônibus que chegam ao alvo com o limite atual. Com `-follow-swaps` (padrão) o simulador supõe que os operadores executam **todas** as recomendações; com `-follow-swaps=false` nenhuma é executada (`planner (sem rodízio)` nas tabelas).

## Resultados de exemplo

`ready%` médio (percentual de ônibus prontos na saída), 50 ônibus, 25 carregadores, `-seeds 20`, limite de 2000 kW (padrão):

| perfil de falhas | fifo | edf | fifo-unplug | safe | planner | planner (sem rodízio) |
|---|---|---|---|---|---|---|
| none | 58.4 | 58.4 | 100.0 | 53.1 | 100.0 | 58.4 |
| mild | 58.3 | 58.3 | 96.6 | 53.1 | 100.0 | 58.5 |
| severe | 45.2 | 45.3 | 63.1 | 50.7 | 82.6 | 53.7 |
| random | 51.0 | 52.0 | 76.5 | 51.4 | 83.7 | 53.5 |

Com limites menores (`-limit 1200` e `-limit 600`):

| limite / perfil | fifo | edf | fifo-unplug | safe | planner | planner (sem rodízio) |
|---|---|---|---|---|---|---|
| 1200 kW none | 57.2 | 55.2 | 95.7 | 50.5 | 100.0 | 58.3 |
| 1200 kW mild | 57.1 | 55.0 | 84.2 | 50.5 | 100.0 | 58.3 |
| 1200 kW severe | 44.1 | 43.1 | 42.2 | 47.9 | 70.6 | 51.8 |
| 1200 kW random | 46.0 | 45.0 | 61.3 | 47.7 | 73.6 | 50.9 |
| 600 kW none | 52.2 | 47.5 | 52.3 | 33.4 | 57.3 | 56.0 |
| 600 kW mild | 50.1 | 45.3 | 42.2 | 32.9 | 56.0 | 54.8 |
| 600 kW severe | 33.1 | 30.6 | 16.1 | 27.8 | 38.6 | 39.3 |
| 600 kW random | 33.0 | 31.3 | 32.5 | 27.6 | 39.9 | 40.7 |

Como ler esses números:

- **O ganho depende de os operadores executarem os rodízios.** Com 25 carregadores para 50 ônibus, o gargalo é o carregador ocupado por ônibus já carregado (só desconectar ônibus carregados já leva o `fifo` de 58.4 a 100 a 2000 kW). Sem rodízio, o planner fica praticamente igual ao `fifo` sem falhas (58.4 contra 58.4 a 2000 kW) e um pouco acima com falhas (53.7 contra 45.2 no `severe`). Que operadores reais executem toda recomendação, em 5 minutos, é uma **premissa a validar** com garagens reais.
- **Contra a rotina manual de hoje (`fifo-unplug`)** o planner com rodízio empata sem falhas a 2000 kW (100 contra 100) e fica à frente nas demais linhas (82.6 contra 63.1 no `severe`; 70.6 contra 42.2 a 1200 kW no `severe`). O `fifo-unplug` depende muito da qualidade da leitura de SoC: a 600 kW no `severe` ele faz 16.1; numa medição em que os operadores decidiam pela carga real (sem as falhas de leitura) fazia 38.0.
- **Os perfis com falhas continuam difíceis.** No `severe` a 2000 kW, 17% dos ônibus saem sem a carga necessária; a 600 kW, só 38.6% saem prontos. Com limite apertado o ganho é pequeno (57.3 contra 52.2 do `fifo` a 600 kW sem falhas) e, nos perfis `severe` e `random` a 600 kW, o planner com rodízio fica ligeiramente abaixo do planner sem rodízio (38.6 contra 39.3 e 39.9 contra 40.7). Em nenhuma linha ele fica abaixo de `fifo`, `edf` ou `fifo-unplug`, o que os testes de propriedade verificam com 8 sementes por padrão (`TestPlannerNotWorseThanBaselines`, 600, 1200 e 2000 kW, todos os perfis).
- **Custo, pico e estabilidade.** O planner usa toda a potência disponível (primeiro quem tem menos folga) para aprontar ônibus, sem achatar o pico. A 2000 kW sem falhas ele custa R$ 11.331 contra R$ 8.130 do `fifo` (+39%), mas entrega 10.832 kWh contra 7.275 kWh (+49%), porque carrega mais ônibus; contra o `fifo-unplug` custa +3,9% (R$ 11.331 contra R$ 10.907) e entrega 4,5% mais energia (o planner mira o alvo da rota mais uma margem de segurança de 10 kWh por ônibus). O pico chega ao limite da garagem tanto no planner quanto nos baselines. O planner muda o plano bem mais vezes (297 contra 59 do `fifo` sem falhas; 580 contra 393 no `severe`). Quem preferir achatar o pico pode limitar a sobra com `Config.SurplusLaxityMin` (por exemplo 120 minutos, o padrão anterior), com menos ônibus prontos. Medido no planner sem rodízio a 2000 kW, passar de 120 minutos para "toda a sobra" manteve 58.4% sem falhas e subiu de 49.4% para 53.7% no `severe`, com custo de R$ 7.094 para R$ 8.397 e pico de 1.773 para 2.000 kW sem falhas; as mudanças de plano caíram de 269 para 134.
- **Tempo de cálculo** (p99 por ciclo, nesta máquina, 50 ônibus): de 0,6 a 6 ms com rodízios seguidos e de 5,8 a 8,4 ms com `-follow-swaps=false`. A busca de rodízios roda enquanto há ônibus esperando, e sem rodízio executado eles esperam mais.

Os cenários são sintéticos e usam premissas de dados públicos ainda não validadas com operadoras reais, então esses números mostram a comparação entre controladores, não o desempenho esperado em campo.

Todas as premissas de carga vêm de dados públicos e ainda precisam de validação com operadoras reais.
