package planner

import (
	"fmt"
	"math"
	"math/rand"
	"reflect"
	"strings"
	"testing"

	"github.com/EduardoMilani8/depot-charge-planner/internal/model"
)

// peakPolicy is a 18:00-21:00 peak (1080..1260). With ClockAtZeroMin 1200 and Now 0 the
// clock is 20:00, so the peak ends in 60 minutes.
func peakPolicy() *CostPolicy {
	return &CostPolicy{ClockAtZeroMin: 1200, PeakFromMin: 1080, PeakToMin: 1260, ScheduleKnown: true}
}

// costConfig has the schedule known (Input.Buses lists every bus): deferral needs one
// plug per bus present or announced. blindConfig does not know it: the policy does nothing.
func costConfig() Config {
	c := testConfig()
	c.Cost = peakPolicy()
	return c
}

func blindConfig() Config {
	c := costConfig()
	pol := *c.Cost
	pol.ScheduleKnown = false
	c.Cost = &pol
	return c
}

// slowCharger has a 100 kW ceiling, so a bus never draws more than 100 kW on it.
func slowCharger(id string) model.Charger {
	c := testCharger(id)
	c.MaxKW = 100
	return c
}

func costInput(limit float64, chargers []model.Charger, buses ...model.Bus) Input {
	return Input{Site: model.Site{LimitKW: limit, StepMin: 1}, Chargers: chargers, Buses: buses}
}

func isDeferred(st BusStatus) bool {
	return strings.HasPrefix(st.Reason, "adiado para depois da ponta")
}

func TestPeakEndsIn(t *testing.T) {
	cases := []struct {
		name string
		p    CostPolicy
		now  int
		want int
	}{
		{"inside, ends in 60", CostPolicy{ClockAtZeroMin: 1200, PeakFromMin: 1080, PeakToMin: 1260}, 0, 60},
		{"the minute the peak ends", CostPolicy{ClockAtZeroMin: 1200, PeakFromMin: 1080, PeakToMin: 1260}, 60, 0},
		{"the last peak minute", CostPolicy{ClockAtZeroMin: 1200, PeakFromMin: 1080, PeakToMin: 1260}, 59, 1},
		{"first peak minute", CostPolicy{ClockAtZeroMin: 1080, PeakFromMin: 1080, PeakToMin: 1260}, 0, 180},
		{"before the peak", CostPolicy{ClockAtZeroMin: 1079, PeakFromMin: 1080, PeakToMin: 1260}, 0, 0},
		{"negative Now", CostPolicy{ClockAtZeroMin: 1200, PeakFromMin: 1080, PeakToMin: 1260}, -10, 70},
		{"negative clock at zero is reduced modulo a day", CostPolicy{ClockAtZeroMin: -240, PeakFromMin: 1080, PeakToMin: 1260}, 0, 60},
		{"clock at zero above a day", CostPolicy{ClockAtZeroMin: 1440 + 1200, PeakFromMin: 1080, PeakToMin: 1260}, 0, 60},
		{"Now beyond a day", CostPolicy{ClockAtZeroMin: 0, PeakFromMin: 1080, PeakToMin: 1260}, 3*1440 + 1100, 160},
		{"crossing midnight, before it", CostPolicy{ClockAtZeroMin: 1380, PeakFromMin: 1320, PeakToMin: 120}, 0, 180},
		{"crossing midnight, after it", CostPolicy{ClockAtZeroMin: 1380, PeakFromMin: 1320, PeakToMin: 120}, 90, 90},
		{"crossing midnight, exactly at it", CostPolicy{ClockAtZeroMin: 1380, PeakFromMin: 1320, PeakToMin: 120}, 60, 120},
		{"crossing midnight, first minute", CostPolicy{ClockAtZeroMin: 1380, PeakFromMin: 1320, PeakToMin: 120}, -60, 240},
		{"crossing midnight, a minute before it starts", CostPolicy{ClockAtZeroMin: 1380, PeakFromMin: 1320, PeakToMin: 120}, -61, 0},
		{"crossing midnight, the minute it ends", CostPolicy{ClockAtZeroMin: 1380, PeakFromMin: 1320, PeakToMin: 120}, 180, 0},
		{"From == To is never peak", CostPolicy{ClockAtZeroMin: 500, PeakFromMin: 500, PeakToMin: 500}, 0, 0},
		{"window to the end of the day", CostPolicy{ClockAtZeroMin: 0, PeakFromMin: 1080, PeakToMin: 1440}, 1100, 340},
		{"whole-day window never ends", CostPolicy{ClockAtZeroMin: 0, PeakFromMin: 0, PeakToMin: 1440}, 10, 0},
		{"whole-day window at midnight", CostPolicy{ClockAtZeroMin: 0, PeakFromMin: 0, PeakToMin: 1440}, 0, 0},
	}
	for _, c := range cases {
		if got := c.p.peakEndsIn(c.now); got != c.want {
			t.Errorf("%s: peakEndsIn(%d) = %d, want %d", c.name, c.now, got, c.want)
		}
	}
}

// Outside the peak the policy changes nothing: the plans are identical, field by field.
func TestCostOutsidePeakPlanIsIdentical(t *testing.T) {
	rng := rand.New(rand.NewSource(7))
	for i := 0; i < 5; i++ {
		in := randomInput(rng, 0)
		in.Now = 100 // clock 1300 with the policy below: after the 21:00 end
		plain := PlanNormal(testConfig(), in)
		cfg := testConfig()
		cfg.Cost = &CostPolicy{ClockAtZeroMin: 1200, PeakFromMin: 1080, PeakToMin: 1260}
		if got := PlanNormal(cfg, in); !reflect.DeepEqual(got, plain) {
			t.Errorf("input %d: plan with the policy outside the peak differs\n got  %+v\n want %+v", i, got, plain)
		}
	}
}

// A bus with plenty of slack is held back: 100 kWh from the grid at 100 kW, 600 min
// to the departure, the peak ends in 60 min. After the peak it still has
// 600 - 60 - 60 = 480 min of laxity, far above the 45 min margin.
func TestCostDefersABusWithSlack(t *testing.T) {
	b := testBus("B1", "C1", 100, 200, 600)
	p := PlanNormal(costConfig(), costInput(1000, []model.Charger{slowCharger("C1")}, b))
	if sp(p, "C1") != 0 {
		t.Errorf("setpoint = %v, want 0 (deferred)", sp(p, "C1"))
	}
	st := statusOf(p, "B1")
	if !st.WillReachTarget || st.ShortfallKWh != 0 {
		t.Errorf("a deferred bus does reach its target after the peak: %+v", st)
	}
	if want := "adiado para depois da ponta: termina com folga de 480 min"; st.Reason != want {
		t.Errorf("reason = %q, want %q", st.Reason, want)
	}
	if st.LaxityMin != 540 {
		t.Errorf("laxity = %v, want 540 (the plain laxity, 600 - 60)", st.LaxityMin)
	}
	if len(p.Setpoints) != 1 || p.Setpoints[0].ChargerID != "C1" {
		t.Errorf("the deferred bus's charger must still be listed (at 0 kW): %+v", p.Setpoints)
	}
}

// Without the policy the same bus charges at once (the baseline the test above defers).
func TestCostDisabledChargesTheSameBus(t *testing.T) {
	b := testBus("B1", "C1", 100, 200, 600)
	p := PlanNormal(testConfig(), costInput(1000, []model.Charger{slowCharger("C1")}, b))
	if sp(p, "C1") != 100 {
		t.Errorf("setpoint = %v, want 100", sp(p, "C1"))
	}
}

// A tight bus cannot wait: 150 kWh at 100 kW with 180 min left would need
// 75 * 100 / 60 = 125 kWh in the minutes left after the peak and the margin, so it
// charges now (at 100 kW: 50 kW to meet the deadline plus all the spare power).
func TestCostDoesNotDeferATightBus(t *testing.T) {
	b := testBus("B1", "C1", 50, 200, 180)
	p := PlanNormal(costConfig(), costInput(1000, []model.Charger{slowCharger("C1")}, b))
	if sp(p, "C1") != 100 {
		t.Errorf("setpoint = %v, want 100", sp(p, "C1"))
	}
	if st := statusOf(p, "B1"); isDeferred(st) || !st.WillReachTarget {
		t.Errorf("status = %+v, want charging and reaching the target", st)
	}
}

// A deferred bus is out of the spare-power pass: the tight bus next to it gets all it can
// use (100 kW), the deferred one nothing although 300 kW are spare, and the sum stays
// within the limit.
func TestCostDeferredBusDoesNotShareTheBudget(t *testing.T) {
	tight := testBus("T", "C1", 50, 200, 180)
	slack := testBus("S", "C2", 100, 200, 600)
	p := PlanNormal(costConfig(), costInput(400, []model.Charger{slowCharger("C1"), slowCharger("C2")}, tight, slack))
	if sp(p, "C1") != 100 || sp(p, "C2") != 0 {
		t.Errorf("setpoints = %v / %v, want 100 / 0", sp(p, "C1"), sp(p, "C2"))
	}
	if !isDeferred(statusOf(p, "S")) || isDeferred(statusOf(p, "T")) {
		t.Errorf("statuses: %+v / %+v", statusOf(p, "S"), statusOf(p, "T"))
	}
}

// Readings that cannot be trusted never defer a bus: stale, low confidence, or absurd.
func TestCostNeverDefersWithoutAReliableReading(t *testing.T) {
	cases := map[string]func(b *model.Bus){
		"stale":          func(b *model.Bus) { b.SoCAgeMin = 100 },
		"low confidence": func(b *model.Bus) { b.SoCConfidence = 0.3 },
		"absurd":         func(b *model.Bus) { b.SoCKWh = 5000 },
		"nan":            func(b *model.Bus) { b.SoCKWh = math.NaN() },
	}
	for name, mutate := range cases {
		b := testBus("B1", "C1", 100, 200, 600)
		mutate(&b)
		p := PlanNormal(costConfig(), costInput(1000, []model.Charger{slowCharger("C1")}, b))
		if st := statusOf(p, "B1"); isDeferred(st) {
			t.Errorf("%s: deferred: %+v", name, st)
		}
		if sp(p, "C1") <= 0 {
			t.Errorf("%s: setpoint = %v, want power", name, sp(p, "C1"))
		}
		plain := PlanNormal(testConfig(), costInput(1000, []model.Charger{slowCharger("C1")}, b))
		if !reflect.DeepEqual(p, plain) {
			t.Errorf("%s: plan differs from the plan without the policy\n got  %+v\n want %+v", name, p, plain)
		}
	}
}

// Two deferrable buses that do not fit together after the peak: 240 kW site limit, 60 %
// of it (144 kW) counts for the test, 60 min usable after the peak (165 - 60 - 45) =
// 144 kWh. B1 needs 90 kWh and B2 80 kWh: both together (170) do not fit, the one
// needing more energy (B1) is the one that charges now, B2 is deferred.
func TestCostJointFeasibilityKeepsTheLargestSetThatFits(t *testing.T) {
	b1 := testBus("B1", "C1", 100, 190, 165) // 90 kWh
	b2 := testBus("B2", "C2", 100, 180, 165) // 80 kWh
	p := PlanNormal(costConfig(), costInput(240, []model.Charger{slowCharger("C1"), slowCharger("C2")}, b1, b2))
	if sp(p, "C1") != 100 || sp(p, "C2") != 0 {
		t.Errorf("setpoints = %v / %v, want 100 / 0", sp(p, "C1"), sp(p, "C2"))
	}
	if isDeferred(statusOf(p, "B1")) {
		t.Errorf("B1 must charge now: %+v", statusOf(p, "B1"))
	}
	// B2: laxity 165 - 80/100*60 = 117, 57 after the peak.
	if want := "adiado para depois da ponta: termina com folga de 57 min"; statusOf(p, "B2").Reason != want {
		t.Errorf("B2 reason = %q, want %q", statusOf(p, "B2").Reason, want)
	}
}

// Same energy, same deadline: the tie is broken by ID, the larger ID charges now.
func TestCostJointFeasibilityTieBreaksByID(t *testing.T) {
	b1 := testBus("B1", "C1", 100, 190, 165)
	b2 := testBus("B2", "C2", 100, 190, 165)
	p := PlanNormal(costConfig(), costInput(240, []model.Charger{slowCharger("C1"), slowCharger("C2")}, b1, b2))
	if sp(p, "C1") != 0 || sp(p, "C2") != 100 {
		t.Errorf("setpoints = %v / %v, want 0 / 100", sp(p, "C1"), sp(p, "C2"))
	}
	if !isDeferred(statusOf(p, "B1")) || isDeferred(statusOf(p, "B2")) {
		t.Errorf("B1 must be deferred and B2 not: %+v / %+v", statusOf(p, "B1"), statusOf(p, "B2"))
	}
}

// If the depot would be too busy after the peak (a waiting bus needs the budget too),
// nothing is deferred: 240 kW site, 144 kW counted, 60 min after the peak = 144 kWh;
// the waiting bus needs 90 kWh and B1 80 kWh, together 170 kWh. W has a free charger
// (C2) it has not been plugged into yet, so plugs are not scarce.
func TestCostWaitingBusCompetesForThePostPeakBudget(t *testing.T) {
	b1 := testBus("B1", "C1", 100, 180, 165) // 80 kWh
	w := testBus("W", "", 100, 190, 165)     // 90 kWh, no charger yet
	in := costInput(240, []model.Charger{slowCharger("C1"), slowCharger("C2")}, b1, w)
	p := PlanNormal(costConfig(), in)
	if isDeferred(statusOf(p, "B1")) || sp(p, "C1") != 100 {
		t.Errorf("B1 must charge now: %+v, setpoint %v", statusOf(p, "B1"), sp(p, "C1"))
	}
	// Without the waiting bus the same B1 is deferred.
	in.Buses = in.Buses[:1]
	if p := PlanNormal(costConfig(), in); !isDeferred(statusOf(p, "B1")) {
		t.Errorf("B1 alone must be deferred: %+v", statusOf(p, "B1"))
	}
}

// Faulted or offline chargers and buses without a charger are never deferred and their
// status is exactly the one without the policy.
func TestCostLeavesUnavailableChargersAndWaitingBusesAlone(t *testing.T) {
	faulted := slowCharger("C1")
	faulted.Status = model.ChargerFaulted
	offline := slowCharger("C2")
	offline.Status = model.ChargerOffline
	offline.LastCommandedKW = 30
	// Four buses need four healthy chargers (C3..C6) for deferral to be allowed at all.
	spare := []model.Charger{slowCharger("C4"), slowCharger("C5"), slowCharger("C6")}
	in := costInput(1000, append([]model.Charger{faulted, offline}, spare...),
		testBus("F", "C1", 100, 200, 600), testBus("O", "C2", 100, 200, 600), testBus("W", "", 100, 200, 600))
	with := PlanNormal(costConfig(), in)
	without := PlanNormal(testConfig(), in)
	if !reflect.DeepEqual(with, without) {
		t.Errorf("plan differs\n got  %+v\n want %+v", with, without)
	}
	for _, id := range []string{"F", "O", "W"} {
		if isDeferred(statusOf(with, id)) {
			t.Errorf("%s was deferred: %+v", id, statusOf(with, id))
		}
	}
	// Next to a bus that is deferred, their statuses are still the plain ones.
	in.Chargers = append(in.Chargers, slowCharger("C3"))
	in.Buses = append(in.Buses, testBus("D", "C3", 100, 200, 600))
	with = PlanNormal(costConfig(), in)
	without = PlanNormal(testConfig(), in)
	if !isDeferred(statusOf(with, "D")) {
		t.Errorf("D must be deferred: %+v", statusOf(with, "D"))
	}
	for _, id := range []string{"F", "O", "W"} {
		if !reflect.DeepEqual(statusOf(with, id), statusOf(without, id)) {
			t.Errorf("%s status changed: %+v vs %+v", id, statusOf(with, id), statusOf(without, id))
		}
	}
}

// The test is re-evaluated every minute with the time left: one minute before the
// peak ends the bus is deferred, on the minute it ends it charges at once.
func TestCostDeferredBusStartsWhenThePeakEnds(t *testing.T) {
	cfg := costConfig()
	cfg.Cost = &CostPolicy{ClockAtZeroMin: 1259, PeakFromMin: 1080, PeakToMin: 1260, ScheduleKnown: true}
	in := costInput(1000, []model.Charger{slowCharger("C1")}, testBus("B1", "C1", 100, 200, 600))
	if p := PlanNormal(cfg, in); sp(p, "C1") != 0 || !isDeferred(statusOf(p, "B1")) {
		t.Errorf("at the last peak minute: setpoint %v, status %+v", sp(p, "C1"), statusOf(p, "B1"))
	}
	in.Now = 1
	if p := PlanNormal(cfg, in); sp(p, "C1") != 100 || isDeferred(statusOf(p, "B1")) {
		t.Errorf("when the peak has ended: setpoint %v, status %+v", sp(p, "C1"), statusOf(p, "B1"))
	}
}

// The clock may cross midnight: a 22:00-02:00 peak, 23:00 at minute 0, ends in 180 min.
func TestCostWindowAcrossMidnight(t *testing.T) {
	cfg := testConfig()
	cfg.Cost = &CostPolicy{ClockAtZeroMin: 1380, PeakFromMin: 1320, PeakToMin: 120, ScheduleKnown: true}
	in := costInput(1000, []model.Charger{slowCharger("C1")}, testBus("B1", "C1", 100, 200, 600))
	p := PlanNormal(cfg, in)
	// laxity 540; after the peak (180 min) 360.
	if want := "adiado para depois da ponta: termina com folga de 360 min"; statusOf(p, "B1").Reason != want || sp(p, "C1") != 0 {
		t.Errorf("status %+v setpoint %v, want %q and 0", statusOf(p, "B1"), sp(p, "C1"), want)
	}
	in.Now = 180 // 02:00: the peak has ended
	in.Buses[0].DepartureMin = 780
	if p := PlanNormal(cfg, in); sp(p, "C1") != 100 {
		t.Errorf("after the peak: setpoint %v, want 100", sp(p, "C1"))
	}
}

// An invalid policy is ignored (with a note), never a panic, and the plan is the plain one.
func TestCostInvalidPolicyIsIgnoredWithANote(t *testing.T) {
	bad := map[string]CostPolicy{
		"from below 0":      {ClockAtZeroMin: 1200, PeakFromMin: -1, PeakToMin: 1260},
		"to above 1440":     {ClockAtZeroMin: 1200, PeakFromMin: 1080, PeakToMin: 1441},
		"from above 1440":   {ClockAtZeroMin: 1200, PeakFromMin: 5000, PeakToMin: 1260},
		"negative margin":   {ClockAtZeroMin: 1200, PeakFromMin: 1080, PeakToMin: 1260, DeferMarginMin: -1},
		"NaN margin":        {ClockAtZeroMin: 1200, PeakFromMin: 1080, PeakToMin: 1260, DeferMarginMin: math.NaN()},
		"infinite margin":   {ClockAtZeroMin: 1200, PeakFromMin: 1080, PeakToMin: 1260, DeferMarginMin: math.Inf(1)},
		"negative target":   {ClockAtZeroMin: 1200, PeakFromMin: 1080, PeakToMin: 1260, DemandTargetKW: -5},
		"NaN target":        {ClockAtZeroMin: 1200, PeakFromMin: 1080, PeakToMin: 1260, DemandTargetKW: math.NaN()},
		"infinite target":   {ClockAtZeroMin: 1200, PeakFromMin: 1080, PeakToMin: 1260, DemandTargetKW: math.Inf(-1)},
		"negative -inf mrg": {ClockAtZeroMin: 1200, PeakFromMin: 1080, PeakToMin: 1260, DeferMarginMin: math.Inf(-1)},
	}
	in := costInput(1000, []model.Charger{slowCharger("C1")}, testBus("B1", "C1", 100, 200, 600))
	plain := PlanNormal(testConfig(), in)
	for name, pol := range bad {
		pol := pol
		cfg := testConfig()
		cfg.Cost = &pol
		got := PlanNormal(cfg, in)
		if len(got.Notes) != 1 || !strings.Contains(got.Notes[0], "política de custo ignorada") {
			t.Errorf("%s: notes = %q, want one 'política de custo ignorada' note", name, got.Notes)
		}
		got.Notes = nil
		if !reflect.DeepEqual(got, plain) {
			t.Errorf("%s: plan differs from the plain one\n got  %+v\n want %+v", name, got, plain)
		}
		// The full planner takes the same path and still hands out a valid plan.
		if v := Violations(in, New(cfg).Plan(in)); len(v) > 0 {
			t.Errorf("%s: Planner plan violates: %v", name, v)
		}
	}
	// A valid policy adds no note.
	cfg := testConfig()
	cfg.Cost = peakPolicy()
	if got := PlanNormal(cfg, in); len(got.Notes) != 0 {
		t.Errorf("valid policy: notes = %q, want none", got.Notes)
	}
}

// The default margin is 45 min when DeferMarginMin is 0, and an explicit one is used as is.
func TestCostDeferMarginDefaultAndExplicit(t *testing.T) {
	// 100 kWh at 100 kW, 6 min of laxity to spare: departure at 60 (peak) + 60 (charging)
	// + margin + 5. With margin 45: 170 -> (170-60-45)*100/60 = 108 >= 100: deferred.
	b := testBus("B1", "C1", 100, 200, 170)
	in := costInput(1000, []model.Charger{slowCharger("C1")}, b)
	cfg := costConfig()
	if p := PlanNormal(cfg, in); !isDeferred(statusOf(p, "B1")) {
		t.Errorf("default margin 45: not deferred: %+v", statusOf(p, "B1"))
	}
	cfg.Cost = &CostPolicy{ClockAtZeroMin: 1200, PeakFromMin: 1080, PeakToMin: 1260, DeferMarginMin: 60, ScheduleKnown: true}
	if p := PlanNormal(cfg, in); isDeferred(statusOf(p, "B1")) { // (170-60-60)*100/60 = 83 < 100
		t.Errorf("margin 60: deferred: %+v", statusOf(p, "B1"))
	}
}

// In a swap scenario (more buses than plugs) the policy does nothing: the plan, swaps
// included, is exactly the one without it, however much slack the connected bus has.
func TestCostDoesNothingWhenPlugsAreScarce(t *testing.T) {
	full := testBus("F", "C2", 250, 200, 600) // already above its target: the donor
	slack := testBus("S", "C1", 100, 200, 600)
	waiting := testBus("W", "", 100, 220, 200)
	in := costInput(1000, []model.Charger{slowCharger("C1"), slowCharger("C2")}, slack, full, waiting)
	without := PlanNormal(testConfig(), in)
	if len(without.Swaps) != 1 || without.Swaps[0].OutBusID != "F" {
		t.Fatalf("setup: swaps without the policy = %+v, want F giving way", without.Swaps)
	}
	for name, cfg := range map[string]Config{"known": costConfig(), "blind": blindConfig()} {
		if with := PlanNormal(cfg, in); !reflect.DeepEqual(with, without) {
			t.Errorf("%s: plan differs with 3 buses on 2 chargers\n got  %+v\n want %+v", name, with, without)
		}
	}
}

// randomInput builds a small depot: every bus connected, some chargers free, a few faulty
// chargers and unreliable readings. Buses depart far enough after the 20:00 Now=0 of
// the policy tests to give some of them slack.
func randomInput(rng *rand.Rand, now int) Input {
	nb := 3 + rng.Intn(8)
	nc := nb + rng.Intn(4)
	in := Input{Now: now, Site: model.Site{LimitKW: float64(100 + rng.Intn(1500)), StepMin: 1}}
	for i := 0; i < nc; i++ {
		c := testCharger(fmt.Sprintf("C%02d", i))
		c.MaxKW = float64(50 + rng.Intn(100))
		c.Efficiency = 0.9 + 0.1*rng.Float64()
		switch rng.Intn(12) {
		case 0:
			c.Status = model.ChargerFaulted
		case 1:
			c.Status = model.ChargerOffline
			c.LastCommandedKW = float64(rng.Intn(40))
		}
		in.Chargers = append(in.Chargers, c)
	}
	for i := 0; i < nb; i++ {
		b := testBus(fmt.Sprintf("B%02d", i), "", float64(30+rng.Intn(150)), float64(150+rng.Intn(120)), now+90+rng.Intn(700))
		b.ChargerID = in.Chargers[i].ID
		switch rng.Intn(10) {
		case 0:
			b.SoCAgeMin = 100
		case 1:
			b.SoCConfidence = 0.2
		}
		in.Buses = append(in.Buses, b)
	}
	return in
}

// 200 random inputs inside the peak: the policy never raises the total power, never
// exceeds the limit, the verifier has nothing to correct, and a deferred bus is reported
// as reaching its target with the right reason.
func TestCostPropertyLowerPowerAndValidPlans(t *testing.T) {
	rng := rand.New(rand.NewSource(20261011))
	deferredSeen := 0
	for i := 0; i < 200; i++ {
		in := randomInput(rng, rng.Intn(50)) // clock 1200..1249: inside the peak
		cfg := costConfig()
		with := PlanNormal(cfg, in)
		without := PlanNormal(testConfig(), in)
		sum := func(p Plan) float64 {
			s := 0.0
			for _, sp := range p.Setpoints {
				s += sp.KW
			}
			return s
		}
		if sum(with) > sum(without)+1e-6 {
			t.Fatalf("input %d: total with the policy %.3f > without %.3f\n%+v", i, sum(with), sum(without), in)
		}
		if v := Violations(in, with); len(v) > 0 {
			t.Fatalf("input %d: %v", i, v)
		}
		if enforced := Enforce(in, with); !reflect.DeepEqual(enforced, with) {
			t.Fatalf("input %d: Enforce changed the plan", i)
		}
		if v := Violations(in, New(cfg).Plan(in)); len(v) > 0 {
			t.Fatalf("input %d: Planner plan: %v", i, v)
		}
		for _, st := range with.Buses {
			if !isDeferred(st) {
				continue
			}
			deferredSeen++
			if !st.WillReachTarget || st.ShortfallKWh != 0 || !strings.HasSuffix(st.Reason, " min") {
				t.Fatalf("input %d: deferred status %+v", i, st)
			}
			for _, s := range with.Setpoints {
				b := busByID(in, st.BusID)
				if s.ChargerID == b.ChargerID && s.KW != 0 {
					t.Fatalf("input %d: deferred bus %s got %v kW", i, st.BusID, s.KW)
				}
			}
		}
	}
	if deferredSeen < 20 {
		t.Errorf("only %d deferred buses in 200 inputs: the property would prove little", deferredSeen)
	}
}

func busByID(in Input, id string) model.Bus {
	for _, b := range in.Buses {
		if b.ID == id {
			return b
		}
	}
	return model.Bus{}
}

// Hostile numbers must never panic or defer on nonsense: PlanNormal is also called directly.
func TestCostNonFiniteInputsAreNeverDeferred(t *testing.T) {
	for name, mutate := range map[string]func(*Input){
		"NaN capacity":  func(in *Input) { in.Buses[0].CapacityKWh = math.NaN() },
		"NaN target":    func(in *Input) { in.Buses[0].TargetKWh = math.NaN() },
		"NaN limit":     func(in *Input) { in.Site.LimitKW = math.NaN() },
		"NaN max power": func(in *Input) { in.Chargers[0].MaxKW = math.NaN() },
		"NaN battery":   func(in *Input) { in.Buses[0].MaxBatteryKW = math.NaN() },
		"zero capacity": func(in *Input) { in.Buses[0].CapacityKWh = 0 },
	} {
		in := costInput(1000, []model.Charger{slowCharger("C1")}, testBus("B1", "C1", 100, 200, 600))
		mutate(&in)
		p := PlanNormal(costConfig(), in) // must not panic
		if st := statusOf(p, "B1"); isDeferred(st) {
			t.Errorf("%s: deferred on non-finite or absurd numbers: %+v", name, st)
		}
	}
}

// A bus whose departure is inside the peak, or too close to its end to leave the margin,
// is never deferred; neither is a short-stay bus that arrives in the middle of the peak.
func TestCostNeverDefersABusThatLeavesInsideThePeak(t *testing.T) {
	// tp = 60. Departure 50: inside the peak. Departure 105: after it but less than the
	// 45 min margin later. Both charge at once.
	for _, dep := range []int{50, 105} {
		p := PlanNormal(costConfig(), costInput(1000, []model.Charger{slowCharger("C1")}, testBus("B1", "C1", 180, 200, dep)))
		if isDeferred(statusOf(p, "B1")) || sp(p, "C1") <= 0 {
			t.Errorf("departure %d: status %+v setpoint %v", dep, statusOf(p, "B1"), sp(p, "C1"))
		}
	}
	// Arrives at minute 30 (clock 20:30, the peak ends in 30 min) and leaves at 80.
	b := testBus("S", "C1", 100, 200, 80)
	b.ArrivalMin = 30
	in := costInput(1000, []model.Charger{slowCharger("C1")}, b)
	in.Now = 30
	if p := PlanNormal(costConfig(), in); isDeferred(statusOf(p, "S")) || sp(p, "C1") <= 0 {
		t.Errorf("short stay: status %+v setpoint %v", statusOf(p, "S"), sp(p, "C1"))
	}
}

// The test is (availMin - tp - margin) * maxKW / 60 >= gridNeed with >=: 100 kWh at
// 100 kW, tp = 60, margin 45: 165 min is exactly enough (60 * 100 / 60 = 100), 164 is not.
func TestCostDeferralBoundaryIsInclusive(t *testing.T) {
	for dep, want := range map[int]bool{165: true, 164: false} {
		p := PlanNormal(costConfig(), costInput(1000, []model.Charger{slowCharger("C1")}, testBus("B1", "C1", 100, 200, dep)))
		if got := isDeferred(statusOf(p, "B1")); got != want {
			t.Errorf("departure %d: deferred = %v, want %v", dep, got, want)
		}
	}
}

// Without the schedule (ScheduleKnown false, the default) the policy does nothing, however
// much slack the buses have and however many chargers are free.
func TestCostDoesNothingWithoutTheSchedule(t *testing.T) {
	chargers := []model.Charger{slowCharger("C1"), slowCharger("C2"), slowCharger("C3"), slowCharger("C4"), slowCharger("C5")}
	in := costInput(4000, chargers, testBus("B1", "C1", 100, 200, 600))
	if p := PlanNormal(blindConfig(), in); !reflect.DeepEqual(p, PlanNormal(testConfig(), in)) {
		t.Errorf("blind: the plan must be the plain one, got %+v", p.Buses)
	}
	if p := PlanNormal(costConfig(), in); !isDeferred(statusOf(p, "B1")) {
		t.Errorf("schedule known: B1 must be deferred, got %+v", statusOf(p, "B1"))
	}
}

// The plug guard: buses present plus buses announced (ArrivalMin > Now) must not exceed
// the healthy chargers. A charger that fails stops deferral. Without the schedule nothing
// is ever deferred.
func TestCostPlugGuard(t *testing.T) {
	chargers := func(n int) []model.Charger {
		var out []model.Charger
		for i := 0; i < n; i++ {
			out = append(out, slowCharger(fmt.Sprintf("C%d", i+1)))
		}
		return out
	}
	buses := func(n int) []model.Bus {
		var out []model.Bus
		for i := 0; i < n; i++ {
			out = append(out, testBus(fmt.Sprintf("B%d", i+1), fmt.Sprintf("C%d", i+1), 100, 200, 600))
		}
		return out
	}
	deferredAny := func(cfg Config, in Input) bool {
		for _, st := range PlanNormal(cfg, in).Buses {
			if isDeferred(st) {
				return true
			}
		}
		return false
	}
	announced := testBus("F", "", 100, 200, 600)
	announced.ArrivalMin = 30
	cases := []struct {
		name             string
		cfg              Config
		nChargers, nBus  int
		announce         bool
		failFirstCharger bool
		want             bool
	}{
		{"known, one plug per bus", costConfig(), 4, 4, false, false, true},
		{"known, a free plug too", costConfig(), 5, 4, false, false, true},
		{"known, an announced bus has no plug", costConfig(), 4, 4, true, false, false},
		{"known, an announced bus has a free plug", costConfig(), 5, 4, true, false, true},
		{"known, a charger fails", costConfig(), 4, 4, false, true, false},
		{"blind, a plug for every bus", blindConfig(), 8, 4, false, false, false},
		{"blind, an announced bus too", blindConfig(), 8, 4, true, false, false},
	}
	for _, c := range cases {
		in := costInput(4000, chargers(c.nChargers), buses(c.nBus)...)
		if c.announce {
			in.Buses = append(in.Buses, announced)
		}
		if c.failFirstCharger {
			in.Chargers[c.nChargers-1].Status = model.ChargerFaulted // a free one: no bus is on it
			in.Chargers[0].Status = model.ChargerFaulted
		}
		if got := deferredAny(c.cfg, in); got != c.want {
			t.Errorf("%s: deferral = %v, want %v", c.name, got, c.want)
		}
	}
}

// Deferral that cannot lower the power drawn in the peak is pointless: when the buses
// that stay on charge could absorb the whole budget anyway nothing is deferred.
func TestCostDoesNotDeferWhenTheOthersAbsorbTheBudget(t *testing.T) {
	slack := testBus("S", "C1", 100, 200, 600)
	tight := testBus("T", "C2", 50, 200, 180)
	in := costInput(100, []model.Charger{slowCharger("C1"), slowCharger("C2")}, slack, tight) // T alone can take all 100 kW
	cfg := costConfig()
	if p := PlanNormal(cfg, in); !reflect.DeepEqual(p, PlanNormal(testConfig(), in)) {
		t.Errorf("T absorbs the whole 100 kW: the plan must be the plain one, got %+v", p.Buses)
	}
	// With a bigger budget T cannot absorb it all: S is deferred.
	in.Site.LimitKW = 400
	if p := PlanNormal(cfg, in); !isDeferred(statusOf(p, "S")) {
		t.Errorf("S must be deferred when T cannot absorb the budget: %+v", statusOf(p, "S"))
	}
}

// The Planner may reuse the last valid plan for LastPlanTTLMin minutes when the normal
// layer fails: a deferred (0 kW) plan replays as such and stays within the limit.
func TestCostLastValidReuseOfADeferredPlan(t *testing.T) {
	cfg := costConfig()
	calls := 0
	pl := New(cfg).WithNormal(func(c Config, in Input) Plan {
		calls++
		if calls > 1 {
			panic("normal layer down")
		}
		return PlanNormal(c, in)
	})
	in := costInput(1000, []model.Charger{slowCharger("C1")}, testBus("B1", "C1", 100, 200, 600))
	first := pl.Plan(in)
	if first.Layer != LayerNormal || sp(first, "C1") != 0 || !isDeferred(statusOf(first, "B1")) {
		t.Fatalf("first plan: %+v", first)
	}
	in.Now = 1
	second := pl.Plan(in)
	if second.Layer != LayerLastValid || sp(second, "C1") != 0 {
		t.Errorf("second plan: layer %v setpoint %v, want last-valid and 0", second.Layer, sp(second, "C1"))
	}
	if v := Violations(in, second); len(v) > 0 {
		t.Errorf("violations: %v", v)
	}
}

// --- demand cap (CostPolicy.DemandTargetKW) ---

// capConfig is a policy with only a demand target: no peak window, no schedule, so the
// only lever on is the cap.
func capConfig(target float64) Config {
	c := testConfig()
	c.Cost = &CostPolicy{DemandTargetKW: target}
	return c
}

func totalKW(p Plan) float64 {
	s := 0.0
	for _, sp := range p.Setpoints {
		s += sp.KW
	}
	return s
}

const (
	noteCapped   = "demanda limitada ao alvo"
	noteNoFit    = "alvo de demanda não cabe: usando o limite"
	noteWaiting  = "alvo de demanda em espera: há ônibus"
	noteUnknown  = "alvo de demanda em espera: agenda"
	slackBuses   = 4
	slackBusKWh  = 100 // battery kWh each slack bus needs
	slackBusMins = 600 // minutes to its departure: 10 kW would do
)

// slackDepot has four buses that each need 100 kWh with 10 hours to go: the whole
// depot needs 40 kW, though the chargers could draw 600 kW and the limit is 1000.
func slackDepot() Input {
	var ch []model.Charger
	var buses []model.Bus
	for i := 0; i < slackBuses; i++ {
		id := fmt.Sprintf("C%d", i+1)
		ch = append(ch, testCharger(id))
		buses = append(buses, testBus(fmt.Sprintf("B%d", i+1), id, 100, 200, slackBusMins))
	}
	return costInput(1000, ch, buses...)
}

// tightDepot has four buses that each need 100 kWh in 60 minutes: 100 kW each, 400 kW in
// all, which a 200 kW target cannot give but the 1000 kW limit can.
func tightDepot() Input {
	var ch []model.Charger
	var buses []model.Bus
	for i := 0; i < 4; i++ {
		id := fmt.Sprintf("C%d", i+1)
		ch = append(ch, testCharger(id))
		buses = append(buses, testBus(fmt.Sprintf("B%d", i+1), id, 100, 200, 60))
	}
	return costInput(1000, ch, buses...)
}

// (a) A target above the limit, or equal to it, changes nothing, field by field.
func TestDemandTargetAboveTheLimitIsTheSamePlan(t *testing.T) {
	for _, in := range []Input{slackDepot(), tightDepot()} {
		plain := PlanNormal(testConfig(), in)
		for _, target := range []float64{in.Site.LimitKW, in.Site.LimitKW + 1, 5000} {
			if got := PlanNormal(capConfig(target), in); !reflect.DeepEqual(got, plain) {
				t.Errorf("target %v with limit %v: plan differs\n got  %+v\n want %+v", target, in.Site.LimitKW, got, plain)
			}
		}
	}
	// Offline chargers shrink the budget below the limit: the target is compared with the budget.
	in := slackDepot()
	in.Chargers[0].Status = model.ChargerOffline
	in.Chargers[0].LastCommandedKW = 300 // budget 700
	if got, want := PlanNormal(capConfig(700), in), PlanNormal(testConfig(), in); !reflect.DeepEqual(got, want) {
		t.Errorf("target equal to the budget (limit minus offline power): plan differs\n got  %+v\n want %+v", got, want)
	}
}

// (b) With room to spare the setpoints add up to no more than the target (and use it:
// the plain plan spends the spare power too), every bus still reaches its target.
func TestDemandTargetCapsASlackDepot(t *testing.T) {
	in := slackDepot()
	plain := PlanNormal(testConfig(), in)
	if totalKW(plain) <= 200 {
		t.Fatalf("test setup: the plain plan draws only %v kW", totalKW(plain))
	}
	for _, target := range []float64{60, 120, 200, 400} {
		p := PlanNormal(capConfig(target), in)
		if got := totalKW(p); got > target+1e-9 {
			t.Errorf("target %v: setpoints add up to %v", target, got)
		}
		if !hasNote(p, noteCapped) || hasNote(p, noteNoFit) {
			t.Errorf("target %v: notes %q, want %q", target, p.Notes, noteCapped)
		}
		for _, st := range p.Buses {
			if !st.WillReachTarget {
				t.Errorf("target %v: bus %s does not reach its target: %+v", target, st.BusID, st)
			}
		}
		if v := Violations(in, p); len(v) > 0 {
			t.Errorf("target %v: %v", target, v)
		}
		if enforced := Enforce(in, p); !reflect.DeepEqual(enforced, p) {
			t.Errorf("target %v: Enforce changed the plan (the limit, not the target, is its bound)", target)
		}
	}
}

// (c) A target the buses cannot live with: the capped plan would leave buses short that
// the limit saves, so the plan is the plain one and may pass the target.
func TestDemandTargetGivesWayWhenTheNeedIsHigher(t *testing.T) {
	in := tightDepot()
	plain := PlanNormal(testConfig(), in)
	got := PlanNormal(capConfig(200), in)
	if totalKW(got) <= 200 {
		t.Fatalf("total %v kW: the capped plan was used, but 4 buses of 100 kW need 400", totalKW(got))
	}
	if !hasNote(got, noteNoFit) || hasNote(got, noteCapped) {
		t.Errorf("notes %q, want %q", got.Notes, noteNoFit)
	}
	for _, st := range got.Buses {
		if !st.WillReachTarget {
			t.Errorf("bus %s does not reach its target: %+v", st.BusID, st)
		}
	}
	got.Notes = nil
	if !reflect.DeepEqual(got, plain) {
		t.Errorf("fallback plan differs from the plain one\n got  %+v\n want %+v", got, plain)
	}
	if v := Violations(in, PlanNormal(capConfig(200), in)); len(v) > 0 {
		t.Errorf("violations: %v", v)
	}
	// A target that does fit the whole need (with the reserve) keeps the capped plan.
	p := PlanNormal(capConfig(600), in)
	if !hasNote(p, noteCapped) || totalKW(p) > 600+1e-9 {
		t.Errorf("target 600: notes %q, total %v", p.Notes, totalKW(p))
	}
}

// (d) A zero target is off: same plan, no note. Negative, NaN and infinite are an invalid
// policy (see TestCostInvalidPolicyIsIgnoredWithANote).
func TestDemandTargetZeroIsOff(t *testing.T) {
	for _, in := range []Input{slackDepot(), tightDepot()} {
		if got, want := PlanNormal(capConfig(0), in), PlanNormal(testConfig(), in); !reflect.DeepEqual(got, want) {
			t.Errorf("target 0: plan differs\n got  %+v\n want %+v", got, want)
		}
	}
	// A nil policy and a policy without a target are the same thing for this lever.
	in := slackDepot()
	if got, want := PlanNormal(blindConfig(), in), PlanNormal(testConfig(), in); !reflect.DeepEqual(got, want) {
		t.Errorf("policy without a target changed the plan")
	}
}

// A bus that even the limit cannot save (negative laxity) must not make the cap give way:
// the full budget would not save it either. The savable buses still fit under the target.
func TestDemandTargetIgnoresBusesTheLimitCannotSave(t *testing.T) {
	in := slackDepot()
	in.Buses = append(in.Buses, testBus("DOOMED", "C9", 0, 290, 30)) // 290 kWh in 30 min on 150 kW
	in.Chargers = append(in.Chargers, testCharger("C9"))
	p := PlanNormal(capConfig(300), in)
	if !hasNote(p, noteCapped) || hasNote(p, noteNoFit) {
		t.Errorf("notes %q, want %q only", p.Notes, noteCapped)
	}
	if totalKW(p) > 300+1e-9 {
		t.Errorf("total %v kW above the target", totalKW(p))
	}
	// With a target that starves it of the leftover power it gets under the limit, the
	// doomed bus does count: the planner's estimate is pessimistic and the bus may still
	// finish on that power (it did in the simulator), so the limit is used.
	p = PlanNormal(capConfig(130), in)
	if !hasNote(p, noteNoFit) || sp(p, "C9") != 150 {
		t.Errorf("target 130: notes %q, doomed bus at %v kW, want the limit and 150 kW", p.Notes, sp(p, "C9"))
	}
}

// A bus the cap would delay is saved at the full limit only; one that the full limit does
// not save either does not count. Here the second bus is short in both plans.
func TestDemandTargetOnlyBusesTheLimitSavesForceTheFallback(t *testing.T) {
	// Budget 100 kW at the limit; two buses of 100 kW need each; target 60.
	// B1 (earlier deadline) is saved at 100, B2 is not (100 kW is shared in time but the
	// admit test leaves one out). Capped, B1 is not saved at 60 kW: so the limit is used.
	ch := []model.Charger{slowCharger("C1"), slowCharger("C2")}
	b1 := testBus("B1", "C1", 100, 200, 60) // 100 kWh in 60 min: 100 kW
	b2 := testBus("B2", "C2", 100, 200, 60)
	in := costInput(100, ch, b1, b2)
	p := PlanNormal(capConfig(60), in)
	if !hasNote(p, noteNoFit) {
		t.Errorf("notes %q, want %q: the limit saves B1", p.Notes, noteNoFit)
	}
	if !statusOf(p, "B1").WillReachTarget && !statusOf(p, "B2").WillReachTarget {
		t.Errorf("one of the buses must be saved: %+v", p.Buses)
	}
}

// The cap works without the schedule and with it, and composes with the deferral: the
// target applies to the budget the deferral works with, a deferred bus stays at 0 kW.
func TestDemandTargetComposesWithDeferral(t *testing.T) {
	slack := testBus("S", "C1", 100, 200, 600)
	tight := testBus("T", "C2", 50, 200, 180)
	ch := []model.Charger{slowCharger("C1"), slowCharger("C2")}
	in := costInput(1000, ch, slack, tight)

	cfg := costConfig()
	pol := *cfg.Cost
	pol.DemandTargetKW = 150
	cfg.Cost = &pol
	p := PlanNormal(cfg, in)
	if !isDeferred(statusOf(p, "S")) || sp(p, "C1") != 0 {
		t.Errorf("S must still be deferred under the cap: %+v setpoint %v", statusOf(p, "S"), sp(p, "C1"))
	}
	if !statusOf(p, "T").WillReachTarget || totalKW(p) > 150+1e-9 || !hasNote(p, noteCapped) {
		t.Errorf("T: %+v total %v notes %q", statusOf(p, "T"), totalKW(p), p.Notes)
	}

	// Target 100 = what T alone can absorb: the saturation guard (applied to the capped
	// budget) keeps S on charge, and the total stays within the target.
	pol.DemandTargetKW = 100
	cfg.Cost = &pol
	p = PlanNormal(cfg, in)
	if isDeferred(statusOf(p, "S")) || totalKW(p) > 100+1e-9 || !statusOf(p, "S").WillReachTarget || !statusOf(p, "T").WillReachTarget {
		t.Errorf("target 100: S %+v T %+v total %v", statusOf(p, "S"), statusOf(p, "T"), totalKW(p))
	}

	// Without the schedule the deferral is off and the cap still acts.
	pol.DemandTargetKW = 150
	pol.ScheduleKnown = false
	cfg.Cost = &pol
	p = PlanNormal(cfg, in)
	if isDeferred(statusOf(p, "S")) || totalKW(p) > 150+1e-9 || !hasNote(p, noteCapped) {
		t.Errorf("blind: S %+v total %v notes %q", statusOf(p, "S"), totalKW(p), p.Notes)
	}
}

// The cap does not need a plug for every bus (it works with fewer chargers than buses),
// but it gives way while a savable bus waits for a plug: slower charging keeps the plugs
// busy longer and the waiting bus would start later. A waiting bus that nothing can save
// does not count.
func TestDemandTargetWithScarcePlugs(t *testing.T) {
	in := slackDepot()
	in.Buses = append(in.Buses, testBus("W", "", 100, 200, slackBusMins)) // waiting, savable
	p := PlanNormal(capConfig(100), in)
	if !hasNote(p, noteWaiting) || hasNote(p, noteCapped) || !reflect.DeepEqual(p.Setpoints, PlanNormal(testConfig(), in).Setpoints) {
		t.Errorf("savable waiting bus: total %v notes %q, want the plain setpoints and %q", totalKW(p), p.Notes, noteWaiting)
	}
	in = slackDepot()
	in.Buses = append(in.Buses, testBus("W", "", 0, 290, 30)) // waiting, 290 kWh in 30 min: lost anyway
	p = PlanNormal(capConfig(100), in)
	if totalKW(p) > 100+1e-9 || !hasNote(p, noteCapped) {
		t.Errorf("doomed waiting bus: total %v notes %q", totalKW(p), p.Notes)
	}
	// Fewer chargers than buses, none waiting (the extra bus has left or is full): capped.
	in = slackDepot()
	in.Buses = append(in.Buses, testBus("F", "", 200, 200, slackBusMins)) // waiting but needs nothing
	if p = PlanNormal(capConfig(100), in); totalKW(p) > 100+1e-9 || !hasNote(p, noteCapped) {
		t.Errorf("full waiting bus: total %v notes %q", totalKW(p), p.Notes)
	}
}

// A cap never makes a plan invalid or raises the draw: 300 random inputs, with and
// without the deferral. The result is either under the target (and lists the note) or the
// plain plan, and every viable reliable bus the plain plan saves is saved.
func TestDemandTargetPropertyReadinessFirst(t *testing.T) {
	rng := rand.New(rand.NewSource(20261012))
	capped, fellBack := 0, 0
	for i := 0; i < 300; i++ {
		in := randomInput(rng, rng.Intn(50)) // clock 1200..1249: inside the peak
		if i%3 != 0 {
			for j := range in.Buses { // readings the cap can trust
				in.Buses[j].SoCAgeMin, in.Buses[j].SoCConfidence = 0, 1
			}
		}
		base := costConfig()
		if i%2 == 1 {
			base = testConfig()
		}
		plain := PlanNormal(base, in)
		cfg := base
		pol := CostPolicy{ClockAtZeroMin: 1200, PeakFromMin: 1080, PeakToMin: 1260, ScheduleKnown: i%2 == 0}
		if base.Cost != nil {
			pol = *base.Cost
		}
		pol.DemandTargetKW = math.Round(AvailableKW(in) * (0.2 + 0.7*rng.Float64()))
		cfg.Cost = &pol
		got := PlanNormal(cfg, in)
		if v := Violations(in, got); len(v) > 0 {
			t.Fatalf("input %d: %v", i, v)
		}
		if enforced := Enforce(in, got); !reflect.DeepEqual(enforced, got) {
			t.Fatalf("input %d: Enforce changed the plan", i)
		}
		if v := Violations(in, New(cfg).Plan(in)); len(v) > 0 {
			t.Fatalf("input %d: Planner plan: %v", i, v)
		}
		switch {
		case hasNote(got, noteNoFit) || hasNote(got, "alvo de demanda em espera"):
			fellBack++
			got.Notes = nil
			plain.Notes = nil
			if !reflect.DeepEqual(got, plain) {
				t.Fatalf("input %d: the fallback plan is not the plain plan\n got  %+v\n want %+v", i, got, plain)
			}
		case hasNote(got, noteCapped):
			capped++
			if totalKW(got) > pol.DemandTargetKW+1e-9 {
				t.Fatalf("input %d: total %v above the target %v", i, totalKW(got), pol.DemandTargetKW)
			}
		default:
			// Nothing to cap (no charging at all), or target at or above the budget.
			if !reflect.DeepEqual(got, plain) && totalKW(got) > 1e-9 {
				t.Fatalf("input %d: no cap note but the plan differs from the plain one", i)
			}
		}
		// Readiness first: no viable reliable bus that the plain plan saves is lost.
		for _, ps := range plain.Buses {
			if !ps.Assessed || !ps.WillReachTarget || isDeferred(ps) {
				continue
			}
			if gs := statusOf(got, ps.BusID); !gs.WillReachTarget {
				t.Fatalf("input %d: bus %s saved by the plain plan but not with a target: %+v", i, ps.BusID, gs)
			}
		}
	}
	if capped < 30 || fellBack < 30 {
		t.Errorf("capped %d, fell back %d of 300: the property would prove little", capped, fellBack)
	}
}

// (f) The three layers of Planner keep working with the cap on: the normal layer, the
// last valid plan (capped, replayed) and the safe profile.
func TestDemandTargetThreeLayers(t *testing.T) {
	cfg := capConfig(120)
	calls := 0
	pl := New(cfg).WithNormal(func(c Config, in Input) Plan {
		calls++
		if calls > 1 {
			panic("normal layer down")
		}
		return PlanNormal(c, in)
	})
	in := slackDepot()
	first := pl.Plan(in)
	if first.Layer != LayerNormal || totalKW(first) > 120+1e-9 || totalKW(first) == 0 || !hasNote(first, noteCapped) {
		t.Fatalf("first plan: layer %v total %v notes %q", first.Layer, totalKW(first), first.Notes)
	}
	in.Now = 1
	second := pl.Plan(in)
	if second.Layer != LayerLastValid || totalKW(second) > 120+1e-9 || totalKW(second) == 0 {
		t.Errorf("second plan: layer %v total %v", second.Layer, totalKW(second))
	}
	if v := Violations(in, second); len(v) > 0 {
		t.Errorf("violations: %v", v)
	}
	// Past the TTL the safe profile takes over and respects the limit.
	in.Now = cfg.LastPlanTTLMin + 2
	third := pl.Plan(in)
	if third.Layer != LayerSafe {
		t.Errorf("third plan layer %v, want safe", third.Layer)
	}
	if v := Violations(in, third); len(v) > 0 {
		t.Errorf("violations: %v", v)
	}
	// Mostly unreliable readings: the safe profile again, with the policy on.
	bad := slackDepot()
	for i := range bad.Buses {
		bad.Buses[i].SoCConfidence = 0.1
	}
	if p := New(cfg).Plan(bad); p.Layer != LayerSafe {
		t.Errorf("unreliable readings: layer %v, want safe", p.Layer)
	}
	// A planner with the cap and a healthy normal layer stays in the normal layer.
	if p := New(cfg).Plan(slackDepot()); p.Layer != LayerNormal {
		t.Errorf("layer %v, want normal", p.Layer)
	}
}

// With the schedule known the cap sees the buses still to arrive: a target that carries
// the buses present but not the announced ones is dropped now, not when they arrive.
func TestDemandTargetCountsAnnouncedBuses(t *testing.T) {
	cfg := capConfig(120)
	pol := *cfg.Cost
	pol.ScheduleKnown = true
	cfg.Cost = &pol
	in := slackDepot() // 40 kW of need under a 120 kW target (102 with the reserve)
	if p := PlanNormal(cfg, in); !hasNote(p, noteCapped) || totalKW(p) > 120+1e-9 {
		t.Fatalf("nothing announced: notes %q total %v", p.Notes, totalKW(p))
	}
	// Two buses announced for minute 60 that need 100 kWh each by minute 120: 200 kWh in
	// the hour they are on charge is 200 kW, more than the target gives.
	for _, id := range []string{"A1", "A2"} {
		b := testBus(id, "", 100, 200, 120)
		b.ArrivalMin = 60
		in.Buses = append(in.Buses, b)
	}
	in.Chargers = append(in.Chargers, testCharger("C5"), testCharger("C6"))
	if p := PlanNormal(cfg, in); !hasNote(p, noteNoFit) || hasNote(p, noteCapped) {
		t.Errorf("announced buses do not fit under the target: notes %q", p.Notes)
	}
	// Without the schedule they are invisible, and the free chargers keep the cap waiting.
	pol.ScheduleKnown = false
	cfg.Cost = &pol
	if p := PlanNormal(cfg, in); !hasNote(p, noteUnknown) {
		t.Errorf("no schedule and free chargers: notes %q, want %q", p.Notes, noteUnknown)
	}
}

// With the schedule known only an urgent waiting bus makes the cap give way; without it
// any savable waiting bus does (see TestDemandTargetWithScarcePlugs).
func TestDemandTargetWaitingBusUrgency(t *testing.T) {
	cfg := capConfig(120)
	pol := *cfg.Cost
	pol.ScheduleKnown = true
	cfg.Cost = &pol
	in := slackDepot()
	in.Buses = append(in.Buses, testBus("W", "", 100, 200, slackBusMins)) // 100 kWh, 600 min: laxity 540 on 150 kW
	if p := PlanNormal(cfg, in); !hasNote(p, noteCapped) || totalKW(p) > 120+1e-9 {
		t.Errorf("relaxed waiting bus: notes %q total %v, want the cap", p.Notes, totalKW(p))
	}
	// 100 kWh at 150 kW takes 40 minutes: departing at minute 99 leaves 59 of laxity.
	in.Buses[len(in.Buses)-1] = testBus("W", "", 100, 200, 99)
	if p := PlanNormal(cfg, in); !hasNote(p, noteWaiting) {
		t.Errorf("urgent waiting bus: notes %q, want %q", p.Notes, noteWaiting)
	}
}

// A bus with an unreliable SoC reading makes the cap give way: its need is a guess.
func TestDemandTargetGivesWayOnUnreliableReadings(t *testing.T) {
	in := slackDepot()
	if p := PlanNormal(capConfig(120), in); !hasNote(p, noteCapped) {
		t.Fatalf("reliable readings: notes %q", p.Notes)
	}
	in.Buses[0].SoCAgeMin = 100 // stale
	p := PlanNormal(capConfig(120), in)
	if !hasNote(p, "alvo de demanda em espera: leitura") || totalKW(p) <= 120 {
		t.Errorf("stale reading: notes %q total %v", p.Notes, totalKW(p))
	}
}
