package main

import (
	"bytes"
	"strings"
	"testing"

	"github.com/EduardoMilani8/depot-charge-planner/internal/realdata"
	"github.com/EduardoMilani8/depot-charge-planner/internal/sim"
)

// The savings line of cmd/replay and of simrun must read the same. printBillNotes is
// duplicated on purpose (no shared package), so both packages pin the same literal for the
// same monthly figures; see cmd/simrun/billnotes_test.go (keep the two strings identical).
const savingsLineGain = "economia mensal estimada do planner (R$/mês, negativa = custa mais): contra fifo R$ 200 (20.0%), contra fifo-unplug R$ 100 (11.1%); compare com o ready%: um fifo que deixa ônibus sem carga gasta menos, então não é uma comparação equivalente"
const savingsLineLoss = "economia mensal estimada do planner (R$/mês, negativa = custa mais): contra fifo R$ -100 (-10.0%), contra fifo-unplug R$ -200 (-22.2%); compare com o ready%: um fifo que deixa ônibus sem carga gasta menos, então não é uma comparação equivalente"

func savingsLine(t *testing.T, fifo, unplug, planner float64) string {
	t.Helper()
	rep := &realdata.Report{DemandBilled: true, NightsPerMonth: 30}
	for _, c := range []struct {
		name string
		m    float64
	}{{"fifo", fifo}, {"fifo-unplug", unplug}, {"planner", planner}} {
		rep.Aggregate = append(rep.Aggregate, realdata.ControllerResult{Name: c.name, Aggregate: sim.Metrics{Bill: &sim.BillMetrics{MonthlyBRL: c.m}}})
	}
	var b bytes.Buffer
	printBillNotes(&b, rep)
	for _, l := range strings.Split(b.String(), "\n") {
		if strings.HasPrefix(l, "economia mensal") {
			return l
		}
	}
	t.Fatalf("no savings line in %q", b.String())
	return ""
}

func TestSavingsLineText(t *testing.T) {
	if got := savingsLine(t, 1000, 900, 800); got != savingsLineGain {
		t.Errorf("gain:\n%s\nwant\n%s", got, savingsLineGain)
	}
	if got := savingsLine(t, 1000, 900, 1100); got != savingsLineLoss {
		t.Errorf("loss:\n%s\nwant\n%s", got, savingsLineLoss)
	}
}
