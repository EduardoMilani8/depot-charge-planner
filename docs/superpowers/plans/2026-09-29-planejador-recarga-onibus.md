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


> **Status do plano:** PARCIAL. Tarefas 1 a 3 escritas. Faltam as tarefas 4 a 13 (verificador de invariantes, camada 1, rodízio, fachada com degradação, simulador, falhas, gerador, registro de decisões, CLI e CI/fuzz/golden).
