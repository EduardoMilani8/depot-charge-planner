# Custo de energia — plano de implementação

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Calcular a fatura (energia por horário + demanda por blocos de 15 min, modos Azul/Verde, contrato e ultrapassagem) com uma única biblioteca usada pelo simulador, `replay` e laboratório, e deixar o planejador, opcionalmente, deslocar carga para fora da ponta e limitar a demanda sem piorar a prontidão.

**Architecture:** Novo pacote `internal/billing` (só biblioteca padrão) com `Tariff`, `Meter` e `Bill`; `sim.Tariff` vira alias de `billing.Tariff` (literais antigos continuam compilando). `sim.Metrics` ganha um bloco opcional `Bill` (ponteiro, `omitempty`: só aparece quando há preço de demanda ou contrato, para os bytes existentes não mudarem). O planejador ganha `Config.Cost *CostPolicy` (nil = comportamento atual) e a lógica fica em `internal/planner/cost.go`. `simrun`, laboratório e `replay` expõem os parâmetros.

**Tech Stack:** Go 1.22 (biblioteca padrão), JavaScript ES modules sem dependências; testes `go test -race ./...` e `node --test web/test/*.test.mjs`.

**Spec:** `docs/superpowers/specs/2026-10-11-custo-de-energia-design.md` (vinculante).

## Global Constraints

- Biblioteca padrão apenas; interface em português; claro/escuro por variáveis CSS; sem requisições externas.
- **Compatibilidade:** com a política nula e sem preços de demanda/contrato, TODA saída existente é idêntica byte a byte: números do README, testes dourados (`internal/planner/testdata`, registro de decisões), hash do `/api/run` sem arquivos (`internal/lab`), saída do `simrun` (exceto a coluna de p99). Novos campos de JSON são `omitempty` ou ponteiros nulos.
- `plan violations` continua 0 em todos os testes de propriedade; o verificador (`Enforce`) e as camadas de segurança não são enfraquecidos.
- Prontidão tem prioridade sobre custo: `ready%` com a política ligada é ≥ `ready%` com ela desligada em cada semente dos testes de propriedade (não só na média).
- Definições: bloco de demanda = 15 minutos alinhados ao relógio; demanda do bloco = energia da rede no bloco ÷ 0,25 h (minutos sem dado contam 0 kW). Ultrapassagem: constantes `OverrunTolerance = 0.05` e `OverrunFactor = 2`. `NightsPerMonth` padrão 30.
- Cada commit termina com `Co-Authored-By: Claude Sonnet 5.5 <noreply@anthropic.com>` e é enviado ao GitHub (`git push`) logo depois. Sem force-push.

## Review Focus

1. Janela de ponta que atravessa a meia-noite, `PeakFromMin == PeakToMin` (nunca ponta), minutos de relógio acima de 1440 ou negativos.
2. Blocos de 15 min nas bordas da noite (bloco parcial), potência 0 durante a noite inteira, NaN/Inf/negativos (nunca entram na fatura; erro ou ignorar com aviso, nunca NaN no resultado).
3. A política consciente de custo nunca deixa um ônibus sem tempo de terminar quando as leituras são confiáveis; com leitura não confiável ou carregador em falha ela não adia; queda de rede no meio da ponta.
4. Estimativa mensal sempre rotulada como estimativa; economia contra `fifo`/`fifo-unplug` calculada com os mesmos parâmetros.
5. Resultados medidos descritos no README exatamente como saíram (inclusive "não economiza" quando for o caso).

---

## File Structure

- Create `internal/billing/{tariff.go,meter.go}` (+ `tariff_test.go`, `meter_test.go`).
- Modify `internal/sim/{scenario.go,metrics.go,world.go,gen.go,validate.go,controllers.go,compare.go}`; tests `internal/sim/billing_test.go`, `gen_test.go` additions.
- Create `internal/planner/cost.go` (+ `cost_test.go`, `cost_property_test.go`); modify `internal/planner/{types.go,normal.go,planner.go,validate.go}` minimally.
- Modify `cmd/simrun/main.go` (+ test), `internal/lab/{params,handlers,convert}.go` (+ tests), `web/static/js/*` (+ tests), `internal/realdata/{load,nights,replay}.go` (+ tests), `docs/dados-reais/{README.md,modelos/garagem.csv}`, `README.md`, spec status.

---

### Task 1: Biblioteca de fatura (`internal/billing`)

**Files:** Create `internal/billing/tariff.go`, `internal/billing/meter.go`, `internal/billing/tariff_test.go`, `internal/billing/meter_test.go`

**Interfaces — Produces:**

```go
package billing

const (
	BlockMin        = 15
	OverrunTolerance = 0.05
	OverrunFactor    = 2.0
	DefaultNightsPerMonth = 30
)

type Tariff struct {
	PeakFromMin, PeakToMin int     // clock minutes of the day; From > To = window crosses midnight; From == To = never peak
	PeakPrice, OffPeakPrice float64 // R$/kWh
	DemandPeakPrice, DemandOffPeakPrice float64 // R$/kW; Verde uses DemandOffPeakPrice only
	DemandVerde    bool
	ContractedKW   float64 // 0 = no contract (billed demand = measured)
	NightsPerMonth int     // 0 = DefaultNightsPerMonth
}
func (t Tariff) InPeak(clockMin int) bool      // any int (negative, > 1440): reduced modulo 1440
func (t Tariff) PriceAt(clockMin int) float64
func (t Tariff) HasDemand() bool               // any demand price > 0 or ContractedKW > 0
func (t Tariff) Nights() int
func (t Tariff) Validate() error               // finite, >= 0, window 0..1440, NightsPerMonth 0..366

type Bill struct {
	EnergyKWh, EnergyBRL float64
	DemandKW             float64 // largest 15-min block (any window)
	DemandPeakKW, DemandOffPeakKW float64 // largest block starting in / outside the peak window
	BilledPeakKW, BilledOffPeakKW float64 // what the tariff charges (see rules)
	OverrunKW            float64 // sum over the windows charged as overrun (kW above contract)
	DemandBRL, OverrunBRL float64 // OverrunBRL is included in DemandBRL
	MonthlyBRL           float64 // EnergyBRL*Nights + DemandBRL (estimate)
}

// Meter accumulates grid energy per clock minute and prices it.
type Meter struct{ /* unexported */ }
func NewMeter(t Tariff) *Meter
func (m *Meter) AddKW(fromClockMin, toClockMin int, kw float64) // constant kW over [from, to) minutes (absolute clock minutes; may exceed 1440); ignores kw<=0, NaN, Inf, to<=from
func (m *Meter) Bill() Bill
```

Regras de cobrança (implementar e testar exatamente):
- Energia: cada minuto `kw/60` kWh × `PriceAt(minuto)`. `EnergyKWh` é a soma.
- Blocos: `block = floor(clockMin/15)` (minuto absoluto, não reduzido); demanda do bloco = soma de kWh no bloco ÷ 0,25. Um bloco pertence à ponta se o seu **primeiro minuto** `InPeak`.
- Azul (`DemandVerde=false`): `DemandPeakKW` = maior bloco em ponta; `DemandOffPeakKW` = maior fora. Verde: `DemandKW` = maior de todos, cobrado só com `DemandOffPeakPrice`.
- Demanda cobrada por janela (Azul) ou única (Verde): sem contrato → `Billed = medido`; com contrato C: se medido ≤ 1,05·C → `Billed = C`; se medido > 1,05·C → `Billed = C` e `OverrunKW = medido − C`, `OverrunBRL = (medido − C) × OverrunFactor × preço`. `DemandBRL = Σ Billed × preço + OverrunBRL`.
- `MonthlyBRL = EnergyBRL × Nights() + DemandBRL`.

- [ ] **Step 1: Testes que falham** (valores calculados à mão; escreva-os literais):
  - `InPeak/PriceAt` com janela 18:00–21:00 (1080–1260): 1079 não, 1080 sim, 1259 sim, 1260 não; com `1500` (25:00 = 01:00) não, `-60` (23:00) não; janela que cruza a meia-noite 22:00–02:00 (1320–120): 1380 sim, 60 sim, 120 não, 720 não, 1440+60 sim; `From==To` nunca; `PriceAt` devolve o preço certo.
  - Caso 1 (Azul, preços de demanda 50/20, sem contrato): 100 kW constantes de 20:00 a 22:00 (1200–1320) → `EnergyKWh=200`, `EnergyBRL=360` (100×2,70 + 100×0,90), `DemandPeakKW=100`, `DemandOffPeakKW=100`, `DemandBRL=7000`, `MonthlyBRL=360×30+7000=17800`.
  - Caso 2 (Verde, `DemandOffPeakPrice=20`): mesmo perfil → `DemandKW=100`, `DemandBRL=2000`.
  - Caso 3 (bloco parcial): 400 kW por 5 minutos (1200–1205) → o bloco 1200 tem demanda 400×5/60 kWh ÷ 0,25 = 133,333… kW.
  - Caso 4 (contrato 80 kW, Azul, perfil do caso 1): medido 100 > 84 → `Billed=80` em cada janela, `OverrunKW=40` (20+20), `OverrunBRL=20×2×50 + 20×2×20 = 2800`, `DemandBRL=80×50 + 80×20 + 2800 = 8400`. Com contrato 100: `Billed=100`, sem ultrapassagem, `DemandBRL=7000`. Com contrato 120 e medido 100: `Billed=120`, `DemandBRL=120×50+120×20=8400`.
  - Entradas inválidas: `AddKW` com NaN/Inf/negativo/zero/`to<=from` ignoradas (Bill vazio, sem NaN); `Validate` rejeita negativos, NaN, janela fora de 0..1440, NightsPerMonth 367.
  - Compatibilidade: `Tariff{PeakFromMin:1080, PeakToMin:1260, PeakPrice:2.7, OffPeakPrice:0.9}` (demanda zero) → `HasDemand()==false`, `PriceAt` idêntico ao antigo `sim.Tariff.PriceAt` em todos os 1440 minutos (reproduza a função antiga no teste).
- [ ] **Step 2: Falhar:** `go test ./internal/billing/ 2>&1 | head`.
- [ ] **Step 3: Implementar** (`tariff.go`, `meter.go`; blocos em `map[int]float64` kWh por bloco e energia por minuto numa soma corrente para `EnergyBRL`; `AddKW` deve ser O(minutos) sem alocar por minuto).
- [ ] **Step 4:** `gofmt -l .; go vet ./... && go test -race ./internal/billing/`.
- [ ] **Step 5: Commit e push** (`feat(billing): tariff and meter with peak windows, 15-minute demand blocks, contract and overrun`).

---

### Task 2: Simulador usa a biblioteca; métricas de fatura; janela de chegadas

**Files:** Modify `internal/sim/{scenario.go,metrics.go,world.go,gen.go,validate.go,compare.go}`; Create `internal/sim/billing_test.go`

**Interfaces:**
- Consumes: `billing.Tariff`, `billing.Meter`, `billing.Bill`.
- Produces:
  - `type Tariff = billing.Tariff` em `scenario.go` (remove a definição antiga e o método `PriceAt` local; todos os usos existentes continuam compilando).
  - `Metrics.Bill *BillMetrics \`json:"bill,omitempty"\`` com `type BillMetrics struct { DemandKW, DemandPeakKW, DemandOffPeakKW, BilledPeakKW, BilledOffPeakKW, OverrunKW, DemandBRL, OverrunBRL, MonthlyBRL float64 }` (tags snake_case). Preenchido **somente** se `sc.Tariff.HasDemand()`; `CostBRL` continua = energia da noite, calculado como hoje (nenhum número antigo muda). `Aggregate` faz a média dos campos de `Bill` entre execuções com Bill (nil se nenhuma).
  - Em `world.advance`: alimentar um `billing.Meter` com a potência física total do minuto (`total` kW) em `[StartClockMin+t, StartClockMin+t+1)`; no `finish`, `m.Bill` a partir de `meter.Bill()` quando `HasDemand()`.
  - `GenParams` ganha: `ArrivalFromMin, ArrivalToMin int` (minutos desde o início do cenário, 18:00; ambos 0 = padrão 120..360), `Demand DemandParams{PeakPrice, OffPeakPrice, ContractedKW float64; Verde bool}`. `Generate` copia `Demand` para `sc.Tariff`; quando `ArrivalToMin > ArrivalFromMin`, a chegada é `ArrivalFromMin + rng.Intn(ArrivalToMin-ArrivalFromMin+1)` — **uma** chamada a `rng.Intn` por ônibus, como hoje (com os valores padrão 120/360 a sequência é idêntica à atual).
  - `ValidateParams` valida os novos campos (limites: preços ≥ 0 e finitos até 1e6; contrato ≥ 0 até `MaxLimitKW`; janela de chegada 0 ≤ From < To ≤ 1500) com `FieldError` de nomes de flag novos (`arrival-from`, `arrival-to`, `demand-peak-price`, `demand-offpeak-price`, `demand-contracted`).

- [ ] **Step 1: Testes que falham:** (a) compatibilidade: com os parâmetros padrão, `Run` de `Generate(DefaultGenParams(), seed)` para sementes 1..5 e os 5 controladores devolve as mesmas `Metrics` (todos os campos antigos) que antes (grave os valores atuais como literais no teste lendo-os do commit atual: rode uma vez no estado atual e cole), `Bill == nil`; (b) `Generate` com `ArrivalFromMin=0, ArrivalToMin=0` e com `120/360` dá cenários idênticos (`reflect.DeepEqual`) e com `0/180` as chegadas ficam em 0..180; (c) com `Demand{PeakPrice:50, OffPeakPrice:20}`: `Bill != nil`, `Bill.DemandKW` igual ao maior bloco de 15 min calculado de forma independente a partir de `Trace.Physical` (some o trace em blocos alinhados ao relógio no teste), `Bill.MonthlyBRL == CostBRL*30 + DemandBRL`; (d) `Aggregate` média de `Bill`; (e) `ValidateParams` rejeita valores inválidos com o `Field` certo.
- [ ] **Step 2: Falhar. Step 3: Implementar. Step 4:** `gofmt -l .; go vet ./... && go test -race ./internal/sim/ ./internal/billing/ ./cmd/... ./internal/realdata/` (o pacote `internal/lab` é lento: rode `go test ./internal/lab -run 'Golden|Determin|Static'` apenas).
- [ ] **Step 5: Commit e push** (`feat(sim): bill the night with demand blocks, optional arrival window and demand parameters`).

---

### Task 3: Planejador desloca carga para fora da ponta

**Files:** Create `internal/planner/cost.go`, `internal/planner/cost_test.go`; Modify `internal/planner/types.go`, `internal/planner/normal.go` (mínimo: ponto de chamada), `internal/planner/validate.go`, `internal/sim/controllers.go` (preencher a política com o cenário)

**Interfaces:**

```go
// types.go
type CostPolicy struct {
	ClockAtZeroMin int     // minute of the day at Input.Now == 0
	PeakFromMin, PeakToMin int // clock minutes; From > To crosses midnight; equal = never peak
	DeferMarginMin float64 // default 45 when 0: minutes of laxity a deferred bus must keep after the peak
	DemandTargetKW float64 // Task 4; 0 = off
}
// Config.Cost *CostPolicy `json:",omitempty"` — nil = behaviour unchanged.
```

Comportamento (`cost.go`, chamado de `allocate`/`PlanNormal` somente se `cfg.Cost != nil`):
1. `peakEndsIn(now)` = minutos até o fim da janela de ponta se `now` está em ponta, senão 0 (janela que cruza a meia-noite incluída). Fora da ponta (`0`) → nada muda (carregar o mais cedo possível continua o padrão: é barato ou pré-ponta).
2. Para cada candidato com `need > 0`, confiável (`cd.reliable`), carregador `OK`, `!unusable` e `laxityMin >= 0`: pode ser adiado se `(availMin - tp - DeferMarginMin) * maxKW/60 >= gridNeed` (terminaria depois da ponta, na potência máxima, com a folga de segurança). Sem leitura confiável, nunca adia.
3. Viabilidade conjunta pós-ponta: ordene os adiáveis por prazo e aplique o mesmo teste cumulativo de `admit` com orçamento `AvailableKW(in)` e tempo `availMin - tp - DeferMarginMin` (mantém o maior conjunto que cabe; os que sobram deixam de ser adiados e são carregados normalmente agora).
4. Os adiados recebem `allocKW = 0`, ficam fora do preenchimento de sobra e saem do orçamento; os demais seguem o `allocate` normal com o orçamento inteiro (nunca menor que hoje). `BusStatus.Reason` dos adiados: `"adiado para depois da ponta: termina com folga de %.0f min"`; `WillReachTarget` deve refletir que ele ainda alcança o alvo depois da ponta (calcule a entrega prevista incluindo o tempo pós-ponta, para que o veredito não diga falsamente "não alcança").
5. `Validate`/sanitização: `Cost` com janela inválida, `DeferMarginMin` negativo/NaN/Inf → ignorado (tratado como nil) e registrado em `Notes`; nunca pânico.
6. `sim.NewPlannerController(cfg, sc)`: se `cfg.Cost != nil`, copia a política e preenche `ClockAtZeroMin = sc.StartClockMin` e a janela de `sc.Tariff` (a política do chamador só traz `DeferMarginMin` e `DemandTargetKW`); o `ForController` e o `Compare` usam isso sem mudar assinaturas.

- [ ] **Step 1: Testes que falham** em `cost_test.go` (entrada construída à mão, valores esperados à mão): (a) fora da ponta o plano com e sem política é idêntico (`reflect.DeepEqual` de `PlanNormal` para 5 entradas aleatórias com semente fixa); (b) dentro da ponta, ônibus com folga grande (exemplo: 100 kWh de rede a 100 kW máx., 600 min até a saída, ponta termina em 60 min → adiado, setpoint 0 e razão "adiado…"); ônibus apertado (200 min até a saída e 150 kWh, nao pode adiar) recebe potência; (c) leitura não confiável ou `Confidence` baixa → nunca adiado; (d) conjunto adiável que não cabe depois da ponta (dois ônibus de 100 kWh, orçamento 100 kW, 90 min pós-ponta úteis) → o de menor folga ou maior necessidade deixa de ser adiado conforme a regra do passo 3 (determinístico por ID); (e) carregador em falha e ônibus sem carregador não mudam; (f) `Cost` inválido é ignorado com nota; (g) propriedade: para 200 entradas aleatórias (semente fixa) dentro da ponta, a soma dos setpoints com política ≤ sem política e nenhum setpoint excede o limite (verificador passa: `Validate`/`Enforce` sem alterações).
  Propriedade de simulação em `cost_property_test.go` (pacote `sim`, em `internal/sim/cost_property_test.go`): para sementes 1..8 × perfis {none, mild, severe, random} × limites {600, 1200, 2000} com chegadas `0/180` (na ponta) e preços de energia padrão: com `cfg.Cost = &CostPolicy{DeferMarginMin:45}` → `PlanViolations == 0`; `ReadyPct(com) >= ReadyPct(sem)` em cada semente; `CostBRL(com) <= CostBRL(sem)*(1+1e-9)` pelo menos em perfil `none` (nos perfis com falhas aceitar menor economia, mas nunca prontidão menor). Use `testing.Short()` para reduzir a grade como nos testes existentes.
- [ ] **Step 2: Falhar. Step 3: Implementar. Step 4:** `gofmt -l .; go vet ./... && go test -race ./internal/planner/ ./internal/sim/` e rode também os testes dourados/fuzz do planejador (já fazem parte do pacote): devem passar sem mudança.
- [ ] **Step 5: Commit e push** (`feat(planner): optionally defer charging past the peak window when buses have the slack`).

---

### Task 4: Planejador limita a demanda

**Files:** Modify `internal/planner/cost.go`, `internal/planner/normal.go` (ponto de chamada), tests `internal/planner/cost_test.go`, `internal/sim/cost_property_test.go`

**Comportamento:** com `Cost != nil && DemandTargetKW > 0`: `PlanNormal` calcula o plano com orçamento `min(AvailableKW, DemandTargetKW)`; se esse plano tiver algum ônibus **avaliado e viável** (`laxity >= 0`, leitura confiável) com `WillReachTarget == false` que o plano com o orçamento cheio salva, usa o plano de orçamento cheio (prontidão primeiro). O orçamento cheio continua sendo o limite do `Enforce`. Notas: `Notes` registra "demanda limitada ao alvo" quando o plano com alvo é o escolhido e "alvo de demanda não cabe: usando o limite" no outro caso. Convive com o deslocamento da Task 3 (aplique o alvo sobre o orçamento antes do deslocamento).

- [ ] **Step 1: Testes que falham:** (a) `DemandTargetKW` maior que o limite → idêntico a sem alvo; (b) entrada folgada: soma dos setpoints ≤ alvo; (c) entrada apertada (necessidade exige mais que o alvo): usa o orçamento cheio e a soma pode passar do alvo; (d) alvo 0 desligado; (e) propriedade de simulação: sementes 1..8, perfis {none, mild}, limite 2000, `DemandTargetKW` ∈ {500, 800}: `PlanViolations==0`, `ReadyPct(com) >= ReadyPct(sem)` por semente, `Bill.DemandKW(com) <= Bill.DemandKW(sem)+1e-9` (usar demanda com preço > 0 para habilitar `Bill`); (f) as 3 camadas de `planner.New(...)` ainda funcionam (último plano válido/seguro) com a política ligada.
- [ ] **Steps 2–5** como acima (`feat(planner): optionally cap the grid demand at a target while every savable bus still finishes`).

---

### Task 5: `simrun` e API do laboratório

**Files:** Modify `cmd/simrun/main.go` (+ `main_test.go`), `internal/lab/{params,handlers,convert}.go` (+ testes)

**Interfaces:**
- `simrun` flags novas: `-cost-aware` (bool), `-defer-margin` (min, padrão 45), `-demand-target` (kW, 0), `-demand-peak-price`, `-demand-offpeak-price`, `-demand-contracted`, `-demand-mode azul|verde`, `-arrival-from`, `-arrival-to` (hora do dia `HH:MM` convertida em minutos desde 18:00, ou número de minutos; escolha uma forma e documente), `-nights-per-month`. Quando `HasDemand()`: colunas extras `demand kW`, `demand R$/mês`, `monthly R$` e uma linha final "economia mensal estimada contra fifo / fifo-unplug" para `planner`. Sem as flags novas a saída é idêntica à de hoje.
- Lab: `Params` ganha campos opcionais (`cost_aware`, `defer_margin_min`, `demand_target_kw`, `demand_peak_price`, `demand_offpeak_price`, `demand_contracted_kw`, `demand_verde`, `arrival_from_min`, `arrival_to_min`, `nights_per_month`) com os mesmos limites e mensagens em português; `/api/defaults` ganha presets "Chegadas na ponta (custo)" e "Fatura com demanda (ilustrativa)" e os limites; `/api/compare` e `/api/run` devolvem `bill` nas métricas quando presente; `defaultParams()` e o resultado de `/api/compare`/`/api/run` com os parâmetros padrão continuam byte a byte idênticos (o teste de hash existente passa sem alterar o hash).
- [ ] **Step 1: Testes que falham:** saída de `simrun` com e sem as flags (golden do texto sem p99); cada flag inválida → exit 2 com mensagem; API: params novos válidos/ inválidos (`field`), presets presentes, `bill` presente só com demanda, determinismo; economia calculada (texto) confere com as métricas.
- [ ] **Steps 2–5** (`feat(simrun,lab): cost parameters, bill columns and monthly savings`).

---

### Task 6: Interface do laboratório

**Files:** Modify `web/static/index.html`, `web/static/js/{form,params,compare,run,format,glossary}.js`, `web/static/app.css`, testes em `web/test/`

- Campos em "Avançado" (grupo "Custo"): chegadas (de/até em horas), política consciente de custo (caixa), margem, alvo de demanda, preços de demanda, contrato, modo Azul/Verde; os valores entram no hash (`stateToHash/hashToState`) com defaults omitidos; erros do servidor nos campos certos; preset novo aparece na lista.
- Comparação: colunas condicionais "demanda kW", "demanda R$/mês (proxy)", "fatura mensal estimada R$", e abaixo da tabela uma frase "Economia mensal estimada do planner: R$ X contra fifo e R$ Y contra fifo-unplug (estimativa: demanda de uma noite tratada como a do mês)"; glossário com as definições (bloco de 15 min, Azul/Verde, contrato, ultrapassagem, proxy mensal).
- Execução: o resumo mostra energia R$, demanda kW e fatura estimada quando presentes; a camada de potência mostra a janela de ponta como faixa hachurada (informação não só por cor) e os ônibus adiados como "adiado" (a razão já vem do servidor).
- Testes Node para funções puras novas (formatação de moeda pt-BR com 2 casas, cálculo da frase de economia, hash round trip dos campos novos, ordem das colunas). Verificar no navegador (claro/escuro, 375 px, sem erro no console).
- [ ] **Steps:** testes → implementação → `node --test web/test/*.test.mjs` + build + verificação visual → `feat(lab): cost fields, bill columns and monthly savings in the interface`.

---

### Task 7: Dados reais usam a mesma fatura

**Files:** Modify `internal/realdata/{load,nights,replay}.go`, `docs/dados-reais/README.md`, `docs/dados-reais/modelos/garagem.csv`, testes

- `garagem.csv`: colunas opcionais `demanda_preco_ponta`, `demanda_preco_fora_ponta`, `demanda_contratada_kw`, `modo_demanda` (`azul`/`verde`, padrão azul), `noites_por_mes`; validar (≥ 0, limites, mensagens em português com linha/coluna); converter para `billing.Tariff`.
- `nights.go`: substituir as tabelas de preço próprias (`priceTable`/`priceSpan`) pela `billing.Meter` (série de potência → `AddKW` por intervalo; sessões → energia espalhada uniformemente por minuto, rotulada "estimado"), com **os mesmos números de hoje** nos casos existentes (energia 480/65, pico 120 estimado/140 medido, custo 522 estimado/58,5 medido) e demanda real (maior bloco de 15 min) além do pico instantâneo; `Real` ganha `DemandKW`, `DemandBRL`, `MonthlyBRL` (ponteiros, `DemandEstimated`); a regra "janela que atravessa a meia-noite torna o custo simulado não comparável" **sai** (a biblioteca trata a janela): remova a nota e `CostComparable` passa a depender só de haver tarifa; atualize testes e docs.
- `replay.go` e `cmd/replay`: colunas `demand kW`, `monthly R$` quando há preços de demanda; linha "real" com selos; mesma economia mensal estimada.
- [ ] **Steps:** testes (os números atuais continuam; novos casos à mão: demanda real do demo noite 2 = maior bloco de 15 min das leituras 22:00–22:30 → blocos 22:00 (média de 120 e 140 em 15 min ⇒ energia (120+140)/2... calcule à mão no teste a partir do arquivo) → implementação → `go test -race ./internal/realdata/ ./cmd/replay/` → `feat(realdata): bill real nights with the shared billing library`.

---

### Task 8: Medições, documentação e verificação final

**Files:** Modify `README.md`, `docs/dados-reais/README.md`, spec (status), roadmap (Plano 3: "implementado")

- [ ] **Step 1: Medições reais** (rodar e colar números exatos no README, sem arredondar a favor): `simrun` com `-arrival-from 0 -arrival-to 180` (chegadas entre 18:00 e 21:00), 20 sementes, perfis `none` e `severe`, limites 600/1200/2000, com e sem `-cost-aware`, com e sem `-demand-target` (500/800) e preços de demanda ilustrativos (azul 50/20, sem contrato): tabela energia R$/noite, demanda kW, mensal estimado, ready%, plan violations. Descrever honestamente onde não economiza.
- [ ] **Step 2: Verificação:** (1) `simrun` sem as flags novas idêntico ao de antes (comparar com o commit anterior a este trabalho, ignorando p99); (2) hash do `/api/run` sem arquivos inalterado; (3) laboratório: Comparação com o preset novo mostra colunas e economia; (4) `replay` do demo com `garagem.csv` estendido; (5) suíte completa `gofmt -l . ; go vet ./... && go test -race ./... && node --test web/test/*.test.mjs`.
- [ ] **Step 3:** README (seção "Custo de energia": modelo, flags, premissas, estimativa mensal, resultados medidos, limitações), roadmap, status da spec; commit e push (`docs: energy cost billing and cost-aware planning`).
