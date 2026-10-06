import { h } from './dom.js';
import { fieldSpecs } from './params.js';

// createForm builds the scenario form inside root. defaults is the /api/defaults reply.
export function createForm(root, defaults, handlers) {
  const specs = fieldSpecs(defaults.limits);
  const inputs = new Map();
  const errors = new Map();

  function field(spec) {
    const id = `f-${spec.key}`;
    let input;
    if (spec.kind === 'select') {
      input = h('select', { id, name: spec.key }, spec.options.map(([v, l]) => h('option', { value: v }, l)));
    } else if (spec.kind === 'bool') {
      input = h('input', { id, name: spec.key, type: 'checkbox' });
    } else {
      input = h('input', {
        id, name: spec.key, type: 'number', min: spec.min, max: spec.max,
        step: spec.kind === 'int' ? '1' : 'any', inputmode: spec.kind === 'int' ? 'numeric' : 'decimal',
      });
    }
    const err = h('div', { class: 'field-error', id: `e-${spec.key}`, role: 'alert' });
    input.setAttribute('aria-describedby', err.id);
    inputs.set(spec.key, { input, spec });
    errors.set(spec.key, err);
    const label = h('label', { for: id }, spec.label);
    return spec.kind === 'bool' ? h('div', { class: 'field check' }, input, label, err) : h('div', { class: 'field' }, label, input, err);
  }

  function write(params) {
    for (const [key, { input, spec }] of inputs) {
      if (params[key] === undefined) continue;
      if (spec.kind === 'bool') input.checked = Boolean(params[key]);
      else input.value = String(params[key]);
    }
    clearErrors();
  }

  function read() {
    const p = {};
    for (const [key, { input, spec }] of inputs) {
      if (spec.kind === 'bool') p[key] = input.checked;
      else if (spec.kind === 'select') p[key] = input.value;
      else p[key] = input.value === '' ? NaN : Number(input.value);
    }
    return p;
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
    errors.get(key).textContent = message;
    entry.input.setAttribute('aria-invalid', 'true');
    entry.input.focus();
    return true;
  }

  const run = h('button', { type: 'submit', class: 'primary' }, 'Rodar');
  function setBusy(busy) {
    run.disabled = busy;
    run.textContent = busy ? 'Rodando…' : 'Rodar';
  }

  const main = specs.filter((s) => !s.advanced).map(field);
  const advanced = specs.filter((s) => s.advanced).map(field);
  const presets = h('div', { class: 'presets' },
    h('span', { class: 'label' }, 'Cenários prontos:'),
    defaults.presets.map((p) => h('button', { type: 'button', class: 'chip', title: p.description, onclick: () => write(p.params) }, p.label)));
  const form = h('form', { class: 'form', novalidate: true },
    presets,
    h('div', { class: 'grid' }, main),
    h('details', { class: 'advanced' }, h('summary', {}, 'Avançado: regra anti-vaivém do rodízio'), h('div', { class: 'grid' }, advanced)),
    h('div', { class: 'actions' }, run));
  form.addEventListener('submit', (e) => {
    e.preventDefault();
    clearErrors();
    const params = read();
    const bad = Object.entries(params).find(([, v]) => typeof v === 'number' && Number.isNaN(v));
    if (bad) {
      showError(bad[0], 'Informe um número.');
      return;
    }
    handlers.onRun(params);
  });
  root.replaceChildren(h('h2', {}, 'Cenário'), form);
  return { read, write, setBusy, showError, clearErrors };
}
