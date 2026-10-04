# depot-charge-planner

Planejador de recarga para garagens de ônibus elétricos: decide quanta potência cada carregador entrega a cada ônibus para que o máximo de ônibus saia com a carga necessária, respeitando o limite da garagem e continuando a funcionar quando a infraestrutura falha.

Status: em desenvolvimento (planejador + simulador). Design: `docs/superpowers/specs/`. Plano: `docs/superpowers/plans/`.

## Uso rápido

    go test -race ./...
    go run ./cmd/simrun -profile severe -seeds 20
    go run ./cmd/simrun -profile severe -seeds 20 -follow-swaps=false

Opções do `simrun`: `-buses`, `-chargers`, `-limit` (kW, até 1e7), `-profile none|mild|severe|random`, `-seeds`, `-follow-swaps` (padrão `true`), `-log decisions.jsonl`.
Colunas: `ready%` (principal), `shortfall kWh` (energia que faltou aos ônibus não prontos, contra a necessidade real da rota), `peak kW`, `plan violations` (deve ser 0), `overshoot min` (potência física acima do limite), `energy kWh` (energia da rede entregue por noite), `cost R$`, `plan changes`, `moves/run` (manobras pedidas aos operadores por noite: rodízios executados no `planner`, ônibus desconectados no `fifo-unplug`) e `p99 µs` (tempo de cálculo por ciclo). Médias por semente, exceto `plan violations` e `overshoot min` (somas) e `p99 µs` (pior semente).

`plan violations` conta os minutos em que a potência comandada passou do limite (deve ser 0). `overshoot min` conta os minutos em que a potência física passou do limite: pode ser diferente de zero mesmo com `plan violations` 0, porque os comandos só fazem efeito um minuto depois e uma queda repentina do limite (perfis `mild`, `severe` e `random`) pode causar excesso por cerca de um minuto mesmo com um plano correto. Como o planner usa toda a potência disponível, ele tem mais minutos de excesso que os baselines nessas quedas (por exemplo 17 contra 8 no perfil `severe` a 2000 kW).

Controladores comparados:

- `fifo`: carrega por ordem de chegada, na potência máxima, até esgotar o limite. Ônibus carregado continua no carregador até sair.
- `edf`: igual, por ordem de saída (quem sai primeiro carrega primeiro).
- `fifo-unplug`: `fifo` mais a rotina manual de hoje: quando há ônibus esperando, operadores desconectam um ônibus que já atingiu o alvo (pela mesma leitura de SoC que os controladores recebem, com as falhas) e ligam o que espera, com os mesmos 5 minutos de manobra de um rodízio.
- `safe`: só a camada 0 do planner (divisão igual do limite entre os ônibus conectados, sem ler SoC). É o perfil de último recurso, medido sozinho; não precisa superar os baselines.
- `planner`: camadas 0 e 1 com rodízio. Quando o limite não dá para terminar todos os ônibus conectados, o planner escolhe o maior grupo que o limite consegue terminar (por ordem de saída, deixando de fora quem precisa de mais energia) e o atende primeiro; os demais recebem só a sobra. Sem sobrecarga, a potência vai primeiro para quem tem menos folga e toda a sobra é usada. O planner recomenda rodízios (um ônibus esperando assume o carregador de um ônibus que já atingiu o alvo) só quando isso aumenta o número de ônibus que chegam ao alvo com o limite atual. Com `-follow-swaps` (padrão) o simulador supõe que os operadores executam **todas** as recomendações; com `-follow-swaps=false` nenhuma é executada (`planner (sem rodízio)` nas tabelas).

## Resultados de exemplo

`ready%` médio (percentual de ônibus prontos na saída), 50 ônibus, 25 carregadores, `-seeds 20`, limite de 2000 kW (padrão):

| perfil de falhas | fifo | edf | fifo-unplug | safe | planner | planner (sem rodízio) |
|---|---|---|---|---|---|---|
| none | 58.4 | 58.4 | 100.0 | 53.1 | 100.0 | 58.4 |
| mild | 58.3 | 58.3 | 96.6 | 53.1 | 100.0 | 58.5 |
| severe | 45.2 | 45.3 | 63.1 | 50.7 | 82.6 | 53.7 |
| random | 51.0 | 52.0 | 76.8 | 51.2 | 83.6 | 53.3 |

Com limites menores (`-limit`):

| limite / perfil | fifo | edf | fifo-unplug | safe | planner | planner (sem rodízio) |
|---|---|---|---|---|---|---|
| 1200 kW none | 57.2 | 55.2 | 95.7 | 50.5 | 100.0 | 58.3 |
| 1200 kW mild | 57.1 | 55.0 | 84.2 | 50.5 | 100.0 | 58.3 |
| 1200 kW severe | 44.1 | 43.1 | 42.2 | 47.9 | 70.6 | 51.8 |
| 1200 kW random | 46.0 | 45.0 | 61.1 | 47.6 | 74.0 | 51.9 |
| 800 kW none | 55.2 | 51.5 | 69.1 | 48.5 | 72.5 | 57.9 |
| 800 kW mild | 55.2 | 51.5 | 58.3 | 47.6 | 72.1 | 57.9 |
| 800 kW severe | 41.6 | 39.8 | 21.9 | 43.0 | 51.3 | 45.8 |
| 800 kW random | 40.1 | 37.8 | 42.8 | 42.3 | 53.8 | 49.7 |
| 600 kW none | 52.2 | 47.5 | 52.3 | 33.4 | 57.6 | 56.2 |
| 600 kW mild | 50.1 | 45.3 | 42.2 | 32.9 | 56.7 | 55.2 |
| 600 kW severe | 33.1 | 30.6 | 16.1 | 27.8 | 40.6 | 40.1 |
| 600 kW random | 32.8 | 31.1 | 32.4 | 27.5 | 42.4 | 42.1 |
| 500 kW none | 42.8 | 39.4 | 42.8 | 21.8 | 49.2 | 49.3 |
| 500 kW mild | 41.3 | 37.8 | 34.1 | 21.2 | 48.5 | 48.3 |
| 500 kW severe | 26.9 | 24.9 | 12.6 | 16.4 | 34.2 | 34.4 |
| 500 kW random | 27.9 | 25.0 | 27.3 | 17.4 | 33.6 | 33.5 |
| 400 kW none | 34.4 | 31.5 | 34.4 | 9.3 | 40.4 | 40.3 |
| 400 kW mild | 33.0 | 30.2 | 28.5 | 8.8 | 39.7 | 39.4 |
| 400 kW severe | 21.0 | 19.6 | 9.5 | 5.9 | 27.7 | 27.9 |
| 400 kW random | 22.6 | 20.0 | 21.9 | 8.2 | 25.0 | 24.9 |
| 300 kW none | 26.3 | 23.2 | 26.3 | 1.3 | 31.2 | 31.0 |
| 300 kW mild | 25.0 | 22.2 | 19.9 | 1.2 | 30.4 | 30.3 |
| 300 kW severe | 16.5 | 14.2 | 7.7 | 0.9 | 21.5 | 21.4 |
| 300 kW random | 16.8 | 14.4 | 16.6 | 1.3 | 17.4 | 17.4 |

Manobras pedidas aos operadores por noite (`moves/run`, mesma configuração):

| limite | planner none / mild / severe / random | fifo-unplug none / mild / severe / random |
|---|---|---|
| 2000 kW | 25.0 / 1124.0 / 1074.5 / 75.4 | 25.4 / 26.1 / 27.2 / 21.7 |
| 1200 kW | 25.0 / 229.4 / 162.0 / 36.4 | 25.0 / 25.5 / 26.9 / 20.5 |
| 600 kW | 3.2 / 23.1 / 28.6 / 4.7 | 22.4 / 22.3 / 20.9 / 15.4 |
| 400 kW | 0.6 / 6.8 / 13.1 / 0.7 | 15.8 / 15.6 / 14.6 / 10.8 |

Como ler esses números:

- **O ganho com limite folgado depende de os operadores executarem os rodízios.** Com 25 carregadores para 50 ônibus, o gargalo é o carregador ocupado por ônibus já carregado (só desconectar ônibus carregados já leva o `fifo` de 58.4 a 100 a 2000 kW). Sem rodízio, o planner fica praticamente igual ao `fifo` sem falhas (58.4 contra 58.4 a 2000 kW) e um pouco acima com falhas (53.7 contra 45.2 no `severe`). Que operadores reais executem toda recomendação, em 5 minutos, é uma **premissa a validar** com garagens reais.
- **Com leituras de SoC ruidosas, o planner pede rodízios demais.** Sem falhas são 25 manobras por noite a 2000 kW, como no `fifo-unplug`; com o ruído de leitura dos perfis `mild` e `severe` passam de 1.000 por noite a 2000 kW (229 a 1200 kW no `mild`). Quase todas trazem de volta um ônibus que acabou de ceder o carregador: depois de desconectado, o ruído faz sua leitura cair abaixo do alvo e um novo rodízio parece ganhar um ônibus pronto (na semente 1, `mild`, 2000 kW, 984 de 1.029 rodízios devolviam ao carregador um ônibus que o tinha cedido havia menos de 60 minutos). Nenhum operador fará isso: os números do planner com rodízio nos perfis com ruído supõem um trabalho irreal. Proibir a volta de quem cedeu o carregador cortou as manobras para cerca de 25 por noite sem perda no `mild`, mas custou até 10 pontos no `severe` (70.6 para 60.8 a 1200 kW), por isso não foi adotado; é um problema em aberto.
- **Contra a rotina manual de hoje (`fifo-unplug`)** o planner com rodízio empata sem falhas a 2000 kW (100 contra 100) e fica à frente nas demais linhas (82.6 contra 63.1 no `severe`; 70.6 contra 42.2 a 1200 kW no `severe`). O `fifo-unplug` depende muito da qualidade da leitura de SoC: a 600 kW no `severe` ele faz 16.1; numa medição em que os operadores decidiam pela carga real (sem as falhas de leitura) fazia 38.0.
- **Com limite apertado o ganho vem da escolha de quem carregar, não do rodízio.** Abaixo de cerca de 550 kW a versão anterior, que distribuía a potência por folga entre todos os ônibus, ficava abaixo do `fifo` (por exemplo 24.1 contra 34.4 a 400 kW sem falhas). Atendendo primeiro o maior grupo que o limite consegue terminar, o planner fica à frente em todas as linhas (40.4 contra 34.4 a 400 kW sem falhas; 31.2 contra 26.3 a 300 kW), com ou sem rodízio. O preço: quem fica de fora recebe pouco, e a energia total que falta aos ônibus não prontos fica no nível do `fifo` sem falhas (5.772 contra 5.698 kWh a 400 kW, +1,3%), abaixo dele com falhas (6.390 contra 6.695 kWh a 400 kW no `severe`).
- **Os perfis com falhas continuam difíceis.** No `severe` a 2000 kW, 17% dos ônibus saem sem a carga necessária; a 600 kW, só 40.6% saem prontos. Nos limites de 400 e 500 kW o rodízio às vezes custa um pouco (27.7 contra 27.9 a 400 kW no `severe`; 49.2 contra 49.3 a 500 kW sem falhas). Em nenhuma linha o planner fica abaixo de `fifo`, `edf` ou `fifo-unplug`, o que os testes de propriedade verificam com 8 sementes por padrão (`TestPlannerNotWorseThanBaselines`, 400, 500, 600, 1200 e 2000 kW, todos os perfis; 100 sementes no CI).
- **Custo, pico e estabilidade.** O planner usa toda a potência disponível para aprontar ônibus, sem achatar o pico. A 2000 kW sem falhas ele custa R$ 11.331 contra R$ 8.130 do `fifo` (+39%), mas entrega 10.832 kWh contra 7.275 kWh (+49%, coluna `energy kWh`), porque carrega mais ônibus; contra o `fifo-unplug` custa +3,9% (R$ 11.331 contra R$ 10.907) e entrega 4,5% mais energia (10.832 contra 10.361 kWh; o planner mira o alvo da rota mais uma margem de segurança de 10 kWh por ônibus). O pico chega ao limite da garagem tanto no planner quanto nos baselines. O planner muda o plano bem mais vezes (297 contra 59 do `fifo` sem falhas; 580 contra 393 no `severe`). Quem preferir achatar o pico pode limitar a sobra com `Config.SurplusLaxityMin` (por exemplo 120 minutos, o padrão anterior), com menos ônibus prontos. Medido no planner sem rodízio a 2000 kW, passar de 120 minutos para "toda a sobra" manteve 58.4% sem falhas e subiu de 49.4% para 53.7% no `severe`, com custo de R$ 7.094 para R$ 8.397 e pico de 1.773 para 2.000 kW sem falhas; as mudanças de plano caíram de 269 para 134.
- **Tempo de cálculo** (p99 por ciclo, 50 ônibus, `simrun` rodando sozinho nesta máquina): de 0,3 a 0,6 ms com rodízios seguidos e de 0,4 a 0,8 ms com `-follow-swaps=false` (a busca de rodízios roda enquanto há ônibus esperando, e sem rodízio executado eles esperam mais). No pior caso de benchmark (200 ônibus, 100 esperando, todos os carregadores com potências diferentes, de modo que a busca de rodízios chega ao teto de 256 tentativas por ciclo: `BenchmarkPlanNormalSwapSearch200Distinct`, e `BenchmarkPlannerPlanSwapSearch200Distinct` para o planner completo) um ciclo leva de 9,0 a 9,7 ms nesta máquina. Essas medições variam bastante com a máquina e a carga: o mesmo tipo de entrada já foi medido em 27,9 ms em outra sessão, ainda dentro da meta de 50 ms.

Os cenários são sintéticos e usam premissas de dados públicos ainda não validadas com operadoras reais, então esses números mostram a comparação entre controladores, não o desempenho esperado em campo.

Todas as premissas de carga vêm de dados públicos e ainda precisam de validação com operadoras reais.
