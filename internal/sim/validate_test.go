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
