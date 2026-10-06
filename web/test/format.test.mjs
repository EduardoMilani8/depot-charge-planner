import test from 'node:test';
import assert from 'node:assert/strict';
import { fmtNum, fmtPct, fmtBRL, clock, duration } from '../static/js/format.js';

test('numbers use the Brazilian separators', () => {
  assert.equal(fmtNum(1234.56, 1), '1.234,6');
  assert.equal(fmtNum(1234567, 0), '1.234.567');
  assert.equal(fmtNum(0.5, 2), '0,50');
  assert.equal(fmtNum(-3.26, 1), '-3,3');
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

// Go's %.Nf rounds the exact binary value, half to even on exact ties; JS toFixed picks the
// larger one on ties. Only exact ties (value*10^d is exactly k+0.5) may differ.
test('exact binary ties round half to even like Go', () => {
  assert.equal(fmtNum(36.25, 1), '36,2');
  assert.equal(fmtNum(56.25, 1), '56,2');
  assert.equal(fmtNum(43.75, 1), '43,8');
  assert.equal(fmtNum(41.25, 1), '41,2');
  assert.equal(fmtNum(2.5, 0), '2');
  assert.equal(fmtNum(3.5, 0), '4');
  assert.equal(fmtNum(0.5, 0), '0');
  assert.equal(fmtNum(1.5, 0), '2');
  assert.equal(fmtNum(0.125, 2), '0,12');
  assert.equal(fmtNum(0.375, 2), '0,38');
  assert.equal(fmtNum(1234.5, 0), '1.234');
  assert.equal(fmtNum(1235.5, 0), '1.236');
  assert.equal(fmtNum(999.5, 0), '1.000'); // 999 is odd: up, carrying into a new digit
});

test('ties round to even in magnitude for negatives', () => {
  assert.equal(fmtNum(-3.25, 1), '-3,2');
  assert.equal(fmtNum(-2.5, 0), '-2');
  assert.equal(fmtNum(-3.5, 0), '-4');
  assert.equal(fmtNum(-0.5, 0), '0'); // rounds to zero: no sign, as before
});

test('values that are not exact ties keep their exact-decimal rounding', () => {
  assert.equal(fmtNum(36.35, 1), '36,4'); // 36.35 is really 36.350000000000001
  assert.equal(fmtNum(36.45, 1), '36,5'); // 36.4500000000000028
  assert.equal(fmtNum(36.15, 1), '36,1'); // 36.1499999999999986
  assert.equal(fmtNum(1.005, 2), '1,00'); // 1.00499999999999989
  assert.equal(fmtNum(2.675, 2), '2,67');
  assert.equal(fmtNum(0.1 + 0.2, 1), '0,3');
  assert.equal(typeof fmtNum(1e21, 0), 'string'); // huge values must not crash
});

test('fmtNum rounds ties at any digit count like Go', () => {
  assert.equal(fmtNum(0.0625, 3), '0,062');
  assert.equal(fmtNum(0.1875, 3), '0,188');
  assert.equal(fmtNum(1048576.5, 0), '1.048.576');
});
