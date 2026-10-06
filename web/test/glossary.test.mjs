import test from 'node:test';
import assert from 'node:assert/strict';
import { FAULT_LABEL, LAYER_LABEL, POWER_FAULTS, describeFault, describeParams } from '../static/js/glossary.js';

test('every fault kind and layer has a Portuguese label', () => {
  for (const k of ['charger_fail', 'charger_offline', 'limit_drop', 'soc_noise', 'soc_bias', 'soc_freeze', 'soc_missing',
    'consumption_over', 'late_arrival', 'early_departure', 'planner_panic', 'planner_slow', 'planner_garbage']) {
    assert.ok(FAULT_LABEL[k], k);
  }
  for (const l of ['normal', 'last-valid', 'safe']) assert.ok(LAYER_LABEL[l], l);
});

test('only power-related faults are drawn on the power chart', () => {
  assert.ok(POWER_FAULTS.has('limit_drop') && POWER_FAULTS.has('charger_fail') && POWER_FAULTS.has('planner_panic'));
  assert.ok(!POWER_FAULTS.has('soc_noise') && !POWER_FAULTS.has('late_arrival'));
});

test('describeFault names the target, the window and the value', () => {
  assert.equal(describeFault({ kind: 'limit_drop', target: '', from: 120, to: 300, value: 0.5 }), 'queda do limite da rede (×0,5), min 120–300');
  assert.equal(describeFault({ kind: 'charger_fail', target: 'C003', from: 10, to: 20, value: 0 }), 'carregador em falha (C003), min 10–20');
  assert.equal(describeFault({ kind: 'soc_noise', target: '*', from: 0, to: 50, value: 3 }), 'ruído na leitura de carga (3 kWh), min 0–50');
  assert.equal(describeFault({ kind: 'weird', target: '', from: 1, to: 2, value: 0 }), 'weird, min 1–2');
});

test('a total limit drop (value 0) still says ×0', () => {
  assert.equal(describeFault({ kind: 'limit_drop', target: '', from: 5, to: 9, value: 0 }), 'queda do limite da rede (×0), min 5–9');
});

const P = { buses: 30, chargers: 12, limit_kw: 600, profile: 'severe', seeds: 20, follow_swaps: true, reading_age_min: 0 };

test('describeParams is a one-line summary of the scenario in Portuguese', () => {
  assert.equal(describeParams(P), '30 ônibus, 12 carregadores, limite 600 kW, falhas severas, operadores seguem os rodízios');
  assert.equal(describeParams({ ...P, profile: 'none', follow_swaps: false, limit_kw: 1234.5 }),
    '30 ônibus, 12 carregadores, limite 1.234 kW, sem falhas, operadores não seguem os rodízios'); // 1234,5 ties to even, like simrun
});

test('describeParams mentions the reading age only when there is one, and the seeds on request', () => {
  assert.match(describeParams({ ...P, reading_age_min: 15 }), /leitura com 15 min de idade/);
  assert.doesNotMatch(describeParams(P), /leitura/);
  assert.match(describeParams(P, { seeds: true }), /20 sementes/);
  assert.doesNotMatch(describeParams(P), /sementes/);
  assert.match(describeParams({ ...P, profile: 'weird' }), /weird/);
});
