# depot-charge-planner

Planejador de recarga para garagens de ônibus elétricos: decide quanta potência cada carregador entrega a cada ônibus para que o máximo de ônibus saia com a carga necessária, respeitando o limite da garagem e continuando a funcionar quando a infraestrutura falha.

Status: em desenvolvimento (planejador + simulador). Design: `docs/superpowers/specs/`. Plano: `docs/superpowers/plans/`.

## Uso rápido

    go test -race ./...
    go run ./cmd/simrun -profile severe -seeds 20

Opções do `simrun`: `-buses`, `-chargers`, `-limit` (kW), `-profile none|mild|severe`, `-seeds`, `-log decisions.jsonl`.
Colunas: `ready%` (principal), `plan violations` (deve ser 0), `overshoot min` (potência física acima do limite).

## Resultados de exemplo

`ready%` médio (percentual de ônibus prontos na saída) com os parâmetros padrão e `-seeds 20`:

| perfil de falhas | fifo | edf | safe | planner |
|---|---|---|---|---|
| none | 58.4 | 58.4 | 53.1 | 75.6 |
| mild | 58.3 | 58.3 | 53.1 | 75.9 |
| severe | 45.2 | 45.3 | 50.7 | 61.0 |

Os cenários são sintéticos e usam premissas de dados públicos ainda não validadas com operadoras reais, então esses números mostram a comparação entre controladores, não o desempenho esperado em campo.

Todas as premissas de carga vêm de dados públicos e ainda precisam de validação com operadoras reais.
