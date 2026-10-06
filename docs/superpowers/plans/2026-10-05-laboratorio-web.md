# Laboratório web de simulação — Plano de implementação

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Uma ferramenta local (`go run ./cmd/lab`) com interface web para configurar cenários do simulador, comparar o planejador com os baselines e abrir uma execução minuto a minuto (potência, linha do tempo de ônibus e carregadores, decisões e motivos).

**Architecture:** O pacote `internal/sim` ganha um `Trace` (a "verdade" do simulador minuto a minuto), `RunTraced`, uma comparação reutilizável (`Compare`) e uma validação compartilhada com o `simrun`. Um servidor HTTP em `internal/lab` expõe três rotas JSON e serve a interface embutida (`web/static`, via `go:embed`): HTML, CSS e módulos JavaScript simples, com gráficos SVG feitos à mão. Nada é gravado em disco: uma execução é recalculada a partir de parâmetros + semente + controlador (o simulador é determinístico).

**Tech Stack:** Go 1.22 (só biblioteca padrão), HTML/CSS/JavaScript ES modules sem dependências, `node --test` apenas para testar funções puras no desenvolvimento e no CI.

**Spec:** `docs/superpowers/specs/2026-10-05-laboratorio-web-design.md` (aprovada em 2026-10-05). Contexto do projeto base: `docs/superpowers/specs/2026-09-29-planejador-recarga-onibus-design.md` e o `README.md`.

## Global Constraints

- Núcleo Go só com a biblioteca padrão; `go.mod` continua em `go 1.22`, sem dependências novas.
- Frontend sem dependências externas e sem rede externa (nada de CDN, fontes remotas ou bibliotecas copiadas); gráficos em SVG feitos à mão.
- Node não é necessário para usar a ferramenta; `node --test` serve só para desenvolvimento e CI.
- Interface e mensagens de erro da API em português; tema claro e escuro conforme `prefers-color-scheme`.
- O servidor escuta somente em loopback (`127.0.0.1`, `localhost` ou `::1`); rejeita `Host` não local; corpo de requisição até 64 KB; sem CORS; no máximo 2 execuções simultâneas (a terceira recebe 503).
- Nada é gravado em disco pelo laboratório.
- O planejador (`internal/planner`) **não é alterado**; `sim.Run` continua devolvendo exatamente as mesmas métricas (os números do README não mudam).
- Limites de validação (compartilhados com o `simrun`): ônibus e carregadores até 10000, sementes até 1000, limite finito em (0, 1e7] kW, idade de leitura de 0 a 10000 minutos.
- Limites de trabalho do laboratório: ônibus × sementes ≤ 20000 em `/api/compare`; ônibus ≤ 1000 em `/api/run`; prazo de 60 s por requisição.
- Informação nunca só pela cor; cores seguras para daltonismo (paleta Okabe-Ito).
- Animações (Tarefa 8) só com CSS e JavaScript simples, sem dependências; respeitam `prefers-reduced-motion` e o interruptor "Animações" (preferência guardada em `localStorage`, com `try/catch`); com animações desligadas nada perde informação.
- Cada tarefa termina com commit direto no `main` e `git push` (autorização permanente do usuário; sem force-push, sem apagar branches, sem reescrever histórico). Mensagens de commit terminam com a linha `Co-Authored-By: Claude Sonnet 5.5 <noreply@anthropic.com>`.
- Antes de cada push: `gofmt -l .` sem saída, `go vet ./...`, `go test -race ./...` (e `node --test web/test` a partir da Tarefa 4).

## Review Focus

Entradas e falhas que a spec implica mas nenhum teste óbvio cobriria; cada linha tem o teste na tarefa dona:

1. **Parâmetros extremos ou absurdos** (10000 ônibus × 1000 sementes, limite 1e308, negativos, perfil desconhecido, corpo JSON malformado ou com campos desconhecidos): a API responde 400/413 com `field`, nunca trava nem derruba o servidor. (Tarefa 3)
2. **Clique repetido em Rodar ou duas execuções em sequência rápida:** a resposta velha nunca sobrescreve a nova (a última vence) e a anterior é cancelada. (Tarefa 4: `createLatest`)
3. **Leituras ausentes (`null`), carregador em falha, ônibus que nunca chegou ou saiu cedo:** os gráficos e o painel não quebram com `null`/NaN e mostram o estado. (Tarefas 2, 5 e 6)
4. **Controlador sem explicações (fifo, edf, safe) ou execução sem nenhuma decisão:** o painel de decisão mostra "este controlador não explica suas decisões", não uma tela vazia ou erro. (Tarefa 6)
5. **Requisição vinda de outro `Host` (DNS rebinding) ou de outra origem:** rejeitada. (Tarefa 3)

---

## Estrutura de arquivos

**Go (criar):**
- `internal/sim/validate.go` — `FieldError`, constantes `Max*`, `ValidateParams`.
- `internal/sim/compare.go` — `ControllerNames`, `ForController`, `SeedResult`, `ControllerResult`, `Compare`.
- `internal/sim/trace.go` — `Trace`, `ChargerTrace`, `BusTrace`, `BusOutcome`, `Decision`, `RunTraced`.
- `internal/lab/server.go` — `Server`, `New`, `Handler`, middleware (host, método, content-type, corpo, limite de concorrência).
- `internal/lab/params.go` — `Params`, `defaultParams`, `toSim`, mensagens de erro em português.
- `internal/lab/handlers.go` — rotas `defaults`, `compare`, `run`.
- `internal/lab/convert.go` — `Trace` → DTO de execução, tabela de motivos.
- `internal/lab/series.go` — tipo `Series` (JSON com `null` para NaN, 1 casa decimal).
- `web/embed.go` — `//go:embed static`.
- `cmd/lab/main.go` — ponto de entrada.

**Go (modificar):** `internal/sim/run.go` (Run chama RunTraced), `internal/sim/world.go` (potência física por carregador), `cmd/simrun/main.go` (usa `sim.ValidateParams` e `sim.Compare`).

**Web (criar, tudo em `web/static`):** `index.html`, `app.css`, `js/main.js`, `js/dom.js`, `js/api.js`, `js/params.js`, `js/format.js`, `js/glossary.js`, `js/form.js`, `js/compare.js`, `js/run.js`, `js/cursor.js`, `js/decision.js`, `js/decisionPanel.js`, `js/charts/svg.js`, `js/charts/scale.js`, `js/charts/layout.js`, `js/charts/power.js`, `js/charts/gantt.js`, `js/charts/line.js`. Testes em `web/test/*.test.mjs`; `web/package.json` (`{"type":"module"}`, fora de `static`, só para o Node tratar os módulos como ESM).

**CI (modificar):** `.github/workflows/ci.yml` (passo `node --test`), `README.md` (seção do laboratório).

---

### Task 1: Validação compartilhada e comparação reutilizável

**Files:**
- Create: `internal/sim/validate.go`, `internal/sim/validate_test.go`, `internal/sim/compare.go`, `internal/sim/compare_test.go`
- Modify: `cmd/simrun/main.go`

**Interfaces:**
- Consumes: `sim.GenParams`, `sim.Generate`, `sim.Run`, `sim.Aggregate`, `sim.NewFIFO/NewEDF/NewSafeOnly/NewPlannerController`, `planner.Config`.
- Produces:
  - `const MaxBuses, MaxChargers, MaxSeeds, MaxReadingAgeMin = 10000, 10000, 1000, 10000` e `const MaxLimitKW = 1e7`.
  - `type FieldError struct{ Field, Msg string }` com `Error() string` (= `Field + " " + Msg`). `Field` usa os nomes das flags do `simrun`: `profile`, `seeds`, `buses`, `chargers`, `limit`, `reading-age`, `swap-back-cooldown`, `swap-back-min-need`.
  - `func ValidateParams(p GenParams, cfg planner.Config, seeds int) error` (devolve `*FieldError` ou `nil`).
  - `var ControllerNames = []string{"fifo", "edf", "fifo-unplug", "safe", "planner"}`.
  - `func ForController(name string, cfg planner.Config, sc Scenario) (Scenario, Controller, error)` (para `fifo-unplug` devolve o cenário com `UnplugFull = true`).
  - `type SeedResult struct{ Seed int64; Metrics Metrics }`; `type ControllerResult struct{ Name string; Aggregate Metrics; Seeds []SeedResult }`.
  - `func Compare(ctx context.Context, p GenParams, cfg planner.Config, seeds, workers int) ([]ControllerResult, error)` — resultados na ordem de `ControllerNames` e das sementes 1..seeds, independentes de `workers` (exceto `PlanP99Micros`, medido em tempo real).

- [ ] **Step 1: Escrever os testes que falham**

`internal/sim/validate_test.go`:

```go
package sim

import (
	"errors"
	"math"
	"testing"

	"github.com/EduardoMilani8/depot-charge-planner/internal/planner"
)

func TestValidateParams(t *testing.T) {
	ok := DefaultGenParams()
	cfg := planner.DefaultConfig()
	if err := ValidateParams(ok, cfg, 20); err != nil {
		t.Fatalf("defaults must be valid: %v", err)
	}
	cases := []struct {
		name  string
		mut   func(p *GenParams, c *planner.Config, seeds *int)
		field string
	}{
		{"profile", func(p *GenParams, c *planner.Config, s *int) { p.Profile = "catastrophic" }, "profile"},
		{"seeds zero", func(p *GenParams, c *planner.Config, s *int) { *s = 0 }, "seeds"},
		{"seeds too many", func(p *GenParams, c *planner.Config, s *int) { *s = MaxSeeds + 1 }, "seeds"},
		{"buses zero", func(p *GenParams, c *planner.Config, s *int) { p.NumBuses = 0 }, "buses"},
		{"buses too many", func(p *GenParams, c *planner.Config, s *int) { p.NumBuses = MaxBuses + 1 }, "buses"},
		{"chargers zero", func(p *GenParams, c *planner.Config, s *int) { p.NumChargers = 0 }, "chargers"},
		{"chargers too many", func(p *GenParams, c *planner.Config, s *int) { p.NumChargers = MaxChargers + 1 }, "chargers"},
		{"limit NaN", func(p *GenParams, c *planner.Config, s *int) { p.LimitKW = math.NaN() }, "limit"},
		{"limit Inf", func(p *GenParams, c *planner.Config, s *int) { p.LimitKW = math.Inf(1) }, "limit"},
		{"limit zero", func(p *GenParams, c *planner.Config, s *int) { p.LimitKW = 0 }, "limit"},
		{"limit negative", func(p *GenParams, c *planner.Config, s *int) { p.LimitKW = -5 }, "limit"},
		{"limit huge", func(p *GenParams, c *planner.Config, s *int) { p.LimitKW = MaxLimitKW * 10 }, "limit"},
		{"reading age negative", func(p *GenParams, c *planner.Config, s *int) { p.ReadingAgeMin = -1 }, "reading-age"},
		{"reading age huge", func(p *GenParams, c *planner.Config, s *int) { p.ReadingAgeMin = MaxReadingAgeMin + 1 }, "reading-age"},
		{"cooldown negative", func(p *GenParams, c *planner.Config, s *int) { c.SwapBackCooldownMin = -1 }, "swap-back-cooldown"},
		{"min need NaN", func(p *GenParams, c *planner.Config, s *int) { c.SwapBackMinNeedKWh = math.NaN() }, "swap-back-min-need"},
		{"min need negative", func(p *GenParams, c *planner.Config, s *int) { c.SwapBackMinNeedKWh = -1 }, "swap-back-min-need"},
		{"min need Inf", func(p *GenParams, c *planner.Config, s *int) { c.SwapBackMinNeedKWh = math.Inf(1) }, "swap-back-min-need"},
	}
	for _, tc := range cases {
		p, c, seeds := ok, cfg, 20
		tc.mut(&p, &c, &seeds)
		err := ValidateParams(p, c, seeds)
		var fe *FieldError
		if !errors.As(err, &fe) {
			t.Errorf("%s: want *FieldError, got %v", tc.name, err)
			continue
		}
		if fe.Field != tc.field {
			t.Errorf("%s: field %q, want %q", tc.name, fe.Field, tc.field)
		}
	}
}
```

`internal/sim/compare_test.go`:

```go
package sim

import (
	"context"
	"reflect"
	"testing"

	"github.com/EduardoMilani8/depot-charge-planner/internal/planner"
)

func smallParams() GenParams {
	p := DefaultGenParams()
	p.NumBuses, p.NumChargers, p.LimitKW = 10, 5, 500
	p.Profile = ProfileMild
	return p
}

func zeroP99(ms ...*Metrics) {
	for _, m := range ms {
		m.PlanP99Micros = 0
	}
}

func TestCompareOrderAndShape(t *testing.T) {
	res, err := Compare(context.Background(), smallParams(), planner.DefaultConfig(), 3, 2)
	if err != nil {
		t.Fatal(err)
	}
	if len(res) != len(ControllerNames) {
		t.Fatalf("%d controllers, want %d", len(res), len(ControllerNames))
	}
	for i, r := range res {
		if r.Name != ControllerNames[i] {
			t.Errorf("controller %d is %q, want %q", i, r.Name, ControllerNames[i])
		}
		if len(r.Seeds) != 3 {
			t.Errorf("%s: %d seeds, want 3", r.Name, len(r.Seeds))
		}
		for j, s := range r.Seeds {
			if s.Seed != int64(j+1) {
				t.Errorf("%s: seed %d at index %d", r.Name, s.Seed, j)
			}
		}
	}
}

// The worker count must not change any result except the wall-clock p99.
func TestCompareIsIndependentOfWorkers(t *testing.T) {
	cfg := planner.DefaultConfig()
	a, err := Compare(context.Background(), smallParams(), cfg, 4, 1)
	if err != nil {
		t.Fatal(err)
	}
	b, err := Compare(context.Background(), smallParams(), cfg, 4, 4)
	if err != nil {
		t.Fatal(err)
	}
	for i := range a {
		zeroP99(&a[i].Aggregate, &b[i].Aggregate)
		for j := range a[i].Seeds {
			zeroP99(&a[i].Seeds[j].Metrics, &b[i].Seeds[j].Metrics)
		}
	}
	if !reflect.DeepEqual(a, b) {
		t.Fatalf("results differ between 1 and 4 workers:\n%+v\n%+v", a, b)
	}
}

// Compare must give exactly what a plain loop of Run gives (what simrun printed before).
func TestCompareMatchesPlainRun(t *testing.T) {
	cfg := planner.DefaultConfig()
	p := smallParams()
	res, err := Compare(context.Background(), p, cfg, 3, 3)
	if err != nil {
		t.Fatal(err)
	}
	for _, r := range res {
		var ms []Metrics
		for seed := int64(1); seed <= 3; seed++ {
			sc, ctrl, err := ForController(r.Name, cfg, Generate(p, seed))
			if err != nil {
				t.Fatal(err)
			}
			ms = append(ms, Run(sc, ctrl, nil))
		}
		want := Aggregate(ms)
		got := r.Aggregate
		zeroP99(&want, &got)
		if !reflect.DeepEqual(want, got) {
			t.Errorf("%s: aggregate differs\nwant %+v\ngot  %+v", r.Name, want, got)
		}
	}
}

func TestCompareHonoursCancellation(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := Compare(ctx, smallParams(), planner.DefaultConfig(), 50, 2); err == nil {
		t.Fatal("a cancelled context must return an error")
	}
}

func TestForControllerUnknownName(t *testing.T) {
	if _, _, err := ForController("nope", planner.DefaultConfig(), Scenario{}); err == nil {
		t.Fatal("unknown controller must be an error")
	}
	sc, _, err := ForController("fifo-unplug", planner.DefaultConfig(), Scenario{})
	if err != nil || !sc.UnplugFull {
		t.Fatalf("fifo-unplug must set UnplugFull: %v %v", sc.UnplugFull, err)
	}
}
```

- [ ] **Step 2: Rodar e ver falhar**

Run: `go test ./internal/sim -run 'TestValidateParams|TestCompare|TestForController' -count=1`
Expected: FAIL (compilação: `undefined: ValidateParams`, `Compare`, `ForController`, ...).

- [ ] **Step 3: Implementar `validate.go`**

```go
package sim

import (
	"fmt"

	"github.com/EduardoMilani8/depot-charge-planner/internal/planner"
)

// Upper bounds that keep one invocation (simrun or the lab) from exhausting memory or time.
const (
	MaxBuses         = 10000
	MaxChargers      = 10000
	MaxSeeds         = 1000
	MaxReadingAgeMin = 10000
	// MaxLimitKW: above this the verifier's absolute tolerance is near float rounding.
	MaxLimitKW = 1e7
)

// FieldError names the invalid parameter by its simrun flag name.
type FieldError struct{ Field, Msg string }

func (e *FieldError) Error() string { return e.Field + " " + e.Msg }

// ValidateParams checks a generator/planner parameter set. Comparisons are written so
// that NaN and +/-Inf fail them.
func ValidateParams(p GenParams, cfg planner.Config, seeds int) error {
	switch p.Profile {
	case ProfileNone, ProfileMild, ProfileSevere, ProfileRandom:
	default:
		return &FieldError{"profile", fmt.Sprintf("unknown %q (use none, mild, severe or random)", string(p.Profile))}
	}
	if seeds < 1 || seeds > MaxSeeds {
		return &FieldError{"seeds", fmt.Sprintf("must be between 1 and %d", MaxSeeds)}
	}
	if p.NumBuses < 1 || p.NumBuses > MaxBuses {
		return &FieldError{"buses", fmt.Sprintf("must be between 1 and %d", MaxBuses)}
	}
	if p.NumChargers < 1 || p.NumChargers > MaxChargers {
		return &FieldError{"chargers", fmt.Sprintf("must be between 1 and %d", MaxChargers)}
	}
	if !(p.LimitKW > 0 && p.LimitKW <= MaxLimitKW) {
		return &FieldError{"limit", fmt.Sprintf("must be a finite value in (0, %g] kW", float64(MaxLimitKW))}
	}
	if p.ReadingAgeMin < 0 || p.ReadingAgeMin > MaxReadingAgeMin {
		return &FieldError{"reading-age", fmt.Sprintf("must be between 0 and %d", MaxReadingAgeMin)}
	}
	if cfg.SwapBackCooldownMin < 0 {
		return &FieldError{"swap-back-cooldown", "must be >= 0"}
	}
	// NaN fails the check; the planner would treat NaN, +Inf or a negative value as
	// "rule off", so callers are told instead.
	if !(cfg.SwapBackMinNeedKWh >= 0 && cfg.SwapBackMinNeedKWh <= MaxLimitKW) {
		return &FieldError{"swap-back-min-need", fmt.Sprintf("must be a finite value in [0, %g] kWh", float64(MaxLimitKW))}
	}
	return nil
}
```

- [ ] **Step 4: Implementar `compare.go`**

```go
package sim

import (
	"context"
	"fmt"
	"sync"

	"github.com/EduardoMilani8/depot-charge-planner/internal/planner"
)

// ControllerNames lists the controllers Compare runs, in table order.
var ControllerNames = []string{"fifo", "edf", "fifo-unplug", "safe", "planner"}

// ForController returns the scenario to run for a controller (fifo-unplug turns on the
// operators' manual unplug routine) and the controller itself.
func ForController(name string, cfg planner.Config, sc Scenario) (Scenario, Controller, error) {
	switch name {
	case "fifo":
		return sc, NewFIFO(), nil
	case "edf":
		return sc, NewEDF(), nil
	case "fifo-unplug":
		sc.UnplugFull = true
		return sc, NewFIFO(), nil
	case "safe":
		return sc, NewSafeOnly(), nil
	case "planner":
		return sc, NewPlannerController(cfg, sc), nil
	}
	return sc, nil, fmt.Errorf("unknown controller %q", name)
}

// SeedResult is one run of one controller.
type SeedResult struct {
	Seed    int64
	Metrics Metrics
}

// ControllerResult is one controller over seeds 1..N: the mean (Aggregate) and each run.
type ControllerResult struct {
	Name      string
	Aggregate Metrics
	Seeds     []SeedResult
}

// Compare runs every controller on seeds 1..seeds. Runs are independent, so up to
// `workers` of them run at once; results are always ordered by controller and seed, so
// they do not depend on workers (PlanP99Micros is wall-clock and does). ctx is checked
// before each run is started.
func Compare(ctx context.Context, p GenParams, cfg planner.Config, seeds, workers int) ([]ControllerResult, error) {
	if workers < 1 {
		workers = 1
	}
	out := make([]ControllerResult, 0, len(ControllerNames))
	for _, name := range ControllerNames {
		res := make([]SeedResult, seeds)
		sem := make(chan struct{}, workers)
		var wg sync.WaitGroup
		for i := 0; i < seeds; i++ {
			if ctx.Err() != nil {
				break
			}
			sem <- struct{}{}
			wg.Add(1)
			go func(i int) {
				defer wg.Done()
				defer func() { <-sem }()
				seed := int64(i + 1)
				sc, ctrl, _ := ForController(name, cfg, Generate(p, seed)) // names are fixed: no error
				res[i] = SeedResult{Seed: seed, Metrics: Run(sc, ctrl, nil)}
			}(i)
		}
		wg.Wait()
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		ms := make([]Metrics, seeds)
		for i, r := range res {
			ms[i] = r.Metrics
		}
		out = append(out, ControllerResult{Name: name, Aggregate: Aggregate(ms), Seeds: res})
	}
	return out, nil
}
```

- [ ] **Step 5: Rodar e ver passar**

Run: `go test ./internal/sim -run 'TestValidateParams|TestCompare|TestForController' -race -count=1`
Expected: PASS.

- [ ] **Step 6: Fazer o `simrun` usar `ValidateParams` e `Compare`**

Em `cmd/simrun/main.go`:
1. Apague o bloco `const ( maxBuses ... )` e todos os `if` de validação de `*seeds`, `p.NumBuses`, `p.NumChargers`, `p.LimitKW`, `p.ReadingAgeMin`, `cfg.SwapBackCooldownMin`, `cfg.SwapBackMinNeedKWh` e o `switch sim.FaultProfile(*profile)`.
2. Depois de `fs.Parse` e de `p.Profile = sim.FaultProfile(*profile)`, mantenha a rejeição de argumentos soltos e valide com a função compartilhada:

```go
	p.Profile = sim.FaultProfile(*profile)
	if fs.NArg() > 0 {
		fmt.Fprintf(stderr, "unexpected argument %q\n", fs.Arg(0))
		return 2
	}
	if err := sim.ValidateParams(p, cfg, *seeds); err != nil {
		fmt.Fprintf(stderr, "-%s\n", err)
		return 2
	}
```

3. Apague `type maker ...`, `makers := ...` e o laço `for _, mk := range makers { ... }`; no lugar, rode a comparação (1 worker, como antes, para o p99 seguir sequencial) e imprima as linhas:

```go
	results, err := sim.Compare(context.Background(), p, cfg, *seeds, 1)
	if err != nil {
		fmt.Fprintln(stderr, err)
		return 1
	}
```

e dentro do `tabwriter`, depois do cabeçalho `fmt.Fprintln(w, "controller\t...")`:

```go
	for _, r := range results {
		a := r.Aggregate
		fmt.Fprintf(w, "%s\t%.1f\t%.1f\t%.0f\t%d\t%d\t%.0f\t%.0f\t%d\t%.1f\t%d\n",
			r.Name, a.ReadyPct, a.ShortfallKWh, a.PeakKW, a.PlanViolations, a.OvershootMin, a.EnergyKWh, a.CostBRL, a.PlanChanges, a.OperatorMoves, a.PlanP99Micros)
	}
```

4. Acrescente `"context"` aos imports. A mensagem de erro de um parâmetro inválido passa a vir de `FieldError` (por exemplo `-limit must be a finite value in (0, 1e+07] kW`); os códigos de saída (2) não mudam.

- [ ] **Step 7: Provar que o `simrun` não mudou**

Compare a saída do código antigo (um checkout limpo do commit atual, sem as suas mudanças) com a do novo, em todas as colunas **menos** a última (`p99 µs`, tempo real):

```bash
BASE=$(mktemp -d) && git worktree add -q "$BASE" HEAD
for prof in none mild severe; do
  (cd "$BASE" && go run ./cmd/simrun -profile $prof -limit 600 -seeds 5) | cut -f1-10 > /tmp/before-$prof.txt
  go run ./cmd/simrun -profile $prof -limit 600 -seeds 5 | cut -f1-10 > /tmp/after-$prof.txt
  diff /tmp/before-$prof.txt /tmp/after-$prof.txt && echo "$prof: SAME"
done
git worktree remove --force "$BASE"
```

Expected: `none: SAME`, `mild: SAME`, `severe: SAME`. (O `tabwriter` alinha com espaços; se o `cut` por tabulação não separar, compare as linhas inteiras e ignore só os dígitos finais de `p99 µs`.)
Run: `go test ./cmd/simrun -race -count=1` — Expected: PASS (`TestRunRejectsBadArguments` ainda recebe exit 2).

- [ ] **Step 8: Verificação completa e commit**

Run: `gofmt -l . ; go vet ./... && go test -race -short ./...` — Expected: sem saída do gofmt, resto PASS.

```bash
git add internal/sim/validate.go internal/sim/validate_test.go internal/sim/compare.go internal/sim/compare_test.go cmd/simrun/main.go
git commit -m "refactor(sim): shared parameter validation and Compare, used by simrun

Co-Authored-By: Claude Sonnet 5.5 <noreply@anthropic.com>"
git push
```

---

### Task 2: Traço da verdade do simulador (`Trace`, `RunTraced`)

**Files:**
- Create: `internal/sim/trace.go`, `internal/sim/trace_test.go`
- Modify: `internal/sim/run.go`, `internal/sim/world.go`

**Interfaces:**
- Consumes: `World` (campos `t`, `buses`, `chargers`, `cmd`, `applied`, métodos `limitAt`, `commandedTotal`, `advance`), `planner.Input`, `planner.Plan`.
- Produces (todos em `package sim`):
  - `func RunTraced(sc Scenario, ctrl Controller, rec Recorder, tr *Trace) Metrics`; `Run(sc, ctrl, rec)` passa a ser `RunTraced(sc, ctrl, rec, nil)` e continua devolvendo as mesmas métricas.
  - `type Trace struct { Limit, Commanded, Physical []float64; Layer []string; Chargers []*ChargerTrace; Buses []*BusTrace; Outcomes []BusOutcome; Decisions []Decision }`. Uma posição por minuto `0..Horizon` em todas as séries; `Chargers` e `Buses` ordenados por ID.
  - `type ChargerTrace struct { ID string; Status []int; CommandedKW, PhysicalKW []float64; BusID []string }` (`Status` = `model.ChargerStatus`; `BusID` vazio = sem ônibus).
  - `type BusTrace struct { ID string; State []int; TrueSoC, Observed []float64; ChargerID []string }` (`State`: 0 ainda não chegou, 1 presente, 2 já saiu; `TrueSoC` e `Observed` são `NaN` quando o ônibus não está presente ou, no caso de `Observed`, quando o planejador não recebeu leitura utilizável).
  - `type BusOutcome struct { ID string; Arrival, Departure int; CapacityKWh, InitialSoCKWh, ForecastTargetKWh, TrueTargetKWh, FinalSoCKWh float64; Departed, Ready bool; ShortfallKWh float64 }` (`Arrival` e `Departure` já incluem chegada tardia e saída antecipada).
  - `type Decision struct { Minute int; Layer string; Setpoints []planner.Setpoint; Buses []planner.BusStatus; Swaps []planner.Swap; Notes []string }` — só os minutos em que a decisão mudou (potência arredondada a 1 kW; números nos motivos mascarados na comparação).
  - `func NewTrace() *Trace`.

- [ ] **Step 1: Escrever os testes que falham**

`internal/sim/trace_test.go`:

```go
package sim

import (
	"math"
	"reflect"
	"testing"

	"github.com/EduardoMilani8/depot-charge-planner/internal/planner"
)

func tracedRun(p GenParams, seed int64, controller string) (Metrics, *Trace, Scenario) {
	cfg := planner.DefaultConfig()
	sc, ctrl, _ := ForController(controller, cfg, Generate(p, seed))
	tr := NewTrace()
	m := RunTraced(sc, ctrl, nil, tr)
	return m, tr, sc
}

func sameFloats(a, b []float64) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if math.IsNaN(a[i]) != math.IsNaN(b[i]) || (!math.IsNaN(a[i]) && a[i] != b[i]) {
			return false
		}
	}
	return true
}

func TestRunTracedMatchesRun(t *testing.T) {
	for _, profile := range []FaultProfile{ProfileNone, ProfileSevere, ProfileRandom} {
		p := smallParams()
		p.Profile = profile
		for _, name := range ControllerNames {
			sc, ctrl, _ := ForController(name, planner.DefaultConfig(), Generate(p, 2))
			want := Run(sc, ctrl, nil)
			sc2, ctrl2, _ := ForController(name, planner.DefaultConfig(), Generate(p, 2))
			got := RunTraced(sc2, ctrl2, nil, NewTrace())
			want.PlanP99Micros, got.PlanP99Micros = 0, 0
			if !reflect.DeepEqual(want, got) {
				t.Errorf("%s/%s: tracing changed the metrics\nwant %+v\ngot  %+v", profile, name, want, got)
			}
		}
	}
}

func TestTraceShapes(t *testing.T) {
	p := smallParams()
	m, tr, sc := tracedRun(p, 1, "planner")
	n := sc.Horizon + 1
	for name, l := range map[string]int{"Limit": len(tr.Limit), "Commanded": len(tr.Commanded), "Physical": len(tr.Physical), "Layer": len(tr.Layer)} {
		if l != n {
			t.Errorf("%s has %d minutes, want %d", name, l, n)
		}
	}
	if len(tr.Chargers) != p.NumChargers || len(tr.Buses) != p.NumBuses || len(tr.Outcomes) != p.NumBuses {
		t.Fatalf("%d chargers, %d buses, %d outcomes", len(tr.Chargers), len(tr.Buses), len(tr.Outcomes))
	}
	for _, c := range tr.Chargers {
		if len(c.Status) != n || len(c.CommandedKW) != n || len(c.PhysicalKW) != n || len(c.BusID) != n {
			t.Errorf("charger %s series have the wrong length", c.ID)
		}
	}
	for _, b := range tr.Buses {
		if len(b.State) != n || len(b.TrueSoC) != n || len(b.Observed) != n || len(b.ChargerID) != n {
			t.Errorf("bus %s series have the wrong length", b.ID)
		}
	}
	if m.PlanViolations != 0 {
		t.Fatalf("planner violated the limit: %d", m.PlanViolations)
	}
	for i := 0; i < n; i++ {
		if tr.Commanded[i] > tr.Limit[i]+1e-6 {
			t.Fatalf("minute %d: commanded %.1f kW above the limit %.1f", i, tr.Commanded[i], tr.Limit[i])
		}
	}
	peak := 0.0
	for _, v := range tr.Physical {
		peak = math.Max(peak, v)
	}
	if math.Abs(peak-m.PeakKW) > 1e-6 {
		t.Errorf("peak of the physical series %.3f != metrics PeakKW %.3f", peak, m.PeakKW)
	}
}

func TestTraceOutcomesMatchMetrics(t *testing.T) {
	m, tr, _ := tracedRun(smallParams(), 3, "planner")
	ready, short := 0, 0.0
	for _, o := range tr.Outcomes {
		if o.Ready {
			ready++
		} else if o.Departed {
			short += o.ShortfallKWh
		}
		if o.Departure < o.Arrival {
			t.Errorf("bus %s departs (%d) before it arrives (%d)", o.ID, o.Departure, o.Arrival)
		}
	}
	if ready != m.Ready {
		t.Errorf("%d ready outcomes, metrics say %d", ready, m.Ready)
	}
	if math.Abs(short-m.ShortfallKWh) > 1e-6 {
		t.Errorf("outcome shortfall %.3f != metrics %.3f", short, m.ShortfallKWh)
	}
}

func TestTraceIsDeterministic(t *testing.T) {
	p := smallParams()
	p.Profile = ProfileRandom
	_, a, _ := tracedRun(p, 4, "planner")
	_, b, _ := tracedRun(p, 4, "planner")
	if !sameFloats(a.Physical, b.Physical) || !sameFloats(a.Commanded, b.Commanded) {
		t.Fatal("power series differ between identical runs")
	}
	for i := range a.Buses {
		if !sameFloats(a.Buses[i].TrueSoC, b.Buses[i].TrueSoC) || !sameFloats(a.Buses[i].Observed, b.Buses[i].Observed) {
			t.Fatalf("bus %s series differ between identical runs", a.Buses[i].ID)
		}
	}
	if !reflect.DeepEqual(a.Decisions, b.Decisions) || !reflect.DeepEqual(a.Outcomes, b.Outcomes) {
		t.Fatal("decisions or outcomes differ between identical runs")
	}
}

func TestDecisionsAreElided(t *testing.T) {
	_, tr, sc := tracedRun(smallParams(), 1, "planner")
	if len(tr.Decisions) == 0 || tr.Decisions[0].Minute != 0 {
		t.Fatalf("the first decision must be at minute 0: %+v", tr.Decisions)
	}
	if len(tr.Decisions) >= sc.Horizon+1 {
		t.Errorf("%d decisions for %d minutes: unchanged minutes must be skipped", len(tr.Decisions), sc.Horizon+1)
	}
	for i := 1; i < len(tr.Decisions); i++ {
		if tr.Decisions[i].Minute <= tr.Decisions[i-1].Minute {
			t.Fatalf("decisions are not in minute order at %d", i)
		}
	}
}

func TestObservedIsNaNWithoutAReading(t *testing.T) {
	p := smallParams()
	p.Profile = ProfileNone
	sc := Generate(p, 1)
	sc.Faults = append(sc.Faults, Fault{Kind: FaultSoCMissing, Target: "*", From: 0, To: forever})
	_, ctrl, _ := ForController("planner", planner.DefaultConfig(), sc)
	tr := NewTrace()
	RunTraced(sc, ctrl, nil, tr)
	seenPresent := false
	for _, b := range tr.Buses {
		for i, st := range b.State {
			if st != 1 {
				if !math.IsNaN(b.TrueSoC[i]) {
					t.Fatalf("bus %s minute %d: true SoC must be NaN while not present", b.ID, i)
				}
				continue
			}
			seenPresent = true
			if math.IsNaN(b.TrueSoC[i]) {
				t.Fatalf("bus %s minute %d: a present bus has a true SoC", b.ID, i)
			}
			if !math.IsNaN(b.Observed[i]) {
				t.Fatalf("bus %s minute %d: observed must be NaN when readings are missing, got %v", b.ID, i, b.Observed[i])
			}
		}
	}
	if !seenPresent {
		t.Fatal("no bus was ever present: the test checks nothing")
	}
}

func TestChargerSeriesAgreeWithBuses(t *testing.T) {
	_, tr, _ := tracedRun(smallParams(), 1, "planner")
	idx := map[string]*BusTrace{}
	for _, b := range tr.Buses {
		idx[b.ID] = b
	}
	for _, c := range tr.Chargers {
		for i, id := range c.BusID {
			if id == "" {
				continue
			}
			if b := idx[id]; b == nil || b.ChargerID[i] != c.ID {
				t.Fatalf("minute %d: charger %s says bus %s, the bus says otherwise", i, c.ID, id)
			}
		}
	}
}
```

- [ ] **Step 2: Rodar e ver falhar**

Run: `go test ./internal/sim -run 'TestRunTraced|TestTrace|TestDecisions|TestObserved|TestChargerSeries' -count=1`
Expected: FAIL (compilação: `undefined: NewTrace`, `RunTraced`, ...).

- [ ] **Step 3: Guardar a potência física por carregador no `World`**

Em `internal/sim/world.go`:

1. No `type World struct`, acrescente depois de `applied`:

```go
	physKW    map[string]float64 // grid power each charger actually delivered this step
	physTotal float64            // sum of physKW (what the overshoot metric compares to the limit)
```

2. Em `newWorld`, na literal `w := &World{...}`, acrescente `physKW: map[string]float64{},`.

3. Em `advance`, no começo da função (antes de `total := 0.0`):

```go
	w.physKW = map[string]float64{}
```

dentro do laço, logo depois de `total += grid / stepHours`:

```go
		w.physKW[c.ID] = grid / stepHours
```

e logo depois do laço `for _, bs := range w.buses { ... }` (antes de `if total > m.PeakKW`):

```go
	w.physTotal = total
```

4. Acrescente o método (perto de `commandedTotal`):

```go
// commandedBy is the power one charger draws under this step's commands (offline
// chargers keep drawing their last power).
func (w *World) commandedBy(c model.Charger) float64 {
	if c.Status == model.ChargerOffline {
		return w.applied[c.ID]
	}
	return w.cmd[c.ID]
}
```

- [ ] **Step 4: Implementar `trace.go`**

```go
package sim

import (
	"fmt"
	"math"
	"regexp"
	"strings"

	"github.com/EduardoMilani8/depot-charge-planner/internal/planner"
)

// ChargerTrace is one charger's minute-by-minute truth.
type ChargerTrace struct {
	ID          string
	Status      []int // model.ChargerStatus
	CommandedKW []float64
	PhysicalKW  []float64
	BusID       []string // "" when no bus is plugged
}

// BusTrace is one bus's minute-by-minute truth next to what the planner was told.
type BusTrace struct {
	ID        string
	State     []int     // 0 not arrived yet, 1 present, 2 departed
	TrueSoC   []float64 // NaN while the bus is not present
	Observed  []float64 // NaN when not present or when the planner got no usable reading
	ChargerID []string  // "" when waiting
}

// BusOutcome is how a bus ended. Arrival and Departure include late arrival and early
// departure faults.
type BusOutcome struct {
	ID                                                               string
	Arrival, Departure                                               int
	CapacityKWh, InitialSoCKWh, ForecastTargetKWh, TrueTargetKWh    float64
	FinalSoCKWh                                                      float64
	Departed, Ready                                                  bool
	ShortfallKWh                                                     float64
}

// Decision is a planning cycle that differs from the previous one (see decisionKey).
type Decision struct {
	Minute    int
	Layer     string
	Setpoints []planner.Setpoint
	Buses     []planner.BusStatus
	Swaps     []planner.Swap
	Notes     []string
}

// Trace records the simulator's truth (what the planner never sees) for one run, one
// entry per minute 0..Horizon, plus the decisions that changed.
type Trace struct {
	Limit, Commanded, Physical []float64
	Layer                      []string
	Chargers                   []*ChargerTrace // sorted by ID
	Buses                      []*BusTrace     // sorted by ID
	Outcomes                   []BusOutcome    // sorted by ID, filled when the run ends
	Decisions                  []Decision

	started bool
	lastKey string
}

// NewTrace returns an empty trace to pass to RunTraced.
func NewTrace() *Trace { return &Trace{} }

func (tr *Trace) start(w *World) {
	tr.started = true
	for _, c := range w.chargers { // sorted by ID in newWorld
		tr.Chargers = append(tr.Chargers, &ChargerTrace{ID: c.ID})
	}
	for _, bs := range w.buses { // sorted by ID in newWorld
		tr.Buses = append(tr.Buses, &BusTrace{ID: bs.spec.Bus.ID})
	}
}

// capture records the end of minute w.t (after the physics step, before commands become
// the power in effect for the next step).
func (tr *Trace) capture(w *World, in planner.Input, plan planner.Plan) {
	if !tr.started {
		tr.start(w)
	}
	tr.Limit = append(tr.Limit, w.limitAt(w.t))
	tr.Commanded = append(tr.Commanded, w.commandedTotal())
	tr.Physical = append(tr.Physical, w.physTotal)
	tr.Layer = append(tr.Layer, plan.Layer.String())

	plugged := map[string]string{}
	for _, bs := range w.buses {
		if bs.present && !bs.departed && bs.chargerID != "" {
			plugged[bs.chargerID] = bs.spec.Bus.ID
		}
	}
	for i, c := range w.chargers {
		ct := tr.Chargers[i]
		ct.Status = append(ct.Status, int(c.Status))
		ct.CommandedKW = append(ct.CommandedKW, w.commandedBy(c))
		ct.PhysicalKW = append(ct.PhysicalKW, w.physKW[c.ID])
		ct.BusID = append(ct.BusID, plugged[c.ID])
	}

	observed := map[string]float64{}
	for _, b := range in.Buses {
		if b.SoCConfidence > 0 {
			observed[b.ID] = b.SoCKWh
		}
	}
	for i, bs := range w.buses {
		bt := tr.Buses[i]
		state, soc, obs, ch := 0, math.NaN(), math.NaN(), ""
		switch {
		case bs.departed:
			state = 2
		case bs.present:
			state, soc, ch = 1, bs.soc, bs.chargerID
			if v, ok := observed[bs.spec.Bus.ID]; ok {
				obs = v
			}
		}
		bt.State = append(bt.State, state)
		bt.TrueSoC = append(bt.TrueSoC, soc)
		bt.Observed = append(bt.Observed, obs)
		bt.ChargerID = append(bt.ChargerID, ch)
	}
}

// recordDecision keeps the plan only when it differs from the previous one.
func (tr *Trace) recordDecision(minute int, plan planner.Plan) {
	key := decisionKey(plan)
	if len(tr.Decisions) > 0 && key == tr.lastKey {
		return
	}
	tr.lastKey = key
	tr.Decisions = append(tr.Decisions, Decision{
		Minute:    minute,
		Layer:     plan.Layer.String(),
		Setpoints: append([]planner.Setpoint(nil), plan.Setpoints...),
		Buses:     append([]planner.BusStatus(nil), plan.Buses...),
		Swaps:     append([]planner.Swap(nil), plan.Swaps...),
		Notes:     append([]string(nil), plan.Notes...),
	})
}

var numbers = regexp.MustCompile(`[0-9]+(?:[.,][0-9]+)?`)

// decisionKey identifies "the same decision": the same layer, setpoints (to 1 kW),
// per-bus verdicts and swaps. Numbers inside the reasons are masked, because texts like
// "folga 252 min" change every minute without the decision changing.
func decisionKey(p planner.Plan) string {
	var b strings.Builder
	fmt.Fprintf(&b, "%s|", p.Layer)
	for _, s := range p.Setpoints {
		fmt.Fprintf(&b, "%s=%.0f;", s.ChargerID, s.KW)
	}
	b.WriteByte('|')
	for _, s := range p.Buses {
		fmt.Fprintf(&b, "%s:%t:%t:%s;", s.BusID, s.Assessed, s.WillReachTarget, numbers.ReplaceAllString(s.Reason, "#"))
	}
	b.WriteByte('|')
	for _, s := range p.Swaps {
		fmt.Fprintf(&b, "%s>%s@%s;", s.OutBusID, s.InBusID, s.ChargerID)
	}
	return b.String()
}

// finish records how every bus ended (call after World.finish).
func (tr *Trace) finish(w *World) {
	for _, bs := range w.buses {
		b := bs.spec.Bus
		ready := bs.departed && bs.soc >= bs.target-1e-6
		short := 0.0
		if bs.departed && !ready {
			short = bs.target - bs.soc
		}
		tr.Outcomes = append(tr.Outcomes, BusOutcome{
			ID: b.ID, Arrival: bs.arrival, Departure: bs.departure,
			CapacityKWh: b.CapacityKWh, InitialSoCKWh: b.SoCKWh,
			ForecastTargetKWh: b.TargetKWh, TrueTargetKWh: bs.target,
			FinalSoCKWh: bs.soc, Departed: bs.departed, Ready: ready, ShortfallKWh: short,
		})
	}
}
```

(Rode `gofmt -w internal/sim/trace.go` para alinhar os campos de `BusOutcome`.)

- [ ] **Step 5: `Run` passa a chamar `RunTraced`**

Em `internal/sim/run.go`, renomeie a função atual e crie o invólucro:

```go
// Run simulates the scenario with the given controller. rec may be nil.
func Run(sc Scenario, ctrl Controller, rec Recorder) Metrics {
	return RunTraced(sc, ctrl, rec, nil)
}

// RunTraced is Run that also fills tr (when not nil) with the simulator's truth and
// the decisions. It returns exactly the metrics Run returns.
func RunTraced(sc Scenario, ctrl Controller, rec Recorder, tr *Trace) Metrics {
	// ... corpo atual de Run, sem mudanças, com estas duas inserções:
}
```

Inserções dentro do corpo: logo depois de `w.advance(&m)` e antes de `w.endTick()`:

```go
		if tr != nil {
			tr.capture(w, in, plan)
			tr.recordDecision(t, plan)
		}
```

e logo depois de `w.finish(&m)`:

```go
	if tr != nil {
		tr.finish(w)
	}
```

- [ ] **Step 6: Rodar e ver passar**

Run: `go test ./internal/sim -run 'TestRunTraced|TestTrace|TestDecisions|TestObserved|TestChargerSeries' -race -count=1 -v`
Expected: PASS (7 testes). Se `TestTraceShapes` reclamar do pico, confira que `physTotal` é atribuído **depois** do laço de `advance` e que `m.PeakKW` usa o mesmo `total`.

- [ ] **Step 7: Garantir que nada mudou nos números do projeto**

Run: `go test -race -short ./...` — Expected: PASS (inclui goldens e propriedades).
Run: `go run ./cmd/simrun -profile severe -limit 1200 -seeds 20 | cut -f1-3` e compare com a tabela do README (mesmas linhas de `ready%` e `shortfall`). Expected: iguais.

- [ ] **Step 8: Commit**

```bash
gofmt -l . ; go vet ./...
git add internal/sim/trace.go internal/sim/trace_test.go internal/sim/run.go internal/sim/world.go
git commit -m "feat(sim): Trace and RunTraced record the simulator's truth minute by minute

Co-Authored-By: Claude Sonnet 5.5 <noreply@anthropic.com>"
git push
```

---

### Task 3: Servidor HTTP (`internal/lab`, `web/embed.go`, `cmd/lab`)

**Files:**
- Create: `web/embed.go`, `web/static/index.html` (provisório, a Tarefa 4 o substitui), `internal/lab/series.go`, `internal/lab/params.go`, `internal/lab/server.go`, `internal/lab/handlers.go`, `internal/lab/convert.go`, `internal/lab/lab_test.go`, `cmd/lab/main.go`, `cmd/lab/main_test.go`

**Interfaces:**
- Consumes: de `internal/sim` — `GenParams`, `DefaultGenParams`, `ValidateParams`, `FieldError`, `Max*`, `ControllerNames`, `ForController`, `Generate`, `Compare`, `RunTraced`, `NewTrace`, `Trace`, `Metrics`, `Scenario`; de `internal/planner` — `Config`, `DefaultConfig`.
- Produces:
  - `package web`: `var Static embed.FS` com `//go:embed static` (arquivos em `static/...`).
  - `package lab`: `type Server`, `func New() *Server`, `func (s *Server) Handler() http.Handler`; campos de teste `s.sem chan struct{}` (capacidade 2) e `s.timeout time.Duration` (60 s).
  - Rotas e formato JSON (ver abaixo).
  - `cmd/lab`: `func run(ctx context.Context, args []string, stdout, stderr io.Writer) int`, `func checkAddr(addr string) error`.

**Contrato da API** (chaves JSON em `snake_case`; todas as respostas de erro são `{"error": "<mensagem em português>", "field": "<chave JSON do campo, opcional>"}`):

`Params` (corpo de `/api/compare` e parte do de `/api/run`; campos ausentes usam o padrão): `buses`, `chargers`, `limit_kw`, `profile` (`none|mild|severe|random`), `seeds`, `follow_swaps`, `reading_age_min`, `swap_back_cooldown_min`, `swap_back_min_need_kwh`.

- `GET /api/defaults` → `{"params": Params, "limits": {"max_buses","max_chargers","max_seeds","max_limit_kw","max_reading_age_min","lab_max_work","lab_max_run_buses"}, "presets": [{"id","label","description","params": Params}], "controllers": [...], "profiles": [...]}`.
- `POST /api/compare` (corpo `Params`) → `{"params": Params, "controllers": [{"name","aggregate": Metrics,"seeds": [{"seed","metrics": Metrics}]}], "p99_note": "<texto>"}`; `Metrics` é o `sim.Metrics` com as tags JSON que ele já tem.
- `POST /api/run` (corpo `Params` + `"seed"` 1..1000 + `"controller"`) → `{"params","seed","controller","metrics","scenario","series","reasons","decisions","outcomes"}`:
  - `scenario`: `{"horizon_min","start_clock_min","base_limit_kw","chargers":[{"id","max_kw","min_kw"}],"faults":[{"kind","target","from","to","value"}]}` (`to` limitado ao horizonte).
  - `series`: `{"limit_kw","commanded_kw","physical_kw" (arrays por minuto), "layer" (array de "normal|last-valid|safe"), "chargers":[{"id","status":[int],"commanded_kw","physical_kw"}], "buses":[{"id","state":[0|1|2],"true_soc_kwh","observed_kwh" (null = sem leitura/ausente),"charger":[índice em scenario.chargers ou -1]}]}`; números com 1 casa decimal.
  - `reasons`: tabela de textos; `decisions`: `[{"minute","layer","setpoints":[{"c","kw"}],"buses":[{"b","ok","as","sf","r"}] (r = índice em reasons ou -1),"swaps":[{"charger","out","in","reason"}],"notes":[...]}]`.
  - `outcomes`: `[{"id","arrival","departure","capacity_kwh","initial_soc_kwh","forecast_target_kwh","true_target_kwh","final_soc_kwh","departed","ready","shortfall_kwh"}]`.

- [ ] **Step 1: Escrever os testes que falham**

`internal/lab/lab_test.go`:

```go
package lab

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"io/fs"
	"net/http"
	"net/http/httptest"
	"path"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/EduardoMilani8/depot-charge-planner/internal/sim"
	"github.com/EduardoMilani8/depot-charge-planner/web"
)

const smallBody = `{"buses":10,"chargers":5,"limit_kw":500,"profile":"mild","seeds":3}`

func newReq(method, target, body string) *http.Request {
	var r io.Reader
	if body != "" {
		r = strings.NewReader(body)
	}
	req := httptest.NewRequest(method, target, r)
	req.Host = "127.0.0.1:8080"
	if method == http.MethodPost {
		req.Header.Set("Content-Type", "application/json")
	}
	return req
}

func do(s *Server, req *http.Request) *httptest.ResponseRecorder {
	rec := httptest.NewRecorder()
	s.Handler().ServeHTTP(rec, req)
	return rec
}

func decode(t *testing.T, rec *httptest.ResponseRecorder, v any) {
	t.Helper()
	if err := json.Unmarshal(rec.Body.Bytes(), v); err != nil {
		t.Fatalf("invalid JSON (%v): %.300s", err, rec.Body.String())
	}
}

func TestDefaults(t *testing.T) {
	rec := do(New(), newReq("GET", "/api/defaults", ""))
	if rec.Code != 200 {
		t.Fatalf("status %d: %s", rec.Code, rec.Body)
	}
	var d struct {
		Params      Params   `json:"params"`
		Controllers []string `json:"controllers"`
		Limits      struct {
			MaxBuses int `json:"max_buses"`
		} `json:"limits"`
		Presets []struct {
			ID     string `json:"id"`
			Label  string `json:"label"`
			Params Params `json:"params"`
		} `json:"presets"`
	}
	decode(t, rec, &d)
	if d.Params != defaultParams() {
		t.Errorf("defaults %+v", d.Params)
	}
	if strings.Join(d.Controllers, ",") != strings.Join(sim.ControllerNames, ",") {
		t.Errorf("controllers %v", d.Controllers)
	}
	if d.Limits.MaxBuses != sim.MaxBuses {
		t.Errorf("max buses %d", d.Limits.MaxBuses)
	}
	if len(d.Presets) < 3 {
		t.Fatalf("want at least 3 presets, got %d", len(d.Presets))
	}
	for _, p := range d.Presets {
		if p.ID == "" || p.Label == "" {
			t.Errorf("preset without id/label: %+v", p)
		}
		if err := p.Params.validate(); err != nil {
			t.Errorf("preset %s is invalid: %+v", p.ID, err)
		}
	}
}

func TestCompareShape(t *testing.T) {
	rec := do(New(), newReq("POST", "/api/compare", smallBody))
	if rec.Code != 200 {
		t.Fatalf("status %d: %s", rec.Code, rec.Body)
	}
	var r struct {
		Controllers []struct {
			Name      string      `json:"name"`
			Aggregate sim.Metrics `json:"aggregate"`
			Seeds     []struct {
				Seed    int64       `json:"seed"`
				Metrics sim.Metrics `json:"metrics"`
			} `json:"seeds"`
		} `json:"controllers"`
		P99Note string `json:"p99_note"`
	}
	decode(t, rec, &r)
	if len(r.Controllers) != len(sim.ControllerNames) {
		t.Fatalf("%d controllers", len(r.Controllers))
	}
	if r.P99Note == "" {
		t.Error("the p99 note is missing")
	}
	g, cfg := mustParams(t, smallBody).toSim()
	want, err := sim.Compare(context.Background(), g, cfg, 3, 1)
	if err != nil {
		t.Fatal(err)
	}
	for i, c := range r.Controllers {
		if c.Name != sim.ControllerNames[i] || len(c.Seeds) != 3 {
			t.Errorf("controller %d: %s with %d seeds", i, c.Name, len(c.Seeds))
		}
		if c.Aggregate.ReadyPct != want[i].Aggregate.ReadyPct || c.Aggregate.CostBRL != want[i].Aggregate.CostBRL {
			t.Errorf("%s differs from sim.Compare: %+v vs %+v", c.Name, c.Aggregate, want[i].Aggregate)
		}
		if c.Name == "planner" && c.Aggregate.PlanViolations != 0 {
			t.Errorf("planner violated the limit %d times", c.Aggregate.PlanViolations)
		}
	}
}

func mustParams(t *testing.T, body string) Params {
	t.Helper()
	p := defaultParams()
	if err := json.Unmarshal([]byte(body), &p); err != nil {
		t.Fatal(err)
	}
	return p
}

func TestCompareRejectsBadInput(t *testing.T) {
	cases := []struct{ name, body, field string }{
		{"zero buses", `{"buses":0}`, "buses"},
		{"too many buses", `{"buses":10001}`, "buses"},
		{"zero chargers", `{"chargers":0}`, "chargers"},
		{"negative limit", `{"limit_kw":-1}`, "limit_kw"},
		{"huge limit", `{"limit_kw":1e308}`, "limit_kw"},
		{"unknown profile", `{"profile":"x"}`, "profile"},
		{"zero seeds", `{"seeds":0}`, "seeds"},
		{"too many seeds", `{"seeds":1001}`, "seeds"},
		{"negative reading age", `{"reading_age_min":-3}`, "reading_age_min"},
		{"negative cooldown", `{"swap_back_cooldown_min":-1}`, "swap_back_cooldown_min"},
		{"negative min need", `{"swap_back_min_need_kwh":-1}`, "swap_back_min_need_kwh"},
		{"work cap", `{"buses":600,"seeds":40}`, "seeds"},
		{"wrong type", `{"buses":"many"}`, ""},
		{"unknown field", `{"nonsense":1}`, ""},
		{"malformed", `{`, ""},
		{"trailing data", `{} {}`, ""},
	}
	s := New()
	for _, tc := range cases {
		rec := do(s, newReq("POST", "/api/compare", tc.body))
		if rec.Code != 400 {
			t.Errorf("%s: status %d, want 400 (%s)", tc.name, rec.Code, rec.Body)
			continue
		}
		var e struct{ Error, Field string }
		decode(t, rec, &e)
		if e.Error == "" {
			t.Errorf("%s: empty error message", tc.name)
		}
		if tc.field != "" && e.Field != tc.field {
			t.Errorf("%s: field %q, want %q", tc.name, e.Field, tc.field)
		}
		if !strings.HasPrefix(rec.Header().Get("Content-Type"), "application/json") {
			t.Errorf("%s: content type %q", tc.name, rec.Header().Get("Content-Type"))
		}
	}
}

func TestBodyTooLarge(t *testing.T) {
	body := `{"buses":10` + strings.Repeat(" ", 70<<10) + `}`
	rec := do(New(), newReq("POST", "/api/compare", body))
	if rec.Code != http.StatusRequestEntityTooLarge {
		t.Fatalf("status %d, want 413", rec.Code)
	}
}

const runBody = `{"buses":10,"chargers":5,"limit_kw":500,"profile":"mild","seed":2,"controller":"planner"}`

type runResp struct {
	Controller string      `json:"controller"`
	Seed       int64       `json:"seed"`
	Metrics    sim.Metrics `json:"metrics"`
	Scenario   struct {
		HorizonMin int `json:"horizon_min"`
		Chargers   []struct {
			ID string `json:"id"`
		} `json:"chargers"`
		Faults []struct {
			To int `json:"to"`
		} `json:"faults"`
	} `json:"scenario"`
	Series struct {
		LimitKW    []float64 `json:"limit_kw"`
		PhysicalKW []float64 `json:"physical_kw"`
		Layer      []string  `json:"layer"`
		Chargers   []struct {
			ID         string    `json:"id"`
			Status     []int     `json:"status"`
			PhysicalKW []float64 `json:"physical_kw"`
		} `json:"chargers"`
		Buses []struct {
			ID      string `json:"id"`
			State   []int  `json:"state"`
			Charger []int  `json:"charger"`
		} `json:"buses"`
	} `json:"series"`
	Reasons   []string `json:"reasons"`
	Decisions []struct {
		Minute int `json:"minute"`
		Buses  []struct {
			B string `json:"b"`
			R int    `json:"r"`
		} `json:"buses"`
	} `json:"decisions"`
	Outcomes []struct {
		ID string `json:"id"`
	} `json:"outcomes"`
}

func TestRunShape(t *testing.T) {
	rec := do(New(), newReq("POST", "/api/run", runBody))
	if rec.Code != 200 {
		t.Fatalf("status %d: %s", rec.Code, rec.Body)
	}
	var r runResp
	decode(t, rec, &r)
	n := r.Scenario.HorizonMin + 1
	if len(r.Scenario.Chargers) != 5 || len(r.Series.Chargers) != 5 || len(r.Series.Buses) != 10 || len(r.Outcomes) != 10 {
		t.Fatalf("%d/%d chargers, %d buses, %d outcomes", len(r.Scenario.Chargers), len(r.Series.Chargers), len(r.Series.Buses), len(r.Outcomes))
	}
	if len(r.Series.LimitKW) != n || len(r.Series.PhysicalKW) != n || len(r.Series.Layer) != n {
		t.Errorf("series lengths %d/%d/%d, want %d", len(r.Series.LimitKW), len(r.Series.PhysicalKW), len(r.Series.Layer), n)
	}
	for _, c := range r.Series.Chargers {
		if len(c.Status) != n || len(c.PhysicalKW) != n {
			t.Errorf("charger %s series length", c.ID)
		}
	}
	for _, b := range r.Series.Buses {
		if len(b.State) != n || len(b.Charger) != n {
			t.Errorf("bus %s series length", b.ID)
		}
		for _, ci := range b.Charger {
			if ci < -1 || ci >= 5 {
				t.Fatalf("bus %s charger index %d out of range", b.ID, ci)
			}
		}
	}
	for _, f := range r.Scenario.Faults {
		if f.To > r.Scenario.HorizonMin {
			t.Errorf("fault window ends at %d, past the horizon %d", f.To, r.Scenario.HorizonMin)
		}
	}
	if len(r.Decisions) == 0 || r.Decisions[0].Minute != 0 {
		t.Fatalf("decisions: %+v", r.Decisions)
	}
	withReason := 0
	for _, d := range r.Decisions {
		for _, b := range d.Buses {
			if b.R < -1 || b.R >= len(r.Reasons) {
				t.Fatalf("reason index %d out of range (%d reasons)", b.R, len(r.Reasons))
			}
			if b.R >= 0 {
				withReason++
			}
		}
	}
	if withReason == 0 {
		t.Error("the planner explains nothing: no bus has a reason")
	}
	if r.Metrics.PlanViolations != 0 {
		t.Errorf("plan violations %d", r.Metrics.PlanViolations)
	}
}

func TestRunBaselineDoesNotExplain(t *testing.T) {
	body := strings.Replace(runBody, `"planner"`, `"fifo"`, 1)
	rec := do(New(), newReq("POST", "/api/run", body))
	if rec.Code != 200 {
		t.Fatalf("status %d: %s", rec.Code, rec.Body)
	}
	var r runResp
	decode(t, rec, &r)
	for _, d := range r.Decisions {
		if len(d.Buses) != 0 {
			t.Fatalf("fifo has no per-bus explanation, got %+v", d.Buses)
		}
	}
}

func TestRunIsDeterministic(t *testing.T) {
	s := New()
	a := do(s, newReq("POST", "/api/run", runBody)).Body.Bytes()
	b := do(s, newReq("POST", "/api/run", runBody)).Body.Bytes()
	if !bytes.Equal(a, b) {
		t.Fatal("the same request gave different bytes")
	}
}

func TestRunSizeUnder2MB(t *testing.T) {
	body := `{"profile":"severe","seed":1,"controller":"planner"}` // defaults: 50 buses, 25 chargers
	rec := do(New(), newReq("POST", "/api/run", body))
	if rec.Code != 200 {
		t.Fatalf("status %d: %.200s", rec.Code, rec.Body)
	}
	t.Logf("run response: %d bytes", rec.Body.Len())
	if rec.Body.Len() > 2<<20 {
		t.Fatalf("response is %d bytes, want under 2 MB", rec.Body.Len())
	}
}

func TestRunRejectsBadInput(t *testing.T) {
	cases := []struct{ name, body, field string }{
		{"unknown controller", `{"controller":"nope"}`, "controller"},
		{"seed zero", `{"seed":0}`, "seed"},
		{"seed too big", `{"seed":1001}`, "seed"},
		{"too many buses for a run", `{"buses":1001}`, "buses"},
		{"bad profile", `{"profile":"x"}`, "profile"},
	}
	s := New()
	for _, tc := range cases {
		rec := do(s, newReq("POST", "/api/run", tc.body))
		var e struct{ Error, Field string }
		decode(t, rec, &e)
		if rec.Code != 400 || e.Field != tc.field {
			t.Errorf("%s: status %d field %q (%s), want 400 %q", tc.name, rec.Code, e.Field, e.Error, tc.field)
		}
	}
}

func TestHostIsChecked(t *testing.T) {
	s := New()
	for _, host := range []string{"evil.example.com", "evil.example.com:8080", "127.0.0.1.evil.com", "192.168.0.10:8080", ""} {
		req := newReq("GET", "/api/defaults", "")
		req.Host = host
		if rec := do(s, req); rec.Code != http.StatusForbidden {
			t.Errorf("host %q: status %d, want 403", host, rec.Code)
		}
		req = newReq("GET", "/", "")
		req.Host = host
		if rec := do(s, req); rec.Code != http.StatusForbidden {
			t.Errorf("host %q on /: status %d, want 403", host, rec.Code)
		}
	}
	for _, host := range []string{"127.0.0.1", "127.0.0.1:8080", "localhost", "localhost:9000", "[::1]:8080", "[::1]"} {
		req := newReq("GET", "/api/defaults", "")
		req.Host = host
		if rec := do(s, req); rec.Code != 200 {
			t.Errorf("host %q: status %d, want 200", host, rec.Code)
		}
	}
}

func TestOriginIsChecked(t *testing.T) {
	s := New()
	req := newReq("POST", "/api/compare", smallBody)
	req.Header.Set("Origin", "http://evil.example.com")
	if rec := do(s, req); rec.Code != http.StatusForbidden {
		t.Errorf("foreign origin: status %d, want 403", rec.Code)
	}
	req = newReq("POST", "/api/compare", smallBody)
	req.Header.Set("Origin", "http://127.0.0.1:8080")
	if rec := do(s, req); rec.Code != 200 {
		t.Errorf("local origin: status %d, want 200", rec.Code)
	}
}

func TestMethodAndContentType(t *testing.T) {
	s := New()
	if rec := do(s, newReq("GET", "/api/compare", "")); rec.Code != http.StatusMethodNotAllowed {
		t.Errorf("GET compare: %d", rec.Code)
	}
	if rec := do(s, newReq("POST", "/api/defaults", "{}")); rec.Code != http.StatusMethodNotAllowed {
		t.Errorf("POST defaults: %d", rec.Code)
	}
	for _, ct := range []string{"", "text/plain", "application/x-www-form-urlencoded"} {
		req := newReq("POST", "/api/compare", smallBody)
		req.Header.Del("Content-Type")
		if ct != "" {
			req.Header.Set("Content-Type", ct)
		}
		if rec := do(s, req); rec.Code != http.StatusUnsupportedMediaType {
			t.Errorf("content type %q: %d, want 415", ct, rec.Code)
		}
	}
}

func TestBusyServerAnswers503(t *testing.T) {
	s := New()
	for i := 0; i < cap(s.sem); i++ {
		s.sem <- struct{}{}
	}
	rec := do(s, newReq("POST", "/api/compare", smallBody))
	if rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("status %d, want 503", rec.Code)
	}
	for i := 0; i < cap(s.sem); i++ {
		<-s.sem
	}
	if rec := do(s, newReq("POST", "/api/compare", smallBody)); rec.Code != 200 {
		t.Fatalf("after releasing the slots: %d", rec.Code)
	}
}

func TestTimeoutAnswers504(t *testing.T) {
	s := New()
	s.timeout = time.Nanosecond
	rec := do(s, newReq("POST", "/api/compare", smallBody))
	if rec.Code != http.StatusGatewayTimeout {
		t.Fatalf("status %d, want 504: %s", rec.Code, rec.Body)
	}
}

func TestStaticIndexIsServed(t *testing.T) {
	rec := do(New(), newReq("GET", "/", ""))
	if rec.Code != 200 || !strings.Contains(rec.Body.String(), "<title>") {
		t.Fatalf("status %d: %.200s", rec.Code, rec.Body)
	}
	if rec.Header().Get("Cache-Control") != "no-store" {
		t.Errorf("cache control %q", rec.Header().Get("Cache-Control"))
	}
}

var (
	htmlRef = regexp.MustCompile(`(?:src|href)="([^"#?]+)"`)
	jsImp   = regexp.MustCompile(`(?:from|import)\s*\(?\s*["'](\.{1,2}/[^"']+)["']`)
)

// Every file the page references (and every relative JS import) must be embedded.
func TestEmbeddedAssetsExist(t *testing.T) {
	exists := func(p string) bool { _, err := fs.Stat(web.Static, "static/"+p); return err == nil }
	idx, err := fs.ReadFile(web.Static, "static/index.html")
	if err != nil {
		t.Fatal(err)
	}
	var queue []string
	for _, m := range htmlRef.FindAllStringSubmatch(string(idx), -1) {
		ref := m[1]
		if strings.Contains(ref, "://") || strings.HasPrefix(ref, "/") || strings.HasPrefix(ref, "data:") {
			continue
		}
		if !exists(ref) {
			t.Errorf("index.html references %q, which is not embedded", ref)
		}
		if strings.HasSuffix(ref, ".js") {
			queue = append(queue, ref)
		}
	}
	seen := map[string]bool{}
	for len(queue) > 0 {
		f := queue[0]
		queue = queue[1:]
		if seen[f] {
			continue
		}
		seen[f] = true
		src, err := fs.ReadFile(web.Static, "static/"+f)
		if err != nil {
			t.Errorf("cannot read %s: %v", f, err)
			continue
		}
		for _, m := range jsImp.FindAllStringSubmatch(string(src), -1) {
			target := path.Join(path.Dir(f), m[1])
			if !exists(target) {
				t.Errorf("%s imports %q, which is not embedded", f, m[1])
				continue
			}
			queue = append(queue, target)
		}
	}
}
```

`internal/lab/series_test.go`:

```go
package lab

import (
	"encoding/json"
	"math"
	"testing"

	"github.com/EduardoMilani8/depot-charge-planner/internal/planner"
	"github.com/EduardoMilani8/depot-charge-planner/internal/sim"
)

func TestSeriesJSON(t *testing.T) {
	got, err := json.Marshal(Series{1.04, math.NaN(), math.Inf(1), -0.04, 150, 37.56, 0.1 + 0.2})
	if err != nil {
		t.Fatal(err)
	}
	want := `[1,null,null,0,150,37.6,0.3]`
	if string(got) != want {
		t.Fatalf("got %s, want %s", got, want)
	}
	if b, _ := json.Marshal(Series(nil)); string(b) != "[]" {
		t.Fatalf("nil series: %s", b)
	}
}

func TestConvertReasonTableAndClamp(t *testing.T) {
	p := defaultParams()
	p.Buses, p.Chargers, p.LimitKW, p.Profile = 10, 5, 500, "severe"
	g, cfg := p.toSim()
	sc, ctrl, _ := sim.ForController("planner", cfg, sim.Generate(g, 1))
	tr := sim.NewTrace()
	m := sim.RunTraced(sc, ctrl, nil, tr)
	resp := buildRun(p, 1, "planner", sc, m, tr)
	if len(resp.Decisions) == 0 {
		t.Fatal("no decisions")
	}
	seen := map[string]bool{}
	for _, r := range resp.Reasons {
		if seen[r] {
			t.Fatalf("reason table repeats %q", r)
		}
		seen[r] = true
	}
	for _, f := range resp.Scenario.Faults {
		if f.To > resp.Scenario.HorizonMin {
			t.Fatalf("fault window not clamped: %+v", f)
		}
	}
	// A planner Plan with a non-finite shortfall must still serialize.
	d := decisionToDTO(sim.Decision{Buses: []planner.BusStatus{{BusID: "B1", ShortfallKWh: math.NaN()}}}, func(string) int { return -1 })
	if _, err := json.Marshal(d); err != nil {
		t.Fatalf("a NaN shortfall broke the JSON: %v", err)
	}
}
```

`cmd/lab/main_test.go`:

```go
package main

import (
	"bytes"
	"context"
	"io"
	"net/http"
	"regexp"
	"sync"
	"testing"
	"time"
)

func TestCheckAddr(t *testing.T) {
	for _, ok := range []string{"127.0.0.1:8080", "localhost:9000", "[::1]:8080", "127.0.0.1:0"} {
		if err := checkAddr(ok); err != nil {
			t.Errorf("%q should be accepted: %v", ok, err)
		}
	}
	for _, bad := range []string{"0.0.0.0:8080", ":8080", "192.168.0.10:8080", "example.com:80", "8080", "[::]:8080"} {
		if err := checkAddr(bad); err == nil {
			t.Errorf("%q must be rejected", bad)
		}
	}
}

type syncBuf struct {
	mu sync.Mutex
	b  bytes.Buffer
}

func (s *syncBuf) Write(p []byte) (int, error) { s.mu.Lock(); defer s.mu.Unlock(); return s.b.Write(p) }
func (s *syncBuf) String() string               { s.mu.Lock(); defer s.mu.Unlock(); return s.b.String() }

func TestRunServesAndStops(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	var out, errOut syncBuf
	done := make(chan int, 1)
	go func() { done <- run(ctx, []string{"-addr", "127.0.0.1:0"}, &out, &errOut) }()
	url := regexp.MustCompile(`http://[^\s]+`)
	var base string
	for i := 0; i < 100 && base == ""; i++ {
		base = url.FindString(out.String())
		time.Sleep(20 * time.Millisecond)
	}
	if base == "" {
		cancel()
		t.Fatalf("no URL printed; stderr: %s", errOut.String())
	}
	resp, err := http.Get(base + "/api/defaults")
	if err != nil {
		cancel()
		t.Fatal(err)
	}
	io.Copy(io.Discard, resp.Body)
	resp.Body.Close()
	if resp.StatusCode != 200 {
		t.Errorf("status %d", resp.StatusCode)
	}
	cancel()
	select {
	case code := <-done:
		if code != 0 {
			t.Errorf("exit %d: %s", code, errOut.String())
		}
	case <-time.After(5 * time.Second):
		t.Fatal("the server did not stop after the context was cancelled")
	}
}

func TestRunRejectsBadArguments(t *testing.T) {
	var out, errOut syncBuf
	if code := run(context.Background(), []string{"-addr", "0.0.0.0:8080"}, &out, &errOut); code != 2 {
		t.Errorf("non-loopback address: exit %d", code)
	}
	if code := run(context.Background(), []string{"extra"}, &out, &errOut); code != 2 {
		t.Errorf("positional argument: exit %d", code)
	}
	if code := run(context.Background(), []string{"-nonsense"}, &out, &errOut); code != 2 {
		t.Errorf("unknown flag: exit %d", code)
	}
}
```

- [ ] **Step 2: Rodar e ver falhar**

Run: `go test ./internal/lab ./cmd/lab -count=1`
Expected: FAIL (pacotes inexistentes / `undefined: New`, `Series`, `run`, ...).

- [ ] **Step 3: `web/embed.go` e o `index.html` provisório**

`web/embed.go`:

```go
// Package web embeds the lab's static files.
package web

import "embed"

// Static holds web/static: index.html, app.css and the JavaScript modules.
//
//go:embed static
var Static embed.FS
```

`web/static/index.html` (provisório):

```html
<!doctype html>
<html lang="pt-BR">
<head>
<meta charset="utf-8">
<meta name="viewport" content="width=device-width, initial-scale=1">
<title>Laboratório de recarga</title>
</head>
<body>
<h1>Laboratório de recarga</h1>
<p>Interface em construção.</p>
</body>
</html>
```

- [ ] **Step 4: `internal/lab/series.go`**

```go
package lab

import (
	"math"
	"strconv"
)

// Series is a per-minute number series. It is written with one decimal and with null
// for NaN/Inf (no reading, bus not present), which encoding/json cannot do on its own.
type Series []float64

func (s Series) MarshalJSON() ([]byte, error) {
	b := make([]byte, 0, len(s)*6+2)
	b = append(b, '[')
	for i, v := range s {
		if i > 0 {
			b = append(b, ',')
		}
		if math.IsNaN(v) || math.IsInf(v, 0) {
			b = append(b, "null"...)
			continue
		}
		r := math.Round(v*10) / 10
		if r == 0 {
			r = 0 // turns -0 into 0
		}
		b = strconv.AppendFloat(b, r, 'f', -1, 64)
	}
	return append(b, ']'), nil
}
```

- [ ] **Step 5: `internal/lab/params.go`**

```go
package lab

import (
	"errors"
	"fmt"

	"github.com/EduardoMilani8/depot-charge-planner/internal/planner"
	"github.com/EduardoMilani8/depot-charge-planner/internal/sim"
)

// Work limits of the lab (on top of sim.Max*): a request must stay interactive.
const (
	labMaxWork     = 20000 // buses x seeds in /api/compare
	labMaxRunBuses = 1000  // buses in /api/run (the response carries every series)
)

// Params is the scenario form. Missing JSON fields keep the default.
type Params struct {
	Buses               int     `json:"buses"`
	Chargers            int     `json:"chargers"`
	LimitKW             float64 `json:"limit_kw"`
	Profile             string  `json:"profile"`
	Seeds               int     `json:"seeds"`
	FollowSwaps         bool    `json:"follow_swaps"`
	ReadingAgeMin       int     `json:"reading_age_min"`
	SwapBackCooldownMin int     `json:"swap_back_cooldown_min"`
	SwapBackMinNeedKWh  float64 `json:"swap_back_min_need_kwh"`
}

func defaultParams() Params {
	g, c := sim.DefaultGenParams(), planner.DefaultConfig()
	return Params{
		Buses: g.NumBuses, Chargers: g.NumChargers, LimitKW: g.LimitKW, Profile: string(g.Profile),
		Seeds: 20, FollowSwaps: g.FollowSwaps, ReadingAgeMin: g.ReadingAgeMin,
		SwapBackCooldownMin: c.SwapBackCooldownMin, SwapBackMinNeedKWh: c.SwapBackMinNeedKWh,
	}
}

func (p Params) toSim() (sim.GenParams, planner.Config) {
	g := sim.DefaultGenParams()
	g.NumBuses, g.NumChargers, g.LimitKW = p.Buses, p.Chargers, p.LimitKW
	g.Profile = sim.FaultProfile(p.Profile)
	g.FollowSwaps, g.ReadingAgeMin = p.FollowSwaps, p.ReadingAgeMin
	cfg := planner.DefaultConfig()
	cfg.SwapBackCooldownMin, cfg.SwapBackMinNeedKWh = p.SwapBackCooldownMin, p.SwapBackMinNeedKWh
	return g, cfg
}

// apiError is the JSON error body.
type apiError struct {
	status  int
	Message string `json:"error"`
	Field   string `json:"field,omitempty"`
}

// jsonField maps sim.FieldError names (simrun flag names) to the API's JSON keys.
var jsonField = map[string]string{
	"profile": "profile", "seeds": "seeds", "buses": "buses", "chargers": "chargers",
	"limit": "limit_kw", "reading-age": "reading_age_min",
	"swap-back-cooldown": "swap_back_cooldown_min", "swap-back-min-need": "swap_back_min_need_kwh",
}

func ptMessage(field string) string {
	switch field {
	case "profile":
		return "Perfil de falhas inválido: use nenhuma, leve, severa ou aleatória."
	case "seeds":
		return fmt.Sprintf("Sementes: informe um valor entre 1 e %d.", sim.MaxSeeds)
	case "buses":
		return fmt.Sprintf("Ônibus: informe um valor entre 1 e %d.", sim.MaxBuses)
	case "chargers":
		return fmt.Sprintf("Carregadores: informe um valor entre 1 e %d.", sim.MaxChargers)
	case "limit":
		return fmt.Sprintf("Limite da garagem: informe um valor maior que 0 e até %g kW.", float64(sim.MaxLimitKW))
	case "reading-age":
		return fmt.Sprintf("Idade da leitura: informe de 0 a %d minutos.", sim.MaxReadingAgeMin)
	case "swap-back-cooldown":
		return "Espera para voltar a um carregador: informe 0 ou mais minutos."
	case "swap-back-min-need":
		return fmt.Sprintf("Necessidade mínima para voltar: informe de 0 a %g kWh.", float64(sim.MaxLimitKW))
	}
	return "Parâmetro inválido."
}

// validate checks the generator and planner parameters (not the lab's work limits).
func (p Params) validate() *apiError {
	g, cfg := p.toSim()
	err := sim.ValidateParams(g, cfg, p.Seeds)
	if err == nil {
		return nil
	}
	var fe *sim.FieldError
	if !errors.As(err, &fe) {
		return &apiError{status: 400, Message: "Parâmetros inválidos."}
	}
	return &apiError{status: 400, Message: ptMessage(fe.Field), Field: jsonField[fe.Field]}
}
```

- [ ] **Step 6: `internal/lab/server.go`**

```go
// Package lab is the HTTP server of the simulation laboratory: three JSON routes plus
// the embedded web interface. It holds no planning logic.
package lab

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"io/fs"
	"mime"
	"net"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/EduardoMilani8/depot-charge-planner/web"
)

const (
	maxBody       = 64 << 10
	maxConcurrent = 2
)

type Server struct {
	sem     chan struct{} // one token per simulation in progress
	static  http.Handler
	timeout time.Duration
}

func New() *Server {
	sub, err := fs.Sub(web.Static, "static")
	if err != nil {
		panic(err) // the directory is embedded at build time
	}
	return &Server{
		sem:     make(chan struct{}, maxConcurrent),
		static:  http.FileServer(http.FS(sub)),
		timeout: 60 * time.Second,
	}
}

func (s *Server) Handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("/api/defaults", s.route(http.MethodGet, s.defaults))
	mux.HandleFunc("/api/compare", s.route(http.MethodPost, s.compare))
	mux.HandleFunc("/api/run", s.route(http.MethodPost, s.run))
	mux.Handle("/", s.static)
	return guard(mux)
}

// isLocalHost accepts 127.0.0.1, localhost and ::1 with or without a port.
func isLocalHost(hostport string) bool {
	host := hostport
	if h, _, err := net.SplitHostPort(hostport); err == nil {
		host = h
	}
	host = strings.Trim(host, "[]")
	if host == "localhost" {
		return true
	}
	ip := net.ParseIP(host)
	return ip != nil && ip.IsLoopback()
}

// guard rejects requests for a non-local Host (DNS rebinding) or from a foreign Origin,
// and marks every response as uncacheable.
func guard(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !isLocalHost(r.Host) {
			writeError(w, &apiError{status: http.StatusForbidden, Message: "Acesso negado: o laboratório só atende endereços locais."})
			return
		}
		if o := r.Header.Get("Origin"); o != "" {
			if u, err := url.Parse(o); err != nil || !isLocalHost(u.Host) {
				writeError(w, &apiError{status: http.StatusForbidden, Message: "Acesso negado: origem não permitida."})
				return
			}
		}
		w.Header().Set("Cache-Control", "no-store")
		w.Header().Set("X-Content-Type-Options", "nosniff")
		next.ServeHTTP(w, r)
	})
}

type handlerFunc func(r *http.Request) (any, *apiError)

// route wraps a JSON handler: method, content type, body limit, response encoding.
func (s *Server) route(method string, h handlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != method {
			w.Header().Set("Allow", method)
			writeError(w, &apiError{status: http.StatusMethodNotAllowed, Message: "Método não permitido."})
			return
		}
		if method == http.MethodPost {
			mt, _, err := mime.ParseMediaType(r.Header.Get("Content-Type"))
			if err != nil || mt != "application/json" {
				writeError(w, &apiError{status: http.StatusUnsupportedMediaType, Message: "Envie o corpo como application/json."})
				return
			}
			r.Body = http.MaxBytesReader(w, r.Body, maxBody)
		}
		body, aerr := h(r)
		if aerr != nil {
			writeError(w, aerr)
			return
		}
		writeJSON(w, http.StatusOK, body)
	}
}

// decodeBody reads a JSON object into v (an empty body leaves v unchanged). Unknown
// fields, wrong types, trailing data and oversized bodies are errors.
func decodeBody(r *http.Request, v any) *apiError {
	dec := json.NewDecoder(r.Body)
	dec.DisallowUnknownFields()
	if err := dec.Decode(v); err != nil {
		if errors.Is(err, io.EOF) {
			return nil
		}
		var tooBig *http.MaxBytesError
		if errors.As(err, &tooBig) {
			return &apiError{status: http.StatusRequestEntityTooLarge, Message: "Corpo da requisição grande demais (limite de 64 KB)."}
		}
		return &apiError{status: http.StatusBadRequest, Message: "JSON inválido: " + err.Error()}
	}
	if dec.More() {
		return &apiError{status: http.StatusBadRequest, Message: "JSON inválido: há dados depois do objeto."}
	}
	return nil
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	var buf bytes.Buffer
	if err := json.NewEncoder(&buf).Encode(v); err != nil {
		writeError(w, &apiError{status: http.StatusInternalServerError, Message: "Erro interno ao montar a resposta."})
		return
	}
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	w.Write(buf.Bytes())
}

func writeError(w http.ResponseWriter, e *apiError) {
	writeJSON(w, e.status, e)
}

var errBusy = &apiError{status: http.StatusServiceUnavailable,
	Message: "Servidor ocupado: já há simulações em andamento. Tente de novo em instantes."}

// acquire reserves a simulation slot without waiting.
func (s *Server) acquire() (release func(), ok bool) {
	select {
	case s.sem <- struct{}{}:
		return func() { <-s.sem }, true
	default:
		return nil, false
	}
}
```

- [ ] **Step 7: `internal/lab/handlers.go`**

```go
package lab

import (
	"context"
	"errors"
	"net/http"
	"runtime"

	"github.com/EduardoMilani8/depot-charge-planner/internal/sim"
)

type presetDTO struct {
	ID          string `json:"id"`
	Label       string `json:"label"`
	Description string `json:"description"`
	Params      Params `json:"params"`
}

type limitsDTO struct {
	MaxBuses         int     `json:"max_buses"`
	MaxChargers      int     `json:"max_chargers"`
	MaxSeeds         int     `json:"max_seeds"`
	MaxLimitKW       float64 `json:"max_limit_kw"`
	MaxReadingAgeMin int     `json:"max_reading_age_min"`
	LabMaxWork       int     `json:"lab_max_work"`
	LabMaxRunBuses   int     `json:"lab_max_run_buses"`
}

type defaultsResponse struct {
	Params      Params      `json:"params"`
	Limits      limitsDTO   `json:"limits"`
	Presets     []presetDTO `json:"presets"`
	Controllers []string    `json:"controllers"`
	Profiles    []string    `json:"profiles"`
}

func presets() []presetDTO {
	d := defaultParams()
	tight, severe, aged := d, d, d
	tight.LimitKW, tight.Profile = 600, "mild"
	severe.Profile = "severe"
	aged.LimitKW, aged.Profile, aged.ReadingAgeMin = 1200, "severe", 1
	return []presetDTO{
		{"default", "Padrão", "50 ônibus, 25 carregadores, 2000 kW, sem falhas.", d},
		{"tight", "Rede apertada (600 kW)", "Pouca potência para muitos ônibus, com falhas leves.", tight},
		{"severe", "Falhas severas", "Carregadores caindo, leituras ruins e queda de rede.", severe},
		{"aged", "Leituras atrasadas", "Gateway que informa leituras com 1 minuto de idade, 1200 kW e falhas severas.", aged},
	}
}

func (s *Server) defaults(r *http.Request) (any, *apiError) {
	return defaultsResponse{
		Params: defaultParams(),
		Limits: limitsDTO{
			MaxBuses: sim.MaxBuses, MaxChargers: sim.MaxChargers, MaxSeeds: sim.MaxSeeds,
			MaxLimitKW: sim.MaxLimitKW, MaxReadingAgeMin: sim.MaxReadingAgeMin,
			LabMaxWork: labMaxWork, LabMaxRunBuses: labMaxRunBuses,
		},
		Presets:     presets(),
		Controllers: sim.ControllerNames,
		Profiles:    []string{"none", "mild", "severe", "random"},
	}, nil
}

type seedDTO struct {
	Seed    int64       `json:"seed"`
	Metrics sim.Metrics `json:"metrics"`
}

type controllerDTO struct {
	Name      string      `json:"name"`
	Aggregate sim.Metrics `json:"aggregate"`
	Seeds     []seedDTO   `json:"seeds"`
}

type compareResponse struct {
	Params      Params          `json:"params"`
	Controllers []controllerDTO `json:"controllers"`
	P99Note     string          `json:"p99_note"`
}

func (s *Server) compare(r *http.Request) (any, *apiError) {
	p := defaultParams()
	if e := decodeBody(r, &p); e != nil {
		return nil, e
	}
	if e := p.validate(); e != nil {
		return nil, e
	}
	if p.Buses*p.Seeds > labMaxWork {
		return nil, &apiError{status: http.StatusBadRequest, Field: "seeds",
			Message: "Trabalho demais para uma tela: ônibus × sementes deve ser no máximo 20000. Reduza as sementes ou os ônibus."}
	}
	release, ok := s.acquire()
	if !ok {
		return nil, errBusy
	}
	defer release()
	ctx, cancel := context.WithTimeout(r.Context(), s.timeout)
	defer cancel()
	g, cfg := p.toSim()
	res, err := sim.Compare(ctx, g, cfg, p.Seeds, runtime.NumCPU())
	if err != nil {
		if errors.Is(err, context.DeadlineExceeded) {
			return nil, &apiError{status: http.StatusGatewayTimeout, Message: "Tempo esgotado: reduza as sementes ou o número de ônibus."}
		}
		return nil, &apiError{status: http.StatusRequestTimeout, Message: "Requisição cancelada."}
	}
	out := compareResponse{
		Params:  p,
		P99Note: "O p99 do planejador é tempo real medido com várias simulações em paralelo: use como ordem de grandeza (para um valor preciso use go run ./cmd/simrun).",
	}
	for _, c := range res {
		cd := controllerDTO{Name: c.Name, Aggregate: c.Aggregate}
		for _, sr := range c.Seeds {
			cd.Seeds = append(cd.Seeds, seedDTO{Seed: sr.Seed, Metrics: sr.Metrics})
		}
		out.Controllers = append(out.Controllers, cd)
	}
	return out, nil
}

type runRequest struct {
	Params
	Seed       int64  `json:"seed"`
	Controller string `json:"controller"`
}

func (s *Server) run(r *http.Request) (any, *apiError) {
	req := runRequest{Params: defaultParams(), Seed: 1, Controller: "planner"}
	if e := decodeBody(r, &req); e != nil {
		return nil, e
	}
	p := req.Params
	if e := p.validate(); e != nil {
		return nil, e
	}
	if req.Seed < 1 || req.Seed > sim.MaxSeeds {
		return nil, &apiError{status: http.StatusBadRequest, Field: "seed", Message: "Semente: informe um valor entre 1 e 1000."}
	}
	known := false
	for _, n := range sim.ControllerNames {
		known = known || n == req.Controller
	}
	if !known {
		return nil, &apiError{status: http.StatusBadRequest, Field: "controller", Message: "Controlador desconhecido."}
	}
	if p.Buses > labMaxRunBuses {
		return nil, &apiError{status: http.StatusBadRequest, Field: "buses",
			Message: "Para abrir uma execução detalhada use no máximo 1000 ônibus."}
	}
	release, ok := s.acquire()
	if !ok {
		return nil, errBusy
	}
	defer release()
	g, cfg := p.toSim()
	sc, ctrl, _ := sim.ForController(req.Controller, cfg, sim.Generate(g, req.Seed))
	tr := sim.NewTrace()
	m := sim.RunTraced(sc, ctrl, nil, tr)
	return buildRun(p, req.Seed, req.Controller, sc, m, tr), nil
}
```

- [ ] **Step 8: `internal/lab/convert.go`**

```go
package lab

import (
	"math"
	"sort"

	"github.com/EduardoMilani8/depot-charge-planner/internal/sim"
)

type chargerDTO struct {
	ID    string  `json:"id"`
	MaxKW float64 `json:"max_kw"`
	MinKW float64 `json:"min_kw"`
}

type faultDTO struct {
	Kind   string  `json:"kind"`
	Target string  `json:"target"`
	From   int     `json:"from"`
	To     int     `json:"to"`
	Value  float64 `json:"value"`
}

type scenarioDTO struct {
	HorizonMin    int          `json:"horizon_min"`
	StartClockMin int          `json:"start_clock_min"`
	BaseLimitKW   float64      `json:"base_limit_kw"`
	Chargers      []chargerDTO `json:"chargers"`
	Faults        []faultDTO   `json:"faults"`
}

type chargerSeriesDTO struct {
	ID          string `json:"id"`
	Status      []int  `json:"status"`
	CommandedKW Series `json:"commanded_kw"`
	PhysicalKW  Series `json:"physical_kw"`
}

type busSeriesDTO struct {
	ID          string `json:"id"`
	State       []int  `json:"state"`
	TrueSoCKWh  Series `json:"true_soc_kwh"`
	ObservedKWh Series `json:"observed_kwh"`
	Charger     []int  `json:"charger"` // index into scenario.chargers, -1 = none
}

type seriesDTO struct {
	LimitKW     Series             `json:"limit_kw"`
	CommandedKW Series             `json:"commanded_kw"`
	PhysicalKW  Series             `json:"physical_kw"`
	Layer       []string           `json:"layer"`
	Chargers    []chargerSeriesDTO `json:"chargers"`
	Buses       []busSeriesDTO     `json:"buses"`
}

type setpointDTO struct {
	Charger string  `json:"c"`
	KW      float64 `json:"kw"`
}

type busStatusDTO struct {
	Bus          string  `json:"b"`
	WillReach    bool    `json:"ok"`
	Assessed     bool    `json:"as"`
	ShortfallKWh float64 `json:"sf"`
	Reason       int     `json:"r"` // index into reasons, -1 = none
}

type swapDTO struct {
	Charger string `json:"charger"`
	Out     string `json:"out"`
	In      string `json:"in"`
	Reason  string `json:"reason"`
}

type decisionDTO struct {
	Minute    int            `json:"minute"`
	Layer     string         `json:"layer"`
	Setpoints []setpointDTO  `json:"setpoints"`
	Buses     []busStatusDTO `json:"buses"`
	Swaps     []swapDTO      `json:"swaps"`
	Notes     []string       `json:"notes"`
}

type outcomeDTO struct {
	ID                string  `json:"id"`
	Arrival           int     `json:"arrival"`
	Departure         int     `json:"departure"`
	CapacityKWh       float64 `json:"capacity_kwh"`
	InitialSoCKWh     float64 `json:"initial_soc_kwh"`
	ForecastTargetKWh float64 `json:"forecast_target_kwh"`
	TrueTargetKWh     float64 `json:"true_target_kwh"`
	FinalSoCKWh       float64 `json:"final_soc_kwh"`
	Departed          bool    `json:"departed"`
	Ready             bool    `json:"ready"`
	ShortfallKWh      float64 `json:"shortfall_kwh"`
}

type runResponse struct {
	Params     Params        `json:"params"`
	Seed       int64         `json:"seed"`
	Controller string        `json:"controller"`
	Metrics    sim.Metrics   `json:"metrics"`
	Scenario   scenarioDTO   `json:"scenario"`
	Series     seriesDTO     `json:"series"`
	Reasons    []string      `json:"reasons"`
	Decisions  []decisionDTO `json:"decisions"`
	Outcomes   []outcomeDTO  `json:"outcomes"`
}

func sortChargerDTOs(cs []chargerDTO) {
	sort.Slice(cs, func(i, j int) bool { return cs[i].ID < cs[j].ID })
}

// finite keeps NaN/Inf (which JSON cannot carry) out of the response.
func finite(v float64) float64 {
	if math.IsNaN(v) || math.IsInf(v, 0) {
		return 0
	}
	return v
}

// decisionToDTO converts one decision; reasonIdx interns reason texts.
func decisionToDTO(d sim.Decision, reasonIdx func(string) int) decisionDTO {
	out := decisionDTO{Minute: d.Minute, Layer: d.Layer, Notes: d.Notes,
		Setpoints: []setpointDTO{}, Buses: []busStatusDTO{}, Swaps: []swapDTO{}}
	if out.Notes == nil {
		out.Notes = []string{}
	}
	for _, s := range d.Setpoints {
		out.Setpoints = append(out.Setpoints, setpointDTO{Charger: s.ChargerID, KW: math.Round(finite(s.KW)*10) / 10})
	}
	for _, b := range d.Buses {
		out.Buses = append(out.Buses, busStatusDTO{
			Bus: b.BusID, WillReach: b.WillReachTarget, Assessed: b.Assessed,
			ShortfallKWh: math.Round(finite(b.ShortfallKWh)*10) / 10, Reason: reasonIdx(b.Reason),
		})
	}
	for _, s := range d.Swaps {
		out.Swaps = append(out.Swaps, swapDTO{Charger: s.ChargerID, Out: s.OutBusID, In: s.InBusID, Reason: s.Reason})
	}
	return out
}

// buildRun converts a traced run into the /api/run response.
func buildRun(p Params, seed int64, controller string, sc sim.Scenario, m sim.Metrics, tr *sim.Trace) runResponse {
	resp := runResponse{Params: p, Seed: seed, Controller: controller, Metrics: m}
	resp.Scenario = scenarioDTO{HorizonMin: sc.Horizon, StartClockMin: sc.StartClockMin, BaseLimitKW: sc.BaseLimitKW,
		Chargers: []chargerDTO{}, Faults: []faultDTO{}}
	chargerIdx := map[string]int{}
	for i, c := range tr.Chargers {
		chargerIdx[c.ID] = i
	}
	for _, c := range sc.Chargers {
		resp.Scenario.Chargers = append(resp.Scenario.Chargers, chargerDTO{ID: c.ID, MaxKW: c.MaxKW, MinKW: c.MinKW})
	}
	sortChargerDTOs(resp.Scenario.Chargers)
	for _, f := range sc.Faults {
		to := f.To
		if to > sc.Horizon {
			to = sc.Horizon
		}
		resp.Scenario.Faults = append(resp.Scenario.Faults, faultDTO{Kind: string(f.Kind), Target: f.Target, From: f.From, To: to, Value: finite(f.Value)})
	}
	resp.Series = seriesDTO{
		LimitKW: tr.Limit, CommandedKW: tr.Commanded, PhysicalKW: tr.Physical, Layer: tr.Layer,
		Chargers: []chargerSeriesDTO{}, Buses: []busSeriesDTO{},
	}
	for _, c := range tr.Chargers {
		resp.Series.Chargers = append(resp.Series.Chargers, chargerSeriesDTO{ID: c.ID, Status: c.Status, CommandedKW: c.CommandedKW, PhysicalKW: c.PhysicalKW})
	}
	for _, b := range tr.Buses {
		idx := make([]int, len(b.ChargerID))
		for i, id := range b.ChargerID {
			if j, ok := chargerIdx[id]; ok && id != "" {
				idx[i] = j
			} else {
				idx[i] = -1
			}
		}
		resp.Series.Buses = append(resp.Series.Buses, busSeriesDTO{ID: b.ID, State: b.State, TrueSoCKWh: b.TrueSoC, ObservedKWh: b.Observed, Charger: idx})
	}

	resp.Reasons = []string{}
	interned := map[string]int{}
	reasonIdx := func(s string) int {
		if s == "" {
			return -1
		}
		if i, ok := interned[s]; ok {
			return i
		}
		interned[s] = len(resp.Reasons)
		resp.Reasons = append(resp.Reasons, s)
		return interned[s]
	}
	resp.Decisions = []decisionDTO{}
	for _, d := range tr.Decisions {
		resp.Decisions = append(resp.Decisions, decisionToDTO(d, reasonIdx))
	}
	resp.Outcomes = []outcomeDTO{}
	for _, o := range tr.Outcomes {
		resp.Outcomes = append(resp.Outcomes, outcomeDTO{
			ID: o.ID, Arrival: o.Arrival, Departure: o.Departure, CapacityKWh: o.CapacityKWh,
			InitialSoCKWh: math.Round(o.InitialSoCKWh*10) / 10, ForecastTargetKWh: math.Round(o.ForecastTargetKWh*10) / 10,
			TrueTargetKWh: math.Round(o.TrueTargetKWh*10) / 10, FinalSoCKWh: math.Round(o.FinalSoCKWh*10) / 10,
			Departed: o.Departed, Ready: o.Ready, ShortfallKWh: math.Round(o.ShortfallKWh*10) / 10,
		})
	}
	return resp
}
```

Nota: `Scenario.Chargers` precisa estar na mesma ordem de `Series.Chargers` (por ID), porque `series.buses[].charger` indexa nela; por isso `sortChargerDTOs` abaixo (o `Trace` já ordena os carregadores por ID).

- [ ] **Step 9: `cmd/lab/main.go`**

```go
// Command lab serves the simulation laboratory (web interface) on a local address.
package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/EduardoMilani8/depot-charge-planner/internal/lab"
)

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	code := run(ctx, os.Args[1:], os.Stdout, os.Stderr)
	stop()
	os.Exit(code)
}

// checkAddr accepts only loopback addresses with an explicit host: the lab has no login.
func checkAddr(addr string) error {
	host, _, err := net.SplitHostPort(addr)
	if err != nil {
		return fmt.Errorf("endereço inválido %q: use host:porta, por exemplo 127.0.0.1:8080", addr)
	}
	if host == "localhost" {
		return nil
	}
	if ip := net.ParseIP(host); ip != nil && ip.IsLoopback() {
		return nil
	}
	return errors.New("o laboratório só escuta em endereço local (127.0.0.1, ::1 ou localhost): ele não tem login")
}

func run(ctx context.Context, args []string, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("lab", flag.ContinueOnError)
	fs.SetOutput(stderr)
	addr := fs.String("addr", "127.0.0.1:8080", "address to listen on (loopback only)")
	if err := fs.Parse(args); err != nil {
		return 2
	}
	if fs.NArg() > 0 {
		fmt.Fprintf(stderr, "unexpected argument %q\n", fs.Arg(0))
		return 2
	}
	if err := checkAddr(*addr); err != nil {
		fmt.Fprintln(stderr, err)
		return 2
	}
	ln, err := net.Listen("tcp", *addr)
	if err != nil {
		fmt.Fprintln(stderr, err)
		return 1
	}
	srv := &http.Server{
		Handler:           lab.New().Handler(),
		ReadHeaderTimeout: 5 * time.Second,
		ReadTimeout:       30 * time.Second,
		WriteTimeout:      120 * time.Second,
	}
	fmt.Fprintf(stdout, "Laboratório em http://%s (Ctrl+C para encerrar)\n", ln.Addr())
	go func() {
		<-ctx.Done()
		sctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
		defer cancel()
		srv.Shutdown(sctx)
	}()
	if err := srv.Serve(ln); err != nil && !errors.Is(err, http.ErrServerClosed) {
		fmt.Fprintln(stderr, err)
		return 1
	}
	return 0
}
```

- [ ] **Step 10: Rodar tudo**

Run: `gofmt -l . ; go vet ./... && go test -race -count=1 ./internal/lab ./cmd/lab ./internal/sim -short`
Expected: PASS. Se `TestRunSizeUnder2MB` falhar, o log mostra o tamanho: **não aumente o limite**; reduza o que domina (por exemplo `notes` por decisão, ou as casas decimais) e registre o motivo no commit. Se `TestStaticIndexIsServed` ou `TestEmbeddedAssetsExist` falharem, confira o `//go:embed static` e o caminho `web/static/index.html`.

- [ ] **Step 11: Rodar de verdade**

Run (em outro terminal ou em segundo plano): `go run ./cmd/lab` e, em seguida:

```bash
curl -s localhost:8080/api/defaults | head -c 400
curl -s -XPOST -H 'Content-Type: application/json' -d '{"buses":10,"chargers":5,"limit_kw":500,"profile":"mild","seeds":3}' localhost:8080/api/compare | head -c 400
curl -s -o /dev/null -w '%{http_code}\n' -H 'Host: evil.example.com' localhost:8080/api/defaults
```

Expected: JSON de padrões; JSON da comparação; `403`. Encerre o servidor com Ctrl+C.

- [ ] **Step 12: Commit**

```bash
git add web internal/lab cmd/lab
git commit -m "feat(lab): local HTTP server with defaults, compare and run routes

Co-Authored-By: Claude Sonnet 5.5 <noreply@anthropic.com>"
git push
```

---

### Task 4: Frontend — base, formulário e aba Comparação

**Files:**
- Create: `web/package.json`, `web/static/app.css`, `web/static/js/main.js`, `web/static/js/dom.js`, `web/static/js/api.js`, `web/static/js/params.js`, `web/static/js/format.js`, `web/static/js/glossary.js`, `web/static/js/form.js`, `web/static/js/compare.js`, `web/static/js/charts/svg.js`, `web/static/js/charts/layout.js`; testes `web/test/params.test.mjs`, `web/test/format.test.mjs`, `web/test/api.test.mjs`, `web/test/layout.test.mjs`
- Modify: `web/static/index.html` (substitui o provisório), `.github/workflows/ci.yml`

**Interfaces:**
- Consumes: a API da Tarefa 3 (`/api/defaults`, `/api/compare`).
- Produces (módulos ES, exportações usadas pelas tarefas 5–7):
  - `dom.js`: `h(tag, attrs, ...children)`, `clear(node)`.
  - `charts/svg.js`: `s(tag, attrs, ...children)` (cria elementos SVG), `NS`.
  - `params.js`: `TABS`, `CONTROLLERS`, `coerce(kind, raw, fallback)`, `stateToHash(state)`, `hashToState(hash, defaults)`, `fieldSpecs(limits)`. `state = { params, tab, seed, controller }`.
  - `format.js`: `fmtNum(v, digits=1)`, `fmtPct(v)`, `fmtBRL(v)`, `clock(startClockMin, minute)`, `duration(min)`.
  - `api.js`: `ApiError(message, field, status)`, `getDefaults(signal)`, `compare(params, signal)`, `run(req, signal)`, `createLatest()` (função `latest(fn)` que devolve `{stale:true}` ou `{stale:false, value}`; `latest.busy()` diz se há chamada em andamento).
  - `glossary.js`: `COLUMNS` (`{key,label,fmt,help,mustBeZero?}`), `CONTROLLER_HELP`, `formatCell(col, metrics)`.
  - `charts/layout.js`: `dotStrip(values, width, height, pad)`; (as tarefas 5–6 acrescentam mais funções puras aqui).
  - `form.js`: `createForm(root, defaults, {onRun})` → `{read, write, setBusy, showError(field, msg) → bool, clearErrors}`.
  - `compare.js`: `renderCompare(root, data, onOpen)`; `onOpen(controllerName, seed)`.
  - `main.js`: `showTab(name)`, `state`, `openRun(controller, seed)` (ligado na Tarefa 5).

- [ ] **Step 1: Escrever os testes (Node) que falham**

`web/package.json`:

```json
{ "type": "module", "private": true }
```

`web/test/params.test.mjs`:

```js
import test from 'node:test';
import assert from 'node:assert/strict';
import { stateToHash, hashToState, coerce, fieldSpecs, TABS, CONTROLLERS } from '../static/js/params.js';

const defaults = {
  buses: 50, chargers: 25, limit_kw: 2000, profile: 'none', seeds: 20, follow_swaps: true,
  reading_age_min: 0, swap_back_cooldown_min: 30, swap_back_min_need_kwh: 10,
};

test('hash round trip keeps every parameter and the view', () => {
  const state = { params: { ...defaults, buses: 80, limit_kw: 612.5, profile: 'severe', follow_swaps: false }, tab: 'run', seed: 7, controller: 'fifo-unplug' };
  assert.deepEqual(hashToState(stateToHash(state), defaults), state);
});

test('an empty or garbage hash falls back to the defaults', () => {
  for (const hash of ['', '#', '#%%%', '#buses=abc&limit_kw=NaN&seeds=1.5&tab=nope&seed=-4&controller=x&follow_swaps=maybe']) {
    const s = hashToState(hash, defaults);
    assert.deepEqual(s.params, defaults, hash);
    assert.equal(s.tab, 'scenario');
    assert.equal(s.seed, 1);
    assert.equal(s.controller, 'planner');
  }
});

test('partial hashes keep what is valid', () => {
  const s = hashToState('#limit_kw=600&profile=mild&tab=compare', defaults);
  assert.equal(s.params.limit_kw, 600);
  assert.equal(s.params.profile, 'mild');
  assert.equal(s.params.buses, 50);
  assert.equal(s.tab, 'compare');
});

test('coerce rejects what is not the kind', () => {
  assert.equal(coerce('int', '', 5), 5);
  assert.equal(coerce('int', '2.5', 5), 5);
  assert.equal(coerce('int', 'x', 5), 5);
  assert.equal(coerce('float', 'Infinity', 1), 1);
  assert.equal(coerce('float', '0.25', 1), 0.25);
  assert.equal(coerce('bool', 'true', false), true);
  assert.equal(coerce('bool', '1', false), false);
  assert.equal(coerce('int', null, 9), 9);
});

test('constants list the views and controllers', () => {
  assert.deepEqual(TABS, ['scenario', 'compare', 'run']);
  assert.deepEqual(CONTROLLERS, ['fifo', 'edf', 'fifo-unplug', 'safe', 'planner']);
});

test('field specs take their bounds from the server limits', () => {
  const specs = fieldSpecs({ max_buses: 10000, max_chargers: 9000, max_seeds: 1000, max_limit_kw: 1e7, max_reading_age_min: 10000 });
  const by = Object.fromEntries(specs.map((f) => [f.key, f]));
  assert.equal(by.buses.max, 10000);
  assert.equal(by.chargers.max, 9000);
  assert.equal(by.limit_kw.max, 1e7);
  assert.equal(by.seeds.max, 1000);
  assert.equal(by.swap_back_cooldown_min.advanced, true);
  assert.equal(by.buses.advanced, undefined);
  assert.equal(specs.length, 9);
});
```

`web/test/format.test.mjs`:

```js
import test from 'node:test';
import assert from 'node:assert/strict';
import { fmtNum, fmtPct, fmtBRL, clock, duration } from '../static/js/format.js';

test('numbers use the Brazilian separators', () => {
  assert.equal(fmtNum(1234.56, 1), '1.234,6');
  assert.equal(fmtNum(1234567, 0), '1.234.567');
  assert.equal(fmtNum(0.5, 2), '0,50');
  assert.equal(fmtNum(-3.25, 1), '-3,3');
  assert.equal(fmtNum(-0.04, 1), '0,0');
});

test('missing values show a dash', () => {
  for (const v of [null, undefined, NaN, Infinity]) assert.equal(fmtNum(v), '—');
});

test('percent and money', () => {
  assert.equal(fmtPct(75.64), '75,6%');
  assert.equal(fmtBRL(11331.4), 'R$ 11.331');
});

test('clock wraps past midnight and before it', () => {
  assert.equal(clock(18 * 60, 0), '18:00');
  assert.equal(clock(18 * 60, 125), '20:05');
  assert.equal(clock(18 * 60, 800), '07:20');
  assert.equal(clock(18 * 60, -60), '17:00');
});

test('durations', () => {
  assert.equal(duration(45), '45 min');
  assert.equal(duration(125), '2 h 05 min');
  assert.equal(duration(60), '1 h 00 min');
  assert.equal(duration(-5), '0 min');
});
```

`web/test/api.test.mjs`:

```js
import test from 'node:test';
import assert from 'node:assert/strict';
import { createLatest, ApiError } from '../static/js/api.js';

const delay = (ms, v) => new Promise((r) => setTimeout(() => r(v), ms));

test('only the latest call delivers a result and the earlier one is aborted', async () => {
  const latest = createLatest();
  let firstSignal;
  const first = latest(async (signal) => { firstSignal = signal; await delay(40); return 'old'; });
  const second = latest(async () => { await delay(5); return 'new'; });
  const [a, b] = await Promise.all([first, second]);
  assert.equal(a.stale, true);
  assert.deepEqual(b, { stale: false, value: 'new' });
  assert.equal(firstSignal.aborted, true);
});

test('an aborted request is reported as stale, not as an error', async () => {
  const latest = createLatest();
  const p = latest((signal) => new Promise((_, rej) => signal.addEventListener('abort', () => rej(Object.assign(new Error('x'), { name: 'AbortError' })))));
  const q = latest(async () => 1);
  assert.equal((await p).stale, true);
  assert.equal((await q).value, 1);
});

test('a real error of the latest call is thrown', async () => {
  const latest = createLatest();
  await assert.rejects(latest(async () => { throw new ApiError('boom', 'seeds', 400); }), (e) => e instanceof ApiError && e.field === 'seeds' && e.status === 400);
});

test('busy tells whether a call is in flight', async () => {
  const latest = createLatest();
  assert.equal(latest.busy(), false);
  const p = latest(() => delay(10, 1));
  assert.equal(latest.busy(), true);
  await p;
  assert.equal(latest.busy(), false);
});
```

`web/test/layout.test.mjs`:

```js
import test from 'node:test';
import assert from 'node:assert/strict';
import { dotStrip } from '../static/js/charts/layout.js';

test('dots sit at their value and never leave the strip', () => {
  const dots = dotStrip([{ seed: 1, value: 0 }, { seed: 2, value: 50 }, { seed: 3, value: 100 }, { seed: 4, value: 140 }, { seed: 5, value: -20 }], 600, 40, 10);
  assert.equal(dots[0].x, 10);
  assert.equal(dots[1].x, 300);
  assert.equal(dots[2].x, 590);
  assert.equal(dots[3].x, 590); // clamped
  assert.equal(dots[4].x, 10);  // clamped
  for (const d of dots) assert.ok(d.y > 0 && d.y < 40);
  assert.deepEqual(dots.map((d) => d.seed), [1, 2, 3, 4, 5]);
});

test('neighbouring dots are spread over rows so they do not hide each other', () => {
  const dots = dotStrip([{ seed: 1, value: 50 }, { seed: 2, value: 50 }, { seed: 3, value: 50 }], 600, 40, 10);
  assert.equal(new Set(dots.map((d) => d.y)).size, 3);
});

test('no values give no dots', () => {
  assert.deepEqual(dotStrip([], 600, 40, 10), []);
});
```

- [ ] **Step 2: Rodar e ver falhar**

Run: `node --test web/test/`
Expected: FAIL (`Cannot find module .../params.js` etc.). Se `node --version` for menor que 18, instale um Node 18+ só para desenvolver e avise.

- [ ] **Step 3: Módulos puros**

`web/static/js/params.js`:

```js
export const TABS = ['scenario', 'compare', 'run'];
export const CONTROLLERS = ['fifo', 'edf', 'fifo-unplug', 'safe', 'planner'];

const KINDS = {
  buses: 'int', chargers: 'int', limit_kw: 'float', profile: 'string', seeds: 'int',
  follow_swaps: 'bool', reading_age_min: 'int', swap_back_cooldown_min: 'int', swap_back_min_need_kwh: 'float',
};

// coerce turns a URL string into the parameter's kind, or returns the fallback.
export function coerce(kind, raw, fallback) {
  if (raw === undefined || raw === null) return fallback;
  switch (kind) {
    case 'bool':
      return raw === 'true' ? true : raw === 'false' ? false : fallback;
    case 'int': {
      const n = Number(raw);
      return raw !== '' && Number.isInteger(n) ? n : fallback;
    }
    case 'float': {
      const n = Number(raw);
      return raw !== '' && Number.isFinite(n) ? n : fallback;
    }
    default:
      return String(raw);
  }
}

export function stateToHash(state) {
  const q = new URLSearchParams();
  q.set('tab', state.tab);
  q.set('seed', String(state.seed));
  q.set('controller', state.controller);
  for (const key of Object.keys(KINDS)) q.set(key, String(state.params[key]));
  return '#' + q.toString();
}

export function hashToState(hash, defaults) {
  const q = new URLSearchParams(String(hash || '').replace(/^#/, ''));
  const params = {};
  for (const [key, kind] of Object.entries(KINDS)) params[key] = coerce(kind, q.get(key), defaults[key]);
  const tab = TABS.includes(q.get('tab')) ? q.get('tab') : 'scenario';
  const seed = coerce('int', q.get('seed'), 1);
  const controller = CONTROLLERS.includes(q.get('controller')) ? q.get('controller') : 'planner';
  return { params, tab, seed: seed >= 1 ? seed : 1, controller };
}

// fieldSpecs describes the form; bounds come from the server's limits.
export function fieldSpecs(limits) {
  return [
    { key: 'buses', label: 'Ônibus', kind: 'int', min: 1, max: limits.max_buses },
    { key: 'chargers', label: 'Carregadores', kind: 'int', min: 1, max: limits.max_chargers },
    { key: 'limit_kw', label: 'Limite da garagem (kW)', kind: 'float', min: 0, max: limits.max_limit_kw },
    { key: 'profile', label: 'Perfil de falhas', kind: 'select',
      options: [['none', 'nenhuma'], ['mild', 'leve'], ['severe', 'severa'], ['random', 'aleatória']] },
    { key: 'seeds', label: 'Sementes (repetições)', kind: 'int', min: 1, max: limits.max_seeds },
    { key: 'reading_age_min', label: 'Idade da leitura de carga (min)', kind: 'int', min: 0, max: limits.max_reading_age_min },
    { key: 'follow_swaps', label: 'Operadores seguem os rodízios recomendados', kind: 'bool' },
    { key: 'swap_back_cooldown_min', label: 'Espera para voltar a um carregador (min)', kind: 'int', min: 0, advanced: true },
    { key: 'swap_back_min_need_kwh', label: 'Necessidade mínima para voltar (kWh)', kind: 'float', min: 0, advanced: true },
  ];
}
```

`web/static/js/format.js`:

```js
export function fmtNum(v, digits = 1) {
  if (typeof v !== 'number' || !Number.isFinite(v)) return '—';
  const fixed = Math.abs(v).toFixed(digits);
  const [int, frac] = fixed.split('.');
  const grouped = int.replace(/\B(?=(\d{3})+(?!\d))/g, '.');
  const out = frac ? `${grouped},${frac}` : grouped;
  return v < 0 && Number(fixed) !== 0 ? '-' + out : out;
}

export const fmtPct = (v) => fmtNum(v, 1) + '%';
export const fmtBRL = (v) => 'R$ ' + fmtNum(v, 0);

// clock returns the wall-clock time (HH:MM) of a scenario minute.
export function clock(startClockMin, minute) {
  const m = (((startClockMin + minute) % 1440) + 1440) % 1440;
  return `${String(Math.floor(m / 60)).padStart(2, '0')}:${String(m % 60).padStart(2, '0')}`;
}

export function duration(min) {
  const m = Math.max(0, Math.round(min));
  if (m < 60) return `${m} min`;
  return `${Math.floor(m / 60)} h ${String(m % 60).padStart(2, '0')} min`;
}
```

`web/static/js/charts/layout.js`:

```js
// dotStrip places one dot per seed on a 0..100 strip; neighbours are spread over three
// rows (by position in the list) so equal values do not hide each other.
export function dotStrip(values, width, height, pad = 8) {
  const inner = width - 2 * pad;
  const rows = 3;
  return values.map((v, i) => {
    const clamped = Math.min(100, Math.max(0, v.value));
    return { seed: v.seed, value: v.value, x: pad + (inner * clamped) / 100, y: (height / (rows + 1)) * ((i % rows) + 1) };
  });
}
```

`web/static/js/api.js`:

```js
export class ApiError extends Error {
  constructor(message, field = '', status = 0) {
    super(message);
    this.name = 'ApiError';
    this.field = field;
    this.status = status;
  }
}

async function request(method, url, body, signal) {
  let resp;
  try {
    resp = await fetch(url, {
      method, signal,
      headers: body ? { 'Content-Type': 'application/json' } : undefined,
      body: body ? JSON.stringify(body) : undefined,
    });
  } catch (e) {
    if (e.name === 'AbortError') throw e;
    throw new ApiError('Não consegui falar com o servidor. Ele ainda está rodando?', '', 0);
  }
  let data = null;
  try { data = await resp.json(); } catch { /* not JSON */ }
  if (!resp.ok) throw new ApiError((data && data.error) || `Erro ${resp.status}`, (data && data.field) || '', resp.status);
  return data;
}

export const getDefaults = (signal) => request('GET', '/api/defaults', null, signal);
export const compare = (params, signal) => request('POST', '/api/compare', params, signal);
export const run = (req, signal) => request('POST', '/api/run', req, signal);

// createLatest returns latest(fn): it runs fn(signal), aborts the previous call and
// delivers only the latest call's result ({stale:true} for the others).
export function createLatest() {
  let ctrl = null;
  let n = 0;
  let inflight = 0;
  async function latest(fn) {
    if (ctrl) ctrl.abort();
    ctrl = new AbortController();
    const mine = ++n;
    inflight++;
    try {
      const value = await fn(ctrl.signal);
      return mine === n ? { stale: false, value } : { stale: true };
    } catch (e) {
      if (e.name === 'AbortError' || mine !== n) return { stale: true };
      throw e;
    } finally {
      inflight--;
    }
  }
  latest.busy = () => inflight > 0;
  return latest;
}
```

- [ ] **Step 4: Rodar e ver passar**

Run: `node --test web/test/`
Expected: PASS (todos os testes de `params`, `format`, `api`, `layout`).

- [ ] **Step 5: `glossary.js`, `dom.js`, `charts/svg.js`**

`web/static/js/glossary.js`:

```js
import { fmtNum, fmtPct, fmtBRL } from './format.js';

const f = {
  pct: (v) => fmtPct(v),
  kwh: (v) => fmtNum(v, 0),
  kw: (v) => fmtNum(v, 0),
  int: (v) => fmtNum(v, 0),
  num1: (v) => fmtNum(v, 1),
  brl: (v) => fmtBRL(v),
};

export const COLUMNS = [
  { key: 'ready_pct', label: 'ônibus prontos', fmt: f.pct, help: 'Porcentagem de ônibus que saem com a carga necessária para a rota. É a métrica principal.' },
  { key: 'shortfall_kwh', label: 'déficit (kWh)', fmt: f.kwh, help: 'Energia que faltava, em média por execução, nos ônibus que saíram sem a carga necessária.' },
  { key: 'peak_kw', label: 'pico (kW)', fmt: f.kw, help: 'Maior potência física puxada da rede.' },
  { key: 'plan_violations', label: 'violações do plano', fmt: f.int, mustBeZero: true, help: 'Quantas vezes o plano mandou mais potência do que o limite da garagem. Precisa ser 0.' },
  { key: 'overshoot_min', label: 'minutos acima do limite', fmt: f.int, help: 'Minutos em que a potência física passou do limite (por exemplo logo após uma queda de rede, pelo atraso de um passo entre comando e potência).' },
  { key: 'energy_kwh', label: 'energia (kWh)', fmt: f.kwh, help: 'Energia entregue pela rede, em média por execução.' },
  { key: 'cost_brl', label: 'custo (R$)', fmt: f.brl, help: 'Custo da energia com tarifa simples de ponta e fora de ponta, em média por execução.' },
  { key: 'plan_changes', label: 'mudanças de plano', fmt: f.int, help: 'Quantas vezes a potência de algum carregador mudou (soma das execuções): mede a estabilidade.' },
  { key: 'operator_moves', label: 'movimentações por noite', fmt: f.num1, help: 'Trabalho manual pedido aos operadores: rodízios executados ou ônibus desconectados, em média por execução.' },
  { key: 'plan_p99_micros', label: 'p99 (µs)', fmt: f.int, help: 'Tempo de cálculo do planejador no pior 1% dos ciclos. Aproximado quando várias simulações rodam em paralelo.' },
];

export const CONTROLLER_HELP = {
  fifo: 'Carrega por ordem de chegada, no máximo de potência, até acabar o limite.',
  edf: 'Carrega primeiro quem sai mais cedo.',
  'fifo-unplug': 'Ordem de chegada mais a rotina de hoje: operadores desconectam ônibus já carregados quando outro espera.',
  safe: 'Só o perfil seguro: divide o limite igualmente entre os ônibus conectados.',
  planner: 'O planejador: menor folga primeiro, potência na hora certa e rodízios recomendados.',
};

export function formatCell(col, metrics) {
  return col.fmt(metrics[col.key]);
}
```

`web/static/js/dom.js`:

```js
// h builds an HTML element: attributes (class, on* handlers, booleans) and children.
export function h(tag, attrs = {}, ...children) {
  const node = document.createElement(tag);
  for (const [k, v] of Object.entries(attrs || {})) {
    if (v === false || v === null || v === undefined) continue;
    if (k === 'class') node.className = v;
    else if (k.startsWith('on') && typeof v === 'function') node.addEventListener(k.slice(2), v);
    else node.setAttribute(k, v === true ? '' : String(v));
  }
  for (const c of children.flat(Infinity)) {
    if (c === null || c === undefined || c === false) continue;
    node.append(c.nodeType ? c : document.createTextNode(String(c)));
  }
  return node;
}

export function clear(node) {
  node.replaceChildren();
  return node;
}
```

`web/static/js/charts/svg.js`:

```js
export const NS = 'http://www.w3.org/2000/svg';

// s builds an SVG element like dom.h builds an HTML one.
export function s(tag, attrs = {}, ...children) {
  const node = document.createElementNS(NS, tag);
  for (const [k, v] of Object.entries(attrs || {})) {
    if (v === false || v === null || v === undefined) continue;
    if (k.startsWith('on') && typeof v === 'function') node.addEventListener(k.slice(2), v);
    else node.setAttribute(k, String(v));
  }
  for (const c of children.flat(Infinity)) {
    if (c === null || c === undefined || c === false) continue;
    node.append(c.nodeType ? c : document.createTextNode(String(c)));
  }
  return node;
}
```

- [ ] **Step 6: `form.js`**

```js
import { h } from './dom.js';
import { fieldSpecs } from './params.js';

// createForm builds the scenario form inside root. defaults is the /api/defaults reply.
export function createForm(root, defaults, handlers) {
  const specs = fieldSpecs(defaults.limits);
  const inputs = new Map();
  const errors = new Map();

  function field(spec) {
    const id = `f-${spec.key}`;
    let input;
    if (spec.kind === 'select') {
      input = h('select', { id, name: spec.key }, spec.options.map(([v, l]) => h('option', { value: v }, l)));
    } else if (spec.kind === 'bool') {
      input = h('input', { id, name: spec.key, type: 'checkbox' });
    } else {
      input = h('input', {
        id, name: spec.key, type: 'number', min: spec.min, max: spec.max,
        step: spec.kind === 'int' ? '1' : 'any', inputmode: spec.kind === 'int' ? 'numeric' : 'decimal',
      });
    }
    const err = h('div', { class: 'field-error', id: `e-${spec.key}`, role: 'alert' });
    input.setAttribute('aria-describedby', err.id);
    inputs.set(spec.key, { input, spec });
    errors.set(spec.key, err);
    const label = h('label', { for: id }, spec.label);
    return spec.kind === 'bool' ? h('div', { class: 'field check' }, input, label, err) : h('div', { class: 'field' }, label, input, err);
  }

  function write(params) {
    for (const [key, { input, spec }] of inputs) {
      if (params[key] === undefined) continue;
      if (spec.kind === 'bool') input.checked = Boolean(params[key]);
      else input.value = String(params[key]);
    }
    clearErrors();
  }

  function read() {
    const p = {};
    for (const [key, { input, spec }] of inputs) {
      if (spec.kind === 'bool') p[key] = input.checked;
      else if (spec.kind === 'select') p[key] = input.value;
      else p[key] = input.value === '' ? NaN : Number(input.value);
    }
    return p;
  }

  function clearErrors() {
    for (const [key, err] of errors) {
      err.textContent = '';
      inputs.get(key).input.removeAttribute('aria-invalid');
    }
  }

  // showError marks a field; it returns false when the form has no such field.
  function showError(key, message) {
    const entry = inputs.get(key);
    if (!entry) return false;
    errors.get(key).textContent = message;
    entry.input.setAttribute('aria-invalid', 'true');
    entry.input.focus();
    return true;
  }

  const run = h('button', { type: 'submit', class: 'primary' }, 'Rodar');
  function setBusy(busy) {
    run.disabled = busy;
    run.textContent = busy ? 'Rodando…' : 'Rodar';
  }

  const main = specs.filter((s) => !s.advanced).map(field);
  const advanced = specs.filter((s) => s.advanced).map(field);
  const presets = h('div', { class: 'presets' },
    h('span', { class: 'label' }, 'Cenários prontos:'),
    defaults.presets.map((p) => h('button', { type: 'button', class: 'chip', title: p.description, onclick: () => write(p.params) }, p.label)));
  const form = h('form', { class: 'form', novalidate: true },
    presets,
    h('div', { class: 'grid' }, main),
    h('details', { class: 'advanced' }, h('summary', {}, 'Avançado: regra anti-vaivém do rodízio'), h('div', { class: 'grid' }, advanced)),
    h('div', { class: 'actions' }, run));
  form.addEventListener('submit', (e) => {
    e.preventDefault();
    clearErrors();
    const params = read();
    const bad = Object.entries(params).find(([, v]) => typeof v === 'number' && Number.isNaN(v));
    if (bad) {
      showError(bad[0], 'Informe um número.');
      return;
    }
    handlers.onRun(params);
  });
  root.replaceChildren(h('h2', {}, 'Cenário'), form);
  return { read, write, setBusy, showError, clearErrors };
}
```

- [ ] **Step 7: `compare.js`**

```js
import { h, clear } from './dom.js';
import { s } from './charts/svg.js';
import { COLUMNS, CONTROLLER_HELP, formatCell } from './glossary.js';
import { dotStrip } from './charts/layout.js';
import { fmtNum } from './format.js';

const PROFILE_PT = { none: 'sem falhas', mild: 'falhas leves', severe: 'falhas severas', random: 'falhas aleatórias' };

function summary(p) {
  return `${p.buses} ônibus, ${p.chargers} carregadores, limite ${fmtNum(p.limit_kw, 0)} kW, ${PROFILE_PT[p.profile] || p.profile}, ` +
    `${p.seeds} sementes, operadores ${p.follow_swaps ? 'seguem' : 'não seguem'} os rodízios. Dados sintéticos: mostram o comportamento do algoritmo, não de uma garagem real.`;
}

function table(data) {
  const best = Math.max(...data.controllers.map((c) => c.aggregate.ready_pct));
  const head = h('tr', {}, h('th', { scope: 'col' }, 'controlador'),
    COLUMNS.map((c) => h('th', { scope: 'col', title: c.help }, c.label)));
  const rows = data.controllers.map((c) => {
    const isBest = c.aggregate.ready_pct === best;
    return h('tr', { class: c.name === 'planner' ? 'planner' : '' },
      h('th', { scope: 'row', title: CONTROLLER_HELP[c.name] }, c.name),
      COLUMNS.map((col) => {
        const text = formatCell(col, c.aggregate);
        const bad = col.mustBeZero && c.aggregate[col.key] > 0;
        const node = col.key === 'ready_pct' && isBest ? h('strong', {}, text, h('span', { class: 'sr' }, ' (melhor)'), ' ▲') : text;
        return h('td', { class: bad ? 'bad' : '' }, bad ? '⚠ ' : '', node);
      }));
  });
  return h('div', { class: 'table-wrap' }, h('table', { class: 'metrics' }, h('thead', {}, head), h('tbody', {}, rows)));
}

function seedsPanel(data, onOpen) {
  const W = 600, H = 44;
  return h('div', { class: 'seeds' },
    h('h3', {}, 'Ônibus prontos por semente'),
    h('p', { class: 'note' }, 'Cada ponto é uma semente (uma garagem simulada). Clique num ponto para abrir aquela execução minuto a minuto.'),
    data.controllers.map((c) => {
      const dots = dotStrip(c.seeds.map((x) => ({ seed: x.seed, value: x.metrics.ready_pct })), W, H, 10);
      const mean = 10 + ((W - 20) * Math.min(100, Math.max(0, c.aggregate.ready_pct))) / 100;
      const svg = s('svg', { viewBox: `0 0 ${W} ${H}`, class: 'strip', role: 'group', 'aria-label': `${c.name}: ônibus prontos por semente` },
        s('line', { x1: 10, x2: W - 10, y1: H - 6, y2: H - 6, class: 'axis' }),
        s('rect', { x: 10, y: H - 12, width: Math.max(0, mean - 10), height: 8, class: 'bar' }, s('title', {}, `média: ${fmtNum(c.aggregate.ready_pct, 1)}%`)),
        s('line', { x1: mean, x2: mean, y1: 2, y2: H - 2, class: 'mean' }),
        dots.map((d) => s('circle', {
          cx: d.x, cy: d.y, r: 5, class: 'dot', tabindex: 0, role: 'button',
          'aria-label': `${c.name}, semente ${d.seed}: ${fmtNum(d.value, 1)}% prontos. Abrir execução`,
          onclick: () => onOpen(c.name, d.seed),
          onkeydown: (e) => { if (e.key === 'Enter' || e.key === ' ') { e.preventDefault(); onOpen(c.name, d.seed); } },
        }, s('title', {}, `semente ${d.seed}: ${fmtNum(d.value, 1)}%`))));
      return h('div', { class: 'strip-row' }, h('span', { class: 'strip-name', title: CONTROLLER_HELP[c.name] }, c.name), svg,
        h('span', { class: 'strip-mean' }, `média ${fmtNum(c.aggregate.ready_pct, 1)}%`));
    }));
}

function glossary() {
  return h('details', { class: 'glossary' }, h('summary', {}, 'O que significa cada coluna e cada controlador'),
    h('dl', {}, COLUMNS.map((c) => [h('dt', {}, c.label), h('dd', {}, c.help)]),
      Object.entries(CONTROLLER_HELP).map(([k, v]) => [h('dt', {}, k), h('dd', {}, v)])));
}

export function renderCompare(root, data, onOpen) {
  clear(root);
  root.append(h('h2', {}, 'Comparação'), h('p', { class: 'note' }, summary(data.params)), table(data), seedsPanel(data, onOpen),
    h('p', { class: 'note' }, data.p99_note), glossary());
}
```

- [ ] **Step 8: `index.html`, `app.css`, `main.js`**

`web/static/index.html` (substitui o provisório):

```html
<!doctype html>
<html lang="pt-BR">
<head>
<meta charset="utf-8">
<meta name="viewport" content="width=device-width, initial-scale=1">
<title>Laboratório de recarga</title>
<link rel="stylesheet" href="app.css">
</head>
<body>
<header class="top">
  <h1>Laboratório de recarga</h1>
  <p class="sub">Simulador de garagem de ônibus elétricos com dados sintéticos: mostra como o algoritmo se comporta, não o resultado numa garagem real.</p>
</header>
<nav class="tabs" role="tablist" aria-label="Seções">
  <button role="tab" data-tab="scenario" aria-selected="true">Cenário</button>
  <button role="tab" data-tab="compare" aria-selected="false">Comparação</button>
  <button role="tab" data-tab="run" aria-selected="false">Execução</button>
</nav>
<div id="banner" class="banner" role="alert" hidden></div>
<main>
  <section id="tab-scenario" role="tabpanel"></section>
  <section id="tab-compare" role="tabpanel" hidden><div id="compare-out"><p class="note">Rode um cenário para ver a comparação.</p></div></section>
  <section id="tab-run" role="tabpanel" hidden><div id="run-out"><p class="note">Abra uma execução pela aba Comparação (clique num ponto).</p></div></section>
</main>
<script type="module" src="js/main.js"></script>
</body>
</html>
```

`web/static/app.css`:

```css
:root {
  --bg: #fafafa; --fg: #1b1f24; --muted: #5a6370; --card: #ffffff; --border: #d5d9df;
  --accent: #0072b2; --accent-fg: #ffffff; --bad: #b3410a; --bad-bg: #fbe9df;
  --ok: #007a5a; --amber: #b07800; --sky: #56b4e9; --purple: #a8497f;
  --chart-a: #3b86b8; --chart-b: #7fb5d9; --limit: #d55e00; --grid: #e3e6ea;
}
@media (prefers-color-scheme: dark) {
  :root {
    --bg: #14171b; --fg: #e6e9ed; --muted: #9aa3ad; --card: #1c2025; --border: #343b44;
    --accent: #56b4e9; --accent-fg: #0b1620; --bad: #ff9b62; --bad-bg: #3a2418;
    --ok: #3ccf9c; --amber: #e8b23a; --sky: #56b4e9; --purple: #d996bd;
    --chart-a: #4f9fd0; --chart-b: #2d6f99; --limit: #ff9b62; --grid: #2a3038;
  }
}
* { box-sizing: border-box; }
body { margin: 0; background: var(--bg); color: var(--fg); font: 15px/1.5 system-ui, -apple-system, "Segoe UI", sans-serif; }
.top, nav.tabs, main, .banner { max-width: 1200px; margin: 0 auto; padding-left: 16px; padding-right: 16px; }
.top { padding-top: 20px; }
h1 { margin: 0; font-size: 1.5rem; }
h2 { margin: 16px 0 8px; font-size: 1.2rem; }
h3 { margin: 20px 0 4px; font-size: 1rem; }
.sub, .note { color: var(--muted); margin: 4px 0 12px; }
nav.tabs { display: flex; gap: 4px; border-bottom: 1px solid var(--border); margin-top: 12px; }
nav.tabs button { background: none; border: 0; border-bottom: 3px solid transparent; padding: 10px 14px; font: inherit; color: var(--muted); cursor: pointer; }
nav.tabs button[aria-selected="true"] { color: var(--fg); border-bottom-color: var(--accent); font-weight: 600; }
button:focus-visible, input:focus-visible, select:focus-visible, summary:focus-visible, .dot:focus-visible, [tabindex]:focus-visible { outline: 3px solid var(--accent); outline-offset: 2px; }
.banner { background: var(--bad-bg); color: var(--bad); border-left: 4px solid var(--bad); margin-top: 12px; padding-top: 8px; padding-bottom: 8px; }
.form { background: var(--card); border: 1px solid var(--border); border-radius: 8px; padding: 16px; }
.grid { display: grid; grid-template-columns: repeat(auto-fill, minmax(230px, 1fr)); gap: 12px 16px; margin: 12px 0; }
.field { display: flex; flex-direction: column; gap: 4px; }
.field.check { flex-direction: row; align-items: center; flex-wrap: wrap; }
.field label { font-size: .9rem; color: var(--muted); }
.field input[type=number], .field select { padding: 7px 8px; border: 1px solid var(--border); border-radius: 6px; background: var(--bg); color: var(--fg); font: inherit; }
.field input[aria-invalid=true] { border-color: var(--bad); }
.field-error { color: var(--bad); font-size: .85rem; min-height: 0; flex-basis: 100%; }
.presets { display: flex; flex-wrap: wrap; gap: 8px; align-items: center; }
.presets .label { color: var(--muted); font-size: .9rem; }
.chip { border: 1px solid var(--border); background: var(--bg); color: var(--fg); border-radius: 999px; padding: 4px 12px; font: inherit; cursor: pointer; }
.chip:hover { border-color: var(--accent); }
.advanced summary, .glossary summary { cursor: pointer; color: var(--muted); }
.actions { margin-top: 12px; }
button.primary { background: var(--accent); color: var(--accent-fg); border: 0; border-radius: 6px; padding: 9px 22px; font: inherit; font-weight: 600; cursor: pointer; }
button.primary:disabled { opacity: .6; cursor: progress; }
.table-wrap { overflow-x: auto; }
table.metrics { border-collapse: collapse; width: 100%; background: var(--card); border: 1px solid var(--border); }
table.metrics th, table.metrics td { padding: 7px 10px; text-align: right; white-space: nowrap; border-bottom: 1px solid var(--border); }
table.metrics thead th { font-size: .8rem; color: var(--muted); font-weight: 600; cursor: help; }
table.metrics th[scope=row], table.metrics thead th:first-child { text-align: left; }
table.metrics tr.planner { background: color-mix(in srgb, var(--accent) 10%, transparent); }
td.bad { background: var(--bad-bg); color: var(--bad); font-weight: 600; }
.sr { position: absolute; left: -9999px; }
.strip-row { display: grid; grid-template-columns: 110px 1fr 110px; align-items: center; gap: 8px; }
.strip-name { font-size: .9rem; } .strip-mean { font-size: .85rem; color: var(--muted); }
.strip { width: 100%; height: 44px; }
.strip .bar { fill: var(--chart-b); opacity: .65; }
.strip .axis { stroke: var(--border); stroke-width: 2; } .strip .mean { stroke: var(--limit); stroke-width: 2; stroke-dasharray: 3 3; }
.dot { fill: var(--accent); stroke: var(--card); stroke-width: 1.5; cursor: pointer; }
.dot:hover { fill: var(--limit); }
.glossary dl { display: grid; grid-template-columns: max-content 1fr; gap: 4px 16px; } .glossary dt { font-weight: 600; } .glossary dd { margin: 0; color: var(--muted); }
.loading { color: var(--muted); }
@media (max-width: 640px) { .strip-row { grid-template-columns: 1fr; } }
```

`web/static/js/main.js`:

```js
import { getDefaults, compare, createLatest, ApiError } from './api.js';
import { hashToState, stateToHash, TABS } from './params.js';
import { createForm } from './form.js';
import { renderCompare } from './compare.js';
import { h } from './dom.js';

const latest = createLatest();
let state;
let form;
const $ = (id) => document.getElementById(id);

function syncHash() {
  history.replaceState(null, '', stateToHash(state));
}

export function showTab(name) {
  state.tab = name;
  for (const t of TABS) {
    $(`tab-${t}`).hidden = t !== name;
    const b = document.querySelector(`[data-tab="${t}"]`);
    b.setAttribute('aria-selected', String(t === name));
    b.tabIndex = t === name ? 0 : -1;
  }
  syncHash();
}

function banner(message) {
  const b = $('banner');
  b.textContent = message || '';
  b.hidden = !message;
}

// reportError puts a server error on its form field when it has one, else in the banner.
function reportError(e) {
  const msg = e instanceof ApiError ? e.message : 'Erro inesperado: ' + e.message;
  showTab('scenario');
  if (e instanceof ApiError && e.field && form.showError(e.field, e.message)) return;
  banner(msg);
}

async function runCompare(params) {
  banner('');
  state.params = params;
  form.setBusy(true);
  $('compare-out').replaceChildren(h('p', { class: 'loading' }, 'Rodando as simulações…'));
  showTab('compare');
  try {
    const r = await latest((signal) => compare(params, signal));
    if (r.stale) return;
    renderCompare($('compare-out'), r.value, openRun);
  } catch (e) {
    reportError(e);
  } finally {
    if (!latest.busy()) form.setBusy(false);
  }
}

export function openRun(controller, seed) {
  state.controller = controller;
  state.seed = seed;
  showTab('run');
}

async function init() {
  let defaults;
  try {
    defaults = await getDefaults();
  } catch (e) {
    banner(e.message);
    return;
  }
  state = hashToState(location.hash, defaults.params);
  form = createForm($('tab-scenario'), defaults, { onRun: runCompare });
  form.write(state.params);
  for (const b of document.querySelectorAll('nav.tabs button')) b.addEventListener('click', () => showTab(b.dataset.tab));
  showTab('scenario');
  if (state.tab === 'compare') runCompare(state.params);
}

init();
```

(Ao abrir um link de comparação, o formulário aparece e `runCompare` troca para a aba Comparação; a Tarefa 5 amplia `init` para links de execução.)

- [ ] **Step 9: Rodar os testes automáticos**

Run: `node --test web/test/` — Expected: PASS.
Run: `go test -race -count=1 ./internal/lab ./cmd/lab` — Expected: PASS (`TestEmbeddedAssetsExist` agora percorre `app.css` e `js/main.js` e seus imports; qualquer módulo importado que não exista falha aqui).

- [ ] **Step 10: Verificar no navegador de verdade**

Run: `go run ./cmd/lab` (segundo plano) e abra `http://127.0.0.1:8080`. Verifique, com capturas de tela se tiver ferramenta de navegador (se não tiver, diga no relatório que a verificação visual ficou pendente para o controlador):
1. A página abre sem erros no console; o formulário mostra os 5 campos principais, a caixa de rodízios e o bloco "Avançado" recolhido.
2. Clique em "Rede apertada (600 kW)": o limite muda para 600 e o perfil para leve.
3. Rodar com 10 ônibus, 5 carregadores, 3 sementes: a aba Comparação mostra a tabela com 5 linhas, `violações do plano` = 0 em todas, o melhor `ready%` com ▲, e uma faixa de pontos por controlador.
4. Digite `0` em Ônibus e rode: o campo fica marcado com a mensagem em português e a aba volta para Cenário.
5. Apague o valor de um campo numérico e rode: "Informe um número." sem chamar o servidor.
6. Recarregue a página com o endereço atual (`#tab=compare&...`): roda a comparação sozinho.
7. Tema escuro (preferência do sistema) legível; o foco do teclado aparece nos botões e nos pontos.

- [ ] **Step 11: CI e commit**

Em `.github/workflows/ci.yml`, depois do passo `go test -race ./...`, acrescente:

```yaml
      - name: web unit tests
        run: node --test web/test/
```

(O runner `ubuntu-latest` já tem Node.) Depois:

```bash
gofmt -l . ; go vet ./... && go test -race -short ./... && node --test web/test/
git add web .github/workflows/ci.yml
git commit -m "feat(lab): web interface base, scenario form and comparison tab

Co-Authored-By: Claude Sonnet 5.5 <noreply@anthropic.com>"
git push
```

---

### Task 5: Aba Execução, parte 1 — carga da execução, gráfico de potência e cursor

**Files:**
- Create: `web/static/js/cursor.js`, `web/static/js/run.js`, `web/static/js/charts/scale.js`, `web/static/js/charts/power.js`; testes `web/test/cursor.test.mjs`, `web/test/scale.test.mjs`, `web/test/layout2.test.mjs`, `web/test/glossary.test.mjs`
- Modify: `web/static/js/charts/layout.js` (acrescentar funções), `web/static/js/glossary.js` (acrescentar), `web/static/js/main.js`, `web/static/app.css` (acrescentar)

**Interfaces:**
- Consumes: Tarefa 4 (`h`, `clear`, `s`, `clock`, `fmtNum`, `api.run`, `createLatest`, `ApiError`, `CONTROLLERS`, `state`, `showTab`) e o formato de `/api/run` da Tarefa 3.
- Produces:
  - `cursor.js`: `createCursor(max)` → `{ max, value (getter), set(v), step(d), onChange(fn) → unsubscribe }`; `keyDelta(key, shift)`; `bindCursorKeys(el, cursor)`.
  - `charts/scale.js`: `linear(d0, d1, r0, r1)` (função com `.invert`), `niceTicks(min, max, count)`, `timeTicks(horizon, stepMin)`.
  - `charts/layout.js` (novas): `MARGIN = {left:120, right:16, top:12, bottom:28}`, `runs(arr)`, `stackLayers(series)`, `areaPath(lower, upper, x, y)`, `linePath(values, x, y, step)`, `assignLanes(items)`, `valuesAt(run, minute)`.
  - `glossary.js` (novas): `FAULT_LABEL`, `LAYER_LABEL`, `POWER_FAULTS` (Set), `describeFault(f)`.
  - `charts/power.js`: `renderPowerChart(root, data, cursor)`.
  - `run.js`: `createRunView(root, hooks)` → `{ load(state) }`; `hooks = { onSelect(controller, seed) }`. Guarda internamente `current = { data, cursor }` e expõe `getCurrent()` para a Tarefa 6.

- [ ] **Step 1: Escrever os testes (Node) que falham**

`web/test/scale.test.mjs`:

```js
import test from 'node:test';
import assert from 'node:assert/strict';
import { linear, niceTicks, timeTicks } from '../static/js/charts/scale.js';

test('linear maps the domain to the range and back', () => {
  const f = linear(0, 100, 10, 210);
  assert.equal(f(0), 10);
  assert.equal(f(50), 110);
  assert.equal(f(100), 210);
  assert.equal(f.invert(110), 50);
  const flip = linear(0, 10, 100, 0);
  assert.equal(flip(2), 80);
});

test('a degenerate domain does not divide by zero', () => {
  const f = linear(5, 5, 0, 100);
  assert.equal(f(5), 0);
  assert.equal(f.invert(50), 5);
});

test('nice ticks', () => {
  assert.deepEqual(niceTicks(0, 2000, 5), [0, 500, 1000, 1500, 2000]);
  assert.deepEqual(niceTicks(0, 87, 5), [0, 20, 40, 60, 80]);
  assert.deepEqual(niceTicks(5, 5), [5]);
  assert.deepEqual(niceTicks(9, 1), [9]);
  assert.deepEqual(niceTicks(0, 1, 5), [0, 0.2, 0.4, 0.6, 0.8, 1]);
});

test('time ticks every hour', () => {
  assert.deepEqual(timeTicks(180, 60), [0, 60, 120, 180]);
  assert.deepEqual(timeTicks(100, 60), [0, 60]);
});
```

`web/test/cursor.test.mjs`:

```js
import test from 'node:test';
import assert from 'node:assert/strict';
import { createCursor, keyDelta } from '../static/js/cursor.js';

test('the cursor clamps, rounds and ignores non-finite values', () => {
  const c = createCursor(100);
  c.set(250); assert.equal(c.value, 100);
  c.set(-5); assert.equal(c.value, 0);
  c.set(12.6); assert.equal(c.value, 13);
  c.set(NaN); assert.equal(c.value, 13);
  c.set(Infinity); assert.equal(c.value, 13);
  c.step(-20); assert.equal(c.value, 0);
});

test('subscribers hear only real changes and can unsubscribe', () => {
  const c = createCursor(10);
  const seen = [];
  const off = c.onChange((v) => seen.push(v));
  c.set(3); c.set(3); c.set(4);
  off();
  c.set(5);
  assert.deepEqual(seen, [3, 4]);
});

test('keys map to steps', () => {
  assert.equal(keyDelta('ArrowRight', false), 1);
  assert.equal(keyDelta('ArrowRight', true), 10);
  assert.equal(keyDelta('ArrowLeft', false), -1);
  assert.equal(keyDelta('PageUp', false), 60);
  assert.equal(keyDelta('PageDown', false), -60);
  assert.equal(keyDelta('a', false), null);
});

test('a zero-length run has a cursor that stays at 0', () => {
  const c = createCursor(0);
  c.set(5);
  assert.equal(c.value, 0);
});
```

`web/test/layout2.test.mjs`:

```js
import test from 'node:test';
import assert from 'node:assert/strict';
import { runs, stackLayers, areaPath, linePath, assignLanes, valuesAt, MARGIN } from '../static/js/charts/layout.js';

const id = (v) => v;

test('runs run-length encodes an array', () => {
  assert.deepEqual(runs(['a', 'a', 'b', 'a']), [
    { from: 0, to: 1, value: 'a' }, { from: 2, to: 2, value: 'b' }, { from: 3, to: 3, value: 'a' },
  ]);
  assert.deepEqual(runs([]), []);
});

test('stackLayers accumulates and treats missing values as zero', () => {
  const l = stackLayers([[1, 2], [3, null], [NaN, 1]]);
  assert.deepEqual(l[0], { lower: [0, 0], upper: [1, 2] });
  assert.deepEqual(l[1], { lower: [1, 2], upper: [4, 2] });
  assert.deepEqual(l[2].upper, [4, 3]);
  assert.deepEqual(stackLayers([]), []);
});

test('area and line paths', () => {
  assert.equal(areaPath([0, 0], [1, 2], id, id), 'M0,1L1,2L1,0L0,0Z');
  assert.equal(areaPath([], [], id, id), '');
  assert.equal(linePath([0, 10], id, id, false), 'M0,0L1,10');
  assert.equal(linePath([0, 10], id, id, true), 'M0,0L1,0L1,10');
});

test('a line breaks where the value is missing', () => {
  assert.equal(linePath([1, null, 2, 3], id, id, false), 'M0,1M2,2L3,3');
  assert.equal(linePath([NaN, NaN], id, id, false), '');
});

test('lanes keep overlapping intervals apart', () => {
  assert.deepEqual(assignLanes([{ from: 0, to: 10 }, { from: 5, to: 20 }, { from: 11, to: 15 }]), [0, 1, 0]);
  assert.deepEqual(assignLanes([]), []);
});

test('valuesAt clamps the minute and tolerates null', () => {
  const run = { series: { physical_kw: [0, null, 5], commanded_kw: [1, 2, 3], limit_kw: [9, 9, 9], layer: ['normal', 'safe', 'safe'] } };
  assert.deepEqual(valuesAt(run, 1), { physical: null, commanded: 2, limit: 9, layer: 'safe' });
  assert.equal(valuesAt(run, 99).physical, 5);
  assert.equal(valuesAt(run, -4).layer, 'normal');
});

test('the shared left margin leaves room for row labels', () => {
  assert.ok(MARGIN.left >= 100);
});
```

`web/test/glossary.test.mjs`:

```js
import test from 'node:test';
import assert from 'node:assert/strict';
import { FAULT_LABEL, LAYER_LABEL, POWER_FAULTS, describeFault } from '../static/js/glossary.js';

test('every fault kind and layer has a Portuguese label', () => {
  for (const k of ['charger_fail', 'charger_offline', 'limit_drop', 'soc_noise', 'soc_bias', 'soc_freeze', 'soc_missing',
    'consumption_over', 'late_arrival', 'early_departure', 'planner_panic', 'planner_slow', 'planner_garbage']) {
    assert.ok(FAULT_LABEL[k], k);
  }
  for (const l of ['normal', 'last-valid', 'safe']) assert.ok(LAYER_LABEL[l], l);
});

test('only power-related faults are drawn on the power chart', () => {
  assert.ok(POWER_FAULTS.has('limit_drop') && POWER_FAULTS.has('charger_fail') && POWER_FAULTS.has('planner_panic'));
  assert.ok(!POWER_FAULTS.has('soc_noise') && !POWER_FAULTS.has('late_arrival'));
});

test('describeFault names the target, the window and the value', () => {
  assert.equal(describeFault({ kind: 'limit_drop', target: '', from: 120, to: 300, value: 0.5 }), 'queda do limite da rede (×0,5), min 120–300');
  assert.equal(describeFault({ kind: 'charger_fail', target: 'C003', from: 10, to: 20, value: 0 }), 'carregador em falha (C003), min 10–20');
  assert.equal(describeFault({ kind: 'soc_noise', target: '*', from: 0, to: 50, value: 3 }), 'ruído na leitura de carga (3 kWh), min 0–50');
  assert.equal(describeFault({ kind: 'weird', target: '', from: 1, to: 2, value: 0 }), 'weird, min 1–2');
});
```

- [ ] **Step 2: Rodar e ver falhar**

Run: `node --test web/test/`
Expected: FAIL (módulos `scale.js`, `cursor.js`, novas exportações de `layout.js` e `glossary.js` inexistentes).

- [ ] **Step 3: Implementar os módulos puros**

`web/static/js/charts/scale.js`:

```js
// linear maps [d0, d1] to [r0, r1]; f.invert maps back. A degenerate domain maps to r0.
export function linear(d0, d1, r0, r1) {
  const k = d1 === d0 ? 0 : (r1 - r0) / (d1 - d0);
  const f = (v) => r0 + (v - d0) * k;
  f.invert = (p) => (k === 0 ? d0 : d0 + (p - r0) / k);
  return f;
}

// niceTicks returns round tick values between min and max (about `count` of them).
export function niceTicks(min, max, count = 5) {
  if (!(max > min)) return [min];
  const raw = (max - min) / count;
  const pow = 10 ** Math.floor(Math.log10(raw));
  const f = raw / pow;
  const step = (f < 1.5 ? 1 : f < 3 ? 2 : f < 7 ? 5 : 10) * pow;
  const out = [];
  for (let v = Math.ceil(min / step) * step; v <= max + step * 1e-9; v += step) out.push(+v.toFixed(10));
  return out;
}

export function timeTicks(horizon, stepMin = 60) {
  const out = [];
  for (let m = 0; m <= horizon; m += stepMin) out.push(m);
  return out;
}
```

`web/static/js/cursor.js`:

```js
// createCursor is the shared time cursor: an integer minute in [0, max].
export function createCursor(max) {
  let value = 0;
  const subs = new Set();
  const cursor = {
    max,
    get value() { return value; },
    set(v) {
      if (!Number.isFinite(v)) return;
      const n = Math.min(max, Math.max(0, Math.round(v)));
      if (n === value) return;
      value = n;
      subs.forEach((fn) => fn(n));
    },
    step(d) { cursor.set(value + d); },
    onChange(fn) { subs.add(fn); return () => subs.delete(fn); },
  };
  return cursor;
}

export function keyDelta(key, shift) {
  switch (key) {
    case 'ArrowRight': case 'ArrowUp': return shift ? 10 : 1;
    case 'ArrowLeft': case 'ArrowDown': return shift ? -10 : -1;
    case 'PageUp': return 60;
    case 'PageDown': return -60;
    default: return null;
  }
}

// bindCursorKeys lets a focusable element drive the cursor with the keyboard.
export function bindCursorKeys(el, cursor) {
  el.addEventListener('keydown', (e) => {
    if (e.key === 'Home') { cursor.set(0); e.preventDefault(); return; }
    if (e.key === 'End') { cursor.set(cursor.max); e.preventDefault(); return; }
    const d = keyDelta(e.key, e.shiftKey);
    if (d !== null) { cursor.step(d); e.preventDefault(); }
  });
}
```

Acrescente ao fim de `web/static/js/charts/layout.js`:

```js
// Margins shared by every time-based chart, so their time axes line up.
export const MARGIN = { left: 120, right: 16, top: 12, bottom: 28 };

const num = (n) => Math.round(n * 10) / 10;

// runs run-length encodes an array into [{from, to (inclusive), value}].
export function runs(arr) {
  const out = [];
  arr.forEach((value, i) => {
    const last = out[out.length - 1];
    if (last && last.value === value) last.to = i;
    else out.push({ from: i, to: i, value });
  });
  return out;
}

// stackLayers stacks series (missing values count as 0): [{lower, upper}] per series.
export function stackLayers(series) {
  if (series.length === 0) return [];
  let base = new Array(series[0].length).fill(0);
  return series.map((s) => {
    const upper = base.map((b, i) => b + (Number.isFinite(s[i]) ? s[i] : 0));
    const layer = { lower: base, upper };
    base = upper;
    return layer;
  });
}

export function areaPath(lower, upper, x, y) {
  if (upper.length === 0) return '';
  let d = `M${num(x(0))},${num(y(upper[0]))}`;
  for (let i = 1; i < upper.length; i++) d += `L${num(x(i))},${num(y(upper[i]))}`;
  for (let i = lower.length - 1; i >= 0; i--) d += `L${num(x(i))},${num(y(lower[i]))}`;
  return d + 'Z';
}

// linePath draws values[i] at x(i); `step` holds the previous value until the next
// minute (for a limit that changes at an instant); a missing value breaks the line.
export function linePath(values, x, y, step = false) {
  let d = '';
  let pen = false;
  let prevY = 0;
  values.forEach((v, i) => {
    if (!Number.isFinite(v)) { pen = false; return; }
    const px = num(x(i));
    const py = num(y(v));
    if (!pen) { d += `M${px},${py}`; pen = true; }
    else if (step) d += `L${px},${prevY}L${px},${py}`;
    else d += `L${px},${py}`;
    prevY = py;
  });
  return d;
}

// assignLanes gives each {from, to} interval a lane so overlapping ones do not share it.
export function assignLanes(items) {
  const laneEnd = [];
  return items.map((it) => {
    let lane = laneEnd.findIndex((end) => end < it.from);
    if (lane < 0) { lane = laneEnd.length; laneEnd.push(it.to); } else laneEnd[lane] = it.to;
    return lane;
  });
}

// valuesAt reads the run-level series at a minute (clamped); missing values are null.
export function valuesAt(run, minute) {
  const sr = run.series;
  const i = Math.min(Math.max(0, Math.round(minute)), sr.limit_kw.length - 1);
  const at = (a) => (a[i] === undefined ? null : a[i]);
  return { physical: at(sr.physical_kw), commanded: at(sr.commanded_kw), limit: at(sr.limit_kw), layer: sr.layer[i] };
}
```

Acrescente ao fim de `web/static/js/glossary.js`:

```js
export const FAULT_LABEL = {
  charger_fail: 'carregador em falha',
  charger_offline: 'carregador sem comunicação',
  limit_drop: 'queda do limite da rede',
  soc_noise: 'ruído na leitura de carga',
  soc_bias: 'viés na leitura de carga',
  soc_freeze: 'leitura de carga congelada',
  soc_missing: 'leitura de carga ausente',
  consumption_over: 'consumo acima do previsto',
  late_arrival: 'chegada atrasada',
  early_departure: 'saída antecipada',
  planner_panic: 'planejador travou',
  planner_slow: 'planejador lento',
  planner_garbage: 'planejador devolveu plano inválido',
};

export const LAYER_LABEL = { normal: 'normal', 'last-valid': 'último plano válido', safe: 'perfil seguro' };

// Faults that change the site's power picture (drawn on the power chart).
export const POWER_FAULTS = new Set(['limit_drop', 'charger_fail', 'charger_offline', 'planner_panic', 'planner_slow', 'planner_garbage']);

const VALUE_TEXT = {
  limit_drop: (v) => `×${String(v).replace('.', ',')}`,
  soc_noise: (v) => `${v} kWh`,
  soc_bias: (v) => `${v} kWh`,
  consumption_over: (v) => `+${v} kWh`,
  late_arrival: (v) => `${v} min`,
  early_departure: (v) => `saída no minuto ${v}`,
};

// describeFault: "<kind> (<target>, <value>), min <from>–<to>"; "*" means every bus.
export function describeFault(f) {
  const label = FAULT_LABEL[f.kind] || f.kind;
  const bits = [];
  if (f.target && f.target !== '*') bits.push(f.target);
  if (VALUE_TEXT[f.kind] && f.value) bits.push(VALUE_TEXT[f.kind](f.value));
  return `${label}${bits.length ? ` (${bits.join(', ')})` : ''}, min ${f.from}–${f.to}`;
}
```

- [ ] **Step 4: Rodar e ver passar**

Run: `node --test web/test/`
Expected: PASS (todos os arquivos de teste, incluindo os da Tarefa 4).

- [ ] **Step 5: `charts/power.js`**

```js
import { h } from '../dom.js';
import { s } from './svg.js';
import { linear, niceTicks, timeTicks } from './scale.js';
import { MARGIN, stackLayers, areaPath, linePath, runs, assignLanes, valuesAt } from './layout.js';
import { clock, fmtNum } from '../format.js';
import { LAYER_LABEL, POWER_FAULTS, describeFault } from '../glossary.js';
import { bindCursorKeys } from '../cursor.js';

const W = 1000;
const PLOT_H = 300;
const RIBBON_H = 14;
const LANE_H = 12;
const MAX_LANES = 4;

// renderPowerChart draws stacked charger power against the site limit, the layer that
// decided each minute and the power-related faults, with a shared time cursor.
export function renderPowerChart(root, data, cursor) {
  const { scenario, series } = data;
  const n = scenario.horizon_min;
  const x = linear(0, n, MARGIN.left, W - MARGIN.right);
  const finite = (a) => a.filter(Number.isFinite);
  const maxKW = Math.max(1, ...finite(series.limit_kw), ...finite(series.commanded_kw), ...finite(series.physical_kw));
  const ticks = niceTicks(0, maxKW * 1.05, 5);
  const yMax = Math.max(maxKW * 1.05, ticks[ticks.length - 1]);
  const plotTop = MARGIN.top;
  const plotBottom = plotTop + PLOT_H;
  const y = linear(0, yMax, plotBottom, plotTop);

  const faults = scenario.faults.filter((f) => POWER_FAULTS.has(f.kind)).sort((a, b) => a.from - b.from || a.to - b.to);
  const lanes = assignLanes(faults);
  const laneCount = Math.min(MAX_LANES, lanes.length ? Math.max(...lanes) + 1 : 0);
  const ribbonY = plotBottom + 24;
  const lanesY = ribbonY + RIBBON_H + 6;
  const totalH = lanesY + laneCount * LANE_H + 8;

  const stacked = stackLayers(series.chargers.map((c) => c.physical_kw));
  const parts = [
    s('defs', {},
      s('pattern', { id: 'pat-lv', width: 6, height: 6, patternUnits: 'userSpaceOnUse', patternTransform: 'rotate(45)' },
        s('rect', { width: 6, height: 6, class: 'pat-lv-bg' }), s('line', { x1: 0, y1: 0, x2: 0, y2: 6, class: 'pat-line', 'stroke-width': 2.5 })),
      s('pattern', { id: 'pat-safe', width: 6, height: 6, patternUnits: 'userSpaceOnUse' },
        s('rect', { width: 6, height: 6, class: 'pat-safe-bg' }), s('circle', { cx: 3, cy: 3, r: 1.4, class: 'pat-dot' }))),
  ];

  // y grid and labels
  for (const t of ticks) {
    parts.push(s('line', { x1: MARGIN.left, x2: W - MARGIN.right, y1: y(t), y2: y(t), class: 'grid' }),
      s('text', { x: MARGIN.left - 8, y: y(t) + 4, class: 'tick', 'text-anchor': 'end' }, fmtNum(t, 0)));
  }
  parts.push(s('text', { x: 8, y: plotTop + 10, class: 'axis-title' }, 'kW'));
  // x axis: one tick per hour, labelled with the wall clock
  for (const m of timeTicks(n, 60)) {
    parts.push(s('line', { x1: x(m), x2: x(m), y1: plotBottom, y2: plotBottom + 4, class: 'axis' }),
      s('text', { x: x(m), y: plotBottom + 17, class: 'tick', 'text-anchor': 'middle' }, clock(scenario.start_clock_min, m)));
  }
  parts.push(s('line', { x1: MARGIN.left, x2: W - MARGIN.right, y1: plotBottom, y2: plotBottom, class: 'axis' }));

  // stacked areas, one per charger (two alternating tints, separated by a thin line)
  stacked.forEach((layer, i) => {
    parts.push(s('path', { d: areaPath(layer.lower, layer.upper, x, y), class: i % 2 ? 'area area-b' : 'area area-a' },
      s('title', {}, `${series.chargers[i].id}: potência física`)));
  });
  parts.push(s('path', { d: linePath(series.commanded_kw, x, y, false), class: 'line-commanded' }),
    s('path', { d: linePath(series.limit_kw, x, y, true), class: 'line-limit' }));

  // layer ribbon
  parts.push(s('text', { x: MARGIN.left - 8, y: ribbonY + 11, class: 'tick', 'text-anchor': 'end' }, 'camada'));
  for (const r of runs(series.layer)) {
    parts.push(s('rect', { x: x(r.from), y: ribbonY, width: Math.max(1, x(r.to + 1) - x(r.from)), height: RIBBON_H, class: `layer layer-${r.value}` },
      s('title', {}, `${LAYER_LABEL[r.value] || r.value}: minutos ${r.from}–${r.to}`)));
  }
  // faults
  if (laneCount > 0) parts.push(s('text', { x: MARGIN.left - 8, y: lanesY + 10, class: 'tick', 'text-anchor': 'end' }, 'falhas'));
  faults.forEach((f, i) => {
    const lane = Math.min(lanes[i], MAX_LANES - 1);
    parts.push(s('rect', { x: x(f.from), y: lanesY + lane * LANE_H, width: Math.max(2, x(f.to) - x(f.from)), height: LANE_H - 2, class: 'fault' },
      s('title', {}, describeFault(f))));
  });

  // cursor and pointer capture
  const cursorLine = s('line', { y1: plotTop, y2: totalH - 6, class: 'cursor' });
  const overlay = s('rect', { x: MARGIN.left, y: plotTop, width: W - MARGIN.left - MARGIN.right, height: totalH - plotTop, class: 'overlay' });
  parts.push(cursorLine, overlay);

  const svg = s('svg', { viewBox: `0 0 ${W} ${totalH}`, class: 'chart power', role: 'img',
    'aria-label': 'Potência por carregador ao longo do tempo, contra o limite da garagem' }, parts);
  const toMinute = (clientX) => {
    const r = svg.getBoundingClientRect();
    return x.invert(((clientX - r.left) * W) / r.width);
  };
  overlay.addEventListener('pointerdown', (e) => { overlay.setPointerCapture(e.pointerId); cursor.set(toMinute(e.clientX)); });
  overlay.addEventListener('pointermove', (e) => { if (e.buttons) cursor.set(toMinute(e.clientX)); });

  const readout = h('div', { class: 'readout', 'aria-live': 'off' });
  const wrap = h('div', { class: 'chart-wrap', tabindex: 0, role: 'slider', 'aria-label': 'Cursor de tempo',
    'aria-valuemin': 0, 'aria-valuemax': n }, svg);
  bindCursorKeys(wrap, cursor);

  const swatch = (cls, text) => h('span', { class: 'legend-item' }, s('svg', { width: 22, height: 12, 'aria-hidden': 'true' }, s('rect', { width: 22, height: 12, class: cls })), text);
  const legend = h('div', { class: 'legend' },
    swatch('area-a', 'potência por carregador (física)'),
    h('span', { class: 'legend-item' }, s('svg', { width: 22, height: 12, 'aria-hidden': 'true' }, s('line', { x1: 0, x2: 22, y1: 6, y2: 6, class: 'line-commanded' })), 'potência comandada'),
    h('span', { class: 'legend-item' }, s('svg', { width: 22, height: 12, 'aria-hidden': 'true' }, s('line', { x1: 0, x2: 22, y1: 6, y2: 6, class: 'line-limit' })), 'limite da garagem'),
    swatch('layer-normal', 'camada normal'), swatch('layer-last-valid', 'último plano válido'), swatch('layer-safe', 'perfil seguro'),
    swatch('fault', 'falha ativa'));
  root.replaceChildren(h('h3', {}, 'Potência'), wrap, readout, legend);

  function update(m) {
    const px = x(m);
    cursorLine.setAttribute('x1', px);
    cursorLine.setAttribute('x2', px);
    wrap.setAttribute('aria-valuenow', m);
    const v = valuesAt(data, m);
    const kw = (val) => (val === null ? '—' : `${fmtNum(val, 0)} kW`);
    wrap.setAttribute('aria-valuetext', `minuto ${m}, ${clock(scenario.start_clock_min, m)}`);
    readout.textContent = `minuto ${m} · ${clock(scenario.start_clock_min, m)} — físico ${kw(v.physical)} · comandado ${kw(v.commanded)} · limite ${kw(v.limit)} · decidiu: ${LAYER_LABEL[v.layer] || v.layer}`;
  }
  cursor.onChange(update);
  update(cursor.value);
}
```

- [ ] **Step 6: `run.js` (carga da execução e painéis)**

```js
import { run as apiRun, createLatest, ApiError } from './api.js';
import { h, clear } from './dom.js';
import { createCursor } from './cursor.js';
import { renderPowerChart } from './charts/power.js';
import { fmtNum, fmtPct, fmtBRL, clock } from './format.js';
import { CONTROLLERS } from './params.js';
import { describeFault, CONTROLLER_HELP } from './glossary.js';

// createRunView owns the "Execução" tab: it loads one run and draws its panels.
export function createRunView(root, hooks) {
  const latest = createLatest();
  let current = null; // { data, cursor, panels } of the run on screen

  function summaryLine(m) {
    const bad = m.plan_violations > 0;
    return `${m.ready} de ${m.buses} ônibus saíram prontos (${fmtPct(m.ready_pct)}) · déficit ${fmtNum(m.shortfall_kwh, 0)} kWh · pico ${fmtNum(m.peak_kw, 0)} kW · ` +
      `${bad ? '⚠ ' : ''}violações do plano ${m.plan_violations} · custo ${fmtBRL(m.cost_brl)}`;
  }

  function controls(data) {
    const select = h('select', { id: 'run-controller', 'aria-label': 'Controlador' },
      CONTROLLERS.map((c) => h('option', { value: c, selected: c === data.controller }, c)));
    const seed = h('input', { id: 'run-seed', type: 'number', min: 1, max: 1000, step: 1, value: data.seed, 'aria-label': 'Semente' });
    const open = () => {
      const n = Number(seed.value);
      if (Number.isInteger(n) && n >= 1 && n <= 1000) hooks.onSelect(select.value, n);
    };
    select.addEventListener('change', open);
    seed.addEventListener('change', open);
    return h('div', { class: 'run-controls' },
      h('label', {}, 'Controlador ', select), h('label', {}, 'Semente ', seed),
      h('span', { class: 'note', title: CONTROLLER_HELP[data.controller] }, CONTROLLER_HELP[data.controller]));
  }

  function cursorRow(data, cursor) {
    const slider = h('input', { type: 'range', min: 0, max: data.scenario.horizon_min, step: 1, value: 0, 'aria-label': 'Minuto da simulação', class: 'time-slider' });
    const label = h('output', { class: 'time-label' });
    slider.addEventListener('input', () => cursor.set(Number(slider.value)));
    cursor.onChange((m) => {
      slider.value = m;
      label.textContent = `minuto ${m} · ${clock(data.scenario.start_clock_min, m)}`;
    });
    label.textContent = `minuto 0 · ${clock(data.scenario.start_clock_min, 0)}`;
    return h('div', { class: 'cursor-row' }, slider, label);
  }

  function faultList(data) {
    if (data.scenario.faults.length === 0) return h('p', { class: 'note' }, 'Este cenário não tem falhas.');
    return h('details', { class: 'faults' }, h('summary', {}, `Falhas deste cenário (${data.scenario.faults.length})`),
      h('ul', {}, data.scenario.faults.map((f) => h('li', {}, describeFault(f)))));
  }

  function render(data) {
    const cursor = createCursor(data.scenario.horizon_min);
    const powerPanel = h('div', { class: 'panel', id: 'panel-power' });
    const below = h('div', { id: 'panels-below' }); // filled by the timelines (task 6)
    clear(root).append(
      h('h2', {}, 'Execução'),
      controls(data),
      h('p', { class: 'summary' }, summaryLine(data.metrics)),
      faultList(data),
      cursorRow(data, cursor),
      powerPanel,
      below);
    renderPowerChart(powerPanel, data, cursor);
    current = { data, cursor, below };
    hooks.onRendered?.(current);
  }

  async function load(state) {
    clear(root).append(h('h2', {}, 'Execução'), h('p', { class: 'loading' }, 'Calculando a execução…'));
    current = null;
    try {
      const r = await latest((signal) => apiRun({ ...state.params, seed: state.seed, controller: state.controller }, signal));
      if (r.stale) return;
      render(r.value);
    } catch (e) {
      const msg = e instanceof ApiError ? e.message : 'Erro inesperado: ' + e.message;
      clear(root).append(h('h2', {}, 'Execução'), h('div', { class: 'banner-inline', role: 'alert' }, msg));
    }
  }

  return { load, getCurrent: () => current };
}
```

- [ ] **Step 7: Ligar a aba no `main.js` e o CSS**

Em `web/static/js/main.js`:
1. Acrescente o import: `import { createRunView } from './run.js';`
2. Perto de `let form;` acrescente `let runView;`.
3. Troque `openRun` por:

```js
export function openRun(controller, seed) {
  state.controller = controller;
  state.seed = seed;
  showTab('run');
  runView.load(state);
}
```

4. Em `init()`, depois de `form.write(state.params);` crie a visão da execução e trate links de execução:

```js
  runView = createRunView($('run-out'), { onSelect: (controller, seed) => openRun(controller, seed) });
```

e no fim de `init()`, depois de `if (state.tab === 'compare') runCompare(state.params);`:

```js
  if (state.tab === 'run') openRun(state.controller, state.seed);
```

Acrescente ao fim de `web/static/app.css`:

```css
.run-controls { display: flex; flex-wrap: wrap; gap: 8px 20px; align-items: center; margin: 8px 0; }
.run-controls label { color: var(--muted); font-size: .9rem; }
.run-controls select, .run-controls input { padding: 5px 8px; border: 1px solid var(--border); border-radius: 6px; background: var(--bg); color: var(--fg); font: inherit; width: auto; }
.run-controls input[type=number] { width: 6rem; }
.summary { font-weight: 600; margin: 4px 0; }
.faults summary { cursor: pointer; color: var(--muted); } .faults ul { margin: 4px 0 8px; padding-left: 20px; }
.cursor-row { display: flex; align-items: center; gap: 12px; margin: 8px 0; position: sticky; top: 0; background: var(--bg); padding: 6px 0; z-index: 2; }
.time-slider { flex: 1; } .time-label { min-width: 12rem; color: var(--muted); font-variant-numeric: tabular-nums; }
.panel { background: var(--card); border: 1px solid var(--border); border-radius: 8px; padding: 8px 12px; margin: 10px 0; }
.chart-wrap { outline-offset: 2px; }
.chart { width: 100%; height: auto; display: block; touch-action: none; }
.chart .grid { stroke: var(--grid); stroke-width: 1; } .chart .axis { stroke: var(--muted); stroke-width: 1; }
.chart .tick { fill: var(--muted); font-size: 11px; } .chart .axis-title { fill: var(--muted); font-size: 11px; }
.area { stroke: var(--card); stroke-width: .6; } .area-a { fill: var(--chart-a); } .area-b { fill: var(--chart-b); }
.line-limit { fill: none; stroke: var(--limit); stroke-width: 2.5; } .line-commanded { fill: none; stroke: var(--fg); stroke-width: 1.5; stroke-dasharray: 5 3; }
.layer-normal { fill: var(--ok); opacity: .55; } .layer-last-valid { fill: url(#pat-lv); } .layer-safe { fill: url(#pat-safe); }
.pat-lv-bg { fill: color-mix(in srgb, var(--amber) 30%, var(--card)); } .pat-safe-bg { fill: color-mix(in srgb, var(--bad) 25%, var(--card)); }
.pat-line { stroke: var(--amber); } .pat-dot { fill: var(--bad); }
.fault { fill: color-mix(in srgb, var(--bad) 35%, transparent); stroke: var(--bad); stroke-width: 1; stroke-dasharray: 3 2; }
.cursor { stroke: var(--fg); stroke-width: 1.5; pointer-events: none; } .overlay { fill: transparent; cursor: ew-resize; }
.readout { font-variant-numeric: tabular-nums; color: var(--muted); font-size: .9rem; margin: 4px 0; }
.legend { display: flex; flex-wrap: wrap; gap: 4px 16px; font-size: .8rem; color: var(--muted); margin-top: 6px; }
.legend-item { display: inline-flex; align-items: center; gap: 6px; }
.legend .area-a { fill: var(--chart-a); } .legend .layer-normal { fill: var(--ok); opacity: .55; }
.banner-inline { background: var(--bad-bg); color: var(--bad); border-left: 4px solid var(--bad); padding: 8px 12px; margin: 8px 0; }
```

- [ ] **Step 8: Testes automáticos**

Run: `node --test web/test/` — Expected: PASS.
Run: `go test -race -count=1 ./internal/lab` — Expected: PASS (`TestEmbeddedAssetsExist` percorre os imports novos).

- [ ] **Step 9: Verificar no navegador de verdade**

Run: `go run ./cmd/lab`; abra `http://127.0.0.1:8080`. Rode "Rede apertada (600 kW)" com 3 sementes e clique num ponto do planejador. Verifique (com capturas, se houver ferramenta de navegador; senão registre "verificação visual pendente"):
1. A aba Execução mostra o resumo, o seletor de controlador e de semente e o gráfico de potência com a linha do limite (laranja), a linha tracejada de comandado e áreas empilhadas.
2. Arrastar no gráfico, mexer no controle deslizante e usar as setas (com o gráfico focado) movem o cursor; a leitura embaixo mostra minuto, hora do relógio, potências e a camada.
3. Com perfil "severa", aparecem faixas de falha e, se a camada mudar, a fita mostra hachura (último plano válido) ou pontos (perfil seguro), além da cor.
4. Trocar o controlador para `fifo` recarrega a execução; o link (`#tab=run&...`) colado numa aba nova abre a mesma execução.
5. Semente `0` ou `1001` mostra mensagem de erro em português (sem tela em branco).
6. Tema escuro legível; o resumo mostra `⚠` apenas se houver violação (nunca deve haver).

- [ ] **Step 10: Commit**

```bash
gofmt -l . ; go vet ./... && go test -race -short ./... && node --test web/test/
git add web
git commit -m "feat(lab): execution tab with power chart, faults, layer ribbon and time cursor

Co-Authored-By: Claude Sonnet 5.5 <noreply@anthropic.com>"
git push
```

---

### Task 6: Aba Execução, parte 2 — linhas do tempo, painel de decisão e "por que não saiu pronto?"

**Files:**
- Create: `web/static/js/decision.js`, `web/static/js/decisionPanel.js`, `web/static/js/charts/gantt.js`, `web/static/js/charts/line.js`; testes `web/test/decision.test.mjs`, `web/test/fill.test.mjs`
- Modify: `web/static/js/charts/layout.js` (acrescentar `fillPath`), `web/static/js/run.js`, `web/static/app.css` (acrescentar)

**Interfaces:**
- Consumes: formato de `/api/run` (Tarefa 3): `data.scenario`, `data.series.{chargers,buses}`, `data.decisions`, `data.reasons`, `data.outcomes`; da Tarefa 5: `createCursor`, `bindCursorKeys`, `MARGIN`, `runs`, `linePath`, `linear`, `timeTicks`, `describeFault`, `LAYER_LABEL`, `createRunView`.
- Produces:
  - `layout.js`: `fillPath(values, x, y, yBase)`.
  - `decision.js` (puro): `decisionAt(decisions, minute, horizon)` → `{decision, from, to}` ou `null`; `swapMarkers(decisions)`; `chargerOccupancy(data)`; `busExposure(data, busIdx)`; `explainBus(data, busIdx)` → `string[]`; `createSelection()` → `{get(), set(id), onChange(fn)}`.
  - `charts/gantt.js`: `renderBusTimeline(root, data, cursor, {onSelectBus})` → `{setSelected(id)}`; `renderChargerTimeline(root, data, cursor)`.
  - `charts/line.js`: `renderSoCChart(root, data, busIdx, cursor)` → `{dispose()}`.
  - `decisionPanel.js`: `renderDecisionPanel(root, data, cursor, selection)`.

Formato dos dados usado (resumo): `series.buses[i] = {id, state[0|1|2], true_soc_kwh[], observed_kwh[] (null = sem leitura), charger[] (índice em series.chargers, -1 = nenhum)}`; `series.chargers[j] = {id, status[0 ok|1 falha|2 sem comunicação], commanded_kw[], physical_kw[]}` (mesma ordem de `scenario.chargers`); `decisions[k] = {minute, layer, setpoints:[{c,kw}], buses:[{b,ok,as,sf,r}], swaps:[{charger,out,in,reason}], notes[]}`; `outcomes[i] = {id, arrival, departure, capacity_kwh, initial_soc_kwh, forecast_target_kwh, true_target_kwh, final_soc_kwh, departed, ready, shortfall_kwh}`.

- [ ] **Step 1: Escrever os testes (Node) que falham**

`web/test/fill.test.mjs`:

```js
import test from 'node:test';
import assert from 'node:assert/strict';
import { fillPath } from '../static/js/charts/layout.js';

const id = (v) => v;

test('a filled area closes down to the baseline', () => {
  assert.equal(fillPath([1, 2], id, id, 0), 'M0,0L0,1L1,2L1,0Z');
});

test('a gap in the data starts a new area', () => {
  assert.equal(fillPath([1, null, 2], id, id, 0), 'M0,0L0,1L0,0ZM2,0L2,2L2,0Z');
});

test('no finite values give an empty path', () => {
  assert.equal(fillPath([], id, id, 0), '');
  assert.equal(fillPath([null, NaN], id, id, 0), '');
});
```

`web/test/decision.test.mjs`:

```js
import test from 'node:test';
import assert from 'node:assert/strict';
import { decisionAt, swapMarkers, chargerOccupancy, busExposure, explainBus, createSelection } from '../static/js/decision.js';

const decisions = [
  { minute: 0, layer: 'normal', swaps: [] },
  { minute: 10, layer: 'normal', swaps: [{ charger: 'C1', out: 'B1', in: 'B2', reason: 'troca' }] },
  { minute: 40, layer: 'safe', swaps: [] },
];

test('decisionAt finds the decision in force and how long it lasts', () => {
  assert.deepEqual(decisionAt(decisions, 0, 100), { decision: decisions[0], from: 0, to: 9 });
  assert.deepEqual(decisionAt(decisions, 25, 100), { decision: decisions[1], from: 10, to: 39 });
  assert.deepEqual(decisionAt(decisions, 100, 100), { decision: decisions[2], from: 40, to: 100 });
  assert.equal(decisionAt([], 5, 100), null);
  assert.equal(decisionAt([{ minute: 7 }], 3, 100), null);
});

test('swap markers give one entry per bus and role', () => {
  assert.deepEqual(swapMarkers(decisions), [
    { minute: 10, bus: 'B2', role: 'in', charger: 'C1', reason: 'troca' },
    { minute: 10, bus: 'B1', role: 'out', charger: 'C1', reason: 'troca' },
  ]);
});

// two chargers, two buses, five minutes; B1 waits, then charges on C1, B2 sits on a failed C2
const data = {
  scenario: { start_clock_min: 1080 },
  series: {
    limit_kw: [0, 0, 0, 0, 0],
    chargers: [
      { id: 'C1', status: [0, 0, 0, 0, 0], physical_kw: [0, 0, 50, 50, 0] },
      { id: 'C2', status: [0, 1, 1, 1, 0], physical_kw: [0, 0, 0, 0, 0] },
    ],
    buses: [
      { id: 'B1', state: [0, 1, 1, 1, 2], observed_kwh: [null, null, 50, null, null], charger: [-1, -1, 0, 0, -1] },
      { id: 'B2', state: [1, 1, 1, 1, 2], observed_kwh: [60, 60, 60, 60, null], charger: [1, 1, 1, 1, -1] },
    ],
  },
  outcomes: [
    { id: 'B1', arrival: 1, departure: 4, capacity_kwh: 300, initial_soc_kwh: 40, forecast_target_kwh: 200, true_target_kwh: 230, final_soc_kwh: 180, departed: true, ready: false, shortfall_kwh: 50 },
    { id: 'B2', arrival: 0, departure: 4, capacity_kwh: 300, initial_soc_kwh: 60, forecast_target_kwh: 100, true_target_kwh: 100, final_soc_kwh: 100, departed: true, ready: true, shortfall_kwh: 0 },
  ],
};

test('charger occupancy is derived from the bus series', () => {
  assert.deepEqual(chargerOccupancy(data), [['', '', 'B1', 'B1', ''], ['B2', 'B2', 'B2', 'B2', '']]);
});

test('bus exposure counts waiting, charging, failed chargers and missing readings', () => {
  assert.deepEqual(busExposure(data, 0), { waiting: 1, charging: 2, onFaulted: 0, onOffline: 0, noReading: 2, present: 3 });
  assert.deepEqual(busExposure(data, 1), { waiting: 0, charging: 0, onFaulted: 3, onOffline: 0, noReading: 0, present: 4 });
});

test('explainBus says what happened to a bus that left without its charge', () => {
  const text = explainBus(data, 0).join(' ');
  assert.match(text, /Saiu sem a carga/);
  assert.match(text, /faltaram 50 kWh/);
  assert.match(text, /consumo real passou do previsto/);
  assert.match(text, /esperando carregador/);
  assert.match(text, /sem leitura de carga/);
});

test('explainBus for a bus that left ready and one that never left', () => {
  assert.match(explainBus(data, 1).join(' '), /Saiu pronto/);
  const never = { ...data, outcomes: [{ ...data.outcomes[0], departed: false }, data.outcomes[1]] };
  assert.match(explainBus(never, 0).join(' '), /Não saiu dentro do horizonte/);
});

test('explainBus names the failed charger as the likely cause', () => {
  const o = { ...data.outcomes[1], ready: false, shortfall_kwh: 20, final_soc_kwh: 80 };
  const failed = { ...data, outcomes: [data.outcomes[0], o] };
  assert.match(explainBus(failed, 1).join(' '), /carregador com falha/);
});

test('explainBus blames the available power when the bus always had a working charger', () => {
  const clean = {
    ...data,
    series: { ...data.series, chargers: [{ id: 'C1', status: [0, 0, 0, 0, 0], physical_kw: [0, 10, 10, 10, 0] }],
      buses: [{ id: 'B1', state: [1, 1, 1, 1, 2], observed_kwh: [1, 1, 1, 1, null], charger: [0, 0, 0, 0, -1] }] },
    outcomes: [{ ...data.outcomes[0], arrival: 0 }],
  };
  assert.match(explainBus(clean, 0).join(' '), /potência disponível/);
});

test('selection notifies only on change', () => {
  const sel = createSelection();
  const seen = [];
  sel.onChange((id) => seen.push(id));
  sel.set('B1'); sel.set('B1'); sel.set(null);
  assert.deepEqual(seen, ['B1', null]);
  assert.equal(sel.get(), null);
});
```

- [ ] **Step 2: Rodar e ver falhar**

Run: `node --test web/test/`
Expected: FAIL (`decision.js` inexistente; `fillPath` não exportada).

- [ ] **Step 3: Implementar `fillPath` e `decision.js`**

Acrescente ao fim de `web/static/js/charts/layout.js`:

```js
// fillPath fills the area under values[i] down to yBase; a missing value starts a new area.
export function fillPath(values, x, y, yBase) {
  let d = '';
  let start = -1;
  const close = (end) => { d += `L${num(x(end))},${num(yBase)}Z`; };
  values.forEach((v, i) => {
    if (Number.isFinite(v)) {
      if (start < 0) { start = i; d += `M${num(x(i))},${num(yBase)}`; }
      d += `L${num(x(i))},${num(y(v))}`;
    } else if (start >= 0) {
      close(i - 1);
      start = -1;
    }
  });
  if (start >= 0) close(values.length - 1);
  return d;
}
```

`web/static/js/decision.js`:

```js
import { clock, duration, fmtNum } from './format.js';

// decisionAt finds the last decision taken at or before `minute` and the minutes it
// stayed in force (until the next decision, or the horizon).
export function decisionAt(decisions, minute, horizon) {
  let lo = 0;
  let hi = decisions.length - 1;
  let ans = -1;
  while (lo <= hi) {
    const mid = (lo + hi) >> 1;
    if (decisions[mid].minute <= minute) { ans = mid; lo = mid + 1; } else hi = mid - 1;
  }
  if (ans < 0) return null;
  const next = decisions[ans + 1];
  return { decision: decisions[ans], from: decisions[ans].minute, to: next ? next.minute - 1 : horizon };
}

// swapMarkers lists, per recommended swap, one marker for the bus that takes the charger
// and one for the bus that gives it up.
export function swapMarkers(decisions) {
  const out = [];
  for (const d of decisions) {
    for (const sw of d.swaps) {
      out.push({ minute: d.minute, bus: sw.in, role: 'in', charger: sw.charger, reason: sw.reason },
        { minute: d.minute, bus: sw.out, role: 'out', charger: sw.charger, reason: sw.reason });
    }
  }
  return out;
}

// chargerOccupancy gives, per charger, the bus plugged in at each minute ('' = none).
export function chargerOccupancy(data) {
  const n = data.series.limit_kw.length;
  const occ = data.series.chargers.map(() => new Array(n).fill(''));
  for (const b of data.series.buses) {
    b.charger.forEach((ci, i) => { if (ci >= 0 && occ[ci]) occ[ci][i] = b.id; });
  }
  return occ;
}

// busExposure counts the minutes a bus spent in each situation while it was present.
export function busExposure(data, busIdx) {
  const b = data.series.buses[busIdx];
  const e = { waiting: 0, charging: 0, onFaulted: 0, onOffline: 0, noReading: 0, present: 0 };
  b.state.forEach((st, i) => {
    if (st !== 1) return;
    e.present++;
    if (b.observed_kwh[i] === null || b.observed_kwh[i] === undefined) e.noReading++;
    const ci = b.charger[i];
    if (ci < 0) { e.waiting++; return; }
    const c = data.series.chargers[ci];
    if (c.status[i] === 1) e.onFaulted++;
    else if (c.status[i] === 2) e.onOffline++;
    if (c.physical_kw[i] > 0) e.charging++;
  });
  return e;
}

// explainBus answers "why did this bus (not) leave ready?" from the run's truth.
export function explainBus(data, busIdx) {
  const b = data.series.buses[busIdx];
  const o = data.outcomes.find((x) => x.id === b.id);
  const start = data.scenario.start_clock_min;
  const out = [];
  if (!o) return out;
  if (!o.departed) {
    out.push(`Não saiu dentro do horizonte da simulação (chegada às ${clock(start, o.arrival)}).`);
    return out;
  }
  const have = `${fmtNum(o.final_soc_kwh, 0)} kWh de ${fmtNum(o.true_target_kwh, 0)} kWh necessários`;
  out.push(o.ready
    ? `Saiu pronto às ${clock(start, o.departure)}: ${have}.`
    : `Saiu sem a carga às ${clock(start, o.departure)}: ${have} (faltaram ${fmtNum(o.shortfall_kwh, 0)} kWh).`);
  if (o.true_target_kwh > o.forecast_target_kwh + 0.5) {
    out.push(`O consumo real passou do previsto: o planejador contava com ${fmtNum(o.forecast_target_kwh, 0)} kWh e a rota exigiu ${fmtNum(o.true_target_kwh, 0)} kWh.`);
  }
  const e = busExposure(data, busIdx);
  const bits = [`carregando ${duration(e.charging)}`];
  if (e.waiting) bits.push(`esperando carregador ${duration(e.waiting)}`);
  if (e.onFaulted) bits.push(`em carregador com falha ${duration(e.onFaulted)}`);
  if (e.onOffline) bits.push(`em carregador sem comunicação ${duration(e.onOffline)}`);
  if (e.noReading) bits.push(`sem leitura de carga ${duration(e.noReading)}`);
  out.push(`No pátio por ${duration(e.present)}: ${bits.join(', ')}.`);
  if (!o.ready) {
    if (e.onFaulted || e.onOffline) out.push('Possível causa: ficou em carregador com falha ou sem comunicação.');
    else if (e.waiting) out.push('Possível causa: esperou por um carregador livre.');
    else out.push('Possível causa: teve carregador o tempo todo; a potência disponível (limite da garagem ou prioridade dada a outros ônibus) não bastou.');
  }
  return out;
}

// createSelection holds the selected bus id (or null) and notifies on change.
export function createSelection() {
  let id = null;
  const subs = new Set();
  return {
    get: () => id,
    set(v) {
      if (v === id) return;
      id = v;
      subs.forEach((fn) => fn(v));
    },
    onChange(fn) { subs.add(fn); return () => subs.delete(fn); },
  };
}
```

- [ ] **Step 4: Rodar e ver passar**

Run: `node --test web/test/`
Expected: PASS. Se `explainBus ... Possível causa` falhar no caso de `B2`, confira que o `status` 1 do carregador conta em `onFaulted`.

- [ ] **Step 5: `charts/gantt.js`**

```js
import { s } from './svg.js';
import { h } from '../dom.js';
import { linear, timeTicks } from './scale.js';
import { MARGIN, runs, fillPath } from './layout.js';
import { chargerOccupancy, swapMarkers } from '../decision.js';
import { clock, fmtNum } from '../format.js';
import { describeFault } from '../glossary.js';
import { bindCursorKeys } from '../cursor.js';

const W = 1000;
const ROW_H = 20;
const BUS_FAULTS = new Set(['soc_noise', 'soc_bias', 'soc_freeze', 'soc_missing', 'consumption_over', 'late_arrival', 'early_departure']);

function timeAxis(x, n, startClock, y) {
  const parts = [s('line', { x1: MARGIN.left, x2: W - MARGIN.right, y1: y, y2: y, class: 'axis' })];
  for (const m of timeTicks(n, 60)) {
    parts.push(s('line', { x1: x(m), x2: x(m), y1: y, y2: y + 4, class: 'axis' }),
      s('text', { x: x(m), y: y + 17, class: 'tick', 'text-anchor': 'middle' }, clock(startClock, m)));
  }
  return parts;
}

// Pointer on the plot moves the cursor; `onRow` hears the row (data-key) that was pressed.
function bindPointer(svg, x, cursor, onRow) {
  const toMinute = (clientX) => {
    const r = svg.getBoundingClientRect();
    return x.invert(((clientX - r.left) * W) / r.width);
  };
  svg.addEventListener('pointerdown', (e) => {
    const row = e.target.closest('[data-key]');
    if (row && onRow) onRow(row.dataset.key);
    svg.setPointerCapture(e.pointerId);
    cursor.set(toMinute(e.clientX));
  });
  svg.addEventListener('pointermove', (e) => { if (e.buttons) cursor.set(toMinute(e.clientX)); });
}

function cursorLine(parts, y1, y2) {
  const line = s('line', { y1, y2, class: 'cursor' });
  parts.push(line);
  return line;
}

const legendItem = (symbol, text) => h('span', { class: 'legend-item' }, symbol, text);

// renderBusTimeline: one row per bus, from arrival to departure, filled with the true
// charge; a mark at departure says whether the bus was ready.
export function renderBusTimeline(root, data, cursor, hooks) {
  const n = data.scenario.horizon_min;
  const x = linear(0, n, MARGIN.left, W - MARGIN.right);
  const buses = data.series.buses;
  const outcomes = new Map(data.outcomes.map((o) => [o.id, o]));
  const top = MARGIN.top;
  const bottom = top + buses.length * ROW_H;
  const markers = swapMarkers(data.decisions);
  const rowNodes = new Map();
  const parts = [];

  buses.forEach((b, i) => {
    const o = outcomes.get(b.id);
    const y0 = top + i * ROW_H;
    const barY = y0 + 3;
    const barH = ROW_H - 6;
    const g = s('g', { class: 'row', 'data-key': b.id });
    g.append(s('rect', { x: 0, y: y0, width: W, height: ROW_H, class: 'row-bg' }));
    const label = s('text', { x: MARGIN.left - 8, y: y0 + ROW_H / 2 + 4, class: 'row-label', 'text-anchor': 'end', tabindex: 0, role: 'button', 'aria-label': `Ônibus ${b.id}: ver detalhes` }, b.id);
    label.addEventListener('keydown', (e) => { if (e.key === 'Enter' || e.key === ' ') { e.preventDefault(); hooks.onSelectBus(b.id); } });
    label.addEventListener('click', () => hooks.onSelectBus(b.id));
    g.append(label);
    if (o) {
      const from = Math.max(0, o.arrival);
      const to = Math.min(n, o.departure);
      g.append(s('rect', { x: x(from), y: barY, width: Math.max(2, x(to) - x(from)), height: barH, class: 'bus-bar' }));
      const frac = (v) => (Number.isFinite(v) ? v / o.capacity_kwh : null);
      g.append(s('path', { d: fillPath(b.true_soc_kwh.map(frac), x, (f) => barY + barH * (1 - f), barY + barH), class: 'soc-fill' }));
      const ty = barY + barH * (1 - o.forecast_target_kwh / o.capacity_kwh);
      g.append(s('line', { x1: x(from), x2: x(to), y1: ty, y2: ty, class: 'target-line' }, s('title', {}, `alvo previsto: ${fmtNum(o.forecast_target_kwh, 0)} kWh`)));
      const dx = x(Math.min(n, o.departure));
      const result = !o.departed ? 'não saiu dentro do horizonte' : o.ready ? 'saiu pronto' : `saiu sem a carga (faltaram ${fmtNum(o.shortfall_kwh, 0)} kWh)`;
      const title = s('title', {}, `${b.id}: ${result}`);
      if (!o.departed) g.append(s('circle', { cx: dx, cy: y0 + ROW_H / 2, r: 4, class: 'mark-pending' }, title));
      else if (o.ready) g.append(s('circle', { cx: dx, cy: y0 + ROW_H / 2, r: 4.5, class: 'mark-ready' }, title));
      else g.append(s('path', { d: `M${dx - 4},${y0 + ROW_H / 2 - 4}l8,8m0,-8l-8,8`, class: 'mark-fail' }, title));
    }
    rowNodes.set(b.id, g);
    parts.push(g);
  });

  // bus-level faults and recommended swaps
  const rowOf = new Map(buses.map((b, i) => [b.id, top + i * ROW_H]));
  for (const f of data.scenario.faults) {
    if (!BUS_FAULTS.has(f.kind) || !rowOf.has(f.target)) continue;
    const y = rowOf.get(f.target);
    parts.push(s('path', { d: `M${x(f.from)},${y + 2}l4,0l-2,5z`, class: 'bus-fault' }, s('title', {}, describeFault(f))));
  }
  for (const m of markers) {
    if (!rowOf.has(m.bus)) continue;
    const y = rowOf.get(m.bus);
    const up = m.role === 'in';
    parts.push(s('path', { d: up ? `M${x(m.minute)},${y + ROW_H - 2}l-4,0l2,-6z` : `M${x(m.minute)},${y + 2}l-4,0l2,6z`, class: up ? 'swap-in' : 'swap-out' },
      s('title', {}, `${up ? 'recebe' : 'cede'} o carregador ${m.charger}: ${m.reason}`)));
  }
  parts.push(...timeAxis(x, n, data.scenario.start_clock_min, bottom));
  const line = cursorLine(parts, top, bottom);
  const svg = s('svg', { viewBox: `0 0 ${W} ${bottom + 26}`, class: 'chart bus-timeline', role: 'group', 'aria-label': 'Linha do tempo dos ônibus' }, parts);
  bindPointer(svg, x, cursor, hooks.onSelectBus);

  const wrap = h('div', { class: 'chart-wrap', tabindex: 0, role: 'slider', 'aria-label': 'Cursor de tempo', 'aria-valuemin': 0, 'aria-valuemax': n }, svg);
  bindCursorKeys(wrap, cursor);
  const legend = h('div', { class: 'legend' },
    legendItem(s('svg', { width: 22, height: 12, 'aria-hidden': 'true' }, s('rect', { width: 22, height: 12, class: 'bus-bar' }), s('rect', { y: 6, width: 22, height: 6, class: 'soc-fill' })), 'ônibus no pátio, preenchido pela carga real'),
    legendItem(s('svg', { width: 22, height: 12, 'aria-hidden': 'true' }, s('line', { x1: 0, x2: 22, y1: 6, y2: 6, class: 'target-line' })), 'alvo previsto'),
    legendItem(s('svg', { width: 12, height: 12, 'aria-hidden': 'true' }, s('circle', { cx: 6, cy: 6, r: 4.5, class: 'mark-ready' })), 'saiu pronto'),
    legendItem(s('svg', { width: 12, height: 12, 'aria-hidden': 'true' }, s('path', { d: 'M2,2l8,8m0,-8l-8,8', class: 'mark-fail' })), 'saiu sem a carga'),
    legendItem(s('svg', { width: 12, height: 12, 'aria-hidden': 'true' }, s('path', { d: 'M1,11l10,0l-5,-9z', class: 'swap-in' })), 'recebe carregador (rodízio recomendado)'),
    legendItem(s('svg', { width: 12, height: 12, 'aria-hidden': 'true' }, s('path', { d: 'M1,1l10,0l-5,9z', class: 'swap-out' })), 'cede carregador (rodízio recomendado)'),
    legendItem(s('svg', { width: 12, height: 12, 'aria-hidden': 'true' }, s('path', { d: 'M1,1l10,0l-5,9z', class: 'bus-fault' })), 'falha do ônibus (leitura, consumo ou horário)'));
  root.replaceChildren(h('h3', {}, 'Ônibus'), h('p', { class: 'note' }, 'Clique numa linha (ou no código do ônibus) para ver por que ele saiu pronto ou não.'), wrap, legend);

  cursor.onChange((m) => { line.setAttribute('x1', x(m)); line.setAttribute('x2', x(m)); });
  line.setAttribute('x1', x(cursor.value)); line.setAttribute('x2', x(cursor.value));
  return {
    setSelected(id) { for (const [k, g] of rowNodes) g.classList.toggle('selected', k === id); },
  };
}

// renderChargerTimeline: one row per charger with the bus plugged in, its power and
// the failure windows.
export function renderChargerTimeline(root, data, cursor) {
  const n = data.scenario.horizon_min;
  const x = linear(0, n, MARGIN.left, W - MARGIN.right);
  const chargers = data.series.chargers;
  const occupancy = chargerOccupancy(data);
  const ROW = 18;
  const top = MARGIN.top;
  const bottom = top + chargers.length * ROW;
  const parts = [s('defs', {},
    s('pattern', { id: 'pat-fail', width: 6, height: 6, patternUnits: 'userSpaceOnUse', patternTransform: 'rotate(45)' },
      s('rect', { width: 6, height: 6, class: 'pat-fail-bg' }), s('line', { x1: 0, y1: 0, x2: 0, y2: 6, class: 'pat-fail-line', 'stroke-width': 2.5 })),
    s('pattern', { id: 'pat-offline', width: 6, height: 6, patternUnits: 'userSpaceOnUse' },
      s('rect', { width: 6, height: 6, class: 'pat-offline-bg' }), s('circle', { cx: 3, cy: 3, r: 1.4, class: 'pat-offline-dot' })))];
  const maxOf = new Map(data.scenario.chargers.map((c) => [c.id, c.max_kw]));

  chargers.forEach((c, i) => {
    const y0 = top + i * ROW;
    parts.push(s('rect', { x: 0, y: y0, width: W, height: ROW, class: 'row-bg' }),
      s('text', { x: MARGIN.left - 8, y: y0 + ROW / 2 + 4, class: 'row-label', 'text-anchor': 'end' }, c.id));
    const max = maxOf.get(c.id) || 1;
    for (const r of runs(occupancy[i])) {
      if (r.value === '') continue;
      const w = Math.max(1, x(r.to + 1) - x(r.from));
      parts.push(s('rect', { x: x(r.from), y: y0 + 2, width: w, height: ROW - 4, class: 'occ' }, s('title', {}, `${r.value} em ${c.id}: minutos ${r.from}–${r.to}`)));
      const slice = c.physical_kw.slice(r.from, r.to + 1).map((v) => (Number.isFinite(v) ? v / max : null));
      parts.push(s('path', { d: fillPath(slice, (k) => x(r.from + k), (f) => y0 + ROW - 2 - (ROW - 4) * f, y0 + ROW - 2), class: 'power-fill' }));
      if (w > 34) parts.push(s('text', { x: x(r.from) + 3, y: y0 + ROW / 2 + 4, class: 'occ-label' }, r.value));
    }
    for (const r of runs(c.status)) {
      if (r.value === 0) continue;
      const failed = r.value === 1;
      parts.push(s('rect', { x: x(r.from), y: y0 + 1, width: Math.max(2, x(r.to + 1) - x(r.from)), height: ROW - 2, class: failed ? 'status-fail' : 'status-offline' },
        s('title', {}, `${c.id}: ${failed ? 'em falha' : 'sem comunicação (mantém a última potência)'}, minutos ${r.from}–${r.to}`)));
    }
  });
  parts.push(...timeAxis(x, n, data.scenario.start_clock_min, bottom));
  const line = cursorLine(parts, top, bottom);
  const svg = s('svg', { viewBox: `0 0 ${W} ${bottom + 26}`, class: 'chart charger-timeline', role: 'group', 'aria-label': 'Linha do tempo dos carregadores' }, parts);
  bindPointer(svg, x, cursor, null);
  const wrap = h('div', { class: 'chart-wrap', tabindex: 0, role: 'slider', 'aria-label': 'Cursor de tempo', 'aria-valuemin': 0, 'aria-valuemax': n }, svg);
  bindCursorKeys(wrap, cursor);
  const legend = h('div', { class: 'legend' },
    legendItem(s('svg', { width: 22, height: 12, 'aria-hidden': 'true' }, s('rect', { width: 22, height: 12, class: 'occ' }), s('rect', { y: 6, width: 22, height: 6, class: 'power-fill' })), 'ônibus ligado (altura = potência / máximo do carregador)'),
    legendItem(s('svg', { width: 22, height: 12, 'aria-hidden': 'true' }, s('rect', { width: 22, height: 12, class: 'status-fail' })), 'carregador em falha'),
    legendItem(s('svg', { width: 22, height: 12, 'aria-hidden': 'true' }, s('rect', { width: 22, height: 12, class: 'status-offline' })), 'sem comunicação'));
  root.replaceChildren(h('h3', {}, 'Carregadores'), wrap, legend);
  cursor.onChange((m) => { line.setAttribute('x1', x(m)); line.setAttribute('x2', x(m)); });
  line.setAttribute('x1', x(cursor.value)); line.setAttribute('x2', x(cursor.value));
}
```

- [ ] **Step 6: `charts/line.js` (carga real contra a lida de um ônibus)**

```js
import { s } from './svg.js';
import { h } from '../dom.js';
import { linear, niceTicks, timeTicks } from './scale.js';
import { MARGIN, linePath } from './layout.js';
import { clock, fmtNum } from '../format.js';

const W = 1000;
const H = 190;

// renderSoCChart draws one bus's true charge, what the planner was told (gaps = no
// reading) and its forecast and true targets. dispose() unsubscribes from the cursor.
export function renderSoCChart(root, data, busIdx, cursor) {
  const n = data.scenario.horizon_min;
  const b = data.series.buses[busIdx];
  const o = data.outcomes.find((x) => x.id === b.id);
  const x = linear(0, n, MARGIN.left, W - MARGIN.right);
  const top = 10;
  const bottom = H - 28;
  const y = linear(0, o.capacity_kwh, bottom, top);
  const parts = [];
  for (const t of niceTicks(0, o.capacity_kwh, 4)) {
    parts.push(s('line', { x1: MARGIN.left, x2: W - MARGIN.right, y1: y(t), y2: y(t), class: 'grid' }),
      s('text', { x: MARGIN.left - 8, y: y(t) + 4, class: 'tick', 'text-anchor': 'end' }, fmtNum(t, 0)));
  }
  parts.push(s('text', { x: 8, y: top + 10, class: 'axis-title' }, 'kWh'));
  for (const m of timeTicks(n, 60)) {
    parts.push(s('text', { x: x(m), y: bottom + 16, class: 'tick', 'text-anchor': 'middle' }, clock(data.scenario.start_clock_min, m)));
  }
  parts.push(s('line', { x1: MARGIN.left, x2: W - MARGIN.right, y1: bottom, y2: bottom, class: 'axis' }));
  const hline = (v, cls, label) => parts.push(s('line', { x1: x(Math.max(0, o.arrival)), x2: x(Math.min(n, o.departure)), y1: y(v), y2: y(v), class: cls }, s('title', {}, label)));
  hline(o.forecast_target_kwh, 'target-line wide', `alvo previsto: ${fmtNum(o.forecast_target_kwh, 0)} kWh`);
  if (Math.abs(o.true_target_kwh - o.forecast_target_kwh) > 0.5) hline(o.true_target_kwh, 'true-target-line', `alvo real: ${fmtNum(o.true_target_kwh, 0)} kWh`);
  parts.push(s('path', { d: linePath(b.observed_kwh, x, y, false), class: 'line-observed' }),
    s('path', { d: linePath(b.true_soc_kwh, x, y, false), class: 'line-true' }));
  const cur = s('line', { y1: top, y2: bottom, class: 'cursor' });
  parts.push(cur);
  const svg = s('svg', { viewBox: `0 0 ${W} ${H}`, class: 'chart soc', role: 'img', 'aria-label': `Carga do ônibus ${b.id}: real contra a lida pelo planejador` }, parts);
  const swatch = (cls, text) => h('span', { class: 'legend-item' }, s('svg', { width: 22, height: 12, 'aria-hidden': 'true' }, s('line', { x1: 0, x2: 22, y1: 6, y2: 6, class: cls })), text);
  root.replaceChildren(svg, h('div', { class: 'legend' }, swatch('line-true', 'carga real'), swatch('line-observed', 'carga lida pelo planejador (vazio = sem leitura)'),
    swatch('target-line wide', 'alvo previsto'), swatch('true-target-line', 'alvo real (se diferente)')));
  const move = (m) => { cur.setAttribute('x1', x(m)); cur.setAttribute('x2', x(m)); };
  move(cursor.value);
  return { dispose: cursor.onChange(move) };
}
```

- [ ] **Step 7: `decisionPanel.js`**

```js
import { h, clear } from './dom.js';
import { decisionAt, explainBus } from './decision.js';
import { renderSoCChart } from './charts/line.js';
import { valuesAt } from './charts/layout.js';
import { clock, fmtNum } from './format.js';
import { LAYER_LABEL } from './glossary.js';

// renderDecisionPanel shows what the controller decided at the cursor minute and, for the
// selected bus, why it did or did not leave ready.
export function renderDecisionPanel(root, data, cursor, selection) {
  const n = data.scenario.horizon_min;
  const start = data.scenario.start_clock_min;
  const busIdx = new Map(data.series.buses.map((b, i) => [b.id, i]));
  const body = h('div', { class: 'decision-body' });
  const detail = h('div', { class: 'bus-detail' });
  root.replaceChildren(h('h3', {}, 'Decisão neste minuto'), body, detail);
  let disposeChart = null;

  function chargerRows(d, m) {
    const set = new Map(d.setpoints.map((p) => [p.c, p.kw]));
    return data.series.chargers.map((c, i) => {
      const bus = data.series.buses.find((b) => b.charger[Math.min(m, b.charger.length - 1)] === i);
      const status = c.status[m];
      return h('tr', {}, h('th', { scope: 'row' }, c.id),
        h('td', {}, set.has(c.id) ? `${fmtNum(set.get(c.id), 1)} kW` : '0 kW (não listado)'),
        h('td', {}, `${fmtNum(c.physical_kw[m], 1)} kW`),
        h('td', {}, bus ? bus.id : '—'),
        h('td', {}, status === 1 ? '⚠ em falha' : status === 2 ? '⚠ sem comunicação' : 'ok'));
    });
  }

  function busRows(d) {
    const order = [...d.buses].sort((a, b) => Number(a.ok) - Number(b.ok) || b.sf - a.sf || (a.b < b.b ? -1 : 1));
    return order.map((b) => h('tr', { class: selection.get() === b.b ? 'selected' : '', tabindex: 0, 'data-bus': b.b,
      onclick: () => selection.set(b.b), onkeydown: (e) => { if (e.key === 'Enter') selection.set(b.b); } },
    h('th', { scope: 'row' }, b.b),
    h('td', {}, b.as ? (b.ok ? '✓ atinge o alvo' : '✗ não atinge') : 'não avaliado'),
    h('td', {}, b.ok ? '—' : `${fmtNum(b.sf, 0)} kWh`),
    h('td', { class: 'reason' }, b.r >= 0 ? data.reasons[b.r] : '—')));
  }

  function update() {
    const m = cursor.value;
    const found = decisionAt(data.decisions, m, n);
    clear(body);
    if (!found) {
      body.append(h('p', { class: 'note' }, 'Ainda não havia decisão neste minuto.'));
      return;
    }
    const d = found.decision;
    const v = valuesAt(data, m);
    body.append(
      h('p', { class: 'note' }, `Minuto ${m} (${clock(start, m)}). Decisão tomada no minuto ${found.from} (${clock(start, found.from)}), em vigor até o minuto ${found.to}. Camada: ${LAYER_LABEL[d.layer] || d.layer}. ` +
        `O texto dos motivos é o do minuto em que a decisão foi tomada; potência física agora: ${v.physical === null ? '—' : fmtNum(v.physical, 0) + ' kW'}.`),
      d.swaps.length ? h('div', {}, h('h4', {}, 'Rodízios recomendados'), h('ul', {}, d.swaps.map((sw) => h('li', {}, `${sw.in} assume ${sw.charger} no lugar de ${sw.out}. ${sw.reason}`)))) : null,
      d.notes.length ? h('div', {}, h('h4', {}, 'Observações do planejador'), h('ul', {}, d.notes.map((t) => h('li', {}, t)))) : null,
      h('h4', {}, 'Carregadores'),
      h('div', { class: 'table-wrap' }, h('table', { class: 'metrics small' }, h('thead', {}, h('tr', {}, ['carregador', 'potência comandada', 'potência física', 'ônibus ligado', 'estado'].map((t) => h('th', { scope: 'col' }, t)))), h('tbody', {}, chargerRows(d, m)))),
      h('p', { class: 'note' }, 'Carregadores que o plano não lista ficam em 0 kW.'),
      h('h4', {}, 'Ônibus'),
      d.buses.length === 0
        ? h('p', { class: 'note' }, 'Este controlador não explica suas decisões ônibus a ônibus: só a potência comandada por carregador (acima). Escolha o controlador "planner" para ver os motivos.')
        : h('div', { class: 'table-wrap' }, h('table', { class: 'metrics small' }, h('thead', {}, h('tr', {}, ['ônibus', 'previsão', 'déficit previsto', 'motivo'].map((t) => h('th', { scope: 'col' }, t)))), h('tbody', {}, busRows(d)))));
  }

  function renderDetail() {
    if (disposeChart) { disposeChart(); disposeChart = null; }
    clear(detail);
    const id = selection.get();
    if (id === null || !busIdx.has(id)) {
      detail.append(h('p', { class: 'note' }, 'Selecione um ônibus (na linha do tempo ou na tabela) para ver por que ele saiu pronto ou não.'));
      return;
    }
    const i = busIdx.get(id);
    const chart = h('div', { class: 'soc-chart' });
    detail.append(h('h3', {}, `Ônibus ${id}`), h('ul', { class: 'explain' }, explainBus(data, i).map((t) => h('li', {}, t))), chart);
    disposeChart = renderSoCChart(chart, data, i, cursor).dispose;
  }

  cursor.onChange(update);
  selection.onChange(() => { update(); renderDetail(); });
  update();
  renderDetail();
}
```

- [ ] **Step 8: Ligar tudo no `run.js` e acrescentar o CSS**

Em `web/static/js/run.js`:
1. Imports novos:

```js
import { createSelection } from './decision.js';
import { renderBusTimeline, renderChargerTimeline } from './charts/gantt.js';
import { renderDecisionPanel } from './decisionPanel.js';
```

2. Em `render(data)`, troque o bloco que cria `below` e o fim da função por:

```js
    const cursor = createCursor(data.scenario.horizon_min);
    const selection = createSelection();
    const powerPanel = h('div', { class: 'panel', id: 'panel-power' });
    const busPanel = h('div', { class: 'panel', id: 'panel-buses' });
    const chargerPanel = h('div', { class: 'panel', id: 'panel-chargers' });
    const decisionPanel = h('div', { class: 'panel', id: 'panel-decision' });
    clear(root).append(
      h('h2', {}, 'Execução'),
      controls(data),
      h('p', { class: 'summary' }, summaryLine(data.metrics)),
      faultList(data),
      cursorRow(data, cursor),
      powerPanel, busPanel, chargerPanel, decisionPanel);
    renderPowerChart(powerPanel, data, cursor);
    const busView = renderBusTimeline(busPanel, data, cursor, { onSelectBus: (id) => selection.set(id) });
    selection.onChange((id) => busView.setSelected(id));
    renderChargerTimeline(chargerPanel, data, cursor);
    renderDecisionPanel(decisionPanel, data, cursor, selection);
    current = { data, cursor, selection };
```

(e apague as linhas antigas `const below = ...`, `renderPowerChart(...)` duplicada e `hooks.onRendered?.(current);`.)

Acrescente ao fim de `web/static/app.css`:

```css
.row-bg { fill: transparent; } .row:hover .row-bg { fill: color-mix(in srgb, var(--accent) 8%, transparent); } .row.selected .row-bg { fill: color-mix(in srgb, var(--accent) 16%, transparent); }
.row-label { fill: var(--muted); font-size: 11px; cursor: pointer; } .row.selected .row-label { fill: var(--fg); font-weight: 700; }
.bus-bar { fill: var(--grid); stroke: var(--border); stroke-width: .8; } .soc-fill { fill: var(--chart-a); opacity: .85; }
.target-line { stroke: var(--fg); stroke-width: 1; stroke-dasharray: 3 2; opacity: .7; } .target-line.wide { stroke-width: 1.5; fill: none; }
.true-target-line { stroke: var(--limit); stroke-width: 1.5; fill: none; }
.mark-ready { fill: var(--ok); stroke: var(--card); stroke-width: 1; } .mark-fail { stroke: var(--bad); stroke-width: 2.4; fill: none; } .mark-pending { fill: none; stroke: var(--muted); stroke-width: 1.5; }
.swap-in { fill: var(--ok); } .swap-out { fill: var(--amber); } .bus-fault { fill: var(--purple); }
.occ { fill: var(--grid); stroke: var(--border); stroke-width: .6; } .power-fill { fill: var(--chart-a); opacity: .85; } .occ-label { fill: var(--fg); font-size: 10px; pointer-events: none; }
.status-fail { fill: url(#pat-fail); stroke: var(--bad); stroke-width: 1; } .status-offline { fill: url(#pat-offline); stroke: var(--amber); stroke-width: 1; }
.pat-fail-bg { fill: color-mix(in srgb, var(--bad) 25%, var(--card)); } .pat-fail-line { stroke: var(--bad); } .pat-offline-bg { fill: color-mix(in srgb, var(--amber) 25%, var(--card)); } .pat-offline-dot { fill: var(--amber); }
.line-true { fill: none; stroke: var(--chart-a); stroke-width: 2.5; } .line-observed { fill: none; stroke: var(--purple); stroke-width: 1.5; stroke-dasharray: 2 3; }
.explain { margin: 4px 0 10px; padding-left: 20px; } .explain li { margin: 2px 0; }
table.metrics.small th, table.metrics.small td { padding: 4px 8px; font-size: .85rem; text-align: left; white-space: normal; }
table.metrics.small td.reason { max-width: 60ch; } table.metrics tr.selected { background: color-mix(in srgb, var(--accent) 16%, transparent); } table.metrics tr[data-bus] { cursor: pointer; }
.decision-body h4 { margin: 14px 0 4px; font-size: .95rem; }
```

- [ ] **Step 9: Testes automáticos**

Run: `node --test web/test/` — Expected: PASS.
Run: `go test -race -count=1 ./internal/lab` — Expected: PASS (todos os imports novos são conferidos).

- [ ] **Step 10: Verificar no navegador de verdade**

Run: `go run ./cmd/lab`; abra `http://127.0.0.1:8080`, rode "Falhas severas" com 3 sementes, abra uma semente do `planner`. Verifique (capturas se houver ferramenta; senão registre "verificação visual pendente"):
1. A aba Execução mostra, de cima para baixo, o gráfico de potência, a linha do tempo dos ônibus (50 linhas), a dos carregadores (25 linhas) e o painel de decisão; os eixos de tempo estão alinhados.
2. Mover o cursor atualiza as três linhas verticais, a leitura de potência e o painel de decisão (minuto, decisão em vigor, tabela de carregadores com ônibus ligado e estado).
3. Clicar numa linha de ônibus (ou no código) destaca a linha e mostra "Ônibus Bxxx" com frases explicando (esperou carregador, carregador com falha, sem leitura, consumo acima do previsto, possível causa) e o gráfico de carga real contra a lida.
4. Um ônibus que saiu sem a carga mostra ✗ no fim da barra e um que saiu pronto mostra ●; marcas de rodízio (▲ ▼) aparecem nas linhas envolvidas com a explicação ao passar o mouse.
5. Trocar o controlador para `fifo`: o painel mostra "Este controlador não explica suas decisões…" e as demais telas funcionam; trocar a semente recarrega.
6. Perfil "nenhuma" com 5 ônibus e 3 carregadores: telas funcionam com poucas linhas; tema escuro legível; tudo operável pelo teclado (Tab até o gráfico, setas movem o cursor; Enter no código do ônibus seleciona).

- [ ] **Step 11: Commit**

```bash
gofmt -l . ; go vet ./... && go test -race -short ./... && node --test web/test/
git add web
git commit -m "feat(lab): bus and charger timelines, decision panel and per-bus explanation

Co-Authored-By: Claude Sonnet 5.5 <noreply@anthropic.com>"
git push
```

---

### Task 7: Acabamento — link copiável, ajuda, README e verificação final

**Files:**
- Modify: `web/static/index.html`, `web/static/js/main.js`, `web/static/app.css`, `README.md`, `docs/superpowers/specs/2026-10-05-laboratorio-web-design.md` (só a linha de status)

**Interfaces:**
- Consumes: tudo das Tarefas 1–6 (`stateToHash`, `syncHash`, `showTab`, abas, rotas).
- Produces: botão "Copiar link desta tela" (`#copy-link`), aviso `#toast`, painel "Como ler esta tela", seção "Laboratório web" no README.

- [ ] **Step 1: Botão de copiar link, aviso e ajuda**

Em `web/static/index.html`, dentro de `<header class="top">`, depois do `<p class="sub">`, acrescente:

```html
  <div class="top-actions">
    <button id="copy-link" type="button" class="chip">Copiar link desta tela</button>
  </div>
  <details class="howto">
    <summary>Como ler esta tela</summary>
    <ul>
      <li><strong>Cenário:</strong> escolha o tamanho da garagem, o limite de potência da rede e o perfil de falhas, e clique em Rodar.</li>
      <li><strong>Comparação:</strong> o planejador contra quatro referências, na média de várias garagens simuladas (sementes). “Violações do plano” deve ser sempre 0.</li>
      <li><strong>Execução:</strong> um dia de uma garagem simulada, minuto a minuto. Arraste o cursor para ver a potência contra o limite, a carga de cada ônibus, os carregadores e a decisão tomada. Clique num ônibus para saber por que ele saiu pronto ou não.</li>
      <li>Os dados são sintéticos: mostram o comportamento do algoritmo, não o de uma garagem real. A premissa de que os operadores executam os rodízios recomendados precisa ser validada com operadores reais.</li>
    </ul>
  </details>
```

e, antes de `<script type="module" ...>`, acrescente `<div id="toast" class="toast" role="status" aria-live="polite"></div>`.

Em `web/static/js/main.js`, acrescente (por exemplo depois de `banner`):

```js
let toastTimer = null;
function toast(message) {
  const t = $('toast');
  t.textContent = message;
  t.classList.add('show');
  clearTimeout(toastTimer);
  toastTimer = setTimeout(() => t.classList.remove('show'), 2500);
}

async function copyLink() {
  syncHash();
  const url = location.href;
  try {
    await navigator.clipboard.writeText(url);
    toast('Link copiado.');
  } catch {
    window.prompt('Copie o link:', url); // clipboard blocked: let the user copy it by hand
  }
}
```

e, em `init()`, junto dos outros `addEventListener`: `$('copy-link').addEventListener('click', copyLink);`.

Acrescente ao fim de `web/static/app.css`:

```css
.top-actions { margin: 8px 0; }
.howto { margin: 8px 0; color: var(--muted); } .howto summary { cursor: pointer; } .howto ul { margin: 6px 0 0; padding-left: 20px; } .howto li { margin: 3px 0; }
.toast { position: fixed; left: 50%; bottom: 20px; transform: translateX(-50%); background: var(--fg); color: var(--bg); padding: 8px 16px; border-radius: 6px; opacity: 0; pointer-events: none; transition: opacity .2s; }
.toast.show { opacity: 1; }
```

- [ ] **Step 2: Seção no README**

No `README.md`, depois da seção que explica o `simrun`, acrescente:

````markdown
## Laboratório web

Uma interface local para configurar cenários, comparar o planejador com as referências e abrir uma execução minuto a minuto (potência contra o limite, carga de cada ônibus, carregadores, decisão tomada e o motivo).

```bash
go run ./cmd/lab
```

Abra `http://127.0.0.1:8080`. Não precisa de Node: a interface vai dentro do programa. O servidor só escuta em endereço local (não tem login) e só aceita requisições para `127.0.0.1`, `localhost` ou `::1`.

- **Cenário:** ônibus, carregadores, limite (kW), perfil de falhas, sementes, operadores seguem rodízios, idade da leitura e, em "Avançado", a regra anti-vaivém do rodízio. Há cenários prontos.
- **Comparação:** a mesma tabela do `simrun` (os números são idênticos para os mesmos parâmetros) e um ponto por semente; clicar num ponto abre aquela execução.
- **Execução:** gráfico de potência, linha do tempo de ônibus e de carregadores e painel de decisão, com um cursor de tempo. Clicar num ônibus explica por que ele saiu pronto ou não. "Copiar link desta tela" gera um endereço que reproduz a execução (o simulador é determinístico: nada é gravado em disco).

Limites para a tela não travar: ônibus × sementes até 20000 na comparação e até 1000 ônibus numa execução detalhada.

API (JSON): `GET /api/defaults`, `POST /api/compare`, `POST /api/run`; o formato está em `docs/superpowers/specs/2026-10-05-laboratorio-web-design.md`. Testes da interface: `node --test web/test/` (só desenvolvimento).

Os dados são sintéticos: mostram o comportamento do algoritmo, não o de uma garagem real.
````

- [ ] **Step 3: Verificação final contra os critérios de sucesso da spec**

1. **Roda sem Node e sem rede externa.** Num diretório limpo: `git clone . /tmp/lab-check && cd /tmp/lab-check && go run ./cmd/lab` (sem `node` no `PATH`, por exemplo `PATH=$(dirname $(which go)):/usr/bin:/bin`), abra `http://127.0.0.1:8080` e confirme que a tela carrega e que o painel de rede do navegador mostra só requisições a `127.0.0.1` (nenhuma fonte, CDN ou script externo).
2. **A tabela do laboratório coincide com a do `simrun`.** Compare, para os mesmos parâmetros:

```bash
go run ./cmd/simrun -buses 10 -chargers 5 -limit 600 -profile mild -seeds 5 | awk 'NR>2 {print $1, $2, $3, $4, $5}' > /tmp/simrun.txt
curl -s -XPOST -H 'Content-Type: application/json' -d '{"buses":10,"chargers":5,"limit_kw":600,"profile":"mild","seeds":5}' localhost:8080/api/compare \
 | python3 -c "import sys,json; d=json.load(sys.stdin); [print(c['name'], '%.1f'%c['aggregate']['ready_pct'], '%.1f'%c['aggregate']['shortfall_kwh'], '%.0f'%c['aggregate']['peak_kw'], c['aggregate']['plan_violations']) for c in d['controllers']]" > /tmp/lab.txt
diff /tmp/simrun.txt /tmp/lab.txt && echo IGUAIS
```

Expected: `IGUAIS`. (A primeira linha do `simrun` é o cabeçalho de parâmetros e a segunda, o de colunas; por isso `NR>2`.)
3. **Causa visível de qualquer ônibus que não saiu pronto.** Em "Falhas severas", abra uma execução do `planner`, escolha um ônibus com ✗ e confirme que o painel explica o déficit, o consumo real contra o previsto, o tempo esperando carregador/em falha/sem leitura e a possível causa, com o gráfico de carga real contra a lida.
4. **Reprodução pelo link.** Clique em "Copiar link desta tela" numa execução, cole numa aba nova e confirme a mesma execução (mesmo resumo e mesma decisão no mesmo minuto). Repita com a aba Comparação.
5. **Nenhuma violação.** Rode 20 sementes de cada perfil (nenhuma, leve, severa, aleatória) a 600 kW e a 2000 kW pela tela: a coluna "violações do plano" é 0 em todos os controladores que passam pelo planejador (e o resumo da execução nunca mostra `⚠`).
6. **Erros e limites.** Ônibus `0`, sementes `1001`, limite `-5`, campo vazio, 600 ônibus × 40 sementes: mensagens em português no campo certo, sem travar. Com o servidor parado, "Rodar" mostra "Não consegui falar com o servidor…".
7. **Segurança.** `curl -s -o /dev/null -w '%{http_code}\n' -H 'Host: evil.example.com' localhost:8080/api/defaults` → `403`; `go run ./cmd/lab -addr 0.0.0.0:8080` → recusa com exit 2.

- [ ] **Step 4: Status da spec e commit**

Em `docs/superpowers/specs/2026-10-05-laboratorio-web-design.md`, troque a linha de status por `Status: aprovado e implementado (plano em docs/superpowers/plans/2026-10-05-laboratorio-web.md)`.

```bash
gofmt -l . ; go vet ./... && go test -race ./... && node --test web/test/
git add web README.md docs/superpowers/specs/2026-10-05-laboratorio-web-design.md
git commit -m "feat(lab): copy-link button, reading help and README section

Co-Authored-By: Claude Sonnet 5.5 <noreply@anthropic.com>"
git push
```

---

### Task 8: Animações — reprodução do dia, feed de eventos e transições

Acrescentada em 2026-10-05 a pedido do autor ("algo com animações bem legais"). Vem depois das telas das tarefas 4 a 6 e só adiciona movimento por cima delas; nada nas tarefas 1 a 3 muda. Detalhe do desenho na seção 5b da spec.

**Files:**
- Create: `web/static/js/motion.js`, `web/static/js/events.js`, `web/static/js/feed.js`; testes `web/test/motion.test.mjs`, `web/test/events.test.mjs`
- Modify: `web/static/index.html`, `web/static/app.css`, `web/static/js/main.js`, `web/static/js/compare.js`, `web/static/js/run.js`, `web/static/js/decisionPanel.js`, `web/static/js/charts/power.js`, `web/static/js/charts/gantt.js`

**Interfaces:**
- Consumes: `createCursor` (`cursor.value`, `cursor.max`, `cursor.set`, `cursor.onChange`), os dados de `/api/run` (`series.layer`, `series.buses[].{state,charger}`, `series.chargers[].physical_kw`, `decisions`, `outcomes`, `scenario.faults`), `POWER_FAULTS`, `describeFault`, `LAYER_LABEL`, `fmtPct`, `clock`.
- Produces:
  - `motion.js`: `easeOutCubic(t)`, `lerp(a, b, t)`, `animationsEnabled()`, `setAnimations(on)`, `applyMotionClass()`, `countUp(el, to, fmt, ms, from)`, `throttle(fn, ms)`, `createPlayer(cursor, deps)` → `{ play, pause, toggle, restart, setSpeed(v), setLoop(v), onChange(fn) → unsubscribe, state }` onde `state = { playing, speed, loop }` (`speed` = minutos simulados por segundo real).
  - `events.js`: `eventsBetween(data, from, to, maxSpan = 30)` → `[{minute, type: 'depart'|'swap'|'layer'|'fault', ok?, bus?, text}]` com `from < minute <= to`, ordenados por minuto; devolve `[]` quando `to - from > maxSpan` (salto do cursor, não reprodução).
  - `feed.js`: `createFeed(root)` → `{ push(events), clear() }`.
  - `charts/power.js` e `charts/gantt.js`: `renderPowerChart`, `renderBusTimeline` e `renderChargerTimeline` passam a aceitar um último parâmetro opcional `player` (o de `createPlayer`) e animam conforme o cursor e o estado de reprodução; sem `player` continuam funcionando como antes.

- [ ] **Step 1: Escrever os testes (Node) que falham**

`web/test/motion.test.mjs`:

```js
import test from 'node:test';
import assert from 'node:assert/strict';
import { easeOutCubic, lerp, createPlayer } from '../static/js/motion.js';
import { createCursor } from '../static/js/cursor.js';

test('easing and interpolation', () => {
  assert.equal(easeOutCubic(0), 0);
  assert.equal(easeOutCubic(1), 1);
  assert.ok(easeOutCubic(0.5) > 0.5);
  assert.equal(lerp(10, 20, 0.5), 15);
});

// a controllable clock and frame scheduler
function fakeClock() {
  let t = 0;
  let next = 1;
  const pending = new Map();
  return {
    deps: { now: () => t, raf: (f) => { const id = next++; pending.set(id, f); return id; }, caf: (id) => pending.delete(id) },
    advance(ms) {
      t += ms;
      const frames = [...pending.values()];
      pending.clear();
      frames.forEach((f) => f());
    },
    pendingFrames: () => pending.size,
  };
}

test('playing advances the cursor by speed minutes per second', () => {
  const clock = fakeClock();
  const cursor = createCursor(1000);
  const p = createPlayer(cursor, clock.deps);
  p.setSpeed(60);
  p.play();
  clock.advance(1000);
  assert.equal(cursor.value, 60);
  clock.advance(500);
  assert.equal(cursor.value, 90);
  assert.equal(p.state.playing, true);
});

test('fractions of a minute accumulate instead of being lost', () => {
  const clock = fakeClock();
  const cursor = createCursor(1000);
  const p = createPlayer(cursor, clock.deps);
  p.setSpeed(30);
  p.play();
  for (let i = 0; i < 10; i++) clock.advance(100); // 1 s in ten frames
  assert.equal(cursor.value, 30);
});

test('pause stops the cursor and play resumes from there', () => {
  const clock = fakeClock();
  const cursor = createCursor(1000);
  const p = createPlayer(cursor, clock.deps);
  p.setSpeed(60);
  p.play();
  clock.advance(1000);
  p.pause();
  clock.advance(5000);
  assert.equal(cursor.value, 60);
  assert.equal(clock.pendingFrames(), 0);
  p.play();
  clock.advance(1000);
  assert.equal(cursor.value, 120);
});

test('reaching the end pauses, and play at the end restarts from zero', () => {
  const clock = fakeClock();
  const cursor = createCursor(100);
  const p = createPlayer(cursor, clock.deps);
  p.setSpeed(600);
  p.play();
  clock.advance(1000);
  assert.equal(cursor.value, 100);
  assert.equal(p.state.playing, false);
  p.play();
  assert.equal(cursor.value, 0);
  assert.equal(p.state.playing, true);
});

test('loop wraps around instead of stopping', () => {
  const clock = fakeClock();
  const cursor = createCursor(100);
  const p = createPlayer(cursor, clock.deps);
  p.setSpeed(60);
  p.setLoop(true);
  p.play();
  clock.advance(2000); // 120 minutes of a 100-minute day
  assert.equal(p.state.playing, true);
  assert.ok(cursor.value < 100);
});

test('invalid speeds are ignored and listeners are told about changes', () => {
  const clock = fakeClock();
  const cursor = createCursor(100);
  const p = createPlayer(cursor, clock.deps);
  const seen = [];
  const off = p.onChange((s) => seen.push({ ...s }));
  p.setSpeed(0);
  p.setSpeed(NaN);
  p.setSpeed(-5);
  assert.equal(p.state.speed, 60);
  p.setSpeed(120);
  p.play();
  p.toggle();
  off();
  p.toggle();
  assert.deepEqual(seen.map((s) => [s.playing, s.speed]), [[false, 120], [true, 120], [false, 120]]);
});

test('restart goes back to minute 0 and plays', () => {
  const clock = fakeClock();
  const cursor = createCursor(100);
  cursor.set(50);
  const p = createPlayer(cursor, clock.deps);
  p.restart();
  assert.equal(cursor.value, 0);
  assert.equal(p.state.playing, true);
});
```

`web/test/events.test.mjs`:

```js
import test from 'node:test';
import assert from 'node:assert/strict';
import { eventsBetween } from '../static/js/events.js';

const data = {
  scenario: {
    start_clock_min: 1080,
    faults: [
      { kind: 'limit_drop', target: '', from: 20, to: 40, value: 0.5 },
      { kind: 'soc_noise', target: '*', from: 22, to: 30, value: 3 },
      { kind: 'charger_fail', target: 'C2', from: 50, to: 60, value: 0 },
    ],
  },
  series: { layer: ['normal', 'normal', 'normal', 'normal', 'normal', 'normal', 'last-valid', 'last-valid', 'normal', 'normal'] },
  decisions: [
    { minute: 0, swaps: [] },
    { minute: 4, swaps: [{ charger: 'C1', out: 'B1', in: 'B2', reason: 'troca' }] },
  ],
  outcomes: [
    { id: 'B1', departed: true, ready: true, departure: 7 },
    { id: 'B2', departed: true, ready: false, departure: 8 },
    { id: 'B3', departed: false, ready: false, departure: 9 },
  ],
};

test('events in (from, to] are returned in minute order', () => {
  const ev = eventsBetween(data, 0, 9);
  assert.deepEqual(ev.map((e) => [e.minute, e.type]), [[4, 'swap'], [6, 'layer'], [7, 'depart'], [8, 'layer'], [8, 'depart']]);
});

test('departures say whether the bus was ready; buses that never left do not depart', () => {
  const dep = eventsBetween(data, 0, 9).filter((e) => e.type === 'depart');
  assert.deepEqual(dep.map((e) => [e.bus, e.ok]), [['B1', true], ['B2', false]]);
});

test('layer events name the layer and only transitions count', () => {
  const layers = eventsBetween(data, 0, 9).filter((e) => e.type === 'layer');
  assert.equal(layers.length, 2);
  assert.match(layers[0].text, /último plano válido/);
  assert.match(layers[1].text, /normal/);
});

test('only power-related faults are events', () => {
  const ev = eventsBetween(data, 15, 25);
  assert.deepEqual(ev.map((e) => e.type), ['fault']);
  assert.match(ev[0].text, /queda do limite da rede/);
});

test('the window is open at the start and closed at the end', () => {
  assert.equal(eventsBetween(data, 4, 5).length, 0);
  assert.equal(eventsBetween(data, 3, 4).length, 1);
});

test('a jump of the cursor produces no events', () => {
  assert.deepEqual(eventsBetween(data, 0, 100), []);
  assert.deepEqual(eventsBetween(data, 0, 100, 200).length > 0, true);
  assert.deepEqual(eventsBetween(data, 9, 3), []);
});
```

- [ ] **Step 2: Rodar e ver falhar**

Run: `node --test web/test/`
Expected: FAIL (`motion.js` e `events.js` inexistentes).

- [ ] **Step 3: `motion.js`**

```js
export const easeOutCubic = (t) => 1 - (1 - t) ** 3;
export const lerp = (a, b, t) => a + (b - a) * t;

const KEY = 'lab-animations';

// animationsEnabled: the user's switch ('on'/'off') wins; otherwise follow the
// system's "reduce motion" preference.
export function animationsEnabled() {
  let stored = null;
  try { stored = localStorage.getItem(KEY); } catch { /* storage blocked */ }
  if (stored === 'on') return true;
  if (stored === 'off') return false;
  return !(typeof matchMedia === 'function' && matchMedia('(prefers-reduced-motion: reduce)').matches);
}

export function applyMotionClass() {
  document.documentElement.classList.toggle('no-motion', !animationsEnabled());
}

export function setAnimations(on) {
  try { localStorage.setItem(KEY, on ? 'on' : 'off'); } catch { /* storage blocked */ }
  applyMotionClass();
}

// countUp animates the text of el from `from` to `to` (instantly when animations are off).
export function countUp(el, to, fmt, ms = 700, from = 0) {
  if (!animationsEnabled() || !Number.isFinite(to)) { el.textContent = fmt(to); return; }
  const t0 = performance.now();
  const step = (now) => {
    const t = Math.min(1, (now - t0) / ms);
    el.textContent = fmt(lerp(from, to, easeOutCubic(t)));
    if (t < 1) requestAnimationFrame(step);
  };
  requestAnimationFrame(step);
}

// throttle runs fn at most once per `ms`, with a trailing call so the last value is never lost.
export function throttle(fn, ms) {
  let last = -Infinity;
  let timer = null;
  let pendingArgs = null;
  return (...args) => {
    const now = Date.now();
    if (now - last >= ms) { last = now; fn(...args); return; }
    pendingArgs = args;
    if (timer === null) {
      timer = setTimeout(() => { timer = null; last = Date.now(); fn(...pendingArgs); }, ms - (now - last));
    }
  };
}

// createPlayer plays the day: it moves the cursor `speed` simulated minutes per real
// second. deps (now, raf, caf) can be replaced to test it without a browser.
export function createPlayer(cursor, deps = {}) {
  const now = deps.now || (() => performance.now());
  const raf = deps.raf || ((f) => requestAnimationFrame(f));
  const caf = deps.caf || ((id) => cancelAnimationFrame(id));
  const state = { playing: false, speed: 60, loop: false };
  const subs = new Set();
  let frameId = null;
  let last = 0;
  let acc = 0;
  const emit = () => subs.forEach((fn) => fn({ ...state }));

  function frame() {
    if (!state.playing) return;
    const t = now();
    acc += ((t - last) / 1000) * state.speed;
    last = t;
    const whole = Math.floor(acc);
    if (whole > 0) {
      acc -= whole;
      const next = cursor.value + whole;
      if (next >= cursor.max) {
        if (state.loop) cursor.set(next % (cursor.max + 1));
        else { cursor.set(cursor.max); pause(); return; }
      } else cursor.set(next);
    }
    frameId = raf(frame);
  }

  function play() {
    if (state.playing) return;
    if (cursor.value >= cursor.max) cursor.set(0);
    state.playing = true;
    last = now();
    acc = 0;
    emit();
    frameId = raf(frame);
  }
  function pause() {
    if (!state.playing) return;
    state.playing = false;
    if (frameId !== null) caf(frameId);
    frameId = null;
    emit();
  }
  return {
    play, pause,
    toggle() { state.playing ? pause() : play(); },
    restart() { pause(); cursor.set(0); play(); },
    setSpeed(v) { if (Number.isFinite(v) && v > 0) { state.speed = v; emit(); } },
    setLoop(v) { state.loop = Boolean(v); emit(); },
    onChange(fn) { subs.add(fn); return () => subs.delete(fn); },
    get state() { return { ...state }; },
  };
}
```

`web/static/js/events.js`:

```js
import { POWER_FAULTS, LAYER_LABEL, describeFault } from './glossary.js';

// Order of events inside one minute.
const RANK = { fault: 0, layer: 1, swap: 2, depart: 3 };

// eventsBetween lists what happened in the minutes (from, to]: departures, recommended
// swaps, layer changes and power faults. A jump longer than maxSpan is a scrub, not a
// playback, and yields nothing.
export function eventsBetween(data, from, to, maxSpan = 30) {
  if (!(to > from) || to - from > maxSpan) return [];
  const inRange = (m) => m > from && m <= to;
  const out = [];
  for (const o of data.outcomes) {
    if (o.departed && inRange(o.departure)) {
      out.push({ minute: o.departure, type: 'depart', ok: o.ready, bus: o.id,
        text: o.ready ? `${o.id} saiu pronto` : `${o.id} saiu sem a carga` });
    }
  }
  for (const d of data.decisions) {
    if (!inRange(d.minute)) continue;
    for (const sw of d.swaps) {
      out.push({ minute: d.minute, type: 'swap', bus: sw.in, text: `rodízio recomendado: ${sw.in} assume ${sw.charger} no lugar de ${sw.out}` });
    }
  }
  const layers = data.series.layer;
  for (let i = Math.max(1, from + 1); i <= to && i < layers.length; i++) {
    if (layers[i] !== layers[i - 1]) {
      out.push({ minute: i, type: 'layer', layer: layers[i], text: `o planejador passou para: ${LAYER_LABEL[layers[i]] || layers[i]}` });
    }
  }
  for (const f of data.scenario.faults) {
    if (POWER_FAULTS.has(f.kind) && inRange(f.from)) out.push({ minute: f.from, type: 'fault', text: describeFault(f) });
  }
  return out.sort((a, b) => a.minute - b.minute || RANK[a.type] - RANK[b.type]);
}
```

`web/static/js/feed.js`:

```js
import { h } from './dom.js';

const ICON = { depart_ok: '●', depart_fail: '✗', swap: '⇄', layer: '⚙', fault: '⚠' };

// createFeed is the list of the latest events (newest first); items slide in via CSS.
export function createFeed(root, max = 6) {
  const list = h('ul', { class: 'feed', 'aria-live': 'off' });
  root.replaceChildren(h('h3', {}, 'Acontecimentos'), list);
  return {
    push(events) {
      for (const e of events) {
        const kind = e.type === 'depart' ? (e.ok ? 'depart_ok' : 'depart_fail') : e.type;
        list.prepend(h('li', { class: `feed-item feed-${kind}` }, h('span', { class: 'feed-icon', 'aria-hidden': 'true' }, ICON[kind]), e.text));
      }
      while (list.children.length > max) list.lastElementChild.remove();
    },
    clear() { list.replaceChildren(); },
  };
}
```

- [ ] **Step 4: Rodar e ver passar**

Run: `node --test web/test/`
Expected: PASS (inclui todos os testes das tarefas anteriores).

- [ ] **Step 5: Transições e chegada da Comparação (`main.js`, `compare.js`, `app.css`, `index.html`)**

1. `index.html`: dentro de `.top-actions` (Tarefa 7), ao lado do botão de copiar link, acrescente:

```html
    <button id="motion-toggle" type="button" class="chip" aria-pressed="true">Animações: ligadas</button>
```

2. `main.js`: importe `{ applyMotionClass, setAnimations, animationsEnabled }` de `./motion.js`; em `init()`, antes de criar o formulário, chame `applyMotionClass();` e ligue o interruptor:

```js
  const motionBtn = $('motion-toggle');
  const paintMotion = () => {
    const on = animationsEnabled();
    motionBtn.setAttribute('aria-pressed', String(on));
    motionBtn.textContent = `Animações: ${on ? 'ligadas' : 'desligadas'}`;
  };
  motionBtn.addEventListener('click', () => { setAnimations(!animationsEnabled()); paintMotion(); });
  paintMotion();
```

3. `compare.js`: importe `{ countUp }` de `./motion.js`. Na linha de cada controlador (`table`), dê a cada `<tr>` o atraso de entrada: acrescente aos atributos do `h('tr', ...)` `style: `--i:${index}`` e a classe `reveal` (use `data.controllers.map((c, index) => ...)`). Na célula de `ready_pct`, em vez do texto pronto, crie `const num = h('span', {}, text)` e chame `countUp(num, c.aggregate.ready_pct, fmtPct)` depois de a tabela entrar no DOM (por exemplo `queueMicrotask(() => countUp(...))`); importe `fmtPct` de `./format.js`. Em `seedsPanel`, dê a cada ponto `style: `--i:${i}`` (índice dentro do controlador) e a classe `dot pop`, e à barra de média a classe `bar grow`.

4. `app.css` (acrescentar):

```css
@keyframes rise { from { opacity: 0; transform: translateY(8px); } to { opacity: 1; transform: none; } }
@keyframes pop { 0% { transform: scale(0); } 70% { transform: scale(1.25); } 100% { transform: scale(1); } }
@keyframes pulse-stroke { 0%, 100% { stroke-width: .8; } 50% { stroke-width: 2.4; } }
@keyframes flow { to { stroke-dashoffset: -12; } }
@keyframes flicker { 0%, 100% { opacity: 1; } 40% { opacity: .35; } 60% { opacity: .9; } 80% { opacity: .5; } }
@keyframes flash { 0% { box-shadow: 0 0 0 0 color-mix(in srgb, var(--limit) 60%, transparent); } 100% { box-shadow: 0 0 0 14px transparent; } }
section[role=tabpanel]:not([hidden]) { animation: rise .25s ease-out; }
.reveal { animation: rise .4s ease-out both; animation-delay: calc(var(--i, 0) * 70ms); }
.dot.pop { transform-box: fill-box; transform-origin: center; animation: pop .35s ease-out both; animation-delay: calc(var(--i, 0) * 25ms + 250ms); }
.strip .bar.grow { transform-box: fill-box; transform-origin: left center; animation: grow .7s cubic-bezier(.2,.8,.2,1) both; }
@keyframes grow { from { transform: scaleX(0); } to { transform: scaleX(1); } }
.no-motion *, .no-motion *::before, .no-motion *::after { animation: none !important; transition: none !important; }
@media (prefers-reduced-motion: reduce) { :root:not(.motion-on) * { animation: none !important; transition: none !important; } }
```

(A última regra cobre quem tem "reduzir movimento" no sistema sem ter usado o interruptor; `applyMotionClass()` já põe `no-motion` nesse caso, então as duas regras concordam.)

- [ ] **Step 6: Reprodução na aba Execução (`run.js`, `decisionPanel.js`)**

Em `run.js`:
1. Importe `{ createPlayer, countUp, animationsEnabled, throttle }` de `./motion.js`, `{ eventsBetween }` de `./events.js` e `{ createFeed }` de `./feed.js`.
2. Mantenha `let player = null;` no escopo de `createRunView`; no início de `load` e antes de cada `render`, chame `player?.pause();`.
3. Crie a linha de reprodução (acima do controle de tempo):

```js
  function playerRow(player) {
    const play = h('button', { type: 'button', class: 'primary play', 'aria-label': 'Reproduzir' }, '▶ Reproduzir');
    const restart = h('button', { type: 'button', class: 'chip', 'aria-label': 'Reiniciar do começo' }, '⏮ Do começo');
    const speed = h('select', { 'aria-label': 'Velocidade da reprodução' },
      [[30, '30 min/s'], [60, '1 h/s'], [120, '2 h/s'], [300, '5 h/s'], [600, '10 h/s']].map(([v, l]) => h('option', { value: v, selected: v === 60 }, l)));
    const loop = h('input', { type: 'checkbox', id: 'loop' });
    play.addEventListener('click', () => player.toggle());
    restart.addEventListener('click', () => player.restart());
    speed.addEventListener('change', () => player.setSpeed(Number(speed.value)));
    loop.addEventListener('change', () => player.setLoop(loop.checked));
    player.onChange((s) => {
      play.textContent = s.playing ? '⏸ Pausar' : '▶ Reproduzir';
      play.setAttribute('aria-label', s.playing ? 'Pausar' : 'Reproduzir');
      root.classList.toggle('playing', s.playing);
    });
    return h('div', { class: 'player-row' }, play, restart, h('label', {}, 'Velocidade ', speed), h('label', {}, loop, ' repetir'));
  }
```

4. Em `render(data)`, depois de criar `cursor` e `selection`: `player = createPlayer(cursor);`, acrescente `playerRow(player)` antes de `cursorRow(...)`, crie `const feedPanel = h('div', { class: 'panel', id: 'panel-feed' });` (acrescentado depois de `powerPanel`) e `const feed = createFeed(feedPanel);`; passe `player` como último argumento de `renderPowerChart`, `renderBusTimeline` e `renderChargerTimeline`. Alimente o feed durante a reprodução:

```js
    let lastMinute = 0;
    cursor.onChange((m) => {
      if (player.state.playing) feed.push(eventsBetween(data, lastMinute, m));
      lastMinute = m;
    });
    player.onChange((s) => { if (s.playing && cursor.value === 0) feed.clear(); });
```

(Se o cursor foi movido à mão, `lastMinute` apenas acompanha; `eventsBetween` já ignora saltos grandes.)

5. A tecla Espaço reproduz/pausa quando o foco não está num campo:

```js
    root.addEventListener('keydown', (e) => {
      if (e.code === 'Space' && !['INPUT', 'SELECT', 'BUTTON', 'TEXTAREA'].includes(e.target.tagName)) { e.preventDefault(); player.toggle(); }
    });
```

6. O resumo anima o percentual de prontos: em `summaryLine`, troque o texto corrido por nós: crie `const pct = h('span', { class: 'num' }, fmtPct(m.ready_pct)); countUp(pct, m.ready_pct, fmtPct, 800);` e monte o `<p class="summary">` com `h('p', {class:'summary'}, `${m.ready} de ${m.buses} ônibus saíram prontos (`, pct, `) · ...resto do texto`)` (ajuste `summaryLine` para devolver um nó em vez de string).

Em `decisionPanel.js`: importe `{ throttle }` de `./motion.js` e troque `cursor.onChange(update);` por `cursor.onChange(throttle(update, 100));` (a tabela de 50 linhas não precisa ser refeita 60 vezes por segundo durante a reprodução; o último minuto nunca se perde por causa do disparo final do `throttle`).

- [ ] **Step 7: Movimento nos gráficos (`charts/power.js`, `charts/gantt.js`)**

`charts/power.js` — o passado fica nítido e o futuro esmaecido enquanto o dia toca:
1. Assinatura: `export function renderPowerChart(root, data, cursor, player)`.
2. Troque o bloco "stacked areas ... linhas comandado/limite" por duas camadas com o mesmo conteúdo, a do futuro esmaecida e a do passado recortada até o cursor:

```js
  const clipRect = s('rect', { x: 0, y: 0, width: W, height: totalH });
  parts.push(s('clipPath', { id: 'clip-power-past' }, clipRect));
  const drawLayer = (extra) => {
    const g = s('g', extra);
    stacked.forEach((layer, i) => {
      g.append(s('path', { d: areaPath(layer.lower, layer.upper, x, y), class: i % 2 ? 'area area-b' : 'area area-a' },
        s('title', {}, `${series.chargers[i].id}: potência física`)));
    });
    g.append(s('path', { d: linePath(series.commanded_kw, x, y, false), class: 'line-commanded' }),
      s('path', { d: linePath(series.limit_kw, x, y, true), class: 'line-limit' }));
    return g;
  };
  const dim = drawLayer({ class: 'future', opacity: 0.28 });
  const past = drawLayer({ 'clip-path': 'url(#clip-power-past)' });
  dim.setAttribute('display', 'none'); // shown only while the day plays
  parts.push(dim, past);
```

3. Em `update(m)`, acrescente (e chame também quando o `player` mudar):

```js
    const playing = player && player.state.playing;
    clipRect.setAttribute('width', playing ? x(m) : W);
    dim.setAttribute('display', playing ? 'inline' : 'none');
```

e `player?.onChange(() => update(cursor.value));`.

`charts/gantt.js` (parâmetro `player` opcional em `renderBusTimeline` e `renderChargerTimeline`):
- **Ônibus carregando brilham:** guarde as barras (`barNodes = new Map()` com `bar` por ônibus ao criar o `rect.bus-bar`) e, em cada mudança do cursor, alterne a classe `charging` em quem está ligado e recebendo potência (`b.state[m] === 1 && b.charger[m] >= 0 && data.series.chargers[b.charger[m]].physical_kw[m] > 0`).
- **Preenchimento que se revela:** dê ao `path.soc-fill` o atributo `clip-path="url(#clip-bus-past)"`, crie `const busClip = s('rect', {x:0, y:0, width:W, height:bottom+26})` dentro de um `<clipPath id="clip-bus-past">` e, em cada mudança do cursor ou da reprodução, ajuste `busClip.setAttribute('width', player && player.state.playing ? x(m) : W)`.
- **Marcas que estouram:** nas marcas de saída (`mark-ready`/`mark-fail`) e nos triângulos de rodízio, guarde `{node, minute}`; quando `player` está tocando e `Math.abs(m - minute) <= 1` (saída) ou `<= 3` (rodízio), adicione a classe `hit` (CSS abaixo) e remova-a quando o cursor se afastar.
- **Carregadores com fluxo:** em `renderChargerTimeline`, para cada trecho ocupado crie, além do `rect.occ`, uma `line.flow-line` ao longo da base do trecho (`x1=x(r.from)`, `x2=x(r.to+1)`, `y1=y2=y0+ROW-3`) escondida; em cada mudança do cursor mostre (`classList.add('on')`) só a do trecho que contém `m` com `physical_kw[m] > 0`. Trechos de carregadores em falha no minuto `m` recebem a classe `flicker`.

`app.css` (acrescentar):

```css
.bus-bar.charging { stroke: var(--ok); animation: pulse-stroke 1.2s ease-in-out infinite; }
.hit { transform-box: fill-box; transform-origin: center; animation: pop .45s ease-out; }
.flow-line { display: none; stroke: var(--limit); stroke-width: 2; stroke-linecap: round; stroke-dasharray: 6 6; }
.flow-line.on { display: inline; animation: flow .6s linear infinite; }
.status-fail.flicker { animation: flicker 1s linear infinite; }
.player-row { display: flex; flex-wrap: wrap; gap: 8px 14px; align-items: center; margin: 8px 0; }
.player-row select { padding: 5px 8px; border: 1px solid var(--border); border-radius: 6px; background: var(--bg); color: var(--fg); font: inherit; }
.player-row label { color: var(--muted); font-size: .9rem; }
.feed { list-style: none; margin: 0; padding: 0; min-height: 3.2rem; }
.feed-item { display: flex; gap: 8px; padding: 4px 8px; border-left: 3px solid var(--border); margin: 3px 0; animation: rise .3s ease-out; }
.feed-icon { width: 1.2em; text-align: center; }
.feed-depart_ok { border-left-color: var(--ok); } .feed-depart_fail, .feed-fault { border-left-color: var(--bad); } .feed-swap { border-left-color: var(--amber); } .feed-layer { border-left-color: var(--purple); }
.playing .cursor-row { animation: flash 1.6s ease-out infinite; border-radius: 6px; }
```

- [ ] **Step 8: Verificar no navegador de verdade**

Run: `go run ./cmd/lab`, abra `http://127.0.0.1:8080` e verifique (com capturas ou, se não houver ferramenta de navegador, registre "verificação visual pendente"):
1. Ao trocar de aba, o painel sobe suavemente; na Comparação as linhas da tabela entram em sequência, o percentual de prontos "conta" até o valor, a barra de média cresce da esquerda e os pontos das sementes aparecem em cascata.
2. Em Execução, "Reproduzir" faz o dia rodar: o cursor anda, o gráfico de potência mostra o passado nítido e o futuro esmaecido, os ônibus que estão carregando pulsam em verde, os trechos dos carregadores com ônibus ligado mostram um fluxo tracejado em movimento, as marcas de saída e de rodízio estouram quando o cursor passa e o feed "Acontecimentos" recebe, em tempo real, saídas (● / ✗), rodízios, mudanças de camada e quedas do limite.
3. Pausar, trocar a velocidade (30 min/s a 10 h/s), "repetir", "Do começo" e a tecla Espaço funcionam; ao chegar ao fim a reprodução para (ou volta ao início com "repetir"). Pausada, o gráfico volta a mostrar o dia inteiro. A reprodução a 10 h/s continua fluida (sem travar) com 50 ônibus.
4. "Animações: desligadas" (e a preferência do sistema "reduzir movimento") deixa tudo estático e instantâneo, sem perder nenhuma informação.
5. Tema escuro e mobile (largura de 375 px) continuam legíveis; nenhum erro no console.

- [ ] **Step 9: README e commit**

No `README.md`, na lista da seção "Laboratório web", acrescente: `- **Animações:** "Reproduzir" toca o dia (30 min/s a 10 h/s) com o passado nítido e o futuro esmaecido, ônibus carregando pulsando, fluxo nos carregadores ocupados e um feed de acontecimentos (saídas, rodízios, mudanças de camada, quedas do limite). O interruptor "Animações" e a preferência de sistema "reduzir movimento" desligam todo o movimento.`

```bash
gofmt -l . ; go vet ./... && go test -race -short ./... && node --test web/test/
git add web README.md
git commit -m "feat(lab): day playback, event feed and motion across the interface

Co-Authored-By: Claude Sonnet 5.5 <noreply@anthropic.com>"
git push
```
