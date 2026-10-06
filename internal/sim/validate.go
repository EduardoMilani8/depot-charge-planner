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
