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
  assert.match(explainBus(failed, 1).join(' '), /carregador com falha/);
});

test('explainBus blames the available power when the bus always had a working charger', () => {
  const clean = {
    ...data,
    series: { ...data.series, chargers: [{ id: 'C1', status: [0, 0, 0, 0, 0], physical_kw: [0, 10, 10, 10, 0] }],
      buses: [{ id: 'B1', state: [1, 1, 1, 1, 2], observed_kwh: [1, 1, 1, 1, null], charger: [0, 0, 0, 0, -1] }] },
    outcomes: [{ ...data.outcomes[0], arrival: 0 }],
  };
  assert.match(explainBus(clean, 0).join(' '), /potência disponível/);
});

test('selection notifies only on change', () => {
  const sel = createSelection();
  const seen = [];
  sel.onChange((id) => seen.push(id));
  sel.set('B1'); sel.set('B1'); sel.set(null);
  assert.deepEqual(seen, ['B1', null]);
  assert.equal(sel.get(), null);
});
