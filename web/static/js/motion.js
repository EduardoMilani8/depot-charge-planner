// Motion helpers: easing, the animations switch, counters, throttles and the day player.
// Everything that touches the clock or the frame scheduler takes `deps` so it can be
// tested without a browser.

export const easeOutCubic = (t) => 1 - (1 - t) ** 3;
export const lerp = (a, b, t) => a + (b - a) * t;

const KEY = 'lab-animations';

// When storage refuses the write (private mode, blocked site data) the switch is kept here, so
// the toggle still works until the page is closed. A write that works clears it.
let override = null;

// animationsEnabled: the user's switch ('on'/'off') wins; otherwise follow the system's
// "reduce motion" preference.
export function animationsEnabled() {
  if (override !== null) return override;
  let stored = null;
  try { stored = localStorage.getItem(KEY); } catch { /* storage blocked */ }
  if (stored === 'on') return true;
  if (stored === 'off') return false;
  return !(typeof matchMedia === 'function' && matchMedia('(prefers-reduced-motion: reduce)').matches);
}

// applyMotionClass tells the CSS: `no-motion` kills every animation and transition;
// `motion-on` lifts the system "reduce motion" rule when the user asked for motion.
export function applyMotionClass() {
  const on = animationsEnabled();
  const root = document.documentElement;
  root.classList.toggle('no-motion', !on);
  root.classList.toggle('motion-on', on);
}

export function setAnimations(on) {
  try { localStorage.setItem(KEY, on ? 'on' : 'off'); override = null; } catch { override = Boolean(on); }
  applyMotionClass();
}

const clocks = (deps) => ({
  now: deps.now || (() => performance.now()),
  raf: deps.raf || ((f) => requestAnimationFrame(f)),
  caf: deps.caf || ((id) => cancelAnimationFrame(id)),
});

// staggerStep is the delay between consecutive items of a cascade of n: `stepMs`, but small
// enough that the whole cascade (n - 1 gaps) stays within `capMs` however many items there are.
export function staggerStep(n, capMs = 350, stepMs = 25) {
  return n > 1 ? Math.min(stepMs, capMs / (n - 1)) : stepMs;
}

// countUpValue is the number shown `elapsed` ms after the count started (after `delay`).
export function countUpValue(from, to, elapsed, ms, delay = 0) {
  const t = ms <= 0 ? 1 : Math.min(1, Math.max(0, (elapsed - delay) / ms));
  return lerp(from, to, easeOutCubic(t));
}

// countUp animates the text of `el` from `from` to `to` (instantly when animations are off
// or the target is not a number). It returns a cancel function that stops the frames and
// snaps to the final text, so a number is never left half counted.
// opts: { delay (ms), enabled, now, raf, caf }.
export function countUp(el, to, fmt, ms = 700, from = 0, opts = {}) {
  const enabled = opts.enabled ?? animationsEnabled();
  if (!enabled || !Number.isFinite(to) || !Number.isFinite(from)) {
    el.textContent = fmt(to);
    return () => {};
  }
  const { now, raf, caf } = clocks(opts);
  const delay = opts.delay || 0;
  const t0 = now();
  let id = null;
  let done = false;
  const finish = () => { done = true; id = null; el.textContent = fmt(to); };
  const step = () => {
    const elapsed = now() - t0;
    if (elapsed - delay >= ms) { finish(); return; }
    el.textContent = fmt(countUpValue(from, to, elapsed, ms, delay));
    id = raf(step);
  };
  el.textContent = fmt(from);
  id = raf(step);
  return () => {
    if (done) return;
    if (id !== null) caf(id);
    finish();
  };
}

// countingText builds a number that counts up. The animated digits are hidden from
// assistive technology; the final value is always there in text for it.
export function countingText(value, fmt, ms = 700, opts = {}) {
  const node = document.createElement('span');
  if (!(opts.enabled ?? animationsEnabled()) || !Number.isFinite(value)) {
    node.textContent = fmt(value);
    return { node, cancel() {} };
  }
  const shown = document.createElement('span');
  shown.setAttribute('aria-hidden', 'true');
  const final = document.createElement('span');
  final.className = 'sr';
  final.textContent = fmt(value);
  node.append(shown, final);
  return { node, cancel: countUp(shown, value, fmt, ms, 0, opts) };
}

// throttle runs fn at once when it has been idle for `ms`, otherwise at most once per `ms`,
// with a trailing call so the last arguments are never lost. It polls with animation
// frames instead of timers, so nothing runs while the tab is hidden. cancel() drops the
// pending call; flush() runs it now.
export function throttle(fn, ms, deps = {}) {
  const { now, raf, caf } = clocks(deps);
  let last = -Infinity;
  let pending = null;
  let id = null;
  const run = () => { const args = pending; pending = null; last = now(); fn(...args); };
  const tick = () => {
    id = null;
    if (pending === null) return;
    if (now() - last >= ms) run(); else id = raf(tick);
  };
  const call = (...args) => {
    pending = args;
    if (id !== null) return;
    if (now() - last >= ms) run(); else id = raf(tick);
  };
  call.cancel = () => { if (id !== null) caf(id); id = null; pending = null; };
  call.flush = () => {
    if (id !== null) caf(id);
    id = null;
    if (pending !== null) run();
  };
  return call;
}

// frameThrottle coalesces calls into one per animation frame (the latest arguments win).
export function frameThrottle(fn, deps = {}) {
  const { raf, caf } = clocks(deps);
  let pending = null;
  let id = null;
  const run = () => { const args = pending; pending = null; fn(...args); };
  const call = (...args) => {
    pending = args;
    if (id === null) id = raf(() => { id = null; if (pending !== null) run(); });
  };
  call.cancel = () => { if (id !== null) caf(id); id = null; pending = null; };
  call.flush = () => {
    if (id !== null) caf(id);
    id = null;
    if (pending !== null) run();
  };
  return call;
}

const MAX_FRAME_MS = 250;

// createPlayer plays the day: it moves the cursor `speed` simulated minutes per real
// second. deps (now, raf, caf) can be replaced to test it without a browser.
export function createPlayer(cursor, deps = {}) {
  const { now, raf, caf } = clocks(deps);
  const state = { playing: false, speed: 60, loop: false };
  const subs = new Set();
  let frameId = null;
  let last = 0;
  let acc = 0;
  let destroyed = false;
  const emit = () => subs.forEach((fn) => fn({ ...state }));

  function frame() {
    frameId = null;
    if (!state.playing) return;
    const t = now();
    // A frame that comes very late (hidden tab, a long pause of the page) must not jump the day
    // ahead and skip its events: it counts as at most MAX_FRAME_MS.
    acc += (Math.min(MAX_FRAME_MS, Math.max(0, t - last)) / 1000) * state.speed;
    last = t;
    const whole = Math.floor(acc);
    if (whole > 0) {
      acc -= whole;
      const next = cursor.value + whole;
      if (next >= cursor.max) {
        if (state.loop) cursor.set(next % (cursor.max + 1));
        else { cursor.set(cursor.max); pause(); return; }
      } else cursor.set(next);
    }
    if (state.playing) frameId = raf(frame); // a listener may have paused it
  }

  function play() {
    if (destroyed || state.playing) return;
    if (cursor.value >= cursor.max) cursor.set(0);
    state.playing = true;
    last = now();
    acc = 0;
    emit();
    if (state.playing && frameId === null) frameId = raf(frame);
  }
  function pause() {
    if (!state.playing) return;
    state.playing = false;
    if (frameId !== null) caf(frameId);
    frameId = null;
    emit();
  }
  return {
    play, pause,
    toggle() { if (state.playing) pause(); else play(); },
    restart() { pause(); cursor.set(0); play(); },
    setSpeed(v) { if (Number.isFinite(v) && v > 0) { state.speed = v; emit(); } },
    setLoop(v) { state.loop = Boolean(v); emit(); },
    onChange(fn) { subs.add(fn); return () => subs.delete(fn); },
    // destroy stops the frame loop for good and forgets the listeners.
    destroy() {
      destroyed = true;
      state.playing = false;
      if (frameId !== null) caf(frameId);
      frameId = null;
      subs.clear();
    },
    get state() { return { ...state }; },
  };
}
