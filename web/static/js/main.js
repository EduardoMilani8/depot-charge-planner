import { getDefaults, compare, createLatest, ApiError } from './api.js';
import { hashToState, hashMatchesState, stateToHash, nextTab, TABS } from './params.js';
import { createForm } from './form.js';
import { renderCompare } from './compare.js';
import { createRunView } from './run.js';
import { h } from './dom.js';

const latest = createLatest();
let state;
let defaults;
let form;
let runView;
const $ = (id) => document.getElementById(id);

function syncHash() {
  history.replaceState(null, '', stateToHash(state));
}

export function showTab(name) {
  state.tab = name;
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

async function runCompare(params) {
  banner('');
  state.params = params;
  form.setBusy(true);
  $('compare-out').replaceChildren(h('p', { class: 'loading' }, 'Rodando as simulações…'));
  showTab('compare');
  try {
    const r = await latest((signal) => compare(params, signal));
    if (r.stale) return;
    renderCompare($('compare-out'), r.value, openRun);
  } catch (e) {
    $('compare-out').replaceChildren(h('p', { class: 'note' }, 'Nenhuma comparação: corrija o cenário e rode de novo.'));
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
  if (!state || hashMatchesState(location.hash, state, defaults)) return;
  applyState(hashToState(location.hash, defaults.params));
}

async function init() {
  try {
    defaults = await getDefaults();
  } catch (e) {
    banner(e.message);
    return;
  }
  state = hashToState(location.hash, defaults.params);
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
