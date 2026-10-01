# Planejador de recarga para garagens de ônibus elétricos — Plano de Implementação

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Construir em Go o planejador de recarga (camadas 0 e 1 + verificador de invariantes) e o simulador com injeção de falhas que o valida.

**Architecture:** O planejador é uma função pura `Plan(estado) → plano` com degradação em camadas (normal → último plano válido → perfil seguro) e um verificador independente de invariantes. O simulador mantém a verdade separada do que o planejador observa, roda o mesmo pacote do planejador e compara com baselines sob falhas injetadas.

**Tech Stack:** Go 1.22+, somente biblioteca padrão no núcleo, `log/slog`, testes/fuzz/benchmark nativos, GitHub Actions.

**Spec:** `docs/superpowers/specs/2026-09-29-planejador-recarga-onibus-design.md`

## Global Constraints

- Linguagem: **Go**; módulo `github.com/EduardoMilani8/depot-charge-planner`.
- Núcleo (model, planner, sim) usa **somente a biblioteca padrão**, sem dependências externas.
- O planejador é uma **função pura**, sem I/O dentro dele.
- Tempo discreto, **passo de 1 minuto**, simulador **determinístico por semente**.
- Verificador de invariantes: potência total nunca acima do limite vigente da garagem; nenhum carregador acima do seu teto; nenhum carregador entre zero e o piso.
- Todo plano tem **motivo legível** por ônibus (texto em português, voltado ao operador).
- `go test -race` sempre; meta inicial de **200 ônibus em menos de 50 ms por ciclo**.
- Logs estruturados com `log/slog`.
- Código e identificadores em inglês; textos de motivo/notas para o operador em português.

## Refinamentos em relação à spec (decididos ao planejar, sujeitos à sua revisão)

1. **Sobra de potência só vai para ônibus com folga pequena** (`SurplusLaxityMin`, padrão 120 min). Distribuir toda a sobra até o limite tornaria o pico sempre igual ao limite; assim os ônibus confortáveis ficam "na hora certa". Colocar um valor enorme reproduz o comportamento literal da spec.
2. **Carregador `offline` continua consumindo** a última potência comandada (`Charger.LastCommandedKW`); o planejador e o verificador reservam essa potência do orçamento.
3. **Violação do limite é medida sobre a potência comandada** (`PlanViolations`, deve ser zero). A potência física, com atraso de 1 passo entre comando e efeito, é medida à parte (`OvershootMin`): uma queda súbita do limite pode causar sobrecarga física de 1 passo mesmo com plano correto.
4. Pacote `internal/gateway` com a interface `Gateway` já neste ciclo (o simulador a implementa), para o adaptador OCPP futuro.
5. O passo "na hora certa" usa uma curva de carga conservadora: energia acima de 80% da bateria custa o dobro no cálculo do planejador (a física do simulador reduz a potência linearmente até 20% em 100%).

## Review Focus

Entradas que a spec implica mas nenhum requisito descreve, e que uma pessoa usando o software provavelmente encontrará (cada uma tem teste na tarefa indicada):

1. Carregador **offline** que continua puxando potência: o orçamento deve descontá-la (Tarefas 3, 4, 5, 9).
2. Limite da bateria **abaixo do piso** do carregador: nunca comandar valor entre 0 e o piso (Tarefa 5).
3. SoC **NaN, negativo ou acima da capacidade**: tratado como não confiável, sem pânico e sem assumir bateria cheia (Tarefa 5).
4. Ônibus com **saída já passada ou igual ao instante atual**: sem divisão por zero, marcado como inviável (Tarefa 5).
5. **Entrada vazia, limite zero, IDs duplicados ou dois ônibus no mesmo carregador**: nunca pânico, sempre plano válido (Tarefas 2, 5, 7).

## Estrutura de arquivos

```
go.mod  README.md  .gitignore  .github/workflows/ci.yml
cmd/simrun/main.go
internal/model/model.go            tipos e curva de carga
internal/gateway/gateway.go        interface Gateway
internal/planner/types.go          Config, Input, Plan
internal/planner/helpers.go        utilidades e cálculo de orçamento
internal/planner/validate.go       ValidateInput
internal/planner/safe.go           camada 0
internal/planner/enforce.go        Violations e Enforce
internal/planner/normal.go         camada 1
internal/planner/swaps.go          recomendação de rodízio
internal/planner/planner.go        fachada com degradação
internal/sim/scenario.go           Scenario, Fault, Tariff
internal/sim/world.go              estado verdadeiro, física, observação
internal/sim/run.go                laço de simulação
internal/sim/controllers.go        baselines, perfil seguro, controlador do planejador
internal/sim/metrics.go            Metrics, Aggregate
internal/sim/gen.go                gerador de cenários e perfis de falha
internal/sim/decisionlog.go        registro e replay
```

---

### Task 1: Repositório e pacote `model`

**Files:**
- Create: `go.mod`, `.gitignore`, `README.md`, `.github/workflows/ci.yml`
- Create: `internal/model/model.go`
- Test: `internal/model/model_test.go`

**Interfaces:**
- Produces: `model.Minute` (= `int`), `model.ChargerStatus` (`ChargerOK`, `ChargerFaulted`, `ChargerOffline`), `model.Site{LimitKW float64; StepMin int}`, `model.Charger{ID string; MaxKW, MinKW, Efficiency float64; Status ChargerStatus; LastCommandedKW float64}` com `Healthy() bool`, `model.Bus{ID string; CapacityKWh, SoCKWh float64; SoCAgeMin int; SoCConfidence, TargetKWh float64; ArrivalMin, DepartureMin Minute; MaxBatteryKW float64; ChargerID string}`, `model.TaperFactor(soc, capacity float64) float64`, `model.EffectiveEnergy(soc, target, capacity float64) float64`, constantes `TaperStartFrac`, `TaperMinFactor`, `PlannerTailCostFactor`.

- [ ] **Step 1: Criar arquivos de projeto**

`go.mod`:
```
module github.com/EduardoMilani8/depot-charge-planner

go 1.22
```

`.gitignore`:
```
*.test
*.out
/bin/
/decisions.jsonl
```

`README.md`:
```markdown
# depot-charge-planner

Planejador de recarga para garagens de ônibus elétricos: decide quanta potência cada carregador entrega a cada ônibus para que o máximo de ônibus saia com a carga necessária, respeitando o limite da garagem e continuando a funcionar quando a infraestrutura falha.

Status: em desenvolvimento (planejador + simulador). Design: `docs/superpowers/specs/`. Plano: `docs/superpowers/plans/`.

## Uso rápido

    go test -race ./...
    go run ./cmd/simrun -profile severe -seeds 20

Todas as premissas de carga vêm de dados públicos e ainda precisam de validação com operadoras reais.
```

`.github/workflows/ci.yml`:
```yaml
name: ci
on: [push, pull_request]
jobs:
  test:
    runs-on: ubuntu-latest
    steps:
      - uses: actions/checkout@v4
      - uses: actions/setup-go@v5
        with:
          go-version-file: go.mod
      - name: gofmt
        run: test -z "$(gofmt -l .)"
      - run: go vet ./...
      - run: go test -race ./...
      - name: fuzz (short)
        run: go test -run '^$' -fuzz FuzzPlannerRespectsInvariants -fuzztime 20s ./internal/planner
      - name: benchmark (smoke)
        run: go test -run '^$' -bench . -benchtime 200x ./internal/planner
```

- [ ] **Step 2: Escrever o teste que falha**

`internal/model/model_test.go`:
```go
package model

import (
	"math"
	"testing"
)

func near(a, b float64) bool { return math.Abs(a-b) < 1e-9 }

func TestTaperFactor(t *testing.T) {
	cases := []struct{ soc, want float64 }{
		{0, 1}, {50, 1}, {80, 1}, {90, 0.6}, {100, 0}, {120, 0},
	}
	for _, c := range cases {
		if got := TaperFactor(c.soc, 100); !near(got, c.want) {
			t.Errorf("TaperFactor(%v,100) = %v, want %v", c.soc, got, c.want)
		}
	}
	if TaperFactor(10, 0) != 0 {
		t.Error("zero capacity must give zero factor")
	}
}

func TestEffectiveEnergy(t *testing.T) {
	cases := []struct{ soc, target, want float64 }{
		{0, 80, 80},
		{80, 100, 40},
		{50, 100, 70},
		{60, 50, 0},
		{90, 100, 20},
	}
	for _, c := range cases {
		if got := EffectiveEnergy(c.soc, c.target, 100); !near(got, c.want) {
			t.Errorf("EffectiveEnergy(%v,%v,100) = %v, want %v", c.soc, c.target, got, c.want)
		}
	}
}

func TestChargerHealthy(t *testing.T) {
	if !(Charger{Status: ChargerOK}).Healthy() {
		t.Error("OK charger should be healthy")
	}
	if (Charger{Status: ChargerOffline}).Healthy() || (Charger{Status: ChargerFaulted}).Healthy() {
		t.Error("offline/faulted chargers must not be healthy")
	}
}
```

- [ ] **Step 3: Rodar e ver falhar**

Run: `go test ./internal/model -v`
Expected: FAIL (build failed: `undefined: TaperFactor`).

- [ ] **Step 4: Implementar**

`internal/model/model.go`:
```go
// Package model defines the domain types shared by the planner and the simulator.
package model

import "math"

// Minute counts minutes since the start of a scenario.
type Minute = int

type ChargerStatus int

const (
	ChargerOK ChargerStatus = iota
	ChargerFaulted
	ChargerOffline
)

const (
	// TaperStartFrac is the SoC fraction where the battery starts reducing acceptance.
	TaperStartFrac = 0.8
	// TaperMinFactor is the acceptance factor reached at 100% SoC.
	TaperMinFactor = 0.2
	// PlannerTailCostFactor is the conservative cost multiplier the planner applies
	// to energy above TaperStartFrac (the real average factor is 1/0.6 ≈ 1.67).
	PlannerTailCostFactor = 2.0
)

// Site describes the depot's grid connection.
type Site struct {
	LimitKW float64 // total grid-side power available right now
	StepMin int     // planning step in minutes
}

// Charger is one charging point. Power values are grid-side kW.
type Charger struct {
	ID              string
	MaxKW           float64
	MinKW           float64 // charger cannot deliver between 0 and MinKW
	Efficiency      float64 // battery power / grid power, in (0,1]
	Status          ChargerStatus
	LastCommandedKW float64 // last power sent; offline chargers keep drawing it
}

func (c Charger) Healthy() bool { return c.Status == ChargerOK }

// Bus is a vehicle as the planner sees it (SoC is an estimate with age/confidence).
type Bus struct {
	ID            string
	CapacityKWh   float64
	SoCKWh        float64
	SoCAgeMin     int
	SoCConfidence float64 // 0..1
	TargetKWh     float64 // battery energy required at departure
	ArrivalMin    Minute
	DepartureMin  Minute
	MaxBatteryKW  float64 // max battery-side acceptance below the taper
	ChargerID     string  // "" when waiting for a charger
}

// TaperFactor is the fraction of MaxBatteryKW the battery accepts at a given SoC.
func TaperFactor(soc, capacity float64) float64 {
	if capacity <= 0 {
		return 0
	}
	f := soc / capacity
	if f <= TaperStartFrac {
		return 1
	}
	if f >= 1 {
		return 0
	}
	return 1 - (f-TaperStartFrac)/(1-TaperStartFrac)*(1-TaperMinFactor)
}

// EffectiveEnergy is the battery energy needed to go from soc to target, with the
// part above the taper knee weighted by PlannerTailCostFactor (conservative).
func EffectiveEnergy(soc, target, capacity float64) float64 {
	if target <= soc {
		return 0
	}
	knee := TaperStartFrac * capacity
	below := math.Max(0, math.Min(target, knee)-soc)
	above := (target - soc) - below
	return below + PlannerTailCostFactor*above
}
```

- [ ] **Step 5: Rodar e ver passar**

Run: `gofmt -l . && go vet ./... && go test ./internal/model -v`
Expected: PASS, gofmt prints nothing.

- [ ] **Step 6: Commit**

```bash
git add go.mod .gitignore README.md .github internal/model
git commit -m "feat: project scaffold and domain model with battery taper curve"
```

---

### Task 2: Tipos do planejador, utilidades e validação de entrada

**Files:**
- Create: `internal/planner/types.go`, `internal/planner/helpers.go`, `internal/planner/validate.go`
- Test: `internal/planner/helpers_test.go`, `internal/planner/validate_test.go`

**Interfaces:**
- Consumes: `model.*` da Tarefa 1.
- Produces: `planner.Config` (campos: `MarginKWh`, `StaleAfterMin int`, `MinConfidence`, `UnreliablePenaltyFrac`, `MaxUnreliableFrac`, `SurplusLaxityMin`, `SwapUrgentLaxityMin`, `SwapDonorGapMin`, `LastPlanTTLMin int`, `Timeout time.Duration`), `planner.DefaultConfig()`, `planner.Input{Now model.Minute; Site model.Site; Chargers []model.Charger; Buses []model.Bus}`, `planner.Layer` (`LayerNormal`, `LayerLastValid`, `LayerSafe`, método `String()` → `"normal"`, `"last-valid"`, `"safe"`), `planner.Setpoint{ChargerID string; KW float64}`, `planner.BusStatus{BusID string; Assessed, WillReachTarget bool; ShortfallKWh, LaxityMin float64; Reason string}`, `planner.Swap{ChargerID, OutBusID, InBusID, Reason string}`, `planner.Plan{Layer Layer; Setpoints []Setpoint; Buses []BusStatus; Swaps []Swap; Notes []string}`, `planner.AvailableKW(Input) float64`, `planner.MaxGridKW(model.Charger, model.Bus) float64`, `planner.ValidateInput(Input) error`; internos: `finite`, `clamp`, `chargerMap`, `presentBuses`.

- [ ] **Step 1: Escrever os testes que falham**

`internal/planner/helpers_test.go`:
```go
package planner

import (
	"math"
	"testing"

	"github.com/EduardoMilani8/depot-charge-planner/internal/model"
)

func testCharger(id string) model.Charger {
	return model.Charger{ID: id, MaxKW: 150, MinKW: 5, Efficiency: 1, Status: model.ChargerOK}
}

func testBus(id, chargerID string, soc, target float64, dep int) model.Bus {
	return model.Bus{ID: id, CapacityKWh: 300, SoCKWh: soc, SoCConfidence: 1, TargetKWh: target,
		ArrivalMin: 0, DepartureMin: dep, MaxBatteryKW: 150, ChargerID: chargerID}
}

func testConfig() Config {
	c := DefaultConfig()
	c.MarginKWh = 0
	return c
}

func sp(p Plan, chargerID string) float64 {
	for _, s := range p.Setpoints {
		if s.ChargerID == chargerID {
			return s.KW
		}
	}
	return 0
}

func statusOf(p Plan, busID string) BusStatus {
	for _, b := range p.Buses {
		if b.BusID == busID {
			return b
		}
	}
	return BusStatus{}
}

func near(a, b float64) bool { return math.Abs(a-b) < 1e-6 }

func TestAvailableKW(t *testing.T) {
	in := Input{Site: model.Site{LimitKW: 100, StepMin: 1}}
	if got := AvailableKW(in); got != 100 {
		t.Errorf("got %v want 100", got)
	}
	off := testCharger("C1")
	off.Status = model.ChargerOffline
	off.LastCommandedKW = 60
	in.Chargers = []model.Charger{off, testCharger("C2")}
	if got := AvailableKW(in); got != 40 {
		t.Errorf("offline draw must be reserved: got %v want 40", got)
	}
	off.LastCommandedKW = 500
	in.Chargers = []model.Charger{off}
	if got := AvailableKW(in); got != 0 {
		t.Errorf("never negative: got %v", got)
	}
	in.Site.LimitKW = math.NaN()
	if got := AvailableKW(in); got != 0 {
		t.Errorf("NaN limit must yield 0, got %v", got)
	}
}

func TestMaxGridKW(t *testing.T) {
	c := testCharger("C1")
	c.Efficiency = 0.5
	b := testBus("B1", "C1", 0, 100, 60)
	b.MaxBatteryKW = 50 // grid side = 100
	if got := MaxGridKW(c, b); got != 100 {
		t.Errorf("got %v want 100", got)
	}
}
```

`internal/planner/validate_test.go`:
```go
package planner

import (
	"math"
	"testing"

	"github.com/EduardoMilani8/depot-charge-planner/internal/model"
)

func validInput() Input {
	return Input{
		Now:      0,
		Site:     model.Site{LimitKW: 500, StepMin: 1},
		Chargers: []model.Charger{testCharger("C1")},
		Buses:    []model.Bus{testBus("B1", "C1", 100, 200, 300)},
	}
}

func TestValidateInput(t *testing.T) {
	if err := ValidateInput(validInput()); err != nil {
		t.Fatalf("valid input rejected: %v", err)
	}
	cases := map[string]func(*Input){
		"negative limit":        func(in *Input) { in.Site.LimitKW = -1 },
		"nan limit":             func(in *Input) { in.Site.LimitKW = math.NaN() },
		"zero step":             func(in *Input) { in.Site.StepMin = 0 },
		"duplicate charger":     func(in *Input) { in.Chargers = append(in.Chargers, testCharger("C1")) },
		"empty charger id":      func(in *Input) { in.Chargers[0].ID = "" },
		"charger min above max": func(in *Input) { in.Chargers[0].MinKW = 200 },
		"bad efficiency":        func(in *Input) { in.Chargers[0].Efficiency = 0 },
		"duplicate bus":         func(in *Input) { in.Buses = append(in.Buses, testBus("B1", "", 0, 100, 300)) },
		"unknown charger ref":   func(in *Input) { in.Buses[0].ChargerID = "X" },
		"two buses one charger": func(in *Input) { in.Buses = append(in.Buses, testBus("B2", "C1", 0, 100, 300)) },
		"zero capacity":         func(in *Input) { in.Buses[0].CapacityKWh = 0 },
		"zero battery power":    func(in *Input) { in.Buses[0].MaxBatteryKW = 0 },
	}
	for name, mutate := range cases {
		in := validInput()
		mutate(&in)
		if ValidateInput(in) == nil {
			t.Errorf("%s: expected validation error", name)
		}
	}
}

func TestValidateInputAcceptsBadSoC(t *testing.T) {
	// Out-of-range SoC is handled as an unreliable reading, not as invalid input.
	for _, soc := range []float64{math.NaN(), -5, 9999} {
		in := validInput()
		in.Buses[0].SoCKWh = soc
		if err := ValidateInput(in); err != nil {
			t.Errorf("soc %v rejected: %v", soc, err)
		}
	}
}

func TestValidateEmptyInput(t *testing.T) {
	in := Input{Site: model.Site{LimitKW: 0, StepMin: 1}}
	if err := ValidateInput(in); err != nil {
		t.Fatalf("empty depot must be valid: %v", err)
	}
}
```

- [ ] **Step 2: Rodar e ver falhar**

Run: `go test ./internal/planner -v`
Expected: FAIL (build failed: `undefined: Input`, `Config`, ...).

- [ ] **Step 3: Implementar**

`internal/planner/types.go`:
```go
// Package planner decides how much power each charger delivers to each bus.
package planner

import (
	"time"

	"github.com/EduardoMilani8/depot-charge-planner/internal/model"
)

// Config holds the planner tunables.
type Config struct {
	MarginKWh             float64       // extra battery energy planned on top of the route target
	StaleAfterMin         int           // SoC readings older than this are unreliable
	MinConfidence         float64       // SoC readings below this confidence are unreliable
	UnreliablePenaltyFrac float64       // fraction of capacity subtracted from unreliable SoC
	MaxUnreliableFrac     float64       // above this fraction of unreliable buses, use the safe profile
	SurplusLaxityMin      float64       // spare power only goes to buses with less laxity than this
	SwapUrgentLaxityMin   float64       // waiting buses with less laxity than this get swap suggestions
	SwapDonorGapMin       float64       // donor bus must have at least this much more laxity
	LastPlanTTLMin        int           // how long the last valid plan may be reused
	Timeout               time.Duration // max time for the normal layer
}

func DefaultConfig() Config {
	return Config{
		MarginKWh:             10,
		StaleAfterMin:         15,
		MinConfidence:         0.5,
		UnreliablePenaltyFrac: 0.10,
		MaxUnreliableFrac:     0.5,
		SurplusLaxityMin:      120,
		SwapUrgentLaxityMin:   60,
		SwapDonorGapMin:       120,
		LastPlanTTLMin:        10,
		Timeout:               500 * time.Millisecond,
	}
}

// Input is everything the planner may look at. Buses with ArrivalMin > Now are ignored.
type Input struct {
	Now      model.Minute
	Site     model.Site
	Chargers []model.Charger
	Buses    []model.Bus
}

type Layer int

const (
	LayerNormal Layer = iota
	LayerLastValid
	LayerSafe
)

func (l Layer) String() string {
	switch l {
	case LayerNormal:
		return "normal"
	case LayerLastValid:
		return "last-valid"
	case LayerSafe:
		return "safe"
	}
	return "unknown"
}

// Setpoint is the grid-side power commanded to one charger. Chargers not listed are at 0.
type Setpoint struct {
	ChargerID string
	KW        float64
}

// BusStatus explains what will happen to one bus and why.
type BusStatus struct {
	BusID           string
	Assessed        bool // false when the layer does not evaluate the target (safe profile)
	WillReachTarget bool
	ShortfallKWh    float64 // effective kWh missing at departure if WillReachTarget is false
	LaxityMin       float64
	Reason          string
}

// Swap suggests a human move: InBusID takes ChargerID from OutBusID.
type Swap struct {
	ChargerID string
	OutBusID  string
	InBusID   string
	Reason    string
}

type Plan struct {
	Layer     Layer
	Setpoints []Setpoint
	Buses     []BusStatus
	Swaps     []Swap
	Notes     []string
}
```

`internal/planner/helpers.go`:
```go
package planner

import (
	"math"
	"sort"

	"github.com/EduardoMilani8/depot-charge-planner/internal/model"
)

func finite(x float64) bool { return !math.IsNaN(x) && !math.IsInf(x, 0) }

func clamp(x, lo, hi float64) float64 { return math.Max(lo, math.Min(hi, x)) }

func chargerMap(in Input) map[string]model.Charger {
	m := make(map[string]model.Charger, len(in.Chargers))
	for _, c := range in.Chargers {
		m[c.ID] = c
	}
	return m
}

// presentBuses returns buses that have arrived, sorted by ID for determinism.
func presentBuses(in Input) []model.Bus {
	out := make([]model.Bus, 0, len(in.Buses))
	for _, b := range in.Buses {
		if b.ArrivalMin <= in.Now {
			out = append(out, b)
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].ID < out[j].ID })
	return out
}

// AvailableKW is the power budget for controllable chargers: the site limit minus
// what offline chargers keep drawing at their last commanded power.
func AvailableKW(in Input) float64 {
	limit := in.Site.LimitKW
	if !finite(limit) || limit < 0 {
		limit = 0
	}
	for _, c := range in.Chargers {
		if c.Status == model.ChargerOffline && finite(c.LastCommandedKW) && c.LastCommandedKW > 0 {
			limit -= c.LastCommandedKW
		}
	}
	if limit < 0 {
		return 0
	}
	return limit
}

// MaxGridKW is the highest grid-side power a charger can deliver to a bus.
func MaxGridKW(c model.Charger, b model.Bus) float64 {
	return math.Min(c.MaxKW, b.MaxBatteryKW/c.Efficiency)
}
```

`internal/planner/validate.go`:
```go
package planner

import "fmt"

// ValidateInput rejects structurally broken input. Bad SoC readings are NOT
// rejected here: they are treated as unreliable by the planner layers.
func ValidateInput(in Input) error {
	if !finite(in.Site.LimitKW) || in.Site.LimitKW < 0 {
		return fmt.Errorf("limite da garagem inválido: %v", in.Site.LimitKW)
	}
	if in.Site.StepMin <= 0 {
		return fmt.Errorf("passo de planejamento inválido: %d", in.Site.StepMin)
	}
	chargers := map[string]bool{}
	for _, c := range in.Chargers {
		switch {
		case c.ID == "":
			return fmt.Errorf("carregador sem ID")
		case chargers[c.ID]:
			return fmt.Errorf("carregador duplicado: %s", c.ID)
		case !finite(c.MaxKW) || c.MaxKW <= 0:
			return fmt.Errorf("carregador %s: potência máxima inválida", c.ID)
		case !finite(c.MinKW) || c.MinKW < 0 || c.MinKW > c.MaxKW:
			return fmt.Errorf("carregador %s: piso inválido", c.ID)
		case !finite(c.Efficiency) || c.Efficiency <= 0 || c.Efficiency > 1:
			return fmt.Errorf("carregador %s: rendimento inválido", c.ID)
		case !finite(c.LastCommandedKW) || c.LastCommandedKW < 0:
			return fmt.Errorf("carregador %s: última potência inválida", c.ID)
		}
		chargers[c.ID] = true
	}
	buses := map[string]bool{}
	used := map[string]string{}
	for _, b := range in.Buses {
		switch {
		case b.ID == "":
			return fmt.Errorf("ônibus sem ID")
		case buses[b.ID]:
			return fmt.Errorf("ônibus duplicado: %s", b.ID)
		case !finite(b.CapacityKWh) || b.CapacityKWh <= 0:
			return fmt.Errorf("ônibus %s: capacidade inválida", b.ID)
		case !finite(b.MaxBatteryKW) || b.MaxBatteryKW <= 0:
			return fmt.Errorf("ônibus %s: potência da bateria inválida", b.ID)
		case !finite(b.TargetKWh) || b.TargetKWh < 0:
			return fmt.Errorf("ônibus %s: alvo inválido", b.ID)
		case b.ChargerID != "" && !chargers[b.ChargerID]:
			return fmt.Errorf("ônibus %s referencia carregador inexistente %s", b.ID, b.ChargerID)
		}
		if b.ChargerID != "" {
			if other, dup := used[b.ChargerID]; dup {
				return fmt.Errorf("ônibus %s e %s no mesmo carregador %s", other, b.ID, b.ChargerID)
			}
			used[b.ChargerID] = b.ID
		}
		buses[b.ID] = true
	}
	return nil
}
```

- [ ] **Step 4: Rodar e ver passar**

Run: `gofmt -l . && go vet ./... && go test ./internal/planner -v`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/planner
git commit -m "feat(planner): types, power budget helpers and input validation"
```

---

### Task 3: Camada 0 — perfil seguro

**Files:**
- Create: `internal/planner/safe.go`
- Test: `internal/planner/safe_test.go`

**Interfaces:**
- Consumes: `Input`, `Plan`, `Setpoint`, `BusStatus`, `LayerSafe`, `AvailableKW`, `MaxGridKW`, `chargerMap`, `presentBuses`.
- Produces: `planner.PlanSafe(in Input) Plan` (setpoints ordenados por ID de carregador; não usa SoC).

- [ ] **Step 1: Escrever os testes que falham**

`internal/planner/safe_test.go`:
```go
package planner

import (
	"math"
	"testing"

	"github.com/EduardoMilani8/depot-charge-planner/internal/model"
)

func fourBusInput(limit float64) Input {
	in := Input{Now: 0, Site: model.Site{LimitKW: limit, StepMin: 1}}
	for _, id := range []string{"1", "2", "3", "4"} {
		in.Chargers = append(in.Chargers, testCharger("C"+id))
		in.Buses = append(in.Buses, testBus("B"+id, "C"+id, 100, 200, 300))
	}
	return in
}

func TestPlanSafeEqualShare(t *testing.T) {
	p := PlanSafe(fourBusInput(400))
	if p.Layer != LayerSafe {
		t.Errorf("layer = %v", p.Layer)
	}
	for _, id := range []string{"C1", "C2", "C3", "C4"} {
		if got := sp(p, id); got != 100 {
			t.Errorf("%s = %v, want 100", id, got)
		}
	}
}

func TestPlanSafeBelowFloorTurnsOff(t *testing.T) {
	p := PlanSafe(fourBusInput(10)) // share 2.5 kW < 5 kW floor
	for _, s := range p.Setpoints {
		if s.KW != 0 {
			t.Errorf("%s = %v, want 0 (below floor)", s.ChargerID, s.KW)
		}
	}
}

func TestPlanSafeIgnoresSoC(t *testing.T) {
	in := fourBusInput(100)
	in.Buses = in.Buses[:1]
	in.Chargers = in.Chargers[:1]
	in.Buses[0].SoCKWh = math.NaN()
	if got := sp(PlanSafe(in), "C1"); got != 100 {
		t.Errorf("got %v, want 100", got)
	}
}

func TestPlanSafeSkipsUnhealthyCharger(t *testing.T) {
	in := fourBusInput(300)
	in.Chargers[0].Status = model.ChargerFaulted
	p := PlanSafe(in)
	if len(p.Setpoints) != 3 {
		t.Fatalf("got %d setpoints, want 3", len(p.Setpoints))
	}
	if got := sp(p, "C2"); got != 100 {
		t.Errorf("C2 = %v, want 100", got)
	}
}

func TestPlanSafeReservesOfflineDraw(t *testing.T) {
	in := fourBusInput(100)
	in.Buses = in.Buses[:2]
	in.Chargers = in.Chargers[:2]
	in.Chargers[0].Status = model.ChargerOffline
	in.Chargers[0].LastCommandedKW = 60
	if got := sp(PlanSafe(in), "C2"); got != 40 {
		t.Errorf("C2 = %v, want 40 (limit minus offline draw)", got)
	}
}

func TestPlanSafeEmpty(t *testing.T) {
	p := PlanSafe(Input{Site: model.Site{LimitKW: 100, StepMin: 1}})
	if len(p.Setpoints) != 0 {
		t.Errorf("empty depot should have no setpoints")
	}
}
```

- [ ] **Step 2: Rodar e ver falhar**

Run: `go test ./internal/planner -run PlanSafe -v`
Expected: FAIL (`undefined: PlanSafe`).

- [ ] **Step 3: Implementar**

`internal/planner/safe.go`:
```go
package planner

import (
	"math"
	"sort"

	"github.com/EduardoMilani8/depot-charge-planner/internal/model"
)

// PlanSafe is layer 0: an equal split of the available power among connected buses
// on healthy chargers. It never reads SoC, so bad sensor data cannot break it.
func PlanSafe(in Input) Plan {
	chargers := chargerMap(in)
	var connected []model.Bus
	for _, b := range presentBuses(in) {
		if c, ok := chargers[b.ChargerID]; ok && c.Healthy() {
			connected = append(connected, b)
		}
	}
	p := Plan{Layer: LayerSafe, Notes: []string{"perfil seguro: divisão igual do limite, sem depender de SoC"}}
	if len(connected) == 0 {
		return p
	}
	share := AvailableKW(in) / float64(len(connected))
	for _, b := range connected {
		c := chargers[b.ChargerID]
		kw := math.Min(share, MaxGridKW(c, b))
		if kw < c.MinKW {
			kw = 0
		}
		p.Setpoints = append(p.Setpoints, Setpoint{ChargerID: c.ID, KW: kw})
		p.Buses = append(p.Buses, BusStatus{BusID: b.ID, Reason: "perfil seguro: potência igual, sem avaliação de meta"})
	}
	sort.Slice(p.Setpoints, func(i, j int) bool { return p.Setpoints[i].ChargerID < p.Setpoints[j].ChargerID })
	return p
}
```

- [ ] **Step 4: Rodar e ver passar**

Run: `gofmt -l . && go vet ./... && go test ./internal/planner -v`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/planner
git commit -m "feat(planner): layer 0 safe profile with equal power split"
```



### Task 4: Verificador de invariantes

**Files:**
- Create: `internal/planner/enforce.go`
- Test: `internal/planner/enforce_test.go`

**Interfaces:**
- Consumes: `Input`, `Plan`, `Setpoint`, `AvailableKW`, `chargerMap`, `finite`.
- Produces: `planner.Violations(in Input, p Plan) []string` (checagem pura, lista vazia = plano válido) e `planner.Enforce(in Input, p Plan) Plan` (devolve o plano corrigido, com nota em `Notes` quando corrigiu algo; nunca altera o plano recebido).

- [ ] **Step 1: Escrever os testes que falham**

`internal/planner/enforce_test.go`:
```go
package planner

import (
	"math"
	"reflect"
	"testing"

	"github.com/EduardoMilani8/depot-charge-planner/internal/model"
)

func twoChargerInput(limit float64) Input {
	return Input{
		Now:      0,
		Site:     model.Site{LimitKW: limit, StepMin: 1},
		Chargers: []model.Charger{testCharger("C1"), testCharger("C2")},
	}
}

func TestEnforceKeepsValidPlan(t *testing.T) {
	in := twoChargerInput(200)
	p := Plan{Setpoints: []Setpoint{{"C1", 80}, {"C2", 100}}}
	if v := Violations(in, p); len(v) != 0 {
		t.Fatalf("unexpected violations: %v", v)
	}
	if got := Enforce(in, p); !reflect.DeepEqual(got, p) {
		t.Errorf("valid plan must be returned unchanged: %+v", got)
	}
}

func TestEnforceScalesDownOverLimit(t *testing.T) {
	in := twoChargerInput(100)
	p := Plan{Setpoints: []Setpoint{{"C1", 100}, {"C2", 100}}}
	if len(Violations(in, p)) == 0 {
		t.Fatal("expected a violation")
	}
	got := Enforce(in, p)
	if !near(sp(got, "C1"), 50) || !near(sp(got, "C2"), 50) {
		t.Errorf("expected 50/50, got %+v", got.Setpoints)
	}
	if v := Violations(in, got); len(v) != 0 {
		t.Errorf("enforced plan still violates: %v", v)
	}
	if len(got.Notes) == 0 {
		t.Error("a correction must leave a note")
	}
	if p.Setpoints[0].KW != 100 {
		t.Error("Enforce must not mutate its input")
	}
}

func TestEnforceClampsToChargerMax(t *testing.T) {
	in := twoChargerInput(1000)
	got := Enforce(in, Plan{Setpoints: []Setpoint{{"C1", 400}}})
	if sp(got, "C1") != 150 {
		t.Errorf("got %v, want 150", sp(got, "C1"))
	}
}

func TestEnforceZeroesBelowFloor(t *testing.T) {
	in := twoChargerInput(1000)
	got := Enforce(in, Plan{Setpoints: []Setpoint{{"C1", 3}}})
	if sp(got, "C1") != 0 {
		t.Errorf("got %v, want 0 (below 5 kW floor)", sp(got, "C1"))
	}
}

func TestEnforceDropsUnknownDuplicateInvalidAndUnhealthy(t *testing.T) {
	in := twoChargerInput(1000)
	in.Chargers[1].Status = model.ChargerFaulted
	p := Plan{Setpoints: []Setpoint{
		{"X", 50}, {"C1", 60}, {"C1", 70}, {"C2", 80},
	}}
	got := Enforce(in, p)
	if len(got.Setpoints) != 1 || got.Setpoints[0].ChargerID != "C1" || got.Setpoints[0].KW != 60 {
		t.Errorf("unexpected setpoints: %+v", got.Setpoints)
	}
	bad := Plan{Setpoints: []Setpoint{{"C1", math.NaN()}}}
	if got := Enforce(in, bad); len(got.Setpoints) != 0 {
		t.Errorf("NaN setpoint must be dropped: %+v", got.Setpoints)
	}
	neg := Plan{Setpoints: []Setpoint{{"C1", -5}}}
	if got := Enforce(in, neg); len(got.Setpoints) != 0 {
		t.Errorf("negative setpoint must be dropped: %+v", got.Setpoints)
	}
}

func TestEnforceReservesOfflineDraw(t *testing.T) {
	in := twoChargerInput(100)
	in.Chargers[0].Status = model.ChargerOffline
	in.Chargers[0].LastCommandedKW = 60
	got := Enforce(in, Plan{Setpoints: []Setpoint{{"C2", 80}}})
	if !near(sp(got, "C2"), 40) {
		t.Errorf("C2 = %v, want 40", sp(got, "C2"))
	}
}

func TestEnforceNaNLimitMeansZero(t *testing.T) {
	in := twoChargerInput(math.NaN())
	got := Enforce(in, Plan{Setpoints: []Setpoint{{"C1", 100}}})
	if sp(got, "C1") != 0 {
		t.Errorf("got %v, want 0", sp(got, "C1"))
	}
}
```

- [ ] **Step 2: Rodar e ver falhar**

Run: `go test ./internal/planner -run Enforce -v`
Expected: FAIL (`undefined: Violations`).

- [ ] **Step 3: Implementar**

`internal/planner/enforce.go`:
```go
package planner

import (
	"fmt"
	"math"
)

const eps = 1e-6

// Violations lists every invariant the plan breaks. It is intentionally small and
// independent of the planner layers: whoever computes is not who guarantees.
func Violations(in Input, p Plan) []string {
	chargers := chargerMap(in)
	var out []string
	seen := map[string]bool{}
	total := 0.0
	for _, s := range p.Setpoints {
		c, ok := chargers[s.ChargerID]
		if !ok {
			out = append(out, fmt.Sprintf("carregador desconhecido: %s", s.ChargerID))
			continue
		}
		if seen[s.ChargerID] {
			out = append(out, fmt.Sprintf("setpoint duplicado: %s", s.ChargerID))
			continue
		}
		seen[s.ChargerID] = true
		if !finite(s.KW) || s.KW < 0 {
			out = append(out, fmt.Sprintf("potência inválida em %s: %v", s.ChargerID, s.KW))
			continue
		}
		if !c.Healthy() && s.KW > 0 {
			out = append(out, fmt.Sprintf("carregador indisponível %s recebeu %.1f kW", s.ChargerID, s.KW))
		}
		if s.KW > c.MaxKW+eps {
			out = append(out, fmt.Sprintf("%s acima do teto: %.1f > %.1f kW", s.ChargerID, s.KW, c.MaxKW))
		}
		if s.KW > 0 && s.KW < c.MinKW-eps {
			out = append(out, fmt.Sprintf("%s abaixo do piso: %.1f < %.1f kW", s.ChargerID, s.KW, c.MinKW))
		}
		total += s.KW
	}
	if limit := AvailableKW(in); total > limit+eps {
		out = append(out, fmt.Sprintf("potência total %.1f kW acima do orçamento %.1f kW", total, limit))
	}
	return out
}

// Enforce returns a plan that satisfies every invariant, correcting the input
// plan if needed (scaling down proportionally, then switching off below the floor).
func Enforce(in Input, p Plan) Plan {
	viol := Violations(in, p)
	if len(viol) == 0 {
		return p
	}
	chargers := chargerMap(in)
	fixed := make([]Setpoint, 0, len(p.Setpoints))
	seen := map[string]bool{}
	total := 0.0
	for _, s := range p.Setpoints {
		c, ok := chargers[s.ChargerID]
		if !ok || seen[s.ChargerID] || !c.Healthy() || !finite(s.KW) || s.KW < 0 {
			continue
		}
		seen[s.ChargerID] = true
		kw := math.Min(s.KW, c.MaxKW)
		fixed = append(fixed, Setpoint{ChargerID: s.ChargerID, KW: kw})
		total += kw
	}
	if limit := AvailableKW(in); total > limit {
		f := 0.0
		if total > 0 {
			f = limit / total
		}
		for i := range fixed {
			fixed[i].KW *= f
		}
	}
	for i := range fixed {
		if fixed[i].KW < chargers[fixed[i].ChargerID].MinKW {
			fixed[i].KW = 0
		}
	}
	out := p
	out.Setpoints = fixed
	out.Notes = append(append([]string(nil), p.Notes...), fmt.Sprintf("verificador corrigiu o plano: %d violação(ões)", len(viol)))
	return out
}
```

- [ ] **Step 4: Rodar e ver passar**

Run: `gofmt -l . && go vet ./... && go test ./internal/planner -v`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/planner
git commit -m "feat(planner): independent invariant verifier (Violations/Enforce)"
```

---

### Task 5: Camada 1 — regra por folga

**Files:**
- Create: `internal/planner/normal.go`
- Test: `internal/planner/normal_test.go`

**Interfaces:**
- Consumes: tudo das Tarefas 1 a 4.
- Produces: `planner.PlanNormal(cfg Config, in Input) Plan` (função pura, `Layer: LayerNormal`, setpoints ordenados por ID de carregador, status ordenados por ID de ônibus, `Swaps` vazio até a Tarefa 6). Internos usados na Tarefa 6: `candidate` (campos `bus model.Bus`, `charger model.Charger`, `need`, `gridNeed`, `availMin`, `maxKW`, `laxityMin`, `requiredKW`, `allocKW float64`, `unusable`, `reliable bool`), `idleBus{bus model.Bus; reason string}`, `newCandidate(cfg Config, in Input, b model.Bus, c model.Charger) candidate`.

**Review Focus pinned here:** limite da bateria abaixo do piso; SoC NaN/negativo/acima da capacidade; saída já passada; entrada vazia e limite zero; carregador offline reservando orçamento.

- [ ] **Step 1: Escrever os testes que falham**

`internal/planner/normal_test.go`:
```go
package planner

import (
	"math"
	"reflect"
	"strings"
	"testing"

	"github.com/EduardoMilani8/depot-charge-planner/internal/model"
)

func oneBus(limit float64, b model.Bus) Input {
	return Input{
		Now:      0,
		Site:     model.Site{LimitKW: limit, StepMin: 1},
		Chargers: []model.Charger{testCharger("C1")},
		Buses:    []model.Bus{b},
	}
}

func TestNormalJustInTime(t *testing.T) {
	// need 120 kWh, 240 min left, laxity 192 min (>= 120): no surplus, just 30 kW.
	p := PlanNormal(testConfig(), oneBus(1000, testBus("B1", "C1", 100, 220, 240)))
	if !near(sp(p, "C1"), 30) {
		t.Errorf("setpoint = %v, want 30", sp(p, "C1"))
	}
	st := statusOf(p, "B1")
	if !st.WillReachTarget || !strings.Contains(st.Reason, "folga") {
		t.Errorf("unexpected status: %+v", st)
	}
	if p.Layer != LayerNormal {
		t.Errorf("layer = %v", p.Layer)
	}
}

func TestNormalSurplusForLowLaxity(t *testing.T) {
	// 100 min left: laxity 52 min (< 120): required 72 kW, surplus raises it to the max.
	p := PlanNormal(testConfig(), oneBus(1000, testBus("B1", "C1", 100, 220, 100)))
	if !near(sp(p, "C1"), 150) {
		t.Errorf("setpoint = %v, want 150", sp(p, "C1"))
	}
}

func TestNormalScarceBudgetPrioritisesLeastLaxity(t *testing.T) {
	in := Input{
		Site:     model.Site{LimitKW: 160, StepMin: 1},
		Chargers: []model.Charger{testCharger("C1"), testCharger("C2")},
		Buses: []model.Bus{
			testBus("A", "C1", 100, 220, 60),  // laxity 12, required 120
			testBus("B", "C2", 100, 220, 300), // laxity 252, required 24
		},
	}
	p := PlanNormal(testConfig(), in)
	if !near(sp(p, "C1"), 136) || !near(sp(p, "C2"), 24) {
		t.Errorf("got A=%v B=%v, want 136/24", sp(p, "C1"), sp(p, "C2"))
	}
}

func TestNormalDoomedBusDoesNotStarveSavableOne(t *testing.T) {
	in := Input{
		Site:     model.Site{LimitKW: 150, StepMin: 1},
		Chargers: []model.Charger{testCharger("C1"), testCharger("C2")},
		Buses: []model.Bus{
			testBus("A", "C1", 0, 240, 60),    // needs 96 min at max, has 60: doomed
			testBus("B", "C2", 100, 220, 300), // savable, required 24
		},
	}
	p := PlanNormal(testConfig(), in)
	if !near(sp(p, "C2"), 24) || !near(sp(p, "C1"), 126) {
		t.Errorf("got A=%v B=%v, want 126/24", sp(p, "C1"), sp(p, "C2"))
	}
	a := statusOf(p, "A")
	if a.WillReachTarget || a.ShortfallKWh <= 0 || !strings.Contains(a.Reason, "inviável") {
		t.Errorf("A should be flagged infeasible with shortfall: %+v", a)
	}
	if !statusOf(p, "B").WillReachTarget {
		t.Error("B must reach its target")
	}
}

func TestNormalBatteryLimitBelowChargerFloor(t *testing.T) {
	b := testBus("B1", "C1", 100, 220, 300)
	b.MaxBatteryKW = 3 // charger floor is 5 kW
	p := PlanNormal(testConfig(), oneBus(1000, b))
	if sp(p, "C1") != 0 {
		t.Errorf("setpoint = %v, want 0", sp(p, "C1"))
	}
	if st := statusOf(p, "B1"); st.WillReachTarget || !strings.Contains(st.Reason, "bateria") {
		t.Errorf("unexpected status: %+v", st)
	}
}

func TestNormalUnreliableSoCIsConservative(t *testing.T) {
	b := testBus("B1", "C1", 200, 220, 300)
	b.SoCAgeMin = 100 // stale: 200 - 10% of 300 = 170, need 50 kWh over 300 min
	p := PlanNormal(testConfig(), oneBus(1000, b))
	if !near(sp(p, "C1"), 10) {
		t.Errorf("setpoint = %v, want 10", sp(p, "C1"))
	}
	if !strings.Contains(statusOf(p, "B1").Reason, "não confiável") {
		t.Errorf("reason should mention unreliable reading: %q", statusOf(p, "B1").Reason)
	}
}

func TestNormalAbsurdSoCReadingsAssumeEmptyBattery(t *testing.T) {
	for _, soc := range []float64{math.NaN(), -10, 500} {
		b := testBus("B1", "C1", soc, 150, 300) // target 150 from 0, 300 min: 30 kW
		p := PlanNormal(testConfig(), oneBus(1000, b))
		if !near(sp(p, "C1"), 30) {
			t.Errorf("soc %v: setpoint = %v, want 30", soc, sp(p, "C1"))
		}
	}
}

func TestNormalOfflineChargerReservesBudget(t *testing.T) {
	in := Input{
		Site: model.Site{LimitKW: 100, StepMin: 1},
		Chargers: []model.Charger{
			{ID: "C1", MaxKW: 150, MinKW: 5, Efficiency: 1, Status: model.ChargerOffline, LastCommandedKW: 60},
			testCharger("C2"),
		},
		Buses: []model.Bus{
			testBus("B1", "C1", 100, 220, 300),
			testBus("B2", "C2", 0, 240, 100),
		},
	}
	p := PlanNormal(testConfig(), in)
	if !near(sp(p, "C2"), 40) {
		t.Errorf("C2 = %v, want 40 (limit 100 minus 60 offline draw)", sp(p, "C2"))
	}
	if sp(p, "C1") != 0 || !strings.Contains(statusOf(p, "B1").Reason, "indisponível") {
		t.Errorf("offline charger must not be commanded: %+v", p)
	}
}

func TestNormalDepartureNotInTheFuture(t *testing.T) {
	p := PlanNormal(testConfig(), oneBus(1000, testBus("B1", "C1", 100, 220, 0)))
	kw := sp(p, "C1")
	if math.IsNaN(kw) || math.IsInf(kw, 0) || kw > 150 {
		t.Errorf("setpoint must be finite and within the charger max, got %v", kw)
	}
	if st := statusOf(p, "B1"); st.WillReachTarget || st.ShortfallKWh <= 0 {
		t.Errorf("bus past its departure must be infeasible: %+v", st)
	}
}

func TestNormalEmptyAndZeroLimit(t *testing.T) {
	if p := PlanNormal(testConfig(), Input{}); len(p.Setpoints) != 0 {
		t.Errorf("empty input: %+v", p)
	}
	p := PlanNormal(testConfig(), oneBus(0, testBus("B1", "C1", 100, 220, 100)))
	if sp(p, "C1") != 0 {
		t.Errorf("zero limit: setpoint %v", sp(p, "C1"))
	}
	if st := statusOf(p, "B1"); !strings.Contains(st.Reason, "potência") {
		t.Errorf("reason should explain the lack of power: %+v", st)
	}
}

func TestNormalBusAlreadyAtTarget(t *testing.T) {
	p := PlanNormal(testConfig(), oneBus(1000, testBus("B1", "C1", 230, 220, 300)))
	if sp(p, "C1") != 0 || !statusOf(p, "B1").WillReachTarget {
		t.Errorf("unexpected plan: %+v", p)
	}
}

func TestNormalIsDeterministicAndPure(t *testing.T) {
	in := Input{
		Site:     model.Site{LimitKW: 200, StepMin: 1},
		Chargers: []model.Charger{testCharger("C1"), testCharger("C2"), testCharger("C3")},
		Buses: []model.Bus{
			testBus("B3", "C3", 90, 220, 200),
			testBus("B1", "C1", 100, 220, 200),
			testBus("B2", "C2", 100, 220, 200),
		},
	}
	a := PlanNormal(testConfig(), in)
	b := PlanNormal(testConfig(), in)
	if !reflect.DeepEqual(a, b) {
		t.Error("same input must give the same plan")
	}
	if in.Buses[0].ID != "B3" {
		t.Error("input must not be reordered or mutated")
	}
	if v := Violations(in, a); len(v) != 0 {
		t.Errorf("normal plan violates invariants: %v", v)
	}
}
```

- [ ] **Step 2: Rodar e ver falhar**

Run: `go test ./internal/planner -run Normal -v`
Expected: FAIL (`undefined: PlanNormal`).

- [ ] **Step 3: Implementar**

`internal/planner/normal.go`:
```go
package planner

import (
	"fmt"
	"math"
	"sort"

	"github.com/EduardoMilani8/depot-charge-planner/internal/model"
)

// candidate is a connected bus on a healthy charger, with its derived numbers.
type candidate struct {
	bus        model.Bus
	charger    model.Charger
	need       float64 // battery kWh missing to the (margin-adjusted) target
	gridNeed   float64 // grid kWh required, using the conservative taper cost
	availMin   float64 // minutes until departure (never negative)
	maxKW      float64 // highest grid-side power for this pair
	laxityMin  float64 // minutes of spare time at max power
	requiredKW float64 // power that just meets the deadline
	allocKW    float64
	unusable   bool // battery limit is below the charger floor: cannot charge at all
	reliable   bool
}

// idleBus is a present bus that cannot be charged right now.
type idleBus struct {
	bus    model.Bus
	reason string
}

func isReliable(cfg Config, b model.Bus) bool {
	if !finite(b.SoCKWh) || b.SoCKWh < 0 || b.SoCKWh > b.CapacityKWh {
		return false
	}
	if !finite(b.SoCConfidence) || b.SoCConfidence < cfg.MinConfidence {
		return false
	}
	return b.SoCAgeMin <= cfg.StaleAfterMin
}

// usableSoC returns the SoC the planner will trust: absurd readings are treated as
// an empty battery (worst case), stale ones get a conservative penalty.
func usableSoC(cfg Config, b model.Bus) (float64, bool) {
	rel := isReliable(cfg, b)
	switch {
	case !finite(b.SoCKWh) || b.SoCKWh < 0 || b.SoCKWh > b.CapacityKWh:
		return 0, false
	case !rel:
		return clamp(b.SoCKWh-cfg.UnreliablePenaltyFrac*b.CapacityKWh, 0, b.CapacityKWh), false
	}
	return b.SoCKWh, true
}

func newCandidate(cfg Config, in Input, b model.Bus, c model.Charger) candidate {
	soc, rel := usableSoC(cfg, b)
	target := math.Min(b.CapacityKWh, b.TargetKWh+cfg.MarginKWh)
	cd := candidate{bus: b, charger: c, reliable: rel}
	cd.need = math.Max(0, target-soc)
	cd.maxKW = MaxGridKW(c, b)
	cd.availMin = math.Max(0, float64(b.DepartureMin-in.Now))
	cd.gridNeed = model.EffectiveEnergy(soc, target, b.CapacityKWh) / c.Efficiency
	if cd.maxKW <= 0 || cd.maxKW < c.MinKW {
		cd.unusable = true
	}
	switch {
	case cd.need <= 0:
		cd.laxityMin = cd.availMin
	case cd.unusable:
		cd.laxityMin = math.Inf(-1)
	default:
		cd.laxityMin = cd.availMin - cd.gridNeed/cd.maxKW*60
		if cd.availMin > 0 {
			cd.requiredKW = clamp(cd.gridNeed/(cd.availMin/60), c.MinKW, cd.maxKW)
		} else {
			cd.requiredKW = cd.maxKW
		}
	}
	return cd
}

// allocate gives each bus the power that just meets its deadline (savable buses
// first, least laxity first; doomed buses last), then spends spare power on buses
// whose laxity is below cfg.SurplusLaxityMin.
func allocate(cfg Config, budget float64, cands []*candidate) {
	var savable, doomed []*candidate
	for _, c := range cands {
		if c.need <= 0 || c.unusable {
			continue
		}
		if c.laxityMin >= 0 {
			savable = append(savable, c)
		} else {
			doomed = append(doomed, c)
		}
	}
	sort.SliceStable(savable, func(i, j int) bool {
		if savable[i].laxityMin != savable[j].laxityMin {
			return savable[i].laxityMin < savable[j].laxityMin
		}
		return savable[i].bus.ID < savable[j].bus.ID
	})
	sort.SliceStable(doomed, func(i, j int) bool {
		if doomed[i].laxityMin != doomed[j].laxityMin {
			return doomed[i].laxityMin > doomed[j].laxityMin
		}
		return doomed[i].bus.ID < doomed[j].bus.ID
	})
	order := append(append([]*candidate(nil), savable...), doomed...)
	for _, c := range order {
		give := math.Min(c.requiredKW, budget)
		if give < c.charger.MinKW {
			give = 0
		}
		c.allocKW = give
		budget -= give
	}
	for _, c := range order {
		if budget <= 0 {
			break
		}
		if c.allocKW == 0 || c.laxityMin >= cfg.SurplusLaxityMin {
			continue
		}
		extra := math.Min(c.maxKW-c.allocKW, budget)
		if extra > 0 {
			c.allocKW += extra
			budget -= extra
		}
	}
}

func finiteLaxity(x float64) float64 {
	switch {
	case math.IsInf(x, -1):
		return -1e6
	case math.IsInf(x, 1):
		return 1e6
	}
	return x
}

func (cd *candidate) status() BusStatus {
	st := BusStatus{BusID: cd.bus.ID, Assessed: true, LaxityMin: finiteLaxity(cd.laxityMin)}
	delivered := cd.allocKW * cd.availMin / 60
	st.WillReachTarget = cd.need <= 0 || delivered >= cd.gridNeed-1e-6
	if !st.WillReachTarget {
		st.ShortfallKWh = math.Max(0, cd.gridNeed-delivered) * cd.charger.Efficiency
	}
	switch {
	case cd.need <= 0:
		st.Reason = "alvo atingido"
	case cd.unusable:
		st.Reason = "limite da bateria abaixo do piso do carregador: não é possível carregar"
	case cd.allocKW == 0:
		st.Reason = "sem potência disponível dentro do limite da garagem"
	case cd.laxityMin < 0:
		st.Reason = fmt.Sprintf("inviável: mesmo na potência máxima faltam %.0f min; déficit previsto %.1f kWh", -cd.laxityMin, st.ShortfallKWh)
	default:
		st.Reason = fmt.Sprintf("folga %.0f min; %.1f kW", cd.laxityMin, cd.allocKW)
	}
	if !cd.reliable && cd.need > 0 {
		st.Reason += " (leitura de SoC não confiável: estimativa conservadora)"
	}
	return st
}

func statusName(s model.ChargerStatus) string {
	switch s {
	case model.ChargerFaulted:
		return "em falha"
	case model.ChargerOffline:
		return "offline"
	}
	return "ok"
}

func idleStatus(cfg Config, ib idleBus) BusStatus {
	soc, _ := usableSoC(cfg, ib.bus)
	target := math.Min(ib.bus.CapacityKWh, ib.bus.TargetKWh+cfg.MarginKWh)
	need := math.Max(0, target-soc)
	return BusStatus{BusID: ib.bus.ID, Assessed: true, WillReachTarget: need <= 0, ShortfallKWh: need, Reason: ib.reason}
}

// PlanNormal is layer 1: least-laxity-first allocation under the power budget.
func PlanNormal(cfg Config, in Input) Plan {
	chargers := chargerMap(in)
	plan := Plan{Layer: LayerNormal}
	var cands []*candidate
	var idles []idleBus
	for _, b := range presentBuses(in) {
		c, ok := chargers[b.ChargerID]
		switch {
		case b.ChargerID == "" || !ok:
			idles = append(idles, idleBus{bus: b, reason: "aguardando carregador"})
		case !c.Healthy():
			idles = append(idles, idleBus{bus: b, reason: fmt.Sprintf("carregador %s indisponível (%s)", c.ID, statusName(c.Status))})
		default:
			cd := newCandidate(cfg, in, b, c)
			cands = append(cands, &cd)
		}
	}
	allocate(cfg, AvailableKW(in), cands)
	for _, cd := range cands {
		plan.Setpoints = append(plan.Setpoints, Setpoint{ChargerID: cd.charger.ID, KW: cd.allocKW})
		plan.Buses = append(plan.Buses, cd.status())
	}
	for _, ib := range idles {
		plan.Buses = append(plan.Buses, idleStatus(cfg, ib))
	}
	sort.Slice(plan.Setpoints, func(i, j int) bool { return plan.Setpoints[i].ChargerID < plan.Setpoints[j].ChargerID })
	sort.Slice(plan.Buses, func(i, j int) bool { return plan.Buses[i].BusID < plan.Buses[j].BusID })
	return plan
}
```

- [ ] **Step 4: Rodar e ver passar**

Run: `gofmt -l . && go vet ./... && go test ./internal/planner -v`
Expected: PASS. Se algum teste numérico falhar, confira a conta no comentário do teste antes de mudar o código.

- [ ] **Step 5: Commit**

```bash
git add internal/planner
git commit -m "feat(planner): layer 1 least-laxity allocation with just-in-time power"
```

---

### Task 6: Recomendação de rodízio

**Files:**
- Create: `internal/planner/swaps.go`
- Modify: `internal/planner/normal.go` (uma linha em `PlanNormal`)
- Test: `internal/planner/swaps_test.go`

**Interfaces:**
- Consumes: `candidate`, `idleBus`, `newCandidate`, `Config.SwapUrgentLaxityMin`, `Config.SwapDonorGapMin`.
- Produces: `recommendSwaps(cfg Config, in Input, cands []*candidate, idles []idleBus) []Swap`; `PlanNormal` passa a preencher `Plan.Swaps`.

Regras: só recomenda quando **não há carregador saudável livre** (se houver, o ônibus esperando deve apenas se conectar a ele); ônibus esperando é "urgente" se `need > 0` e folga menor que `SwapUrgentLaxityMin` (calculada contra o melhor carregador saudável); o doador é o ônibus conectado com maior folga, que precisa ter pelo menos `SwapDonorGapMin` minutos a mais de folga que o urgente.

- [ ] **Step 1: Escrever os testes que falham**

`internal/planner/swaps_test.go`:
```go
package planner

import (
	"testing"

	"github.com/EduardoMilani8/depot-charge-planner/internal/model"
)

func swapInput(donor, waiting model.Bus, extraChargers ...model.Charger) Input {
	in := Input{
		Site:     model.Site{LimitKW: 500, StepMin: 1},
		Chargers: append([]model.Charger{testCharger("C1")}, extraChargers...),
		Buses:    []model.Bus{donor, waiting},
	}
	return in
}

func TestSwapRecommendedWhenWaitingBusIsUrgent(t *testing.T) {
	donor := testBus("D", "C1", 220, 220, 600) // already at target, laxity 600
	waiting := testBus("W", "", 100, 220, 90)  // laxity 42 < 60
	p := PlanNormal(testConfig(), swapInput(donor, waiting))
	if len(p.Swaps) != 1 {
		t.Fatalf("expected 1 swap, got %+v", p.Swaps)
	}
	s := p.Swaps[0]
	if s.ChargerID != "C1" || s.OutBusID != "D" || s.InBusID != "W" || s.Reason == "" {
		t.Errorf("unexpected swap: %+v", s)
	}
}

func TestNoSwapWhenAFreeChargerExists(t *testing.T) {
	donor := testBus("D", "C1", 220, 220, 600)
	waiting := testBus("W", "", 100, 220, 90)
	p := PlanNormal(testConfig(), swapInput(donor, waiting, testCharger("C2")))
	if len(p.Swaps) != 0 {
		t.Errorf("a free charger exists, expected no swap: %+v", p.Swaps)
	}
}

func TestNoSwapWhenWaitingBusIsNotUrgent(t *testing.T) {
	donor := testBus("D", "C1", 220, 220, 600)
	waiting := testBus("W", "", 100, 220, 600)
	if p := PlanNormal(testConfig(), swapInput(donor, waiting)); len(p.Swaps) != 0 {
		t.Errorf("unexpected swap: %+v", p.Swaps)
	}
}

func TestNoSwapWhenDonorHasNoSpareLaxity(t *testing.T) {
	donor := testBus("D", "C1", 100, 220, 150) // laxity 102
	waiting := testBus("W", "", 100, 220, 90)  // laxity 42: gap 60 < 120
	if p := PlanNormal(testConfig(), swapInput(donor, waiting)); len(p.Swaps) != 0 {
		t.Errorf("unexpected swap: %+v", p.Swaps)
	}
}

func TestNoSwapWithoutHealthyCharger(t *testing.T) {
	donor := testBus("D", "C1", 220, 220, 600)
	waiting := testBus("W", "", 100, 220, 90)
	in := swapInput(donor, waiting)
	in.Chargers[0].Status = model.ChargerFaulted
	if p := PlanNormal(testConfig(), in); len(p.Swaps) != 0 {
		t.Errorf("unexpected swap: %+v", p.Swaps)
	}
}
```

- [ ] **Step 2: Rodar e ver falhar**

Run: `go test ./internal/planner -run Swap -v`
Expected: FAIL (`TestSwapRecommendedWhenWaitingBusIsUrgent`: expected 1 swap, got []).

- [ ] **Step 3: Implementar**

`internal/planner/swaps.go`:
```go
package planner

import (
	"fmt"
	"sort"

	"github.com/EduardoMilani8/depot-charge-planner/internal/model"
)

// bestCharger returns the healthy charger with the highest max power (ties by ID).
func bestCharger(in Input) (model.Charger, bool) {
	var best model.Charger
	found := false
	for _, c := range in.Chargers {
		if !c.Healthy() {
			continue
		}
		if !found || c.MaxKW > best.MaxKW || (c.MaxKW == best.MaxKW && c.ID < best.ID) {
			best, found = c, true
		}
	}
	return best, found
}

func recommendSwaps(cfg Config, in Input, cands []*candidate, idles []idleBus) []Swap {
	ref, ok := bestCharger(in)
	if !ok || len(idles) == 0 || len(cands) == 0 {
		return nil
	}
	occupied := map[string]bool{}
	for _, cd := range cands {
		occupied[cd.charger.ID] = true
	}
	for _, ib := range idles {
		if ib.bus.ChargerID != "" {
			occupied[ib.bus.ChargerID] = true
		}
	}
	for _, c := range in.Chargers {
		if c.Healthy() && !occupied[c.ID] {
			return nil // a free charger exists: the waiting bus just plugs in there
		}
	}
	type urgent struct {
		bus    model.Bus
		laxity float64
	}
	var urgents []urgent
	for _, ib := range idles {
		cd := newCandidate(cfg, in, ib.bus, ref)
		if cd.need > 0 && !cd.unusable && cd.laxityMin < cfg.SwapUrgentLaxityMin {
			urgents = append(urgents, urgent{ib.bus, cd.laxityMin})
		}
	}
	sort.Slice(urgents, func(i, j int) bool {
		if urgents[i].laxity != urgents[j].laxity {
			return urgents[i].laxity < urgents[j].laxity
		}
		return urgents[i].bus.ID < urgents[j].bus.ID
	})
	donors := append([]*candidate(nil), cands...)
	sort.Slice(donors, func(i, j int) bool {
		if donors[i].laxityMin != donors[j].laxityMin {
			return donors[i].laxityMin > donors[j].laxityMin
		}
		return donors[i].bus.ID < donors[j].bus.ID
	})
	var swaps []Swap
	for i, u := range urgents {
		if i >= len(donors) {
			break
		}
		d := donors[i]
		if d.laxityMin-u.laxity < cfg.SwapDonorGapMin {
			break
		}
		swaps = append(swaps, Swap{
			ChargerID: d.charger.ID,
			OutBusID:  d.bus.ID,
			InBusID:   u.bus.ID,
			Reason: fmt.Sprintf("ônibus %s tem folga de %.0f min e precisa de carregador; ônibus %s tem folga de %.0f min",
				u.bus.ID, u.laxity, d.bus.ID, finiteLaxity(d.laxityMin)),
		})
	}
	return swaps
}
```

Em `internal/planner/normal.go`, dentro de `PlanNormal`, troque:
```go
	sort.Slice(plan.Setpoints, func(i, j int) bool { return plan.Setpoints[i].ChargerID < plan.Setpoints[j].ChargerID })
```
por:
```go
	plan.Swaps = recommendSwaps(cfg, in, cands, idles)
	sort.Slice(plan.Setpoints, func(i, j int) bool { return plan.Setpoints[i].ChargerID < plan.Setpoints[j].ChargerID })
```

- [ ] **Step 4: Rodar e ver passar**

Run: `gofmt -l . && go vet ./... && go test ./internal/planner -v`
Expected: PASS (inclusive os testes das tarefas anteriores).

- [ ] **Step 5: Commit**

```bash
git add internal/planner
git commit -m "feat(planner): swap recommendations for urgent waiting buses"
```


### Task 7: Fachada com degradação graciosa

**Files:**
- Create: `internal/planner/planner.go`
- Test: `internal/planner/planner_test.go`

**Interfaces:**
- Consumes: `ValidateInput`, `PlanNormal`, `PlanSafe`, `Enforce`, `isReliable`, `chargerMap`, `presentBuses`, `Config`.
- Produces: `planner.Planner` com `New(cfg Config) *Planner`, `(*Planner).WithNormal(f NormalFunc) *Planner`, `(*Planner).WithLogger(l *slog.Logger) *Planner`, `(*Planner).Plan(in Input) Plan`; `planner.NormalFunc = func(Config, Input) Plan`. Garantia: o plano devolvido **sempre** passa em `Violations`. Ordem de degradação: normal → último plano válido (dentro de `LastPlanTTLMin`) → perfil seguro; entrada estruturalmente inválida → último plano válido ou todos os carregadores em 0 kW.

**Review Focus pinned here:** IDs duplicados/dois ônibus no mesmo carregador (entrada inválida) e entrada vazia nunca causam pânico.

- [ ] **Step 1: Escrever os testes que falham**

`internal/planner/planner_test.go`:
```go
package planner

import (
	"strings"
	"testing"
	"time"

	"github.com/EduardoMilani8/depot-charge-planner/internal/model"
)

func TestPlannerNormalPath(t *testing.T) {
	in := validInput()
	p := New(testConfig()).Plan(in)
	if p.Layer != LayerNormal || sp(p, "C1") <= 0 {
		t.Errorf("unexpected plan: %+v", p)
	}
	if v := Violations(in, p); len(v) != 0 {
		t.Errorf("violations: %v", v)
	}
}

func TestPlannerPanicFallsBackToLastValidThenSafe(t *testing.T) {
	calls := 0
	pl := New(testConfig()).WithNormal(func(c Config, in Input) Plan {
		calls++
		if calls > 1 {
			panic("boom")
		}
		return PlanNormal(c, in)
	})
	in := validInput()
	first := pl.Plan(in)
	if first.Layer != LayerNormal {
		t.Fatalf("first layer = %v", first.Layer)
	}
	in.Now = 1
	second := pl.Plan(in)
	if second.Layer != LayerLastValid || sp(second, "C1") != sp(first, "C1") {
		t.Errorf("expected last-valid with the same setpoint: %+v", second)
	}
	in.Now = 100 // beyond LastPlanTTLMin
	third := pl.Plan(in)
	if third.Layer != LayerSafe {
		t.Errorf("expected safe layer after TTL, got %v", third.Layer)
	}
	if v := Violations(in, third); len(v) != 0 {
		t.Errorf("violations: %v", v)
	}
}

func TestPlannerTimeoutFallsBackToSafe(t *testing.T) {
	cfg := testConfig()
	cfg.Timeout = 20 * time.Millisecond
	pl := New(cfg).WithNormal(func(c Config, in Input) Plan {
		time.Sleep(300 * time.Millisecond)
		return PlanNormal(c, in)
	})
	p := pl.Plan(validInput())
	if p.Layer != LayerSafe {
		t.Errorf("layer = %v, want safe", p.Layer)
	}
	if !strings.Contains(strings.Join(p.Notes, " "), "timeout") {
		t.Errorf("notes should mention the timeout: %v", p.Notes)
	}
}

func TestPlannerInvalidInputGivesZeroPlan(t *testing.T) {
	in := validInput()
	in.Chargers = append(in.Chargers, testCharger("C1")) // duplicate ID
	p := New(testConfig()).Plan(in)
	if p.Layer != LayerSafe {
		t.Errorf("layer = %v", p.Layer)
	}
	for _, s := range p.Setpoints {
		if s.KW != 0 {
			t.Errorf("invalid input must command 0 kW, got %+v", s)
		}
	}
	if v := Violations(in, p); len(v) != 0 {
		t.Errorf("violations: %v", v)
	}
}

func TestPlannerTwoBusesOnOneChargerDoesNotPanic(t *testing.T) {
	in := validInput()
	in.Buses = append(in.Buses, testBus("B2", "C1", 50, 200, 300))
	p := New(testConfig()).Plan(in)
	for _, s := range p.Setpoints {
		if s.KW != 0 {
			t.Errorf("unexpected power: %+v", s)
		}
	}
}

func TestPlannerEmptyInput(t *testing.T) {
	p := New(testConfig()).Plan(Input{Site: model.Site{LimitKW: 100, StepMin: 1}})
	if len(p.Setpoints) != 0 || p.Layer != LayerNormal {
		t.Errorf("unexpected plan: %+v", p)
	}
}

func TestPlannerMostlyUnreliableSoCUsesSafeProfile(t *testing.T) {
	in := validInput()
	in.Buses[0].SoCAgeMin = 100
	p := New(testConfig()).Plan(in)
	if p.Layer != LayerSafe {
		t.Errorf("layer = %v, want safe", p.Layer)
	}
}

func TestPlannerEnforcesBrokenNormalLayer(t *testing.T) {
	pl := New(testConfig()).WithNormal(func(c Config, in Input) Plan {
		return Plan{Setpoints: []Setpoint{{"C1", 99999}}}
	})
	in := validInput()
	p := pl.Plan(in)
	if v := Violations(in, p); len(v) != 0 {
		t.Errorf("verifier must repair the plan: %v", v)
	}
	if sp(p, "C1") != 150 {
		t.Errorf("setpoint = %v, want clamped to 150", sp(p, "C1"))
	}
}
```

- [ ] **Step 2: Rodar e ver falhar**

Run: `go test ./internal/planner -run Planner -v`
Expected: FAIL (`undefined: New`).

- [ ] **Step 3: Implementar**

`internal/planner/planner.go`:
```go
package planner

import (
	"errors"
	"fmt"
	"io"
	"log/slog"
	"time"

	"github.com/EduardoMilani8/depot-charge-planner/internal/model"
)

// NormalFunc is the layer-1 implementation; replaceable so the simulator can inject faults.
type NormalFunc func(Config, Input) Plan

// Planner wraps the layers with graceful degradation. It is the only stateful
// piece: it remembers the last valid plan. Not safe for concurrent use.
type Planner struct {
	cfg     Config
	normal  NormalFunc
	log     *slog.Logger
	last    Plan
	hasLast bool
	lastAt  model.Minute
}

func New(cfg Config) *Planner {
	return &Planner{cfg: cfg, normal: PlanNormal, log: slog.New(slog.NewTextHandler(io.Discard, nil))}
}

func (p *Planner) WithNormal(f NormalFunc) *Planner   { p.normal = f; return p }
func (p *Planner) WithLogger(l *slog.Logger) *Planner { p.log = l; return p }

// Plan always returns a plan that satisfies every invariant.
func (p *Planner) Plan(in Input) Plan {
	if err := ValidateInput(in); err != nil {
		p.log.Warn("entrada inválida", "minute", in.Now, "err", err)
		return p.fallback(in, "entrada inválida: "+err.Error(), false)
	}
	if tooUnreliable(p.cfg, in) {
		p.log.Warn("leituras de SoC pouco confiáveis; usando perfil seguro", "minute", in.Now)
		plan := PlanSafe(in)
		plan.Notes = append(plan.Notes, "leituras de SoC pouco confiáveis na maioria dos ônibus")
		return p.finish(in, plan)
	}
	plan, err := p.runNormal(in)
	if err != nil {
		p.log.Warn("camada normal falhou", "minute", in.Now, "err", err)
		return p.fallback(in, err.Error(), true)
	}
	plan.Layer = LayerNormal
	plan = p.finish(in, plan)
	p.last, p.hasLast, p.lastAt = plan, true, in.Now
	return plan
}

func (p *Planner) finish(in Input, plan Plan) Plan {
	out := Enforce(in, plan)
	if len(out.Notes) > len(plan.Notes) {
		p.log.Warn("verificador corrigiu o plano", "minute", in.Now, "layer", plan.Layer.String())
	}
	return out
}

func (p *Planner) fallback(in Input, reason string, inputValid bool) Plan {
	if p.hasLast && in.Now >= p.lastAt && in.Now-p.lastAt <= p.cfg.LastPlanTTLMin {
		plan := p.last
		plan.Layer = LayerLastValid
		plan.Swaps = nil
		plan.Notes = append(append([]string(nil), plan.Notes...), "usando o último plano válido: "+reason)
		return p.finish(in, plan)
	}
	var plan Plan
	if inputValid {
		plan = PlanSafe(in)
	} else {
		plan = zeroPlan(in)
	}
	plan.Notes = append(plan.Notes, "fallback: "+reason)
	return p.finish(in, plan)
}

func (p *Planner) runNormal(in Input) (Plan, error) {
	type result struct {
		plan Plan
		err  error
	}
	cfg, normal := p.cfg, p.normal
	ch := make(chan result, 1)
	go func() {
		defer func() {
			if r := recover(); r != nil {
				ch <- result{err: fmt.Errorf("panic: %v", r)}
			}
		}()
		ch <- result{plan: normal(cfg, in)}
	}()
	select {
	case r := <-ch:
		return r.plan, r.err
	case <-time.After(cfg.Timeout):
		// The abandoned goroutine only reads its inputs and finishes on its own.
		return Plan{}, errors.New("timeout da camada normal")
	}
}

// tooUnreliable reports whether most connected buses have untrustworthy SoC data.
func tooUnreliable(cfg Config, in Input) bool {
	chargers := chargerMap(in)
	total, bad := 0, 0
	for _, b := range presentBuses(in) {
		if c, ok := chargers[b.ChargerID]; ok && c.Healthy() {
			total++
			if !isReliable(cfg, b) {
				bad++
			}
		}
	}
	return total > 0 && float64(bad)/float64(total) > cfg.MaxUnreliableFrac
}

// zeroPlan commands 0 kW on every (deduplicated) charger; used for broken input.
func zeroPlan(in Input) Plan {
	p := Plan{Layer: LayerSafe, Notes: []string{"entrada inválida: todos os carregadores em 0 kW"}}
	seen := map[string]bool{}
	for _, c := range in.Chargers {
		if seen[c.ID] {
			continue
		}
		seen[c.ID] = true
		p.Setpoints = append(p.Setpoints, Setpoint{ChargerID: c.ID})
	}
	return p
}
```

- [ ] **Step 4: Rodar e ver passar**

Run: `gofmt -l . && go vet ./... && go test -race ./internal/planner -v`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/planner
git commit -m "feat(planner): facade with timeout, panic recovery and graceful degradation"
```

---

### Task 8: Núcleo do simulador

**Files:**
- Create: `internal/gateway/gateway.go`
- Create: `internal/sim/scenario.go`, `internal/sim/metrics.go`, `internal/sim/world.go`, `internal/sim/run.go`, `internal/sim/controllers.go`
- Test: `internal/sim/helpers_test.go`, `internal/sim/run_test.go`, `internal/sim/metrics_test.go`

**Interfaces:**
- Consumes: pacotes `model` e `planner` (`Input`, `Plan`, `Setpoint`, `Swap`, `New`, `PlanNormal`, `PlanSafe`, `Enforce`, `AvailableKW`, `MaxGridKW`).
- Produces:
  - `gateway.Gateway` (`Chargers() []model.Charger`, `SetPower(chargerID string, kw float64) error`).
  - `sim.Fault{Kind FaultKind; Target string; From, To model.Minute; Value float64}` com `Active(t int) bool`, constantes `FaultChargerFail`, `FaultChargerOffline`, `FaultLimitDrop`, `FaultSoCNoise`, `FaultSoCBias`, `FaultSoCFreeze`, `FaultSoCMissing`, `FaultConsumption`, `FaultLateArrival`, `FaultEarlyDeparture`, `FaultPlannerPanic`, `FaultPlannerSlow`, e `forever`.
  - `sim.Tariff{PeakFromMin, PeakToMin int; PeakPrice, OffPeakPrice float64}` com `PriceAt(clockMin int) float64`.
  - `sim.BusSpec{Bus model.Bus; TrueTargetKWh float64}`.
  - `sim.Scenario{Name string; Seed int64; StartClockMin int; Horizon model.Minute; BaseLimitKW float64; Chargers []model.Charger; Buses []BusSpec; Faults []Fault; Tariff Tariff; FollowSwaps bool}`.
  - `sim.Metrics{Buses, Ready int; ReadyPct, ShortfallKWh, PeakKW float64; PlanViolations, OvershootMin int; EnergyKWh, CostBRL float64; PlanChanges int; LayerTicks map[string]int; PlanP99Micros int64}` e `sim.Aggregate([]Metrics) Metrics`.
  - `sim.Controller` (`Plan(in planner.Input) planner.Plan`), `sim.NewFIFO()`, `sim.NewEDF()`, `sim.NewSafeOnly()`, `sim.NewPlannerController(cfg planner.Config, sc Scenario) Controller`.
  - `sim.Recorder` (`Record(minute int, in planner.Input, p planner.Plan) error`) e `sim.Run(sc Scenario, ctrl Controller, rec Recorder) Metrics` (`rec` pode ser `nil`).

Modelo de tempo de um passo `t`: (1) o mundo aplica chegadas/saídas/falhas; (2) o planejador vê só a **observação**; (3) os comandos são guardados; (4) a física usa os comandos do **passo anterior** (latência de 1 passo); (5) mede-se pico, energia e custo.

- [ ] **Step 1: Escrever os testes que falham**

`internal/sim/helpers_test.go`:
```go
package sim

import (
	"github.com/EduardoMilani8/depot-charge-planner/internal/model"
	"github.com/EduardoMilani8/depot-charge-planner/internal/planner"
)

func baseScenario() Scenario {
	return Scenario{
		Name: "base", Seed: 1, StartClockMin: 1080, Horizon: 400, BaseLimitKW: 500,
		Chargers: []model.Charger{{ID: "C1", MaxKW: 150, MinKW: 5, Efficiency: 0.95, Status: model.ChargerOK}},
		Buses: []BusSpec{{Bus: model.Bus{
			ID: "B1", CapacityKWh: 300, SoCKWh: 50, SoCConfidence: 1, TargetKWh: 200,
			ArrivalMin: 0, DepartureMin: 300, MaxBatteryKW: 150,
		}}},
		Tariff:      Tariff{PeakFromMin: 1080, PeakToMin: 1260, PeakPrice: 2.7, OffPeakPrice: 0.9},
		FollowSwaps: true,
	}
}

// stubController always commands the same power on C1.
type stubController struct{ kw float64 }

func (s stubController) Plan(in planner.Input) planner.Plan {
	return planner.Plan{Setpoints: []planner.Setpoint{{ChargerID: "C1", KW: s.kw}}}
}

func plannerCtrl(sc Scenario) Controller {
	cfg := planner.DefaultConfig()
	return NewPlannerController(cfg, sc)
}
```

`internal/sim/run_test.go`:
```go
package sim

import (
	"reflect"
	"testing"

	"github.com/EduardoMilani8/depot-charge-planner/internal/model"
)

func TestRunSingleBusReady(t *testing.T) {
	sc := baseScenario()
	m := Run(sc, plannerCtrl(sc), nil)
	if m.Ready != 1 || m.ReadyPct != 100 {
		t.Errorf("bus should leave ready: %+v", m)
	}
	if m.PlanViolations != 0 || m.OvershootMin != 0 {
		t.Errorf("no violations expected: %+v", m)
	}
	if m.EnergyKWh < 150 {
		t.Errorf("energy delivered %.1f kWh is below the 150 kWh the battery gained", m.EnergyKWh)
	}
	if m.CostBRL <= 0 || m.PeakKW <= 0 {
		t.Errorf("cost/peak not measured: %+v", m)
	}
}

func TestRunFIFOReady(t *testing.T) {
	if m := Run(baseScenario(), NewFIFO(), nil); m.Ready != 1 {
		t.Errorf("FIFO should also finish: %+v", m)
	}
}

func TestRunEDFAndSafeOnlyReady(t *testing.T) {
	for name, c := range map[string]Controller{"edf": NewEDF(), "safe": NewSafeOnly()} {
		if m := Run(baseScenario(), c, nil); m.Ready != 1 {
			t.Errorf("%s: %+v", name, m)
		}
	}
}

func TestRunDeterministic(t *testing.T) {
	sc := baseScenario()
	a := Run(sc, plannerCtrl(sc), nil)
	b := Run(sc, plannerCtrl(sc), nil)
	a.PlanP99Micros, b.PlanP99Micros = 0, 0
	if !reflect.DeepEqual(a, b) {
		t.Errorf("same seed must give identical metrics:\n%+v\n%+v", a, b)
	}
}

func TestRunCountsPlanViolations(t *testing.T) {
	m := Run(baseScenario(), stubController{kw: 1000}, nil)
	if m.PlanViolations == 0 {
		t.Error("commanding 1000 kW on a 500 kW site must be counted")
	}
	if m.PeakKW > 150+1e-6 {
		t.Errorf("physical power %.1f kW exceeds the charger max", m.PeakKW)
	}
}

func TestRunNoPowerMeansShortfall(t *testing.T) {
	m := Run(baseScenario(), stubController{kw: 0}, nil)
	if m.Ready != 0 || m.ShortfallKWh < 149 || m.ShortfallKWh > 151 {
		t.Errorf("expected a 150 kWh shortfall: %+v", m)
	}
}

func TestRunWaitingBusTakesFreedCharger(t *testing.T) {
	sc := baseScenario()
	sc.Buses[0].Bus.DepartureMin = 100
	sc.Buses = append(sc.Buses, BusSpec{Bus: model.Bus{
		ID: "B2", CapacityKWh: 300, SoCKWh: 50, SoCConfidence: 1, TargetKWh: 200,
		ArrivalMin: 0, DepartureMin: 400, MaxBatteryKW: 150,
	}})
	m := Run(sc, plannerCtrl(sc), nil)
	if m.Ready != 2 {
		t.Errorf("both buses should be served in turn: %+v", m)
	}
}

func TestTariffPriceAt(t *testing.T) {
	tf := Tariff{PeakFromMin: 1080, PeakToMin: 1260, PeakPrice: 3, OffPeakPrice: 1}
	cases := map[int]float64{0: 1, 1079: 1, 1080: 3, 1259: 3, 1260: 1, 1440 + 1100: 3}
	for clock, want := range cases {
		if got := tf.PriceAt(clock); got != want {
			t.Errorf("PriceAt(%d) = %v, want %v", clock, got, want)
		}
	}
}
```

`internal/sim/metrics_test.go`:
```go
package sim

import (
	"testing"
	"time"
)

func TestP99Micros(t *testing.T) {
	var d []time.Duration
	for i := 1; i <= 100; i++ {
		d = append(d, time.Duration(i)*time.Microsecond)
	}
	if got := p99Micros(d); got != 99 {
		t.Errorf("p99 = %d, want 99", got)
	}
	if p99Micros(nil) != 0 {
		t.Error("empty must be 0")
	}
}

func TestAggregate(t *testing.T) {
	a := Aggregate([]Metrics{
		{Buses: 10, Ready: 10, ReadyPct: 100, PeakKW: 100, PlanViolations: 1, PlanChanges: 10, PlanP99Micros: 5},
		{Buses: 10, Ready: 5, ReadyPct: 50, PeakKW: 200, PlanViolations: 2, PlanChanges: 20, PlanP99Micros: 9},
	})
	if a.ReadyPct != 75 || a.PeakKW != 150 || a.PlanViolations != 3 || a.PlanChanges != 15 || a.PlanP99Micros != 9 || a.Ready != 15 {
		t.Errorf("unexpected aggregate: %+v", a)
	}
	if z := Aggregate(nil); z.Buses != 0 || z.ReadyPct != 0 {
		t.Error("empty aggregate must be zero")
	}
}
```

- [ ] **Step 2: Rodar e ver falhar**

Run: `go test ./internal/sim -v`
Expected: FAIL (build failed: `undefined: Scenario`, ...).

- [ ] **Step 3: Implementar a interface do gateway e os tipos do cenário**

`internal/gateway/gateway.go`:
```go
// Package gateway is the boundary between the planner and physical (or simulated) chargers.
package gateway

import "github.com/EduardoMilani8/depot-charge-planner/internal/model"

type Gateway interface {
	// Chargers returns every charger as the controller observes it.
	Chargers() []model.Charger
	// SetPower commands a charger's grid-side power in kW; it takes effect on the next step.
	SetPower(chargerID string, kw float64) error
}
```

`internal/sim/scenario.go`:
```go
// Package sim is a deterministic discrete-time simulator of a bus depot.
package sim

import (
	"math"

	"github.com/EduardoMilani8/depot-charge-planner/internal/model"
)

type FaultKind string

const (
	FaultChargerFail    FaultKind = "charger_fail"     // Target: charger ID; delivers nothing
	FaultChargerOffline FaultKind = "charger_offline"  // Target: charger ID; keeps last power, cannot be commanded
	FaultLimitDrop      FaultKind = "limit_drop"       // Value: factor applied to the site limit
	FaultSoCNoise       FaultKind = "soc_noise"        // Target: bus ID or "*"; Value: std dev in kWh
	FaultSoCBias        FaultKind = "soc_bias"         // Target: bus ID or "*"; Value: kWh added to readings
	FaultSoCFreeze      FaultKind = "soc_freeze"       // Target: bus ID or "*"; reading stops updating
	FaultSoCMissing     FaultKind = "soc_missing"      // Target: bus ID or "*"; no usable reading
	FaultConsumption    FaultKind = "consumption_over" // Target: bus ID; Value: extra kWh needed at departure
	FaultLateArrival    FaultKind = "late_arrival"     // Target: bus ID; Value: minutes of delay
	FaultEarlyDeparture FaultKind = "early_departure"  // Target: bus ID; From: when announced; Value: new departure minute
	FaultPlannerPanic   FaultKind = "planner_panic"    // the normal layer panics while active
	FaultPlannerSlow    FaultKind = "planner_slow"     // the normal layer exceeds its timeout while active
)

// forever is a To value for permanent faults.
const forever = math.MaxInt32

// Fault is active during the window [From, To).
type Fault struct {
	Kind     FaultKind
	Target   string
	From, To model.Minute
	Value    float64
}

func (f Fault) Active(t int) bool { return t >= f.From && t < f.To }

// Tariff has a peak window expressed in minutes of the day.
type Tariff struct {
	PeakFromMin, PeakToMin   int
	PeakPrice, OffPeakPrice float64
}

func (t Tariff) PriceAt(clockMin int) float64 {
	m := ((clockMin % 1440) + 1440) % 1440
	if m >= t.PeakFromMin && m < t.PeakToMin {
		return t.PeakPrice
	}
	return t.OffPeakPrice
}

// BusSpec is a bus plus the truth the planner does not know.
type BusSpec struct {
	Bus           model.Bus // SoCKWh is the true initial SoC; TargetKWh is the forecast the planner sees
	TrueTargetKWh float64   // energy actually needed at departure; 0 means equal to the forecast
}

type Scenario struct {
	Name          string
	Seed          int64
	StartClockMin int // minute of the day at scenario minute 0
	Horizon       model.Minute
	BaseLimitKW   float64
	Chargers      []model.Charger
	Buses         []BusSpec
	Faults        []Fault
	Tariff        Tariff
	FollowSwaps   bool // operators execute the planner's swap recommendations
}
```

`internal/sim/metrics.go`:
```go
package sim

import (
	"math"
	"sort"
	"time"
)

type Metrics struct {
	Buses          int            `json:"buses"`
	Ready          int            `json:"ready"`
	ReadyPct       float64        `json:"ready_pct"`
	ShortfallKWh   float64        `json:"shortfall_kwh"`
	PeakKW         float64        `json:"peak_kw"`
	PlanViolations int            `json:"plan_violations"` // commanded power above the limit (must be 0)
	OvershootMin   int            `json:"overshoot_min"`   // minutes of physical power above the limit
	EnergyKWh      float64        `json:"energy_kwh"`
	CostBRL        float64        `json:"cost_brl"`
	PlanChanges    int            `json:"plan_changes"`
	LayerTicks     map[string]int `json:"layer_ticks"`
	PlanP99Micros  int64          `json:"plan_p99_micros"`
}

func p99Micros(d []time.Duration) int64 {
	if len(d) == 0 {
		return 0
	}
	s := append([]time.Duration(nil), d...)
	sort.Slice(s, func(i, j int) bool { return s[i] < s[j] })
	idx := int(math.Ceil(0.99*float64(len(s)))) - 1
	if idx < 0 {
		idx = 0
	}
	return s[idx].Microseconds()
}

// Aggregate averages rates and costs, sums counts of violations, and keeps the worst p99.
func Aggregate(ms []Metrics) Metrics {
	var a Metrics
	if len(ms) == 0 {
		return a
	}
	n := float64(len(ms))
	for _, m := range ms {
		a.Buses += m.Buses
		a.Ready += m.Ready
		a.ReadyPct += m.ReadyPct / n
		a.ShortfallKWh += m.ShortfallKWh / n
		a.PeakKW += m.PeakKW / n
		a.PlanViolations += m.PlanViolations
		a.OvershootMin += m.OvershootMin
		a.EnergyKWh += m.EnergyKWh / n
		a.CostBRL += m.CostBRL / n
		a.PlanChanges += m.PlanChanges
		if m.PlanP99Micros > a.PlanP99Micros {
			a.PlanP99Micros = m.PlanP99Micros
		}
	}
	a.PlanChanges /= len(ms)
	return a
}
```

- [ ] **Step 4: Implementar o mundo (verdade, física, observação)**

`internal/sim/world.go`:
```go
package sim

import (
	"fmt"
	"math"
	"math/rand"
	"sort"

	"github.com/EduardoMilani8/depot-charge-planner/internal/gateway"
	"github.com/EduardoMilani8/depot-charge-planner/internal/model"
	"github.com/EduardoMilani8/depot-charge-planner/internal/planner"
)

const (
	stepHours       = 1.0 / 60.0
	swapDurationMin = 5
)

var _ gateway.Gateway = (*World)(nil)

type busState struct {
	spec      BusSpec
	soc       float64 // true SoC
	target    float64 // true energy required at departure
	arrival   int
	departure int
	chargerID string
	present   bool
	departed  bool
	busyUntil int
	lastGood  float64 // last observed value, used by the freeze fault
	lastGoodT int
}

// World holds the truth. The planner only ever sees observe() and Chargers().
type World struct {
	sc        Scenario
	rng       *rand.Rand
	t         int
	buses     []*busState // sorted by ID
	chargers  []model.Charger
	cmd       map[string]float64 // commands issued this step
	applied   map[string]float64 // power in effect (commands of the previous step)
	ready     int
	shortfall float64
}

func newWorld(sc Scenario) *World {
	w := &World{
		sc:      sc,
		rng:     rand.New(rand.NewSource(sc.Seed)),
		cmd:     map[string]float64{},
		applied: map[string]float64{},
	}
	w.chargers = append([]model.Charger(nil), sc.Chargers...)
	sort.Slice(w.chargers, func(i, j int) bool { return w.chargers[i].ID < w.chargers[j].ID })
	specs := append([]BusSpec(nil), sc.Buses...)
	sort.Slice(specs, func(i, j int) bool { return specs[i].Bus.ID < specs[j].Bus.ID })
	for _, s := range specs {
		bs := &busState{spec: s, soc: s.Bus.SoCKWh, target: s.Bus.TargetKWh,
			arrival: s.Bus.ArrivalMin, departure: s.Bus.DepartureMin}
		if s.TrueTargetKWh > 0 {
			bs.target = s.TrueTargetKWh
		}
		for _, f := range sc.Faults {
			if f.Target != s.Bus.ID {
				continue
			}
			switch f.Kind {
			case FaultLateArrival:
				bs.arrival += int(f.Value)
			case FaultConsumption:
				bs.target += f.Value
			}
		}
		bs.target = math.Min(bs.target, s.Bus.CapacityKWh)
		w.buses = append(w.buses, bs)
	}
	return w
}

func (w *World) charger(id string) *model.Charger {
	for i := range w.chargers {
		if w.chargers[i].ID == id {
			return &w.chargers[i]
		}
	}
	return nil
}

func (w *World) statusOf(id string) model.ChargerStatus {
	if c := w.charger(id); c != nil {
		return c.Status
	}
	return model.ChargerFaulted
}

func (w *World) find(id string) *busState {
	for _, bs := range w.buses {
		if bs.spec.Bus.ID == id {
			return bs
		}
	}
	return nil
}

// Chargers implements gateway.Gateway: the observed state (offline chargers are
// visible as offline, with the power they keep drawing).
func (w *World) Chargers() []model.Charger {
	out := make([]model.Charger, len(w.chargers))
	for i, c := range w.chargers {
		c.LastCommandedKW = w.applied[c.ID]
		out[i] = c
	}
	return out
}

// SetPower implements gateway.Gateway.
func (w *World) SetPower(id string, kw float64) error {
	c := w.charger(id)
	if c == nil {
		return fmt.Errorf("carregador desconhecido: %s", id)
	}
	if c.Status != model.ChargerOK {
		return fmt.Errorf("carregador %s indisponível", id)
	}
	if math.IsNaN(kw) || math.IsInf(kw, 0) || kw < 0 {
		return fmt.Errorf("potência inválida: %v", kw)
	}
	w.cmd[id] = kw
	return nil
}

func (w *World) limitAt(t int) float64 {
	f := 1.0
	for _, x := range w.sc.Faults {
		if x.Kind == FaultLimitDrop && x.Active(t) && x.Value < f {
			f = x.Value
		}
	}
	return w.sc.BaseLimitKW * f
}

// beginTick applies faults, departures, arrivals and operator behaviour.
func (w *World) beginTick(t int) {
	w.t = t
	for i := range w.chargers {
		w.chargers[i].Status = model.ChargerOK
		for _, f := range w.sc.Faults {
			if f.Target != w.chargers[i].ID || !f.Active(t) {
				continue
			}
			switch f.Kind {
			case FaultChargerFail:
				w.chargers[i].Status = model.ChargerFaulted
			case FaultChargerOffline:
				if w.chargers[i].Status == model.ChargerOK {
					w.chargers[i].Status = model.ChargerOffline
				}
			}
		}
	}
	for _, f := range w.sc.Faults {
		if f.Kind == FaultEarlyDeparture && f.From == t {
			if bs := w.find(f.Target); bs != nil {
				bs.departure = int(f.Value)
			}
		}
	}
	for _, bs := range w.buses {
		if bs.present && !bs.departed && t >= bs.departure {
			w.depart(bs)
		}
	}
	for _, bs := range w.buses {
		if !bs.present && !bs.departed && t >= bs.arrival {
			bs.present = true
			bs.lastGood, bs.lastGoodT = bs.soc, t
		}
	}
	w.connectWaiting()
}

func (w *World) depart(bs *busState) {
	bs.departed = true
	bs.chargerID = ""
	if bs.soc >= bs.target-1e-6 {
		w.ready++
	} else {
		w.shortfall += bs.target - bs.soc
	}
}

// connectWaiting plugs waiting buses (and buses on faulted chargers) into free healthy chargers.
func (w *World) connectWaiting() {
	occupied := map[string]bool{}
	for _, bs := range w.buses {
		if bs.present && !bs.departed && bs.chargerID != "" {
			occupied[bs.chargerID] = true
		}
	}
	var waiting []*busState
	for _, bs := range w.buses {
		if !bs.present || bs.departed {
			continue
		}
		if bs.chargerID == "" || w.statusOf(bs.chargerID) == model.ChargerFaulted {
			waiting = append(waiting, bs)
		}
	}
	sort.SliceStable(waiting, func(i, j int) bool { return waiting[i].arrival < waiting[j].arrival })
	for _, bs := range waiting {
		for i := range w.chargers {
			c := &w.chargers[i]
			if c.Status == model.ChargerOK && !occupied[c.ID] {
				occupied[c.ID] = true
				bs.chargerID = c.ID
				break
			}
		}
	}
}

// sense returns what the SoC sensor reports for a bus.
func (w *World) sense(bs *busState) (soc float64, ageMin int, conf float64) {
	soc = bs.soc
	frozen, missing := false, false
	for _, f := range w.sc.Faults {
		if !f.Active(w.t) || (f.Target != "*" && f.Target != bs.spec.Bus.ID) {
			continue
		}
		switch f.Kind {
		case FaultSoCNoise:
			soc += w.rng.NormFloat64() * f.Value
		case FaultSoCBias:
			soc += f.Value
		case FaultSoCFreeze:
			frozen = true
		case FaultSoCMissing:
			missing = true
		}
	}
	switch {
	case missing:
		return 0, 10000, 0
	case frozen:
		return bs.lastGood, w.t - bs.lastGoodT, 1
	}
	bs.lastGood, bs.lastGoodT = soc, w.t
	return soc, 0, 1
}

// observe builds the planner input from sensors and schedules (never from the truth).
func (w *World) observe() planner.Input {
	in := planner.Input{
		Now:      w.t,
		Site:     model.Site{LimitKW: w.limitAt(w.t), StepMin: 1},
		Chargers: w.Chargers(),
	}
	for _, bs := range w.buses {
		if !bs.present || bs.departed {
			continue
		}
		b := bs.spec.Bus
		b.ChargerID = bs.chargerID
		b.ArrivalMin = bs.arrival
		b.DepartureMin = bs.departure
		b.SoCKWh, b.SoCAgeMin, b.SoCConfidence = w.sense(bs)
		in.Buses = append(in.Buses, b)
	}
	return in
}

// commandedTotal is the power the commands imply: this step's commands for
// controllable chargers plus what offline chargers keep drawing.
func (w *World) commandedTotal() float64 {
	total := 0.0
	for _, c := range w.chargers {
		if c.Status == model.ChargerOffline {
			total += w.applied[c.ID]
		} else {
			total += w.cmd[c.ID]
		}
	}
	return total
}

// applySwaps executes recommended swaps (when operators follow them).
func (w *World) applySwaps(swaps []planner.Swap) {
	for _, s := range swaps {
		out, in := w.find(s.OutBusID), w.find(s.InBusID)
		if out == nil || in == nil || !out.present || out.departed || !in.present || in.departed {
			continue
		}
		if out.chargerID != s.ChargerID || w.statusOf(s.ChargerID) != model.ChargerOK {
			continue
		}
		prev := in.chargerID
		in.chargerID = s.ChargerID
		out.chargerID = ""
		if prev != "" && w.statusOf(prev) == model.ChargerOK {
			out.chargerID = prev
		}
		in.busyUntil = w.t + swapDurationMin
		out.busyUntil = w.t + swapDurationMin
	}
}

// advance runs the physics for one step using the power in effect (previous commands).
func (w *World) advance(m *Metrics) {
	total := 0.0
	for _, bs := range w.buses {
		if !bs.present || bs.departed || bs.chargerID == "" || w.t < bs.busyUntil {
			continue
		}
		c := w.charger(bs.chargerID)
		if c == nil || c.Status == model.ChargerFaulted {
			continue
		}
		gridKW := math.Min(w.applied[c.ID], c.MaxKW)
		if gridKW < c.MinKW {
			gridKW = 0
		}
		b := bs.spec.Bus
		battKW := math.Min(gridKW*c.Efficiency, b.MaxBatteryKW*model.TaperFactor(bs.soc, b.CapacityKWh))
		e := math.Min(battKW*stepHours, b.CapacityKWh-bs.soc)
		if e <= 0 {
			continue
		}
		bs.soc += e
		grid := e / c.Efficiency
		total += grid / stepHours
		m.EnergyKWh += grid
		m.CostBRL += grid * w.sc.Tariff.PriceAt(w.sc.StartClockMin+w.t)
	}
	if total > m.PeakKW {
		m.PeakKW = total
	}
	if total > w.limitAt(w.t)+1e-6 {
		m.OvershootMin++
	}
}

// endTick makes this step's commands the power in effect for the next step.
func (w *World) endTick() {
	for _, c := range w.chargers {
		switch c.Status {
		case model.ChargerOK:
			w.applied[c.ID] = w.cmd[c.ID]
		case model.ChargerFaulted:
			w.applied[c.ID] = 0
		} // offline chargers keep drawing their last power
	}
}

func (w *World) finish(m *Metrics) {
	for _, bs := range w.buses {
		if bs.present && !bs.departed {
			w.depart(bs)
		}
	}
	m.Ready = w.ready
	m.ShortfallKWh = w.shortfall
	if m.Buses > 0 {
		m.ReadyPct = 100 * float64(m.Ready) / float64(m.Buses)
	}
}
```

- [ ] **Step 5: Implementar o laço e os controladores**

`internal/sim/run.go`:
```go
package sim

import (
	"math"
	"time"

	"github.com/EduardoMilani8/depot-charge-planner/internal/planner"
)

// Recorder receives every decision (input and plan); see the decision log.
type Recorder interface {
	Record(minute int, in planner.Input, p planner.Plan) error
}

// Run simulates the scenario with the given controller. rec may be nil.
func Run(sc Scenario, ctrl Controller, rec Recorder) Metrics {
	w := newWorld(sc)
	m := Metrics{Buses: len(sc.Buses), LayerTicks: map[string]int{}}
	durations := make([]time.Duration, 0, sc.Horizon+1)
	prev := map[string]float64{}
	for t := 0; t <= sc.Horizon; t++ {
		w.beginTick(t)
		in := w.observe()
		start := time.Now()
		plan := ctrl.Plan(in)
		durations = append(durations, time.Since(start))
		m.LayerTicks[plan.Layer.String()]++
		if rec != nil {
			_ = rec.Record(t, in, plan)
		}
		w.cmd = map[string]float64{}
		for _, s := range plan.Setpoints {
			_ = w.SetPower(s.ChargerID, s.KW)
		}
		if w.commandedTotal() > w.limitAt(t)+1e-6 {
			m.PlanViolations++
		}
		if changed(prev, w.cmd) {
			m.PlanChanges++
		}
		prev = w.cmd
		if sc.FollowSwaps {
			w.applySwaps(plan.Swaps)
		}
		w.advance(&m)
		w.endTick()
	}
	w.finish(&m)
	m.PlanP99Micros = p99Micros(durations)
	return m
}

func changed(a, b map[string]float64) bool {
	for k, v := range a {
		if math.Abs(v-b[k]) > 1e-6 {
			return true
		}
	}
	for k, v := range b {
		if math.Abs(v-a[k]) > 1e-6 {
			return true
		}
	}
	return false
}
```

`internal/sim/controllers.go`:
```go
package sim

import (
	"math"
	"sort"
	"time"

	"github.com/EduardoMilani8/depot-charge-planner/internal/model"
	"github.com/EduardoMilani8/depot-charge-planner/internal/planner"
)

// Controller is anything that turns an observation into a plan.
type Controller interface {
	Plan(in planner.Input) planner.Plan
}

// greedyController serves buses in a fixed order at full power until the power budget runs out.
type greedyController struct {
	less func(a, b model.Bus) bool
}

// NewFIFO serves buses by arrival time (charge on arrival, capped by the site limit).
func NewFIFO() Controller {
	return greedyController{less: func(a, b model.Bus) bool {
		if a.ArrivalMin != b.ArrivalMin {
			return a.ArrivalMin < b.ArrivalMin
		}
		return a.ID < b.ID
	}}
}

// NewEDF serves buses by earliest departure first.
func NewEDF() Controller {
	return greedyController{less: func(a, b model.Bus) bool {
		if a.DepartureMin != b.DepartureMin {
			return a.DepartureMin < b.DepartureMin
		}
		return a.ID < b.ID
	}}
}

func (g greedyController) Plan(in planner.Input) planner.Plan {
	chargers := map[string]model.Charger{}
	for _, c := range in.Chargers {
		chargers[c.ID] = c
	}
	var buses []model.Bus
	for _, b := range in.Buses {
		c, ok := chargers[b.ChargerID]
		if ok && c.Healthy() && b.ArrivalMin <= in.Now && b.SoCKWh < b.TargetKWh {
			buses = append(buses, b)
		}
	}
	sort.SliceStable(buses, func(i, j int) bool { return g.less(buses[i], buses[j]) })
	budget := planner.AvailableKW(in)
	var p planner.Plan
	for _, b := range buses {
		c := chargers[b.ChargerID]
		kw := math.Min(planner.MaxGridKW(c, b), budget)
		if kw < c.MinKW {
			kw = 0
		}
		p.Setpoints = append(p.Setpoints, planner.Setpoint{ChargerID: c.ID, KW: kw})
		budget -= kw
	}
	return p
}

type safeController struct{}

// NewSafeOnly runs only layer 0 (equal split), to measure the fallback on its own.
func NewSafeOnly() Controller { return safeController{} }

func (safeController) Plan(in planner.Input) planner.Plan {
	return planner.Enforce(in, planner.PlanSafe(in))
}

// NewPlannerController wraps the real planner, injecting the scenario's planner faults
// (panic or slowness) into its normal layer.
func NewPlannerController(cfg planner.Config, sc Scenario) Controller {
	normal := func(c planner.Config, in planner.Input) planner.Plan {
		for _, f := range sc.Faults {
			if !f.Active(in.Now) {
				continue
			}
			switch f.Kind {
			case FaultPlannerPanic:
				panic("injected planner fault")
			case FaultPlannerSlow:
				time.Sleep(2 * c.Timeout)
			}
		}
		return planner.PlanNormal(c, in)
	}
	return planner.New(cfg).WithNormal(normal)
}
```

- [ ] **Step 6: Rodar e ver passar**

Run: `gofmt -l . && go vet ./... && go test -race ./...`
Expected: PASS em todos os pacotes. Se `TestRunWaitingBusTakesFreedCharger` falhar, imprima `m` e verifique se o ônibus B1 chegou a 210 kWh antes do minuto 100 (necessário ~70 min a 150 kW) antes de mexer no simulador.

- [ ] **Step 7: Commit**

```bash
git add internal
git commit -m "feat(sim): deterministic simulator core with truth/observation split and baselines"
```

> **Status do plano:** PARCIAL. Tarefas 1 a 8 escritas. Faltam as tarefas 9 a 13 (testes de falhas, gerador e propriedades, registro de decisões, CLI, CI/fuzz/golden).
