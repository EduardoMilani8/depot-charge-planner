import { getDefaults, compare, createLatest, ApiError } from './api.js';
import { hashToState, decideHashAction, stateToHash, linkState, nextTab, TABS } from './params.js';
import { createForm } from './form.js';
import { renderCompare } from './compare.js';
import { createRunView } from './run.js';
import { createRealView } from './realdataView.js';
import { dropsRun } from './realdata.js';
import { h } from './dom.js';
import { applyMotionClass, setAnimations, animationsEnabled } from './motion.js';

const latest = createLatest();
let state;
let defaultParams; // the flat `params` of the /api/defaults reply
let form;
let runView;
let realView;
let compareView = null; // { dispose } of the comparison on screen
// What the Execução tab shows can differ from `state.params` (a newer comparison replaces those
// without touching the run on screen), so the screen's own values are tracked: the link and the
// run view's reloads use them, and a seed dot opens a run with the comparison's.
// `real` ({ files, night }) marks a night of imported data: it is reloaded with the same files and
// has no link, since the files stay in this page's memory.
let runSel = null; // { params, controller, seed, real? } of the run on screen (or loading), null when none
let compareParams = null; // params of the comparison on screen, null when none
const $ = (id) => document.getElementById(id);

function syncHash() {
  history.replaceState(null, '', stateToHash(linkState(state, runSel)));
}

export function showTab(name) {
  state.tab = name;
  if (name !== 'run') runView?.pause(); // a day left playing in a hidden tab would keep the frame loop busy
  for (const t of TABS) {
    $(`tab-${t}`).hidden = t !== name;
    const b = document.querySelector(`[data-tab="${t}"]`);
    b.setAttribute('aria-selected', String(t === name));
    b.tabIndex = t === name ? 0 : -1;
  }
  syncHash();
}

function banner(message) {
  const b = $('banner');
  b.textContent = message || '';
  b.hidden = !message;
}

// reportError puts a server error on its form field when it has one, else in the banner.
function reportError(e) {
  const msg = e instanceof ApiError ? e.message : 'Erro inesperado: ' + e.message;
  showTab('scenario');
  if (e instanceof ApiError && e.field && form.showError(e.field, e.message)) return;
  banner(msg);
}

// setCompare puts new content in the Comparação tab, stopping the counters of the old one.
function setCompare(...nodes) {
  compareView?.dispose();
  compareView = null;
  compareParams = null;
  $('compare-out').replaceChildren(...nodes);
}

async function runCompare(params) {
  banner('');
  state.params = params;
  form.setBusy(true);
  setCompare(h('p', { class: 'loading' }, 'Rodando as simulações…'));
  showTab('compare');
  try {
    const r = await latest((signal) => compare(params, signal));
    if (r.stale) return;
    setCompare();
    compareView = renderCompare($('compare-out'), r.value, (controller, seed) =>
      openRun(controller, seed, compareParams ?? state.params, { heading: true }));
    compareParams = r.value.params;
  } catch (e) {
    setCompare(h('p', { class: 'note' }, 'Nenhuma comparação: corrija o cenário e rode de novo.'));
    reportError(e);
  } finally {
    if (!latest.busy()) form.setBusy(false);
  }
}

// openRealRun shows one imported night (`real` = { files, night }) in the Execução tab. The form's
// state is left alone: the link of this screen only names the "Dados reais" tab.
function openRealRun(real, controller, opts = { heading: true }) {
  runSel = { params: state.params, controller, seed: 1, real };
  showTab('run');
  runView.load({ params: state.params, controller, seed: 1, real }, opts);
}

// openRun shows one run. `params` default to the form's; `opts.heading` moves focus to the run's
// heading once it is drawn (a seed dot was activated).
export function openRun(controller, seed, params = state.params, opts = {}) {
  state.controller = controller;
  state.seed = seed;
  runSel = { params, controller, seed };
  showTab('run');
  runView.load({ params, controller, seed }, opts);
}

let toastTimer = null;
function toast(message) {
  const t = $('toast');
  t.textContent = message;
  t.classList.add('show');
  clearTimeout(toastTimer);
  toastTimer = setTimeout(() => t.classList.remove('show'), 2500);
}

async function copyLink() {
  if (state.tab === 'run' && runSel?.real) {
    toast('Execuções de dados reais não geram link: os dados ficam só neste computador.');
    return;
  }
  syncHash();
  const url = location.href;
  try {
    await navigator.clipboard.writeText(url);
    toast(state.tab === 'real' ? 'Link copiado (só a aba; os dados não vão no link).' : 'Link copiado.');
  } catch {
    window.prompt('Copie o link:', url); // clipboard blocked: let the user copy it by hand
  }
}

// applyState shows `next` (read from the hash) as if the page had just loaded with that link.
function applyState(next) {
  // Whatever is still loading belongs to the previous link: drop it so it cannot render into a
  // tab that is now hidden or send its error to showTab('scenario').
  if (latest.busy()) setCompare(h('p', { class: 'note' }, 'Rode um cenário para ver a comparação.'));
  latest.cancel();
  if (runView.cancel()) runSel = null; // a load in flight was dropped: no run on screen any more
  form.setBusy(false);
  banner('');
  state = next;
  form.write(state.params);
  const wanted = state.tab; // showTab overwrites state.tab, so remember the link's view first
  showTab('scenario');
  if (wanted === 'compare') runCompare(state.params);
  if (wanted === 'real') showTab('real'); // only the tab comes from a link, never the data
  if (wanted === 'run') openRun(state.controller, state.seed);
}

// Pasting or editing a link in the same tab only changes the hash. showTab/syncHash use
// replaceState, which fires no hashchange, so only a hash that differs from the screen lands here.
function onHashChange() {
  if (!state) return;
  const next = decideHashAction(location.hash, linkState(state, runSel), defaultParams);
  if (next) applyState(next);
}

// The switch in the header; the page also follows the system's "reduce motion" setting while the
// user has not chosen.
function bindMotionToggle() {
  const btn = $('motion-toggle');
  const paint = () => {
    const on = animationsEnabled();
    btn.setAttribute('aria-pressed', String(on));
    btn.textContent = `Animações: ${on ? 'ligadas' : 'desligadas'}`;
  };
  btn.addEventListener('click', () => { setAnimations(!animationsEnabled()); paint(); });
  if (typeof matchMedia === 'function') {
    matchMedia('(prefers-reduced-motion: reduce)').addEventListener?.('change', () => { applyMotionClass(); paint(); });
  }
  paint();
}

async function init() {
  applyMotionClass();
  bindMotionToggle();
  let defaults;
  try {
    defaults = await getDefaults();
    defaultParams = defaults.params;
  } catch (e) {
    banner(e.message);
    return;
  }
  state = hashToState(location.hash, defaultParams);
  form = createForm($('tab-scenario'), defaults, { onRun: runCompare });
  runView = createRunView($('run-out'), {
    // The pickers of the run on screen reload it with the parameters it was computed with.
    onSelect: (controller, seed) => (runSel?.real
      ? openRealRun(runSel.real, controller, {})
      : openRun(controller, seed, runSel?.params ?? state.params)),
    onBack: () => { showTab('real'); document.querySelector('[data-tab="real"]').focus(); },
    onRendered: ({ data }) => {
      runSel = { params: data.params, controller: data.controller, seed: data.seed, real: data.source ? runSel?.real : undefined };
      if (state.tab === 'run') syncHash();
    },
  });
  realView = createRealView({
    filesRoot: $('real-files'), outRoot: $('real-out'),
    onOpenRun: ({ files, night, controller }) => openRealRun({ files, night }, controller),
    // "Limpar tudo" forgets the files, so the imported run in the Execução tab goes with them.
    onClear: () => {
      if (!dropsRun(runSel)) return;
      runView.reset();
      runSel = null;
      syncHash();
    },
  });
  for (const b of document.querySelectorAll('nav.tabs button')) b.addEventListener('click', () => showTab(b.dataset.tab));
  // Keyboard pattern of tabs: arrows (with wrap), Home and End move focus and activate the tab.
  $('tablist').addEventListener('keydown', (e) => {
    const next = nextTab(state.tab, e.key);
    if (!next) return;
    e.preventDefault();
    showTab(next);
    document.querySelector(`[data-tab="${next}"]`).focus();
  });
  $('copy-link').addEventListener('click', copyLink);
  window.addEventListener('hashchange', onHashChange);
  applyState(state);
}

init();
