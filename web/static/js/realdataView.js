// The "Dados reais" tab: choose the depot's spreadsheets, check them, compare what happened with what
// the planner and the references would have done. The files are read by the browser and live only in
// this page's memory; every request sends their text again to this local server, which keeps nothing.
import { importFiles, replay, createLatest, ApiError } from './api.js';
import { h } from './dom.js';
import { fmtNum } from './format.js';
import {
  FILE_SPECS, readFileText, pickKnownFiles, formatImportError, columns, realRowCells, controllerCells,
  pickRows, worstBuses, busStatus, summarizeImport, controllerOptions, controllerHelp,
  compareButtonState, SEAL_PARTIAL, partialText, checkSizes, checkBody, bodySize, describeFailure, pickOption, importStatusText, errorStatusText, failureStatusText, fmtSize,
} from './realdata.js';

const MAX_DROPPED = 1000; // files read from a dropped folder, so a huge folder cannot hang the page
const MAX_LISTED = 20; // warnings shown before the rest goes into a "more" block
const BANNER = 'Dados reais: o resultado vale para esta garagem e estes dias. As premissas abaixo continuam sendo suposição.';
const EMPTY_STATUS = 'Nenhuma planilha escolhida. Escolha os arquivos ou a pasta, ou arraste-os para a área acima.';
const EMPTY_OUT = 'Importe as planilhas e clique em Comparar para ver aqui o que aconteceu na garagem ao lado do que o planejador teria feito.';

// put replaces the children of node; null, false and nested arrays are fine (replaceChildren alone would print them).
const put = (node, ...children) => node.replaceChildren(...children.flat(Infinity).filter((c) => c !== null && c !== undefined && c !== false));
const plural = (n, one, many) => (n === 1 ? one : many);
const noData = () => [h('span', { 'aria-hidden': 'true' }, '—'), h('span', { class: 'sr' }, 'sem dado')];

// collectDropped lists the files of a drop, walking dropped folders. The directory entries must be
// taken from the event before the first await, so the first statements run synchronously.
async function collectDropped(dt) {
  const entries = Array.from(dt.items || []).map((i) => (i.webkitGetAsEntry ? i.webkitGetAsEntry() : null)).filter(Boolean);
  if (entries.length === 0) return Array.from(dt.files || []);
  const out = [];
  const walk = async (entry, depth) => {
    if (out.length >= MAX_DROPPED) return;
    if (entry.isFile) {
      out.push(await new Promise((res, rej) => entry.file(res, rej)));
    } else if (entry.isDirectory && depth < 4) {
      const reader = entry.createReader();
      for (;;) {
        const batch = await new Promise((res, rej) => reader.readEntries(res, rej));
        if (batch.length === 0) break;
        for (const e of batch) await walk(e, depth + 1);
      }
    }
  };
  for (const e of entries) await walk(e, 0);
  return out;
}

const helpBlock = () => h('details', { class: 'howto real-help' },
  h('summary', {}, 'Como ler esta aba'),
  h('ul', {},
    h('li', {}, h('strong', {}, 'O que importar: '), 'as planilhas CSV da garagem (o pedido à operadora e os modelos estão em ',
      h('code', {}, 'docs/dados-reais/'), '). ', h('em', {}, 'garagem'), ', ', h('em', {}, 'carregadores'), ' e ', h('em', {}, 'onibus'),
      ' são obrigatórias; ', h('em', {}, 'sessoes'), ' e ', h('em', {}, 'potencia'), ' são opcionais e dão energia, pico e custo. Escolha os arquivos, a pasta inteira ou arraste para a área.'),
    h('li', {}, h('strong', {}, 'Os arquivos ficam neste computador: '), 'o navegador lê o texto e fala só com este laboratório (127.0.0.1), que não grava nada em disco. Fechar ou recarregar a página esquece tudo.'),
    h('li', {}, h('strong', {}, 'Medido × estimado: '), 'a linha ', h('em', {}, 'real'), ' mostra o que as planilhas registram. “—” é dado que a planilha não traz. “estimado” marca pico e custo calculados a partir das sessões de carga, supostas constantes do início ao fim (costuma subestimar o pico); com ',
      h('em', {}, 'potencia.csv'), ' eles são medidos. Violações do plano, minutos acima do limite, mudanças de plano e movimentações não são medidos na realidade.'),
    h('li', {}, h('strong', {}, 'Premissas: '), 'as leituras de carga são perfeitas (ou têm o ruído que você informar), o limite da rede é fixo, nenhum defeito é injetado e o ',
      h('em', {}, 'planner'), ' supõe que os operadores executam todos os rodízios recomendados (o “sem rodízio” supõe nenhum). O resultado vale para esta garagem e estes dias.')));

// createRealView builds the tab into filesRoot (choosing and checking the files) and outRoot (the
// comparison). onOpenRun({ files, night, controller }) opens an imported night in the Execução tab.
export function createRealView({ filesRoot, outRoot, onOpenRun }) {
  const latest = createLatest(); // the import and the comparison share it: a new action drops the previous one
  const cache = new WeakMap(); // File -> its text, so a file is decoded once
  let held = {}; // canonical name -> File
  let ignored = [];
  let texts = {}; // name -> text of the files of the last import
  let imported = null; // answer of /api/import, null when none is good
  let importError = null; // formatImportError() of a spreadsheet error
  let failure = null; // { message, retry } of an error that is not about the data (network, busy, timeout)
  let report = null;
  let lastNoise = 0;
  let selected = 'all';
  let showAllBuses = false;
  let runController = 'planner'; // the controller chosen for "Abrir esta noite", kept across redraws
  let choiceNote = ''; // why part of the last choice was left out (size limits)
  // What the tab is doing. The Comparar button is always derived from these (syncButton); `seq`
  // numbers the actions, so one that was superseded never resets what the newer one set.
  let importing = false;
  let comparing = false;
  let seq = 0;

  // ---- the file chooser (built once, so focus and the noise value survive every redraw) ----

  // The status is the page's one polite announcement for each phase; errors are written in it
// (and drawn below as text, without a second alert role).
  const statusEl = h('p', { class: 'real-status', role: 'status', 'aria-live': 'polite', tabindex: -1 }, EMPTY_STATUS);
  const setStatus = (text) => { if (statusEl.textContent !== text) statusEl.textContent = text; };
  const dyn = h('div', { class: 'real-dyn' });

  const pick = h('input', { id: 'real-pick', type: 'file', multiple: true, accept: '.csv,text/csv' });
  const pickDir = h('input', { id: 'real-pick-dir', type: 'file', webkitdirectory: true, multiple: true });
  const drop = h('div', { class: 'dropzone', role: 'group', 'aria-label': 'Área para soltar as planilhas' },
    h('p', { class: 'drop-hint' }, 'Arraste aqui os arquivos ou a pasta com as planilhas (só ficam neste computador), ou use os seletores.'),
    h('label', { class: 'filepick', for: 'real-pick' }, 'Escolher planilhas (.csv)', pick),
    h('label', { class: 'filepick', for: 'real-pick-dir' }, 'Escolher a pasta inteira', pickDir),
    h('button', { type: 'button', class: 'chip', id: 'real-clear', onclick: () => clearAll() }, 'Limpar tudo'));

  const noise = h('input', {
    id: 'real-noise', type: 'number', min: 0, step: 'any', value: '0', inputmode: 'decimal',
    'aria-describedby': 'real-noise-hint real-noise-err',
  });
  const noiseErr = h('div', { class: 'field-error', id: 'real-noise-err', role: 'alert' });
  const compareBtn = h('button', { type: 'submit', class: 'primary', id: 'real-compare', disabled: true }, 'Comparar');
  const compareForm = h('form', { class: 'form real-compare', novalidate: true },
    h('div', { class: 'field' },
      h('label', { for: 'real-noise' }, 'Ruído na leitura de carga (kWh)'), noise,
      h('span', { class: 'note', id: 'real-noise-hint' }, '0 = o planejador lê a carga de cada ônibus sem erro. Um valor maior testa como ele reage a leituras imprecisas.'),
      noiseErr),
    h('div', { class: 'actions' }, compareBtn));
  compareForm.addEventListener('submit', (e) => { e.preventDefault(); compare(); });

  put(filesRoot, h('h2', {}, 'Dados reais'), helpBlock(), drop, statusEl, dyn, compareForm);
  put(outRoot, h('p', { class: 'note' }, EMPTY_OUT));

  for (const input of [pick, pickDir]) {
    input.addEventListener('change', () => {
      const files = Array.from(input.files || []);
      input.value = ''; // the same file can be chosen again
      ingest(files);
    });
  }
  let depth = 0; // dragenter/dragleave also fire for the children of the zone
  drop.addEventListener('dragenter', (e) => { e.preventDefault(); depth++; drop.classList.add('over'); });
  drop.addEventListener('dragover', (e) => { e.preventDefault(); if (e.dataTransfer) e.dataTransfer.dropEffect = 'copy'; });
  drop.addEventListener('dragleave', () => { depth = Math.max(0, depth - 1); if (depth === 0) drop.classList.remove('over'); });
  drop.addEventListener('drop', (e) => {
    e.preventDefault();
    depth = 0;
    drop.classList.remove('over');
    collectDropped(e.dataTransfer).then(ingest, () => {
      failure = { message: 'Não consegui ler o que foi solto. Use os seletores de arquivo.', retry: null };
      setStatus(failure.message);
      paint();
    });
  });

  // ---- drawing the part under the chooser ----

  function fileTable() {
    const rows = FILE_SPECS.map((spec) => {
      const f = held[spec.name];
      const situation = f ? [h('span', { 'aria-hidden': 'true' }, '✓ '), `recebido (${fmtSize(f.size)})`]
        : spec.required ? [h('span', { 'aria-hidden': 'true' }, '✗ '), 'falta (obrigatório)']
          : [h('span', { 'aria-hidden': 'true' }, '– '), 'não enviado (opcional)'];
      return h('tr', {}, h('th', { scope: 'row' }, h('code', {}, spec.name)), h('td', {}, spec.required ? 'obrigatório' : 'opcional'),
        h('td', { class: 'reason' }, spec.what), h('td', {}, situation));
    });
    return h('div', { class: 'table-wrap' }, h('table', { class: 'metrics small' },
      h('caption', { class: 'sr' }, 'Arquivos esperados e situação de cada um'),
      h('thead', {}, h('tr', {}, ['arquivo', 'tipo', 'para quê', 'situação'].map((t) => h('th', { scope: 'col' }, t)))),
      h('tbody', {}, rows)));
  }

  function ignoredNote() {
    if (ignored.length === 0) return null;
    const shown = ignored.slice(0, 8).join(', ');
    const more = ignored.length > 8 ? ` e mais ${ignored.length - 8}` : '';
    return h('p', { class: 'note' }, `Ignorado${ignored.length > 1 ? 's' : ''} (${plural(ignored.length, 'nome não reconhecido', 'nomes não reconhecidos')}): ${shown}${more}.`);
  }

  function errorBlock() {
    if (importError) {
      const e = importError;
      return h('div', { class: 'real-errors' },
        h('h3', {}, 'Erro nos dados'),
        h('div', { class: 'table-wrap' }, h('table', { class: 'metrics small' },
          h('caption', { class: 'sr' }, 'Erro encontrado nas planilhas'),
          h('thead', {}, h('tr', {}, ['arquivo', 'linha', 'coluna', 'o que está errado'].map((t) => h('th', { scope: 'col' }, t)))),
          h('tbody', {}, h('tr', {},
            h('td', {}, e.file ? h('code', {}, e.file) : noData()),
            h('td', {}, e.line > 0 ? String(e.line) : noData()),
            h('td', {}, e.column ? h('code', {}, e.column) : noData()),
            h('td', { class: 'reason' }, e.message))))),
        h('p', { class: 'note' }, 'Corrija a planilha e escolha o arquivo de novo: ele substitui o anterior.'));
    }
    if (failure) {
      return h('div', { class: 'banner-inline' }, failure.message, ' ',
        failure.retry ? h('button', { type: 'button', class: 'chip', onclick: failure.retry }, 'Tentar de novo') : null);
    }
    return null;
  }

  function warningItem(w) {
    const where = [w.file, w.line > 0 ? `linha ${w.line}` : ''].filter(Boolean).join(', ');
    return h('li', {}, where ? h('code', {}, where) : null, where ? ': ' : '', w.message);
  }

  function warningsBlock(res) {
    const ws = res.warnings || [];
    if (ws.length === 0) return h('p', { class: 'note' }, 'Sem avisos: nada foi suposto nem deixado de fora na leitura das planilhas.');
    return h('div', { class: 'real-warnings' },
      h('h3', {}, `Avisos (${ws.length})`),
      h('p', { class: 'note' }, 'O que foi suposto, ignorado ou deixado de fora ao ler as planilhas.'),
      h('ul', {}, ws.slice(0, MAX_LISTED).map(warningItem)),
      ws.length > MAX_LISTED ? h('details', {}, h('summary', {}, `Mais ${ws.length - MAX_LISTED} ${plural(ws.length - MAX_LISTED, 'aviso', 'avisos')}`),
        h('ul', {}, ws.slice(MAX_LISTED).map(warningItem))) : null);
  }

  function summaryBlock(res) {
    const s = summarizeImport(res);
    const nightTable = h('div', { class: 'table-wrap' }, h('table', { class: 'metrics small' },
      h('caption', { class: 'sr' }, 'Noites encontradas nas planilhas'),
      h('thead', {}, h('tr', {}, ['noite', 'ônibus', 'saídas medidas', 'prontos na realidade'].map((t) => h('th', { scope: 'col' }, t)))),
      h('tbody', {}, s.nightRows.map((r) => h('tr', {},
        h('th', { scope: 'row' }, r.key), h('td', {}, String(r.buses)), h('td', {}, String(r.withOutcome)),
        h('td', {}, r.ready === '—' ? noData() : r.ready))))));
    return h('div', { class: 'real-summary' },
      h('h3', {}, 'Planilhas lidas'),
      h('p', {}, `${s.nights} ${plural(s.nights, 'noite', 'noites')} e ${s.buses} ${plural(s.buses, 'ônibus', 'ônibus')} no total.`),
      h('ul', { class: 'extras' }, s.extras.map((e) => h('li', {}, h('span', { 'aria-hidden': 'true' }, e.present ? '✓ ' : '– '), `${e.label}: ${e.present ? 'sim' : 'não'}`))),
      nightTable);
  }

  // syncButton: label and disabled of Comparar, from the state, in this one place.
  function syncButton() {
    const s = compareButtonState({ imported: Boolean(imported && imported.nights.length > 0), importing, comparing });
    if (compareBtn.textContent !== s.label) compareBtn.textContent = s.label;
    compareBtn.disabled = s.disabled;
  }

  function paint() {
    put(dyn,
      errorBlock(), choiceNote ? h('p', { class: 'note' }, choiceNote) : null, fileTable(), ignoredNote(),
      imported ? [summaryBlock(imported), warningsBlock(imported)] : null);
    syncButton();
  }

  // ---- reading and importing ----

  const abortError = () => new DOMException('superseded', 'AbortError');

  // textOf decodes a File once (the cache also holds the text of every file kept in `held`).
  async function textOf(f) {
    let t = cache.get(f);
    if (t === undefined) {
      t = readFileText(await f.arrayBuffer());
      cache.set(f, t);
    }
    return t;
  }

  const unreadable = (name) => new ApiError(`Não consegui ler ${name}. Se o arquivo mudou ou foi movido, escolha-o de novo.`, 'files', 400);

  // readAll returns the text of every held file.
  async function readAll(signal) {
    const out = {};
    for (const [name, f] of Object.entries(held)) {
      try {
        out[name] = await textOf(f);
      } catch {
        if (held[name] === f) delete held[name]; // it must not stay in the list; a newer file of this name is left alone
        throw unreadable(name);
      }
      if (signal.aborted) throw abortError();
    }
    return out;
  }

  function resetResults() {
    report = null;
    selected = 'all';
    put(outRoot, h('p', { class: 'note' }, EMPTY_OUT));
  }

  // invalidate drops whatever is running and the results: the files held no longer give a good import.
  function invalidate() {
    latest.cancel();
    seq++;
    importing = false;
    comparing = false;
    imported = null;
    importError = null;
    failure = null;
    resetResults();
  }

  function clearAll() {
    invalidate();
    held = {}; ignored = []; texts = {}; choiceNote = '';
    noiseErr.textContent = '';
    noise.removeAttribute('aria-invalid');
    setStatus(EMPTY_STATUS);
    paint();
  }

  async function ingest(files) {
    if (files.length === 0) return;
    const { known, ignored: skipped } = pickKnownFiles(files);
    ignored = skipped;
    choiceNote = '';
    if (Object.keys(known).length === 0) {
      // Nothing usable in this choice: say so, and leave the import, the results and the files as they were.
      const names = FILE_SPECS.map((f) => f.name).join(', ');
      choiceNote = files.length === 1 ? `O arquivo escolhido não é um dos cinco esperados (${names}).`
        : `Nenhum dos ${files.length} arquivos escolhidos é um dos cinco esperados (${names}).`;
      setStatus(choiceNote);
      paint();
      return;
    }
    const notes = [];
    const raw = checkSizes(Object.fromEntries(Object.entries(known).map(([n, f]) => [n, f.size])));
    for (const name of raw.drop) delete known[name];
    if (raw.message) notes.push(raw.message);
    setStatus(`Lendo ${Object.keys(known).length} ${plural(Object.keys(known).length, 'arquivo', 'arquivos')}…`);
    const incoming = {}; // name -> text
    for (const [name, f] of Object.entries(known)) {
      try {
        incoming[name] = await textOf(f);
      } catch {
        delete known[name];
        notes.push(unreadable(name).message);
      }
    }
    // The real size of the request is that of the JSON text, not of the files on disk.
    const heldTexts = Object.fromEntries(Object.entries(held).map(([n, f]) => [n, cache.get(f)]));
    const body = checkBody(heldTexts, incoming);
    if (body.refuse) {
      for (const name of Object.keys(incoming)) delete known[name];
      notes.push(body.message);
    }
    choiceNote = notes.join(' ');
    if (Object.keys(known).length === 0) { // everything chosen was refused: what was held stays as it was
      setStatus(choiceNote);
      paint();
      return;
    }
    held = { ...held, ...known }; // a new choice replaces the file of the same name and keeps the rest
    await runImport();
  }

  async function runImport() {
    const mine = ++seq;
    const refocus = dyn.contains(document.activeElement); // the "Tentar de novo" button is about to be redrawn
    importing = true;
    comparing = false; // a comparison in flight is aborted by the shared latest
    importError = null;
    failure = null;
    imported = null;
    resetResults();
    setStatus(`Lendo ${Object.keys(held).length} ${plural(Object.keys(held).length, 'arquivo', 'arquivos')}…`);
    paint();
    if (refocus) statusEl.focus({ preventScroll: true });
    let sent = 0; // bytes of the body of the request in flight
    try {
      const r = await latest(async (signal) => {
        const t = await readAll(signal);
        if (!signal.aborted) setStatus('Conferindo as planilhas neste computador…');
        sent = bodySize(t);
        return { t, res: await importFiles(t, signal) };
      });
      if (r.stale) return;
      texts = r.value.t;
      imported = r.value.res;
      setStatus(importStatusText(summarizeImport(imported), choiceNote));
    } catch (e) {
      imported = null;
      if (e instanceof ApiError && e.status === 400 && (e.file || e.line || e.column)) {
        importError = formatImportError(e);
        setStatus(errorStatusText(e, choiceNote));
      } else {
        const f = describeFailure(e, sent);
        failure = { message: f.message, retry: f.retry ? runImport : null };
        setStatus(failureStatusText(f.message, choiceNote));
      }
    } finally {
      if (mine === seq) {
        importing = false;
        paint();
      }
    }
  }

  // ---- comparing ----

  async function compare() {
    if (!imported) return;
    noiseErr.textContent = '';
    noise.removeAttribute('aria-invalid');
    const n = noise.value === '' ? 0 : Number(noise.value);
    if (!Number.isFinite(n) || n < 0) {
      noiseErr.textContent = 'Informe um número de 0 em diante.';
      noise.setAttribute('aria-invalid', 'true');
      noise.focus();
      return;
    }
    failure = null;
    report = null;
    const mine = ++seq;
    const refocus = outRoot.contains(document.activeElement); // the "Tentar de novo" button is about to be redrawn
    comparing = true;
    importing = false;
    syncButton();
    setStatus('Comparando: simulando o planejador e as referências em cada noite…');
    put(outRoot, h('h2', { tabindex: -1 }, 'Comparação com a realidade'), h('p', { class: 'loading' }, 'Rodando as simulações das noites…'));
    if (refocus) outRoot.querySelector('h2').focus({ preventScroll: true });
    try {
      const r = await latest((signal) => replay({ files: texts, soc_noise_kwh: n }, signal));
      if (r.stale) return;
      report = r.value;
      lastNoise = n;
      selected = 'all';
      showAllBuses = false;
      setStatus('Comparação pronta.');
      renderReport();
      focusHeading();
    } catch (e) {
      report = null;
      const f = describeFailure(e, bodySize(texts));
      const msg = f.message;
      if (e instanceof ApiError && e.field === 'soc_noise_kwh') {
        setStatus(''); // the field's own alert says it
        noiseErr.textContent = msg;
        noise.setAttribute('aria-invalid', 'true');
        put(outRoot, h('p', { class: 'note' }, 'Nenhuma comparação: corrija o ruído e tente de novo.'));
        noise.focus();
      } else {
        setStatus(`A comparação falhou: ${msg}`);
        put(outRoot, h('h2', { tabindex: -1 }, 'Comparação com a realidade'),
          h('div', { class: 'banner-inline' }, msg, ' ', f.retry ? h('button', { type: 'button', class: 'chip', onclick: compare }, 'Tentar de novo') : null));
        focusHeading();
      }
    } finally {
      if (mine === seq) {
        comparing = false;
        syncButton();
      }
    }
  }

  // The heading takes focus once the result is there, unless the person has already left the tab.
  function focusHeading() {
    const panel = outRoot.closest('section');
    if (panel && panel.hidden) return;
    const heading = outRoot.querySelector('h2');
    if (!heading) return;
    heading.focus({ preventScroll: true });
    heading.scrollIntoView({ block: 'start' }); // the result starts at the top, with the banner and the table below it
  }

  // ---- drawing the comparison ----

  function renderCell(cell) {
    const td = h('td', { class: cell.bad ? 'bad' : '', title: cell.title || null },
      cell.bad ? '⚠ ' : null,
      cell.text === '—' ? noData() : cell.text,
      cell.estimated ? [' ', h('span', { class: 'seal', title: cell.title }, 'estimado')] : null,
      cell.partial ? [' ', h('span', { class: 'seal', title: SEAL_PARTIAL }, cell.partial)] : null,
      cell.detail ? h('small', { class: 'detail' }, cell.detail) : null);
    return td;
  }

  function metricsTable(rows) {
    const cols = columns();
    const head = h('tr', {}, h('th', { scope: 'col' }, 'linha'), cols.map((c) => h('th', { scope: 'col', title: c.help }, c.label)));
    const realRow = h('tr', { class: 'real-row reveal', style: '--i:0' },
      h('th', { scope: 'row', title: controllerHelp('real') }, 'real', h('span', { class: 'sr' }, ' (o que aconteceu)')),
      realRowCells(rows.real).map(renderCell));
    const sims = rows.controllers.map((c, i) => h('tr', { class: c.name === 'planner' ? 'planner reveal' : 'reveal', style: `--i:${i + 1}` },
      h('th', { scope: 'row', title: controllerHelp(c.name) }, c.name),
      controllerCells(c, report.cost_comparable).map(renderCell)));
    return h('div', { class: 'table-wrap' }, h('table', { class: 'metrics real-table' },
      h('caption', { class: 'sr' }, 'Linha real e controladores simulados'),
      h('thead', {}, head), h('tbody', {}, realRow, sims)));
  }

  function tableNotes(real) {
    const partial = partialText(real);
    return [
      partial ? h('p', { class: 'note' }, h('span', { class: 'seal' }, 'parcial'), ' ', partial) : null,
      h('p', { class: 'note' }, h('strong', {}, '—'), ' = a planilha não traz esse dado. ', h('span', { class: 'seal' }, 'estimado'),
        ' = calculado com as sessões de carga espalhadas uniformemente no tempo (subestima o pico); com potencia.csv o valor é medido. ',
        'Violações do plano, minutos acima do limite, mudanças de plano e movimentações não são medidos na realidade. Cada linha simulada é uma execução por noite (ou a média das noites).'),
      report.cost_comparable ? null : h('p', { class: 'note' }, 'O custo simulado aparece como “—”: faltam as colunas de tarifa em garagem.csv ou a janela de ponta atravessa a meia-noite, que o simulador não representa. O custo real, quando existe, continua na linha real.'),
      lastNoise > 0 ? h('p', { class: 'note' }, `Esta comparação usou ruído de ${fmtNum(lastNoise, 1)} kWh na leitura de carga. A execução detalhada não aplica esse ruído.`) : null,
    ];
  }

  const statusCell = (ready, kwh) => h('td', {},
    ready === null || ready === undefined ? noData() : [h('span', { 'aria-hidden': 'true' }, ready ? '✓ ' : '✗ '), busStatus(ready)],
    typeof kwh === 'number' ? ` · ${fmtNum(kwh, 0)} kWh` : null);

  function busesBlock(night) {
    if (!night) return h('p', { class: 'note' }, 'Escolha uma noite para ver, ônibus a ônibus, quem saiu pronto na realidade, no planejador e no planejador sem rodízio.');
    const worst = worstBuses(night);
    const ids = new Set(worst.map((b) => b.id));
    const rows = (night.per_bus || []).filter((b) => showAllBuses || ids.has(b.id));
    const ok = (key) => worst.filter((b) => b[key] === 'pronto').length;
    let line;
    if (night.real.with_outcome === 0) line = 'Esta noite não tem a carga de saída real (soc_saida_real_pct): não dá para comparar ônibus a ônibus com a realidade.';
    else if (worst.length === 0) line = 'Na realidade, nenhum dos ônibus com saída medida deixou de sair pronto nesta noite.';
    else line = `Na realidade, ${worst.length} ${plural(worst.length, 'ônibus não saiu pronto', 'ônibus não saíram prontos')}. O planejador deixaria ${ok('planner')} ${plural(ok('planner'), 'pronto', 'prontos')} e o planejador sem rodízio, ${ok('noSwap')}.`;
    const toggle = h('input', { type: 'checkbox', id: 'real-show-all', checked: showAllBuses });
    toggle.addEventListener('change', () => { showAllBuses = toggle.checked; renderBody(); document.getElementById('real-show-all')?.focus(); });
    const table = rows.length === 0 ? null : h('div', { class: 'table-wrap' }, h('table', { class: 'metrics small' },
      h('caption', { class: 'sr' }, `Ônibus da noite ${night.key}: realidade, planejador e planejador sem rodízio`),
      h('thead', {}, h('tr', {}, ['ônibus', 'carga exigida (kWh)', 'na realidade', 'planejador', 'planejador sem rodízio'].map((t) => h('th', { scope: 'col' }, t)))),
      h('tbody', {}, rows.map((b) => h('tr', {},
        h('th', { scope: 'row' }, b.id), h('td', {}, fmtNum(b.target_kwh, 0)),
        statusCell(b.real_ready, b.real_final_kwh),
        statusCell(b.planner_ready, b.planner_final_kwh),
        statusCell(b.no_swap_ready, b.no_swap_final_kwh))))));
    return h('div', { class: 'real-buses' },
      h('p', { class: 'summary' }, line),
      h('label', { class: 'check-inline' }, toggle, ' mostrar todos os ônibus da noite'),
      table);
  }

  function openBlock(night) {
    const options = controllerOptions(true);
    const current = pickOption(options, runController, 'planner');
    const choice = h('select', { id: 'real-run-controller', 'aria-label': 'Controlador da execução' },
      options.map((c) => h('option', { value: c, selected: c === current }, c)));
    choice.addEventListener('change', () => { runController = choice.value; });
    const btn = h('button', { type: 'button', class: 'primary', id: 'real-open-run', disabled: !night }, 'Abrir esta noite na Execução');
    btn.addEventListener('click', () => onOpenRun({ files: texts, night: night.key, controller: choice.value }));
    return h('div', { class: 'real-open' },
      h('h3', {}, 'Execução detalhada'),
      h('p', { class: 'note' }, night
        ? 'Veja a noite minuto a minuto: potência contra o limite, carga de cada ônibus e decisões, com o controlador escolhido.'
        : 'Escolha uma noite para abri-la na Execução.'),
      h('div', { class: 'run-controls' }, h('label', {}, 'Controlador ', choice), btn));
  }

  const body = h('div', { class: 'real-body' });
  function renderBody() {
    const rows = pickRows(report, selected);
    if (!rows) return;
    put(body, 
      metricsTable(rows), tableNotes(rows.real),
      h('h3', {}, 'Ônibus a ônibus'), busesBlock(rows.night),
      openBlock(rows.night));
  }

  function renderReport() {
    if (!report.nights || report.nights.length === 0) {
      put(outRoot, h('h2', { tabindex: -1 }, 'Comparação com a realidade'), h('p', { class: 'note' }, 'Nenhuma noite para comparar: veja os avisos da importação.'));
      return;
    }
    const nightSelect = h('select', { id: 'real-night' },
      h('option', { value: 'all' }, `Todas (média de ${report.nights.length} ${plural(report.nights.length, 'noite', 'noites')})`),
      report.nights.map((n) => h('option', { value: n.key }, `${n.key} (${n.buses} ${plural(n.buses, 'ônibus', 'ônibus')})`)));
    nightSelect.addEventListener('change', () => { selected = nightSelect.value; showAllBuses = false; renderBody(); });
    put(outRoot, 
      h('h2', { tabindex: -1 }, 'Comparação com a realidade'),
      h('p', { class: 'real-banner' }, BANNER),
      h('h3', {}, 'Premissas'),
      h('ul', { class: 'assumptions' }, (report.assumptions || []).map((a) => h('li', {}, a))),
      h('div', { class: 'real-night' }, h('label', { for: 'real-night' }, 'Noite '), nightSelect),
      body);
    renderBody();
  }

  paint();
  return { clear: clearAll };
}
