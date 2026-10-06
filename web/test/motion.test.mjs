import test from 'node:test';
import assert from 'node:assert/strict';
import {
  easeOutCubic, lerp, createPlayer, countUpValue, countUp, throttle, frameThrottle, animationsEnabled,
} from '../static/js/motion.js';
import { createCursor } from '../static/js/cursor.js';

test('easing and interpolation', () => {
  assert.equal(easeOutCubic(0), 0);
  assert.equal(easeOutCubic(1), 1);
  assert.ok(easeOutCubic(0.5) > 0.5);
  assert.equal(lerp(10, 20, 0.5), 15);
});

// a controllable clock and frame scheduler
function fakeClock() {
  let t = 0;
  let next = 1;
  const pending = new Map();
  return {
    deps: { now: () => t, raf: (f) => { const id = next++; pending.set(id, f); return id; }, caf: (id) => pending.delete(id) },
    advance(ms) {
      t += ms;
      const frames = [...pending.values()];
      pending.clear();
      frames.forEach((f) => f());
    },
    pendingFrames: () => pending.size,
  };
}

test('playing advances the cursor by speed minutes per second', () => {
  const clock = fakeClock();
  const cursor = createCursor(1000);
  const p = createPlayer(cursor, clock.deps);
  p.setSpeed(60);
  p.play();
  clock.advance(1000);
  assert.equal(cursor.value, 60);
  clock.advance(500);
  assert.equal(cursor.value, 90);
  assert.equal(p.state.playing, true);
});

test('fractions of a minute accumulate instead of being lost', () => {
  const clock = fakeClock();
  const cursor = createCursor(1000);
  const p = createPlayer(cursor, clock.deps);
  p.setSpeed(30);
  p.play();
  for (let i = 0; i < 10; i++) clock.advance(100); // 1 s in ten frames
  assert.equal(cursor.value, 30);
});

test('pause stops the cursor and play resumes from there', () => {
  const clock = fakeClock();
  const cursor = createCursor(1000);
  const p = createPlayer(cursor, clock.deps);
  p.setSpeed(60);
  p.play();
  clock.advance(1000);
  p.pause();
  clock.advance(5000);
  assert.equal(cursor.value, 60);
  assert.equal(clock.pendingFrames(), 0);
  p.play();
  clock.advance(1000);
  assert.equal(cursor.value, 120);
});

test('reaching the end pauses, and play at the end restarts from zero', () => {
  const clock = fakeClock();
  const cursor = createCursor(100);
  const p = createPlayer(cursor, clock.deps);
  p.setSpeed(600);
  p.play();
  clock.advance(1000);
  assert.equal(cursor.value, 100);
  assert.equal(p.state.playing, false);
  p.play();
  assert.equal(cursor.value, 0);
  assert.equal(p.state.playing, true);
});

test('loop wraps around instead of stopping', () => {
  const clock = fakeClock();
  const cursor = createCursor(100);
  const p = createPlayer(cursor, clock.deps);
  p.setSpeed(60);
  p.setLoop(true);
  p.play();
  clock.advance(2000); // 120 minutes of a 100-minute day
  assert.equal(p.state.playing, true);
  assert.ok(cursor.value < 100);
});

test('invalid speeds are ignored and listeners are told about changes', () => {
  const clock = fakeClock();
  const cursor = createCursor(100);
  const p = createPlayer(cursor, clock.deps);
  const seen = [];
  const off = p.onChange((s) => seen.push({ ...s }));
  p.setSpeed(0);
  p.setSpeed(NaN);
  p.setSpeed(-5);
  assert.equal(p.state.speed, 60);
  p.setSpeed(120);
  p.play();
  p.toggle();
  off();
  p.toggle();
  assert.deepEqual(seen.map((s) => [s.playing, s.speed]), [[false, 120], [true, 120], [false, 120]]);
});

test('restart goes back to minute 0 and plays', () => {
  const clock = fakeClock();
  const cursor = createCursor(100);
  cursor.set(50);
  const p = createPlayer(cursor, clock.deps);
  p.restart();
  assert.equal(cursor.value, 0);
  assert.equal(p.state.playing, true);
});

test('destroy stops the frame loop and drops the listeners', () => {
  const clock = fakeClock();
  const cursor = createCursor(1000);
  const p = createPlayer(cursor, clock.deps);
  const seen = [];
  p.onChange((s) => seen.push(s.playing));
  p.play();
  p.destroy();
  assert.equal(clock.pendingFrames(), 0);
  clock.advance(1000);
  assert.equal(cursor.value, 0);
  assert.equal(p.state.playing, false);
  p.play(); // a destroyed player stays quiet
  assert.equal(clock.pendingFrames(), 0);
  assert.deepEqual(seen, [true]);
});

test('countUpValue eases from the start to the end and honours the delay', () => {
  assert.equal(countUpValue(0, 100, 0, 1000), 0);
  assert.equal(countUpValue(0, 100, 1000, 1000), 100);
  assert.equal(countUpValue(0, 100, 5000, 1000), 100);
  assert.ok(countUpValue(0, 100, 500, 1000) > 50);
  assert.equal(countUpValue(0, 100, 200, 1000, 300), 0); // still waiting
  assert.equal(countUpValue(20, 80, 1300, 1000, 300), 80);
  assert.equal(countUpValue(0, 100, 10, 0), 100); // zero duration lands at once
});

function fakeEl() { return { textContent: '' }; }

test('countUp runs from the start to the exact end value and stops', () => {
  const clock = fakeClock();
  const el = fakeEl();
  const texts = [];
  const fmt = (v) => { const t = v.toFixed(1); texts.push(t); return t; };
  countUp(el, 80, fmt, 1000, 0, clock.deps);
  assert.equal(el.textContent, '0.0');
  clock.advance(500);
  const mid = Number(el.textContent);
  assert.ok(mid > 40 && mid < 80);
  clock.advance(600);
  assert.equal(el.textContent, '80.0');
  assert.equal(clock.pendingFrames(), 0);
});

test('countUp is instant when animations are off or the target is not a number', () => {
  const clock = fakeClock();
  const el = fakeEl();
  countUp(el, 42, (v) => `n${v}`, 1000, 0, { ...clock.deps, enabled: false });
  assert.equal(el.textContent, 'n42');
  assert.equal(clock.pendingFrames(), 0);
  countUp(el, NaN, (v) => `n${v}`, 1000, 0, clock.deps);
  assert.equal(el.textContent, 'nNaN');
  assert.equal(clock.pendingFrames(), 0);
});

test('cancelling countUp stops the frames and snaps to the final value', () => {
  const clock = fakeClock();
  const el = fakeEl();
  const cancel = countUp(el, 10, (v) => String(Math.round(v)), 1000, 0, clock.deps);
  clock.advance(100);
  cancel();
  assert.equal(el.textContent, '10');
  assert.equal(clock.pendingFrames(), 0);
  cancel(); // harmless twice
});

test('countUp waits for its delay before moving', () => {
  const clock = fakeClock();
  const el = fakeEl();
  countUp(el, 100, (v) => String(Math.round(v)), 500, 0, { ...clock.deps, delay: 300 });
  clock.advance(200);
  assert.equal(el.textContent, '0');
  clock.advance(700);
  assert.equal(el.textContent, '100');
});

test('throttle runs at once, then at most once per interval, and the last call is never lost', () => {
  const clock = fakeClock();
  const seen = [];
  const t = throttle((v) => seen.push(v), 100, clock.deps);
  t(1);
  assert.deepEqual(seen, [1]); // leading call is immediate
  t(2);
  t(3);
  clock.advance(40);
  assert.deepEqual(seen, [1]);
  clock.advance(40);
  assert.deepEqual(seen, [1]);
  clock.advance(40); // 120 ms since the first call
  assert.deepEqual(seen, [1, 3]);
  clock.advance(500);
  assert.deepEqual(seen, [1, 3]);
  t(4); // idle for long enough: immediate again
  assert.deepEqual(seen, [1, 3, 4]);
});

test('throttle cancel drops the pending call and flush runs it now', () => {
  const clock = fakeClock();
  const seen = [];
  const t = throttle((v) => seen.push(v), 100, clock.deps);
  t(1); t(2);
  t.cancel();
  clock.advance(1000);
  assert.deepEqual(seen, [1]);
  assert.equal(clock.pendingFrames(), 0);
  t(3); t(4);
  t.flush();
  assert.deepEqual(seen, [1, 3, 4]); // 3 ran at once (idle), flush ran the pending 4
  assert.equal(clock.pendingFrames(), 0);
});

test('frameThrottle coalesces calls into one per frame, using the latest arguments', () => {
  const clock = fakeClock();
  const seen = [];
  const f = frameThrottle((v) => seen.push(v), clock.deps);
  f(1); f(2); f(3);
  assert.deepEqual(seen, []);
  clock.advance(16);
  assert.deepEqual(seen, [3]);
  f(4);
  f.cancel();
  clock.advance(16);
  assert.deepEqual(seen, [3]);
  f(5);
  f.flush();
  assert.deepEqual(seen, [3, 5]);
  assert.equal(clock.pendingFrames(), 0);
});

function withGlobals(globals, fn) {
  const saved = {};
  for (const k of Object.keys(globals)) { saved[k] = Object.getOwnPropertyDescriptor(globalThis, k); Object.defineProperty(globalThis, k, { value: globals[k], configurable: true, writable: true }); }
  try { fn(); } finally {
    for (const k of Object.keys(globals)) { if (saved[k]) Object.defineProperty(globalThis, k, saved[k]); else delete globalThis[k]; }
  }
}
const store = (value) => ({ getItem: () => value, setItem() {} });
const media = (reduce) => () => ({ matches: reduce });

test('the stored switch wins over the system preference; without it the system decides', () => {
  withGlobals({ localStorage: store('on'), matchMedia: media(true) }, () => assert.equal(animationsEnabled(), true));
  withGlobals({ localStorage: store('off'), matchMedia: media(false) }, () => assert.equal(animationsEnabled(), false));
  withGlobals({ localStorage: store(null), matchMedia: media(true) }, () => assert.equal(animationsEnabled(), false));
  withGlobals({ localStorage: store(null), matchMedia: media(false) }, () => assert.equal(animationsEnabled(), true));
});

test('blocked storage falls back to the system preference', () => {
  const blocked = { getItem() { throw new Error('denied'); }, setItem() { throw new Error('denied'); } };
  withGlobals({ localStorage: blocked, matchMedia: media(true) }, () => assert.equal(animationsEnabled(), false));
  withGlobals({ localStorage: blocked, matchMedia: media(false) }, () => assert.equal(animationsEnabled(), true));
});
