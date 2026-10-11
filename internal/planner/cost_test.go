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
	return &CostPolicy{ClockAtZeroMin: 1200, PeakFromMin: 1080, PeakToMin: 1260}
}

func costConfig() Config {
	c := testConfig()
	c.Cost = peakPolicy()
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

// Two deferrable buses that do not fit together after the peak: 400 kW site limit, 35 %
// of it (140 kW) counts for the test, 60 min usable after the peak (165 - 60 - 45) =
// 140 kWh. B1 needs 90 kWh and B2 80 kWh: both together (170) do not fit, the one
// needing more energy (B1) is the one that charges now, B2 is deferred.
func TestCostJointFeasibilityKeepsTheLargestSetThatFits(t *testing.T) {
	b1 := testBus("B1", "C1", 100, 190, 165) // 90 kWh
	b2 := testBus("B2", "C2", 100, 180, 165) // 80 kWh
	p := PlanNormal(costConfig(), costInput(400, []model.Charger{slowCharger("C1"), slowCharger("C2")}, b1, b2))
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
	p := PlanNormal(costConfig(), costInput(400, []model.Charger{slowCharger("C1"), slowCharger("C2")}, b1, b2))
	if sp(p, "C1") != 0 || sp(p, "C2") != 100 {
		t.Errorf("setpoints = %v / %v, want 0 / 100", sp(p, "C1"), sp(p, "C2"))
	}
	if !isDeferred(statusOf(p, "B1")) || isDeferred(statusOf(p, "B2")) {
		t.Errorf("B1 must be deferred and B2 not: %+v / %+v", statusOf(p, "B1"), statusOf(p, "B2"))
	}
}

// If the depot would be too busy after the peak (a waiting bus needs the budget too),
// nothing is deferred: 400 kW site, 140 kW counted, 60 min after the peak = 140 kWh;
// the waiting bus needs 90 kWh and would leave 50 kWh, less than the 80 kWh B1 needs.
func TestCostWaitingBusCompetesForThePostPeakBudget(t *testing.T) {
	b1 := testBus("B1", "C1", 100, 180, 165) // 80 kWh
	w := testBus("W", "", 100, 190, 165)     // 90 kWh, no charger
	in := costInput(400, []model.Charger{slowCharger("C1")}, b1, w)
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

// A free charger is expected to get a bus before the peak ends, with the mean energy of
// the buses present: 4 free chargers, each expected to need 80 kWh, do not fit in the
// 140 kWh left after the peak next to B1, so B1 charges now.
func TestCostFreeChargersReserveBudget(t *testing.T) {
	b1 := testBus("B1", "C1", 100, 180, 165) // 80 kWh
	chargers := []model.Charger{slowCharger("C1"), slowCharger("C2"), slowCharger("C3"), slowCharger("C4"), slowCharger("C5")}
	p := PlanNormal(costConfig(), costInput(400, chargers, b1))
	if isDeferred(statusOf(p, "B1")) {
		t.Errorf("B1 must charge now, the depot expects more buses: %+v", statusOf(p, "B1"))
	}
	// One free charger expects one more 80 kWh bus: 80 + 80 > 140 still; a large limit fits.
	if p := PlanNormal(costConfig(), costInput(4000, chargers, b1)); !isDeferred(statusOf(p, "B1")) {
		t.Errorf("with a 4000 kW limit B1 fits next to the expected buses and is deferred: %+v", statusOf(p, "B1"))
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
	in := costInput(1000, []model.Charger{faulted, offline},
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
	cfg.Cost = &CostPolicy{ClockAtZeroMin: 1259, PeakFromMin: 1080, PeakToMin: 1260}
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
	cfg.Cost = &CostPolicy{ClockAtZeroMin: 1380, PeakFromMin: 1320, PeakToMin: 120}
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
	cfg.Cost = &CostPolicy{ClockAtZeroMin: 1200, PeakFromMin: 1080, PeakToMin: 1260, DeferMarginMin: 60}
	if p := PlanNormal(cfg, in); isDeferred(statusOf(p, "B1")) { // (170-60-60)*100/60 = 83 < 100
		t.Errorf("margin 60: deferred: %+v", statusOf(p, "B1"))
	}
}

// Swaps are computed before the allocation, on copies: a deferred bus is not "full",
// so it is never offered as the donor of a swap, and the swap that frees a charger for
// a waiting bus is the same one as without the policy.
func TestCostSwapsIgnoreDeferredBuses(t *testing.T) {
	full := testBus("F", "C2", 250, 200, 600) // already above its target: the donor
	slack := testBus("S", "C1", 100, 200, 600)
	waiting := testBus("W", "", 100, 220, 200)
	in := costInput(1000, []model.Charger{slowCharger("C1"), slowCharger("C2")}, slack, full, waiting)
	with := PlanNormal(costConfig(), in)
	without := PlanNormal(testConfig(), in)
	if !isDeferred(statusOf(with, "S")) {
		t.Fatalf("S must be deferred: %+v", statusOf(with, "S"))
	}
	if len(without.Swaps) != 1 || without.Swaps[0].OutBusID != "F" {
		t.Fatalf("setup: swaps without the policy = %+v, want F giving way", without.Swaps)
	}
	if len(with.Swaps) != 1 || with.Swaps[0].OutBusID != "F" || with.Swaps[0].InBusID != "W" {
		t.Errorf("swaps with the policy = %+v, want F out and W in", with.Swaps)
	}
	for _, s := range with.Swaps {
		if s.OutBusID == "S" {
			t.Errorf("a deferred bus was offered as donor: %+v", s)
		}
	}
}

// randomInput builds a small depot: some buses connected, some waiting, a few faulty
// chargers and unreliable readings. Buses depart far enough after the 20:00 Now=0 of
// the policy tests to give some of them slack.
func randomInput(rng *rand.Rand, now int) Input {
	nb := 3 + rng.Intn(8)
	nc := nb - rng.Intn(3)
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
		if i < nc {
			b.ChargerID = in.Chargers[i].ID
		}
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
		"NaN departure": nil, // placeholder: Minute is an int, nothing to break
	} {
		if mutate == nil {
			continue
		}
		in := costInput(1000, []model.Charger{slowCharger("C1")}, testBus("B1", "C1", 100, 200, 600))
		mutate(&in)
		p := PlanNormal(costConfig(), in) // must not panic
		if st := statusOf(p, "B1"); isDeferred(st) {
			t.Errorf("%s: deferred on non-finite or absurd numbers: %+v", name, st)
		}
	}
}
