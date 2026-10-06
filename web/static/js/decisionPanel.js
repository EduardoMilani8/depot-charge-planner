import { h, clear } from './dom.js';
import { decisionAt, decisionNotes, statusesAt, chargerOccupancy, explainBus } from './decision.js';
import { renderSoCChart } from './charts/line.js';
import { valuesAt } from './charts/layout.js';
import { clock, fmtNum } from './format.js';
import { LAYER_LABEL } from './glossary.js';

const kw = (v, digits = 1) => (v === null || v === undefined || !Number.isFinite(v) ? '—' : `${fmtNum(v, digits)} kW`);

// renderDecisionPanel shows what the controller decided at the cursor minute and, for the
// selected bus, why it did or did not leave ready.
export function renderDecisionPanel(root, data, cursor, selection) {
  const n = data.scenario.horizon_min;
  const start = data.scenario.start_clock_min;
  const busIdx = new Map(data.series.buses.map((b, i) => [b.id, i]));
  const occupancy = chargerOccupancy(data);
  // Only the planner explains itself bus by bus; the other controllers list no buses at all.
  const explains = data.decisions.some((d) => (d.buses?.length ?? 0) > 0);
  const body = h('div', { class: 'decision-body' });
  const detail = h('div', { class: 'bus-detail', tabindex: -1, role: 'region', 'aria-label': 'Detalhe do ônibus selecionado' });
  root.replaceChildren(h('h3', {}, 'Decisão neste minuto'), body, detail);
  let disposeChart = null;
  // The statuses are rebuilt from deltas, so keep the last result per decision.
  let cached = { index: -2, list: [] };
  const statuses = (index) => {
    if (cached.index !== index) cached = { index, list: statusesAt(data.decisions, index) };
    return cached.list;
  };

  function chargerRows(m) {
    return data.series.chargers.map((c, i) => {
      const status = c.status[m];
      return h('tr', {}, h('th', { scope: 'row' }, c.id),
        h('td', {}, kw(c.commanded_kw[m])),
        h('td', {}, kw(c.physical_kw[m])),
        h('td', {}, occupancy[i][m] || '—'),
        h('td', {}, status === 1 ? '⚠ em falha' : status === 2 ? '⚠ sem comunicação' : 'ok'));
    });
  }

  function busRows(list) {
    const order = [...list].sort((a, b) => Number(a.ok) - Number(b.ok) || b.sf - a.sf || (a.b < b.b ? -1 : a.b > b.b ? 1 : 0));
    const pick = (id) => selection.set(id);
    return order.map((b) => h('tr', { class: selection.get() === b.b ? 'selected' : '', tabindex: 0, 'data-bus': b.b, 'aria-current': selection.get() === b.b ? 'true' : false,
      onclick: () => pick(b.b), onkeydown: (e) => { if (e.key === 'Enter' || e.key === ' ') { e.preventDefault(); pick(b.b); } } },
    h('th', { scope: 'row' }, b.b),
    h('td', {}, b.as ? (b.ok ? '✓ atinge o alvo' : '✗ não atinge') : 'não avaliado'),
    h('td', {}, b.ok ? '—' : `${fmtNum(b.sf, 0)} kWh`),
    h('td', { class: 'reason' }, b.r >= 0 ? data.reasons[b.r] : '—')));
  }

  function busSection(list) {
    if ((list?.length ?? 0) > 0) {
      return h('div', { class: 'table-wrap' }, h('table', { class: 'metrics small' },
        h('thead', {}, h('tr', {}, ['ônibus', 'previsão', 'déficit previsto', 'motivo'].map((t) => h('th', { scope: 'col' }, t)))),
        h('tbody', {}, busRows(list))));
    }
    return h('p', { class: 'note' }, explains
      ? 'Nenhum ônibus avaliado pelo planejador neste momento.'
      : 'Este controlador não explica suas decisões ônibus a ônibus: só a potência comandada por carregador (acima). Escolha o controlador "planner" para ver os motivos.');
  }

  function update() {
    const m = cursor.value;
    const found = decisionAt(data.decisions, m, n);
    clear(body);
    if (!found) {
      body.append(h('p', { class: 'note' }, 'Ainda não havia decisão neste minuto.'));
      return;
    }
    const d = found.decision;
    const v = valuesAt(data, m);
    const notes = decisionNotes(data, d);
    const swaps = d.swaps ?? [];
    const blocks = [
      h('p', { class: 'note' }, `Minuto ${m} (${clock(start, m)}). Decisão tomada no minuto ${found.from} (${clock(start, found.from)}), em vigor até o minuto ${found.to}. Camada: ${LAYER_LABEL[d.layer] || d.layer}. ` +
        `${explains ? 'Os motivos são os do minuto em que a decisão foi tomada. ' : ''}Potência física agora: ${kw(v.physical, 0)}.`),
      swaps.length ? h('div', {}, h('h4', {}, 'Rodízios recomendados'), h('ul', {}, swaps.map((sw) => h('li', {}, `${sw.in} assume ${sw.charger} no lugar de ${sw.out}. ${sw.reason}`)))) : null,
      (notes?.length ?? 0) > 0 ? h('div', {}, h('h4', {}, 'Observações do planejador'), h('ul', {}, notes.map((t) => h('li', {}, t)))) : null,
      h('h4', {}, 'Carregadores'),
      h('div', { class: 'table-wrap' }, h('table', { class: 'metrics small' },
        h('thead', {}, h('tr', {}, ['carregador', 'potência comandada', 'potência física', 'ônibus ligado', 'estado'].map((t) => h('th', { scope: 'col' }, t)))),
        h('tbody', {}, chargerRows(m)))),
      h('h4', {}, 'Ônibus'),
      busSection(statuses(found.index)),
    ];
    // Element.append would write the text "null" for the sections left out.
    body.append(...blocks.filter(Boolean));
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

  cursor.onChange(update);
  selection.onChange((id) => {
    // Redrawing the table drops keyboard focus; give it back to the row that was chosen.
    const hadFocus = root.contains(document.activeElement) && document.activeElement.dataset?.bus === id;
    update();
    renderDetail();
    if (hadFocus) body.querySelector(`[data-bus="${CSS.escape(id)}"]`)?.focus();
  });
  update();
  renderDetail();
}
