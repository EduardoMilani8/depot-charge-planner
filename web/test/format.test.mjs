import test from 'node:test';
import assert from 'node:assert/strict';
import { fmtNum, fmtPct, fmtBRL, clock, duration } from '../static/js/format.js';

test('numbers use the Brazilian separators', () => {
  assert.equal(fmtNum(1234.56, 1), '1.234,6');
  assert.equal(fmtNum(1234567, 0), '1.234.567');
  assert.equal(fmtNum(0.5, 2), '0,50');
  assert.equal(fmtNum(-3.25, 1), '-3,3');
  assert.equal(fmtNum(-0.04, 1), '0,0');
});

test('missing values show a dash', () => {
  for (const v of [null, undefined, NaN, Infinity]) assert.equal(fmtNum(v), '—');
});

test('percent and money', () => {
  assert.equal(fmtPct(75.64), '75,6%');
  assert.equal(fmtBRL(11331.4), 'R$ 11.331');
});

test('clock wraps past midnight and before it', () => {
  assert.equal(clock(18 * 60, 0), '18:00');
  assert.equal(clock(18 * 60, 125), '20:05');
  assert.equal(clock(18 * 60, 800), '07:20');
  assert.equal(clock(18 * 60, -60), '17:00');
});

test('durations', () => {
  assert.equal(duration(45), '45 min');
  assert.equal(duration(125), '2 h 05 min');
  assert.equal(duration(60), '1 h 00 min');
  assert.equal(duration(-5), '0 min');
});
