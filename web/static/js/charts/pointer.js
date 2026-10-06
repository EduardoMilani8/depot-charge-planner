import { frameThrottle } from '../motion.js';
import { MARGIN } from './layout.js';

// bindPlotPointer lets a pointer press or drag on the plot of a time chart move the cursor
// (the label column and the right margin do not). `onRow` hears the row (data-key) that was
// pressed. Moves are coalesced to one per frame; the returned function stops the pending one.
// The handlers sit on the svg itself, not on an overlay, so the <title> tooltips of the
// bars, runs and faults underneath stay reachable.
export function bindPlotPointer(svg, x, width, cursor, onRow) {
  const toUnits = (clientX) => {
    const r = svg.getBoundingClientRect();
    return ((clientX - r.left) * width) / r.width;
  };
  const moveTo = frameThrottle((clientX) => cursor.set(x.invert(toUnits(clientX))));
  let dragging = false;
  svg.addEventListener('pointerdown', (e) => {
    if (e.button !== 0) return; // only the primary button selects or drags; right-click opens the context menu
    const row = e.target.closest('[data-key]');
    if (row && onRow) onRow(row.dataset.key);
    const u = toUnits(e.clientX);
    if (u < MARGIN.left || u > width - MARGIN.right) return;
    dragging = true;
    svg.setPointerCapture(e.pointerId);
    moveTo.cancel();
    cursor.set(x.invert(u));
  });
  svg.addEventListener('pointermove', (e) => { if (dragging && e.buttons) moveTo(e.clientX); });
  const stop = () => { dragging = false; moveTo.flush(); };
  svg.addEventListener('pointerup', stop);
  svg.addEventListener('pointercancel', stop);
  return () => moveTo.cancel();
}
