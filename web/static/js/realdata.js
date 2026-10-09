// Pure helpers of the "Dados reais" tab: reading the spreadsheets, the rows of the comparison
// table and the per-bus list. Nothing here touches the DOM or the network.
import { COLUMNS, CONTROLLER_HELP, formatCell } from './glossary.js';
import { fmtNum, fmtPct } from './format.js';
import { CONTROLLERS } from './params.js';
import { ApiError } from './api.js';

// The planner run as if no operator carried out any swap (a row of the report, a controller of /api/run).
export const NO_SWAP = 'planner (sem rodízio)';

export const FILE_SPECS = [
  { name: 'garagem.csv', required: true, what: 'limite de potência da rede e, se houver, a tarifa' },
  { name: 'carregadores.csv', required: true, what: 'um carregador por linha, com a potência máxima' },
  { name: 'onibus.csv', required: true, what: 'um ônibus por noite: chegada, carga, saída prevista e carga exigida' },
  { name: 'sessoes.csv', required: false, what: 'sessões de carga medidas (dão a energia e um pico estimado)' },
  { name: 'potencia.csv', required: false, what: 'leituras de potência (dão o pico e o custo medidos)' },
];

const KNOWN = new Set(FILE_SPECS.map((f) => f.name));
const DASH = '—';

// The server's own message for a UTF-16 file (internal/realdata/table.go): the browser would decode such
// a file as Windows-1252 (every other byte is NUL), the server would never see it and the error would be nonsense.
export const UTF16_MESSAGE = 'arquivo em UTF-16 não é suportado: salve como CSV UTF-8';

export class UnsupportedEncodingError extends Error {
  constructor(message = UTF16_MESSAGE) {
    super(message);
    this.name = 'UnsupportedEncodingError';
  }
}

// looksUtf16: a UTF-16 byte order mark (FF FE or FE FF), or a lot of NUL bytes at the start (UTF-16
// text of Latin letters is a NUL every other byte; a spreadsheet in any 8-bit encoding has none).
export function looksUtf16(bytes) {
  if (bytes.length >= 2 && ((bytes[0] === 0xff && bytes[1] === 0xfe) || (bytes[0] === 0xfe && bytes[1] === 0xff))) return true;
  const n = Math.min(bytes.length, 4096);
  let nuls = 0;
  for (let i = 0; i < n; i++) if (bytes[i] === 0) nuls++;
  return n >= 4 && nuls * 5 >= n; // at least a fifth of the first 4 KB
}

// readFileText decodes the bytes of a spreadsheet: UTF-8 (a byte order mark is dropped) and, when
// the bytes are not valid UTF-8, Windows-1252, which is what Excel writes in Brazil. UTF-16 is refused
// (UnsupportedEncodingError) like the command line does.
export function readFileText(buf) {
  const bytes = buf instanceof Uint8Array ? buf : new Uint8Array(buf);
  if (looksUtf16(bytes)) throw new UnsupportedEncodingError();
  try {
    return new TextDecoder('utf-8', { fatal: true }).decode(bytes);
  } catch {
    return new TextDecoder('windows-1252').decode(bytes);
  }
}

// readFailureText: why a file could not be read, for the list of what was left out of a choice.
export function readFailureText(name, err) {
  if (err instanceof UnsupportedEncodingError) return `${name}: ${err.message}.`;
  return `Não consegui ler ${name}. Se o arquivo mudou ou foi movido, escolha-o de novo.`;
}

const baseName = (name) => String(name).split(/[\\/]/).pop();

// pickKnownFiles keeps the files whose base name is one of the five of the kit (any letter case;
// the files of a dropped or chosen folder count by their base name). known maps the canonical name
// to the File; ignored lists what was left out, in order. Of two files with the same known name
// the first wins and the other is reported as repeated.
export function pickKnownFiles(fileList) {
  const known = {};
  const ignored = [];
  for (const f of Array.from(fileList || [])) {
    const base = baseName(f.name);
    const key = base.toLowerCase();
    if (!KNOWN.has(key)) {
      ignored.push(base);
    } else if (known[key]) {
      ignored.push(`${f.webkitRelativePath || f.name} (repetido)`);
    } else {
      known[key] = f;
    }
  }
  return { known, ignored };
}

// formatImportError splits a server error into the parts of the error list (file, line, column,
// plain message) and the one-line text "arquivo, linha N, coluna X: mensagem". The server's
// `error` already starts with that location, so the plain message is what follows it.
export function formatImportError(err) {
  const raw = err && typeof err.message === 'string' && err.message !== '' ? err.message : 'Erro desconhecido.';
  const file = (err && err.file) || '';
  const line = (err && err.line) || 0;
  const column = (err && err.column) || '';
  const where = [];
  if (file) where.push(file);
  if (line > 0) where.push(`linha ${line}`);
  if (column) where.push(`coluna ${column}`);
  if (where.length === 0) return { file: '', line: 0, column: '', message: raw, text: raw };
  const prefix = where.join(', ') + ': ';
  const message = raw.startsWith(prefix) ? raw.slice(prefix.length) : raw;
  return { file, line, column, message, text: prefix + message };
}

// columns: the laboratory's columns of the comparison, without the planner's timing (p99), which
// the lab's server zeroes for imported data.
export const columns = () => COLUMNS.filter((c) => c.key !== 'plan_p99_micros');

const SEAL_PEAK = 'Estimado: não há leituras de potência nesta noite; o pico vem das sessões de carga, tratadas como potência constante do início ao fim. Costuma subestimar o pico real. Com potencia.csv ele é medido.';
const SEAL_COST = 'Estimado: o custo vem das sessões de carga espalhadas uniformemente entre o início e o fim de cada uma. Com potencia.csv ele é medido.';
const NO_COST = 'Sem custo simulado comparável: faltam as colunas de tarifa em garagem.csv ou a janela de ponta atravessa a meia-noite, que o simulador não representa.';

export const SEAL_PARTIAL = 'Parcial: a energia, o pico e o custo reais vêm de sessões ou leituras que não cobrem todos os ônibus (ou que divergem entre si). O valor é medido, mas é de parte da noite: não o compare, sem ressalva, com as linhas simuladas, que cobrem todos os ônibus.';

const cell = (key, text, extra = {}) => ({ key, text, estimated: false, partial: '', detail: '', title: '', bad: false, ...extra });

// realRowCells: the cells of the `real` row, in the order of columns(). A figure the spreadsheets
// cannot give is a dash; an estimated one carries `estimated: true`, which the table draws as a
// visible "estimado" seal (never by colour alone); one whose sessions or readings cover only part of
// the night carries `partial` ("parcial: 2 de 3 ônibus"), drawn as a seal too. Plan violations, minutes over the limit, plan
// changes and operator moves are not measured by a depot's spreadsheets: always a dash.
export function realRowCells(real) {
  const r = real || {};
  const measured = r.with_outcome > 0;
  const num = (v, col) => (typeof v === 'number' && Number.isFinite(v) ? col.fmt(v) : DASH);
  // the partial seal goes with a figure that exists, never with a dash
  const partial = (text) => (text !== DASH && r.partial ? String(r.partial_note || 'parcial') : '');
  const by = Object.fromEntries(columns().map((c) => [c.key, c]));
  return columns().map((col) => {
    switch (col.key) {
      case 'ready_pct':
        return cell(col.key, measured ? fmtPct(r.ready_pct) : DASH, {
          detail: measured && r.with_outcome < r.buses ? `${r.with_outcome} de ${r.buses} ônibus com saída medida` : '',
        });
      case 'shortfall_kwh':
        return cell(col.key, measured ? num(r.shortfall_kwh, by.shortfall_kwh) : DASH);
      case 'peak_kw': {
        const text = num(r.peak_kw, col);
        const estimated = text !== DASH && Boolean(r.peak_estimated);
        return cell(col.key, text, { estimated, partial: partial(text), title: estimated ? SEAL_PEAK : '' });
      }
      case 'energy_kwh': {
        const text = num(r.energy_kwh, col);
        return cell(col.key, text, { partial: partial(text) });
      }
      case 'cost_brl': {
        const text = num(r.cost_brl, col);
        const estimated = text !== DASH && Boolean(r.cost_estimated);
        return cell(col.key, text, { estimated, partial: partial(text), title: estimated ? SEAL_COST : '' });
      }
      default:
        return cell(col.key, DASH);
    }
  });
}

// partialText: the sentence under the table when the real energy, peak and cost cover only part of
// the night(s) ("" when they cover everything).
export function partialText(real) {
  if (!real || !real.partial) return '';
  return `Energia, pico e custo reais são parciais (${String(real.partial_note || 'parcial').replace(/^parcial:\s*/, 'parcial: ')}): vêm de sessões ou leituras que não cobrem todos os ônibus, então cobrem só parte da noite e não são comparáveis com as linhas simuladas, que cobrem todos os ônibus. O valor está na tabela com o selo “parcial”.`;
}

// controllerCells: the cells of a simulated row. Cost is a dash when the report says the simulated
// cost cannot be compared with the real one.
export function controllerCells(controller, costComparable) {
  const m = controller.aggregate || {};
  return columns().map((col) => {
    if (col.key === 'cost_brl' && !costComparable) return cell(col.key, DASH, { title: NO_COST });
    return cell(col.key, formatCell(col, m), { bad: Boolean(col.mustBeZero && m[col.key] > 0) });
  });
}

// pickRows chooses what the table shows: 'all' is the mean over the nights, otherwise one night.
// null when the report has no such night.
export function pickRows(report, key) {
  if (!report) return null;
  if (key === 'all') return { real: report.real_aggregate, controllers: report.aggregate, matched: report.matched_aggregate, night: null };
  const night = (report.nights || []).find((n) => n.key === key);
  return night ? { real: night.real, controllers: night.controllers, matched: night.matched, night } : null;
}

// aggregateNote: what the "all nights" rows are. Most columns are means per night, but sim.Aggregate
// sums the violations and the minutes over the limit and cuts the plan changes to a whole number.
export const AGGREGATE_NOTE = 'Nas linhas de todas as noites, os valores são médias por noite, exceto violações do plano e minutos acima do limite, que são somas das noites, e mudanças de plano, que é a média arredondada para baixo.';

// fairText: the comparison over the same buses. The simulated rows cover every bus, the real ready%
// only the buses with a measured departure; this line puts the planner on those same buses. "" when
// there is no bus with a measured outcome (nothing to compare).
export function fairText(real, matched) {
  if (!matched || !(matched.buses > 0) || !real) return '';
  const pct = (v) => fmtPct(v);
  const kwh = (v) => fmtNum(v, 1);
  return `Comparação justa (mesmos ${matched.buses} ônibus do real): real ${pct(real.ready_pct)} | planner ${pct(matched.planner_ready_pct)} | sem rodízio ${pct(matched.no_swap_ready_pct)} `
    + `(déficit em kWh: real ${kwh(real.shortfall_kwh)} | planner ${kwh(matched.planner_shortfall_kwh)} | sem rodízio ${kwh(matched.no_swap_shortfall_kwh)})`;
}

// A bus that left more than this many minutes after the planned time is a late departure (the server's
// realdata.LateDepartureTolerance).
export const LATE_MINUTES = 15;

// lateText: what late departures mean for the "pronto" of the real row. "" when there are none.
export function lateText(n) {
  if (!(n > 0)) return '';
  if (n === 1) return `1 ônibus saiu mais de ${LATE_MINUTES} min depois do previsto: conta como pronto se a carga estava completa na saída.`;
  return `${n} ônibus saíram mais de ${LATE_MINUTES} min depois do previsto: contam como prontos se a carga estava completa na saída.`;
}

// busStatus: "pronto", "não pronto" or a dash when the outcome is unknown.
export const busStatus = (ready) => (ready === true ? 'pronto' : ready === false ? 'não pronto' : DASH);

// worstBuses: the buses that were NOT ready in reality, by ID, with what the planner and the planner
// without swaps would have done. A bus whose real outcome is unknown (null) never counts.
export function worstBuses(night) {
  const rows = (night && night.per_bus) || [];
  return rows
    .filter((b) => b.real_ready === false)
    .map((b) => ({
      id: b.id, targetKWh: b.target_kwh, realKWh: b.real_final_kwh,
      plannerKWh: b.planner_final_kwh, noSwapKWh: b.no_swap_final_kwh,
      planner: busStatus(b.planner_ready), noSwap: busStatus(b.no_swap_ready),
    }))
    .sort((a, b) => (a.id < b.id ? -1 : a.id > b.id ? 1 : 0));
}

// busTableRows: the rows of the per-bus table of a night, with the kWh at one decimal like the
// command line. Only the buses that were not ready in reality, or all of them with `showAll`.
export function busTableRows(night, showAll = false) {
  const kwh = (v) => (typeof v === 'number' && Number.isFinite(v) ? fmtNum(v, 1) : null);
  return ((night && night.per_bus) || [])
    .filter((b) => showAll || b.real_ready === false)
    .map((b) => ({
      id: b.id, target: fmtNum(b.target_kwh, 1),
      real: { ready: b.real_ready ?? null, kwh: kwh(b.real_final_kwh) },
      planner: { ready: b.planner_ready ?? null, kwh: kwh(b.planner_final_kwh) },
      noSwap: { ready: b.no_swap_ready ?? null, kwh: kwh(b.no_swap_final_kwh) },
    }));
}

// createSequence numbers the runs of an async action, so one that was superseded (or cleared) can tell
// after each await that it must stop: `const my = seq.next(); await …; if (!seq.current(my)) return;`.
export function createSequence() {
  let n = 0;
  return { next: () => ++n, bump: () => { n++; }, current: (mine) => mine === n };
}

// dropsRun: does clearing the imported data also remove the run on screen? The detailed run of an
// imported night re-sends the files, so it is removed with them.
export const dropsRun = (runSel) => Boolean(runSel && runSel.real);

// summarizeImport: what the import answer says, ready to draw.
export function summarizeImport(res) {
  const nights = (res && res.nights) || [];
  return {
    nights: nights.length,
    buses: nights.reduce((n, x) => n + (x.buses || 0), 0),
    extras: [
      { key: 'sessions', label: 'Sessões de carga', present: Boolean(res && res.has_sessions) },
      { key: 'power', label: 'Leituras de potência', present: Boolean(res && res.has_power) },
      { key: 'tariff', label: 'Tarifa (ponta e fora da ponta)', present: Boolean(res && res.has_tariff) },
    ],
    nightRows: nights.map((n) => ({
      key: n.key, buses: n.buses, withOutcome: n.with_outcome,
      ready: n.has_outcome ? fmtPct(n.real_ready_pct) : DASH,
    })),
  };
}

// runRequestBody: the /api/run body of the run on screen. An imported night sends the files again
// (the server keeps nothing); a generated one sends the form's parameters.
export function runRequestBody(state) {
  if (state.real) return { files: state.real.files, night: state.real.night, controller: state.controller };
  return { ...state.params, seed: state.seed, controller: state.controller };
}

// controllerOptions: what the run's controller picker offers.
export const controllerOptions = (imported) => (imported ? [...CONTROLLERS, NO_SWAP] : [...CONTROLLERS]);

const HELP_NO_SWAP = 'O planejador supondo que os operadores não executam nenhum rodízio: só o que ele consegue sem mover ônibus.';
const HELP_REAL = 'O que aconteceu na garagem, como as planilhas registram.';

// controllerHelp: the explanation of a row of the table or of an option of the picker.
export function controllerHelp(name) {
  if (name === 'real') return HELP_REAL;
  if (name === NO_SWAP) return HELP_NO_SWAP;
  return CONTROLLER_HELP[name] || '';
}

// ---- the compare button, from one state ----

// compareButtonState: the label and `disabled` of the Comparar button, derived in one place from what
// the tab is doing: `imported` (a good import with nights is held), `importing` (reading or sending
// the files) and `comparing`. A new import aborts a comparison in flight, so the label goes back to
// "Comparar" the moment only the import is running.
export function compareButtonState({ imported = false, importing = false, comparing = false } = {}) {
  return { label: comparing ? 'Comparando\u2026' : 'Comparar', disabled: !imported || importing || comparing };
}

// ---- sizes the server accepts ----

export const MAX_FILE_BYTES = 8 * 1024 * 1024; // per file, raw bytes (the server refuses more)
const SERVER_BODY_BYTES = 16 * 1024 * 1024; // what /api/import, /api/replay and /api/run accept
// What the client allows itself to send: the server limit minus a margin.
export const MAX_BODY_BYTES = 15.5 * 1024 * 1024;
// A network error after a body this big is more likely the server cutting the request than a lost connection.
const LARGE_BODY_BYTES = 14 * 1024 * 1024;
const BODY_OVERHEAD = 512; // the other keys of the request ("files", the noise and swap-back parameters)

// fmtSize rounds up (to 0.1 MB), so a size over a limit never prints the same as the limit.
export function fmtSize(bytes) {
  if (bytes < 1024) return `${bytes} B`;
  if (bytes < 1024 * 1024) return `${fmtNum(Math.ceil(bytes / 1024), 0)} KB`;
  const mb = Math.ceil((bytes / (1024 * 1024)) * 10) / 10;
  return `${fmtNum(mb, Number.isInteger(mb) ? 0 : 1)} MB`;
}

// checkSizes drops the files just chosen (`incoming`: name -> raw bytes) that are over the per-file limit.
export function checkSizes(incoming) {
  const drop = [];
  const msgs = [];
  for (const [name, size] of Object.entries(incoming)) {
    if (size > MAX_FILE_BYTES) {
      drop.push(name);
      msgs.push(`${name} tem ${fmtSize(size)} e o limite é ${fmtSize(MAX_FILE_BYTES)} por arquivo.`);
    }
  }
  return { drop, message: msgs.join(' ') };
}

const encoder = new TextEncoder();

// bodySize is the size in bytes of the request carrying `files` (name -> decoded text): the text goes
// as a JSON string, so every quote takes two bytes, CRLF four, a control character six, and an accent
// two (UTF-8), however small the file was on disk. A margin covers the other keys.
export function bodySize(files) {
  let n = BODY_OVERHEAD;
  for (const [name, text] of Object.entries(files)) {
    n += encoder.encode(JSON.stringify(name)).length + encoder.encode(JSON.stringify(text)).length + 2;
  }
  return n;
}

// checkBody: would the files held (name -> text), each replaced by its new version in `incoming`, fit in one request?
export function checkBody(held, incoming) {
  const size = bodySize({ ...held, ...incoming });
  if (size <= MAX_BODY_BYTES) return { refuse: false, size, message: '' };
  return {
    refuse: true, size,
    message: `O envio ficaria com ${fmtSize(size)} (aspas, quebras de linha e acentos ocupam mais no envio do que no arquivo) e o limite é ${fmtSize(MAX_BODY_BYTES)}: o servidor aceita 16 MB, com uma folga. Exporte menos noites e escolha de novo.`,
  };
}

// describeFailure: the message to show for a failed request and whether trying again can help. A 413
// (and a network error right after a very large body) is the body passing the server's limit: never retried.
export function describeFailure(err, bodyBytes = 0) {
  const tooBig = 'O envio passou do limite do servidor (16 MB). Exporte menos noites e escolha os arquivos de novo.';
  if (!(err instanceof ApiError)) return { message: 'Erro inesperado: ' + (err && err.message), retry: false };
  if (err.status === 413) return { message: tooBig, retry: false };
  if (err.status === 0 && bodyBytes > LARGE_BODY_BYTES) {
    return { message: `${err.message} Se ele está rodando, o envio (${fmtSize(bodyBytes)}) pode ter passado do limite do servidor (16 MB): exporte menos noites.`, retry: false };
  }
  return { message: err.message, retry: [0, 408, 503, 504].includes(err.status) };
}

// pickOption keeps `value` if it is one of `options`, else `fallback` (a choice that survives a redraw).
export const pickOption = (options, value, fallback) => (options.includes(value) ? value : fallback);

// ---- status sentences (the page's one polite announcement per phase) ----

// A note (what was refused in the choice) goes in the same sentence, so the live region announces it.
const withNote = (text, note) => (note ? `${text} ${note}` : text);

export function importStatusText({ nights, buses }, note = '') {
  if (nights === 0) return withNote('As planilhas não têm nenhuma noite utilizável: veja os avisos.', note);
  return withNote(`Planilhas conferidas: ${nights} ${nights === 1 ? 'noite' : 'noites'}, ${buses} ônibus. Agora clique em Comparar.`, note);
}

export const failureStatusText = (message, note = '') => withNote(`A importação falhou: ${message}`, note);
export const errorStatusText = (err, note = '') => failureStatusText(formatImportError(err).text, note);
