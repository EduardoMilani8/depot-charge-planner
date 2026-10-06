# depot-charge-planner

Planejador de recarga para garagens de ônibus elétricos: decide quanta potência cada carregador entrega a cada ônibus para que o máximo de ônibus saia com a carga necessária, respeitando o limite da garagem e continuando a funcionar quando a infraestrutura falha.

Status: em desenvolvimento (planejador + simulador). Design: `docs/superpowers/specs/`. Plano: `docs/superpowers/plans/`.

## Uso rápido

    go test -race ./...
    go run ./cmd/simrun -profile severe -seeds 20
    go run ./cmd/simrun -profile severe -seeds 20 -follow-swaps=false

Opções do `simrun`: `-buses`, `-chargers`, `-limit` (kW, até 1e7), `-profile none|mild|severe|random`, `-seeds`, `-follow-swaps` (padrão `true`), `-swap-back-cooldown` (minutos, padrão 30) e `-swap-back-min-need` (kWh, padrão 10) da regra contra o vaivém de rodízios (os dois em 0 desligam a regra; veja abaixo), `-reading-age` (minutos de idade que o sensor informa para uma leitura nova, padrão 0), `-log decisions.jsonl`.
Colunas: `ready%` (principal), `shortfall kWh` (energia que faltou aos ônibus não prontos, contra a necessidade real da rota), `peak kW`, `plan violations` (deve ser 0), `overshoot min` (potência física acima do limite), `energy kWh` (energia da rede entregue por noite), `cost R$`, `plan changes`, `moves/run` (manobras pedidas aos operadores por noite: rodízios executados no `planner`, ônibus desconectados no `fifo-unplug`) e `p99 µs` (tempo de cálculo por ciclo). Médias por semente, exceto `plan violations` e `overshoot min` (somas) e `p99 µs` (pior semente).

`plan violations` conta os minutos em que a potência comandada passou do limite (deve ser 0). `overshoot min` conta os minutos em que a potência física passou do limite: pode ser diferente de zero mesmo com `plan violations` 0, porque os comandos só fazem efeito um minuto depois e uma queda repentina do limite (perfis `mild`, `severe` e `random`) pode causar excesso por cerca de um minuto mesmo com um plano correto. Como o planner usa toda a potência disponível, ele tem mais minutos de excesso que os baselines nessas quedas (por exemplo 18 contra 8 no perfil `severe` a 2000 kW).

Controladores comparados:

- `fifo`: carrega por ordem de chegada, na potência máxima, até esgotar o limite. Ônibus carregado continua no carregador até sair.
- `edf`: igual, por ordem de saída (quem sai primeiro carrega primeiro).
- `fifo-unplug`: `fifo` mais a rotina manual de hoje: quando há ônibus esperando, operadores desconectam um ônibus que já atingiu o alvo (pela mesma leitura de SoC que os controladores recebem, com as falhas) e ligam o que espera, com os mesmos 5 minutos de manobra de um rodízio.
- `safe`: só a camada 0 do planner (divisão igual do limite entre os ônibus conectados, sem ler SoC). É o perfil de último recurso, medido sozinho; não precisa superar os baselines.
- `planner`: camadas 0 e 1 com rodízio. Quando o limite não dá para terminar todos os ônibus conectados, o planner escolhe o maior grupo que o limite consegue terminar (por ordem de saída, deixando de fora quem precisa de mais energia) e o atende primeiro; os demais recebem só a sobra. Sem sobrecarga, a potência vai primeiro para quem tem menos folga e toda a sobra é usada. O planner recomenda rodízios (um ônibus esperando assume o carregador de um ônibus que já atingiu o alvo) só quando isso aumenta o número de ônibus que chegam ao alvo com o limite atual. Um ônibus que cedeu o carregador só volta por rodízio depois de 30 minutos e se a média das suas leituras desde que foi desconectado (parado, a carga dele não muda) indicar que falta mais de 10 kWh para o alvo com margem, ou seja, que ele está abaixo do próprio alvo da rota (`Config.SwapBackCooldownMin` e `Config.SwapBackMinNeedKWh`). Ficam fora da média as leituras não confiáveis e as que não são mais novas, pelo horário em que foram feitas (minuto atual menos `SoCAgeMin`), que a última já contada para aquele ônibus; assim a mesma leitura conta uma vez só, mesmo congelada ou informada com alguns minutos de idade. Com `-follow-swaps` (padrão) o simulador supõe que os operadores executam **todas** as recomendações; com `-follow-swaps=false` nenhuma é executada (`planner (sem rodízio)` nas tabelas).

## Laboratório web

Uma interface local para configurar cenários, comparar o planejador com as referências e abrir uma execução minuto a minuto (potência contra o limite, carga de cada ônibus, carregadores, decisão tomada e o motivo).

    go run ./cmd/lab

Abra `http://127.0.0.1:8080`. Não precisa de Node: a interface vai dentro do programa. O servidor só escuta em endereço local (não tem login) e só aceita requisições para `127.0.0.1`, `localhost` ou `::1`.

- **Cenário:** ônibus, carregadores, limite (kW), perfil de falhas, sementes, operadores seguem rodízios, idade da leitura e, em "Avançado", a regra anti-vaivém do rodízio. Há cenários prontos.
- **Comparação:** a mesma tabela do `simrun` (para os mesmos parâmetros os números são os mesmos, com o arredondamento do Go, apenas com menos casas decimais e separador de milhar; a exceção é o `p99 µs`, um tempo medido que varia de uma execução para outra) e um ponto por semente; clicar num ponto abre aquela execução.
- **Execução:** gráfico de potência, linha do tempo de ônibus e de carregadores e painel de decisão, com um cursor de tempo. Clicar num ônibus explica por que ele saiu pronto ou não. "Copiar link desta tela" gera um endereço (o estado fica no `#` do endereço) que reproduz a execução; colar um link novo na mesma aba também atualiza a tela. O simulador é determinístico: a mesma semente e os mesmos parâmetros dão sempre a mesma execução, e nada é gravado em disco.
- **Animações:** "Reproduzir" toca o dia (30 min/s a 10 h/s) com o passado nítido e o futuro esmaecido, ônibus carregando pulsando, fluxo nos carregadores ocupados e um feed de acontecimentos (saídas, rodízios, mudanças de camada, quedas do limite). O interruptor "Animações" e a preferência de sistema "reduzir movimento" desligam as animações (entradas, pulsos, fluxo, contagens); a reprodução do dia continua funcionando, com o cursor se movendo e o passado nítido contra o futuro esmaecido, que fazem parte dela.

Limites para a tela não travar: ônibus × sementes até 20000 na comparação e até 500 ônibus e 500 carregadores numa execução detalhada (e sementes de 1 a 1000).

API (JSON): `GET /api/defaults`, `POST /api/compare`, `POST /api/run`; o formato está em `docs/superpowers/specs/2026-10-05-laboratorio-web-design.md`. Testes da interface: `node --test web/test/*.test.mjs` (só desenvolvimento).

Os dados são sintéticos: mostram o comportamento do algoritmo, não o de uma garagem real.

## Resultados de exemplo

Todos os números desta seção saem do `simrun` com o código deste repositório (50 ônibus, 25 carregadores, `-seeds 20`, configuração padrão), por exemplo `go run ./cmd/simrun -limit 1200 -profile severe -seeds 20`; a coluna `planner (sem rodízio)` usa `-follow-swaps=false` e a coluna "regra desligada" usa `-swap-back-cooldown 0 -swap-back-min-need 0`.

`ready%` médio (percentual de ônibus prontos na saída), limite de 2000 kW (padrão):

| perfil de falhas | fifo | edf | fifo-unplug | safe | planner | planner (sem rodízio) |
|---|---|---|---|---|---|---|
| none | 58.4 | 58.4 | 100.0 | 53.1 | 100.0 | 58.4 |
| mild | 58.3 | 58.3 | 96.6 | 53.1 | 100.0 | 58.5 |
| severe | 45.2 | 45.3 | 63.1 | 50.7 | 88.5 | 53.7 |
| random | 51.0 | 52.0 | 76.8 | 51.2 | 83.5 | 53.3 |

Com limites menores (`-limit`):

| limite / perfil | fifo | edf | fifo-unplug | safe | planner | planner (sem rodízio) |
|---|---|---|---|---|---|---|
| 1200 kW none | 57.2 | 55.2 | 95.7 | 50.5 | 100.0 | 58.3 |
| 1200 kW mild | 57.1 | 55.0 | 84.2 | 50.5 | 100.0 | 58.3 |
| 1200 kW severe | 44.1 | 43.1 | 42.2 | 47.9 | 72.0 | 51.8 |
| 1200 kW random | 46.0 | 45.0 | 61.1 | 47.6 | 74.1 | 51.9 |
| 800 kW none | 55.2 | 51.5 | 69.1 | 48.5 | 72.5 | 57.9 |
| 800 kW mild | 55.2 | 51.5 | 58.3 | 47.6 | 72.3 | 57.9 |
| 800 kW severe | 41.6 | 39.8 | 21.9 | 43.0 | 52.0 | 45.8 |
| 800 kW random | 40.1 | 37.8 | 42.8 | 42.3 | 54.1 | 49.7 |
| 600 kW none | 52.2 | 47.5 | 52.3 | 33.4 | 57.6 | 56.2 |
| 600 kW mild | 50.1 | 45.3 | 42.2 | 32.9 | 56.7 | 55.2 |
| 600 kW severe | 33.1 | 30.6 | 16.1 | 27.8 | 40.9 | 40.1 |
| 600 kW random | 32.8 | 31.1 | 32.4 | 27.5 | 42.6 | 42.1 |
| 500 kW none | 42.8 | 39.4 | 42.8 | 21.8 | 49.2 | 49.3 |
| 500 kW mild | 41.3 | 37.8 | 34.1 | 21.2 | 48.6 | 48.3 |
| 500 kW severe | 26.9 | 24.9 | 12.6 | 16.4 | 34.6 | 34.4 |
| 500 kW random | 27.9 | 25.0 | 27.3 | 17.4 | 33.5 | 33.5 |
| 400 kW none | 34.4 | 31.5 | 34.4 | 9.3 | 40.4 | 40.3 |
| 400 kW mild | 33.0 | 30.2 | 28.5 | 8.8 | 39.8 | 39.4 |
| 400 kW severe | 21.0 | 19.6 | 9.5 | 5.9 | 28.2 | 27.9 |
| 400 kW random | 22.6 | 20.0 | 21.9 | 8.2 | 25.0 | 24.9 |
| 300 kW none | 26.3 | 23.2 | 26.3 | 1.3 | 31.2 | 31.0 |
| 300 kW mild | 25.0 | 22.2 | 19.9 | 1.2 | 30.5 | 30.3 |
| 300 kW severe | 16.5 | 14.2 | 7.7 | 0.9 | 21.5 | 21.4 |
| 300 kW random | 16.8 | 14.4 | 16.6 | 1.3 | 17.4 | 17.4 |

Manobras pedidas aos operadores por noite (`moves/run`) e `ready%` do planner, com a regra contra o vaivém (padrão) e com ela desligada:

| limite / perfil | moves planner | moves planner, regra desligada | moves fifo-unplug | ready% planner | ready% planner, regra desligada |
|---|---|---|---|---|---|
| 2000 kW none | 25.0 | 25.0 | 25.4 | 100.0 | 100.0 |
| 2000 kW mild | 25.5 | 1124.0 | 26.1 | 100.0 | 100.0 |
| 2000 kW severe | 37.3 | 1074.5 | 27.2 | 88.5 | 82.6 |
| 2000 kW random | 21.4 | 75.4 | 21.7 | 83.5 | 83.6 |
| 1200 kW none | 25.0 | 25.0 | 25.0 | 100.0 | 100.0 |
| 1200 kW mild | 25.1 | 229.4 | 25.5 | 100.0 | 100.0 |
| 1200 kW severe | 41.7 | 162.0 | 26.9 | 72.0 | 70.6 |
| 1200 kW random | 18.6 | 36.4 | 20.5 | 74.1 | 74.0 |
| 800 kW mild | 11.6 | 63.7 | 25.1 | 72.3 | 72.1 |
| 800 kW severe | 21.9 | 84.9 | 25.7 | 52.0 | 51.3 |
| 600 kW severe | 8.0 | 28.6 | 20.9 | 40.9 | 40.6 |
| 400 kW severe | 3.3 | 13.1 | 14.6 | 28.2 | 27.7 |

Como ler esses números:

- **O ganho com limite folgado depende de os operadores executarem os rodízios.** Com 25 carregadores para 50 ônibus, o gargalo é o carregador ocupado por ônibus já carregado (só desconectar ônibus carregados já leva o `fifo` de 58.4 a 100 a 2000 kW). Sem rodízio, o planner fica praticamente igual ao `fifo` sem falhas (58.4 contra 58.4 a 2000 kW) e um pouco acima com falhas (53.7 contra 45.2 no `severe`). Que operadores reais executem toda recomendação, em 5 minutos, é uma **premissa a validar** com garagens reais.
- **Leituras de SoC ruidosas e o vaivém de rodízios.** Um ônibus que já atingiu o alvo cede o carregador; parado, o ruído da leitura faz ele parecer abaixo do alvo de tempos em tempos, e uma foto isolada da garagem diz que trazê-lo de volta (tirando outro ônibus cheio) ganha um ônibus pronto. Sem nenhuma regra, isso pedia mais de 1.000 manobras por noite a 2000 kW nos perfis `mild` e `severe` (coluna "regra desligada"). Como a carga de um ônibus parado não muda, o planner passa a usar a média das leituras desde que ele cedeu o carregador, que tem muito menos ruído que a leitura do minuto, e espera 30 minutos antes de trazê-lo de volta. Nos perfis `mild` e `severe` a 2000 e 1200 kW as manobras caem para 25 a 42 por noite (o `fifo-unplug` pede de 26 a 27). O `ready%` não cai mais de 0,1 ponto em nenhuma linha de 300 a 2000 kW (2000 kW `random`: 83.5 contra 83.6; 500 kW `random`: 33.5 contra 33.6) e no `severe` sobe ou fica igual (88.5 contra 82.6 a 2000 kW; 72.0 contra 70.6 a 1200 kW), provavelmente porque cada manobra deixa o ônibus que entra 5 minutos sem carregar. Ônibus sem nenhuma leitura contada desde que cederam o carregador (só leituras não confiáveis) não voltam por rodízio. Com leituras informadas com 1 minuto de idade (`-reading-age 1`), a regra não perde mais de 0,2 ponto de `ready%` em relação a ela desligada em nenhuma linha de 300 a 2000 kW (1200 kW `severe`: 72.1 contra 70.9; 2000 kW `severe`: 88.2 contra 82.4).
- **Contra a rotina manual de hoje (`fifo-unplug`)** o planner com rodízio empata sem falhas a 2000 kW (100 contra 100) e fica à frente nas demais linhas (88.5 contra 63.1 no `severe`; 72.0 contra 42.2 a 1200 kW no `severe`). O `fifo-unplug` depende muito da qualidade da leitura de SoC: a 600 kW ele cai de 52.3 sem falhas para 16.1 no `severe`.
- **Com limite apertado o ganho vem da escolha de quem carregar, não do rodízio.** Abaixo de cerca de 550 kW a versão que distribuía a potência por folga entre todos os ônibus (commit `7797aab`) ficava abaixo do `fifo` (por exemplo 24.1 contra 34.4 a 400 kW sem falhas). Atendendo primeiro o maior grupo que o limite consegue terminar, o planner fica à frente em todas as linhas (40.4 contra 34.4 a 400 kW sem falhas; 31.2 contra 26.3 a 300 kW), com ou sem rodízio. O preço: quem fica de fora recebe pouco, e a energia total que falta aos ônibus não prontos (`shortfall kWh`) fica no nível do `fifo` sem falhas (5.772 contra 5.698 kWh a 400 kW, +1,3%), abaixo dele com falhas (6.383 contra 6.695 kWh a 400 kW no `severe`).
- **Os perfis com falhas continuam difíceis.** No `severe` a 2000 kW, 11,5% dos ônibus saem sem a carga necessária; a 600 kW, só 40.9% saem prontos. A 500 kW sem falhas o rodízio custa 0,1 ponto (49.2 contra 49.3 sem rodízio). Em nenhuma linha o planner com rodízio fica abaixo de `fifo`, `edf` ou `fifo-unplug`, o que os testes de propriedade verificam com 8 sementes por padrão (`TestPlannerNotWorseThanBaselines`, 300, 400, 500, 600, 800, 1200 e 2000 kW, todos os perfis; 100 sementes no CI). Fora do empate em 100 a 2000 kW sem falhas, a menor folga é a 300 kW no `random`: 17.4 contra 16.8 do `fifo`.
- **Custo, pico e estabilidade.** O planner usa toda a potência disponível para aprontar ônibus, sem achatar o pico. A 2000 kW sem falhas ele custa R$ 11.331 contra R$ 8.130 do `fifo` (+39%), mas entrega 10.832 kWh contra 7.275 kWh (+49%, coluna `energy kWh`), porque carrega mais ônibus; contra o `fifo-unplug` custa +3,9% (R$ 11.331 contra R$ 10.907) e entrega 4,5% mais energia (10.832 contra 10.361 kWh; o planner mira o alvo da rota mais uma margem de segurança de 10 kWh por ônibus). O pico chega ao limite da garagem tanto no planner quanto nos baselines. O planner muda o plano bem mais vezes (297 contra 59 do `fifo` sem falhas; 581 contra 393 no `severe`). Quem preferir achatar o pico pode limitar a sobra com `Config.SurplusLaxityMin` (por exemplo 120 minutos, o padrão anterior), ao custo de menos ônibus prontos; esse ajuste não tem opção no `simrun`.
- **Tempo de cálculo** (p99 por ciclo, 50 ônibus, `simrun` rodando sozinho nesta máquina, uma linha por vez, de 300 a 2000 kW em todos os perfis): de 0,3 a 0,7 ms com rodízios seguidos e de 0,4 a 1,2 ms com `-follow-swaps=false` (a busca de rodízios roda enquanto há ônibus esperando, e sem rodízio executado eles esperam mais). No pior caso de benchmark (200 ônibus, 100 esperando, todos os carregadores com potências diferentes, de modo que a busca de rodízios chega ao teto de 256 tentativas por ciclo: `BenchmarkPlanNormalSwapSearch200Distinct`, e `BenchmarkPlannerPlanSwapSearch200Distinct` para o planner completo) um ciclo leva de 9,2 a 9,6 ms nesta máquina (`go test -run '^$' -bench SwapSearch200Distinct -benchtime 100x ./internal/planner`). Esses tempos variam bastante com a máquina e a carga; a meta é 50 ms por ciclo.

Os cenários são sintéticos e usam premissas de dados públicos ainda não validadas com operadoras reais, então esses números mostram a comparação entre controladores, não o desempenho esperado em campo.

Todas as premissas de carga vêm de dados públicos e ainda precisam de validação com operadoras reais.
