import { COST_KEYS, COST_KINDS, normalizeCostParams } from './cost.js';

export const TABS = ['scenario', 'compare', 'real', 'run'];
export const CONTROLLERS = ['fifo', 'edf', 'fifo-unplug', 'safe', 'planner'];

// nextTab returns the tab a key moves to (arrows wrap, Home/End jump), or null for other keys.
export function nextTab(current, key) {
  const i = TABS.indexOf(current);
  if (i < 0) return null;
  switch (key) {
    case 'ArrowRight': return TABS[(i + 1) % TABS.length];
    case 'ArrowLeft': return TABS[(i + TABS.length - 1) % TABS.length];
    case 'Home': return TABS[0];
    case 'End': return TABS[TABS.length - 1];
    default: return null;
  }
}

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
  // Cost fields at their default (zero) are left out, so a link without cost settings is the one it always was.
  const cost = normalizeCostParams(state.params);
  for (const key of COST_KEYS) if (cost[key] !== undefined) q.set(key, String(cost[key]));
  return '#' + q.toString();
}

export function hashToState(hash, defaults) {
  const q = new URLSearchParams(String(hash || '').replace(/^#/, ''));
  const params = {};
  for (const [key, kind] of Object.entries(KINDS)) params[key] = coerce(kind, q.get(key), defaults[key]);
  for (const [key, kind] of Object.entries(COST_KINDS)) {
    const v = coerce(kind, q.get(key), undefined);
    if (v !== undefined) params[key] = v;
  }
  const tab = TABS.includes(q.get('tab')) ? q.get('tab') : 'scenario';
  const seed = coerce('int', q.get('seed'), 1);
  const controller = CONTROLLERS.includes(q.get('controller')) ? q.get('controller') : 'planner';
  return { params: normalizeCostParams(params), tab, seed: seed >= 1 ? seed : 1, controller };
}

// linkState is the state a link must carry. On the run tab that is the run on screen
// (`run` = { params, controller, seed }, or null when none): `state.params` may already hold
// the parameters of a newer comparison, and the link has to reproduce what the person sees.
// Other tabs show what `state` holds. A run of imported real data (`run.real`) cannot be rebuilt
// from a link, because the files stay on this computer: its link only names the "Dados reais" tab.
export function linkState(state, run) {
  if (state.tab !== 'run' || !run) return state;
  if (run.real) return { ...state, tab: 'real' };
  return { ...state, params: run.params, controller: run.controller, seed: run.seed };
}

// decideHashAction: what a hashchange should do. null when `hash` already describes `state`
// (after normalising: key order, garbage values), so pasting the same link needs no re-run;
// otherwise the next state read from the hash. `defaultParams` is the flat parameter object
// (`/api/defaults` reply's `.params`), not the whole reply.
export function decideHashAction(hash, state, defaultParams) {
  const next = hashToState(hash, defaultParams);
  return stateToHash(next) === stateToHash(state) ? null : next;
}

// fieldSpecs describes the form; bounds come from the server's limits. `advanced` fields live in the
// collapsed block, in the group named by `group`: 'swap' (the anti-flip-flop rule) or 'cost'.
// `optional` fields may be left empty (the default, left out of requests and links); `hours` fields
// are typed in hours after 18:00 and travel in minutes; `needsCostAware` fields only count with the
// cost-aware planner on; `hint` is the short help shown under the field.
export function fieldSpecs(limits) {
  const hours = (min) => (min === undefined ? undefined : min / 60);
  return [
    { key: 'buses', label: 'Ônibus', kind: 'int', min: 1, max: limits.max_buses },
    { key: 'chargers', label: 'Carregadores', kind: 'int', min: 1, max: limits.max_chargers },
    { key: 'limit_kw', label: 'Limite da garagem (kW)', kind: 'float', min: 0, max: limits.max_limit_kw },
    { key: 'profile', label: 'Perfil de falhas', kind: 'select',
      options: [['none', 'nenhuma'], ['mild', 'leve'], ['severe', 'severa'], ['random', 'aleatória']] },
    { key: 'seeds', label: 'Sementes (repetições)', kind: 'int', min: 1, max: limits.max_seeds },
    { key: 'reading_age_min', label: 'Idade da leitura de carga (min)', kind: 'int', min: 0, max: limits.max_reading_age_min },
    { key: 'follow_swaps', label: 'Operadores seguem os rodízios recomendados', kind: 'bool' },
    { key: 'swap_back_cooldown_min', label: 'Espera para voltar a um carregador (min)', kind: 'int', min: 0, advanced: true, group: 'swap' },
    { key: 'swap_back_min_need_kwh', label: 'Necessidade mínima para voltar (kWh)', kind: 'float', min: 0, advanced: true, group: 'swap' },
    { key: 'arrival_from_min', label: 'Chegadas a partir de (horas depois das 18:00)', kind: 'hours', min: 0, max: hours(limits.max_arrival_min), optional: true,
      placeholder: '2', advanced: true, group: 'cost', hint: 'Vazio = padrão: das 20:00 às 24:00. Para chegar na ponta (18:00 às 21:00) use de 0 a 3.' },
    { key: 'arrival_to_min', label: 'Chegadas até (horas depois das 18:00)', kind: 'hours', min: 0, max: hours(limits.max_arrival_min), optional: true,
      placeholder: '6', advanced: true, group: 'cost', hint: 'Precisa ser maior que o início e terminar antes da primeira saída.' },
    { key: 'cost_aware', label: 'Planejador consciente de custo', kind: 'bool', advanced: true, group: 'cost',
      hint: 'Só o planejador muda: adia a carga dos ônibus que ainda saem prontos para depois da ponta e pode limitar a demanda a uma meta.' },
    { key: 'defer_margin_min', label: 'Folga do adiamento (min)', kind: 'int', min: 0, max: limits.max_defer_margin_min, optional: true,
      placeholder: String(limits.default_defer_margin_min ?? 45), needsCostAware: true, advanced: true, group: 'cost',
      hint: 'Folga mínima que o ônibus adiado ainda deve ter ao terminar a carga. Vazio = padrão.' },
    { key: 'demand_target_kw', label: 'Meta de demanda (kW)', kind: 'float', min: 0, max: limits.max_limit_kw, optional: true, placeholder: 'sem meta',
      needsCostAware: true, advanced: true, group: 'cost', hint: 'Limita a potência média de cada bloco de 15 min. Pode reduzir a demanda e, mesmo assim, aumentar a fatura mensal.' },
    { key: 'demand_peak_price', label: 'Preço de demanda na ponta (R$/kW)', kind: 'float', min: 0, max: limits.max_demand_price, optional: true, placeholder: '0',
      advanced: true, group: 'cost' },
    { key: 'demand_offpeak_price', label: 'Preço de demanda fora da ponta (R$/kW)', kind: 'float', min: 0, max: limits.max_demand_price, optional: true, placeholder: '0',
      advanced: true, group: 'cost' },
    { key: 'demand_contracted_kw', label: 'Demanda contratada (kW)', kind: 'float', min: 0, max: limits.max_limit_kw, optional: true, placeholder: 'sem contrato',
      advanced: true, group: 'cost', hint: 'Com contrato, paga-se o contratado; acima de 105% há ultrapassagem.' },
    { key: 'demand_verde', label: 'Modo de demanda', kind: 'select', options: [['azul', 'Azul'], ['verde', 'Verde']],
      encode: (v) => (v ? 'verde' : 'azul'), decode: (v) => v === 'verde', advanced: true, group: 'cost', note: limits.demand_verde_note },
    { key: 'nights_per_month', label: 'Noites por mês (estimativa mensal)', kind: 'int', min: 1, max: limits.max_nights_per_month, optional: true,
      placeholder: String(limits.default_nights_per_month ?? 30), advanced: true, group: 'cost', hint: 'Quantas noites iguais a esta valem o mês na fatura estimada. Vazio = padrão.' },
  ];
}
