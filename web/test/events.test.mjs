import test from 'node:test';
import assert from 'node:assert/strict';
import { eventsBetween, crossed, crossedPlayback, feedUpdate } from '../static/js/events.js';

const data = {
  scenario: {
    start_clock_min: 1080,
    faults: [
      { kind: 'limit_drop', target: '', from: 20, to: 40, value: 0.5 },
      { kind: 'soc_noise', target: '*', from: 22, to: 30, value: 3 },
      { kind: 'charger_fail', target: 'C2', from: 50, to: 60, value: 0 },
    ],
  },
  series: { layer: ['normal', 'normal', 'normal', 'normal', 'normal', 'normal', 'last-valid', 'last-valid', 'normal', 'normal'] },
  decisions: [
    { minute: 0, swaps: [] },
    { minute: 4, swaps: [{ charger: 'C1', out: 'B1', in: 'B2', reason: 'troca' }] },
  ],
  outcomes: [
    { id: 'B1', departed: true, ready: true, departure: 7 },
    { id: 'B2', departed: true, ready: false, departure: 8 },
    { id: 'B3', departed: false, ready: false, departure: 9 },
  ],
};

test('events in (from, to] are returned in minute order', () => {
  const ev = eventsBetween(data, 0, 9);
  assert.deepEqual(ev.map((e) => [e.minute, e.type]), [[4, 'swap'], [6, 'layer'], [7, 'depart'], [8, 'layer'], [8, 'depart']]);
});

test('departures say whether the bus was ready; buses that never left do not depart', () => {
  const dep = eventsBetween(data, 0, 9).filter((e) => e.type === 'depart');
  assert.deepEqual(dep.map((e) => [e.bus, e.ok]), [['B1', true], ['B2', false]]);
});

test('layer events name the layer and only transitions count', () => {
  const layers = eventsBetween(data, 0, 9).filter((e) => e.type === 'layer');
  assert.equal(layers.length, 2);
  assert.match(layers[0].text, /último plano válido/);
  assert.match(layers[1].text, /normal/);
});

test('only power-related faults are events', () => {
  const ev = eventsBetween(data, 15, 25);
  assert.deepEqual(ev.map((e) => e.type), ['fault']);
  assert.match(ev[0].text, /queda do limite da rede/);
});

test('the window is open at the start and closed at the end', () => {
  assert.equal(eventsBetween(data, 4, 5).length, 0);
  assert.equal(eventsBetween(data, 3, 4).length, 1);
});

test('a jump of the cursor produces no events', () => {
  assert.deepEqual(eventsBetween(data, 0, 100), []);
  assert.deepEqual(eventsBetween(data, 0, 100, 200).length > 0, true);
  assert.deepEqual(eventsBetween(data, 9, 3), []);
});

test('a playback that starts at minute 0 can ask from -1 and still sees minute 0', () => {
  const d = { ...data, decisions: [{ minute: 0, swaps: [{ charger: 'C1', out: 'B1', in: 'B2', reason: 'x' }] }] };
  assert.equal(eventsBetween(d, 0, 5).filter((e) => e.type === 'swap').length, 0);
  assert.equal(eventsBetween(d, -1, 5).filter((e) => e.type === 'swap').length, 1);
});

test('decisions of controllers without swaps and runs without layers do not break it', () => {
  const bare = { scenario: { faults: [] }, series: { layer: [] }, decisions: [{ minute: 3 }], outcomes: [] };
  assert.deepEqual(eventsBetween(bare, 0, 10), []);
});

test('crossed lists the markers the cursor passed, in (from, to], and ignores jumps', () => {
  const marks = [{ minute: 2 }, { minute: 5 }, { minute: 5 }, { minute: 9 }, { minute: 40 }];
  assert.deepEqual(crossed(marks, 2, 9).map((m) => m.minute), [5, 5, 9]);
  assert.deepEqual(crossed(marks, 5, 5), []);
  assert.deepEqual(crossed(marks, 9, 2), []);
  assert.deepEqual(crossed(marks, 0, 40), []); // a scrub, not playback
  assert.deepEqual(crossed(marks, 0, 40, 100).map((m) => m.minute), [2, 5, 5, 9, 40]);
  assert.deepEqual(crossed([], 0, 5), []);
});

test('crossedPlayback treats a wrap-around as a restart from before minute 0', () => {
  const marks = [{ minute: 0 }, { minute: 1 }, { minute: 3 }, { minute: 50 }];
  assert.deepEqual(crossedPlayback(marks, 1, 3).map((m) => m.minute), [3]); // normal playback
  assert.deepEqual(crossedPlayback(marks, 99, 2, 30).map((m) => m.minute), [0, 1]); // looped to minute 2
  assert.deepEqual(crossedPlayback(marks, 99, 60, 30), []); // a backward scrub far from 0 pops nothing
});

// a day of 1000 minutes with a departure every 10 minutes
const busy = {
  scenario: { faults: [] },
  series: { layer: [] },
  decisions: [],
  outcomes: Array.from({ length: 100 }, (_, i) => ({ id: `B${i}`, departed: true, ready: true, departure: i * 10 })),
};

test('feedUpdate appends while playback advances by less than the span', () => {
  const u = feedUpdate(busy, 100, 120, { span: 30, max: 6 });
  assert.equal(u.rebuild, false);
  assert.deepEqual(u.events.map((e) => e.minute), [110, 120]);
});

test('feedUpdate rebuilds on play, on a backward seek and on a forward jump past the span', () => {
  const play = feedUpdate(busy, 800, 200, { span: 30, max: 6, rebuild: true });
  assert.equal(play.rebuild, true);
  assert.deepEqual(play.events.map((e) => e.minute), [150, 160, 170, 180, 190, 200]); // newest `max`, in order
  assert.equal(play.total, 21); // departures at 0, 10, ..., 200: the counter is everything up to the cursor
  const back = feedUpdate(busy, 800, 200, { span: 30, max: 6 });
  assert.equal(back.rebuild, true);
  assert.equal(back.total, 21);
  const jump = feedUpdate(busy, 100, 700, { span: 30, max: 6 });
  assert.equal(jump.rebuild, true);
  assert.equal(jump.total, 71);
});

test('a rebuild at minute 0 still sees minute 0 and an empty cursor gives an empty feed', () => {
  const r = feedUpdate(busy, -1, 0, { span: 30, max: 6, rebuild: true });
  assert.deepEqual(r.events.map((e) => e.minute), [0]);
  assert.equal(r.total, 1);
  const none = feedUpdate({ ...busy, outcomes: [] }, -1, 0, { rebuild: true });
  assert.deepEqual(none, { rebuild: true, events: [], total: 0 });
});
