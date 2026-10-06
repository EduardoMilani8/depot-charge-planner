import test from 'node:test';
import assert from 'node:assert/strict';
import { createCursor, keyDelta, bindCursorKeys } from '../static/js/cursor.js';

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

// A minimal stand-in for a focusable element: it records the keydown listener.
function fakeKeyTarget() {
  const el = { handler: null, addEventListener(type, fn) { if (type === 'keydown') el.handler = fn; } };
  const press = (key, shiftKey = false) => {
    const e = { key, shiftKey, prevented: false, preventDefault() { e.prevented = true; } };
    el.handler(e);
    return e;
  };
  return { el, press };
}

test('Home and End jump to the ends and the page does not scroll', () => {
  const c = createCursor(100);
  const { el, press } = fakeKeyTarget();
  bindCursorKeys(el, c);
  c.set(40);
  assert.equal(press('End').prevented, true);
  assert.equal(c.value, 100);
  assert.equal(press('Home').prevented, true);
  assert.equal(c.value, 0);
});

test('arrow keys step the cursor and other keys are left alone', () => {
  const c = createCursor(100);
  const { el, press } = fakeKeyTarget();
  bindCursorKeys(el, c);
  assert.equal(press('ArrowRight').prevented, true);
  assert.equal(c.value, 1);
  press('ArrowRight', true);
  assert.equal(c.value, 11);
  press('PageUp');
  assert.equal(c.value, 71);
  assert.equal(press('a').prevented, false);
  assert.equal(press('Tab').prevented, false);
  assert.equal(c.value, 71);
});
