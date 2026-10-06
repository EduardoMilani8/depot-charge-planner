import test from 'node:test';
import assert from 'node:assert/strict';
import { feedKind, createFeedModel } from '../static/js/feed.js';

const ev = (minute, type, extra = {}) => ({ minute, type, text: `${type}@${minute}`, ...extra });

test('feedKind tells ready departures from the ones that left short', () => {
  assert.equal(feedKind(ev(1, 'depart', { ok: true })), 'depart_ok');
  assert.equal(feedKind(ev(1, 'depart', { ok: false })), 'depart_fail');
  assert.equal(feedKind(ev(1, 'swap')), 'swap');
  assert.equal(feedKind(ev(1, 'layer')), 'layer');
  assert.equal(feedKind(ev(1, 'fault')), 'fault');
});

test('the newest event is first and each event gets its own id', () => {
  const m = createFeedModel(5);
  const a = m.push([ev(1, 'swap'), ev(2, 'layer')]);
  assert.deepEqual(m.items.map((i) => i.minute), [2, 1]);
  assert.equal(a.added.length, 2);
  m.push([ev(3, 'fault')]);
  assert.deepEqual(m.items.map((i) => i.minute), [3, 2, 1]);
  assert.equal(new Set(m.items.map((i) => i.id)).size, 3);
});

test('only the newest `max` are kept and the push says what was dropped', () => {
  const m = createFeedModel(3);
  m.push([ev(1, 'swap'), ev(2, 'swap'), ev(3, 'swap')]);
  const r = m.push([ev(4, 'swap'), ev(5, 'swap')]);
  assert.deepEqual(m.items.map((i) => i.minute), [5, 4, 3]);
  assert.deepEqual(r.removed.map((i) => i.minute), [2, 1]);
  assert.equal(m.total, 5);
});

test('a burst bigger than max still ends with the newest max and counts all of them', () => {
  const m = createFeedModel(2);
  const burst = Array.from({ length: 40 }, (_, i) => ev(i, 'depart', { ok: true }));
  const r = m.push(burst);
  assert.deepEqual(m.items.map((i) => i.minute), [39, 38]);
  assert.equal(r.added.length, 2); // no point building 40 nodes to throw 38 away
  assert.equal(m.total, 40);
});

test('clear empties the list and the counter, and an empty push is a no-op', () => {
  const m = createFeedModel(3);
  m.push([ev(1, 'swap')]);
  const none = m.push([]);
  assert.deepEqual(none, { added: [], removed: [] });
  m.clear();
  assert.deepEqual(m.items, []);
  assert.equal(m.total, 0);
});

test('reset replaces the list and sets the counter to a total that may exceed what is shown', () => {
  const m = createFeedModel(3);
  m.push([ev(1, 'swap'), ev(2, 'swap'), ev(3, 'swap'), ev(4, 'swap')]);
  const r = m.reset([ev(8, 'swap'), ev(9, 'swap')], 21);
  assert.deepEqual(m.items.map((i) => i.minute), [9, 8]);
  assert.equal(m.total, 21);
  assert.equal(r.added.length, 2);
  m.reset([], 0);
  assert.deepEqual(m.items, []);
  assert.equal(m.total, 0);
});
