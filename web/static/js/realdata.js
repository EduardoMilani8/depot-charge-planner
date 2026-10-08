// Pure helpers of the "Dados reais" tab: reading the spreadsheets, the rows of the comparison
// table and the per-bus list. Nothing here touches the DOM or the network.
import { COLUMNS, CONTROLLER_HELP, formatCell } from './glossary.js';
import { fmtNum, fmtPct } from './format.js';
import { CONTROLLERS } from './params.js';

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

// readFileText decodes the bytes of a spreadsheet: UTF-8 (a byte order mark is dropped) and, when
// the bytes are not valid UTF-8, Windows-1252, which is what Excel writes in Brazil.
export function readFileText(buf) {
  const bytes = buf instanceof Uint8Array ? buf : new Uint8Array(buf);
  try {
    return new TextDecoder('utf-8', { fatal: true }).decode(bytes);
  } catch {
    return new TextDecoder('windows-1252').decode(bytes);
  }
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

const cell = (key, text, extra = {}) => ({ key, text, estimated: false, detail: '', title: '', bad: false, ...extra });

// realRowCells: the cells of the `real` row, in the order of columns(). A figure the spreadsheets
// cannot give is a dash; an estimated one carries `estimated: true`, which the table draws as a
// visible "estimado" seal (never by colour alone). Plan violations, minutes over the limit, plan
// changes and operator moves are not measured by a depot's spreadsheets: always a dash.
export function realRowCells(real) {
  const r = real || {};
  const measured = r.with_outcome > 0;
  const num = (v, col) => (typeof v === 'number' && Number.isFinite(v) ? col.fmt(v) : DASH);
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
        return cell(col.key, text, { estimated, title: estimated ? SEAL_PEAK : '' });
      }
      case 'energy_kwh':
        return cell(col.key, num(r.energy_kwh, col));
      case 'cost_brl': {
        const text = num(r.cost_brl, col);
        const estimated = text !== DASH && Boolean(r.cost_estimated);
        return cell(col.key, text, { estimated, title: estimated ? SEAL_COST : '' });
      }
      default:
        return cell(col.key, DASH);
    }
  });
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
  if (key === 'all') return { real: report.real_aggregate, controllers: report.aggregate, night: null };
  const night = (report.nights || []).find((n) => n.key === key);
  return night ? { real: night.real, controllers: night.controllers, night } : null;
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

export const MAX_FILE_BYTES = 8 * 1024 * 1024; // per file (the server refuses more)
// Every request carries all the files as JSON text, and the server's body limit is 16 MB: stay under it
// with a margin for the JSON quoting and the other fields.
export const MAX_TOTAL_BYTES = 15 * 1024 * 1024;

export function fmtSize(bytes) {
  if (bytes < 1024) return `${bytes} B`;
  if (bytes < 1024 * 1024) return `${fmtNum(bytes / 1024, 0)} KB`;
  const mb = bytes / (1024 * 1024);
  return `${fmtNum(mb, Number.isInteger(mb) ? 0 : 1)} MB`;
}

// checkSizes decides which of the files just chosen (`incoming`: name -> bytes) cannot be kept next
// to the ones already held (`held`: name -> bytes): a file over the per-file limit is dropped; if the
// total (held files, each replaced by its new version) is then over the limit, the whole new choice is
// dropped and the held files stay as they were. Returns the names to drop and a Portuguese message.
export function checkSizes(held, incoming) {
  const drop = [];
  const msgs = [];
  for (const [name, size] of Object.entries(incoming)) {
    if (size > MAX_FILE_BYTES) {
      drop.push(name);
      msgs.push(`${name} tem ${fmtSize(size)} e o limite é ${fmtSize(MAX_FILE_BYTES)} por arquivo.`);
    }
  }
  const kept = { ...held };
  for (const [name, size] of Object.entries(incoming)) if (!drop.includes(name)) kept[name] = size;
  const total = Object.values(kept).reduce((n, v) => n + v, 0);
  if (total > MAX_TOTAL_BYTES) {
    for (const name of Object.keys(incoming)) if (!drop.includes(name)) drop.push(name);
    msgs.push(`Os arquivos somariam ${fmtSize(total)} e o limite é ${fmtSize(MAX_TOTAL_BYTES)} por envio. Exporte menos noites e escolha de novo.`);
  }
  return { drop, message: msgs.join(' ') };
}

// pickOption keeps `value` if it is one of `options`, else `fallback` (a choice that survives a redraw).
export const pickOption = (options, value, fallback) => (options.includes(value) ? value : fallback);

// ---- status sentences (the page's one polite announcement per phase) ----

export function importStatusText({ nights, buses }) {
  if (nights === 0) return 'As planilhas não têm nenhuma noite utilizável: veja os avisos.';
  return `Planilhas conferidas: ${nights} ${nights === 1 ? 'noite' : 'noites'}, ${buses} ${buses === 1 ? 'ônibus' : 'ônibus'}. Agora clique em Comparar.`;
}

export const errorStatusText = (err) => `A importação falhou: ${formatImportError(err).text}`;
