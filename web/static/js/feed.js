import { h } from './dom.js';
import { clock } from './format.js';

export const FEED_MAX = 6;

const ICON = { depart_ok: '●', depart_fail: '✗', swap: '⇄', layer: '⚙', fault: '⚠' };
const KIND_LABEL = { depart_ok: 'saída', depart_fail: 'saída sem a carga', swap: 'rodízio', layer: 'camada', fault: 'falha' };

// feedKind is the visual kind of an event (a departure is ok or not).
export function feedKind(e) {
  return e.type === 'depart' ? (e.ok ? 'depart_ok' : 'depart_fail') : e.type;
}

// createFeedModel keeps the latest `max` events, newest first. push() says which items were
// added and which fell off the end so a view only touches those.
export function createFeedModel(max = FEED_MAX) {
  let items = [];
  let nextId = 1;
  let total = 0;
  return {
    get items() { return items; },
    get total() { return total; },
    push(events) {
      if (events.length === 0) return { added: [], removed: [] };
      total += events.length;
      // Only the newest `max` can stay, so do not build the rest.
      const fresh = events.slice(-max).map((e) => ({ ...e, id: nextId++, kind: feedKind(e) }));
      const merged = [...fresh].reverse().concat(items);
      items = merged.slice(0, max);
      return { added: fresh, removed: merged.slice(max).filter((i) => !fresh.includes(i)) };
    },
    clear() { items = []; total = 0; },
    // reset replaces everything with `events` (the newest ones) and a counter of `count` events.
    reset(events, count = events.length) {
      items = [];
      const r = this.push(events);
      total = count;
      return r;
    },
  };
}

// createFeed draws the list of the latest events (newest first); items rise in via CSS.
// opts: { max, startClock } (startClock adds the wall-clock time of each event).
export function createFeed(root, opts = {}) {
  const model = createFeedModel(opts.max ?? FEED_MAX);
  const counter = h('span', { class: 'feed-count' });
  const empty = h('li', { class: 'feed-empty' }, 'Os acontecimentos do dia aparecem aqui enquanto ele toca.');
  // aria-live stays off on purpose: at high speed the feed would flood a screen reader. The
  // same information is on the timelines and in the decision panel.
  const list = h('ul', { class: 'feed', 'aria-live': 'off' }, empty);
  root.replaceChildren(h('h3', {}, 'Acontecimentos ', counter), list);
  const nodes = new Map();
  const paintCount = () => { counter.textContent = model.total > 0 ? `(${model.total} até aqui)` : ''; };
  const node = (e) => h('li', { class: `feed-item feed-${e.kind}` },
    h('span', { class: 'feed-icon', 'aria-hidden': 'true' }, ICON[e.kind]),
    opts.startClock !== undefined ? h('span', { class: 'feed-time' }, clock(opts.startClock, e.minute)) : null,
    h('span', { class: 'sr' }, `${KIND_LABEL[e.kind]}: `), e.text);
  return {
    push(events) {
      const { added, removed } = model.push(events);
      if (added.length === 0) return;
      empty.remove();
      for (const e of removed) { nodes.get(e.id)?.remove(); nodes.delete(e.id); }
      for (const e of added) { const li = node(e); nodes.set(e.id, li); list.prepend(li); }
      paintCount();
    },
    reset(events, count) {
      model.clear();
      nodes.clear();
      list.replaceChildren(empty);
      const { added } = model.reset(events, count);
      if (added.length > 0) {
        empty.remove();
        for (const e of added) { const li = node(e); nodes.set(e.id, li); list.prepend(li); }
      }
      paintCount();
    },
    clear() {
      model.clear();
      nodes.clear();
      list.replaceChildren(empty);
      paintCount();
    },
  };
}
