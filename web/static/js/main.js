import { getDefaults, compare, createLatest, ApiError } from './api.js';
import { hashToState, decideHashAction, stateToHash, nextTab, TABS } from './params.js';
import { createForm } from './form.js';
import { renderCompare } from './compare.js';
import { createRunView } from './run.js';
import { h } from './dom.js';
import { applyMotionClass, setAnimations, animationsEnabled } from './motion.js';

const latest = createLatest();
let state;
let defaultParams; // the flat `params` of the /api/defaults reply
let form;
let runView;
let compareView = null; // { dispose } of the comparison on screen
const $ = (id) => document.getElementById(id);

function syncHash() {
  history.replaceState(null, '', stateToHash(state));
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
    compareView = renderCompare($('compare-out'), r.value, openRun);
  } catch (e) {
    setCompare(h('p', { class: 'note' }, 'Nenhuma comparação: corrija o cenário e rode de novo.'));
    reportError(e);
  } finally {
    if (!latest.busy()) form.setBusy(false);
  }
}

export function openRun(controller, seed) {
  state.controller = controller;
  state.seed = seed;
  showTab('run');
  runView.load(state);
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
  syncHash();
  const url = location.href;
  try {
    await navigator.clipboard.writeText(url);
    toast('Link copiado.');
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
  runView.cancel();
  form.setBusy(false);
  banner('');
  state = next;
  form.write(state.params);
  const wanted = state.tab; // showTab overwrites state.tab, so remember the link's view first
  showTab('scenario');
  if (wanted === 'compare') runCompare(state.params);
  if (wanted === 'run') openRun(state.controller, state.seed);
}

// Pasting or editing a link in the same tab only changes the hash. showTab/syncHash use
// replaceState, which fires no hashchange, so only a hash that differs from the screen lands here.
function onHashChange() {
  if (!state) return;
  const next = decideHashAction(location.hash, state, defaultParams);
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
  runView = createRunView($('run-out'), { onSelect: (controller, seed) => openRun(controller, seed) });
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
