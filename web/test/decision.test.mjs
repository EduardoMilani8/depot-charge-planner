import test from 'node:test';
import assert from 'node:assert/strict';
import { decisionAt, statusesAt, decisionNotes, swapMarkers, chargerOccupancy, busExposure, explainBus, createSelection } from '../static/js/decision.js';

const decisions = [
  { minute: 0, layer: 'normal', swaps: [] },
  { minute: 10, layer: 'normal', swaps: [{ charger: 'C1', out: 'B1', in: 'B2', reason: 'troca' }] },
  { minute: 40, layer: 'safe', swaps: [] },
];

test('decisionAt finds the decision in force, its index and how long it lasts', () => {
  assert.deepEqual(decisionAt(decisions, 0, 100), { decision: decisions[0], index: 0, from: 0, to: 9 });
  assert.deepEqual(decisionAt(decisions, 25, 100), { decision: decisions[1], index: 1, from: 10, to: 39 });
  assert.deepEqual(decisionAt(decisions, 100, 100), { decision: decisions[2], index: 2, from: 40, to: 100 });
  assert.equal(decisionAt([], 5, 100), null);
  assert.equal(decisionAt([{ minute: 7 }], 3, 100), null);
});

test('swap markers give one entry per bus and role', () => {
  assert.deepEqual(swapMarkers(decisions), [
    { minute: 10, bus: 'B2', role: 'in', charger: 'C1', reason: 'troca' },
    { minute: 10, bus: 'B1', role: 'out', charger: 'C1', reason: 'troca' },
  ]);
});

// Deltas as the API sends them: the first decision lists every bus, later ones only what
// changed (sf is absent when 0) and `gone` names the buses that left the list.
const deltas = [
  { minute: 0, buses: [{ b: 'B3', ok: true, as: true, r: -1 }, { b: 'B1', ok: false, as: true, sf: 12.5, r: 0 }], gone: [] },
  { minute: 5, buses: [{ b: 'B2', ok: true, as: false, r: -1 }, { b: 'B1', ok: true, as: true, r: -1 }], gone: [] },
  { minute: 9, buses: [], gone: ['B3'] },
  { minute: 20, buses: [{ b: 'B3', ok: false, as: true, sf: 3, r: 1 }], gone: ['B1', 'B2'] },
];
const ids = (list) => list.map((s) => s.b);

test('statusesAt accumulates deltas from the first decision and sorts by bus id', () => {
  assert.deepEqual(statusesAt(deltas, 0), [
    { b: 'B1', ok: false, as: true, sf: 12.5, r: 0 },
    { b: 'B3', ok: true, as: true, sf: 0, r: -1 },
  ]);
  const at1 = statusesAt(deltas, 1);
  assert.deepEqual(ids(at1), ['B1', 'B2', 'B3']);
  assert.deepEqual(at1[0], { b: 'B1', ok: true, as: true, sf: 0, r: -1 }); // overwritten by the later entry
});

test('statusesAt drops the buses a decision lists as gone', () => {
  assert.deepEqual(ids(statusesAt(deltas, 2)), ['B1', 'B2']);
  const last = statusesAt(deltas, 3);
  assert.deepEqual(last, [{ b: 'B3', ok: false, as: true, sf: 3, r: 1 }]); // B3 came back after being gone
});

test('statusesAt reads a missing sf as 0, clamps k and copes with no decisions', () => {
  assert.equal(statusesAt(deltas, 1).every((s) => s.sf === 0), true);
  assert.deepEqual(statusesAt(deltas, 99), statusesAt(deltas, 3));
  assert.deepEqual(statusesAt(deltas, -1), []);
  assert.deepEqual(statusesAt([], 0), []);
  assert.deepEqual(statusesAt([{ minute: 0 }], 0), []); // a decision without lists (fifo, edf, ...)
});

test('statusesAt does not change the decisions it reads', () => {
  const copy = JSON.parse(JSON.stringify(deltas));
  statusesAt(deltas, 3);
  assert.deepEqual(deltas, copy);
});

test('decision notes are resolved through the run-level table', () => {
  const data = { notes: ['rodízio não recomendado: B6 cedeu há 3 min', 'sem folga'] };
  assert.deepEqual(decisionNotes(data, { notes: [1, 0] }), ['sem folga', 'rodízio não recomendado: B6 cedeu há 3 min']);
  assert.deepEqual(decisionNotes(data, { notes: [] }), []);
  assert.deepEqual(decisionNotes(data, {}), []);
  assert.deepEqual(decisionNotes(data, { notes: [7] }), []); // an index outside the table is skipped
});

// two chargers, two buses, five minutes; B1 waits, then charges on C1, B2 sits on a failed C2
const data = {
  scenario: { start_clock_min: 1080 },
  series: {
    limit_kw: [0, 0, 0, 0, 0],
    chargers: [
      { id: 'C1', status: [0, 0, 0, 0, 0], physical_kw: [0, 0, 50, 50, 0] },
      { id: 'C2', status: [0, 1, 1, 1, 0], physical_kw: [0, 0, 0, 0, 0] },
    ],
    buses: [
      { id: 'B1', state: [0, 1, 1, 1, 2], observed_kwh: [null, null, 50, null, null], charger: [-1, -1, 0, 0, -1] },
      { id: 'B2', state: [1, 1, 1, 1, 2], observed_kwh: [60, 60, 60, 60, null], charger: [1, 1, 1, 1, -1] },
    ],
  },
  outcomes: [
    { id: 'B1', arrival: 1, departure: 4, capacity_kwh: 300, initial_soc_kwh: 40, forecast_target_kwh: 200, true_target_kwh: 230, final_soc_kwh: 180, departed: true, ready: false, shortfall_kwh: 50 },
    { id: 'B2', arrival: 0, departure: 4, capacity_kwh: 300, initial_soc_kwh: 60, forecast_target_kwh: 100, true_target_kwh: 100, final_soc_kwh: 100, departed: true, ready: true, shortfall_kwh: 0 },
  ],
};

test('charger occupancy is derived from the bus series', () => {
  assert.deepEqual(chargerOccupancy(data), [['', '', 'B1', 'B1', ''], ['B2', 'B2', 'B2', 'B2', '']]);
});

test('bus exposure counts waiting, charging, failed chargers and missing readings', () => {
  assert.deepEqual(busExposure(data, 0), { waiting: 1, charging: 2, onFaulted: 0, onOffline: 0, noReading: 2, present: 3 });
  assert.deepEqual(busExposure(data, 1), { waiting: 0, charging: 0, onFaulted: 3, onOffline: 0, noReading: 0, present: 4 });
});

test('explainBus says what happened to a bus that left without its charge', () => {
  const text = explainBus(data, 0).join(' ');
  assert.match(text, /Saiu sem a carga/);
  assert.match(text, /faltaram 50 kWh/);
  assert.match(text, /consumo real passou do previsto/);
  assert.match(text, /esperando carregador/);
  assert.match(text, /sem leitura de carga/);
});

test('explainBus for a bus that left ready and one that never left', () => {
  assert.match(explainBus(data, 1).join(' '), /Saiu pronto/);
  const never = { ...data, outcomes: [{ ...data.outcomes[0], departed: false }, data.outcomes[1]] };
  assert.match(explainBus(never, 0).join(' '), /Não saiu dentro do horizonte/);
});

test('explainBus names the failed charger as the likely cause', () => {
  const o = { ...data.outcomes[1], ready: false, shortfall_kwh: 20, final_soc_kwh: 80 };
  const failed = { ...data, outcomes: [data.outcomes[0], o] };
  assert.match(explainBus(failed, 1).join(' '), /Possível causa: ficou em carregador com falha/);
});

test('explainBus blames the available power when the bus always had a working charger', () => {
  const clean = {
    ...data,
    series: { ...data.series, chargers: [{ id: 'C1', status: [0, 0, 0, 0, 0], physical_kw: [0, 10, 10, 10, 0] }],
      buses: [{ id: 'B1', state: [1, 1, 1, 1, 2], observed_kwh: [1, 1, 1, 1, null], charger: [0, 0, 0, 0, -1] }] },
    outcomes: [{ ...data.outcomes[0], arrival: 0 }],
  };
  assert.match(explainBus(clean, 0).join(' '), /Possível causa: teve carregador o tempo todo; a potência disponível/);
});

// A one-bus run of `n` minutes for the cause tests: the bus is present the whole time,
// `charger(i)` gives its charger index at minute i (-1 = waiting) and `err` the constant
// difference between the reading and the truth.
function causeRun({ n = 200, o = {}, charger = () => 0, status = () => 0, err = 0, maxKw = 150, missing = () => false } = {}) {
  const idx = [...Array(n).keys()];
  return {
    params: { reading_age_min: 0 },
    scenario: { start_clock_min: 1080, horizon_min: n, chargers: [{ id: 'C1', max_kw: maxKw, min_kw: 0 }] },
    series: {
      limit_kw: idx.map(() => 0),
      chargers: [{ id: 'C1', status: idx.map(status), physical_kw: idx.map((i) => (charger(i) === 0 ? 100 : 0)) }],
      buses: [{ id: 'B1', state: idx.map(() => 1), charger: idx.map(charger),
        true_soc_kwh: idx.map(() => 100), observed_kwh: idx.map((i) => (missing(i) ? null : 100 + err)) }],
    },
    outcomes: [{ id: 'B1', arrival: 0, departure: n, capacity_kwh: 350, initial_soc_kwh: 50, forecast_target_kwh: 200, true_target_kwh: 200,
      final_soc_kwh: 180, departed: true, ready: false, shortfall_kwh: 20, ...o }],
  };
}
const cause = (d) => explainBus(d, 0).filter((t) => t.startsWith('Possível causa')).join(' | ');

test('explainBus: reaching the forecast but not the real need blames the consumption, not the charger queue', () => {
  // The bus waited for a charger half of the time, yet it got all the energy the planner asked for.
  const d = causeRun({ o: { forecast_target_kwh: 289.1, true_target_kwh: 319.1, final_soc_kwh: 312, shortfall_kwh: 7.1, initial_soc_kwh: 50 },
    charger: (i) => (i < 100 ? -1 : 0) });
  assert.match(cause(d), /consumo real passou do previsto \(o planejador não tinha como saber\)/);
  assert.doesNotMatch(cause(d), /esperou por um carregador/);
});

test('explainBus: a bus a little under the forecast (within 1 kWh) counts as having reached it', () => {
  const d = causeRun({ o: { forecast_target_kwh: 200, true_target_kwh: 230, final_soc_kwh: 199.4, shortfall_kwh: 30.6 } });
  assert.match(cause(d), /^Possível causa: o consumo real/);
});

test('explainBus: a bus that could not charge enough in its stay at the fastest charger says so', () => {
  // 60 min at 150 kW = 150 kWh, but it needed 300 - 40 = 260 kWh
  const d = causeRun({ n: 60, maxKw: 150, o: { arrival: 0, departure: 60, initial_soc_kwh: 40, forecast_target_kwh: 300, true_target_kwh: 300, final_soc_kwh: 140, shortfall_kwh: 160 } });
  assert.match(cause(d), /^Possível causa: fisicamente impossível/);
  assert.match(cause(d), /1 h 00 min/);
  assert.match(cause(d), /150 kW/);
  assert.match(cause(d), /150 kWh/);
  assert.match(cause(d), /260 kWh/);
  assert.doesNotMatch(cause(d), /potência disponível/);
});

test('explainBus: it is not "impossible" when the fastest charger could have delivered the energy', () => {
  const d = causeRun({ n: 600, o: { departure: 600 } });
  assert.doesNotMatch(cause(d), /fisicamente impossível/);
});

test('explainBus: large reading errors over the stay are blamed when the bus fell short of its target', () => {
  const d = causeRun({ err: 12, o: { forecast_target_kwh: 200, true_target_kwh: 200, final_soc_kwh: 185, shortfall_kwh: 15 } });
  assert.match(cause(d), /^Possível causa: leituras de carga imprecisas ou atrasadas/);
  assert.match(cause(d), /12 kWh/);
});

test('explainBus: small reading errors are not a cause', () => {
  const d = causeRun({ err: 2, o: { forecast_target_kwh: 200, true_target_kwh: 200, final_soc_kwh: 185, shortfall_kwh: 15 } });
  assert.doesNotMatch(cause(d), /leituras de carga/);
  assert.match(cause(d), /potência disponível/);
});

test('explainBus: a reading error much smaller than the shortfall does not explain it', () => {
  const d = causeRun({ err: 6, o: { forecast_target_kwh: 200, true_target_kwh: 200, final_soc_kwh: 130, shortfall_kwh: 70 } });
  assert.doesNotMatch(cause(d), /leituras de carga/);
});

test('explainBus: a long wait for a charger is the cause when nothing the planner cannot see applies', () => {
  const d = causeRun({ charger: (i) => (i < 120 ? -1 : 0) });
  assert.match(cause(d), /^Possível causa: esperou por um carregador livre/);
});

test('explainBus: a few minutes of waiting do not count', () => {
  const d = causeRun({ charger: (i) => (i < 5 ? -1 : 0) });
  assert.doesNotMatch(cause(d), /esperou/);
  assert.match(cause(d), /potência disponível/);
});

test('explainBus: time on a failed charger comes before waiting and the power fallback', () => {
  const d = causeRun({ charger: (i) => (i < 30 ? -1 : 0), status: (i) => (i >= 100 && i < 130 ? 1 : 0) });
  assert.match(cause(d), /^Possível causa: ficou em carregador com falha/);
  assert.match(cause(d), /esperou por um carregador livre/); // both are listed, the failure first
});

test('explainBus: precedence is consumption, then impossible, then readings, then the charger', () => {
  const base = { err: 12, status: (i) => (i < 50 ? 1 : 0), charger: (i) => (i < 20 ? -1 : 0) };
  const impossible = { n: 60, o: { departure: 60, initial_soc_kwh: 40, forecast_target_kwh: 300, true_target_kwh: 300, final_soc_kwh: 140, shortfall_kwh: 160 } };
  assert.match(cause(causeRun({ ...base, ...impossible })), /^Possível causa: fisicamente impossível/);
  assert.match(cause(causeRun({ ...base, ...impossible, o: { ...impossible.o, forecast_target_kwh: 130, final_soc_kwh: 140 } })), /^Possível causa: o consumo real/);
  assert.match(cause(causeRun({ ...base, o: { final_soc_kwh: 185, shortfall_kwh: 15 } })), /^Possível causa: leituras de carga/);
  assert.match(cause(causeRun({ ...base, err: 0, o: { final_soc_kwh: 185, shortfall_kwh: 15 } })), /^Possível causa: ficou em carregador com falha/);
});

test('explainBus: when the consumption is the cause the charger queue is not listed as another one', () => {
  const d = causeRun({ o: { forecast_target_kwh: 150, true_target_kwh: 190, final_soc_kwh: 160, shortfall_kwh: 30 }, charger: (i) => (i < 100 ? -1 : 0), status: (i) => (i >= 150 ? 1 : 0), err: 12 });
  assert.equal(explainBus(d, 0).filter((t) => t.startsWith('Possível causa')).length, 1);
  assert.doesNotMatch(cause(d), /esperou|falha|leituras/);
});

test('explainBus: ready buses and buses without a reading series still explain without a cause', () => {
  const ready = causeRun({ o: { ready: true, final_soc_kwh: 200, shortfall_kwh: 0 } });
  assert.equal(cause(ready), '');
  const noTruth = causeRun({ o: { shortfall_kwh: 20 } });
  delete noTruth.series.buses[0].true_soc_kwh;
  assert.match(cause(noTruth), /Possível causa/); // no true SoC: the reading check is skipped, not crashed
});

test('selection notifies only on change', () => {
  const sel = createSelection();
  const seen = [];
  sel.onChange((id) => seen.push(id));
  sel.set('B1'); sel.set('B1'); sel.set(null);
  assert.deepEqual(seen, ['B1', null]);
  assert.equal(sel.get(), null);
});
