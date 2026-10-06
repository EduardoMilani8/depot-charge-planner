import test from 'node:test';
import assert from 'node:assert/strict';
import { createLatest, ApiError } from '../static/js/api.js';

const delay = (ms, v) => new Promise((r) => setTimeout(() => r(v), ms));

test('only the latest call delivers a result and the earlier one is aborted', async () => {
  const latest = createLatest();
  let firstSignal;
  const first = latest(async (signal) => { firstSignal = signal; await delay(40); return 'old'; });
  const second = latest(async () => { await delay(5); return 'new'; });
  const [a, b] = await Promise.all([first, second]);
  assert.equal(a.stale, true);
  assert.deepEqual(b, { stale: false, value: 'new' });
  assert.equal(firstSignal.aborted, true);
});

test('an aborted request is reported as stale, not as an error', async () => {
  const latest = createLatest();
  const p = latest((signal) => new Promise((_, rej) => signal.addEventListener('abort', () => rej(Object.assign(new Error('x'), { name: 'AbortError' })))));
  const q = latest(async () => 1);
  assert.equal((await p).stale, true);
  assert.equal((await q).value, 1);
});

test('a real error of the latest call is thrown', async () => {
  const latest = createLatest();
  await assert.rejects(latest(async () => { throw new ApiError('boom', 'seeds', 400); }), (e) => e instanceof ApiError && e.field === 'seeds' && e.status === 400);
});

test('busy tells whether a call is in flight', async () => {
  const latest = createLatest();
  assert.equal(latest.busy(), false);
  const p = latest(() => delay(10, 1));
  assert.equal(latest.busy(), true);
  await p;
  assert.equal(latest.busy(), false);
});
