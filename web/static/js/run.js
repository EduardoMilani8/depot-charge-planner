import { run as apiRun, createLatest, ApiError } from './api.js';
import { h, clear } from './dom.js';
import { createCursor } from './cursor.js';
import { createSelection } from './decision.js';
import { renderPowerChart } from './charts/power.js';
import { renderBusTimeline, renderChargerTimeline } from './charts/gantt.js';
import { renderDecisionPanel } from './decisionPanel.js';
import { fmtNum, fmtPct, fmtBRL, clock } from './format.js';
import { CONTROLLERS } from './params.js';
import { describeFault, CONTROLLER_HELP } from './glossary.js';

// createRunView owns the "Execução" tab: it loads one run and draws its panels.
export function createRunView(root, hooks) {
  const latest = createLatest();
  let current = null; // { data, cursor, selection } of the run on screen

  function summaryLine(m) {
    const bad = m.plan_violations > 0;
    return `${m.ready} de ${m.buses} ônibus saíram prontos (${fmtPct(m.ready_pct)}) · déficit ${fmtNum(m.shortfall_kwh, 0)} kWh · pico ${fmtNum(m.peak_kw, 0)} kW · ` +
      `${bad ? '⚠ ' : ''}violações do plano ${m.plan_violations} · custo ${fmtBRL(m.cost_brl)}`;
  }

  // controls draws the controller and seed pickers; `sel` is { controller, seed }.
  function controls(sel) {
    const select = h('select', { id: 'run-controller', 'aria-label': 'Controlador' },
      CONTROLLERS.map((c) => h('option', { value: c, selected: c === sel.controller }, c)));
    const seed = h('input', { id: 'run-seed', type: 'number', step: 1, value: sel.seed, 'aria-label': 'Semente' });
    // Empty, the hint is hidden so it does not take a blank row of the flex layout.
    const hint = h('span', { class: 'field-error', role: 'alert', hidden: true });
    const setHint = (text) => { hint.textContent = text; hint.hidden = text === ''; };
    const open = () => {
      const n = Number(seed.value);
      // Any whole number goes to the server, which answers out-of-range seeds in Portuguese.
      if (seed.value.trim() === '' || !Number.isInteger(n)) { setHint('A semente deve ser um número inteiro.'); return; }
      setHint('');
      hooks.onSelect(select.value, n);
    };
    select.addEventListener('change', open);
    seed.addEventListener('change', open);
    return h('div', { class: 'run-controls' },
      h('label', {}, 'Controlador ', select), h('label', {}, 'Semente ', seed), hint,
      h('span', { class: 'note', title: CONTROLLER_HELP[sel.controller] }, CONTROLLER_HELP[sel.controller]));
  }

  function cursorRow(data, cursor) {
    const slider = h('input', { type: 'range', min: 0, max: data.scenario.horizon_min, step: 1, value: 0, 'aria-label': 'Minuto da simulação', class: 'time-slider' });
    const label = h('output', { class: 'time-label' });
    slider.addEventListener('input', () => cursor.set(Number(slider.value)));
    cursor.onChange((m) => {
      slider.value = m;
      label.textContent = `minuto ${m} · ${clock(data.scenario.start_clock_min, m)}`;
    });
    label.textContent = `minuto 0 · ${clock(data.scenario.start_clock_min, 0)}`;
    return h('div', { class: 'cursor-row' }, slider, label);
  }

  function faultList(data) {
    if (data.scenario.faults.length === 0) return h('p', { class: 'note' }, 'Este cenário não tem falhas.');
    return h('details', { class: 'faults' }, h('summary', {}, `Falhas deste cenário (${data.scenario.faults.length})`),
      h('ul', {}, data.scenario.faults.map((f) => h('li', {}, describeFault(f)))));
  }

  function render(data) {
    const cursor = createCursor(data.scenario.horizon_min);
    const selection = createSelection();
    const powerPanel = h('div', { class: 'panel', id: 'panel-power' });
    const busPanel = h('div', { class: 'panel', id: 'panel-buses' });
    const chargerPanel = h('div', { class: 'panel', id: 'panel-chargers' });
    const decisionPanel = h('div', { class: 'panel', id: 'panel-decision' });
    clear(root).append(
      h('h2', {}, 'Execução'),
      controls(data),
      h('p', { class: 'summary' }, summaryLine(data.metrics)),
      faultList(data),
      cursorRow(data, cursor),
      powerPanel, busPanel, chargerPanel, decisionPanel);
    renderPowerChart(powerPanel, data, cursor);
    const busView = renderBusTimeline(busPanel, data, cursor, {
      onSelectBus: (id) => selection.set(id),
      onJump: () => {
        const target = decisionPanel.querySelector('.bus-detail');
        if (!target) return;
        target.scrollIntoView({ block: 'start' });
        target.focus({ preventScroll: true }); // so a screen reader announces the section it landed on
      },
    });
    selection.onChange((id) => busView.setSelected(id));
    renderChargerTimeline(chargerPanel, data, cursor);
    renderDecisionPanel(decisionPanel, data, cursor, selection);
    current = { data, cursor, selection };
    hooks.onRendered?.(current);
  }

  async function load(state) {
    clear(root).append(h('h2', {}, 'Execução'), h('p', { class: 'loading' }, 'Calculando a execução…'));
    current = null;
    try {
      const r = await latest((signal) => apiRun({ ...state.params, seed: state.seed, controller: state.controller }, signal));
      if (r.stale) return;
      render(r.value);
    } catch (e) {
      const msg = e instanceof ApiError ? e.message : 'Erro inesperado: ' + e.message;
      // Keep the pickers so the seed or controller can be corrected right here.
      clear(root).append(h('h2', {}, 'Execução'), controls(state), h('div', { class: 'banner-inline', role: 'alert' }, msg));
    }
  }

  return { load, getCurrent: () => current };
}
