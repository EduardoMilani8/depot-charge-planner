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
import { createPlayer, countingText } from './motion.js';
import { eventsBetween } from './events.js';
import { createFeed } from './feed.js';

const SPEEDS = [[30, '30 min/s'], [60, '1 h/s'], [120, '2 h/s'], [300, '5 h/s'], [600, '10 h/s']];
// Elements where Space already means something else.
const OWN_SPACE = new Set(['SELECT', 'BUTTON', 'TEXTAREA', 'SUMMARY', 'A']);

const IDLE_NOTE = 'Nenhuma execução aberta. Rode um cenário e, na Comparação, clique num ponto de semente para ver aquela execução minuto a minuto.';

// createRunView owns the "Execução" tab: it loads one run and draws its panels.
export function createRunView(root, hooks) {
  const latest = createLatest();
  let current = null; // { data, cursor, selection } of the run on screen
  let loading = false;
  let player = null; // plays the run on screen
  let disposers = []; // everything the run on screen started: frame loops, timers, subscriptions

  // teardown stops whatever the run on screen was doing (playback, counters, throttled redraws).
  function teardown() {
    const ds = disposers;
    disposers = [];
    ds.forEach((d) => d());
    player?.destroy();
    player = null;
    root.classList.remove('playing');
  }

  // summaryLine is the one-line result; the share of ready buses counts up to its value.
  function summaryLine(m) {
    const bad = m.plan_violations > 0;
    const pct = countingText(m.ready_pct, fmtPct, 800);
    disposers.push(pct.cancel);
    return h('p', { class: 'summary' }, `${m.ready} de ${m.buses} ônibus saíram prontos (`, pct.node,
      `) · déficit ${fmtNum(m.shortfall_kwh, 0)} kWh · pico ${fmtNum(m.peak_kw, 0)} kW · ` +
      `${bad ? '⚠ ' : ''}violações do plano ${m.plan_violations} · custo ${fmtBRL(m.cost_brl)}`);
  }

  // playerRow draws the buttons that play the day; the play button also lives in the sticky bar.
  function playerRow(p) {
    const restart = h('button', { type: 'button', class: 'chip' }, h('span', { 'aria-hidden': 'true' }, '⏮ '), 'Do começo');
    const speed = h('select', { 'aria-label': 'Velocidade da reprodução' },
      SPEEDS.map(([v, l]) => h('option', { value: v, selected: v === p.state.speed }, l)));
    const loop = h('input', { type: 'checkbox' });
    restart.addEventListener('click', () => p.restart());
    speed.addEventListener('change', () => p.setSpeed(Number(speed.value)));
    loop.addEventListener('change', () => p.setLoop(loop.checked));
    return h('div', { class: 'player-row' }, restart, h('label', {}, 'Velocidade ', speed), h('label', {}, loop, ' repetir'));
  }

  function playButton(p) {
    const icon = h('span', { 'aria-hidden': 'true' });
    const text = h('span');
    const btn = h('button', { type: 'button', class: 'primary play' }, icon, text);
    const paint = (s) => {
      icon.textContent = s.playing ? '⏸ ' : '▶ ';
      text.textContent = s.playing ? 'Pausar' : 'Reproduzir';
      root.classList.toggle('playing', s.playing);
    };
    btn.addEventListener('click', () => p.toggle());
    p.onChange(paint);
    paint(p.state);
    return btn;
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

  function cursorRow(data, cursor, play) {
    const slider = h('input', { type: 'range', min: 0, max: data.scenario.horizon_min, step: 1, value: 0, 'aria-label': 'Minuto da simulação', class: 'time-slider' });
    const label = h('output', { class: 'time-label' });
    slider.addEventListener('input', () => cursor.set(Number(slider.value)));
    cursor.onChange((m) => {
      slider.value = m;
      label.textContent = `minuto ${m} · ${clock(data.scenario.start_clock_min, m)}`;
    });
    label.textContent = `minuto 0 · ${clock(data.scenario.start_clock_min, 0)}`;
    return h('div', { class: 'cursor-row' }, play, slider, label, h('span', { class: 'live-dot', 'aria-hidden': 'true', title: 'reproduzindo' }));
  }

  function faultList(data) {
    if (data.scenario.faults.length === 0) return h('p', { class: 'note' }, 'Este cenário não tem falhas.');
    return h('details', { class: 'faults' }, h('summary', {}, `Falhas deste cenário (${data.scenario.faults.length})`),
      h('ul', {}, data.scenario.faults.map((f) => h('li', {}, describeFault(f)))));
  }

  function render(data) {
    teardown();
    const cursor = createCursor(data.scenario.horizon_min);
    const selection = createSelection();
    player = createPlayer(cursor);
    const powerPanel = h('div', { class: 'panel', id: 'panel-power' });
    const feedPanel = h('div', { class: 'panel', id: 'panel-feed' });
    const busPanel = h('div', { class: 'panel', id: 'panel-buses' });
    const chargerPanel = h('div', { class: 'panel', id: 'panel-chargers' });
    const decisionPanel = h('div', { class: 'panel', id: 'panel-decision' });
    clear(root).append(
      h('h2', {}, 'Execução'),
      controls(data),
      summaryLine(data.metrics),
      faultList(data),
      h('div', { class: 'transport' }, playerRow(player), cursorRow(data, cursor, playButton(player))),
      powerPanel, feedPanel, busPanel, chargerPanel, decisionPanel);
    disposers.push(renderPowerChart(powerPanel, data, cursor, player).dispose);
    const feed = createFeed(feedPanel, { startClock: data.scenario.start_clock_min });
    // The feed follows the day while it plays. A cursor moved by hand only updates `lastMinute`
    // (eventsBetween ignores jumps), and a wrap-around of "repetir" starts the list over.
    let lastMinute = -1;
    const span = () => Math.max(30, player.state.speed / 2); // half a second of playback, so a slow frame loses nothing
    cursor.onChange((m) => {
      if (player.state.playing) {
        if (m < lastMinute) { feed.clear(); feed.push(eventsBetween(data, -1, m, span())); } else feed.push(eventsBetween(data, lastMinute, m, span()));
      }
      lastMinute = m;
    });
    player.onChange((s) => {
      if (!s.playing) return;
      if (cursor.value === 0) { feed.clear(); lastMinute = -1; } else lastMinute = cursor.value;
    });
    const busView = renderBusTimeline(busPanel, data, cursor, {
      onSelectBus: (id) => selection.set(id),
      onJump: () => {
        const target = decisionPanel.querySelector('.bus-detail');
        if (!target) return;
        target.scrollIntoView({ block: 'start' });
        target.focus({ preventScroll: true }); // so a screen reader announces the section it landed on
      },
    }, player);
    disposers.push(busView.dispose);
    selection.onChange((id) => busView.setSelected(id));
    disposers.push(renderChargerTimeline(chargerPanel, data, cursor).dispose);
    disposers.push(renderDecisionPanel(decisionPanel, data, cursor, selection).dispose);
    current = { data, cursor, selection };
    hooks.onRendered?.(current);
  }

  async function load(state) {
    teardown();
    clear(root).append(h('h2', {}, 'Execução'), h('p', { class: 'loading' }, 'Calculando a execução…'));
    current = null;
    loading = true;
    try {
      const r = await latest((signal) => apiRun({ ...state.params, seed: state.seed, controller: state.controller }, signal));
      if (r.stale) return;
      loading = false;
      render(r.value);
    } catch (e) {
      loading = false;
      const msg = e instanceof ApiError ? e.message : 'Erro inesperado: ' + e.message;
      // Keep the pickers so the seed or controller can be corrected right here.
      clear(root).append(h('h2', {}, 'Execução'), controls(state), h('div', { class: 'banner-inline', role: 'alert' }, msg));
    }
  }

  // cancel drops a load in flight (its result is never drawn); the "Calculando…" text goes back
  // to the idle note. A run already on screen is left alone.
  function cancel() {
    latest.cancel();
    pause();
    if (!loading) return;
    loading = false;
    clear(root).append(h('p', { class: 'note' }, IDLE_NOTE));
  }

  // pause stops the day playing (leaving the tab, hiding the page, a new link); the run stays.
  function pause() {
    player?.pause();
  }

  // Space plays or pauses when it is not already doing something in the control that has focus.
  root.addEventListener('keydown', (e) => {
    if (e.code !== 'Space' || !player || e.defaultPrevented || e.ctrlKey || e.metaKey || e.altKey) return;
    const tag = e.target.tagName;
    if (OWN_SPACE.has(tag) || (tag === 'INPUT' && e.target.type !== 'range') || e.target.closest?.('[role=button]')) return;
    e.preventDefault();
    player.toggle();
  });
  // A hidden page gets no frames; on return the clock would have jumped, so stop instead.
  document.addEventListener('visibilitychange', () => { if (document.hidden) pause(); });

  return { load, cancel, pause, getCurrent: () => current };
}
