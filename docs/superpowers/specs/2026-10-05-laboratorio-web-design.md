# Laboratório web de simulação — Design (sub-projeto 2)

Data: 2026-10-05
Status: aprovado e implementado (plano em docs/superpowers/plans/2026-10-05-laboratorio-web.md)

## 1. Contexto e objetivo

O planejador e o simulador (sub-projeto 1) hoje só são usados pelo terminal (`cmd/simrun`), que imprime uma tabela de médias. Isso não permite responder, sem ler JSON, "por que esse ônibus não saiu pronto?" nem "o que o planejador fez nesse minuto?".

Objetivo: um **laboratório de simulação** local, com interface web, para entender e explorar o comportamento do planejador. Quem usa: o autor primeiro, depois outros engenheiros. Não é produto para o cliente final, nem demonstração polida, nem painel de operação (esses ficam para ciclos futuros).

Critério de sucesso: o usuário configura um cenário, compara o planejador com os baselines, abre uma execução e responde às duas perguntas acima só pela tela.

## 2. Decisões já tomadas

- Um **único programa Go** com a interface embutida (`go:embed`). Rodar: `go run ./cmd/lab`, abrir `127.0.0.1:8080`. Nada de Node para usar a ferramenta.
- Núcleo continua só com a biblioteca padrão do Go. O frontend é HTML, CSS e módulos JavaScript simples, **sem dependências**, com gráficos SVG escritos à mão.
- A interface em português, com tema claro e escuro conforme o sistema.
- Nada é gravado em disco: o simulador é determinístico por semente, então uma execução detalhada é recalculada a partir de parâmetros + semente + controlador.

## 3. Fora do escopo

Login, banco de dados, multiusuário, painel de operação real, gateway OCPP, comparação lado a lado de dois controladores na mesma tela, exportar PDF, edição manual de falhas uma a uma, hospedagem na nuvem.

## 4. Arquitetura

1. **`internal/sim` (extensão pequena).**
   - `Trace`: captura, minuto a minuto, o estado **real** (carga de cada ônibus, potência física e comandada de cada carregador, ônibus ligado, limite vigente, falhas ativas). Hoje essa verdade fica dentro do `World` e não sai.
   - `RunTraced(sc, ctrl, rec, trace)`: igual a `Run` mais o traço. `Run` passa a chamar `RunTraced` sem traço; resultados e números do README **não mudam**.
   - Função de comparação reutilizável (monta fifo, edf, fifo-unplug, safe e planner para um conjunto de parâmetros e sementes), movida do `cmd/simrun` e usada por ele e pelo laboratório.
   - Função de validação de parâmetros compartilhada (hoje dentro do `simrun`), com os mesmos limites.
2. **`internal/lab`:** servidor HTTP. Valida, roda, converte para JSON, serve os arquivos embutidos. Sem lógica de planejamento.
3. **`web/`:** HTML, CSS e módulos JS embutidos. Só desenha e coleta parâmetros.
4. **`cmd/lab`:** ponto de entrada; escuta somente em `127.0.0.1`.

O planejador não é alterado.

## 5. Telas

Página única, três abas sincronizadas.

**Cenário (formulário):** ônibus, carregadores, limite (kW), perfil de falha (nenhuma, leve, severa, aleatória), número de sementes, operadores seguem rodízios, idade da leitura. Parâmetros do anti-thrash em bloco "avançado" recolhido. Cenários prontos ("padrão", "rede apertada (600 kW)", "falhas severas"). Botão Rodar com progresso e proteção contra clique repetido.

**Comparação:** tabela com os cinco controladores e as colunas do `simrun`, cada uma com explicação curta ao passar o mouse (destaque para "violações do plano", que deve ser 0, e "movimentações por noite"). Barra de `ready%` por controlador e um ponto por semente para mostrar a variação. Clicar num ponto abre aquela semente e controlador na aba Execução.

**Execução:** quatro painéis sincronizados por um cursor de tempo (arrastável e controlável pelo teclado):
- Potência: área empilhada por carregador contra a linha do limite (que muda em queda de rede), faixas de falhas ativas, fita com a camada que decidiu (normal, último plano válido, perfil seguro).
- Linha do tempo dos ônibus: uma linha por ônibus, da chegada à saída, preenchida pela carga real; marcador de saiu pronto ou não; marcador de rodízios.
- Linha do tempo dos carregadores: ônibus ligado, potência e falhas.
- Decisão neste minuto: camada, potência por carregador, motivo legível por ônibus, rodízios recomendados. Clicar num ônibus mostra carga real contra a lida pelo planejador, alvo e déficit.

Seletor para trocar o controlador no mesmo cenário e semente. Os parâmetros ficam no endereço (`#...`) para gerar um link que reproduz a execução.

Erros e estados: carregando, vazio, falha de rede, erro do servidor com mensagem em português; nova execução cancela a anterior; campo inválido marcado com o texto do servidor. Informação nunca só pela cor; cores seguras para daltonismo.

## 5b. Animações (acrescentado em 2026-10-05 a pedido do autor)

Movimento que ajuda a entender, não enfeite. Tudo em CSS e JavaScript simples, sem dependências; respeita `prefers-reduced-motion` e um interruptor "Animações" (preferência guardada no navegador); desligadas, nenhuma informação se perde.

- **Reproduzir o dia** na aba Execução: botão reproduzir/pausar (e tecla Espaço), velocidade de 30 min/s a 10 h/s, repetir e "do começo". Durante a reprodução o gráfico de potência mostra o passado nítido e o futuro esmaecido.
- **Vida nas linhas do tempo:** ônibus que estão carregando pulsam; nos carregadores ocupados corre um fluxo tracejado; carregador em falha pisca; as marcas de saída (● / ✗) e de rodízio estouram quando o cursor passa.
- **Feed de acontecimentos** em tempo real: saídas (prontos ou não), rodízios recomendados, mudanças de camada do planejador (último plano válido, perfil seguro) e quedas do limite ou falhas de carregador.
- **Transições:** painéis sobem ao trocar de aba; na Comparação as linhas entram em sequência, o percentual de prontos conta até o valor, a barra da média cresce e os pontos das sementes aparecem em cascata; o resumo da execução também conta.
- **Desempenho:** a reprodução move o cursor uma vez por quadro; o painel de decisão é atualizado no máximo 10 vezes por segundo.

## 6. API

Rotas JSON; requisições POST com corpo JSON.

**`GET /api/defaults`:** padrões, limites máximos aceitos e cenários prontos.

**`POST /api/compare`** (parâmetros): roda as N sementes para os cinco controladores. As sementes são independentes e rodam em paralelo; resultado ordenado por semente, idêntico ao do `simrun`. Resposta: por controlador, o agregado (mesmas métricas do `simrun`) e uma linha por semente (`ready%`, déficit, custo, movimentações). O p99 do planejador é medido em tempo real em paralelo, portanto é aproximado, e a tela avisa.

**`POST /api/run`** (parâmetros, semente, controlador):
- `scenario`: horizonte, limite ao longo do tempo, carregadores, ônibus (chegada, saída, capacidade, alvo previsto e real), falhas.
- `series` em colunas: por minuto, potência comandada e física total e camada; por carregador, potência e ônibus ligado; por ônibus, carga real, carga lida (`null` quando não há leitura) e carregador.
- `decisions`: só os minutos em que a decisão mudou, com camada, rodízios e o estado dos ônibus **em delta**: cada decisão lista apenas os ônibus cuja entrada (`b`, `ok`, `as`, `sf`, `r`) mudou desde a decisão anterior (a primeira lista todos) e `gone` traz os ids que estavam listados e deixaram de estar (sempre uma lista, possivelmente vazia). O leitor mantém um mapa, aplica `buses` e remove `gone`; o resultado após a decisão *k* é exatamente a lista de estados que o planejador produziu nela (sem perda). `sf` é omitido quando vale 0. Não há potência por carregador nas decisões: a potência comandada vem de `series.chargers[].commanded_kw`. Os motivos vão numa tabela de textos (`reasons`, cada ônibus aponta por `r`, -1 = sem motivo) e os avisos do planejador (por exemplo "rodízio não recomendado: ônibus B006 cedeu o carregador há 3 min…") vão numa tabela `notes` do topo da resposta, cada texto exato uma vez, com os números; `decisions[].notes` é a lista de índices nessa tabela. O cursor usa a última decisão até o minuto (acumulando os deltas).
- `metrics.plan_p99_micros` é zerado em `/api/run` de propósito (é tempo real medido): a mesma requisição devolve sempre os mesmos bytes.
- Tamanho típico (50 ônibus, 25 carregadores, um dia): abaixo de 2 MB transferidos (gzip) e abaixo de 3 MB sem compressão (cerca de 1 MB sem falhas, até uns 2,3 MB com falhas). As rotas JSON comprimem a resposta com gzip quando o pedido traz `Accept-Encoding: gzip` (`Content-Encoding: gzip`, `Vary: Accept-Encoding`); sem o cabeçalho os bytes são exatamente o JSON sem compressão. Os arquivos estáticos não são comprimidos. Para limitar a resposta, `/api/run` aceita no máximo 500 ônibus e 500 carregadores.

**Validação:** mesmos limites do `simrun` (ônibus e carregadores até 10 mil, sementes até 1000, limite finito em (0, 1e7] kW), via função compartilhada. Inválido devolve 400 com `{"error": "...", "field": "..."}`. No máximo duas execuções simultâneas; a terceira recebe 503 "ocupado". Prazo de 60 s por requisição, tanto em `/api/compare` quanto em `/api/run`: a simulação verifica o cancelamento a cada minuto simulado, então o prazo interrompe execuções em andamento e libera a vaga (504 com mensagem em português). Limites do `/api/run`: ônibus e carregadores até 500 (`lab_max_run_buses` e `lab_max_run_chargers` em `GET /api/defaults`). O JSON de erro de perfil lista os valores da API (`none`, `mild`, `severe`, `random`).

**Segurança local:** só `127.0.0.1`; rejeita `Host` não local (DNS rebinding) e `Origin` cujo host não seja o próprio `Host` (sem `Origin` é aceito); corpo até 64 KB, incluindo o que vem depois do objeto JSON; sem CORS; os arquivos estáticos não têm listagem de diretório (caminho de diretório responde 404).

## 7. Testes

**Go:**
- `RunTraced` devolve as mesmas métricas que `Run`.
- A comparação do laboratório é idêntica à do `simrun` para os mesmos parâmetros.
- Mesma requisição duas vezes devolve JSON idêntico, inclusive com sementes em paralelo.
- Rotas com `httptest`: parâmetros inválidos, NaN/Inf, limites estourados, corpo grande, `Host` não local, rejeição acima de duas execuções, tamanho da resposta de `/api/run` nos quatro cenários prontos (abaixo de 3 MiB puro e de 1 MiB em gzip, e o gzip decodificado igual ao corpo puro), execução cancelada por prazo (504) em `/api/compare` e `/api/run`, e codificação em delta das decisões sem perda (reconstrução igual à lista do planejador).
- Todo arquivo referenciado pelo `index.html` existe no conteúdo embutido.

**Frontend:** cálculos puros (escalas, posição das barras, formatação, escolha da decisão por minuto) em módulos separados, testados com `node --test` (somente desenvolvimento e CI, não para usar a ferramenta). O desenho é verificado em navegador real com capturas de tela a cada marco.

## 8. Ordem de construção

Cada marco é usável, vai para o `main` e é enviado ao GitHub.
1. Núcleo: `Trace`, `RunTraced`, comparação e validação compartilhadas, testes de igualdade com o `simrun`.
2. Servidor: `internal/lab`, três rotas, segurança, testes, `cmd/lab`.
3. Formulário e aba Comparação.
4. Aba Execução, parte 1: gráfico de potência, limite, falhas, camadas e cursor.
5. Aba Execução, parte 2: linhas do tempo de ônibus e carregadores, painel de decisão e "por que não saiu pronto".
6. Acabamento: cenários prontos, link copiável, textos de ajuda, README.

## 9. Riscos

- **Tamanho e velocidade do traço:** medidos no marco 1; teste de tamanho (3 MiB puro, 1 MiB em gzip).
- **p99 com paralelismo:** a tela avisa que é aproximado.
- **JavaScript sem framework que cresce demais:** módulos pequenos, uma responsabilidade cada.
- **Muitos elementos SVG:** estimativa de cerca de 36 mil pontos no gráfico de potência; se pesar, esse painel passa para canvas.
- **Dados sintéticos:** o laboratório mostra o comportamento do algoritmo em cenários gerados, não de uma garagem real; a tela deve dizer isso.

## 10. Critérios de sucesso

- `go run ./cmd/lab` abre a ferramenta sem Node e sem rede externa.
- A tabela do laboratório coincide com a do `simrun` para os mesmos parâmetros.
- Qualquer ônibus que não saiu pronto tem a causa visível na tela (carga real contra lida, motivo, rodízios).
- Qualquer execução pode ser reproduzida a partir do link.
- Nenhuma violação do limite aparece em nenhuma execução (a tela mostra o contador, que deve ser 0).
