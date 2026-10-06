import test from 'node:test';
import assert from 'node:assert/strict';
import { dotStrip } from '../static/js/charts/layout.js';

test('dots sit at their value and never leave the strip', () => {
  const dots = dotStrip([{ seed: 1, value: 0 }, { seed: 2, value: 50 }, { seed: 3, value: 100 }, { seed: 4, value: 140 }, { seed: 5, value: -20 }], 600, 40, 10);
  assert.equal(dots[0].x, 10);
  assert.equal(dots[1].x, 300);
  assert.equal(dots[2].x, 590);
  assert.equal(dots[3].x, 590); // clamped
  assert.equal(dots[4].x, 10);  // clamped
  for (const d of dots) assert.ok(d.y > 0 && d.y < 40);
  assert.deepEqual(dots.map((d) => d.seed), [1, 2, 3, 4, 5]);
});

test('neighbouring dots are spread over rows so they do not hide each other', () => {
  const dots = dotStrip([{ seed: 1, value: 50 }, { seed: 2, value: 50 }, { seed: 3, value: 50 }], 600, 40, 10);
  assert.equal(new Set(dots.map((d) => d.y)).size, 3);
});

test('no values give no dots', () => {
  assert.deepEqual(dotStrip([], 600, 40, 10), []);
});
