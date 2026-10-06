import { h } from '../dom.js';
import { s } from './svg.js';
import { linear, niceTicks, timeTicks } from './scale.js';
import { MARGIN, stackLayers, areaPath, linePath, runs, assignLanes, valuesAt } from './layout.js';
import { clock, fmtNum } from '../format.js';
import { LAYER_LABEL, POWER_FAULTS, describeFault } from '../glossary.js';
import { bindCursorKeys } from '../cursor.js';

const W = 1000;
const PLOT_H = 300;
const RIBBON_H = 14;
const LANE_H = 12;
const MAX_LANES = 4;

// renderPowerChart draws stacked charger power against the site limit, the layer that
// decided each minute and the power-related faults, with a shared time cursor.
export function renderPowerChart(root, data, cursor) {
  const { scenario, series } = data;
  const n = scenario.horizon_min;
  const x = linear(0, n, MARGIN.left, W - MARGIN.right);
  const finite = (a) => a.filter(Number.isFinite);
  const maxKW = Math.max(1, ...finite(series.limit_kw), ...finite(series.commanded_kw), ...finite(series.physical_kw));
  const ticks = niceTicks(0, maxKW * 1.05, 5);
  const yMax = Math.max(maxKW * 1.05, ticks[ticks.length - 1]);
  const plotTop = MARGIN.top;
  const plotBottom = plotTop + PLOT_H;
  const y = linear(0, yMax, plotBottom, plotTop);

  const faults = scenario.faults.filter((f) => POWER_FAULTS.has(f.kind)).sort((a, b) => a.from - b.from || a.to - b.to);
  const lanes = assignLanes(faults);
  const laneCount = Math.min(MAX_LANES, lanes.length ? Math.max(...lanes) + 1 : 0);
  const ribbonY = plotBottom + 24;
  const lanesY = ribbonY + RIBBON_H + 6;
  const totalH = lanesY + laneCount * LANE_H + 8;

  const stacked = stackLayers(series.chargers.map((c) => c.physical_kw));
  const parts = [
    s('defs', {},
      s('pattern', { id: 'pat-lv', width: 6, height: 6, patternUnits: 'userSpaceOnUse', patternTransform: 'rotate(45)' },
        s('rect', { width: 6, height: 6, class: 'pat-lv-bg' }), s('line', { x1: 0, y1: 0, x2: 0, y2: 6, class: 'pat-line', 'stroke-width': 2.5 })),
      s('pattern', { id: 'pat-safe', width: 6, height: 6, patternUnits: 'userSpaceOnUse' },
        s('rect', { width: 6, height: 6, class: 'pat-safe-bg' }), s('circle', { cx: 3, cy: 3, r: 1.4, class: 'pat-dot' }))),
  ];

  // y grid and labels
  for (const t of ticks) {
    parts.push(s('line', { x1: MARGIN.left, x2: W - MARGIN.right, y1: y(t), y2: y(t), class: 'gridline' }),
      s('text', { x: MARGIN.left - 8, y: y(t) + 4, class: 'tick', 'text-anchor': 'end' }, fmtNum(t, 0)));
  }
  parts.push(s('text', { x: 8, y: plotTop + 10, class: 'axis-title' }, 'kW'));
  // x axis: one tick per hour, labelled with the wall clock
  for (const m of timeTicks(n, 60)) {
    parts.push(s('line', { x1: x(m), x2: x(m), y1: plotBottom, y2: plotBottom + 4, class: 'axis' }),
      s('text', { x: x(m), y: plotBottom + 17, class: 'tick', 'text-anchor': 'middle' }, clock(scenario.start_clock_min, m)));
  }
  parts.push(s('line', { x1: MARGIN.left, x2: W - MARGIN.right, y1: plotBottom, y2: plotBottom, class: 'axis' }));

  // stacked areas, one per charger (two alternating tints, separated by a thin line)
  stacked.forEach((layer, i) => {
    parts.push(s('path', { d: areaPath(layer.lower, layer.upper, x, y), class: i % 2 ? 'area area-b' : 'area area-a' },
      s('title', {}, `${series.chargers[i].id}: potência física`)));
  });
  parts.push(s('path', { d: linePath(series.commanded_kw, x, y, false), class: 'line-commanded' }),
    s('path', { d: linePath(series.limit_kw, x, y, true), class: 'line-limit' }));

  // layer ribbon
  parts.push(s('text', { x: MARGIN.left - 8, y: ribbonY + 11, class: 'tick', 'text-anchor': 'end' }, 'camada'));
  for (const r of runs(series.layer)) {
    parts.push(s('rect', { x: x(r.from), y: ribbonY, width: Math.max(1, x(r.to + 1) - x(r.from)), height: RIBBON_H, class: `layer layer-${r.value}` },
      s('title', {}, `${LAYER_LABEL[r.value] || r.value}: minutos ${r.from}–${r.to}`)));
  }
  // faults
  if (laneCount > 0) parts.push(s('text', { x: MARGIN.left - 8, y: lanesY + 10, class: 'tick', 'text-anchor': 'end' }, 'falhas'));
  faults.forEach((f, i) => {
    const lane = Math.min(lanes[i], MAX_LANES - 1);
    parts.push(s('rect', { x: x(f.from), y: lanesY + lane * LANE_H, width: Math.max(2, x(f.to) - x(f.from)), height: LANE_H - 2, class: 'fault' },
      s('title', {}, describeFault(f))));
  });

  // cursor and pointer capture
  const cursorLine = s('line', { y1: plotTop, y2: totalH - 6, class: 'cursor' });
  const overlay = s('rect', { x: MARGIN.left, y: plotTop, width: W - MARGIN.left - MARGIN.right, height: totalH - plotTop, class: 'overlay' });
  parts.push(cursorLine, overlay);

  const svg = s('svg', { viewBox: `0 0 ${W} ${totalH}`, class: 'chart power', role: 'img',
    'aria-label': 'Potência por carregador ao longo do tempo, contra o limite da garagem' }, parts);
  const toMinute = (clientX) => {
    const r = svg.getBoundingClientRect();
    return x.invert(((clientX - r.left) * W) / r.width);
  };
  overlay.addEventListener('pointerdown', (e) => { overlay.setPointerCapture(e.pointerId); cursor.set(toMinute(e.clientX)); });
  overlay.addEventListener('pointermove', (e) => { if (e.buttons) cursor.set(toMinute(e.clientX)); });

  const readout = h('div', { class: 'readout', 'aria-live': 'off' });
  const wrap = h('div', { class: 'chart-wrap', tabindex: 0, role: 'slider', 'aria-label': 'Cursor de tempo',
    'aria-valuemin': 0, 'aria-valuemax': n }, svg);
  bindCursorKeys(wrap, cursor);

  const swatch = (cls, text) => h('span', { class: 'legend-item' }, s('svg', { width: 22, height: 12, 'aria-hidden': 'true' }, s('rect', { width: 22, height: 12, class: cls })), text);
  const legend = h('div', { class: 'legend' },
    swatch('area-a', 'potência por carregador (física)'),
    h('span', { class: 'legend-item' }, s('svg', { width: 22, height: 12, 'aria-hidden': 'true' }, s('line', { x1: 0, x2: 22, y1: 6, y2: 6, class: 'line-commanded' })), 'potência comandada'),
    h('span', { class: 'legend-item' }, s('svg', { width: 22, height: 12, 'aria-hidden': 'true' }, s('line', { x1: 0, x2: 22, y1: 6, y2: 6, class: 'line-limit' })), 'limite da garagem'),
    swatch('layer-normal', 'camada normal'), swatch('layer-last-valid', 'último plano válido'), swatch('layer-safe', 'perfil seguro'),
    swatch('fault', 'falha ativa'));
  root.replaceChildren(h('h3', {}, 'Potência'), wrap, readout, legend);

  function update(m) {
    const px = x(m);
    cursorLine.setAttribute('x1', px);
    cursorLine.setAttribute('x2', px);
    wrap.setAttribute('aria-valuenow', m);
    const v = valuesAt(data, m);
    const kw = (val) => (val === null ? '—' : `${fmtNum(val, 0)} kW`);
    wrap.setAttribute('aria-valuetext', `minuto ${m}, ${clock(scenario.start_clock_min, m)}`);
    readout.textContent = `minuto ${m} · ${clock(scenario.start_clock_min, m)} — físico ${kw(v.physical)} · comandado ${kw(v.commanded)} · limite ${kw(v.limit)} · decidiu: ${LAYER_LABEL[v.layer] || v.layer}`;
  }
  cursor.onChange(update);
  update(cursor.value);
}
