import { h } from './dom.js';
import { fieldSpecs } from './params.js';
import { normalizeCostParams, hoursToMin, minToHours, hoursMessage } from './cost.js';

const GROUPS = {
  swap: 'Rodízio (anti-vaivém)',
  cost: 'Custo de energia e demanda',
};

// createForm builds the scenario form inside root. defaults is the /api/defaults reply.
export function createForm(root, defaults, handlers) {
  const specs = fieldSpecs(defaults.limits);
  const inputs = new Map();
  const errors = new Map();
  const wrappers = new Map();
  const verdeNotes = [];

  function field(spec) {
    const id = `f-${spec.key}`;
    let input;
    if (spec.kind === 'select') {
      input = h('select', { id, name: spec.key }, spec.options.map(([v, l]) => h('option', { value: v }, l)));
    } else if (spec.kind === 'bool') {
      input = h('input', { id, name: spec.key, type: 'checkbox' });
    } else {
      const whole = spec.kind === 'int';
      input = h('input', {
        id, name: spec.key, type: 'number', min: spec.min, max: spec.max, placeholder: spec.placeholder,
        step: whole ? '1' : 'any', inputmode: whole ? 'numeric' : 'decimal',
      });
    }
    const err = h('div', { class: 'field-error', id: `e-${spec.key}`, role: 'alert' });
    const hint = spec.hint ? h('div', { class: 'field-hint', id: `h-${spec.key}` }, spec.hint) : null;
    // The Verde note comes from the server (limits.demand_verde_note) and shows only in that mode.
    const note = spec.note ? h('div', { class: 'field-hint verde-note', id: `n-${spec.key}`, hidden: true }, spec.note) : null;
    if (note) verdeNotes.push({ input, note });
    const needs = spec.needsCostAware
      ? h('div', { class: 'field-hint needs', id: `w-${spec.key}`, hidden: true }, 'Só vale com o planejador consciente de custo ligado.') : null;
    input.setAttribute('aria-describedby', [hint && hint.id, note && note.id, needs && needs.id, err.id].filter(Boolean).join(' '));
    inputs.set(spec.key, { input, spec, needs });
    errors.set(spec.key, err);
    const label = h('label', { for: id }, spec.label);
    const box = spec.kind === 'bool'
      ? h('div', { class: 'field check' }, input, label, hint, needs, err)
      : h('div', { class: 'field' }, label, input, hint, note, needs, err);
    wrappers.set(spec.key, box);
    return box;
  }

  // syncDependents: the margin and the demand target only count with cost_aware on (the server
  // rejects them otherwise), so they are disabled, emptied and marked while it is off. The Verde
  // note follows the mode.
  function syncDependents() {
    const on = inputs.get('cost_aware').input.checked;
    for (const { input, spec, needs } of inputs.values()) {
      if (!spec.needsCostAware) continue;
      input.disabled = !on;
      if (!on) input.value = '';
      needs.hidden = on;
      wrappers.get(spec.key).classList.toggle('inactive', !on);
    }
    for (const { input, note } of verdeNotes) note.hidden = input.value !== 'verde';
  }

  function write(params) {
    for (const [key, { input, spec }] of inputs) {
      // The cost fields are optional in a request, so a missing one means "default" and must reset the field.
      const v = spec.group === 'cost' && params[key] === undefined ? (spec.kind === 'bool' || spec.kind === 'select' ? false : '') : params[key];
      if (v === undefined) continue;
      if (spec.kind === 'bool') input.checked = Boolean(v);
      else if (spec.kind === 'select' && spec.encode) input.value = spec.encode(v);
      else if (spec.kind === 'hours') input.value = v === '' ? '' : String(minToHours(v));
      else input.value = String(v);
    }
    syncDependents();
    clearErrors();
  }

  // read gives the request parameters; cost fields at their default are left out.
  function read() {
    const p = {};
    for (const [key, { input, spec }] of inputs) {
      if (spec.kind === 'bool') p[key] = input.checked;
      else if (spec.kind === 'select') p[key] = spec.decode ? spec.decode(input.value) : input.value;
      else if (spec.optional && input.value === '') continue;
      else if (spec.kind === 'hours') p[key] = hoursToMin(Number(input.value));
      else p[key] = input.value === '' ? NaN : Number(input.value);
    }
    return normalizeCostParams(p);
  }

  function clearErrors() {
    for (const [key, err] of errors) {
      err.textContent = '';
      inputs.get(key).input.removeAttribute('aria-invalid');
    }
  }

  // showError marks a field; it returns false when the form has no such field.
  function showError(key, message) {
    const entry = inputs.get(key);
    if (!entry) return false;
    // The arrival fields are typed in hours; the server's message speaks minutes.
    errors.get(key).textContent = entry.spec.kind === 'hours' ? hoursMessage(message) : message;
    entry.input.setAttribute('aria-invalid', 'true');
    // A field inside the collapsed "Avançado" block is not rendered until it is opened.
    const details = entry.input.closest('details');
    if (details) details.open = true;
    entry.input.focus();
    return true;
  }

  const run = h('button', { type: 'submit', class: 'primary' }, 'Rodar');
  function setBusy(busy) {
    run.disabled = busy;
    run.textContent = busy ? 'Rodando…' : 'Rodar';
  }

  // What the server says about valid parameters that do less than they seem to.
  const warnings = h('div', { class: 'warnings', role: 'status', hidden: true });
  function setWarnings(list) {
    warnings.replaceChildren();
    if (list && list.length) {
      warnings.append(h('strong', {}, 'Avisos da última comparação:'), h('ul', {}, list.map((w) => h('li', {}, '⚠ ' + w))));
    }
    warnings.hidden = !(list && list.length);
  }

  // A night of real data opened in the Execução tab ignores the cost fields (the server reads only the
  // spreadsheets there), so they are shown as inactive until a simulated scenario is chosen again.
  // The hint sits outside the collapsed "Avançado" block (and outside the disabled fieldset, so its button
  // works): it is what tells why the cost fields below are inactive.
  const realHint = h('div', { class: 'real-hint', hidden: true },
    h('span', {}, 'Uma noite de dados reais está aberta na Execução e ignora os campos de custo (em Avançado): eles só valem para cenários simulados. '),
    h('button', { type: 'button', class: 'chip', onclick: () => setRealMode(false) }, 'Voltar ao cenário simulado'));
  const costSet = h('fieldset', { class: 'group group-cost' }, h('legend', {}, GROUPS.cost));
  function setRealMode(on) {
    costSet.disabled = on;
    costSet.classList.toggle('inactive', on);
    realHint.hidden = !on;
  }

  const main = specs.filter((s) => !s.advanced).map(field);
  const swapSet = h('fieldset', { class: 'group' }, h('legend', {}, GROUPS.swap), h('div', { class: 'grid' }, specs.filter((s) => s.group === 'swap').map(field)));
  costSet.append(h('div', { class: 'grid' }, specs.filter((s) => s.group === 'cost').map(field)));
  const presets = h('div', { class: 'presets' },
    h('span', { class: 'label' }, 'Cenários prontos:'),
    defaults.presets.map((p) => h('button', { type: 'button', class: 'chip', title: p.description, onclick: () => { setRealMode(false); write(p.params); } }, p.label)));
  const form = h('form', { class: 'form', novalidate: true },
    presets,
    h('div', { class: 'grid' }, main),
    realHint,
    h('details', { class: 'advanced' }, h('summary', {}, 'Avançado: rodízio e custo de energia'), swapSet, costSet),
    h('div', { class: 'actions' }, run),
    warnings);
  inputs.get('cost_aware').input.addEventListener('change', syncDependents);
  inputs.get('demand_verde').input.addEventListener('change', syncDependents);
  form.addEventListener('input', () => setWarnings(null)); // the warnings belong to the values they were computed for
  form.addEventListener('submit', (e) => {
    e.preventDefault();
    clearErrors();
    setRealMode(false);
    const params = read();
    const bad = Object.entries(params).find(([, v]) => typeof v === 'number' && Number.isNaN(v));
    if (bad) {
      showError(bad[0], 'Informe um número.');
      return;
    }
    handlers.onRun(params);
  });
  root.replaceChildren(h('h2', {}, 'Cenário'), form);
  syncDependents();
  return { read, write, setBusy, showError, clearErrors, setWarnings, setRealMode };
}
