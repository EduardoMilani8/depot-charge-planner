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
