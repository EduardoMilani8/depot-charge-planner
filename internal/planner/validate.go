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
