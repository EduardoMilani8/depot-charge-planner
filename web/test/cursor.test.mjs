import test from 'node:test';
import assert from 'node:assert/strict';
import { createCursor, keyDelta } from '../static/js/cursor.js';

test('the cursor clamps, rounds and ignores non-finite values', () => {
  const c = createCursor(100);
  c.set(250); assert.equal(c.value, 100);
  c.set(-5); assert.equal(c.value, 0);
  c.set(12.6); assert.equal(c.value, 13);
  c.set(NaN); assert.equal(c.value, 13);
  c.set(Infinity); assert.equal(c.value, 13);
  c.step(-20); assert.equal(c.value, 0);
});

test('subscribers hear only real changes and can unsubscribe', () => {
  const c = createCursor(10);
  const seen = [];
  const off = c.onChange((v) => seen.push(v));
  c.set(3); c.set(3); c.set(4);
  off();
  c.set(5);
  assert.deepEqual(seen, [3, 4]);
});

test('keys map to steps', () => {
  assert.equal(keyDelta('ArrowRight', false), 1);
  assert.equal(keyDelta('ArrowRight', true), 10);
  assert.equal(keyDelta('ArrowLeft', false), -1);
  assert.equal(keyDelta('PageUp', false), 60);
  assert.equal(keyDelta('PageDown', false), -60);
  assert.equal(keyDelta('a', false), null);
});

test('a zero-length run has a cursor that stays at 0', () => {
  const c = createCursor(0);
  c.set(5);
  assert.equal(c.value, 0);
});
