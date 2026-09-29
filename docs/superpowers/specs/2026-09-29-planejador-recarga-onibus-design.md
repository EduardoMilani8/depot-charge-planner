# Planejador de recarga para garagens de ônibus elétricos — Design (sub-projeto 1)

Data: 2026-09-29
Status: aprovada pelo autor em 2026-09-29

## 1. Contexto e objetivo

Garagens de ônibus elétricos têm potência de rede limitada e, em muitos casos, menos carregadores que ônibus. Em São Paulo, a ligação física à rede é o gargalo atual (ônibus zero-km parados aguardando conexão; garagens com 12 carregadores para cerca de 40 ônibus por dia). O objetivo do produto é **decidir, continuamente, quanta potência cada carregador entrega a cada ônibus**, de modo que o máximo de ônibus saia com a carga necessária para sua rota, respeitando o limite da garagem e **continuando a funcionar quando a infraestrutura falha** (ideia do "no-break": degradação graciosa, nunca parada).

Intenção do autor: **startup** (não venda pontual, não apenas portfólio). Isso exige rigor de engenharia desde o início: testes automatizados, decisões explicáveis, reprodutibilidade de falhas e adoção gradual pelo cliente.

Situação de partida: o autor não tem acesso a operadoras, garagens ou fornecedores de carregadores. As premissas iniciais vêm de fontes públicas e estão marcadas como **premissas a validar** (seção 11).

## 2. Decisões já tomadas

- Linguagem: **Go**.
- Controle **direto** dos carregadores, via **OCPP** (1.6 primeiro, 2.0.1 depois). O OCPP fica **fora deste sub-projeto**, mas a arquitetura o prevê.
- O serviço roda **localmente na garagem** (edge), sem depender de nuvem. Uma camada em nuvem multi-garagem é opcional e futura.
- Abordagem **A**: regra por folga em camadas (perfil seguro + *Least Laxity First* com reotimização contínua), com otimizador de custo como camada 2 em ciclo futuro.
- O **simulador é escrito em Go e compartilha o pacote do planejador** com o serviço real.

## 3. Escopo deste sub-projeto

Dentro: modelo de domínio, planejador (camadas 0 e 1), verificador de invariantes, simulador com injeção de falhas, métricas, CLI e testes/CI.

Fora (ciclos futuros, nesta ordem): gateway OCPP → painel do operador → camada 2 (otimizador de custo). Também fora: nuvem e multi-garagem, tarifa detalhada, bateria estacionária, solar, V2G, automação da movimentação física dos ônibus.

## 4. Arquitetura

Módulo Go, biblioteca padrão apenas no núcleo (sem dependências externas):

- `internal/model` — tipos do domínio, sem lógica.
- `internal/planner` — camadas 0 e 1 e o verificador de invariantes. **Função pura**: `Plan(estado, restrições, agora) → plano`, sem I/O.
- `internal/sim` — simulador de eventos discretos, gerador de cenários, injeção de falhas, métricas.
- `cmd/simrun` — CLI que roda cenários e emite resultados (JSON/CSV).

Interface prevista para o futuro: `ChargerGateway` (ler estado dos carregadores, definir potência). O simulador a implementa neste ciclo; um adaptador OCPP a implementará depois.

## 5. Modelo de dados

- **Site**: limite de potência da garagem em kW, **variável no tempo** (permite *derating* e horário de ponta); passo de planejamento (padrão 1 minuto).
- **Charger**: potência máxima e **mínima** (carregadores reais têm piso), status (`ok`, `falha`, `offline`), rendimento.
- **Bus**: capacidade, energia atual estimada com **idade e confiança da leitura**, energia necessária na saída, horários de chegada e saída, limite de potência da bateria (redução perto de 100%).
- **Plan**: setpoint de potência por carregador para o próximo intervalo; previsão até a saída; por ônibus: atinge o alvo (sim/não), déficit em kWh, **motivo legível** da decisão; lista de **recomendações de rodízio**.

## 6. Planejador

Executa a cada passo e a cada evento (chegada, saída, falha de carregador, mudança do limite, leitura de SoC fora do esperado).

### Camada 1 — regra por folga (caminho normal)

1. **Validar entradas.** Leituras fora de faixa, velhas ou de carregador em falha são marcadas como não confiáveis e tratadas como tal.
2. **Folga do ônibus** = (tempo até a saída) − (energia que falta ÷ potência efetiva máxima), onde a energia considera o rendimento do carregador e a potência efetiva é o menor entre o limite do carregador e o da bateria.
3. **Passo "na hora certa":** cada ônibus recebe a potência mínima que cumpre o prazo (energia que falta ÷ tempo disponível), limitada pelo piso e teto do carregador. Isso achata o pico. Abaixo do piso, o carregador liga no piso ou fica desligado (não há valor intermediário).
4. **Sobra de potência:** o restante do limite da garagem vai primeiro a quem tem menor folga, formando margem de segurança.
5. **Caso inviável** (soma dos mínimos acima do limite): priorizar ônibus que ainda podem ser concluídos; marcar os demais com déficit e motivo. O risco é reportado o mais cedo possível.
6. **Rodízio:** quando há ônibus esperando com folga pequena e ônibus conectados com folga grande, recomendar a troca (executada por humano).
7. **Margem de segurança:** o alvo efetivo é a energia da rota mais uma margem configurável, para cobrir consumo real acima do previsto.

### Camada 0 — perfil seguro (último recurso)

Divisão fixa do limite da garagem entre os ônibus conectados, respeitando teto e piso dos carregadores. Usado se o planejador falhar, estourar o tempo ou os dados estiverem velhos demais.

Ordem de degradação: plano normal → último plano válido (por tempo curto e configurável) → perfil seguro.

### Verificador de invariantes

Componente separado e pequeno que roda após qualquer plano. Garante: potência total nunca acima do limite vigente da garagem; nenhum carregador acima do seu teto; nenhum carregador entre zero e o piso. Se alguma condição falhar, o plano é corrigido (redução proporcional ou desligamento) e o evento é registrado. Quem calcula não é quem garante o limite.

## 7. Simulador

- Tempo discreto, passo de 1 minuto, **determinístico por semente**.
- **Verdade separada da observação:** o simulador mantém o estado real; o planejador recebe leituras com ruído, atraso, congelamento ou ausência.
- Física: potência constante até cerca de 80% e depois decrescente; perdas do carregador; atraso entre comando e potência real.
- **Gerador de cenários** parametrizável: número de ônibus e carregadores (incluindo menos carregadores que ônibus), limite da garagem, janelas de chegada e saída, SoC de chegada, energia por rota. Valores padrão de fontes públicas, marcados como premissas.
- **Falhas injetáveis:** carregador falha (permanente/intermitente) ou perde comunicação; limite da rede cai; SoC com ruído, viés, congelado ou ausente; consumo real acima do previsto; chegada tardia ou saída antecipada; erro ou estouro de tempo do próprio planejador. Perfis prontos (nenhuma, leve, severa) e modo aleatório (*fuzz*).
- **Baselines** para comparação: carregar ao chegar (FIFO) e menor horário de saída primeiro.

### Métricas

1. % de ônibus que saem com a carga necessária (principal).
2. Déficit total em kWh dos que não saem prontos.
3. Pico de kW e violações do limite (esperado: zero).
4. Custo com tarifa simples (fora de ponta × ponta).
5. Estabilidade (número de mudanças de plano).
6. Tempo de cálculo do planejador (p99).

## 8. Testes e CI

1. Unitários no planejador e no verificador.
2. Propriedades e fuzz nativo do Go: qualquer entrada produz plano que respeita as invariantes.
3. Cenários no simulador com sementes fixas, comparando com baselines.
4. Testes de referência (*golden*): estados conhecidos com plano e motivo esperados.
5. `go test -race` sempre; benchmark com meta inicial de 200 ônibus em menos de 50 ms por ciclo (meta a validar na medição).
6. Tudo executado em CI a cada mudança.

## 9. Observabilidade

- Logs estruturados com `log/slog`.
- **Registro de decisões**: cada ciclo grava estado de entrada, plano e motivos. O registro pode ser **reexecutado no simulador**, transformando problemas de campo em cenários reproduzíveis.

## 10. Caminho até o mundo real

- Adaptador OCPP 1.6 (biblioteca `ocpp-go`) implementará `ChargerGateway` em ciclo futuro, com **malha fechada**: confere a potência real pelas leituras do carregador, pois alguns carregadores atrasam ou ignoram perfis.
- Adoção gradual para reduzir o risco do cliente: **modo sombra** (só observa e mostra o que faria) → **modo consultivo** (recomenda, operador decide) → **modo controle**.

## 11. Premissas e riscos

- Premissas de consumo, tempos e curvas vêm de dados públicos (por exemplo, 1,4 kWh/km e simultaneidade de 0,65 citados pela Revista AutoBus; estudo de 100 ônibus da Ampcontrol) e **precisam de validação real**. Parte dessas fontes é material de fornecedor ou modelagem de um único autor.
- A curva real de carga (redução perto de 100%) pode alterar a folga calculada; o simulador deve medir esse efeito.
- Compatibilidade OCPP dos carregadores usados no Brasil é desconhecida.
- Não se sabe quem paga a energia nos contratos de operadoras paulistanas, o que afeta quem tem interesse em pagar pela economia.
- Roteiro de validação com primeiros contatos: quantidade e modelo de carregadores e suporte a OCPP; potência contratada e histórico de multa por demanda; rotina atual de recarga; casos de saída sem carga e seu custo; origem da leitura de SoC; quem paga a energia.

## 12. Critérios de sucesso

- **Zero violações** do limite da garagem em todas as sementes e perfis de falha.
- Em todos os perfis, o % de ônibus prontos da camada 1 é **maior ou igual** ao dos baselines, todos avaliados sob o mesmo limite de garagem (os baselines atendem por ordem de chegada ou de saída até esgotar o limite).
- A camada 0 sempre produz um plano válido (respeitando as invariantes) para qualquer entrada, mesmo que não seja o melhor plano; seu desempenho é medido e reportado, sem exigência de superar os baselines.
- Cada decisão do plano tem motivo legível.
- Qualquer falha encontrada é reproduzível a partir da semente ou do registro de decisões.
- O tempo de cálculo atinge a meta da seção 8 ou a meta é revista com dados medidos.
