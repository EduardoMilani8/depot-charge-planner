import test from 'node:test';
import assert from 'node:assert/strict';
import {
  COST_KEYS, normalizeCostParams, costActive, hoursToMin, minToHours, hasBill, savingsSentence, withoutTarget,
  targetComparison, needsTargetComparison, hoursMessage, billSummary, peakBand, isDeferredReason, deferredWindows, describeCost,
} from '../static/js/cost.js';
import { fmtBRL2 } from '../static/js/format.js';
import { COLUMNS, BILL_COLUMNS, columnsFor, formatCell, describeParams, COST_TERMS } from '../static/js/glossary.js';

test('fmtBRL2: pt-BR currency with two decimals', () => {
  assert.equal(fmtBRL2(1234.5), 'R$ 1.234,50');
  assert.equal(fmtBRL2(0), 'R$ 0,00');
  assert.equal(fmtBRL2(419971.004), 'R$ 419.971,00');
  assert.equal(fmtBRL2(1234567.891), 'R$ 1.234.567,89');
  assert.equal(fmtBRL2(-50), '-R$ 50,00');
  assert.equal(fmtBRL2(-0.001), 'R$ 0,00');
  assert.equal(fmtBRL2(0.125), 'R$ 0,12'); // an exact tie goes to the even digit, like Go's %.2f
  assert.equal(fmtBRL2(NaN), '—');
  assert.equal(fmtBRL2(undefined), '—');
});

test('arrival hours convert to minutes since 18:00 and back', () => {
  assert.equal(hoursToMin(2), 120);
  assert.equal(hoursToMin(2.5), 150);
  assert.equal(hoursToMin(0.0166), 1);
  assert.equal(minToHours(120), 2);
  assert.equal(minToHours(150), 2.5);
  assert.equal(minToHours(1), 0.02);
});

test('normalizeCostParams keeps only the cost keys that differ from their defaults', () => {
  const p = { buses: 5, cost_aware: false, defer_margin_min: 0, demand_target_kw: 0, demand_peak_price: 0, demand_verde: false, arrival_from_min: 0 };
  assert.deepEqual(normalizeCostParams(p), { buses: 5 });
  assert.deepEqual(normalizeCostParams({ buses: 5, demand_peak_price: 40, nights_per_month: 22, demand_verde: true }),
    { buses: 5, demand_peak_price: 40, nights_per_month: 22, demand_verde: true });
  assert.deepEqual(normalizeCostParams({ cost_aware: true, defer_margin_min: 30, demand_target_kw: 1600 }),
    { cost_aware: true, defer_margin_min: 30, demand_target_kw: 1600 });
});

test('the margin and the demand target are dropped when cost_aware is off (the server rejects them)', () => {
  assert.deepEqual(normalizeCostParams({ defer_margin_min: 30, demand_target_kw: 1600, demand_peak_price: 40 }), { demand_peak_price: 40 });
  assert.deepEqual(normalizeCostParams({ cost_aware: false, defer_margin_min: 30, demand_target_kw: 1600 }), {});
});

test('normalizeCostParams does not change its input and drops NaN', () => {
  const p = { cost_aware: false, defer_margin_min: 30, nights_per_month: NaN };
  const copy = { ...p };
  assert.deepEqual(normalizeCostParams(p), {});
  assert.deepEqual(p, copy);
});

test('COST_KEYS lists the ten new parameters', () => {
  assert.equal(COST_KEYS.length, 10);
  assert.ok(COST_KEYS.includes('cost_aware') && COST_KEYS.includes('arrival_to_min') && COST_KEYS.includes('nights_per_month'));
});

test('costActive: any cost or demand setting', () => {
  assert.equal(costActive({ buses: 5 }), false);
  assert.equal(costActive({ cost_aware: true }), true);
  assert.equal(costActive({ demand_offpeak_price: 20 }), true);
  assert.equal(costActive({ demand_contracted_kw: 1500 }), true);
  assert.equal(costActive({ arrival_to_min: 180 }), false);
});

const bill = (monthly, demand = 2000, demandBRL = 40000) => ({ demand_kw: demand, demand_brl: demandBRL, monthly_brl: monthly });
const ctl = (name, monthly, ready = 90) => ({ name, aggregate: monthly === undefined ? { ready_pct: ready } : { ready_pct: ready, bill: bill(monthly) } });

test('hasBill: only when a controller carries a bill', () => {
  assert.equal(hasBill({ controllers: [ctl('fifo'), ctl('planner')] }), false);
  assert.equal(hasBill({ controllers: [ctl('fifo', 100), ctl('planner', 90)] }), true);
  assert.equal(hasBill({ controllers: [] }), false);
});

const SAVINGS_TAIL = ' Compare com a porcentagem de ônibus prontos: um fifo que deixa ônibus sem carga gasta menos, então a comparação não é equivalente.';

test('savingsSentence: all savings keep the "Economia" lead, in pt-BR with two decimals and the word estimativa', () => {
  const s = savingsSentence([ctl('fifo', 676578.5), ctl('fifo-unplug', 500000), ctl('planner', 415016.25)]);
  assert.equal(s, 'Economia mensal estimada do planner: R$ 261.562,25 contra fifo e R$ 84.983,75 contra fifo-unplug (estimativa: demanda de uma noite tratada como a do mês).');
  assert.match(s, /estimativa/);
});

test('savingsSentence: mixed signs get a neutral lead and one verb per clause', () => {
  const s = savingsSentence([ctl('fifo', 100), ctl('fifo-unplug', 3337.45 + 100), ctl('planner', 100 + 80076.67)]);
  assert.equal(s, 'Diferença mensal estimada do planner: custa R$ 80.076,67 a mais contra fifo e custa R$ 76.739,22 a mais contra fifo-unplug (estimativa: demanda de uma noite tratada como a do mês).');
  const mixed = savingsSentence([ctl('fifo', 100), ctl('fifo-unplug', 500), ctl('planner', 300)]);
  assert.equal(mixed, 'Diferença mensal estimada do planner: custa R$ 200,00 a mais contra fifo e economiza R$ 200,00 contra fifo-unplug (estimativa: demanda de uma noite tratada como a do mês).');
  assert.doesNotMatch(mixed, /^Economia/);
});

test('savingsSentence: a baseline that is cheaper is worded as a cost, not a negative saving', () => {
  const s = savingsSentence([ctl('fifo', 100), ctl('planner', 150)]);
  assert.equal(s, 'Diferença mensal estimada do planner: custa R$ 50,00 a mais contra fifo (estimativa: demanda de uma noite tratada como a do mês).');
});

test('savingsSentence: zero differences', () => {
  assert.equal(savingsSentence([ctl('fifo', 200), ctl('planner', 200)]),
    'Diferença mensal estimada do planner: não muda contra fifo (R$ 0,00) (estimativa: demanda de uma noite tratada como a do mês).');
  assert.match(savingsSentence([ctl('fifo', 300), ctl('fifo-unplug', 200), ctl('planner', 200)]), /economiza R\$ 100,00 contra fifo e não muda contra fifo-unplug/);
  assert.match(savingsSentence([ctl('fifo', 200.001), ctl('planner', 200)]), /não muda contra fifo/); // rounds to R$ 0,00
});

test('savingsSentence: one baseline, and no bill at all', () => {
  assert.match(savingsSentence([ctl('fifo', 300), ctl('planner', 200)]), /^Economia mensal estimada do planner: R\$ 100,00 contra fifo \(estimativa/);
  assert.equal(savingsSentence([ctl('fifo'), ctl('planner')]), null);
  assert.equal(savingsSentence([ctl('fifo', 100)]), null); // no planner
  assert.equal(savingsSentence([ctl('edf', 100), ctl('planner', 90)]), null); // no baseline of the two
  assert.equal(savingsSentence([]), null);
});

test('savingsSentence: the equivalence caveat only when the planner readies more buses than a baseline', () => {
  const unequal = savingsSentence([ctl('fifo', 300, 60), ctl('planner', 200, 100)]);
  assert.ok(unequal.endsWith(SAVINGS_TAIL));
  assert.doesNotMatch(savingsSentence([ctl('fifo', 300, 100), ctl('planner', 200, 100)]), /Compare com a porcentagem/);
  assert.doesNotMatch(savingsSentence([ctl('fifo', 300, 100), ctl('planner', 200, 90)]), /Compare com a porcentagem/); // planner readies fewer
  // one of two baselines is enough
  assert.ok(savingsSentence([ctl('fifo', 300, 100), ctl('fifo-unplug', 300, 50), ctl('planner', 200, 100)]).endsWith(SAVINGS_TAIL));
});

test('withoutTarget removes the demand target and keeps cost_aware', () => {
  const p = { buses: 5, cost_aware: true, demand_target_kw: 1600, demand_peak_price: 40 };
  assert.deepEqual(withoutTarget(p), { buses: 5, cost_aware: true, demand_peak_price: 40 });
  assert.equal('demand_target_kw' in p, true);
});

const cmp = (monthly, demand) => ({ controllers: [ctl('fifo', 900000), { name: 'planner', aggregate: { bill: bill(monthly, demand) } }] });

test('targetComparison: the bill went up with the target -> warning tone and the difference', () => {
  const r = targetComparison(cmp(419971, 1600), cmp(404882, 2000), 1600);
  assert.equal(r.tone, 'warn');
  assert.equal(r.text, 'Atenção: com a meta de demanda de 1.600 kW a fatura mensal estimada do planner sobe R$ 15.089,00 por mês '
    + '(R$ 419.971,00 com a meta contra R$ 404.882,00 sem ela, no mesmo modo consciente de custo; estimativa). '
    + 'Limitar a demanda não é, por si só, economia na fatura.');
});

test('targetComparison: the bill went down, or did not change', () => {
  const down = targetComparison(cmp(419970.5, 1600), cmp(435060, 2000), 1600);
  assert.equal(down.tone, 'info');
  assert.equal(down.text, 'Com a meta de demanda de 1.600 kW a fatura mensal estimada do planner cai R$ 15.089,50 por mês '
    + '(R$ 419.970,50 com a meta contra R$ 435.060,00 sem ela, no mesmo modo consciente de custo; estimativa).');
  const same = targetComparison(cmp(1000, 1600), cmp(1000.001, 1600), 1600);
  assert.equal(same.tone, 'info');
  assert.match(same.text, /não muda a fatura mensal estimada do planner/);
  assert.match(same.text, /estimativa/);
});

test('targetComparison: null when a side has no planner bill', () => {
  assert.equal(targetComparison({ controllers: [ctl('fifo', 1)] }, cmp(1, 1), 1600), null);
  assert.equal(targetComparison(cmp(1, 1), { controllers: [{ name: 'planner', aggregate: {} }] }, 1600), null);
  assert.equal(targetComparison(null, cmp(1, 1), 1600), null);
});

test('billSummary: demand, its monthly proxy and the estimated bill, beside each other', () => {
  assert.equal(billSummary({ bill: { demand_kw: 1600.4, demand_brl: 40000, monthly_brl: 419971 } }),
    'demanda 1.600 kW (R$ 40.000,00/mês, proxy) · fatura mensal estimada R$ 419.971,00 (estimativa)');
  assert.equal(billSummary({}), null);
  assert.equal(billSummary(undefined), null);
});

test('bill columns come right after the energy cost and before the plan changes', () => {
  const keys = (cols) => cols.map((c) => c.key);
  assert.deepEqual(keys(columnsFor({ controllers: [ctl('fifo'), ctl('planner')] })), keys(COLUMNS));
  const withBill = columnsFor({ controllers: [ctl('fifo', 1), ctl('planner', 1)] });
  const k = keys(withBill);
  assert.equal(k.length, COLUMNS.length + 3);
  const i = k.indexOf('cost_brl');
  assert.deepEqual(k.slice(i + 1, i + 4), ['bill_demand_kw', 'bill_demand_brl', 'bill_monthly_brl']);
  assert.equal(k[i + 4], 'plan_changes');
  assert.equal(k[k.length - 1], 'plan_p99_micros'); // p99 stays last
  assert.deepEqual(BILL_COLUMNS.map((c) => c.label), ['demanda kW', 'demanda R$/mês (proxy)', 'fatura mensal estimada R$']);
});

test('formatCell reads the bill columns from aggregate.bill', () => {
  const agg = { bill: { demand_kw: 1599.6, demand_brl: 40000.5, monthly_brl: 419971.255 } };
  const [kw, dem, month] = BILL_COLUMNS;
  assert.equal(formatCell(kw, agg), '1.600');
  assert.equal(formatCell(dem, agg), 'R$ 40.000,50');
  assert.equal(formatCell(month, agg), 'R$ 419.971,26'); // 419971.255 is not an exact tie in binary
  assert.equal(formatCell(kw, {}), '—');
  assert.equal(formatCell(month, {}), '—');
});

test('the glossary defines the cost terms', () => {
  const terms = COST_TERMS.map(([t]) => t).join('|');
  for (const w of ['bloco de 15 min', 'Azul e Verde', 'demanda contratada', 'ultrapassagem', 'proxy mensal', 'adiado']) assert.match(terms, new RegExp(w));
  for (const [, def] of COST_TERMS) assert.ok(def.length > 20);
});

test('describeParams mentions the cost settings only when set', () => {
  const P = { buses: 30, chargers: 12, limit_kw: 600, profile: 'none', seeds: 20, follow_swaps: true, reading_age_min: 0 };
  assert.equal(describeParams(P), '30 ônibus, 12 carregadores, limite 600 kW, sem falhas, operadores seguem os rodízios');
  const d = describeParams({ ...P, cost_aware: true, defer_margin_min: 30, demand_target_kw: 1600, demand_peak_price: 40, demand_offpeak_price: 20.5,
    demand_contracted_kw: 1500, arrival_from_min: 0, arrival_to_min: 180, nights_per_month: 22 });
  assert.match(d, /chegadas das 18:00 às 21:00/);
  assert.match(d, /planejador consciente de custo \(folga de 30 min, meta de demanda de 1\.600 kW\)/);
  assert.match(d, /demanda Azul: R\$ 40\/kW na ponta e R\$ 20,50\/kW fora dela/);
  assert.match(d, /contrato de 1\.500 kW/);
  assert.match(d, /22 noites por mês/);
  assert.match(describeParams({ ...P, demand_verde: true, demand_offpeak_price: 20 }), /demanda Verde: R\$ 20\/kW/);
  assert.deepEqual(describeCost({}), []);
});

test('peakBand: the 18:00-21:00 window as a hatched band, only for generated nights with cost settings', () => {
  const data = { params: { cost_aware: true }, scenario: { horizon_min: 600, start_clock_min: 1080 } };
  assert.deepEqual(peakBand(data), { from: 0, to: 180 });
  assert.equal(peakBand({ ...data, params: { buses: 3 } }), null); // default runs keep their look
  assert.equal(peakBand({ ...data, source: 'noite.csv' }), null); // an imported night has its own tariff
  assert.deepEqual(peakBand({ ...data, scenario: { horizon_min: 100, start_clock_min: 1080 } }), { from: 0, to: 100 });
  assert.deepEqual(peakBand({ ...data, scenario: { horizon_min: 600, start_clock_min: 1020 } }), { from: 60, to: 240 });
  assert.deepEqual(peakBand({ ...data, scenario: { horizon_min: 600, start_clock_min: 1140 } }), { from: 0, to: 120 }); // starts inside the peak: what is left of it
  assert.deepEqual(peakBand({ ...data, scenario: { horizon_min: 60, start_clock_min: 1140 } }), { from: 0, to: 60 });
  assert.equal(peakBand({ ...data, scenario: { horizon_min: 600, start_clock_min: 1260 } }), null); // starts after the peak and the next one is past the horizon
});

test('deferred buses: windows come from the planner reasons, which the server writes', () => {
  assert.equal(isDeferredReason('adiado para depois da ponta: termina com folga de 80 min'), true);
  assert.equal(isDeferredReason('alvo atingido'), false);
  assert.equal(isDeferredReason(undefined), false);
  const data = {
    scenario: { horizon_min: 100 },
    reasons: ['adiado para depois da ponta: termina com folga de 80 min', 'alvo atingido', 'adiado para depois da ponta: termina com folga de 70 min'],
    decisions: [
      { minute: 0, buses: [{ b: 'B1', r: 0 }, { b: 'B2', r: 1 }, { b: 'B3', r: 0 }], gone: [] },
      { minute: 10, buses: [{ b: 'B1', r: 2 }], gone: [] }, // still deferred, another folga
      { minute: 20, buses: [{ b: 'B1', r: 1 }], gone: ['B3'] }, // B1 starts charging, B3 left
      { minute: 30, buses: [{ b: 'B4', r: -1 }], gone: [] },
    ],
  };
  const w = deferredWindows(data);
  assert.deepEqual([...w.keys()].sort(), ['B1', 'B3']);
  assert.deepEqual(w.get('B1'), [{ from: 0, to: 19 }]);
  assert.deepEqual(w.get('B3'), [{ from: 0, to: 19 }]);
  assert.equal(deferredWindows({ scenario: { horizon_min: 5 }, reasons: [], decisions: [] }).size, 0);
  // a deferral still in force at the end runs to the horizon
  const open = deferredWindows({ scenario: { horizon_min: 50 }, reasons: ['adiado para depois da ponta: x'], decisions: [{ minute: 5, buses: [{ b: 'B1', r: 0 }], gone: [] }] });
  assert.deepEqual(open.get('B1'), [{ from: 5, to: 50 }]);
});

test('needsTargetComparison: only a target on a cost-aware planner with a bill', () => {
  const data = (params, withBill = true) => ({ params, controllers: [withBill ? ctl('planner', 1) : ctl('planner')] });
  assert.equal(needsTargetComparison(data({ cost_aware: true, demand_target_kw: 1600 })), true);
  assert.equal(needsTargetComparison(data({ cost_aware: true, demand_target_kw: 1600 }, false)), false); // no prices or contract: no bill
  assert.equal(needsTargetComparison(data({ cost_aware: true })), false);
  assert.equal(needsTargetComparison(data({ demand_target_kw: 1600 })), false);
  assert.equal(needsTargetComparison(null), false);
});

test('hoursMessage: the server speaks minutes since 18:00, the arrival fields are in hours', () => {
  assert.equal(hoursMessage('Início das chegadas: informe de 0 a 599 minutos desde as 18:00 (180 = 21:00).'),
    'Início das chegadas: informe de 0 a 9,98 horas depois das 18:00 (3 = 21:00).');
  assert.equal(hoursMessage('Fim das chegadas: informe um valor maior que o início e até 600 minutos desde as 18:00 (a janela deve terminar antes da primeira saída).'),
    'Fim das chegadas: informe um valor maior que o início e até 10 horas depois das 18:00 (a janela deve terminar antes da primeira saída).');
  assert.equal(hoursMessage('Informe um número.'), 'Informe um número.');
});
