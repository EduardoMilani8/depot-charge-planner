import test from 'node:test';
import assert from 'node:assert/strict';
import { nextFocusTarget } from '../static/js/focus.js';

// A run reload clears the view; the control that asked for it gets focus back afterwards.
test('focus goes back to the control that triggered the reload', () => {
  assert.deepEqual(nextFocusTarget(null, { activeId: 'run-controller', inside: true }), { id: 'run-controller' });
  assert.deepEqual(nextFocusTarget(null, { activeId: 'run-seed', inside: true }), { id: 'run-seed' });
});

test('a reload started from outside the run view moves no focus', () => {
  assert.equal(nextFocusTarget(null, { activeId: 'other', inside: false }), null);
  assert.equal(nextFocusTarget(null, { activeId: '', inside: true }), null);
  assert.equal(nextFocusTarget(null, { activeId: null, inside: true }), null);
});

test('a reload while another is loading keeps the pending target (focus is on <body> by then)', () => {
  const pending = { id: 'run-controller' };
  assert.equal(nextFocusTarget(pending, { activeId: '', inside: false }), pending);
  assert.deepEqual(nextFocusTarget(pending, { activeId: 'run-seed', inside: true }), { id: 'run-seed' });
});

test('opening a run from a seed dot focuses the run heading', () => {
  assert.deepEqual(nextFocusTarget(null, { activeId: '', inside: false, heading: true }), { heading: true });
  assert.deepEqual(nextFocusTarget({ id: 'run-seed' }, { activeId: '', inside: false, heading: true }), { heading: true });
});
