export class ApiError extends Error {
  constructor(message, field = '', status = 0) {
    super(message);
    this.name = 'ApiError';
    this.field = field;
    this.status = status;
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
  if (!resp.ok) throw new ApiError((data && data.error) || `Erro ${resp.status}`, (data && data.field) || '', resp.status);
  return data;
}

export const getDefaults = (signal) => request('GET', '/api/defaults', null, signal);
export const compare = (params, signal) => request('POST', '/api/compare', params, signal);
export const run = (req, signal) => request('POST', '/api/run', req, signal);

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
