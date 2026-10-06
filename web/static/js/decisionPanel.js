import { h, clear } from './dom.js';
import { decisionAt, decisionNotes, statusesAt, chargerOccupancy, explainBus } from './decision.js';
import { renderSoCChart } from './charts/line.js';
import { valuesAt } from './charts/layout.js';
import { clock, fmtNum } from './format.js';
import { LAYER_LABEL } from './glossary.js';
import { throttle } from './motion.js';

const kw = (v, digits = 1) => (v === null || v === undefined || !Number.isFinite(v) ? '—' : `${fmtNum(v, digits)} kW`);

// renderDecisionPanel shows what the controller decided at the cursor minute and, for the
// selected bus, why it did or did not leave ready. The bus table is rebuilt only when the
// decision in force changes; between decisions only the numbers that follow the minute are
// rewritten in place, at most ten times a second. Returns { dispose }.
export function renderDecisionPanel(root, data, cursor, selection) {
  const n = data.scenario.horizon_min;
  const start = data.scenario.start_clock_min;
  const busIdx = new Map(data.series.buses.map((b, i) => [b.id, i]));
  const occupancy = chargerOccupancy(data);
  // Only the planner explains itself bus by bus; the other controllers list no buses at all.
  const explains = data.decisions.some((d) => (d.buses?.length ?? 0) > 0);
  const noteEl = h('p', { class: 'note' });
  const extras = h('div', {});
  const chargerCells = []; // per charger: [commanded, physical, plugged, state] cells and their last text
  const chargerTable = chargerTableNode();
  const busBox = h('div', {});
  const rest = h('div', { hidden: true }, extras, h('h4', {}, 'Carregadores'), chargerTable, h('h4', {}, 'Ônibus'), busBox);
  const body = h('div', { class: 'decision-body' }, noteEl, rest);
  const detail = h('div', { class: 'bus-detail', tabindex: -1, role: 'region', 'aria-label': 'Detalhe do ônibus selecionado' });
  root.replaceChildren(h('h3', {}, 'Decisão neste minuto'), body, detail);
  let disposeChart = null;
  let builtIndex = -2; // decision index the extras and the bus table show (-1 = none yet)
  let busRows = new Map(); // bus id -> its table row, for the current decision
  let marked = null; // id whose row carries the "selected" mark

  function chargerTableNode() {
    const rows = data.series.chargers.map((c) => {
      const cells = [h('td'), h('td'), h('td'), h('td')];
      chargerCells.push(cells.map((td) => ({ td, text: '' })));
      return h('tr', {}, h('th', { scope: 'row' }, c.id), cells);
    });
    return h('div', { class: 'table-wrap' }, h('table', { class: 'metrics small' },
      h('thead', {}, h('tr', {}, ['carregador', 'potência comandada', 'potência física', 'ônibus ligado', 'estado'].map((t) => h('th', { scope: 'col' }, t)))),
      h('tbody', {}, rows)));
  }

  // The statuses are rebuilt from deltas, so keep the last result per decision.
  let cached = { index: -2, list: [] };
  const statuses = (index) => {
    if (cached.index !== index) cached = { index, list: statusesAt(data.decisions, index) };
    return cached.list;
  };

  function refreshChargers(m) {
    data.series.chargers.forEach((c, i) => {
      const status = c.status[m];
      const texts = [kw(c.commanded_kw[m]), kw(c.physical_kw[m]), occupancy[i][m] || '—',
        status === 1 ? '⚠ em falha' : status === 2 ? '⚠ sem comunicação' : 'ok'];
      texts.forEach((text, k) => {
        const cell = chargerCells[i][k];
        if (cell.text !== text) { cell.text = text; cell.td.textContent = text; }
      });
    });
  }

  function markRow(id, on) {
    const tr = busRows.get(id);
    if (!tr) return;
    tr.classList.toggle('selected', on);
    if (on) tr.setAttribute('aria-current', 'true'); else tr.removeAttribute('aria-current');
  }
  function applySelection() {
    const id = selection.get();
    if (marked !== null) markRow(marked, false);
    marked = id;
    if (id !== null) markRow(id, true);
  }

  function busTable(list) {
    const order = [...list].sort((a, b) => Number(a.ok) - Number(b.ok) || b.sf - a.sf || (a.b < b.b ? -1 : a.b > b.b ? 1 : 0));
    const pick = (id) => selection.set(id);
    busRows = new Map();
    marked = null;
    const rows = order.map((b) => {
      const tr = h('tr', { tabindex: 0, 'data-bus': b.b,
        onclick: () => pick(b.b), onkeydown: (e) => { if (e.key === 'Enter' || e.key === ' ') { e.preventDefault(); pick(b.b); } } },
      h('th', { scope: 'row' }, b.b),
      h('td', {}, b.as ? (b.ok ? '✓ atinge o alvo' : '✗ não atinge') : 'não avaliado'),
      h('td', {}, b.ok ? '—' : `${fmtNum(b.sf, 0)} kWh`),
      h('td', { class: 'reason' }, b.r >= 0 ? data.reasons[b.r] : '—'));
      busRows.set(b.b, tr);
      return tr;
    });
    return h('div', { class: 'table-wrap' }, h('table', { class: 'metrics small' },
      h('thead', {}, h('tr', {}, ['ônibus', 'previsão', 'déficit previsto', 'motivo'].map((t) => h('th', { scope: 'col' }, t)))),
      h('tbody', {}, rows)));
  }

  function busSection(list) {
    if ((list?.length ?? 0) > 0) return busTable(list);
    busRows = new Map();
    marked = null;
    return h('p', { class: 'note' }, explains
      ? 'Nenhum ônibus avaliado pelo planejador neste momento.'
      : 'Este controlador não explica suas decisões ônibus a ônibus: só a potência comandada por carregador (acima). Escolha o controlador "planner" para ver os motivos.');
  }

  // rebuild redraws what depends on the decision in force (not on the minute).
  function rebuild(found) {
    const focusedBus = busBox.contains(document.activeElement) ? document.activeElement.dataset?.bus : undefined;
    builtIndex = found ? found.index : -1;
    rest.hidden = !found;
    if (!found) return;
    const d = found.decision;
    const notes = decisionNotes(data, d);
    const swaps = d.swaps ?? [];
    clear(extras).append(...[
      swaps.length ? h('div', {}, h('h4', {}, 'Rodízios recomendados'), h('ul', {}, swaps.map((sw) => h('li', {}, `${sw.in} assume ${sw.charger} no lugar de ${sw.out}. ${sw.reason}`)))) : null,
      (notes?.length ?? 0) > 0 ? h('div', {}, h('h4', {}, 'Observações do planejador'), h('ul', {}, notes.map((t) => h('li', {}, t)))) : null,
    ].filter(Boolean)); // Element.append would write the text "null" for the sections left out
    clear(busBox).append(busSection(statuses(found.index)));
    applySelection();
    // Redrawing the table drops keyboard focus; give it back to the row that had it.
    if (focusedBus !== undefined) busRows.get(focusedBus)?.focus();
  }

  function paint() {
    const m = cursor.value;
    const found = decisionAt(data.decisions, m, n);
    if (!found) {
      if (builtIndex !== -1) rebuild(null);
      noteEl.textContent = 'Ainda não havia decisão neste minuto.';
      return;
    }
    if (found.index !== builtIndex) rebuild(found);
    const d = found.decision;
    const v = valuesAt(data, m);
    noteEl.textContent = `Minuto ${m} (${clock(start, m)}). Decisão tomada no minuto ${found.from} (${clock(start, found.from)}), em vigor até o minuto ${found.to}. Camada: ${LAYER_LABEL[d.layer] || d.layer}. ` +
      `${explains ? 'Os motivos são os do minuto em que a decisão foi tomada. ' : ''}Potência física agora: ${kw(v.physical, 0)}.`;
    refreshChargers(m);
  }

  function renderDetail() {
    if (disposeChart) { disposeChart(); disposeChart = null; }
    clear(detail);
    const id = selection.get();
    if (id === null || !busIdx.has(id)) {
      detail.append(h('p', { class: 'note' }, 'Selecione um ônibus (na linha do tempo ou na tabela) para ver por que ele saiu pronto ou não.'));
      return;
    }
    const i = busIdx.get(id);
    const chart = h('div', { class: 'soc-chart' });
    detail.append(h('h3', {}, `Ônibus ${id}`), h('ul', { class: 'explain' }, explainBus(data, i).map((t) => h('li', {}, t))), chart);
    disposeChart = renderSoCChart(chart, data, i, cursor).dispose;
  }

  const schedule = throttle(paint, 100);
  const offCursor = cursor.onChange(schedule);
  // A selection only moves the mark between rows: nothing is rebuilt and focus stays where it is.
  const offSelection = selection.onChange(() => {
    applySelection();
    renderDetail();
  });
  paint();
  renderDetail();
  return {
    dispose() {
      offCursor();
      offSelection();
      schedule.cancel();
      if (disposeChart) { disposeChart(); disposeChart = null; }
    },
  };
}
