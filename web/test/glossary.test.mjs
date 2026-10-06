import test from 'node:test';
import assert from 'node:assert/strict';
import { FAULT_LABEL, LAYER_LABEL, POWER_FAULTS, describeFault } from '../static/js/glossary.js';

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
