# depot-charge-planner

Planejador de recarga para garagens de ônibus elétricos: decide quanta potência cada carregador entrega a cada ônibus para que o máximo de ônibus saia com a carga necessária, respeitando o limite da garagem e continuando a funcionar quando a infraestrutura falha.

Status: em desenvolvimento (planejador + simulador). Design: `docs/superpowers/specs/`. Plano: `docs/superpowers/plans/`.

## Uso rápido

    go test -race ./...
    go run ./cmd/simrun -profile severe -seeds 20

Opções do `simrun`: `-buses`, `-chargers`, `-limit` (kW), `-profile none|mild|severe`, `-seeds`, `-log decisions.jsonl`.
Colunas: `ready%` (principal), `plan violations` (deve ser 0), `overshoot min` (potência física acima do limite).

`plan violations` conta os minutos em que a potência comandada passou do limite (deve ser 0). `overshoot min` conta os minutos em que a potência física passou do limite: pode ser diferente de zero mesmo com `plan violations` 0, porque os comandos só fazem efeito um minuto depois e uma queda repentina do limite (perfis `mild` e `severe`) pode causar excesso por cerca de um minuto mesmo com um plano correto. Nas execuções com `-seeds 20` o `overshoot min` do planner foi 0 (sem falhas e perfil `mild`) e 1 (perfil `severe`), contra 5 a 8 minutos nos baselines nos perfis `mild` e `severe`.

## Resultados de exemplo

`ready%` médio (percentual de ônibus prontos na saída) com os parâmetros padrão e `-seeds 20`:

| perfil de falhas | fifo | edf | safe | planner |
|---|---|---|---|---|
| none | 58.4 | 58.4 | 53.1 | 75.6 |
| mild | 58.3 | 58.3 | 53.1 | 75.9 |
| severe | 45.2 | 45.3 | 50.7 | 61.0 |

Nessas execuções o planner custa cerca de 13% a 14% mais (R$ 9.203 a 9.488 contra R$ 8.130 a 8.312 de fifo/edf) e troca o plano de 1,6 a 4,3 vezes mais vezes (por exemplo, 636 contra cerca de 390 no perfil severe e 256 contra cerca de 60 sem falhas) em troca de bem mais ônibus prontos.

Os cenários são sintéticos e usam premissas de dados públicos ainda não validadas com operadoras reais, então esses números mostram a comparação entre controladores, não o desempenho esperado em campo.

Todas as premissas de carga vêm de dados públicos e ainda precisam de validação com operadoras reais.
