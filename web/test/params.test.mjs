import test from 'node:test';
import assert from 'node:assert/strict';
import { stateToHash, hashToState, hashMatchesState, coerce, fieldSpecs, TABS, CONTROLLERS } from '../static/js/params.js';

const defaults = {
  buses: 50, chargers: 25, limit_kw: 2000, profile: 'none', seeds: 20, follow_swaps: true,
  reading_age_min: 0, swap_back_cooldown_min: 30, swap_back_min_need_kwh: 10,
};

test('hash round trip keeps every parameter and the view', () => {
  const state = { params: { ...defaults, buses: 80, limit_kw: 612.5, profile: 'severe', follow_swaps: false }, tab: 'run', seed: 7, controller: 'fifo-unplug' };
  assert.deepEqual(hashToState(stateToHash(state), defaults), state);
});

test('an empty or garbage hash falls back to the defaults', () => {
  for (const hash of ['', '#', '#%%%', '#buses=abc&limit_kw=NaN&seeds=1.5&tab=nope&seed=-4&controller=x&follow_swaps=maybe']) {
    const s = hashToState(hash, defaults);
    assert.deepEqual(s.params, defaults, hash);
    assert.equal(s.tab, 'scenario');
    assert.equal(s.seed, 1);
    assert.equal(s.controller, 'planner');
  }
});

test('partial hashes keep what is valid', () => {
  const s = hashToState('#limit_kw=600&profile=mild&tab=compare', defaults);
  assert.equal(s.params.limit_kw, 600);
  assert.equal(s.params.profile, 'mild');
  assert.equal(s.params.buses, 50);
  assert.equal(s.tab, 'compare');
});

test('coerce rejects what is not the kind', () => {
  assert.equal(coerce('int', '', 5), 5);
  assert.equal(coerce('int', '2.5', 5), 5);
  assert.equal(coerce('int', 'x', 5), 5);
  assert.equal(coerce('float', 'Infinity', 1), 1);
  assert.equal(coerce('float', '0.25', 1), 0.25);
  assert.equal(coerce('bool', 'true', false), true);
  assert.equal(coerce('bool', '1', false), false);
  assert.equal(coerce('int', null, 9), 9);
});

test('constants list the views and controllers', () => {
  assert.deepEqual(TABS, ['scenario', 'compare', 'run']);
  assert.deepEqual(CONTROLLERS, ['fifo', 'edf', 'fifo-unplug', 'safe', 'planner']);
});

test('field specs take their bounds from the server limits', () => {
  const specs = fieldSpecs({ max_buses: 10000, max_chargers: 9000, max_seeds: 1000, max_limit_kw: 1e7, max_reading_age_min: 10000 });
  const by = Object.fromEntries(specs.map((f) => [f.key, f]));
  assert.equal(by.buses.max, 10000);
  assert.equal(by.chargers.max, 9000);
  assert.equal(by.limit_kw.max, 1e7);
  assert.equal(by.seeds.max, 1000);
  assert.equal(by.swap_back_cooldown_min.advanced, true);
  assert.equal(by.buses.advanced, undefined);
  assert.equal(specs.length, 9);
});

test('nextTab implements the keyboard pattern of tabs', async () => {
  const { nextTab } = await import('../static/js/params.js');
  assert.equal(nextTab('scenario', 'ArrowRight'), 'compare');
  assert.equal(nextTab('compare', 'ArrowRight'), 'run');
  assert.equal(nextTab('run', 'ArrowRight'), 'scenario'); // wraps
  assert.equal(nextTab('scenario', 'ArrowLeft'), 'run');  // wraps
  assert.equal(nextTab('run', 'ArrowLeft'), 'compare');
  assert.equal(nextTab('compare', 'Home'), 'scenario');
  assert.equal(nextTab('compare', 'End'), 'run');
  assert.equal(nextTab('compare', 'a'), null);
  assert.equal(nextTab('compare', 'Enter'), null);
  assert.equal(nextTab('bogus', 'ArrowRight'), null);
});

test('hashMatchesState tells a hash that is already on screen from a new one', () => {
  const state = { params: { ...defaults, limit_kw: 600 }, tab: 'run', seed: 3, controller: 'planner' };
  assert.equal(hashMatchesState(stateToHash(state), state, defaults), true);
  // order of keys and omitted defaults do not make a different state
  const reordered = '#' + [...new URLSearchParams(stateToHash(state).slice(1))].reverse().map(([k, v]) => `${k}=${v}`).join('&');
  assert.equal(hashMatchesState(reordered, state, defaults), true);
  assert.equal(hashMatchesState(stateToHash({ ...state, seed: 4 }), state, defaults), false);
  assert.equal(hashMatchesState(stateToHash({ ...state, tab: 'compare' }), state, defaults), false);
  assert.equal(hashMatchesState(stateToHash({ ...state, params: { ...state.params, profile: 'severe' } }), state, defaults), false);
  // garbage normalises to the defaults, so it matches only a state that is the defaults
  const fresh = hashToState('', defaults);
  assert.equal(hashMatchesState('#tab=nope&buses=abc', fresh, defaults), true);
  assert.equal(hashMatchesState('', state, defaults), false);
});
