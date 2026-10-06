import test from 'node:test';
import assert from 'node:assert/strict';
import { linear, niceTicks, timeTicks } from '../static/js/charts/scale.js';

test('linear maps the domain to the range and back', () => {
  const f = linear(0, 100, 10, 210);
  assert.equal(f(0), 10);
  assert.equal(f(50), 110);
  assert.equal(f(100), 210);
  assert.equal(f.invert(110), 50);
  const flip = linear(0, 10, 100, 0);
  assert.equal(flip(2), 80);
});

test('a degenerate domain does not divide by zero', () => {
  const f = linear(5, 5, 0, 100);
  assert.equal(f(5), 0);
  assert.equal(f.invert(50), 5);
});

test('nice ticks', () => {
  assert.deepEqual(niceTicks(0, 2000, 5), [0, 500, 1000, 1500, 2000]);
  assert.deepEqual(niceTicks(0, 87, 5), [0, 20, 40, 60, 80]);
  assert.deepEqual(niceTicks(5, 5), [5]);
  assert.deepEqual(niceTicks(9, 1), [9]);
  assert.deepEqual(niceTicks(0, 1, 5), [0, 0.2, 0.4, 0.6, 0.8, 1]);
});

test('time ticks every hour', () => {
  assert.deepEqual(timeTicks(180, 60), [0, 60, 120, 180]);
  assert.deepEqual(timeTicks(100, 60), [0, 60]);
});
