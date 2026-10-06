import test from 'node:test';
import assert from 'node:assert/strict';
import { runs, stackLayers, areaPath, linePath, assignLanes, valuesAt, MARGIN } from '../static/js/charts/layout.js';

const id = (v) => v;

test('runs run-length encodes an array', () => {
  assert.deepEqual(runs(['a', 'a', 'b', 'a']), [
    { from: 0, to: 1, value: 'a' }, { from: 2, to: 2, value: 'b' }, { from: 3, to: 3, value: 'a' },
  ]);
  assert.deepEqual(runs([]), []);
});

test('stackLayers accumulates and treats missing values as zero', () => {
  const l = stackLayers([[1, 2], [3, null], [NaN, 1]]);
  assert.deepEqual(l[0], { lower: [0, 0], upper: [1, 2] });
  assert.deepEqual(l[1], { lower: [1, 2], upper: [4, 2] });
  assert.deepEqual(l[2].upper, [4, 3]);
  assert.deepEqual(stackLayers([]), []);
});

test('area and line paths', () => {
  assert.equal(areaPath([0, 0], [1, 2], id, id), 'M0,1L1,2L1,0L0,0Z');
  assert.equal(areaPath([], [], id, id), '');
  assert.equal(linePath([0, 10], id, id, false), 'M0,0L1,10');
  assert.equal(linePath([0, 10], id, id, true), 'M0,0L1,0L1,10');
});

test('a line breaks where the value is missing', () => {
  assert.equal(linePath([1, null, 2, 3], id, id, false), 'M0,1M2,2L3,3');
  assert.equal(linePath([NaN, NaN], id, id, false), '');
});

test('lanes keep overlapping intervals apart', () => {
  assert.deepEqual(assignLanes([{ from: 0, to: 10 }, { from: 5, to: 20 }, { from: 11, to: 15 }]), [0, 1, 0]);
  assert.deepEqual(assignLanes([]), []);
});

test('valuesAt clamps the minute and tolerates null', () => {
  const run = { series: { physical_kw: [0, null, 5], commanded_kw: [1, 2, 3], limit_kw: [9, 9, 9], layer: ['normal', 'safe', 'safe'] } };
  assert.deepEqual(valuesAt(run, 1), { physical: null, commanded: 2, limit: 9, layer: 'safe' });
  assert.equal(valuesAt(run, 99).physical, 5);
  assert.equal(valuesAt(run, -4).layer, 'normal');
});

test('the shared left margin leaves room for row labels', () => {
  assert.ok(MARGIN.left >= 100);
});
