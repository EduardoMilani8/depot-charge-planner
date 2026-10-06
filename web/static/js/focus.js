// Keyboard focus across a reload of the run view. Reloading clears the view, which sends
// focus to <body>; the control that asked for the reload must get it back so a keyboard user can
// keep stepping (a closed <select> fires `change` on every arrow key).

// nextFocusTarget returns what should get focus once the reload is drawn: { heading: true } for
// a run opened from elsewhere (a seed dot), { id } for the control that had focus inside the
// view, the `pending` target while another reload is already in flight (focus is on <body> by
// then), or null for no focus change.
export function nextFocusTarget(pending, { activeId, inside, heading = false }) {
  if (heading) return { heading: true };
  if (inside && activeId) return { id: activeId };
  return pending ?? null;
}

// restoreFocus gives `target` focus within `root` (the heading carries id `heading`).
export function restoreFocus(root, target, headingSelector = 'h2') {
  if (!target) return;
  if (target.heading) root.querySelector(headingSelector)?.focus(); // scrolls to it: the person asked to go there
  else root.querySelector(`#${CSS.escape(target.id)}`)?.focus({ preventScroll: true }); // the control was already in view
}
