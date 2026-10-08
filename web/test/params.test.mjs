import test from 'node:test';
import assert from 'node:assert/strict';
import { stateToHash, hashToState, decideHashAction, linkState, coerce, fieldSpecs, TABS, CONTROLLERS } from '../static/js/params.js';

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
  assert.deepEqual(TABS, ['scenario', 'compare', 'real', 'run']);
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
  assert.equal(nextTab('compare', 'ArrowRight'), 'real');
  assert.equal(nextTab('real', 'ArrowRight'), 'run');
  assert.equal(nextTab('run', 'ArrowRight'), 'scenario'); // wraps
  assert.equal(nextTab('scenario', 'ArrowLeft'), 'run');  // wraps
  assert.equal(nextTab('run', 'ArrowLeft'), 'real');
  assert.equal(nextTab('real', 'ArrowLeft'), 'compare');
  assert.equal(nextTab('compare', 'Home'), 'scenario');
  assert.equal(nextTab('compare', 'End'), 'run');
  assert.equal(nextTab('real', 'End'), 'run');
  assert.equal(nextTab('compare', 'a'), null);
  assert.equal(nextTab('compare', 'Enter'), null);
  assert.equal(nextTab('bogus', 'ArrowRight'), null);
});

test('#tab=real is a valid view: the tab goes into the hash, never the data', () => {
  const s = hashToState('#tab=real&buses=12', defaults);
  assert.equal(s.tab, 'real');
  assert.equal(s.params.buses, 12);
  assert.ok(stateToHash(s).startsWith('#tab=real&'));
  assert.deepEqual(hashToState(stateToHash(s), defaults), s);
  // nothing of an imported run (files, night, a controller name outside the list) is ever read from a hash
  const odd = hashToState('#tab=run&controller=planner%20(sem%20rod%C3%ADzio)&night=2026-03-04&files=x', defaults);
  assert.equal(odd.controller, 'planner');
  assert.equal(stateToHash(odd).includes('night'), false);
});

// The shape of the real /api/defaults reply: the parameters are nested under `params`.
const response = { params: defaults, limits: { max_buses: 10000 }, presets: [] };

test('decideHashAction: a hash that normalises to the screen state needs no action', () => {
  const state = { params: { ...defaults, limit_kw: 600 }, tab: 'run', seed: 3, controller: 'planner' };
  assert.equal(decideHashAction(stateToHash(state), state, response.params), null);
  const reordered = '#' + [...new URLSearchParams(stateToHash(state).slice(1))].reverse().map(([k, v]) => `${k}=${v}`).join('&');
  assert.equal(decideHashAction(reordered, state, response.params), null);
  // empty hash against a fresh default state, and garbage that falls back to the defaults
  const fresh = hashToState('', response.params);
  assert.equal(decideHashAction('', fresh, response.params), null);
  assert.equal(decideHashAction('#', fresh, response.params), null);
  assert.equal(decideHashAction('#tab=nope&buses=abc&seeds=1.5&follow_swaps=maybe', fresh, response.params), null);
});

test('decideHashAction: a different hash returns the next state', () => {
  const state = { params: { ...defaults, limit_kw: 600 }, tab: 'run', seed: 3, controller: 'planner' };
  const seed = decideHashAction(stateToHash({ ...state, seed: 4 }), state, response.params);
  assert.deepEqual(seed, { ...state, seed: 4 });
  assert.equal(decideHashAction(stateToHash({ ...state, tab: 'compare' }), state, response.params).tab, 'compare');
  const profile = decideHashAction(stateToHash({ ...state, params: { ...state.params, profile: 'severe' } }), state, response.params);
  assert.equal(profile.params.profile, 'severe');
  assert.equal(profile.params.limit_kw, 600);
  // partial hash: what is missing comes from the defaults, not from the screen state
  const partial = decideHashAction('#tab=compare&limit_kw=800&profile=mild', state, response.params);
  assert.deepEqual(partial, { params: { ...defaults, limit_kw: 800, profile: 'mild' }, tab: 'compare', seed: 1, controller: 'planner' });
  // an empty hash against a non-default screen is a change back to the defaults
  assert.deepEqual(decideHashAction('', state, response.params), hashToState('', response.params));
  // garbage values fall back to the defaults, and that is still a change from a custom screen
  assert.deepEqual(decideHashAction('#buses=abc&tab=nope&seed=-4', state, response.params), hashToState('', response.params));
});

// The Execução tab can show a run computed with other parameters than the ones a newer
// comparison put in `state.params`: the link must describe what is on screen.
const onScreen = { params: { ...defaults, buses: 30, profile: 'severe' }, controller: 'fifo', seed: 3 };
const newer = { params: { ...defaults, buses: 20 }, tab: 'run', seed: 1, controller: 'planner' };

test('linkState: on the run tab the link carries the run on screen', () => {
  const l = linkState(newer, onScreen);
  assert.deepEqual(l.params, onScreen.params);
  assert.equal(l.controller, 'fifo');
  assert.equal(l.seed, 3);
  assert.equal(l.tab, 'run');
  const back = hashToState(stateToHash(l), defaults);
  assert.deepEqual(back.params, onScreen.params);
});

test('linkState: other tabs, or no run on screen, keep the state as it is', () => {
  assert.deepEqual(linkState({ ...newer, tab: 'compare' }, onScreen).params, newer.params);
  assert.deepEqual(linkState({ ...newer, tab: 'scenario' }, onScreen).params, newer.params);
  assert.equal(linkState(newer, null), newer);
  assert.equal(linkState(newer, undefined), newer);
});

test('linkState does not change the state it reads', () => {
  const copy = JSON.parse(JSON.stringify(newer));
  linkState(newer, onScreen);
  assert.deepEqual(newer, copy);
});

test('a hash that describes the run on screen needs no action even when state.params is newer', () => {
  const hash = stateToHash(linkState(newer, onScreen));
  assert.equal(decideHashAction(hash, linkState(newer, onScreen), defaults), null);
  assert.notEqual(decideHashAction(hash, newer, defaults), null); // without linkState it would re-run
});

// A run of imported real data cannot be rebuilt from a link (the files stay on this computer): the
// link of that screen leads to the "Dados reais" tab, never to a run of the generator.
test('linkState: a run of imported data links to the Dados reais tab', () => {
  const real = { params: onScreen.params, controller: 'planner (sem rodízio)', seed: 1, real: { night: '2026-03-04' } };
  const l = linkState({ ...newer, tab: 'run' }, real);
  assert.equal(l.tab, 'real');
  assert.deepEqual(l.params, newer.params); // the form's parameters, not those of the imported run
  assert.equal(l.controller, newer.controller);
  const hash = stateToHash(l);
  assert.ok(hash.startsWith('#tab=real&'));
  assert.equal(hash.includes('2026-03-04'), false);
  assert.equal(decideHashAction(hash, l, defaults), null);
});

test('linkState: an imported run only matters on the run tab', () => {
  const real = { params: onScreen.params, controller: 'planner', seed: 1, real: { night: '2026-03-04' } };
  assert.equal(linkState({ ...newer, tab: 'real' }, real).tab, 'real');
  assert.equal(linkState({ ...newer, tab: 'compare' }, real).tab, 'compare');
});
