import { POWER_FAULTS, LAYER_LABEL, describeFault } from './glossary.js';

// Order of events inside one minute.
const RANK = { fault: 0, layer: 1, swap: 2, depart: 3 };

// eventsBetween lists what happened in the minutes (from, to]: departures, recommended
// swaps, layer changes and power faults. A jump longer than maxSpan is a scrub, not a
// playback, and yields nothing. A playback that starts at minute 0 asks from -1 so that
// minute 0 is not lost.
export function eventsBetween(data, from, to, maxSpan = 30) {
  if (!(to > from) || to - from > maxSpan) return [];
  const inRange = (m) => m > from && m <= to;
  const out = [];
  for (const o of data.outcomes) {
    if (o.departed && inRange(o.departure)) {
      out.push({ minute: o.departure, type: 'depart', ok: o.ready, bus: o.id,
        text: o.ready ? `${o.id} saiu pronto` : `${o.id} saiu sem a carga` });
    }
  }
  for (const d of data.decisions) {
    if (!inRange(d.minute)) continue;
    for (const sw of d.swaps ?? []) {
      out.push({ minute: d.minute, type: 'swap', bus: sw.in, text: `rodízio recomendado: ${sw.in} assume ${sw.charger} no lugar de ${sw.out}` });
    }
  }
  const layers = data.series.layer;
  for (let i = Math.max(1, from + 1); i <= to && i < layers.length; i++) {
    if (layers[i] !== layers[i - 1]) {
      out.push({ minute: i, type: 'layer', layer: layers[i], text: `o planejador passou para: ${LAYER_LABEL[layers[i]] || layers[i]}` });
    }
  }
  for (const f of data.scenario.faults) {
    if (POWER_FAULTS.has(f.kind) && inRange(f.from)) out.push({ minute: f.from, type: 'fault', text: describeFault(f) });
  }
  return out.sort((a, b) => a.minute - b.minute || RANK[a.type] - RANK[b.type]);
}

// crossed returns the markers (sorted by `minute`) the cursor passed going from `from` to
// `to`, that is minute in (from, to]. Longer than maxSpan is a scrub and yields nothing.
export function crossed(markers, from, to, maxSpan = 30) {
  if (!(to > from) || to - from > maxSpan) return [];
  let lo = 0;
  let hi = markers.length;
  while (lo < hi) { // first marker after `from`
    const mid = (lo + hi) >> 1;
    if (markers[mid].minute > from) hi = mid; else lo = mid + 1;
  }
  const out = [];
  for (let i = lo; i < markers.length && markers[i].minute <= to; i++) out.push(markers[i]);
  return out;
}

// crossedPlayback is crossed() for a cursor that is playing: a move backwards is a wrap-around
// ("repetir"), so it starts over from before minute 0 like the feed does and the marks of the
// first minutes after the wrap are not skipped. A backward scrub far from 0 is longer than
// maxSpan and yields nothing.
export function crossedPlayback(markers, from, to, maxSpan = 30) {
  return crossed(markers, to < from ? -1 : from, to, maxSpan);
}

// feedUpdate says how the event feed gets from the cursor at `from` to the one at `to`.
// While playback moves forward by at most `span` minutes it only appends what happened in
// between ({ rebuild: false, events }). Anything else (play starting, a seek backwards or a
// jump forward past the span, or `rebuild: true`) must redraw the feed as it would look had
// the day played up to `to`: { rebuild: true, events: the newest `max`, total: all of them }.
export function feedUpdate(data, from, to, { span = 30, max = 6, rebuild = false } = {}) {
  if (rebuild || to < from || to - from > span) {
    const all = eventsBetween(data, -1, to, Infinity);
    return { rebuild: true, events: all.slice(-max), total: all.length };
  }
  return { rebuild: false, events: eventsBetween(data, from, to, span), total: null };
}
