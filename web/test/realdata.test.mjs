import test from 'node:test';
import assert from 'node:assert/strict';
import {
  FILE_SPECS, NO_SWAP, readFileText, pickKnownFiles, formatImportError, realRowCells, controllerCells,
  worstBuses, busStatus, summarizeImport, pickRows, columns, runRequestBody, controllerOptions, controllerHelp,
  compareButtonState, checkSizes, pickOption, importStatusText, errorStatusText, fmtSize, MAX_FILE_BYTES, MAX_BODY_BYTES,
  bodySize, checkBody, describeFailure, failureStatusText, partialText,
} from '../static/js/realdata.js';
import { ApiError } from '../static/js/api.js';

const bytes = (...b) => new Uint8Array(b).buffer;
const enc = (s) => new TextEncoder().encode(s).buffer;
const file = (name, extra = {}) => ({ name, size: 10, ...extra });

// ---- readFileText ----

test('readFileText decodes UTF-8', () => {
  assert.equal(readFileText(enc('garagem,ônibus ç')), 'garagem,ônibus ç');
});

test('readFileText drops a UTF-8 byte order mark', () => {
  assert.equal(readFileText(bytes(0xef, 0xbb, 0xbf, 0x61)), 'a');
});

test('readFileText falls back to Windows-1252 when the bytes are not UTF-8 (0xE7 is ç)', () => {
  assert.equal(readFileText(bytes(0x61, 0xe7, 0xe3, 0x6f)), 'a\u00e7\u00e3o');
  assert.equal(readFileText(bytes(0xe7)), '\u00e7');
  assert.equal(readFileText(bytes(0xd4, 0x6e, 0x69, 0x62, 0x75, 0x73)), '\u00d4nibus');
});

test('readFileText takes typed arrays and empty input', () => {
  assert.equal(readFileText(new Uint8Array([0xe7])), 'ç');
  assert.equal(readFileText(new ArrayBuffer(0)), '');
});

// ---- pickKnownFiles ----

test('pickKnownFiles keeps the five known names, whatever the case', () => {
  const list = [file('ONIBUS.CSV'), file('Garagem.csv'), file('carregadores.csv'), file('Sessoes.Csv'), file('potencia.csv')];
  const { known, ignored } = pickKnownFiles(list);
  assert.deepEqual(Object.keys(known).sort(), ['carregadores.csv', 'garagem.csv', 'onibus.csv', 'potencia.csv', 'sessoes.csv']);
  assert.equal(known['onibus.csv'], list[0]);
  assert.deepEqual(ignored, []);
});

test('pickKnownFiles lists what it ignores, in order', () => {
  const { known, ignored } = pickKnownFiles([file('onibus.csv'), file('LEIAME.txt'), file('onibus_antigo.csv'), file('onibus.csv.bak')]);
  assert.deepEqual(Object.keys(known), ['onibus.csv']);
  assert.deepEqual(ignored, ['LEIAME.txt', 'onibus_antigo.csv', 'onibus.csv.bak']);
});

test('pickKnownFiles accepts the files of a dropped folder by their base name', () => {
  const { known, ignored } = pickKnownFiles([
    file('onibus.csv', { webkitRelativePath: 'garagem-x/onibus.csv' }),
    file('GARAGEM.CSV', { webkitRelativePath: 'garagem-x/GARAGEM.CSV' }),
    file('a.csv', { webkitRelativePath: 'garagem-x/sub/a.csv' }),
    file('C:\\dados\\carregadores.csv'),
  ]);
  assert.deepEqual(Object.keys(known).sort(), ['carregadores.csv', 'garagem.csv', 'onibus.csv']);
  assert.deepEqual(ignored, ['a.csv']);
});

test('pickKnownFiles keeps the first of two files with the same name and reports the other', () => {
  const a = file('onibus.csv', { webkitRelativePath: 'x/onibus.csv' });
  const b = file('onibus.csv', { webkitRelativePath: 'y/onibus.csv' });
  const { known, ignored } = pickKnownFiles([a, b]);
  assert.equal(known['onibus.csv'], a);
  assert.deepEqual(ignored, ['y/onibus.csv (repetido)']);
});

test('pickKnownFiles takes any array-like (a FileList) and empty input', () => {
  const list = { length: 2, 0: file('onibus.csv'), 1: file('x.csv'), item(i) { return this[i]; } };
  const { known, ignored } = pickKnownFiles(list);
  assert.deepEqual(Object.keys(known), ['onibus.csv']);
  assert.deepEqual(ignored, ['x.csv']);
  assert.deepEqual(pickKnownFiles([]), { known: {}, ignored: [] });
  assert.deepEqual(pickKnownFiles(null), { known: {}, ignored: [] });
});

test('FILE_SPECS names the five files and which ones are required', () => {
  assert.deepEqual(FILE_SPECS.map((f) => f.name), ['garagem.csv', 'carregadores.csv', 'onibus.csv', 'sessoes.csv', 'potencia.csv']);
  assert.deepEqual(FILE_SPECS.filter((f) => f.required).map((f) => f.name), ['garagem.csv', 'carregadores.csv', 'onibus.csv']);
});

// ---- formatImportError ----

test('formatImportError splits the located prefix from the plain message', () => {
  const e = new ApiError('onibus.csv, linha 2, coluna soc_chegada_pct: 120 % fora da faixa', 'onibus.csv', 400,
    { file: 'onibus.csv', line: 2, column: 'soc_chegada_pct' });
  assert.deepEqual(formatImportError(e), {
    file: 'onibus.csv', line: 2, column: 'soc_chegada_pct', message: '120 % fora da faixa',
    text: 'onibus.csv, linha 2, coluna soc_chegada_pct: 120 % fora da faixa',
  });
});

test('formatImportError handles an error with a file only, and one with no location', () => {
  const f = new ApiError('garagem.csv: arquivo obrigatório ausente', 'garagem.csv', 400, { file: 'garagem.csv', line: 0, column: '' });
  assert.deepEqual(formatImportError(f), { file: 'garagem.csv', line: 0, column: '', message: 'arquivo obrigatório ausente', text: 'garagem.csv: arquivo obrigatório ausente' });
  const g = new ApiError('Não consegui falar com o servidor.', '', 0);
  assert.deepEqual(formatImportError(g), { file: '', line: 0, column: '', message: 'Não consegui falar com o servidor.', text: 'Não consegui falar com o servidor.' });
  assert.equal(formatImportError(new Error('boom')).message, 'boom');
  assert.equal(formatImportError(null).message, 'Erro desconhecido.');
});

test('formatImportError leaves a message alone when it does not start with the location', () => {
  const e = new ApiError('outra coisa', 'onibus.csv', 400, { file: 'onibus.csv', line: 3, column: 'capacidade_kwh' });
  const out = formatImportError(e);
  assert.equal(out.message, 'outra coisa');
  assert.equal(out.text, 'onibus.csv, linha 3, coluna capacidade_kwh: outra coisa');
});

// ---- realRowCells ----

const real = {
  buses: 3, with_outcome: 3, ready: 2, ready_pct: 66.66666, shortfall_kwh: 12.4,
  energy_kwh: 812.3, peak_kw: 410.2, peak_estimated: true, cost_brl: 900.4, cost_estimated: true,
};
const byKey = (cells) => Object.fromEntries(cells.map((c) => [c.key, c]));

test('columns are the laboratory columns without p99', () => {
  assert.deepEqual(columns().map((c) => c.key), [
    'ready_pct', 'shortfall_kwh', 'peak_kw', 'plan_violations', 'overshoot_min', 'energy_kwh', 'cost_brl', 'plan_changes', 'operator_moves',
  ]);
});

test('realRowCells: measured figures plain, estimated ones sealed, unmeasured ones with a dash', () => {
  const cells = realRowCells(real);
  assert.deepEqual(cells.map((c) => c.key), columns().map((c) => c.key));
  const c = byKey(cells);
  assert.equal(c.ready_pct.text, '66,7%');
  assert.equal(c.ready_pct.estimated, false);
  assert.equal(c.shortfall_kwh.text, '12');
  assert.equal(c.peak_kw.text, '410');
  assert.equal(c.peak_kw.estimated, true);
  assert.equal(c.energy_kwh.text, '812');
  assert.equal(c.energy_kwh.estimated, false);
  assert.equal(c.cost_brl.text, 'R$ 900');
  assert.equal(c.cost_brl.estimated, true);
  for (const k of ['plan_violations', 'overshoot_min', 'plan_changes', 'operator_moves']) {
    assert.equal(c[k].text, '—', k);
    assert.equal(c[k].estimated, false, k);
  }
});

test('realRowCells: a measured peak and cost carry no seal', () => {
  const c = byKey(realRowCells({ ...real, peak_estimated: false, cost_estimated: false }));
  assert.equal(c.peak_kw.estimated, false);
  assert.equal(c.cost_brl.estimated, false);
  assert.equal(c.cost_brl.text, 'R$ 900');
});

test('realRowCells: null figures are dashes and never sealed', () => {
  const c = byKey(realRowCells({ ...real, energy_kwh: null, peak_kw: null, cost_brl: null }));
  for (const k of ['energy_kwh', 'peak_kw', 'cost_brl']) {
    assert.equal(c[k].text, '—', k);
    assert.equal(c[k].estimated, false, k);
  }
});

test('realRowCells: a partial night seals energy, peak and cost with the note, and only those', () => {
  const c = byKey(realRowCells({ ...real, partial: true, covered_buses: 2, partial_note: 'parcial: 2 de 3 ônibus' }));
  for (const k of ['energy_kwh', 'peak_kw', 'cost_brl']) assert.equal(c[k].partial, 'parcial: 2 de 3 ônibus', k);
  for (const k of ['ready_pct', 'shortfall_kwh', 'plan_violations']) assert.equal(c[k].partial, '', k);
  // the value is kept (sealed, not nulled) and the estimated seal is independent
  assert.equal(c.energy_kwh.text, '812');
  assert.equal(c.peak_kw.estimated, true);
  // a complete night, or a report without the field, has no partial seal
  for (const r of [real, { ...real, partial: false, partial_note: '' }]) {
    for (const cell of realRowCells(r)) assert.equal(cell.partial, '', cell.key);
  }
  // a figure the spreadsheets cannot give stays a plain dash
  const n = byKey(realRowCells({ ...real, partial: true, partial_note: 'parcial: 2 de 3 ônibus', cost_brl: null }));
  assert.equal(n.cost_brl.text, '—');
  assert.equal(n.cost_brl.partial, '');
});

test('partialText explains a partial real row and is empty otherwise', () => {
  assert.equal(partialText(real), '');
  assert.equal(partialText(null), '');
  assert.equal(partialText({ ...real, partial: false }), '');
  const t = partialText({ ...real, partial: true, partial_note: 'parcial: 2 de 3 ônibus' });
  assert.match(t, /parcial: 2 de 3 ônibus/);
  assert.match(t, /não são comparáveis/);
});

test('realRowCells: a night with no measured departures has no readiness and no shortfall', () => {
  const c = byKey(realRowCells({ ...real, with_outcome: 0, ready: 0, ready_pct: 0, shortfall_kwh: 0 }));
  assert.equal(c.ready_pct.text, '—');
  assert.equal(c.shortfall_kwh.text, '—');
});

test('realRowCells: readiness over only some buses says how many', () => {
  const c = byKey(realRowCells({ ...real, buses: 3, with_outcome: 2, ready: 1, ready_pct: 50 }));
  assert.equal(c.ready_pct.text, '50,0%');
  assert.match(c.ready_pct.detail, /2 de 3/);
  assert.equal(byKey(realRowCells(real)).ready_pct.detail, '');
});

test('realRowCells: a missing or empty real object is all dashes', () => {
  for (const r of [null, undefined, {}]) {
    for (const cell of realRowCells(r)) {
      assert.equal(cell.text, '—');
      assert.equal(cell.estimated, false);
    }
  }
});

// ---- controllerCells ----

const metrics = {
  buses: 3, ready: 3, ready_pct: 100, shortfall_kwh: 0, peak_kw: 380.4, plan_violations: 0, overshoot_min: 0,
  energy_kwh: 700.2, cost_brl: 640.9, plan_changes: 12, operator_moves: 1.5, plan_p99_micros: 0,
};

test('controllerCells formats the nine columns and never shows p99', () => {
  const cells = controllerCells({ name: 'planner', aggregate: metrics }, true);
  assert.equal(cells.length, 9);
  const c = byKey(cells);
  assert.equal(c.ready_pct.text, '100,0%');
  assert.equal(c.cost_brl.text, 'R$ 641');
  assert.equal(c.operator_moves.text, '1,5');
  assert.equal(cells.some((x) => x.key === 'plan_p99_micros'), false);
  assert.equal(c.plan_violations.bad, false);
});

test('controllerCells: cost is a dash when it cannot be compared, with the reason', () => {
  const c = byKey(controllerCells({ name: 'fifo', aggregate: metrics }, false));
  assert.equal(c.cost_brl.text, '—');
  assert.match(c.cost_brl.title, /tarifa|janela/i);
  assert.equal(c.energy_kwh.text, '700');
});

test('controllerCells flags plan violations', () => {
  const c = byKey(controllerCells({ name: 'x', aggregate: { ...metrics, plan_violations: 2 } }, true));
  assert.equal(c.plan_violations.bad, true);
});

// ---- pickRows ----

const night1 = { key: '2026-03-04', buses: 3, real, controllers: [{ name: 'planner' }], per_bus: [] };
const report = { nights: [night1, { key: '2026-03-05' }], aggregate: [{ name: 'agg' }], real_aggregate: { buses: 6 }, cost_comparable: true };

test('pickRows: "all" is the mean over nights, a key is that night', () => {
  assert.deepEqual(pickRows(report, 'all'), { real: report.real_aggregate, controllers: report.aggregate, night: null });
  const one = pickRows(report, '2026-03-04');
  assert.equal(one.real, real);
  assert.equal(one.controllers, night1.controllers);
  assert.equal(one.night, night1);
  assert.equal(pickRows(report, '1999-01-01'), null);
  assert.equal(pickRows(null, 'all'), null);
});

// ---- worstBuses / busStatus ----

const perBus = [
  { id: 'B10', capacity_kwh: 300, target_kwh: 270, real_final_kwh: 200, real_ready: false, planner_final_kwh: 270, planner_ready: true, no_swap_final_kwh: 250, no_swap_ready: false },
  { id: 'B02', capacity_kwh: 300, target_kwh: 255, real_final_kwh: 210, real_ready: false, planner_final_kwh: 255, planner_ready: true, no_swap_final_kwh: 255, no_swap_ready: true },
  { id: 'B03', capacity_kwh: 300, target_kwh: 270, real_final_kwh: 270, real_ready: true, planner_final_kwh: 270, planner_ready: true, no_swap_final_kwh: 270, no_swap_ready: true },
  { id: 'B04', capacity_kwh: 300, target_kwh: 270, real_final_kwh: null, real_ready: null, planner_final_kwh: 100, planner_ready: false, no_swap_final_kwh: 100, no_swap_ready: false },
  { id: 'B09', capacity_kwh: 300, target_kwh: 270, real_final_kwh: 100, real_ready: false, planner_final_kwh: 100, planner_ready: false, no_swap_final_kwh: 100, no_swap_ready: false },
];

test('worstBuses: only buses that were not ready in reality, sorted by id, with the other statuses', () => {
  const w = worstBuses({ per_bus: perBus });
  assert.deepEqual(w.map((b) => b.id), ['B02', 'B09', 'B10']);
  assert.deepEqual(w.map((b) => [b.planner, b.noSwap]), [['pronto', 'pronto'], ['não pronto', 'não pronto'], ['pronto', 'não pronto']]);
  assert.equal(w[0].realKWh, 210);
  assert.equal(w[0].targetKWh, 255);
});

test('worstBuses: a bus with unknown real outcome is never counted as not ready', () => {
  assert.equal(worstBuses({ per_bus: perBus }).some((b) => b.id === 'B04'), false);
  assert.deepEqual(worstBuses({ per_bus: [perBus[3]] }), []);
});

test('worstBuses: tolerates a night without buses', () => {
  assert.deepEqual(worstBuses({ per_bus: [] }), []);
  assert.deepEqual(worstBuses({ per_bus: null }), []);
  assert.deepEqual(worstBuses(null), []);
});

test('worstBuses sorts ids as text without side effects on the input', () => {
  const input = [{ ...perBus[0], id: 'B2' }, { ...perBus[1], id: 'B10' }, { ...perBus[1], id: 'A1' }];
  const copy = JSON.parse(JSON.stringify(input));
  assert.deepEqual(worstBuses({ per_bus: input }).map((b) => b.id), ['A1', 'B10', 'B2']);
  assert.deepEqual(input, copy);
});

test('busStatus: ready, not ready, and unknown for null', () => {
  assert.equal(busStatus(true), 'pronto');
  assert.equal(busStatus(false), 'não pronto');
  assert.equal(busStatus(null), '—');
  assert.equal(busStatus(undefined), '—');
});

// ---- summarizeImport ----

test('summarizeImport counts nights and buses and says which optional files came', () => {
  const s = summarizeImport({
    nights: [{ key: 'a', buses: 3, with_outcome: 3, ready: 2, real_ready_pct: 66.666, has_outcome: true }, { key: 'b', buses: 2, with_outcome: 0, ready: 0, real_ready_pct: 0, has_outcome: false }],
    warnings: [], has_sessions: true, has_power: false, has_tariff: true,
  });
  assert.equal(s.nights, 2);
  assert.equal(s.buses, 5);
  assert.deepEqual(s.extras, [
    { key: 'sessions', label: 'Sessões de carga', present: true },
    { key: 'power', label: 'Leituras de potência', present: false },
    { key: 'tariff', label: 'Tarifa (ponta e fora da ponta)', present: true },
  ]);
  assert.deepEqual(s.nightRows.map((r) => r.ready), ['66,7%', '—']);
});

test('summarizeImport tolerates an empty answer', () => {
  const s = summarizeImport({});
  assert.equal(s.nights, 0);
  assert.equal(s.buses, 0);
  assert.deepEqual(s.nightRows, []);
});

// ---- run request / controller choices ----

test('runRequestBody: a generated run sends the form parameters', () => {
  const body = runRequestBody({ params: { buses: 5 }, seed: 3, controller: 'fifo' });
  assert.deepEqual(body, { buses: 5, seed: 3, controller: 'fifo' });
});

test('runRequestBody: an imported night re-sends the files and the night, nothing else', () => {
  const files = { 'onibus.csv': 'x' };
  const body = runRequestBody({ params: { buses: 5 }, seed: 3, controller: NO_SWAP, real: { files, night: '2026-03-04' } });
  assert.deepEqual(body, { files, night: '2026-03-04', controller: NO_SWAP });
});

test('controllerOptions: the no-swap planner only for imported nights', () => {
  assert.deepEqual(controllerOptions(false), ['fifo', 'edf', 'fifo-unplug', 'safe', 'planner']);
  assert.deepEqual(controllerOptions(true), ['fifo', 'edf', 'fifo-unplug', 'safe', 'planner', 'planner (sem rodízio)']);
  assert.equal(NO_SWAP, 'planner (sem rodízio)');
});

test('controllerHelp explains every row, including the real one and the no-swap planner', () => {
  for (const n of ['real', NO_SWAP, ...controllerOptions(false)]) assert.ok(controllerHelp(n).length > 10, n);
  assert.equal(controllerHelp('inexistente'), '');
});

// ---- compareButtonState ----

test('compareButtonState: label and disabled derive from one state, for every combination', () => {
  const cases = [
    // imported, importing, comparing -> label, disabled
    [false, false, false, 'Comparar', true],   // nothing good imported yet
    [true, false, false, 'Comparar', false],   // good import, idle
    [true, false, true, 'Comparando\u2026', true], // comparing
    [false, true, false, 'Comparar', true],    // importing (the previous import was dropped)
    [true, true, false, 'Comparar', true],     // importing while a compare was aborted: label back to Comparar, still disabled
    [false, false, true, 'Comparando\u2026', true],
    [true, true, true, 'Comparando\u2026', true],
    [false, true, true, 'Comparando\u2026', true],
  ];
  for (const [imported, importing, comparing, label, disabled] of cases) {
    assert.deepEqual(compareButtonState({ imported, importing, comparing }), { label, disabled }, JSON.stringify({ imported, importing, comparing }));
  }
});

test('compareButtonState: a missing state is the idle, nothing-imported button', () => {
  assert.deepEqual(compareButtonState({}), { label: 'Comparar', disabled: true });
});

// ---- checkSizes (per file, raw bytes) ----

const MB = 1024 * 1024;

test('checkSizes: the per-file limit is 8 MB of raw bytes', () => {
  assert.equal(MAX_FILE_BYTES, 8 * MB);
  assert.deepEqual(checkSizes({ 'onibus.csv': 8 * MB, 'sessoes.csv': 6 * MB }), { drop: [], message: '' });
  assert.deepEqual(checkSizes({}), { drop: [], message: '' });
});

test('checkSizes: a file over 8 MB is dropped by name, with the limit in the message', () => {
  const r = checkSizes({ 'onibus.csv': 9 * MB, 'sessoes.csv': 1 * MB });
  assert.deepEqual(r.drop, ['onibus.csv']);
  assert.match(r.message, /onibus\.csv/);
  assert.match(r.message, /limite é 8 MB/);
  assert.deepEqual(checkSizes({ 'a.csv': 8 * MB + 1 }).drop, ['a.csv']);
});

// ---- bodySize / checkBody: the real JSON body, not the raw bytes ----

const utf8 = (v) => Buffer.byteLength(JSON.stringify(v));

test('bodySize is the UTF-8 size of the JSON the server receives, plus a margin for the keys', () => {
  const files = { 'garagem.csv': 'limite_kw\n400\n', 'onibus.csv': 'a,b\n1,2\n' };
  const real = utf8({ files, soc_noise_kwh: 0, swap_back_cooldown_min: 30, swap_back_min_need_kwh: 10 });
  const size = bodySize(files);
  assert.ok(size >= real, `${size} < ${real}`);
  assert.ok(size - real < 1024, 'the margin stays small');
  assert.equal(bodySize({}), bodySize({}));
});

test('bodySize: an all-quoted CRLF file is about 1.6 times its raw size', () => {
  const text = '"a","b","c"\r\n'.repeat(100000); // 1.3 MB raw, a quote becomes \" and CRLF becomes \r\n as text
  const raw = Buffer.byteLength(text);
  const size = bodySize({ 'onibus.csv': text });
  assert.ok(size > raw * 1.5, `${size} vs ${raw}`);
  assert.ok(size >= utf8({ files: { 'onibus.csv': text } }));
});

test('bodySize: Windows-1252 accents count as two UTF-8 bytes once decoded', () => {
  const bytes = new Uint8Array(100000).fill(0xe7); // 100 000 raw bytes: ç
  const text = readFileText(bytes.buffer);
  assert.equal(text.length, 100000);
  const size = bodySize({ 'onibus.csv': text });
  assert.ok(size > 200000, `${size}`);
  assert.ok(size < 200000 + 1024);
});

test('bodySize: control characters and a stray quote grow as the JSON writes them', () => {
  assert.ok(bodySize({ a: '\u0001'.repeat(1000) }) > 6000);
  assert.ok(bodySize({ a: '\\'.repeat(1000) }) > 2000);
});

test('checkBody: under the limit is accepted; a replacement counts instead of the file it replaces', () => {
  const big = (n) => 'x'.repeat(n);
  assert.deepEqual(checkBody({ 'garagem.csv': 'a' }, { 'onibus.csv': big(1000) }).refuse, false);
  const held = { 'onibus.csv': big(8 * MB), 'sessoes.csv': big(7 * MB) };
  assert.equal(checkBody(held, { 'sessoes.csv': big(1 * MB) }).refuse, false);
  assert.equal(checkBody(held, { 'potencia.csv': big(1 * MB) }).refuse, true); // 16 MB raw
});

test('checkBody: all-quoted files that are small raw but big once escaped are refused with the escaped size', () => {
  const quoted = '"'.repeat(5 * MB); // 5 MB raw, 10 MB in the body
  const r = checkBody({ 'onibus.csv': quoted }, { 'sessoes.csv': quoted });
  assert.equal(r.refuse, true);
  assert.ok(r.size > MAX_BODY_BYTES);
  assert.match(r.message, /ficaria com/);
  assert.match(r.message, /limite é 15,5 MB/);
  assert.match(r.message, /aspas/);
  assert.equal(checkBody({ 'onibus.csv': quoted }, {}).refuse, false);
});

test('MAX_BODY_BYTES leaves a margin under the 16 MB the server accepts', () => {
  assert.equal(MAX_BODY_BYTES, 15.5 * MB);
  assert.ok(MAX_BODY_BYTES < 16 * MB);
});

// ---- describeFailure ----

test('describeFailure: a 413 says the send passed the server limit and is not retried', () => {
  const f = describeFailure(new ApiError('Corpo grande demais', '', 413), 1000);
  assert.match(f.message, /limite do servidor \(16 MB\)/);
  assert.equal(f.retry, false);
});

test('describeFailure: a network error after sending a large body is not retried and mentions the limit', () => {
  const f = describeFailure(new ApiError('Não consegui falar com o servidor. Ele ainda está rodando?', '', 0), 15 * MB);
  assert.match(f.message, /16 MB/);
  assert.equal(f.retry, false);
});

test('describeFailure: a network error with a small body, busy, timeout and cancelled can be retried', () => {
  for (const status of [0, 408, 503, 504]) {
    const f = describeFailure(new ApiError('msg', '', status), 1000);
    assert.equal(f.retry, true, String(status));
    assert.equal(f.message, 'msg');
  }
});

test('describeFailure: other errors and non-ApiErrors are not retried', () => {
  assert.equal(describeFailure(new ApiError('feio', '', 400), 0).retry, false);
  assert.equal(describeFailure(new ApiError('boom', '', 500), 0).retry, false);
  const g = describeFailure(new Error('x'), 0);
  assert.equal(g.message, 'Erro inesperado: x');
  assert.equal(g.retry, false);
});

// ---- fmtSize ----

test('fmtSize writes bytes, KB and MB, rounding up so a size over a limit never prints like the limit', () => {
  assert.equal(fmtSize(88), '88 B');
  assert.equal(fmtSize(2048), '2 KB');
  assert.equal(fmtSize(16 * MB), '16 MB');
  assert.equal(fmtSize(1.5 * MB), '1,5 MB');
  assert.equal(fmtSize(8 * MB + 1), '8,1 MB');
  assert.equal(fmtSize(MAX_BODY_BYTES), '15,5 MB');
  assert.notEqual(fmtSize(MAX_BODY_BYTES + 1), fmtSize(MAX_BODY_BYTES));
});

// ---- pickOption ----

test('pickOption keeps a choice that is still offered and falls back otherwise', () => {
  const opts = controllerOptions(true);
  assert.equal(pickOption(opts, 'fifo', 'planner'), 'fifo');
  assert.equal(pickOption(opts, NO_SWAP, 'planner'), NO_SWAP);
  assert.equal(pickOption(opts, 'nope', 'planner'), 'planner');
  assert.equal(pickOption(opts, undefined, 'planner'), 'planner');
});

// ---- status texts ----

test('importStatusText: a refusal note is part of the announced sentence', () => {
  assert.equal(importStatusText({ nights: 2, buses: 6 }, 'sessoes.csv foi recusado.'),
    'Planilhas conferidas: 2 noites, 6 ônibus. Agora clique em Comparar. sessoes.csv foi recusado.');
  assert.equal(importStatusText({ nights: 2, buses: 6 }, ''), importStatusText({ nights: 2, buses: 6 }));
});

test('errorStatusText: with a note it is appended', () => {
  assert.equal(errorStatusText(new ApiError('x', '', 0), 'Nota.'), 'A importação falhou: x Nota.');
});

test('importStatusText: one sentence per outcome of a good import', () => {
  assert.equal(importStatusText({ nights: 2, buses: 6 }), 'Planilhas conferidas: 2 noites, 6 ônibus. Agora clique em Comparar.');
  assert.equal(importStatusText({ nights: 1, buses: 1 }), 'Planilhas conferidas: 1 noite, 1 ônibus. Agora clique em Comparar.');
  assert.match(importStatusText({ nights: 0, buses: 0 }), /nenhuma noite utilizável/);
});

test('errorStatusText: the failure says what failed once, with the location when there is one', () => {
  const e = new ApiError('onibus.csv, linha 3, coluna x: ruim', 'onibus.csv', 400, { file: 'onibus.csv', line: 3, column: 'x' });
  assert.equal(errorStatusText(e), 'A importação falhou: onibus.csv, linha 3, coluna x: ruim');
  assert.equal(errorStatusText(new ApiError('Não consegui falar com o servidor.', '', 0)), 'A importação falhou: Não consegui falar com o servidor.');
});

test('failureStatusText: the failure and the refusal note in one announced sentence', () => {
  assert.equal(failureStatusText('Sem rede.'), 'A importação falhou: Sem rede.');
  assert.equal(failureStatusText('Sem rede.', 'b.csv recusado.'), 'A importação falhou: Sem rede. b.csv recusado.');
});
