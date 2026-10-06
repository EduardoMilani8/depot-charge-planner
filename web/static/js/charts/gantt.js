import { s } from './svg.js';
import { h } from '../dom.js';
import { linear, timeTicks } from './scale.js';
import { MARGIN, runs, fillPath } from './layout.js';
import { chargerOccupancy, swapMarkers } from '../decision.js';
import { clock, fmtNum } from '../format.js';
import { describeFault } from '../glossary.js';
import { bindCursorKeys } from '../cursor.js';
import { bindPlotPointer } from './pointer.js';
import { crossedPlayback } from '../events.js';
import { animationsEnabled } from '../motion.js';

const W = 1000;
const ROW_H = 20;
const BUS_FAULTS = new Set(['soc_noise', 'soc_bias', 'soc_freeze', 'soc_missing', 'consumption_over', 'late_arrival', 'early_departure']);

function timeAxis(x, n, startClock, y) {
  const parts = [s('line', { x1: MARGIN.left, x2: W - MARGIN.right, y1: y, y2: y, class: 'axis' })];
  for (const m of timeTicks(n, 60)) {
    parts.push(s('line', { x1: x(m), x2: x(m), y1: y, y2: y + 4, class: 'axis' }),
      s('text', { x: x(m), y: y + 17, class: 'tick', 'text-anchor': 'middle' }, clock(startClock, m)));
  }
  return parts;
}

// cursorLine adds the vertical time cursor and keeps it at the cursor's minute; the returned
// function unsubscribes.
function cursorLine(parts, x, cursor, y1, y2) {
  const line = s('line', { y1, y2, class: 'cursor' });
  parts.push(line);
  const move = (m) => { line.setAttribute('x1', x(m)); line.setAttribute('x2', x(m)); };
  move(cursor.value);
  return cursor.onChange(move);
}

// pop restarts the entrance "pop" of a mark the cursor just passed (only with animations on).
function pop(node) {
  if (!animationsEnabled() || node.classList.contains('hit')) return;
  node.classList.add('hit');
  // animationcancel: the switch turned animations off mid-pop, so animationend never comes
  const done = () => {
    node.classList.remove('hit');
    node.removeEventListener('animationend', done);
    node.removeEventListener('animationcancel', done);
  };
  node.addEventListener('animationend', done);
  node.addEventListener('animationcancel', done);
}

const swatchSvg = (w, ...kids) => s('svg', { width: w, height: 12, 'aria-hidden': 'true' }, kids);
const legendItem = (symbol, text) => h('span', { class: 'legend-item' }, symbol, text);

// renderBusTimeline: one row per bus, from arrival to departure, filled with the true
// charge; a mark at departure says whether the bus was ready. With a `player`, buses being
// charged pulse, the fill is revealed up to the cursor while the day plays and the marks pop
// when the cursor passes them. Returns { setSelected, dispose }.
export function renderBusTimeline(root, data, cursor, hooks, player) {
  const n = data.scenario.horizon_min;
  const x = linear(0, n, MARGIN.left, W - MARGIN.right);
  const buses = data.series.buses;
  const outcomes = new Map(data.outcomes.map((o) => [o.id, o]));
  const top = MARGIN.top;
  const bottom = top + buses.length * ROW_H;
  const markers = swapMarkers(data.decisions);
  const rowNodes = new Map();
  const busClip = s('rect', { x: 0, y: 0, width: W, height: bottom + 26 });
  const parts = [s('defs', {}, s('clipPath', { id: 'clip-bus-past' }, busClip))];
  const bars = []; // { node, b }: the bus bars, to light up the ones being charged
  const fills = []; // the charge fills, clipped to the cursor while the day plays
  const pops = []; // { node, minute, id }: marks that pop when the cursor passes them

  buses.forEach((b, i) => {
    const o = outcomes.get(b.id);
    const y0 = top + i * ROW_H;
    const barY = y0 + 3;
    const barH = ROW_H - 6;
    const g = s('g', { class: 'row', 'data-key': b.id });
    g.append(s('rect', { x: 0, y: y0, width: W, height: ROW_H, class: 'row-bg' }));
    const label = s('text', { x: MARGIN.left - 8, y: y0 + ROW_H / 2 + 4, class: 'row-label', 'text-anchor': 'end', tabindex: 0, role: 'button', 'aria-label': `Ônibus ${b.id}: ver detalhes` }, b.id);
    label.addEventListener('keydown', (e) => { if (e.key === 'Enter' || e.key === ' ') { e.preventDefault(); e.stopPropagation(); hooks.onSelectBus(b.id); } });
    g.append(label);
    if (o) {
      const from = Math.max(0, o.arrival);
      const to = Math.min(n, o.departure);
      const bar = s('rect', { x: x(from), y: barY, width: Math.max(2, x(to) - x(from)), height: barH, class: 'bus-bar' });
      g.append(bar);
      bars.push({ node: bar, b, on: false });
      const frac = (v) => (Number.isFinite(v) ? v / o.capacity_kwh : null);
      const fill = s('path', { d: fillPath(b.true_soc_kwh.map(frac), x, (f) => barY + barH * (1 - f), barY + barH), class: 'soc-fill' });
      fills.push(fill);
      g.append(fill);
      const ty = barY + barH * (1 - o.forecast_target_kwh / o.capacity_kwh);
      g.append(s('line', { x1: x(from), x2: x(to), y1: ty, y2: ty, class: 'target-line' }, s('title', {}, `alvo previsto: ${fmtNum(o.forecast_target_kwh, 0)} kWh`)));
      const dx = x(to);
      const result = !o.departed ? 'não saiu dentro do horizonte' : o.ready ? 'saiu pronto' : `saiu sem a carga (faltaram ${fmtNum(o.shortfall_kwh, 0)} kWh)`;
      const title = s('title', {}, `${b.id}: ${result}`);
      if (!o.departed) g.append(s('circle', { cx: dx, cy: y0 + ROW_H / 2, r: 4, class: 'mark-pending' }, title));
      else {
        const mark = o.ready ? s('circle', { cx: dx, cy: y0 + ROW_H / 2, r: 4.5, class: 'mark-ready' }, title)
          : s('path', { d: `M${dx - 4},${y0 + ROW_H / 2 - 4}l8,8m0,-8l-8,8`, class: 'mark-fail' }, title);
        g.append(mark);
        pops.push({ node: mark, minute: o.departure });
      }
    }
    rowNodes.set(b.id, g);
    parts.push(g);
  });

  // bus-level faults and recommended swaps
  const rowOf = new Map(buses.map((b, i) => [b.id, top + i * ROW_H]));
  // Faults on every bus ("*") are a thin band above the first row (a lane per fault, up to three).
  let lane = 0;
  for (const f of data.scenario.faults) {
    if (!BUS_FAULTS.has(f.kind) || f.target !== '*') continue;
    const from = Math.max(0, f.from);
    const to = Math.min(n, f.to);
    if (to < from) continue;
    parts.push(s('rect', { x: x(from), y: 1 + Math.min(lane, 2) * 4, width: Math.max(2, x(to) - x(from)), height: 3, class: 'bus-fault-all' },
      s('title', {}, `${describeFault(f)}, todos os ônibus`)));
    lane++;
  }
  for (const f of data.scenario.faults) {
    if (!BUS_FAULTS.has(f.kind) || !rowOf.has(f.target)) continue;
    const y = rowOf.get(f.target);
    parts.push(s('path', { d: `M${x(f.from)},${y + 2}l4,0l-2,5z`, class: 'bus-fault' }, s('title', {}, describeFault(f))));
  }
  for (const m of markers) {
    if (!rowOf.has(m.bus)) continue;
    const y = rowOf.get(m.bus);
    const up = m.role === 'in';
    const tri = s('path', { d: up ? `M${x(m.minute)},${y + ROW_H - 2}l-4,0l2,-6z` : `M${x(m.minute)},${y + 2}l-4,0l2,6z`, class: up ? 'swap-in' : 'swap-out' },
      s('title', {}, `${up ? 'recebe' : 'cede'} o carregador ${m.charger}: ${m.reason}`));
    parts.push(tri);
    pops.push({ node: tri, minute: m.minute });
  }
  pops.sort((a, b) => a.minute - b.minute);
  parts.push(...timeAxis(x, n, data.scenario.start_clock_min, bottom));
  const offs = [cursorLine(parts, x, cursor, top, bottom)];
  const svg = s('svg', { viewBox: `0 0 ${W} ${bottom + 26}`, class: 'chart bus-timeline', role: 'group', 'aria-label': 'Linha do tempo dos ônibus' }, parts);
  offs.push(bindPlotPointer(svg, x, W, cursor, hooks.onSelectBus));

  // The wrapper is a plain group: its rows hold buttons, which a slider role would hide.
  const wrap = h('div', { class: 'chart-wrap', tabindex: 0, role: 'group', 'aria-label': 'Linhas do tempo dos ônibus: use as setas para mover o cursor de tempo' }, svg);
  bindCursorKeys(wrap, cursor);
  const picked = h('span', { class: 'picked' });
  const jump = h('button', { type: 'button', class: 'link', hidden: true, onclick: () => hooks.onJump?.() }, 'ver detalhes do ônibus');
  const legend = h('div', { class: 'legend' },
    legendItem(swatchSvg(22, s('rect', { width: 22, height: 12, class: 'bus-bar' }), s('rect', { y: 6, width: 22, height: 6, class: 'soc-fill' })), 'ônibus no pátio, preenchido pela carga real'),
    legendItem(swatchSvg(22, s('line', { x1: 0, x2: 22, y1: 6, y2: 6, class: 'target-line' })), 'alvo previsto'),
    legendItem(swatchSvg(12, s('circle', { cx: 6, cy: 6, r: 4.5, class: 'mark-ready' })), 'saiu pronto'),
    legendItem(swatchSvg(12, s('path', { d: 'M2,2l8,8m0,-8l-8,8', class: 'mark-fail' })), 'saiu sem a carga'),
    legendItem(swatchSvg(12, s('circle', { cx: 6, cy: 6, r: 4, class: 'mark-pending' })), 'não saiu dentro do horizonte'),
    legendItem(swatchSvg(12, s('path', { d: 'M1,11l10,0l-5,-9z', class: 'swap-in' })), 'recebe carregador (rodízio recomendado)'),
    legendItem(swatchSvg(12, s('path', { d: 'M1,1l10,0l-5,9z', class: 'swap-out' })), 'cede carregador (rodízio recomendado)'),
    legendItem(swatchSvg(12, s('path', { d: 'M1,1l10,0l-5,9z', class: 'bus-fault' })), 'falha do ônibus (leitura, consumo ou horário)'),
    legendItem(swatchSvg(22, s('rect', { y: 4, width: 22, height: 3, class: 'bus-fault-all' })), 'falha em todos os ônibus (faixa no topo)'));
  root.replaceChildren(h('h3', {}, 'Ônibus'),
    h('p', { class: 'note' }, 'Clique numa linha (ou no código do ônibus) para ver por que ele saiu pronto ou não. ', picked, jump),
    wrap, legend);

  // Live look at the cursor's minute: who is being charged, how much of the day is revealed,
  // and which marks the cursor just passed.
  const chargers = data.series.chargers;
  let lastMinute = cursor.value;
  const playing = () => Boolean(player && player.state.playing);
  function paint(m) {
    for (const bar of bars) {
      const ci = bar.b.charger[m];
      const on = bar.b.state[m] === 1 && ci >= 0 && chargers[ci].physical_kw[m] > 0;
      if (on !== bar.on) { bar.on = on; bar.node.classList.toggle('charging', on); }
    }
    busClip.setAttribute('width', playing() ? x(m) : W);
    if (playing()) for (const mk of crossedPlayback(pops, lastMinute, m, Math.max(30, player.state.speed / 2))) pop(mk.node);
    lastMinute = m;
  }
  let revealed = playing();
  function setReveal() {
    revealed = playing();
    const attr = revealed;
    for (const f of fills) { if (attr) f.setAttribute('clip-path', 'url(#clip-bus-past)'); else f.removeAttribute('clip-path'); }
    paint(cursor.value);
  }
  offs.push(cursor.onChange(paint));
  // speed and loop changes also emit; the reveal only changes when playback starts or stops
  if (player) offs.push(player.onChange((s) => { if (s.playing !== revealed) setReveal(); }));
  paint(cursor.value);

  return {
    setSelected(id) {
      for (const [k, g] of rowNodes) g.classList.toggle('selected', k === id);
      const known = id !== null && rowNodes.has(id);
      picked.textContent = known ? `Selecionado: ${id}. ` : '';
      jump.hidden = !known;
    },
    dispose() { offs.forEach((off) => off()); },
  };
}

// renderChargerTimeline: one row per charger with the bus plugged in, its power and
// the failure windows. A dashed flow marks the chargers delivering power at the cursor's
// minute and chargers in failure flicker; the CSS moves both only while the day plays (the
// `playing` class of the run view), so no player is needed here. Returns { dispose }.
export function renderChargerTimeline(root, data, cursor) {
  const n = data.scenario.horizon_min;
  const startClock = data.scenario.start_clock_min;
  const x = linear(0, n, MARGIN.left, W - MARGIN.right);
  const chargers = data.series.chargers;
  const occupancy = chargerOccupancy(data);
  const ROW = 18;
  const top = MARGIN.top;
  const bottom = top + chargers.length * ROW;
  const parts = [s('defs', {},
    s('pattern', { id: 'pat-fail', width: 6, height: 6, patternUnits: 'userSpaceOnUse', patternTransform: 'rotate(45)' },
      s('rect', { width: 6, height: 6, class: 'pat-fail-bg' }), s('line', { x1: 0, y1: 0, x2: 0, y2: 6, class: 'pat-fail-line', 'stroke-width': 2.5 })),
    s('pattern', { id: 'pat-offline', width: 6, height: 6, patternUnits: 'userSpaceOnUse' },
      s('rect', { width: 6, height: 6, class: 'pat-offline-bg' }), s('circle', { cx: 3, cy: 3, r: 1.4, class: 'pat-offline-dot' })))];
  const maxOf = new Map(data.scenario.chargers.map((c) => [c.id, c.max_kw]));
  const flows = []; // per charger: its flow line and its occupied runs, to place the line under the cursor
  const fails = []; // { from, to, node }: the failure rectangles

  chargers.forEach((c, i) => {
    const y0 = top + i * ROW;
    parts.push(s('rect', { x: 0, y: y0, width: W, height: ROW, class: 'row-bg' }),
      s('text', { x: MARGIN.left - 8, y: y0 + ROW / 2 + 4, class: 'row-label plain', 'text-anchor': 'end' }, c.id));
    const max = maxOf.get(c.id) || 1;
    const occRuns = [];
    for (const r of runs(occupancy[i])) {
      if (r.value === '') continue;
      occRuns.push(r);
      const w = Math.max(1, x(r.to + 1) - x(r.from));
      parts.push(s('rect', { x: x(r.from), y: y0 + 2, width: w, height: ROW - 4, class: 'occ' }, s('title', {}, `${r.value} em ${c.id}: minutos ${r.from}–${r.to}`)));
      const slice = c.physical_kw.slice(r.from, r.to + 1).map((v) => (Number.isFinite(v) ? v / max : null));
      parts.push(s('path', { d: fillPath(slice, (k) => x(r.from + k), (f) => y0 + ROW - 2 - (ROW - 4) * Math.min(1, f), y0 + ROW - 2), class: 'power-fill' }));
      if (w > 34) parts.push(s('text', { x: x(r.from) + 3, y: y0 + ROW / 2 + 4, class: 'occ-label' }, r.value));
    }
    const flow = s('line', { y1: y0 + ROW - 3, y2: y0 + ROW - 3, class: 'flow-line' });
    parts.push(flow);
    flows.push({ node: flow, runs: occRuns, power: c.physical_kw, at: -1, on: false });
    for (const r of runs(c.status)) {
      if (r.value === 0) continue;
      const failed = r.value === 1;
      const rect = s('rect', { x: x(r.from), y: y0 + 1, width: Math.max(2, x(r.to + 1) - x(r.from)), height: ROW - 2, class: failed ? 'status-fail' : 'status-offline' },
        s('title', {}, `${c.id}: ${failed ? 'em falha' : 'sem comunicação (mantém a última potência)'}, minutos ${r.from}–${r.to}`));
      parts.push(rect);
      if (failed) fails.push({ from: r.from, to: r.to, node: rect, on: false });
    }
  });
  parts.push(...timeAxis(x, n, startClock, bottom));
  const offs = [cursorLine(parts, x, cursor, top, bottom)];
  const svg = s('svg', { viewBox: `0 0 ${W} ${bottom + 26}`, class: 'chart charger-timeline', role: 'img', 'aria-label': 'Linha do tempo dos carregadores: ônibus ligado, potência e falhas' }, parts);
  offs.push(bindPlotPointer(svg, x, W, cursor, null));
  const wrap = h('div', { class: 'chart-wrap', tabindex: 0, role: 'slider', 'aria-label': 'Cursor de tempo', 'aria-valuemin': 0, 'aria-valuemax': n }, svg);
  bindCursorKeys(wrap, cursor);
  // The run of occupied minutes holding minute m, or -1 (runs are sorted and do not overlap).
  const runAt = (list, m) => {
    let lo = 0;
    let hi = list.length - 1;
    while (lo <= hi) {
      const mid = (lo + hi) >> 1;
      if (list[mid].to < m) lo = mid + 1; else if (list[mid].from > m) hi = mid - 1; else return mid;
    }
    return -1;
  };
  const sync = (m) => {
    wrap.setAttribute('aria-valuenow', m);
    wrap.setAttribute('aria-valuetext', `minuto ${m}, ${clock(startClock, m)}`);
    for (const f of flows) {
      const k = runAt(f.runs, m);
      const on = k >= 0 && f.power[m] > 0;
      if (on && k !== f.at) {
        f.at = k;
        f.node.setAttribute('x1', x(f.runs[k].from));
        f.node.setAttribute('x2', x(f.runs[k].to + 1));
      }
      if (on !== f.on) { f.on = on; f.node.classList.toggle('on', on); }
    }
    for (const f of fails) {
      const on = m >= f.from && m <= f.to;
      if (on !== f.on) { f.on = on; f.node.classList.toggle('flicker', on); }
    }
  };
  offs.push(cursor.onChange(sync));
  sync(cursor.value);
  const legend = h('div', { class: 'legend' },
    legendItem(swatchSvg(22, s('rect', { width: 22, height: 12, class: 'occ' }), s('rect', { y: 6, width: 22, height: 6, class: 'power-fill' })), 'ônibus ligado (altura = potência / máximo do carregador)'),
    legendItem(swatchSvg(22, s('rect', { width: 22, height: 12, class: 'status-fail' })), 'carregador em falha'),
    legendItem(swatchSvg(22, s('rect', { width: 22, height: 12, class: 'status-offline' })), 'sem comunicação'));
  root.replaceChildren(h('h3', {}, 'Carregadores'), wrap, legend);
  return { dispose() { offs.forEach((off) => off()); } };
}
