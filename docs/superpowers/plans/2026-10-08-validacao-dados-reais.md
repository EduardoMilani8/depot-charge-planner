# Validação com dados reais — plano de implementação

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Ler planilhas CSV de uma garagem real (kit em `docs/dados-reais/`), montar um cenário do simulador por noite, rodar o planejador e as referências sobre ele e mostrar a tabela "real × planejador × referências" no comando `replay` e numa aba "Dados reais" do laboratório.

**Architecture:** Pacote novo `internal/realdata` (só biblioteca padrão): leitor tolerante de CSV → `Dataset` validado → `Night{Scenario, Real}` → `Replay` que usa `sim.ForController`/`sim.RunContext`. `cmd/replay` imprime a tabela. `internal/lab` ganha `POST /api/import`, `POST /api/replay` e estende `POST /api/run` com `files`+`night`; o servidor não guarda nada (o navegador reenvia o texto das planilhas). `web/` ganha a aba "Dados reais". O planejador e o simulador não mudam.

**Tech Stack:** Go 1.22 (biblioteca padrão), JavaScript ES modules sem dependências, testes `go test` e `node --test web/test/*.test.mjs` (o formato de diretório falha no Node 22; use o glob).

**Spec:** `docs/superpowers/specs/2026-10-08-validacao-dados-reais-design.md` (vinculante; este plano a detalha). Kit e modelos: `docs/dados-reais/`.

## Global Constraints

- Biblioteca padrão apenas no Go; JS sem dependências; interface em português; claro/escuro por variáveis CSS; nada de requisições externas; o laboratório continua escutando só em endereço local.
- `internal/sim` e `internal/planner` **não mudam de comportamento**; os números do README não mudam. Só é permitido, em `internal/sim`, acrescentar funções exportadas novas (nada removido ou alterado).
- "Pronto" tem a definição do simulador (`world.go`, `depart`): carga real na saída `>= alvo - 1e-6`, com o alvo limitado à capacidade. Alvo em kWh = `capacidade * soc_exigido_pct / 100` (multiplicar primeiro, depois dividir).
- Datas e horas locais sem fuso: `AAAA-MM-DD HH:MM` (também `DD/MM/AAAA HH:MM`). Separador `,` ou `;` detectado pela primeira linha; com `;` o decimal pode ser vírgula. Cabeçalhos em português, sem acento, minúsculos; coluna desconhecida: ignorada com aviso; cabeçalho comparado sem diferenciar maiúsculas e sem espaços nas pontas.
- Noite de um ônibus = data de `(chegada - 12h)`; uma sessão pertence à noite de `(inicio - 12h)`; uma leitura de potência à noite de `(instante - 12h)`.
- Limites: cada arquivo ≤ 8 MB; ≤ 1000 ônibus por noite (`sim.MaxBuses` vale para o total), ≤ 366 noites, ≤ `sim.MaxChargers` carregadores, ≤ 2 000 000 linhas de potência no total. Capacidade 1 a 2000 kWh; percentuais 0 a 100; potência do carregador > 0 e ≤ 5000 kW; `limite_kw` em (0, `sim.MaxLimitKW`].
- Erros de dados: `*realdata.FieldError{File string; Line int; Column, Message string}` com mensagem em português; avisos: `realdata.Warning{File string; Line int; Message string}` (Line 0 = geral). Nenhum erro de planilha pode causar pânico (inclui arquivos vazios, só cabeçalho, linhas curtas, aspas mal fechadas, números `NaN`/`Inf`, UTF-8 inválido).
- Segurança do laboratório: o corpo de `/api/import`, `/api/replay` e `/api/run` com `files` pode ter até 16 MB (as demais rotas continuam em 64 KB); nomes de arquivo aceitos fixos (os cinco do kit); nada é gravado em disco; nenhum caminho de arquivo vem do navegador.
- Cada commit termina com a linha `Co-Authored-By: Claude Sonnet 5.5 <noreply@anthropic.com>` e é enviado ao GitHub (`git push`) logo após o commit. Sem force-push, sem reescrever história.

## Review Focus

1. Planilha de Excel brasileiro (`;`, decimal com vírgula, BOM, Windows-1252, CRLF, linhas em branco no fim): deve ser lida igual à versão `,`/ponto.
2. Noite que atravessa a meia-noite e ônibus com chegada de madrugada (00:00–11:59): cai na noite certa; `saida_prevista` antes da `chegada` é erro com linha.
3. Valores impossíveis nos dados reais (SoC 0 ou 100, saída exigida acima de 100 %, capacidade 0, carregador duplicado, sessão de ônibus inexistente, `fim` antes de `inicio`, potência negativa): erro claro com arquivo/linha/coluna, nunca pânico nem NaN no resultado.
4. Dado incompleto mas aceitável (sem `soc_saida_real_pct` em alguns ônibus, sem `sessoes.csv`, sem `potencia.csv`, sem tarifa): a tela e o relatório mostram "—" e "estimado" nos lugares certos e as contas dos ônibus com dado não os contam como não prontos.
5. Honestidade dos números: pico/custo estimados (sessões espalhadas uniformemente) sempre rotulados; "real" nunca misturado com "estimado" na mesma célula sem selo; o aviso de premissas (leitura de SoC perfeita, limite fixo, rodízios seguidos × não seguidos) sempre presente.

---

## File Structure

- Create `internal/realdata/table.go` (+ `table_test.go`): leitura tolerante de CSV, conversões, `FieldError`, `Warning`.
- Create `internal/realdata/load.go` (+ `load_test.go`): `Dataset`, `Load(files map[string][]byte)`, `LoadDir`.
- Create `internal/realdata/testdata/demo/{garagem,carregadores,onibus,sessoes,potencia}.csv`: conjunto fictício (2 noites).
- Create `internal/realdata/nights.go` (+ `nights_test.go`): `Night`, `Real`, `(*Dataset).Nights()`.
- Create `internal/realdata/replay.go` (+ `replay_test.go`): `Replay`, `Report`.
- Create `cmd/replay/main.go` (+ `main_test.go`).
- Modify `internal/lab/server.go` (limite por rota), create `internal/lab/realdata.go` (+ testes em `internal/lab/realdata_test.go`), modify `internal/lab/handlers.go` (`/api/run` com `files`), `internal/lab/convert.go` (campo `source`).
- Create `web/static/js/realdata.js`, `web/test/realdata.test.mjs`; modify `web/static/index.html`, `web/static/js/main.js`, `web/static/js/api.js`, `web/static/js/run.js`, `web/static/app.css`.
- Modify `README.md`, `docs/dados-reais/README.md`, spec (linha de status).

---

### Task 1: Leitor de CSV tolerante e conversões

**Files:** Create `internal/realdata/table.go`, `internal/realdata/table_test.go`

**Interfaces:**
- Produces (todos em `package realdata`):

```go
type FieldError struct {
	File   string
	Line   int    // 1-based line in the file (header = 1); 0 = whole file
	Column string // header name, "" = whole line
	Message string
}
func (e *FieldError) Error() string // "arquivo.csv, linha 7, coluna soc_chegada_pct: mensagem" (omit missing parts)

type Warning struct{ File string; Line int; Message string }

// Table is a parsed CSV: Header (normalized names) and Rows with their source line numbers.
type Table struct {
	File    string
	Header  []string
	Rows    []Row
	Warnings []Warning
}
type Row struct {
	Line   int
	Fields []string // same length as Header (short rows padded with "", long rows are an error)
}
const maxFileBytes = 8 << 20
func ReadTable(file string, data []byte) (*Table, error) // error is *FieldError
func (t *Table) Col(name string) int                      // index or -1
func (t *Table) Float(r Row, col string) (v float64, present bool, err *FieldError)
func (t *Table) Str(r Row, col string) string             // trimmed
func (t *Table) Time(r Row, col string) (tm time.Time, present bool, err *FieldError)
```

- [ ] **Step 1: Testes que falham** em `table_test.go` (tabela de casos; cada um chama `ReadTable("x.csv", []byte(...))`):
  - `,` e `;` dão a mesma `Table` para o mesmo conteúdo; `;` com `12,5` lê `12.5` em `Float`; com `,` como separador o número `12,5` é erro ("use ponto decimal").
  - BOM UTF-8 no início é removido; CRLF e LF; linhas totalmente em branco são ignoradas (mas a numeração das demais continua correta); espaços nas pontas dos campos e do cabeçalho são removidos; cabeçalho em maiúsculas vira minúsculo.
  - Bytes inválidos em UTF-8 são lidos como Windows-1252 (ex.: `0xE7` → `ç`) e geram `Warning{Message: "arquivo não está em UTF-8; lido como Windows-1252"}`.
  - Arquivo vazio, só com cabeçalho, ou maior que 8 MB → `*FieldError` (vazio: "arquivo vazio"; sem linhas de dados é permitido e devolve `Rows` vazio com aviso "sem linhas de dados").
  - Linha com mais campos que o cabeçalho → erro com a linha; com menos campos → preenchida com vazio.
  - Aspas: `"Ônibus, 1"` é um campo; aspas não fechadas → erro com linha.
  - `Float`: `""` → `present=false`; `abc`, `NaN`, `Inf`, `1e999` → erro com coluna; `1.5`, `-3`, `1e2` ok.
  - `Time`: aceita `2026-03-04 21:10`, `2026-03-04T21:10`, `2026-03-04 21:10:00`, `04/03/2026 21:10`; rejeita `2026-13-40 25:61`; a hora é interpretada em `time.UTC` com a data/hora como escritas (relógio local sem fuso: só diferenças importam).
  - Coluna desconhecida gera `Warning` uma vez por coluna ("coluna 'xyz' ignorada") — verificado pelo chamador via `Table.Warnings` quando ele informar as colunas conhecidas: acrescente `func (t *Table) Known(cols ...string)` que gera esses avisos.
  - Fuzz leve: `FuzzReadTable` com `go test -fuzz` não é obrigatório, mas um teste com 200 entradas aleatórias de bytes (semente fixa) nunca pode entrar em pânico.

- [ ] **Step 2: Rodar e ver falhar:** `go test ./internal/realdata/ -run . 2>&1 | head`  (esperado: erro de compilação, pacote sem código).
- [ ] **Step 3: Implementar** `table.go` com `encoding/csv` (`csv.Reader` com `LazyQuotes=false`, `FieldsPerRecord=-1`, `Comma` escolhido contando `;` e `,` na primeira linha não vazia fora de aspas; empate → `,`) lendo de um `bytes.Reader` já convertido para UTF-8 (BOM removido; se `!utf8.Valid`, decodificar Windows-1252 com tabela própria para 0x80–0x9F e Latin-1 acima, sem dependências). Use `csv.Reader.FieldPos` para obter a linha de cada registro (e lembre que linhas em branco são puladas pelo `csv.Reader`, então a linha vem de `FieldPos`, não de um contador).
- [ ] **Step 4: Rodar:** `go test -race ./internal/realdata/ && gofmt -l .` — tudo verde e `gofmt` vazio.
- [ ] **Step 5: Commit e push**

```bash
git add internal/realdata && git commit -m "feat(realdata): tolerant CSV reader with Portuguese field errors

Co-Authored-By: Claude Sonnet 5.5 <noreply@anthropic.com>" && git push
```

---

### Task 2: `Dataset`, validação e conjunto fictício

**Files:** Create `internal/realdata/load.go`, `internal/realdata/load_test.go`, `internal/realdata/testdata/demo/*.csv`

**Interfaces:**
- Consumes: `ReadTable`, `Table`, `FieldError`, `Warning` (Task 1).
- Produces:

```go
const (
	FileGaragem, FileCarregadores, FileOnibus = "garagem.csv", "carregadores.csv", "onibus.csv"
	FileSessoes, FilePotencia                 = "sessoes.csv", "potencia.csv"
)
var FileNames = []string{FileGaragem, FileCarregadores, FileOnibus, FileSessoes, FilePotencia}

type Garage struct {
	LimitKW                    float64
	PeakFromMin, PeakToMin     int      // minutes of the day; both 0 with HasTariff=false
	PeakPrice, OffPeakPrice    float64  // R$/kWh
	HasTariff                  bool
}
type Charger struct{ ID string; MaxKW, MinKW, Efficiency float64 }
type Bus struct {
	Line                int        // line in onibus.csv
	ID                  string
	CapacityKWh         float64
	Arrival, Departure  time.Time  // planned departure
	SoCArrivalPct       float64
	RequiredPct         float64
	MaxBatteryKW        float64    // 0 = default (resolved in Nights)
	RealSoCPct          *float64   // nil = unknown
	RealDeparture       *time.Time
}
type Session struct{ Line int; BusID, ChargerID string; Start, End time.Time; EnergyKWh float64 }
type PowerSample struct{ At time.Time; ChargerID string; KW float64 }
type Dataset struct {
	Garage   Garage
	Chargers []Charger
	Buses    []Bus
	Sessions []Session      // nil if sessoes.csv absent
	Power    []PowerSample  // nil if potencia.csv absent
	HasSessions, HasPower bool
	Warnings []Warning
}
func Load(files map[string][]byte) (*Dataset, error) // error is *FieldError; names outside FileNames are a warning
func LoadDir(dir string) (*Dataset, error)           // reads only the five fixed names inside dir
func NightKey(t time.Time) string                    // (t - 12h) formatted "2006-01-02"
```

Regras (testar cada uma): `garagem.csv` exige 1 linha de dados (mais de uma: aviso e usa a primeira); `limite_kw` obrigatório; `ponta_inicio/ponta_fim` em `HH:MM` e preços ≥ 0 só valem juntos (os quatro presentes, senão aviso "tarifa incompleta: custo não será calculado" e `HasTariff=false`); `carregadores.csv`: id único, `potencia_max_kw` obrigatório > 0, `potencia_min_kw` padrão 5 (com aviso geral "padrões usados: ..." agrupado por arquivo, não uma linha por padrão), `eficiencia` padrão 0,94 em (0,1]; `onibus.csv`: colunas obrigatórias da spec, `(onibus_id, noite)` único, `saida_prevista > chegada`, percentuais 0–100 (`soc_saida_exigido_pct` > 0), `capacidade_kwh` 1–2000, `potencia_max_bateria_kw` opcional > 0, `soc_saida_real_pct` vazio = desconhecido; `saida_real` opcional; `sessoes.csv`: ônibus precisa existir na noite do início da sessão, carregador precisa existir, `fim > inicio`, `energia_kwh >= 0`; `potencia.csv`: carregador conhecido, `potencia_kw >= 0` e ≤ 2× `potencia_max_kw` do carregador (acima: aviso, não erro), amostras ordenadas por instante dentro de cada carregador (desordenadas são ordenadas com aviso). Arquivos obrigatórios ausentes: `FieldError{File: nome, Message: "arquivo obrigatório ausente"}`. Totais acima dos limites das Global Constraints: erro com o limite.

Conjunto fictício `testdata/demo/` (valores usados pelos testes das Tasks 3–5; copie exatamente):

`garagem.csv`
```
limite_kw,ponta_inicio,ponta_fim,preco_ponta,preco_fora_ponta
400,18:00,21:00,2.70,0.90
```
`carregadores.csv`
```
carregador_id,potencia_max_kw,potencia_min_kw,eficiencia
C01,150,5,0.94
C02,150,5,0.94
C03,150,5,0.94
```
`onibus.csv`
```
onibus_id,capacidade_kwh,chegada,soc_chegada_pct,saida_prevista,soc_saida_exigido_pct,potencia_max_bateria_kw,soc_saida_real_pct,saida_real
B01,300,2026-03-04 20:00,30,2026-03-05 05:00,90,150,91,2026-03-05 05:02
B02,300,2026-03-04 22:00,40,2026-03-05 06:00,85,150,70,2026-03-05 06:00
B03,300,2026-03-04 23:30,20,2026-03-05 05:30,90,150,90,2026-03-05 05:30
B01,300,2026-03-05 21:30,35,2026-03-06 05:00,90,150,88,2026-03-06 05:00
B02,300,2026-03-05 22:00,50,2026-03-06 06:00,80,150,80,2026-03-06 06:00
B03,300,2026-03-06 00:20,25,2026-03-06 06:00,90,150,,
```
`sessoes.csv`
```
onibus_id,carregador_id,inicio,fim,energia_kwh
B01,C01,2026-03-04 20:00,2026-03-05 00:00,200
B02,C02,2026-03-04 22:00,2026-03-05 02:00,120
B03,C03,2026-03-04 23:30,2026-03-05 03:30,160
B01,C01,2026-03-05 22:00,2026-03-05 22:30,50
B02,C02,2026-03-05 22:00,2026-03-05 22:30,15
```
`potencia.csv`
```
instante,carregador_id,potencia_kw
2026-03-05 22:00,C01,100
2026-03-05 22:00,C02,20
2026-03-05 22:15,C01,100
2026-03-05 22:15,C02,40
2026-03-05 22:30,C01,0
2026-03-05 22:30,C02,0
```

- [ ] **Step 1: Testes que falham** em `load_test.go`: (a) `LoadDir("testdata/demo")` carrega 3 carregadores, 6 ônibus, 5 sessões, 6 amostras, `HasTariff`, `Garage.PeakFromMin==1080`, `PeakToMin==1260`; o ônibus B03 da segunda noite tem `RealSoCPct==nil`; `NightKey` de `2026-03-06 00:20` é `2026-03-05` e de `2026-03-04 20:00` é `2026-03-04`. (b) a mesma entrada convertida para `;`/vírgula decimal/CRLF/BOM dá `Dataset` igual (compare com `reflect.DeepEqual` nos campos de dados, ignorando `Warnings`). (c) uma tabela de erros: cada regra acima com arquivo, linha e coluna esperados (ex.: `soc_chegada_pct` = `120` na linha 4 de `onibus.csv` → `File:"onibus.csv", Line:4, Column:"soc_chegada_pct"`). (d) arquivo obrigatório ausente. (e) nome de arquivo desconhecido no mapa → aviso, sem erro. (f) sem `sessoes.csv`/`potencia.csv`: `HasSessions=false`, `HasPower=false`, sem erro. (g) nenhum pânico em 200 mapas de bytes aleatórios (semente fixa).
- [ ] **Step 2: Rodar e ver falhar:** `go test ./internal/realdata/ 2>&1 | head`.
- [ ] **Step 3: Implementar** `load.go` (e criar os cinco arquivos de `testdata/demo/` com o conteúdo acima).
- [ ] **Step 4: Rodar:** `go test -race ./internal/realdata/ && gofmt -l . && go vet ./...`.
- [ ] **Step 5: Commit e push** (`feat(realdata): validated dataset loader and a fictional demo depot`).

---

### Task 3: Noites — cenário do simulador e desfecho real

**Files:** Create `internal/realdata/nights.go`, `internal/realdata/nights_test.go`

**Interfaces:**
- Consumes: `Dataset`, `NightKey` (Task 2); `sim.Scenario`, `sim.BusSpec`, `sim.Tariff`, `model.Bus`, `model.Charger` (já existem).
- Produces:

```go
type Real struct {
	Buses        int      // buses in the night
	WithOutcome  int      // buses with soc_saida_real_pct
	Ready        int      // of WithOutcome
	ReadyPct     float64  // 100*Ready/WithOutcome, 0 if WithOutcome==0
	ShortfallKWh float64  // sum over buses with outcome that were not ready
	EnergyKWh    *float64 // nil = unknown (no sessions and no power)
	PeakKW       *float64
	PeakEstimated bool
	CostBRL      *float64 // nil = no tariff or no energy data
	CostEstimated bool
}
type BusReal struct{ ID string; FinalKWh *float64; Ready *bool }
type Night struct {
	Key      string        // "2026-03-04"
	Scenario sim.Scenario  // Name "real-<Key>", Seed 1, FollowSwaps true
	Real     Real
	BusReal  []BusReal     // sorted by bus ID
}
type NightOptions struct{ SoCNoiseKWh float64 } // >0 adds a soc_noise fault on every bus ("*", whole night)
func (d *Dataset) Nights(o NightOptions) []Night // sorted by Key
```

Regras: origem da noite = menor chegada da noite arredondada para baixo na hora cheia menos 60 min; `StartClockMin` = minuto do dia da origem; `ArrivalMin/DepartureMin` = minutos desde a origem; `Horizon` = maior saída prevista (e real, se maior) desde a origem + 30; `BaseLimitKW` = `LimitKW`; `Chargers` = todos, `Status: model.ChargerOK`; ônibus: `SoCKWh = Capacity*SoCArrivalPct/100`, `TargetKWh = Capacity*RequiredPct/100`, `TrueTargetKWh = TargetKWh`, `SoCConfidence: 1`, `MaxBatteryKW` = valor do ônibus ou o maior `MaxKW` entre os carregadores; `Tariff` quando `HasTariff` (senão o `Tariff` zero: preços 0); `Faults` vazio (ou o `soc_noise` pedido, `Value: SoCNoiseKWh`, `From: 0`, `To: math.MaxInt32`). Se o horizonte passar do que o simulador suporta (veja `sim.Run`/`Scenario` — use no máximo 3000 min) → noite é omitida com `Warning`.

Desfecho real: `Ready` por ônibus com `RealSoCPct` = `Capacity*RealSoCPct/100 >= min(TargetKWh, Capacity) - 1e-6`; `ShortfallKWh` soma `target - final` dos não prontos. Energia = soma de `EnergyKWh` das sessões da noite (se `HasSessions`), senão integral da série (retângulo, cada amostra vale até a próxima do mesmo carregador; a última vale 0 minutos) se `HasPower`, senão nil. Pico: se há amostras da noite → máximo da soma por instante entre carregadores (cada carregador mantém a última potência até a próxima amostra; avaliar em cada instante distinto) com `PeakEstimated=false`; senão, se há sessões → máximo da soma das potências médias `EnergyKWh/horas` das sessões ativas (avaliação nos inícios de sessão) com `PeakEstimated=true`; senão nil. Custo: com tarifa e amostras → energia de cada intervalo × preço no instante de início do intervalo (`sim.Tariff.PriceAt(minuto do dia)`) com `CostEstimated=false`; com tarifa e só sessões → cada sessão distribui a energia uniformemente por minuto de `Start` a `End` e cada minuto paga o preço do seu minuto do dia, `CostEstimated=true`; sem tarifa ou sem dados → nil.

- [ ] **Step 1: Testes que falham** (valores do conjunto fictício; calcule-os à mão como abaixo e escreva-os no teste, não derive do código):
  - Noite `2026-03-04`: 3 ônibus; origem 19:00 (`StartClockMin==1140`); `B01.ArrivalMin==60`; `B01.DepartureMin==600` (05:00 do dia seguinte); `Horizon==690+...`: maior saída 06:00 = 660 min desde 19:00, mais 30 → `Horizon==690`; real: `WithOutcome==3`, `Ready==2` (B02: 85 % de 300 = 255 > 70 % = 210 → déficit 45 kWh), `ShortfallKWh==45`, `ReadyPct` ≈ 66.667, `EnergyKWh==480` (200+120+160), `PeakEstimated==true` e `PeakKW==120` (50+30+40 entre 23:30 e 00:00), custo estimado: B01 20:00–24:00 → 1 h × 50 kW = 50 kWh a 2,70 + 150 kWh a 0,90 = 135 + 135; B02 120×0,90=108; B03 160×0,90=144 → `CostBRL==522` com `CostEstimated==true`. Verifique também `BusReal` (B03 `Ready==true` com 90 % exato, igual ao alvo).
  - Noite `2026-03-05`: chegada 00:20 do dia 06 pertence a ela (3 ônibus); B03 sem `RealSoCPct` → `WithOutcome==2`, `Ready==1` (B01: 88 % = 264 < 270 → déficit 6; B02: 80 % = 240 ≥ 240 → pronto), `ShortfallKWh==6`; `EnergyKWh==65`; `PeakEstimated==false`, `PeakKW==140`; custo medido: potência de 22:00–22:15 soma 120 kW × 0,25 h = 30 kWh, 22:15–22:30 soma 140 kW × 0,25 = 35 kWh, ambos fora da ponta → `CostBRL==58.5`, `CostEstimated==false`.
  - Sem tarifa → `CostBRL==nil`; sem sessões nem potência → `EnergyKWh`, `PeakKW`, `CostBRL` nil; `SoCNoiseKWh=3` acrescenta exatamente um `Fault` (`soc_noise`, `"*"`); `Scenario` de cada noite passa a rodar com `sim.Run(sc, sim.NewPlannerController(planner.DefaultConfig(), sc), nil)` sem pânico e com `PlanViolations==0`.
- [ ] **Step 2: Rodar e ver falhar.**
- [ ] **Step 3: Implementar** `nights.go`.
- [ ] **Step 4: Rodar:** `go test -race ./internal/realdata/ && gofmt -l . && go vet ./...`.
- [ ] **Step 5: Commit e push** (`feat(realdata): nights as simulator scenarios with the measured real outcome`).

---

### Task 4: `Replay` — controladores sobre cada noite

**Files:** Create `internal/realdata/replay.go`, `internal/realdata/replay_test.go`

**Interfaces:**
- Consumes: `Night`, `Real` (Task 3); `sim.ForController`, `sim.RunContext`, `sim.NewTrace`, `sim.Aggregate`, `sim.ControllerNames`, `planner.Config`.
- Produces:

```go
// ReplayNames are the rows after "real": the five sim.ControllerNames, with the planner
// twice (operators follow swaps / do not).
var ReplayNames = []string{"fifo", "edf", "fifo-unplug", "safe", "planner", "planner (sem rodízio)"}

type BusRow struct {
	ID         string
	CapacityKWh, TargetKWh float64
	RealFinalKWh *float64; RealReady *bool
	PlannerFinalKWh float64; PlannerReady bool
	NoSwapFinalKWh  float64; NoSwapReady  bool
}
type NightReport struct {
	Key         string
	Buses       int
	Real        Real
	Controllers []sim.ControllerResult // one Seed (Seed=1) per night, in ReplayNames order; Aggregate = that run
	PerBus      []BusRow               // sorted by ID
}
type Report struct {
	Nights      []NightReport
	Aggregate   []sim.ControllerResult // Aggregate over nights, in ReplayNames order (Seeds = one per night, Seed = night index+1)
	RealAggregate Real                  // see below
	Assumptions []string                // fixed Portuguese statements
	Warnings    []Warning
}
func Replay(ctx context.Context, d *Dataset, cfg planner.Config, o NightOptions, workers int) (*Report, error)
```

`RealAggregate`: `Buses` e `WithOutcome` somados; `ReadyPct` = média dos `ReadyPct` das noites que têm `WithOutcome>0` (mesmo critério de `sim.Aggregate`: média por execução); `ShortfallKWh` = média por noite com desfecho; `EnergyKWh`, `PeakKW`, `CostBRL` = média das noites com valor (nil se nenhuma); `PeakEstimated`/`CostEstimated` verdadeiros se alguma noite contribuinte for estimada. `Assumptions` (texto fixo, na ordem): leituras de carga do ônibus tratadas como perfeitas (ou, com ruído, "com ruído de X kWh"); limite de potência da garagem fixo durante a noite; nenhum defeito de carregador é injetado (o que aconteceu de errado já está nos dados); "planner" supõe que os operadores executam todos os rodízios e "planner (sem rodízio)" supõe que nenhum; a operação real tomou decisões que o planejador não vê.

Regras: para cada noite e cada nome, `sim.ForController` (para `"planner (sem rodízio)"` use `"planner"` com `sc.FollowSwaps=false`); `sim.RunContext(ctx, sc, ctrl, nil, tr)` com `tr := sim.NewTrace()` somente para `planner` e `planner (sem rodízio)` (os `Outcomes` alimentam `PerBus`); até `workers` execuções ao mesmo tempo; a ordem do resultado nunca depende de `workers`; `ctx` cancelado devolve `ctx.Err()` sem resultado parcial. `Metrics.PlanP99Micros` pode variar (tempo real) — o relatório o preserva (os testes o ignoram).

- [ ] **Step 1: Testes que falham:** (a) com `testdata/demo` e `planner.DefaultConfig()`: 2 noites; 6 linhas de controladores por noite na ordem de `ReplayNames`; `Buses` 3; para cada noite e controlador `Metrics.Buses==3`; `planner` e `planner (sem rodízio)` com `PlanViolations==0`; `PerBus` com 3 linhas por noite e `RealReady` batendo com `BusReal`. (b) determinismo: duas chamadas (workers 1 e 4) dão `Report` idêntico depois de zerar `PlanP99Micros` (`reflect.DeepEqual`). (c) `ctx` já cancelado → erro `context.Canceled`. (d) `RealAggregate.ReadyPct` ≈ média (66,667 e 50,0 → 58,333) e `ShortfallKWh` = (45+6)/2 = 25,5; `EnergyKWh` = (480+65)/2 = 272,5; `CostEstimated==true` (uma noite estimada); `PeakEstimated==true`. (e) `Assumptions` não vazio e com o texto do ruído quando `SoCNoiseKWh>0`.
- [ ] **Step 2: Rodar e ver falhar.**
- [ ] **Step 3: Implementar** `replay.go`.
- [ ] **Step 4: Rodar:** `go test -race ./internal/realdata/ && gofmt -l . && go vet ./...`.
- [ ] **Step 5: Commit e push** (`feat(realdata): replay controllers over real nights and compare with what happened`).

---

### Task 5: Comando `cmd/replay`

**Files:** Create `cmd/replay/main.go`, `cmd/replay/main_test.go`

**Interfaces:** Consumes `realdata.LoadDir`, `realdata.Replay`. Produces `func run(args []string, stdout, stderr io.Writer) int` (mesmo padrão de `cmd/simrun`).

Uso: `replay -dir PASTA [-soc-noise KWH] [-swap-back-cooldown N] [-swap-back-min-need KWH] [-json]`. Saída em texto: primeiro os avisos (`aviso: arquivo, linha N: mensagem`), depois `Premissas:` (uma por linha), depois, para cada noite e para o agregado, uma tabela (`text/tabwriter`) com as colunas do `simrun` (`controller ready% shortfall kWh peak kW plan violations overshoot min energy kWh cost R$ plan changes moves/run`) mais uma primeira linha `real` (com `—` onde for nil e o sufixo ` (estimado)` em pico/custo estimados) e, no fim de cada noite, a lista de ônibus não prontos na realidade com a situação do `planner`. Com `-json`, escreve o `Report` como JSON indentado (`json.Encoder`). Códigos: 0 ok, 1 erro de execução, 2 uso/dados inválidos (mensagem do `FieldError` em stderr, sem pânico). `-dir` obrigatório. `p99` não aparece na tabela de texto.

- [ ] **Step 1: Testes que falham** (`run` com `testdata` via caminho `../../internal/realdata/testdata/demo`): código 0 e stdout contém `2026-03-04`, a linha `real`, `planner (sem rodízio)`, `66.7` e `(estimado)`; `-dir` ausente → 2; pasta inexistente → 2 com mensagem em português; arquivo com erro (crie em `t.TempDir()` copiando o demo e quebrando `soc_chegada_pct`) → 2 e stderr contém `onibus.csv, linha`; `-json` produz JSON válido com `Nights` de tamanho 2; `-soc-noise 3` muda a linha das premissas.
- [ ] **Step 2: Falhar, Step 3: implementar, Step 4: `go test -race ./cmd/replay/ && go vet ./... && gofmt -l .`**
- [ ] **Step 5: Commit e push** (`feat(replay): command that replays real depot nights`).

---

### Task 6: API do laboratório — importar, comparar e abrir uma noite

**Files:** Modify `internal/lab/server.go`, `internal/lab/handlers.go`, `internal/lab/convert.go`; Create `internal/lab/realdata.go`, `internal/lab/realdata_test.go`

**Interfaces:**
- Consumes: `realdata.Load`, `Dataset.Nights`, `realdata.Replay`, `buildRun` (existente), `sim.ForController`.
- Produces (JSON; corpo até 16 MB nestas rotas):
  - `POST /api/import` — corpo `{"files": {"onibus.csv": "texto", ...}}` (valores string UTF-8; o navegador lê o arquivo como texto; se o arquivo não for UTF-8 o navegador envia `"..."` decodificado como Windows-1252, mas basta aceitar string). Resposta 200: `{"nights": [{"key","buses","with_outcome","ready","real_ready_pct"}], "warnings":[{"file","line","message"}], "has_sessions":bool,"has_power":bool,"has_tariff":bool}`; erro 400: `{"error":"mensagem","field":"onibus.csv","file":"onibus.csv","line":4,"column":"soc_chegada_pct"}` (acrescente `File`, `Line`, `Column` ao `apiError` com `omitempty`).
  - `POST /api/replay` — `{"files":{...},"soc_noise_kwh":0,"swap_back_cooldown_min":30,"swap_back_min_need_kwh":10}` (defaults do planejador quando ausentes); resposta = `realdata.Report` em JSON com campos em `snake_case` (acrescente tags `json` aos tipos de `realdata` Task 3–4 se ainda não existirem; `PlanP99Micros` zerado para o resultado ser determinístico). Limite de trabalho: soma de ônibus das noites × 6 ≤ `labMaxWork`×6 (isto é, soma de ônibus ≤ 20000); acima → 400 em português. Usa o mesmo semáforo (`s.acquire()`), prazo (`s.timeout`) e erros 503/504 das outras rotas.
  - `POST /api/run` — `runRequest` ganha `Files map[string]string \`json:"files,omitempty"\`` e `Night string \`json:"night,omitempty"\``; quando `Files` existe: carrega o conjunto, escolhe a noite `Night` (erro 400 se ausente/inexistente), usa `Controller` (`planner`, os cinco de `sim.ControllerNames` ou `"planner (sem rodízio)"`), ignora `Params` exceto `swap_back_*`, limites `labMaxRunBuses`/`labMaxRunChargers` valem; a resposta é a de hoje mais `"source": "Dados reais, noite AAAA-MM-DD"`; para o caso sem `Files` o JSON de hoje **não muda** (`source` fica omitido).
- Limite por rota: `route(method, limit int64, h)`; as rotas atuais passam `maxBody`; as três acima passam `maxBodyReal = 16 << 20`. A mensagem de 413 cita o limite da rota.
- Segurança: nomes de arquivo fora dos cinco → ignorados com aviso; nenhum acesso a disco; `Load` recebe só o mapa.

- [ ] **Step 1: Testes que falham** em `realdata_test.go` (use `httptest` como os testes existentes de `lab_test.go`; leia os arquivos de `../realdata/testdata/demo`): import do demo → 200 com 2 noites e `has_tariff`; import com `soc_chegada_pct=120` → 400 com `file`, `line`, `column` e mensagem em português; corpo de 17 MB → 413; corpo de 70 KB em `/api/compare` continua 413 (limite antigo); replay do demo → 200 com `controllers` de 6 nomes por noite e determinístico (duas respostas iguais); replay com ruído muda a resposta; run com `files`+`night:"2026-03-04"`+`controller:"planner"` → 200 com `source`, as séries e as `outcomes` coerentes com o demo (3 ônibus), `controller:"planner (sem rodízio)"` também; `night` inexistente → 400; run sem `files` continua byte a byte igual ao de antes (use um teste existente de golden/determinismo como referência e acrescente a verificação de ausência de `"source"`); `go test -race ./internal/lab/` completo continua passando.
- [ ] **Step 2: Falhar, Step 3: implementar, Step 4: `go test -race ./internal/lab/ && go vet ./... && gofmt -l .`**
- [ ] **Step 5: Commit e push** (`feat(lab): import, replay and detailed run for real depot nights`).

---

### Task 7: Aba "Dados reais" no laboratório

**Files:** Create `web/static/js/realdata.js`, `web/test/realdata.test.mjs`; Modify `web/static/index.html`, `web/static/js/main.js`, `web/static/js/api.js`, `web/static/js/run.js`, `web/static/app.css`

**Interfaces:**
- Consumes: `POST /api/import|replay|run` (Task 6); helpers existentes (`h`, `s`, `createLatest`, `fmtNum`, `fmtPct`, `describeParams`, `createRunView`, `showTab`, `TABS`, `nextTab`, glossário).
- Produces: nova aba `real` (`TABS` ganha `'real'` entre `compare` e `run`... ordem: `scenario`, `compare`, `real`, `run`), com ids `tab-real`, `real-files`, `real-out`; funções puras testadas em `realdata.js`: `readFileText(buf)` (decodifica `ArrayBuffer` como UTF-8 e, se falhar, como Windows-1252), `pickKnownFiles(fileList)` (devolve `{nome: File}` só dos cinco nomes, ignorando maiúsculas, e a lista de ignorados), `formatImportError(err)` (`arquivo, linha N, coluna X: mensagem`), `realRowCells(real)` (células da linha `real` com `—` e selo `estimado` para pico/custo), `worstBuses(night)` (ônibus não prontos na realidade com a situação do planejador).

Comportamento: escolher arquivos (`<input type="file" multiple accept=".csv">`, também arrastar) → o navegador lê o texto de cada um (nunca envia para outro lugar) → `POST /api/import` → mostra o resumo (noites, ônibus, o que existe: sessões/potência/tarifa), a lista de avisos e, se houver erro, a lista de erros com arquivo/linha/coluna; botão **Comparar** (habilitado após import ok) → `POST /api/replay` → faixa fixa "Dados reais: o resultado vale para esta garagem e estes dias. As premissas abaixo continuam sendo suposição." + lista de premissas + seleção da noite ("Todas (média)" ou uma) + tabela com a linha `real` primeiro e os 6 controladores (colunas do laboratório, sem p99) + por ônibus "pronto na realidade × no planejador × sem rodízio" + botão "Abrir esta noite na Execução" (usa `/api/run` com `files` e `night`, controlador escolhido) — a Execução mostra `source` no cabeçalho e **não oferece link** ("Copiar link" avisa: "Execuções de dados reais não geram link: os dados ficam só neste computador"). Os arquivos importados ficam só na memória da página. Estados: vazio, lendo, importando, erro de rede, erro de dados, comparando (com `createLatest`, nova ação cancela a anterior). Acessível: tabela com cabeçalhos, foco após ações, `aria-live` para status, informação nunca só por cor.

- [ ] **Step 1: Testes Node que falham** (`realdata.test.mjs`) para as cinco funções puras, incluindo `0xE7` → `ç` em `readFileText`, nomes `ONIBUS.CSV` aceitos, ignorados listados, `—`/`estimado` em `realRowCells`, e `worstBuses` ordenado por ID.
- [ ] **Step 2: Falhar:** `node --test web/test/realdata.test.mjs`.
- [ ] **Step 3: Implementar** os módulos, a aba, o CSS (tokens existentes, claro/escuro) e a integração com `main.js`/`run.js` (cabeçalho da execução com `source`, bloqueio do link).
- [ ] **Step 4: Rodar tudo:** `gofmt -l . ; go vet ./... && go test -race ./... && node --test web/test/*.test.mjs`; reconstruir o binário (`go build ./...`; os arquivos de `web/static` são embutidos) e **verificar no navegador embutido**: importar a pasta `internal/realdata/testdata/demo` (use o seletor de arquivos), ver avisos, Comparar, abrir a noite 2026-03-04 do planejador na Execução, tema escuro, importar um arquivo com erro (veja a lista de erros), console sem erros, sem requisições externas; encerrar o servidor.
- [ ] **Step 5: Commit e push** (`feat(lab): "Dados reais" tab to import depot spreadsheets and compare with what happened`).

---

### Task 8: Documentação e verificação final

**Files:** Modify `README.md`, `docs/dados-reais/README.md`, `docs/superpowers/specs/2026-10-08-validacao-dados-reais-design.md` (linha de status)

- [ ] **Step 1: README.** Seção "Validação com dados reais": o que faz, `go run ./cmd/replay -dir docs/dados-reais/modelos` (cuidado: os modelos têm pouca informação; para o exemplo completo use `internal/realdata/testdata/demo`), a aba "Dados reais", o que é medido × estimado, premissas, e o aviso "dados fictícios de exemplo". `docs/dados-reais/README.md`: acrescente "Como rodar" e os nomes exatos dos cinco arquivos.
- [ ] **Step 2: Verificação.** (1) `go run ./cmd/replay -dir internal/realdata/testdata/demo` imprime as tabelas e coincide com os valores esperados da Task 3 (linha `real`: 66.7 e 45 kWh de déficit na primeira noite). (2) O mesmo conjunto pela aba mostra os mesmos números que o `replay` (compare `ready%`, `peak kW`, `energy kWh`, `cost R$` linha a linha). (3) `go run ./cmd/simrun -profile none -seeds 3` e a aba Comparação continuam idênticos aos de antes: rode o `simrun` no commit `ea0ffa6` (por exemplo numa cópia temporária com `git worktree add` ou `git archive`) e no atual, e use `diff` ignorando a coluna `p99 µs`. (4) Erros: planilha com número fora de faixa, sem arquivo obrigatório, CSV vazio, 17 MB → mensagens em português e sem travar. (5) Segurança: `curl` com `Host: evil.example.com` → 403; corpo de 17 MB → 413.
- [ ] **Step 3: Status da spec** (`Status: implementado ...`), `gofmt -l . ; go vet ./... && go test -race ./... && node --test web/test/*.test.mjs`, commit e push (`docs: real-data validation guide and README section`).
