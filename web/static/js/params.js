export const TABS = ['scenario', 'compare', 'run'];
export const CONTROLLERS = ['fifo', 'edf', 'fifo-unplug', 'safe', 'planner'];

const KINDS = {
  buses: 'int', chargers: 'int', limit_kw: 'float', profile: 'string', seeds: 'int',
  follow_swaps: 'bool', reading_age_min: 'int', swap_back_cooldown_min: 'int', swap_back_min_need_kwh: 'float',
};

// coerce turns a URL string into the parameter's kind, or returns the fallback.
export function coerce(kind, raw, fallback) {
  if (raw === undefined || raw === null) return fallback;
  switch (kind) {
    case 'bool':
      return raw === 'true' ? true : raw === 'false' ? false : fallback;
    case 'int': {
      const n = Number(raw);
      return raw !== '' && Number.isInteger(n) ? n : fallback;
    }
    case 'float': {
      const n = Number(raw);
      return raw !== '' && Number.isFinite(n) ? n : fallback;
    }
    default:
      return String(raw);
  }
}

export function stateToHash(state) {
  const q = new URLSearchParams();
  q.set('tab', state.tab);
  q.set('seed', String(state.seed));
  q.set('controller', state.controller);
  for (const key of Object.keys(KINDS)) q.set(key, String(state.params[key]));
  return '#' + q.toString();
}

export function hashToState(hash, defaults) {
  const q = new URLSearchParams(String(hash || '').replace(/^#/, ''));
  const params = {};
  for (const [key, kind] of Object.entries(KINDS)) params[key] = coerce(kind, q.get(key), defaults[key]);
  const tab = TABS.includes(q.get('tab')) ? q.get('tab') : 'scenario';
  const seed = coerce('int', q.get('seed'), 1);
  const controller = CONTROLLERS.includes(q.get('controller')) ? q.get('controller') : 'planner';
  return { params, tab, seed: seed >= 1 ? seed : 1, controller };
}

// fieldSpecs describes the form; bounds come from the server's limits.
export function fieldSpecs(limits) {
  return [
    { key: 'buses', label: 'Ônibus', kind: 'int', min: 1, max: limits.max_buses },
    { key: 'chargers', label: 'Carregadores', kind: 'int', min: 1, max: limits.max_chargers },
    { key: 'limit_kw', label: 'Limite da garagem (kW)', kind: 'float', min: 0, max: limits.max_limit_kw },
    { key: 'profile', label: 'Perfil de falhas', kind: 'select',
      options: [['none', 'nenhuma'], ['mild', 'leve'], ['severe', 'severa'], ['random', 'aleatória']] },
    { key: 'seeds', label: 'Sementes (repetições)', kind: 'int', min: 1, max: limits.max_seeds },
    { key: 'reading_age_min', label: 'Idade da leitura de carga (min)', kind: 'int', min: 0, max: limits.max_reading_age_min },
    { key: 'follow_swaps', label: 'Operadores seguem os rodízios recomendados', kind: 'bool' },
    { key: 'swap_back_cooldown_min', label: 'Espera para voltar a um carregador (min)', kind: 'int', min: 0, advanced: true },
    { key: 'swap_back_min_need_kwh', label: 'Necessidade mínima para voltar (kWh)', kind: 'float', min: 0, advanced: true },
  ];
}
