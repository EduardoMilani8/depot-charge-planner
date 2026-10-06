import test from 'node:test';
import assert from 'node:assert/strict';
import { fillPath } from '../static/js/charts/layout.js';

const id = (v) => v;

test('a filled area closes down to the baseline', () => {
  assert.equal(fillPath([1, 2], id, id, 0), 'M0,0L0,1L1,2L1,0Z');
});

test('a gap in the data starts a new area', () => {
  assert.equal(fillPath([1, null, 2], id, id, 0), 'M0,0L0,1L0,0ZM2,0L2,2L2,0Z');
});

test('no finite values give an empty path', () => {
  assert.equal(fillPath([], id, id, 0), '');
  assert.equal(fillPath([null, NaN], id, id, 0), '');
});
