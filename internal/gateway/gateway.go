// Package gateway is the boundary between the planner and physical (or simulated) chargers.
package gateway

import "github.com/EduardoMilani8/depot-charge-planner/internal/model"

// Gateway applies plans to chargers.
//
// A planner.Plan lists setpoints only for some chargers; every charger not listed
// must be at 0 kW. Implementations that apply a plan (e.g. the future OCPP adapter)
// must switch unlisted chargers off explicitly — clear any charging profile left from
// a previous plan — instead of leaving them at their last power.
type Gateway interface {
	// Chargers returns every charger as the controller observes it.
	Chargers() []model.Charger
	// SetPower commands a charger's grid-side power in kW; it takes effect on the next step.
	SetPower(chargerID string, kw float64) error
}
