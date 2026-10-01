// Package gateway is the boundary between the planner and physical (or simulated) chargers.
package gateway

import "github.com/EduardoMilani8/depot-charge-planner/internal/model"

type Gateway interface {
	// Chargers returns every charger as the controller observes it.
	Chargers() []model.Charger
	// SetPower commands a charger's grid-side power in kW; it takes effect on the next step.
	SetPower(chargerID string, kw float64) error
}
