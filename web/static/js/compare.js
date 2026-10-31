import { h, clear } from './dom.js';
import { s } from './charts/svg.js';
import { CONTROLLER_HELP, COST_TERMS, columnsFor, formatCell, describeParams } from './glossary.js';
import { savingsSentence, hasBill } from './cost.js';
import { dotStrip } from './charts/layout.js';
import { fmtNum, fmtPct } from './format.js';
import { countingText, staggerStep } from './motion.js';

function summary(p) {
  return `${describeParams(p, { seeds: true })}. Dados sintéticos: mostram o comportamento do algoritmo, não de uma garagem real.`;
}

// table draws the comparison; rows enter one after the other and the share of ready buses
// counts up. `cancels` collects what stops the counters.
function table(data, cancels) {
  const best = Math.max(...data.controllers.map((c) => c.aggregate.ready_pct));
  const columns = columnsFor(data);
  const head = h('tr', {}, h('th', { scope: 'col' }, 'controlador'),
    columns.map((c) => h('th', { scope: 'col', title: c.help }, c.label)));
  const rows = data.controllers.map((c, index) => {
    const isBest = c.aggregate.ready_pct === best;
    return h('tr', { class: c.name === 'planner' ? 'planner reveal' : 'reveal', style: `--i:${index}` },
      h('th', { scope: 'row', title: CONTROLLER_HELP[c.name] }, c.name),
      columns.map((col) => {
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

function glossary(data) {
  return h('details', { class: 'glossary' }, h('summary', {}, 'O que significa cada coluna, cada controlador e cada termo de custo'),
    h('dl', {}, columnsFor(data).map((c) => [h('dt', {}, c.label), h('dd', {}, c.help)]),
      Object.entries(CONTROLLER_HELP).map(([k, v]) => [h('dt', {}, k), h('dd', {}, v)]),
      COST_TERMS.map(([t, d]) => [h('dt', {}, t), h('dd', {}, d)])));
}

// warningList shows the server's warnings about valid parameters that do less than they seem to.
export function warningList(list) {
  if (!list || list.length === 0) return null;
  return h('ul', { class: 'warnings' }, list.map((w) => h('li', {}, '⚠ ' + w)));
}

// targetSlot is the place of the "with and without the demand target" sentence; main.js fills it in.
function targetSlot(data) {
  const p = data.params;
  if (!(p.cost_aware && p.demand_target_kw > 0)) return null;
  const text = hasBill(data) ? 'Calculando a fatura sem a meta de demanda…'
    : 'Para ver se a meta de demanda reduz a fatura mensal estimada, informe os preços de demanda ou a demanda contratada.';
  return h('p', { class: 'note target-note', role: 'status' }, text);
}

// renderCompare draws the Comparação tab and returns { dispose }, which stops the counters.
export function renderCompare(root, data, onOpen) {
  const cancels = [];
  clear(root);
  const slot = targetSlot(data);
  const savings = savingsSentence(data.controllers);
  root.append(...[h('h2', {}, 'Comparação'), h('p', { class: 'note' }, summary(data.params)), warningList(data.warnings), table(data, cancels),
    savings ? h('p', { class: 'savings' }, savings) : null, slot, seedsPanel(data, onOpen),
    h('p', { class: 'note' }, data.p99_note), glossary(data)].filter(Boolean)); // append(null) would write the text "null"
  return {
    dispose() { cancels.forEach((c) => c()); },
    // setTargetNote({ text, tone }) replaces the placeholder sentence; a warning tone gets the warning style.
    setTargetNote(note) {
      if (!slot || !note) return;
      slot.textContent = note.text;
      slot.classList.toggle('warn', note.tone === 'warn');
    },
  };
}
