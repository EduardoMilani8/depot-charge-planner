import { h, clear } from './dom.js';
import { s } from './charts/svg.js';
import { COLUMNS, CONTROLLER_HELP, formatCell } from './glossary.js';
import { dotStrip } from './charts/layout.js';
import { fmtNum, fmtPct } from './format.js';
import { countingText, staggerStep } from './motion.js';

const PROFILE_PT = { none: 'sem falhas', mild: 'falhas leves', severe: 'falhas severas', random: 'falhas aleatórias' };

function summary(p) {
  return `${p.buses} ônibus, ${p.chargers} carregadores, limite ${fmtNum(p.limit_kw, 0)} kW, ${PROFILE_PT[p.profile] || p.profile}, ` +
    `${p.seeds} sementes, operadores ${p.follow_swaps ? 'seguem' : 'não seguem'} os rodízios. Dados sintéticos: mostram o comportamento do algoritmo, não de uma garagem real.`;
}

// table draws the comparison; rows enter one after the other and the share of ready buses
// counts up. `cancels` collects what stops the counters.
function table(data, cancels) {
  const best = Math.max(...data.controllers.map((c) => c.aggregate.ready_pct));
  const head = h('tr', {}, h('th', { scope: 'col' }, 'controlador'),
    COLUMNS.map((c) => h('th', { scope: 'col', title: c.help }, c.label)));
  const rows = data.controllers.map((c, index) => {
    const isBest = c.aggregate.ready_pct === best;
    return h('tr', { class: c.name === 'planner' ? 'planner reveal' : 'reveal', style: `--i:${index}` },
      h('th', { scope: 'row', title: CONTROLLER_HELP[c.name] }, c.name),
      COLUMNS.map((col) => {
        const bad = col.mustBeZero && c.aggregate[col.key] > 0;
        let text = formatCell(col, c.aggregate);
        if (col.key === 'ready_pct') {
          const count = countingText(c.aggregate.ready_pct, fmtPct, 800, { delay: index * 70 });
          cancels.push(count.cancel);
          text = count.node;
        }
        const node = col.key === 'ready_pct' && isBest ? h('strong', {}, text, h('span', { class: 'sr' }, ' (melhor)'), ' ▲') : text;
        return h('td', { class: bad ? 'bad' : '' }, bad ? '⚠ ' : '', node);
      }));
  });
  return h('div', { class: 'table-wrap' }, h('table', { class: 'metrics' }, h('thead', {}, head), h('tbody', {}, rows)));
}

function seedsPanel(data, onOpen) {
  const W = 600, H = 44;
  return h('div', { class: 'seeds' },
    h('h3', {}, 'Ônibus prontos por semente'),
    h('p', { class: 'note' }, 'Cada ponto é uma semente (uma garagem simulada). Clique num ponto para abrir aquela execução minuto a minuto.'),
    data.controllers.map((c) => {
      const dots = dotStrip(c.seeds.map((x) => ({ seed: x.seed, value: x.metrics.ready_pct })), W, H, 10);
      const step = staggerStep(dots.length); // the cascade is capped, so every dot is there within about a second
      const mean = 10 + ((W - 20) * Math.min(100, Math.max(0, c.aggregate.ready_pct))) / 100;
      const svg = s('svg', { viewBox: `0 0 ${W} ${H}`, class: 'strip', role: 'group', 'aria-label': `${c.name}: ônibus prontos por semente` },
        s('line', { x1: 10, x2: W - 10, y1: H - 6, y2: H - 6, class: 'axis' }),
        s('rect', { x: 10, y: H - 12, width: Math.max(0, mean - 10), height: 8, class: 'bar grow' }, s('title', {}, `média: ${fmtNum(c.aggregate.ready_pct, 1)}%`)),
        s('line', { x1: mean, x2: mean, y1: 2, y2: H - 2, class: 'mean' }),
        dots.map((d, i) => s('circle', {
          cx: d.x, cy: d.y, r: 5, class: 'dot pop', style: `--i:${i};--step:${step}ms`, tabindex: 0, role: 'button',
          'aria-label': `${c.name}, semente ${d.seed}: ${fmtNum(d.value, 1)}% prontos. Abrir execução`,
          onclick: () => onOpen(c.name, d.seed),
          onkeydown: (e) => { if (e.key === 'Enter' || e.key === ' ') { e.preventDefault(); onOpen(c.name, d.seed); } },
        }, s('title', {}, `semente ${d.seed}: ${fmtNum(d.value, 1)}%`))));
      return h('div', { class: 'strip-row' }, h('span', { class: 'strip-name', title: CONTROLLER_HELP[c.name] }, c.name), svg,
        h('span', { class: 'strip-mean' }, `média ${fmtNum(c.aggregate.ready_pct, 1)}%`));
    }));
}

function glossary() {
  return h('details', { class: 'glossary' }, h('summary', {}, 'O que significa cada coluna e cada controlador'),
    h('dl', {}, COLUMNS.map((c) => [h('dt', {}, c.label), h('dd', {}, c.help)]),
      Object.entries(CONTROLLER_HELP).map(([k, v]) => [h('dt', {}, k), h('dd', {}, v)])));
}

// renderCompare draws the Comparação tab and returns { dispose }, which stops the counters.
export function renderCompare(root, data, onOpen) {
  const cancels = [];
  clear(root);
  root.append(h('h2', {}, 'Comparação'), h('p', { class: 'note' }, summary(data.params)), table(data, cancels), seedsPanel(data, onOpen),
    h('p', { class: 'note' }, data.p99_note), glossary());
  return { dispose() { cancels.forEach((c) => c()); } };
}
