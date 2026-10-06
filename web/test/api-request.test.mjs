import test from 'node:test';
import assert from 'node:assert/strict';
import { getDefaults, compare, ApiError } from '../static/js/api.js';

const realFetch = globalThis.fetch;
test.afterEach(() => { globalThis.fetch = realFetch; });

const reply = (status, body) => async () => ({
  ok: status >= 200 && status < 300,
  status,
  json: async () => { if (body === undefined) throw new SyntaxError('not json'); return body; },
});

test('a JSON error body becomes an ApiError with message, field and status', async () => {
  globalThis.fetch = reply(400, { error: 'Ônibus: valor inválido', field: 'buses' });
  await assert.rejects(compare({}), (e) => e instanceof ApiError && e.message === 'Ônibus: valor inválido' && e.field === 'buses' && e.status === 400);
});

test('a non-JSON error body falls back to the status', async () => {
  globalThis.fetch = reply(500, undefined);
  await assert.rejects(getDefaults(), (e) => e instanceof ApiError && e.message === 'Erro 500' && e.field === '' && e.status === 500);
});

test('a network failure is reported in Portuguese with status 0', async () => {
  globalThis.fetch = async () => { throw new TypeError('failed to fetch'); };
  await assert.rejects(getDefaults(), (e) => e instanceof ApiError && e.status === 0 && e.message.startsWith('Não consegui falar com o servidor'));
});

test('an abort is rethrown as is', async () => {
  const abort = Object.assign(new Error('aborted'), { name: 'AbortError' });
  globalThis.fetch = async () => { throw abort; };
  await assert.rejects(getDefaults(), (e) => e === abort);
});

test('success returns the parsed JSON and posts the body as JSON', async () => {
  let seen;
  globalThis.fetch = async (url, opts) => { seen = { url, opts }; return reply(200, { ok: 1 })(); };
  assert.deepEqual(await compare({ buses: 3 }), { ok: 1 });
  assert.equal(seen.url, '/api/compare');
  assert.equal(seen.opts.method, 'POST');
  assert.equal(seen.opts.body, '{"buses":3}');
  assert.equal(seen.opts.headers['Content-Type'], 'application/json');
});
