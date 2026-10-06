import { s } from './svg.js';
import { h } from '../dom.js';
import { linear, niceTicks, timeTicks } from './scale.js';
import { MARGIN, linePath } from './layout.js';
import { clock, fmtNum } from '../format.js';

const W = 1000;
const H = 190;

// renderSoCChart draws one bus's true charge, what the planner was told (gaps = no
// reading) and its forecast and true targets. dispose() unsubscribes from the cursor.
export function renderSoCChart(root, data, busIdx, cursor) {
  const n = data.scenario.horizon_min;
  const b = data.series.buses[busIdx];
  const o = data.outcomes.find((x) => x.id === b.id);
  if (!o) {
    root.replaceChildren(h('p', { class: 'note' }, 'Sem dados de resultado para este ônibus.'));
    return { dispose() {} };
  }
  const x = linear(0, n, MARGIN.left, W - MARGIN.right);
  const top = 10;
  const bottom = H - 28;
  // A noisy or biased reading can read above the battery's capacity; keep it on the plot.
  const high = Math.max(o.capacity_kwh, ...b.observed_kwh.filter(Number.isFinite), ...b.true_soc_kwh.filter(Number.isFinite));
  const y = linear(0, high, bottom, top);
  const parts = [];
  for (const t of niceTicks(0, high, 4)) {
    parts.push(s('line', { x1: MARGIN.left, x2: W - MARGIN.right, y1: y(t), y2: y(t), class: 'gridline' }),
      s('text', { x: MARGIN.left - 8, y: y(t) + 4, class: 'tick', 'text-anchor': 'end' }, fmtNum(t, 0)));
  }
  parts.push(s('text', { x: 8, y: top + 10, class: 'axis-title' }, 'kWh'));
  for (const m of timeTicks(n, 60)) {
    parts.push(s('text', { x: x(m), y: bottom + 16, class: 'tick', 'text-anchor': 'middle' }, clock(data.scenario.start_clock_min, m)));
  }
  parts.push(s('line', { x1: MARGIN.left, x2: W - MARGIN.right, y1: bottom, y2: bottom, class: 'axis' }));
  const hline = (v, cls, label) => parts.push(s('line', { x1: x(Math.max(0, o.arrival)), x2: x(Math.min(n, o.departure)), y1: y(v), y2: y(v), class: cls }, s('title', {}, label)));
  hline(o.forecast_target_kwh, 'target-line wide', `alvo previsto: ${fmtNum(o.forecast_target_kwh, 0)} kWh`);
  if (Math.abs(o.true_target_kwh - o.forecast_target_kwh) > 0.5) hline(o.true_target_kwh, 'true-target-line', `alvo real: ${fmtNum(o.true_target_kwh, 0)} kWh`);
  parts.push(s('path', { d: linePath(b.observed_kwh, x, y, false), class: 'line-observed' }),
    s('path', { d: linePath(b.true_soc_kwh, x, y, false), class: 'line-true' }));
  const cur = s('line', { y1: top, y2: bottom, class: 'cursor' });
  parts.push(cur);
  const svg = s('svg', { viewBox: `0 0 ${W} ${H}`, class: 'chart soc', role: 'img', 'aria-label': `Carga do ônibus ${b.id}: real contra a lida pelo planejador` }, parts);
  const swatch = (cls, text) => h('span', { class: 'legend-item' }, s('svg', { width: 22, height: 12, 'aria-hidden': 'true' }, s('line', { x1: 0, x2: 22, y1: 6, y2: 6, class: cls })), text);
  root.replaceChildren(svg, h('div', { class: 'legend' }, swatch('line-true', 'carga real'), swatch('line-observed', 'carga lida pelo planejador (vazio = sem leitura)'),
    swatch('target-line wide', 'alvo previsto'), swatch('true-target-line', 'alvo real (se diferente)')));
  const move = (m) => { cur.setAttribute('x1', x(m)); cur.setAttribute('x2', x(m)); };
  move(cursor.value);
  return { dispose: cursor.onChange(move) };
}
