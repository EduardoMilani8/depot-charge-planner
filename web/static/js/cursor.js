// createCursor is the shared time cursor: an integer minute in [0, max].
export function createCursor(max) {
  let value = 0;
  const subs = new Set();
  const cursor = {
    max,
    get value() { return value; },
    set(v) {
      if (!Number.isFinite(v)) return;
      const n = Math.min(max, Math.max(0, Math.round(v)));
      if (n === value) return;
      value = n;
      subs.forEach((fn) => fn(n));
    },
    step(d) { cursor.set(value + d); },
    onChange(fn) { subs.add(fn); return () => subs.delete(fn); },
  };
  return cursor;
}

export function keyDelta(key, shift) {
  switch (key) {
    case 'ArrowRight': case 'ArrowUp': return shift ? 10 : 1;
    case 'ArrowLeft': case 'ArrowDown': return shift ? -10 : -1;
    case 'PageUp': return 60;
    case 'PageDown': return -60;
    default: return null;
  }
}

// bindCursorKeys lets a focusable element drive the cursor with the keyboard.
export function bindCursorKeys(el, cursor) {
  el.addEventListener('keydown', (e) => {
    if (e.key === 'Home') { cursor.set(0); e.preventDefault(); return; }
    if (e.key === 'End') { cursor.set(cursor.max); e.preventDefault(); return; }
    const d = keyDelta(e.key, e.shiftKey);
    if (d !== null) { cursor.step(d); e.preventDefault(); }
  });
}
