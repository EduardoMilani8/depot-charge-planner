export class ApiError extends Error {
  // `where` is the location of a spreadsheet error: { file, line, column }.
  constructor(message, field = '', status = 0, where = {}) {
    super(message);
    this.name = 'ApiError';
    this.field = field;
    this.status = status;
    this.file = where.file || '';
    this.line = where.line || 0;
    this.column = where.column || '';
  }
}

async function request(method, url, body, signal) {
  let resp;
  try {
    resp = await fetch(url, {
      method, signal,
      headers: body ? { 'Content-Type': 'application/json' } : undefined,
      body: body ? JSON.stringify(body) : undefined,
    });
  } catch (e) {
    if (e.name === 'AbortError') throw e;
    throw new ApiError('Não consegui falar com o servidor. Ele ainda está rodando?', '', 0);
  }
  let data = null;
  try { data = await resp.json(); } catch { /* not JSON */ }
  if (!resp.ok) {
    throw new ApiError((data && data.error) || `Erro ${resp.status}`, (data && data.field) || '', resp.status,
      { file: data?.file, line: data?.line, column: data?.column });
  }
  return data;
}

export const getDefaults = (signal) => request('GET', '/api/defaults', null, signal);
export const compare = (params, signal) => request('POST', '/api/compare', params, signal);
export const run = (req, signal) => request('POST', '/api/run', req, signal);
// The real-data routes receive the text of the spreadsheets in every request ({files: {name: text}}):
// the server keeps nothing between calls.
export const importFiles = (files, signal) => request('POST', '/api/import', { files }, signal);
export const replay = (req, signal) => request('POST', '/api/replay', req, signal);

// createLatest returns latest(fn): it runs fn(signal), aborts the previous call and
// delivers only the latest call's result ({stale:true} for the others).
export function createLatest() {
  let ctrl = null;
  let n = 0;
  let inflight = 0;
  async function latest(fn) {
    if (ctrl) ctrl.abort();
    ctrl = new AbortController();
    const mine = ++n;
    inflight++;
    try {
      const value = await fn(ctrl.signal);
      return mine === n ? { stale: false, value } : { stale: true };
    } catch (e) {
      if (e.name === 'AbortError' || mine !== n) return { stale: true };
      throw e;
    } finally {
      inflight--;
    }
  }
  // cancel() supersedes the call in flight without starting another: it is aborted and
  // reports {stale:true}, so nothing of it is rendered and none of its errors surface.
  latest.cancel = () => {
    n++;
    if (ctrl) ctrl.abort();
  };
  latest.busy = () => inflight > 0;
  return latest;
}
