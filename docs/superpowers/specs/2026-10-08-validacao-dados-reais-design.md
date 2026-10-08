# Validação com dados reais — especificação

Status: implementado (2026-10-08): `internal/realdata`, `cmd/replay` e a aba "Dados reais" do laboratório; plano de implementação em `docs/superpowers/plans/2026-10-08-validacao-dados-reais.md`. Falta validar com dados reais de uma operadora. Passo 1 do roadmap (`docs/superpowers/plans/2026-10-08-proximos-passos.md`).

## 1. Objetivo e para quem

Responder, com dados de uma garagem de verdade: **"nas mesmas condições em que a operação real trabalhou, o planejador teria deixado mais ônibus prontos, com menos energia perdida e menos pico?"**

Usuário: Eduardo primeiro; depois quem for conversar com uma operadora. Sucesso:

1. Existe um kit que Eduardo pode enviar a uma operadora pedindo os dados, em português, com modelos de planilha e cuidados com LGPD.
2. Uma pasta de planilhas nesse formato vira, sem edição manual, uma tabela "real × planejador × referências" por noite e agregada.
3. Qualquer coisa estimada ou suposta aparece rotulada na tela e no relatório; nada é apresentado como medido quando não foi.
4. Erros de planilha dizem arquivo, linha, coluna e o que esperar, em português.
5. O planejador e o simulador não mudam; os números do README não mudam.

## 2. Fora do escopo

Leitura ao vivo de carregadores (Plano 2), tela de pátio (Plano 5), otimização de custo (Plano 3), limite de potência que varia ao longo da noite (v1 usa um valor único por garagem), importação automática de formatos de fabricantes, persistência em disco no laboratório, link do laboratório que reproduz dados importados (os dados importados não vão para o endereço).

## 3. Formato de dados (decisões)

Pasta com planilhas CSV, codificação UTF-8 (aceitar também Windows-1252 com aviso), separador `,` ou `;` detectado pelo cabeçalho; número com ponto decimal, ou vírgula quando o separador é `;`. Datas e horas no horário local, sem fuso: `AAAA-MM-DD HH:MM` (aceitar `DD/MM/AAAA HH:MM`). Cabeçalhos em português, sem acento, minúsculos; colunas desconhecidas são ignoradas com aviso.

Uma **noite** é identificada pela data de `chegada` menos 12 horas (chegadas de 12:00 a 23:59 pertencem à data do dia; de 00:00 a 11:59, ao dia anterior). Cada noite vira um cenário; várias noites são agregadas como sementes.

| Arquivo | Obrigatório | Uma linha por | Colunas (obrigatórias em negrito) |
|---|---|---|---|
| `garagem.csv` | sim | garagem (1 linha) | **`limite_kw`** (potência contratada ou disponível), `ponta_inicio`, `ponta_fim` (HH:MM), `preco_ponta`, `preco_fora_ponta` (R$/kWh) |
| `carregadores.csv` | sim | carregador | **`carregador_id`**, **`potencia_max_kw`**, `potencia_min_kw`, `eficiencia` |
| `onibus.csv` | sim | ônibus por noite | **`onibus_id`**, **`capacidade_kwh`**, **`chegada`**, **`soc_chegada_pct`**, **`saida_prevista`**, **`soc_saida_exigido_pct`**, `potencia_max_bateria_kw`, `soc_saida_real_pct`, `saida_real` |
| `sessoes.csv` | recomendado | sessão de carga | **`onibus_id`**, **`carregador_id`**, **`inicio`**, **`fim`**, **`energia_kwh`** (medida no carregador, lado da rede) |
| `potencia.csv` | opcional | leitura de potência | **`instante`**, **`carregador_id`**, **`potencia_kw`** (de 1 a 15 min) |

Padrões quando a coluna opcional falta, sempre com aviso listado no relatório: `potencia_min_kw` 5; `eficiencia` 0,94; `potencia_max_bateria_kw` igual à potência máxima do carregador mais comum; sem `ponta_*`/`preco_*` o custo não é calculado (a coluna fica "—"); sem `soc_saida_real_pct` o desfecho real "pronto" não é calculado e o relatório compara só planejador × referências.

O ônibus em `sessoes.csv` casa com `onibus_id` e a noite pelo horário da sessão. Sessões de ônibus ou carregadores desconhecidos geram erro.

## 4. Do dado ao cenário

Por noite, `internal/realdata` monta um `sim.Scenario`:

- `StartClockMin` = hora cheia anterior à menor chegada menos 60 min; `Horizon` = maior saída prevista (ou real) mais 30 min, em minutos desde o início. Limite de horizonte igual ao do simulador.
- `BaseLimitKW` = `limite_kw`; `Chargers` = lista de carregadores, todos `ChargerOK`.
- Ônibus: `SoCKWh` = `soc_chegada_pct`/100 × capacidade; `TargetKWh` = `soc_saida_exigido_pct`/100 × capacidade; `ArrivalMin`/`DepartureMin` = minutos desde o início; `TrueTargetKWh` = o alvo (a necessidade real é a exigida pela operação).
- Sem falhas injetadas: o que aconteceu de errado na realidade já está nos dados. Leituras de SoC perfeitas por padrão (**premissa**, rotulada); opção `-soc-noise kWh` para testar sensibilidade.
- `FollowSwaps` verdadeiro e falso: o relatório sempre mostra o planejador **com** e **sem** rodízios (a premissa dos operadores continua sendo a mais frágil).
- `Tariff` a partir de `garagem.csv` quando houver (janela de ponta e dois preços).

Todos os controladores existentes (`fifo`, `edf`, `fifo-unplug`, `safe`, `planner`, `planner sem rodízio`) rodam sobre o mesmo cenário.

## 5. Desfecho real (coluna "real")

Calculado só dos dados, com a mesma definição do simulador onde existe equivalente:

- **pronto real:** `soc_saida_real_pct` na saída ≥ o exigido (mesmo critério de `Metrics.Ready` do simulador; usar a mesma função para não divergir).
- **déficit real (kWh):** soma de (exigido − real) dos ônibus não prontos.
- **energia da rede:** soma de `energia_kwh` das sessões da noite.
- **pico (kW):** de `potencia.csv` (soma por instante) se existir; senão **estimado** distribuindo a energia de cada sessão uniformemente do início ao fim (piso do pico real; rotulado "estimado").
- **custo (R$):** energia por faixa de preço, usando `potencia.csv` ou a mesma distribuição uniforme (rotulado "estimado" quando uniforme).
- Sem `sessoes.csv`: energia, pico e custo reais ficam "—".

Cuidado de interpretação, exibido na tela: a operação real fez escolhas (ordem de ligação, rodízios manuais) que o planejador não vê e vice-versa; o simulador reproduz condições, não o passado minuto a minuto. Por isso a comparação principal é "pronto" e "déficit", que dependem de chegada, saída e necessidade, e não do caminho.

## 6. Arquitetura

1. **`internal/realdata` (novo, só biblioteca padrão):** `Load(dir) (Dataset, []Warning, error)` valida e lê; `Dataset.Nights()` devolve, por noite, `sim.Scenario` e `RealOutcome`. Erros: `type FieldError{File, Line int, Column, Message}`; mensagens em português. Limites: arquivos até 8 MB, até 1000 ônibus por noite, até 366 noites, até 10 000 carregadores (os mesmos limites do simulador onde já existirem). Sem leitura fora da pasta pedida; nomes de arquivo fixos (sem travessia de caminho).
2. **`cmd/replay` (novo):** `replay -dir PASTA [-soc-noise KWH] [-json]` imprime a tabela por noite e agregada, com a coluna `real`, e a lista de avisos. Mesmo formato de tabela do `simrun` mais a coluna.
3. **`internal/lab` + `web/` (extensão):** aba "Dados reais". O navegador lê os arquivos escolhidos pelo usuário (sem enviar para a nuvem; tudo continua em `127.0.0.1`), envia o conteúdo ao `POST /api/import` (limite de corpo maior só nessa rota, 16 MB, ainda ≤64 KB nas demais), recebe o conjunto validado em JSON canônico e o devolve em `POST /api/replay` para comparar e em `POST /api/run` (campo `dataset`) para abrir a execução detalhada com as mesmas telas de hoje. O servidor não guarda nada entre requisições. Erros de planilha aparecem em lista (arquivo, linha, coluna, texto).
4. **`docs/dados-reais/` (novo):** `README.md` (o pedido à operadora, em português, versão mínima e ideal, LGPD e anonimização) e `modelos/*.csv` fictícios com cabeçalho e exemplo.

O planejador e `internal/sim` ficam intactos, exceto, se necessário, uma função exportada para o critério de "pronto" (mover, não mudar).

## 7. Tela (aba "Dados reais")

Escolher arquivos (ou arrastar a pasta) → lista de avisos (o que foi suposto) → botão Comparar → tabela com linhas `real`, `planner`, `planner (sem rodízio)`, `fifo-unplug`, `fifo`, `edf`, `safe` e as colunas do laboratório, uma seleção por noite ou "todas (média)"; por ônibus: pronto na realidade × no planejador. Células estimadas com selo "estimado" e explicação. Faixa fixa: "Dados reais: o resultado vale para esta garagem e estes dias. As premissas listadas em Avisos continuam sendo suposição." Clicar numa noite abre a execução detalhada do planejador.

## 8. Testes

- `realdata`: um conjunto de planilhas fictício e pequeno em `testdata/` (2 noites, 6 ônibus, 3 carregadores); testes por arquivo para separador `,`/`;`, decimal com vírgula, datas nos dois formatos, noite virando o dia, colunas ausentes opcionais, sessões órfãs, número fora de faixa, linha em branco, BOM UTF-8; cada erro com arquivo, linha e coluna.
- Desfecho real contra valores calculados à mão no conjunto fictício (inclui pico estimado × medido).
- Propriedade: o cenário montado passa `sim.ValidateParams`/validações equivalentes e roda em todos os controladores sem violação de plano.
- `replay` e `/api/import`/`/api/replay`: determinismo (duas chamadas, mesmo resultado, exceto p99), limites de tamanho, erros em português, sem leitura de arquivos arbitrários.
- Front: formatação de avisos, estados vazio/erro/carregando, rótulo "estimado".

## 9. Decisões que tomei e o que custam se estiverem erradas

1. **CSV, nomes em português, um pacote de arquivos fixo** (em vez de Excel ou JSON): operadoras exportam planilhas; custa um parser. Se a operadora só tiver outro formato, faz-se um conversor, sem mudar o resto.
2. **Noite = chegada − 12 h.** Falha em garagens com chegadas durante o dia; custo: noites mal agrupadas, corrigido com uma coluna `noite` opcional (não incluída na v1).
3. **Limite único e leituras de SoC perfeitas.** Otimista para o planejador? Leituras perfeitas favorecem os controladores que usam SoC; o `-soc-noise` mede a sensibilidade. Limite fixo ignora quedas de rede que a operação real tenha sofrido.
4. **Pico e custo "estimados" quando só há sessões.** Distribuição uniforme subestima picos; por isso o selo e a recomendação de pedir `potencia.csv`.
5. **Dados importados não vão para o link.** Evita expor dados da operadora em endereço; custa não poder compartilhar o link de uma noite real. Quem quiser compartilha a pasta anonimizada.
6. **Nada sai da máquina.** O navegador lê os arquivos localmente e fala só com `127.0.0.1`.

## 10. Respostas do autor (2026-10-08)

1. **Sem operadora em mente.** Eduardo imagina que só São Paulo, EUA e Europa teriam esse dado; aceita pedir por e-mail. A pesquisa inicial (ver `docs/dados-reais/README.md`) não achou conjunto público ônibus a ônibus. Consequência: o kit e o formato continuam como estão, e o código é feito e testado com um conjunto fictício até os dados chegarem.
2. **Tarifa:** a pergunta não ficou clara para o autor, então a decisão é: a v1 trata só preço por horário (ponta e fora da ponta), como o simulador já faz; demanda contratada e ultrapassagem ficam para o Plano 3.
3. **Autorizado a começar** pela parte que não depende de dados reais: leitor de planilhas, `replay` e aba "Dados reais", testados com o conjunto fictício.
