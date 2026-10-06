import { s } from './svg.js';
import { h } from '../dom.js';
import { linear, timeTicks } from './scale.js';
import { MARGIN, runs, fillPath } from './layout.js';
import { chargerOccupancy, swapMarkers } from '../decision.js';
import { clock, fmtNum } from '../format.js';
import { describeFault } from '../glossary.js';
import { bindCursorKeys } from '../cursor.js';

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

// Pointer on the plot moves the cursor (the label column does not); `onRow` hears the row
// (data-key) that was pressed.
function bindPointer(svg, x, cursor, onRow) {
  const toUnits = (clientX) => {
    const r = svg.getBoundingClientRect();
    return ((clientX - r.left) * W) / r.width;
  };
  let dragging = false;
  svg.addEventListener('pointerdown', (e) => {
    if (e.button !== 0) return; // only the primary button selects or drags; right-click opens the context menu
    const row = e.target.closest('[data-key]');
    if (row && onRow) onRow(row.dataset.key);
    const u = toUnits(e.clientX);
    if (u < MARGIN.left) return;
    dragging = true;
    svg.setPointerCapture(e.pointerId);
    cursor.set(x.invert(u));
  });
  svg.addEventListener('pointermove', (e) => { if (dragging && e.buttons) cursor.set(x.invert(toUnits(e.clientX))); });
  const stop = () => { dragging = false; };
  svg.addEventListener('pointerup', stop);
  svg.addEventListener('pointercancel', stop);
}

// cursorLine adds the vertical time cursor and keeps it at the cursor's minute.
function cursorLine(parts, x, cursor, y1, y2) {
  const line = s('line', { y1, y2, class: 'cursor' });
  parts.push(line);
  const move = (m) => { line.setAttribute('x1', x(m)); line.setAttribute('x2', x(m)); };
  move(cursor.value);
  cursor.onChange(move);
}

const swatchSvg = (w, ...kids) => s('svg', { width: w, height: 12, 'aria-hidden': 'true' }, kids);
const legendItem = (symbol, text) => h('span', { class: 'legend-item' }, symbol, text);

// renderBusTimeline: one row per bus, from arrival to departure, filled with the true
// charge; a mark at departure says whether the bus was ready.
export function renderBusTimeline(root, data, cursor, hooks) {
  const n = data.scenario.horizon_min;
  const x = linear(0, n, MARGIN.left, W - MARGIN.right);
  const buses = data.series.buses;
  const outcomes = new Map(data.outcomes.map((o) => [o.id, o]));
  const top = MARGIN.top;
  const bottom = top + buses.length * ROW_H;
  const markers = swapMarkers(data.decisions);
  const rowNodes = new Map();
  const parts = [];

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
      g.append(s('rect', { x: x(from), y: barY, width: Math.max(2, x(to) - x(from)), height: barH, class: 'bus-bar' }));
      const frac = (v) => (Number.isFinite(v) ? v / o.capacity_kwh : null);
      g.append(s('path', { d: fillPath(b.true_soc_kwh.map(frac), x, (f) => barY + barH * (1 - f), barY + barH), class: 'soc-fill' }));
      const ty = barY + barH * (1 - o.forecast_target_kwh / o.capacity_kwh);
      g.append(s('line', { x1: x(from), x2: x(to), y1: ty, y2: ty, class: 'target-line' }, s('title', {}, `alvo previsto: ${fmtNum(o.forecast_target_kwh, 0)} kWh`)));
      const dx = x(to);
      const result = !o.departed ? 'não saiu dentro do horizonte' : o.ready ? 'saiu pronto' : `saiu sem a carga (faltaram ${fmtNum(o.shortfall_kwh, 0)} kWh)`;
      const title = s('title', {}, `${b.id}: ${result}`);
      if (!o.departed) g.append(s('circle', { cx: dx, cy: y0 + ROW_H / 2, r: 4, class: 'mark-pending' }, title));
      else if (o.ready) g.append(s('circle', { cx: dx, cy: y0 + ROW_H / 2, r: 4.5, class: 'mark-ready' }, title));
      else g.append(s('path', { d: `M${dx - 4},${y0 + ROW_H / 2 - 4}l8,8m0,-8l-8,8`, class: 'mark-fail' }, title));
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
    parts.push(s('path', { d: up ? `M${x(m.minute)},${y + ROW_H - 2}l-4,0l2,-6z` : `M${x(m.minute)},${y + 2}l-4,0l2,6z`, class: up ? 'swap-in' : 'swap-out' },
      s('title', {}, `${up ? 'recebe' : 'cede'} o carregador ${m.charger}: ${m.reason}`)));
  }
  parts.push(...timeAxis(x, n, data.scenario.start_clock_min, bottom));
  cursorLine(parts, x, cursor, top, bottom);
  const svg = s('svg', { viewBox: `0 0 ${W} ${bottom + 26}`, class: 'chart bus-timeline', role: 'group', 'aria-label': 'Linha do tempo dos ônibus' }, parts);
  bindPointer(svg, x, cursor, hooks.onSelectBus);

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

  return {
    setSelected(id) {
      for (const [k, g] of rowNodes) g.classList.toggle('selected', k === id);
      const known = id !== null && rowNodes.has(id);
      picked.textContent = known ? `Selecionado: ${id}. ` : '';
      jump.hidden = !known;
    },
  };
}

// renderChargerTimeline: one row per charger with the bus plugged in, its power and
// the failure windows.
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

  chargers.forEach((c, i) => {
    const y0 = top + i * ROW;
    parts.push(s('rect', { x: 0, y: y0, width: W, height: ROW, class: 'row-bg' }),
      s('text', { x: MARGIN.left - 8, y: y0 + ROW / 2 + 4, class: 'row-label plain', 'text-anchor': 'end' }, c.id));
    const max = maxOf.get(c.id) || 1;
    for (const r of runs(occupancy[i])) {
      if (r.value === '') continue;
      const w = Math.max(1, x(r.to + 1) - x(r.from));
      parts.push(s('rect', { x: x(r.from), y: y0 + 2, width: w, height: ROW - 4, class: 'occ' }, s('title', {}, `${r.value} em ${c.id}: minutos ${r.from}–${r.to}`)));
      const slice = c.physical_kw.slice(r.from, r.to + 1).map((v) => (Number.isFinite(v) ? v / max : null));
      parts.push(s('path', { d: fillPath(slice, (k) => x(r.from + k), (f) => y0 + ROW - 2 - (ROW - 4) * Math.min(1, f), y0 + ROW - 2), class: 'power-fill' }));
      if (w > 34) parts.push(s('text', { x: x(r.from) + 3, y: y0 + ROW / 2 + 4, class: 'occ-label' }, r.value));
    }
    for (const r of runs(c.status)) {
      if (r.value === 0) continue;
      const failed = r.value === 1;
      parts.push(s('rect', { x: x(r.from), y: y0 + 1, width: Math.max(2, x(r.to + 1) - x(r.from)), height: ROW - 2, class: failed ? 'status-fail' : 'status-offline' },
        s('title', {}, `${c.id}: ${failed ? 'em falha' : 'sem comunicação (mantém a última potência)'}, minutos ${r.from}–${r.to}`)));
    }
  });
  parts.push(...timeAxis(x, n, startClock, bottom));
  cursorLine(parts, x, cursor, top, bottom);
  const svg = s('svg', { viewBox: `0 0 ${W} ${bottom + 26}`, class: 'chart charger-timeline', role: 'img', 'aria-label': 'Linha do tempo dos carregadores: ônibus ligado, potência e falhas' }, parts);
  bindPointer(svg, x, cursor, null);
  const wrap = h('div', { class: 'chart-wrap', tabindex: 0, role: 'slider', 'aria-label': 'Cursor de tempo', 'aria-valuemin': 0, 'aria-valuemax': n }, svg);
  bindCursorKeys(wrap, cursor);
  const sync = (m) => {
    wrap.setAttribute('aria-valuenow', m);
    wrap.setAttribute('aria-valuetext', `minuto ${m}, ${clock(startClock, m)}`);
  };
  cursor.onChange(sync);
  sync(cursor.value);
  const legend = h('div', { class: 'legend' },
    legendItem(swatchSvg(22, s('rect', { width: 22, height: 12, class: 'occ' }), s('rect', { y: 6, width: 22, height: 6, class: 'power-fill' })), 'ônibus ligado (altura = potência / máximo do carregador)'),
    legendItem(swatchSvg(22, s('rect', { width: 22, height: 12, class: 'status-fail' })), 'carregador em falha'),
    legendItem(swatchSvg(22, s('rect', { width: 22, height: 12, class: 'status-offline' })), 'sem comunicação'));
  root.replaceChildren(h('h3', {}, 'Carregadores'), wrap, legend);
}
