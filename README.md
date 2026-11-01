# depot-charge-planner

Planejador de recarga para garagens de ônibus elétricos: decide quanta potência cada carregador entrega a cada ônibus para que o máximo de ônibus saia com a carga necessária, respeitando o limite da garagem e continuando a funcionar quando a infraestrutura falha.

Status: em desenvolvimento (planejador + simulador + validação com dados reais, ainda sem dados de uma operadora). Design: `docs/superpowers/specs/`. Plano: `docs/superpowers/plans/`.

## Uso rápido

    go test -race ./...
    go run ./cmd/simrun -profile severe -seeds 20
    go run ./cmd/simrun -profile severe -seeds 20 -follow-swaps=false

Opções do `simrun`: `-buses`, `-chargers`, `-limit` (kW, até 1e7), `-profile none|mild|severe|random`, `-seeds`, `-follow-swaps` (padrão `true`), `-swap-back-cooldown` (minutos, padrão 30) e `-swap-back-min-need` (kWh, padrão 10) da regra contra o vaivém de rodízios (os dois em 0 desligam a regra; veja abaixo), `-reading-age` (minutos de idade que o sensor informa para uma leitura nova, padrão 0), `-log decisions.jsonl`. Custo de energia (opcional, desligado por padrão; veja `go run ./cmd/simrun -help`): `-cost-aware` (planejador adia a carga para depois da ponta e, com `-demand-target KW`, limita a demanda; `-defer-margin MIN`, padrão 45), `-demand-peak-price`/`-demand-offpeak-price` (R$/kW; no modo `-demand-mode verde` só o preço fora da ponta vale), `-demand-contracted KW`, `-arrival-from`/`-arrival-to` (minutos desde as 18:00; 0 a 180 é a ponta) e `-nights-per-month` (padrão 30). Com preço de demanda a tabela ganha `demand kW`, `demand R$/mês` e `monthly R$` (estimativa: energia da noite × noites + demanda da noite) e uma linha com a economia mensal estimada do `planner` contra `fifo` e `fifo-unplug`.
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
- **Dados reais:** importa as planilhas de uma garagem de verdade e compara com o que aconteceu (veja "Validação com dados reais", abaixo).

Limites para a tela não travar: ônibus × sementes até 20000 na comparação e até 500 ônibus e 500 carregadores numa execução detalhada (e sementes de 1 a 1000).

API (JSON): `GET /api/defaults`, `POST /api/compare`, `POST /api/run`, `POST /api/import` e `POST /api/replay` (os dois últimos, e `/api/run` com `files`, recebem as planilhas no corpo, até 16 MB); o formato das rotas `/api/defaults`, `/api/compare` e `/api/run` sem `files` está em `docs/superpowers/specs/2026-10-05-laboratorio-web-design.md`; o de `/api/import`, `/api/replay` e `/api/run` com `files` está em `docs/superpowers/specs/2026-10-08-validacao-dados-reais-design.md` (seção 6) e em `internal/lab/realdata.go`. Testes da interface: `node --test web/test/*.test.mjs` (só desenvolvimento).

Os dados são sintéticos: mostram o comportamento do algoritmo, não o de uma garagem real.

## Validação com dados reais

Responde, com planilhas CSV de uma garagem de verdade, à pergunta: **nas mesmas condições em que a operação real trabalhou numa noite, o planejador teria deixado mais ônibus prontos, com menos energia, pico e custo?** Para cada noite, o simulador recebe as chegadas, as saídas, a carga de chegada, a carga exigida na saída, os carregadores e o limite da garagem, roda `fifo`, `edf`, `fifo-unplug`, `safe` e o `planner` (com e sem rodízios) e mostra uma linha `real` com o que as planilhas registram. O kit para pedir os dados a uma operadora (texto do pedido, formato, limites aceitos, LGPD e limitações dos números) está em `docs/dados-reais/README.md`; os modelos de planilha estão em `docs/dados-reais/modelos/`.

    go run ./cmd/replay -dir internal/realdata/testdata/demo

`internal/realdata/testdata/demo` é um exemplo **fictício** completo (2 noites de 3 ônibus, com sessões, potência e tarifa); nenhum número dele vem de uma garagem real. Os modelos de `docs/dados-reais/modelos/` têm uma noite com pouca informação, só para mostrar o formato. Trecho da saída para a primeira noite do exemplo:

    Noite 2026-03-04 (3 ônibus)
    controller             ready%  shortfall kWh  peak kW         plan violations  overshoot min  energy kWh  cost R$         plan changes  moves/run
    real                   66.7    45.0           120 (estimado)  —                —              480         522 (estimado)  —             —
    fifo                   100.0   0.0            150             0                0              565         774             6             0.0
    planner                100.0   0.0            150             0                0              598         804             6             0.0
    planner (sem rodízio)  100.0   0.0            150             0                0              598         804             6             0.0
    Comparação justa (mesmos 3 ônibus do real): real 66.7% | planner 100.0% | sem rodízio 100.0% (shortfall kWh: real 45.0 | planner 0.0 | sem rodízio 0.0)
    Ônibus não prontos na realidade (alvo; real; planner; planner sem rodízio):
      B02: alvo 255.0 kWh; real 210.0 kWh (não pronto); planner 268.1 kWh (pronto); sem rodízio 268.1 kWh (pronto)

(O programa imprime também as linhas `edf`, `fifo-unplug` e `safe`, as premissas e o agregado das noites; aqui foram omitidos.) As colunas são as do `simrun`. Opções do `replay`:

- `-dir PASTA` (obrigatório): pasta com `garagem.csv`, `carregadores.csv`, `onibus.csv` e, se houver, `sessoes.csv` e `potencia.csv`.
- `-soc-noise KWH` (padrão 0): ruído (desvio padrão, em kWh) nas leituras de carga que o planejador recebe, para testar a sensibilidade a sensores imprecisos.
- `-swap-back-cooldown MIN` (padrão 30) e `-swap-back-min-need KWH` (padrão 10): a regra anti-vaivém do rodízio, como no `simrun` (os dois em 0 desligam a regra).
- `-cost-aware` (padrão desligado): o `planner` passa a considerar o custo da energia, como no `simrun`: adia a carga para depois da janela de ponta quando os ônibus têm folga e, com `-demand-target`, limita a demanda. Só o `planner` muda; as outras linhas não. `-defer-margin MIN` (padrão 45; 0 a 240; 0 = 45) e `-demand-target KW` (padrão 0 = desligado) só valem com `-cost-aware` (sem ele o programa recusa, saída 2).
- `-announce-arrivals` (só com `-cost-aware`): o `planner` conhece a agenda da noite, isto é, os ônibus que ainda não chegaram. **O padrão é não conhecer**: quem reproduz uma noite gravada não tem escala anunciada, a menos que diga que tem, e adiar a carga de um ônibus sem saber se os que chegam depois encontram carregador livre perdeu ônibus nos testes. Por isso, sem `-announce-arrivals`, `-cost-aware` não adia nada (só a meta de demanda age) e o programa avisa isso numa linha em stderr; as premissas impressas dizem qual modo rodou.
- `-json`: escreve o relatório completo (por noite, por controlador e por ônibus) em JSON, em vez das tabelas.
- `-workers N` (padrão: número de CPUs): execuções simultâneas; o resultado não depende disso.

**Medido × estimado.** A linha `real` mostra o que as planilhas registram, e `—` é dado que elas não trazem. `ready%` e `shortfall kWh` vêm de `soc_saida_real_pct` (pronto = carga real na saída pelo menos igual à exigida, a mesma regra do simulador; ônibus sem esse campo não entram nem como prontos nem como não prontos; por isso a linha "Comparação justa" refaz `ready%` e `shortfall` do `planner` e do `planner (sem rodízio)` só com esses mesmos ônibus, para a comparação com a linha `real` não ficar enviesada). Um ônibus que saiu mais de 15 minutos depois do previsto (`saida_real`) conta como pronto se a carga estava completa na saída; o relatório avisa quantos foram. No agregado, a maioria das colunas é média por noite, mas `plan violations` e `overshoot min` são somas das noites e `plan changes` é a média arredondada para baixo. `energy kWh` vem das sessões (ou, sem sessões, da integral de `potencia.csv`). `peak kW` e `cost R$` vêm de `potencia.csv` quando há leituras na noite (medidos); só com sessões, saem marcados `(estimado)`: a energia de cada sessão é espalhada de modo uniforme entre o início e o fim, o que costuma subestimar o pico. Violações do plano, minutos acima do limite, mudanças de plano e manobras não existem na realidade e aparecem como `—`. As limitações dos números (qual noite recebe cada leitura, leituras sem zero final etc.) estão em "Limitações dos números" no kit.

**Fatura com demanda.** Se `garagem.csv` tem as colunas opcionais `demanda_preco_ponta`, `demanda_preco_fora_ponta`, `demanda_contratada_kw`, `modo_demanda` e `noites_por_mes`, a tabela ganha `demand kW` (o maior bloco de 15 minutos alinhado ao relógio, não o pico instantâneo) e `monthly R$` (energia da noite x noites por mês + demanda cobrada, uma estimativa), a linha `real` leva o selo `(medido)` (leituras de `potencia.csv`) ou `(estimado)` (sessões espalhadas de modo uniforme), e o fim do relatório traz a "economia mensal estimada do planner" contra `fifo` e `fifo-unplug`, com a ressalva do `ready%`. Sem essas colunas a saída é a de antes. Detalhes em `docs/dados-reais/README.md`.

**Premissas** (impressas no relatório): as leituras de carga (SoC) são perfeitas, a não ser que se use `-soc-noise`; o limite de potência da garagem é fixo durante a noite e nenhum defeito de carregador é injetado (o que deu errado já está nos dados); `planner` supõe que os operadores executam todos os rodízios e `planner (sem rodízio)`, que nenhum é executado; e a operação real tomou decisões (ordem de ligação, rodízios manuais) que o planejador não vê, então o simulador reproduz as condições da noite, não o passado minuto a minuto.

**Aba "Dados reais" do laboratório.** No `go run ./cmd/lab`, a aba aceita os arquivos (ou a pasta inteira) por seletor ou por arrastar e soltar, mostra as noites encontradas e os avisos, e compara com a mesma tabela do `replay` (o `p99 µs` fica em 0 na aba). Cada noite pode ser aberta como uma execução detalhada (minuto a minuto), de qualquer controlador; essa execução detalhada **ignora a opção de ruído** e usa leituras perfeitas. Os arquivos importados nunca saem desta máquina: o navegador lê o texto e fala só com o laboratório em `127.0.0.1`, que não grava nada em disco; os dados também não vão no link "Copiar link desta tela", e fechar ou recarregar a página esquece tudo.

Ainda não houve validação com dados de uma operadora: tudo o que está neste repositório usa dados fictícios ou sintéticos.

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

## Custo de energia

Duas coisas, ambas opcionais e desligadas por padrão (sem as opções abaixo, nenhum número deste README muda e `plan violations` segue 0): **medir a fatura** de uma noite de forma mais próxima da conta brasileira de energia (energia por horário mais demanda de potência) e **deixar o planner reduzir essa fatura** sem deixar ônibus sem carga. A especificação está em `docs/superpowers/specs/2026-10-11-custo-de-energia-design.md`.

**Aviso, antes de tudo.** A tarifa é um modelo **paramétrico**, não a tarifa oficial de nenhuma distribuidora. Os preços de energia do gerador (ponta 18:00 a 21:00 a R$ 2,70/kWh, fora da ponta R$ 0,90/kWh) e os preços de demanda usados nas medições abaixo (R$ 50/kW na ponta e R$ 20/kW fora dela) são **ilustrativos**. Os valores reais, o modo (Azul ou Verde), a demanda contratada e a regra de ultrapassagem vêm da fatura e do contrato de cada garagem: **conferir com a distribuidora**. Os números em reais abaixo comparam estratégias entre si; não preveem a conta de ninguém.

### O modelo

- **Energia (R$/kWh).** Dois preços, ponta e fora da ponta, por janela de horário. A janela pode **atravessar a meia-noite** (por exemplo 22:00 a 06:00). `cost R$` continua sendo a energia da noite.
- **Demanda (R$/kW).** Cobra-se o maior valor médio de potência da rede em **blocos de 15 minutos alinhados ao relógio** (`:00`, `:15`, `:30`, `:45`), não o pico instantâneo. Dois modos:
  - **Azul:** um preço na ponta e outro fora dela, cada um sobre o maior bloco da sua janela.
  - **Verde:** uma só cobrança, sobre o maior bloco de toda a noite, ao preço de **fora da ponta**; o preço de demanda da ponta é **ignorado** (o `simrun` e o laboratório avisam).
- **Contrato (`-demand-contracted`).** Com demanda contratada, paga-se o contratado mesmo que se use menos (baixar o pico abaixo do contratado não baixa a fatura). Até 105% do contratado (tolerância de 5%) cobra-se o contratado; acima disso, o contratado mais o excedente acima do contratado a **2 vezes** o preço de demanda. No Azul o mesmo valor vale para cada janela. Sem contrato (0), cobra-se o maior bloco medido, que serve de proxy do que seria contratado e é onde reduzir o pico aparece em reais.
- **Estimativa mensal.** `energia da noite x noites por mês (padrão 30) + demanda da noite`. É um **proxy**: trata uma noite como o mês inteiro, e na fatura real a demanda vale o maior bloco do mês. O número é rotulado "estimativa" em todas as telas (`monthly R$`, "demanda (proxy mensal)", "fatura mensal estimada").
- Uma só biblioteca (`internal/billing`) calcula a fatura do simulador, do `replay` (linha `real`, a partir de `potencia.csv` ou, estimada, das sessões) e do laboratório, para que real e simulado não divirjam por causa de duas implementações.

### Como usar

- `simrun`: `-cost-aware` liga a política de custo do `planner` (as outras linhas da tabela não mudam); `-defer-margin MIN` (folga mínima, padrão 45) e `-demand-target KW` só valem com `-cost-aware` (sem ele o programa recusa). Preços e janela de chegadas: `-demand-peak-price`, `-demand-offpeak-price`, `-demand-mode azul|verde`, `-demand-contracted`, `-nights-per-month`, `-arrival-from` e `-arrival-to` (minutos desde as 18:00; 0 a 180 é a ponta). Com preços de demanda a tabela ganha `demand kW`, `demand R$/mês` e `monthly R$` e uma linha com a economia mensal estimada do `planner` contra `fifo` e `fifo-unplug`. O `simrun` com `-cost-aware` **anuncia a agenda de chegadas** ao `planner` (veja as limitações).
- `replay`: `-cost-aware`, `-defer-margin`, `-demand-target` e `-announce-arrivals`; a fatura com demanda vem das colunas opcionais de `garagem.csv` (veja "Fatura com demanda" e `docs/dados-reais/README.md`). O `replay` é **às cegas** por padrão (não conhece os ônibus que ainda não chegaram) e só adia carga com `-announce-arrivals`.
- Laboratório: os campos avançados "custo de energia" da aba Cenário (`cost_aware`, `defer_margin_min`, `demand_target_kw`, `demand_peak_price`, `demand_offpeak_price`, `demand_contracted_kw`, `demand_verde`, `arrival_from_min`, `arrival_to_min` e `nights_per_month` na API; as chegadas na interface são em horas do dia), os cenários prontos "Chegadas na ponta (custo)" e "Fatura com demanda (ilustrativa)", e na Comparação as colunas de demanda e fatura mensal, a frase com a diferença mensal do `planner` e, quando há alvo de demanda, a comparação com a mesma política sem alvo. A aba "Dados reais" ainda não tem as opções de custo do `planner`.

### Premissas das medições

Todos os números abaixo saem do `simrun` com o código deste repositório, 20 sementes, **ônibus que chegam entre 18:00 e 21:00** (`-arrival-from 0 -arrival-to 180`, ou seja, dentro da ponta) e saem entre 04:30 e 07:00, carregadores de 150 kW, bateria de 350 kWh, preços de demanda ilustrativos **Azul R$ 50/kW na ponta e R$ 20/kW fora**, **sem contrato**, 30 noites por mês e tarifa de energia padrão do gerador. "Simples" é o `planner` sem `-cost-aware`; "com custo" é `-cost-aware` (agenda anunciada); "alvo N" acrescenta `-demand-target N`. As colunas são médias por semente; `plan violations` foi 0 em todas as execuções de todas as tabelas (soma das 20 sementes, todos os controladores). Exemplo de comando: `go run ./cmd/simrun -buses 50 -chargers 50 -limit 2000 -profile severe -seeds 20 -arrival-from 0 -arrival-to 180 -demand-peak-price 50 -demand-offpeak-price 20 -cost-aware`.

### Resultados medidos

**Garagem padrão: 50 ônibus, 25 carregadores de 150 kW**

| perfil | limite (kW) | planner | ready% | energia (R$/noite) | demanda (kW) | mensal estimado (R$) | mensal contra o planner simples | mensal contra o mesmo planner sem alvo |
|---|---|---|---|---|---|---|---|---|
| none | 600 | simples | 66,8 | 9.775 | 600 | 335.249 | — | — |
| none | 600 | com custo | 66,8 | 9.775 | 600 | 335.249 | 0,0% | — |
| none | 600 | com custo e alvo 500 | 66,8 | 9.770 | 600 | 335.105 | 0,0% | 0,0% |
| none | 600 | com custo e alvo 800 | 66,8 | 9.775 | 600 | 335.249 | 0,0% | 0,0% |
| none | 1.200 | simples | 100,0 | 15.527 | 1.200 | 549.801 | — | — |
| none | 1.200 | com custo | 100,0 | 15.527 | 1.200 | 549.801 | 0,0% | — |
| none | 1.200 | com custo e alvo 500 | 100,0 | 15.518 | 1.200 | 549.521 | -0,1% | -0,1% |
| none | 1.200 | com custo e alvo 800 | 100,0 | 15.522 | 1.200 | 549.616 | 0,0% | 0,0% |
| none | 2.000 | simples | 100,0 | 18.706 | 1.999 | 700.398 | — | — |
| none | 2.000 | com custo | 100,0 | 18.706 | 1.999 | 700.398 | 0,0% | — |
| none | 2.000 | com custo e alvo 500 | 100,0 | 18.687 | 1.999 | 699.567 | -0,1% | -0,1% |
| none | 2.000 | com custo e alvo 800 | 100,0 | 18.552 | 1.998 | 684.028 | -2,3% | -2,3% |
| severe | 600 | simples | 46,2 | 9.339 | 600 | 322.170 | — | — |
| severe | 600 | com custo | 46,2 | 9.339 | 600 | 322.170 | 0,0% | — |
| severe | 600 | com custo e alvo 500 | 46,2 | 9.337 | 600 | 322.111 | 0,0% | 0,0% |
| severe | 600 | com custo e alvo 800 | 46,2 | 9.339 | 600 | 322.170 | 0,0% | 0,0% |
| severe | 1.200 | simples | 82,3 | 16.021 | 1.200 | 564.510 | — | — |
| severe | 1.200 | com custo | 82,3 | 16.021 | 1.200 | 564.510 | 0,0% | — |
| severe | 1.200 | com custo e alvo 500 | 82,3 | 16.019 | 1.200 | 564.434 | 0,0% | 0,0% |
| severe | 1.200 | com custo e alvo 800 | 82,3 | 16.020 | 1.200 | 564.458 | 0,0% | 0,0% |
| severe | 2.000 | simples | 89,4 | 19.376 | 1.997 | 720.041 | — | — |
| severe | 2.000 | com custo | 89,4 | 19.376 | 1.997 | 720.041 | 0,0% | — |
| severe | 2.000 | com custo e alvo 500 | 89,0 | 19.354 | 1.997 | 719.313 | -0,1% | -0,1% |
| severe | 2.000 | com custo e alvo 800 | 89,6 | 19.255 | 1.997 | 714.228 | -0,8% | -0,8% |

**Uma tomada por ônibus: 30 ônibus, 30 carregadores**

| perfil | limite (kW) | planner | ready% | energia (R$/noite) | demanda (kW) | mensal estimado (R$) | mensal contra o planner simples | mensal contra o mesmo planner sem alvo |
|---|---|---|---|---|---|---|---|---|
| none | 800 | simples | 100,0 | 9.740 | 800 | 348.209 | — | — |
| none | 800 | com custo | 100,0 | 9.628 | 800 | 344.844 | -1,0% | — |
| none | 800 | com custo e alvo 400 | 100,0 | 9.623 | 800 | 344.676 | -1,0% | 0,0% |
| none | 800 | com custo e alvo 600 | 100,0 | 9.608 | 800 | 342.422 | -1,7% | -0,7% |
| none | 1.500 | simples | 100,0 | 12.533 | 1.500 | 480.567 | — | — |
| none | 1.500 | com custo | 100,0 | 5.836 | 1.500 | 205.075 | -57,3% | — |
| none | 1.500 | com custo e alvo 750 | 100,0 | 9.515 | 750 | 337.936 | -29,7% | +64,8% |
| none | 1.500 | com custo e alvo 1.000 | 100,0 | 8.610 | 1.000 | 324.590 | -32,5% | +58,3% |
| none | 2.500 | simples | 100,0 | 14.513 | 2.357 | 591.923 | — | — |
| none | 2.500 | com custo | 100,0 | 5.842 | 2.500 | 225.275 | -61,9% | — |
| none | 2.500 | com custo e alvo 1.000 | 100,0 | 8.610 | 1.000 | 324.590 | -45,2% | +44,1% |
| none | 2.500 | com custo e alvo 1.500 | 100,0 | 5.836 | 1.500 | 205.075 | -65,4% | -9,0% |
| severe | 800 | simples | 91,2 | 10.179 | 800 | 361.349 | — | — |
| severe | 800 | com custo | 90,8 | 10.044 | 800 | 357.300 | -1,1% | — |
| severe | 800 | com custo e alvo 400 | 90,8 | 10.037 | 800 | 357.075 | -1,2% | -0,1% |
| severe | 800 | com custo e alvo 600 | 89,7 | 10.008 | 800 | 355.995 | -1,5% | -0,4% |
| severe | 1.500 | simples | 99,7 | 13.066 | 1.492 | 494.813 | — | — |
| severe | 1.500 | com custo | 98,3 | 6.567 | 1.500 | 235.747 | -52,4% | — |
| severe | 1.500 | com custo e alvo 750 | 98,7 | 10.097 | 1.286 | 366.127 | -26,0% | +55,3% |
| severe | 1.500 | com custo e alvo 1.000 | 98,7 | 9.280 | 1.272 | 350.846 | -29,1% | +48,8% |
| severe | 2.500 | simples | 100,0 | 15.127 | 2.316 | 609.878 | — | — |
| severe | 2.500 | com custo | 99,5 | 6.741 | 2.500 | 266.819 | -56,3% | — |
| severe | 2.500 | com custo e alvo 1.000 | 99,5 | 9.330 | 2.006 | 367.023 | -39,8% | +37,6% |
| severe | 2.500 | com custo e alvo 1.500 | 98,7 | 6.634 | 2.060 | 248.976 | -59,2% | -6,7% |

**Uma tomada por ônibus: 50 ônibus, 50 carregadores**

| perfil | limite (kW) | planner | ready% | energia (R$/noite) | demanda (kW) | mensal estimado (R$) | mensal contra o planner simples | mensal contra o mesmo planner sem alvo |
|---|---|---|---|---|---|---|---|---|
| none | 600 | simples | 71,4 | 9.881 | 600 | 338.428 | — | — |
| none | 600 | com custo | 71,4 | 9.881 | 600 | 338.428 | 0,0% | — |
| none | 600 | com custo e alvo 300 | 71,4 | 9.880 | 600 | 338.396 | 0,0% | 0,0% |
| none | 600 | com custo e alvo 500 | 71,4 | 9.879 | 600 | 338.370 | 0,0% | 0,0% |
| none | 1.200 | simples | 100,0 | 15.648 | 1.200 | 553.447 | — | — |
| none | 1.200 | com custo | 100,0 | 15.646 | 1.200 | 553.379 | 0,0% | — |
| none | 1.200 | com custo e alvo 600 | 100,0 | 15.636 | 1.200 | 553.077 | -0,1% | -0,1% |
| none | 1.200 | com custo e alvo 800 | 100,0 | 15.640 | 1.200 | 553.195 | 0,0% | 0,0% |
| none | 2.000 | simples | 100,0 | 18.985 | 2.000 | 709.243 | — | — |
| none | 2.000 | com custo | 100,0 | 11.605 | 2.000 | 421.060 | -40,6% | — |
| none | 2.000 | com custo e alvo 1.000 | 100,0 | 11.591 | 2.000 | 420.642 | -40,7% | -0,1% |
| none | 2.000 | com custo e alvo 1.600 | 100,0 | 14.801 | 1.600 | 555.491 | -21,7% | +31,9% |
| severe | 600 | simples | 50,8 | 9.446 | 600 | 325.365 | — | — |
| severe | 600 | com custo | 50,8 | 9.446 | 600 | 325.365 | 0,0% | — |
| severe | 600 | com custo e alvo 300 | 50,8 | 9.446 | 600 | 325.366 | 0,0% | 0,0% |
| severe | 600 | com custo e alvo 500 | 50,8 | 9.445 | 600 | 325.360 | 0,0% | 0,0% |
| severe | 1.200 | simples | 84,5 | 16.322 | 1.200 | 573.665 | — | — |
| severe | 1.200 | com custo | 84,6 | 16.320 | 1.200 | 573.606 | 0,0% | — |
| severe | 1.200 | com custo e alvo 600 | 84,4 | 16.311 | 1.200 | 573.327 | -0,1% | 0,0% |
| severe | 1.200 | com custo e alvo 800 | 84,4 | 16.313 | 1.200 | 573.400 | 0,0% | 0,0% |
| severe | 2.000 | simples | 98,2 | 19.992 | 1.999 | 738.639 | — | — |
| severe | 2.000 | com custo | 95,4 | 12.766 | 2.000 | 467.611 | -36,7% | — |
| severe | 2.000 | com custo e alvo 1.000 | 95,4 | 12.759 | 2.000 | 467.393 | -36,7% | 0,0% |
| severe | 2.000 | com custo e alvo 1.600 | 96,8 | 15.881 | 1.783 | 591.372 | -19,9% | +26,5% |

Perfis `none` e `severe` (falhas), 20 sementes; as colunas "mensal contra..." comparam o mensal estimado da linha com o do `planner` simples e, para as linhas com alvo, com o `planner` com custo sem alvo. Leitura:

- **Na garagem padrão (50 ônibus, 25 carregadores), a política de custo não economiza nada.** Ligar `-cost-aware` sem alvo dá exatamente os mesmos números do `planner` simples em todos os limites e perfis (por exemplo 2.000 kW `none`: R$ 18.706 por noite, mensal R$ 700.398, nos dois), porque ela só adia a carga quando há uma tomada para cada ônibus. Os alvos de 500 e 800 kW são inertes nos limites de 600 e 1.200 kW (diferença de 0,1% ou menos) e a 2.000 kW o de 500 kW também (-0,1%); o de 800 kW a 2.000 kW baixa o mensal 2,3% (R$ 684.028 contra R$ 700.398) e a demanda não muda. Um alvo viável nessa garagem funciona: a 2.000 kW `none`, alvo 1.600 kW baixa a demanda de 1.999 para 1.600 kW (-20,0%), a energia de R$ 18.706 para R$ 17.170 por noite e o mensal de R$ 700.398 para R$ 626.853 (-10,5%), com o mesmo `ready%` (100,0); no `severe` a demanda cai de 1.997 para 1.692 kW, o mensal de R$ 720.041 para R$ 648.856 e o `ready%` vai de 89,4 para 89,6. Mesmo assim, contra o `fifo-unplug` (a rotina manual de hoje) o `planner` custa 1,1% a mais a 2.000 kW `none` (R$ 700.398 contra R$ 692.823 por mês, os dois com 100% de ônibus prontos) porque entrega mais energia, e contra o `fifo` custa 18,9% a mais (o `fifo` deixa 41,4% dos ônibus sem carga). Com o alvo de 1.600 kW, o `planner` fica 9,5% abaixo do `fifo-unplug` (R$ 65.969 por mês) a 2.000 kW `none`.
- **Em garagens com uma tomada por ônibus a economia é grande, quando o limite deixa folga.** 30 ônibus/30 carregadores a 1.500 kW `none`: energia de R$ 12.533 para R$ 5.836 por noite, mensal de R$ 480.567 para R$ 205.075 (-57,3%), e 56,4% (R$ 265.356 por mês) abaixo do `fifo-unplug`; a 2.500 kW, -61,9%. 50/50 a 2.000 kW `none`: -40,6% (R$ 709.243 para R$ 421.060) e 39,2% (R$ 271.763) abaixo do `fifo-unplug`; no `severe`, -36,7% e 34,3% abaixo do `fifo-unplug`. O limite precisa deixar folga: com limite apertado não há como adiar, e o ganho some (30/30 a 800 kW: -1,0%; 50/50 a 600 e 1.200 kW: 0,0%).
- **O adiamento não baixa a demanda e pode subir.** Na tabela, a demanda com custo é igual ao limite (ou ao pico do simples) e, quando o simples não chegava ao limite, ela sobe: 30/30 a 2.500 kW `none`, de 2.357 para 2.500 kW (+6,1%). A economia vem da energia fora da ponta, não do pico.

**Sob falhas o ganho é menor e a prontidão pode cair.** `ready%` médio e mensal estimado do `planner` simples e com custo, três margens de adiamento (`-defer-margin`), 20 sementes, mesmos preços e chegadas:

| garagem | perfil | ready% planner simples | ready% com custo, margem 45 / 90 / 120 min | mensal estimado (R$) simples | mensal com custo, margem 45 / 90 / 120 min |
|---|---|---|---|---|---|
| 30/30 a 1.500 kW | mild | 100,0 | 100,0 / 100,0 / 100,0 | 486.202 | 211.085 / 214.047 / 231.168 |
| 30/30 a 1.500 kW | random | 99,0 | 97,7 / 97,5 / 97,3 | 476.371 | 309.189 / 311.893 / 319.644 |
| 30/30 a 1.500 kW | severe | 99,7 | 98,3 / 98,3 / 98,0 | 494.813 | 235.747 / 239.003 / 255.956 |
| 50/50 a 2.000 kW | mild | 100,0 | 100,0 / 100,0 / 100,0 | 719.298 | 430.865 / 495.084 / 535.420 |
| 50/50 a 2.000 kW | random | 94,4 | 93,4 / 93,7 / 93,8 | 727.420 | 512.912 / 561.874 / 592.024 |
| 50/50 a 2.000 kW | severe | 98,2 | 95,4 / 96,0 / 96,7 | 738.639 | 467.611 / 526.026 / 564.426 |

Margem maior deixa mais folga e perde menos ônibus, mas economiza menos: 50/50 a 2.000 kW `severe`, margens 45, 90 e 120 min dão `ready%` 95,4, 96,0 e 96,7 contra 98,2 do simples e mensal de R$ 467.611, R$ 526.026 e R$ 564.426 contra R$ 738.639. Veja as limitações.

**O alvo de demanda é uma troca do operador, e pode subir a fatura mensal.** Baixar o pico não é, por si só, economia na fatura. Com o adiamento ligado, um alvo abaixo da carga que o adiamento concentra depois da ponta faz os ônibus carregarem durante ela: 50/50 a 2.000 kW `none`, alvo 1.600 kW: demanda de 2.000 para 1.600 kW (-20%), mas mensal de R$ 421.060 para R$ 555.491 (+31,9% contra a mesma política sem alvo); 30/30 a 1.500 kW, alvo 750 kW: R$ 205.075 para R$ 337.936 (+64,8%), alvo 1.000 kW: +58,3%. Pode também ajudar: 30/30 com limite de 2.500 kW e alvo 1.500 kW, -9,0% (R$ 225.275 para R$ 205.075). Quando existe **demanda contratada** igual ao limite, a demanda cobrada não muda (R$ 140.000 por mês nas duas linhas de 50/50 a 2.000 kW com contrato de 2.000 kW) e a fatura sobe do mesmo jeito (R$ 488.135 para R$ 584.037; 30/30 a 1.500 kW com contrato de 1.500 kW e alvo 750 kW: R$ 280.075 para R$ 390.436): o piso do contrato já supera a demanda medida. Ajuste o alvo olhando a `monthly R$`, não a `demand kW`.

### Limitações (leia antes de confiar nos números)

1. **A tarifa é paramétrica, não oficial.** Preços, janelas, o modo Azul/Verde, a tolerância de 5% e a ultrapassagem a 2 vezes são parâmetros do modelo; **conferir com a distribuidora**. O Verde usa o preço de demanda fora da ponta e ignora o preço de demanda da ponta. O contrato único vale para as duas janelas do Azul (na prática há dois contratos) e a regra exata entre o contratado e 105% dele é a simplificação "cobra-se o contratado".
2. **O adiamento para depois da ponta só age quando a agenda de chegadas é anunciada e há uma tomada saudável para cada ônibus** (ônibus presentes mais anunciados que ainda não saíram, no máximo o número de carregadores saudáveis). Nos depósitos com menos tomadas que ônibus, como a garagem padrão de 50 ônibus/25 carregadores e o cenário de rodízio do projeto, ele **não faz nada** (os números são os do `planner` simples), e sem agenda também não adia (não há adiamento às cegas: ele perdia ônibus nos testes). O `replay` é às cegas por padrão; `-announce-arrivals` é a opção de quem sabe a escala da noite.
3. **Exposição a falhas.** Com os perfis `severe` e `random` a prontidão pode cair com a política ligada (tabela acima: por exemplo 98,2 para 95,4 no 50/50 a 2.000 kW `severe`, margem 45). A garantia por semente (`ready%` nunca abaixo do `planner` simples, em toda semente) vale para os perfis `none` e `mild` e é verificada pelos testes nas garagens listadas; para `severe` e `random` os testes fixam apenas limites, medidos em 8 sementes nas garagens com uma tomada por ônibus: no máximo **40 pontos de `ready%` perdidos no total, 20 na pior semente e 7 sementes piores** (`faultBounds`, em `internal/sim/cost_property_test.go`), e para o alvo de demanda **30 pontos no total, 22 na pior semente e 3 sementes piores** (`capFaultBounds`, em `internal/sim/demand_property_test.go`). A versão anterior do adiamento, que ainda agia nos depósitos com menos tomadas que ônibus, tinha limites de 60 pontos no total, 32 na pior semente e 6 sementes piores. Os limites são os piores casos medidos, não uma garantia: uma garagem real com falhas pode ficar pior.
4. **Tolerância de custo de 0,2%.** Em alguns casos o custo de energia pode ser até 0,2% maior com a política ligada do que sem ela, em alguma semente (os testes aceitam 0,2%; a varredura larga de 300 geometrias mediu no máximo 0,021%).
5. **Anunciar as chegadas põe ônibus futuros no `Input` do planner e no registro de decisões** (`-log`): o `planner` com agenda anunciada recebe e grava, a cada minuto, também os ônibus que ainda não chegaram, com horário e carga de chegada. Isso é uma suposição sobre a garagem (que ela tem a escala em mãos) e aumenta o registro.
6. **O alvo de demanda (`demand-target`) é uma troca, não uma economia garantida.** Pode subir a fatura mensal (linhas 30/30 e 50/50 acima), não economiza nada quando o piso do contrato de demanda já supera a demanda medida, e alvos muito abaixo do limite que a energia da noite não comporta são inertes (não mudam nada). Em garagens com **vários ônibus por carregador e sem agenda anunciada** ele pode perder até **4 pontos de `ready%`** em uma semente (medido, em até 6 de 8 sementes de uma célula): **não ligue o alvo nessas garagens sem uma escala**. Com a agenda anunciada, a demanda ficou acima da mesma política sem alvo em 5 de 3.120 sementes de uma varredura aleatória medida uma vez, fora do CI (até 2% do limite), e `ready%` nunca ficou abaixo. As medições da garagem padrão acima anunciam a agenda (é o que o `simrun` faz com `-cost-aware`); sem ela o alvo age menos, porque espera todas as tomadas serem ocupadas. Limitação de pico não é, por si só, economia na fatura.
7. **A estimativa mensal trata uma noite como o mês.** `monthly R$` multiplica a energia de uma noite por 30 e soma a demanda dessa noite como se fosse a do mês; noites diferentes, fins de semana, bandeiras tarifárias, impostos e a demanda máxima do mês inteiro não entram. Serve para comparar estratégias, não para prever a fatura.
8. **Dados sintéticos.** Todas as tabelas usam o gerador do simulador e preços ilustrativos; nenhum número vem de uma garagem real.

## Próximos passos

O roadmap (validar com dados reais, modo sombra, custo de energia, dimensionamento e tela para o pátio; o custo de energia já está implementado, veja acima) está em `docs/superpowers/plans/2026-10-08-proximos-passos.md`. Para pedir dados a uma operadora, veja `docs/dados-reais/README.md`.

## Licença

MIT. Veja o arquivo [LICENSE](LICENSE).
