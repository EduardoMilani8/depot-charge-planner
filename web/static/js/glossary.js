import { fmtNum, fmtPct, fmtBRL, fmtBRL2 } from './format.js';
import { hasBill, describeCost } from './cost.js';

const f = {
  pct: (v) => fmtPct(v),
  kwh: (v) => fmtNum(v, 0),
  kw: (v) => fmtNum(v, 0),
  int: (v) => fmtNum(v, 0),
  num1: (v) => fmtNum(v, 1),
  brl: (v) => fmtBRL(v),
};

export const COLUMNS = [
  { key: 'ready_pct', label: 'ônibus prontos', fmt: f.pct, help: 'Porcentagem de ônibus que saem com a carga necessária para a rota. É a métrica principal.' },
  { key: 'shortfall_kwh', label: 'déficit (kWh)', fmt: f.kwh, help: 'Energia que faltava, em média por execução, nos ônibus que saíram sem a carga necessária.' },
  { key: 'peak_kw', label: 'pico (kW)', fmt: f.kw, help: 'Maior potência física puxada da rede.' },
  { key: 'plan_violations', label: 'violações do plano', fmt: f.int, mustBeZero: true, help: 'Quantas vezes o plano mandou mais potência do que o limite da garagem. Precisa ser 0.' },
  { key: 'overshoot_min', label: 'minutos acima do limite', fmt: f.int, help: 'Minutos em que a potência física passou do limite (por exemplo logo após uma queda de rede, pelo atraso de um passo entre comando e potência).' },
  { key: 'energy_kwh', label: 'energia (kWh)', fmt: f.kwh, help: 'Energia entregue pela rede, em média por execução.' },
  { key: 'cost_brl', label: 'custo (R$)', fmt: f.brl, help: 'Custo da energia com tarifa simples de ponta e fora de ponta, em média por execução.' },
  { key: 'plan_changes', label: 'mudanças de plano', fmt: f.int, help: 'Quantas vezes a potência de algum carregador mudou (soma das execuções): mede a estabilidade.' },
  { key: 'operator_moves', label: 'movimentações por noite', fmt: f.num1, help: 'Trabalho manual pedido aos operadores: rodízios executados ou ônibus desconectados, em média por execução.' },
  { key: 'plan_p99_micros', label: 'p99 (µs)', fmt: f.int, help: 'Tempo de cálculo do planejador no pior 1% dos ciclos. Aproximado quando várias simulações rodam em paralelo.' },
];

// Shown only when a comparison carries a bill (the server sends one when demand is priced). They sit
// right after the energy cost, so p99 stays the last column.
const bill = (key, fmt) => (m) => fmt(m.bill?.[key]);
export const BILL_COLUMNS = [
  { key: 'bill_demand_kw', label: 'demanda kW', get: bill('demand_kw', (v) => fmtNum(v, 0)),
    help: 'Demanda: a maior potência média da rede em um bloco de 15 minutos da noite, que é o que a distribuidora cobra.' },
  { key: 'bill_demand_brl', label: 'demanda R$/mês (proxy)', get: bill('demand_brl', fmtBRL2),
    help: 'Cobrança de demanda estimada para o mês, tratando a demanda desta noite como a do mês (proxy). Com contrato, vale o contratado.' },
  { key: 'bill_monthly_brl', label: 'fatura mensal estimada R$', get: bill('monthly_brl', fmtBRL2),
    help: 'Estimativa: energia da noite × noites por mês + a cobrança de demanda. Não é uma fatura.' },
];

// columnsFor: the columns of the comparison table, with the bill ones after the energy cost when the data has a bill.
export function columnsFor(data) {
  if (!hasBill(data)) return COLUMNS;
  const i = COLUMNS.findIndex((c) => c.key === 'cost_brl');
  return [...COLUMNS.slice(0, i + 1), ...BILL_COLUMNS, ...COLUMNS.slice(i + 1)];
}

export const COST_TERMS = [
  ['bloco de 15 min', 'A distribuidora mede a potência média da rede em blocos de 15 minutos alinhados ao relógio (:00, :15, :30, :45). A demanda é o maior bloco.'],
  ['Azul e Verde', 'Azul: um preço de demanda na ponta e outro fora da ponta, cada um sobre o maior bloco da sua janela. Verde: uma única cobrança, ao preço fora da ponta, sobre o maior bloco da noite (o preço de ponta é ignorado).'],
  ['demanda contratada', 'Contrato (0 = sem contrato). Com contrato, paga-se o contratado mesmo que a garagem use menos; sem contrato, a demanda medida serve de proxy do que seria contratado.'],
  ['ultrapassagem', 'Quando o maior bloco passa de 105% do contratado, cobra-se o contratado mais o excedente a 2 vezes o preço de demanda (valores a conferir com a distribuidora).'],
  ['proxy mensal', 'O laboratório simula uma noite. A fatura mensal estimada trata a demanda dessa noite como a do mês e multiplica a energia pelo número de noites por mês: é uma estimativa, não uma fatura.'],
  ['adiado', 'Ônibus que o planejador consciente de custo mantém ligado sem carregar até a ponta (18:00 às 21:00) acabar, porque ainda sai pronto. Limitar a demanda (meta) não é, por si só, economia na fatura: pode até aumentá-la.'],
];

export const CONTROLLER_HELP = {
  fifo: 'Carrega por ordem de chegada, no máximo de potência, até acabar o limite.',
  edf: 'Carrega primeiro quem sai mais cedo.',
  'fifo-unplug': 'Ordem de chegada mais a rotina de hoje: operadores desconectam ônibus já carregados quando outro espera.',
  safe: 'Só o perfil seguro: divide o limite igualmente entre os ônibus conectados.',
  planner: 'O planejador: menor folga primeiro, potência na hora certa e rodízios recomendados.',
};

export function formatCell(col, metrics) {
  return col.get ? col.get(metrics) : col.fmt(metrics[col.key]);
}

export const FAULT_LABEL = {
  charger_fail: 'carregador em falha',
  charger_offline: 'carregador sem comunicação',
  limit_drop: 'queda do limite da rede',
  soc_noise: 'ruído na leitura de carga',
  soc_bias: 'viés na leitura de carga',
  soc_freeze: 'leitura de carga congelada',
  soc_missing: 'leitura de carga ausente',
  consumption_over: 'consumo acima do previsto',
  late_arrival: 'chegada atrasada',
  early_departure: 'saída antecipada',
  planner_panic: 'planejador travou',
  planner_slow: 'planejador lento',
  planner_garbage: 'planejador devolveu plano inválido',
};

export const LAYER_LABEL = { normal: 'normal', 'last-valid': 'último plano válido', safe: 'perfil seguro' };

// Faults that change the site's power picture (drawn on the power chart).
export const POWER_FAULTS = new Set(['limit_drop', 'charger_fail', 'charger_offline', 'planner_panic', 'planner_slow', 'planner_garbage']);

const VALUE_TEXT = {
  limit_drop: (v) => `×${String(v).replace('.', ',')}`,
  soc_noise: (v) => `${v} kWh`,
  soc_bias: (v) => `${v} kWh`,
  consumption_over: (v) => `+${v} kWh`,
  late_arrival: (v) => `${v} min`,
  early_departure: (v) => `saída no minuto ${v}`,
};

// describeFault: "<kind> (<target>, <value>), min <from>–<to>"; "*" means every bus.
export function describeFault(f) {
  const label = FAULT_LABEL[f.kind] || f.kind;
  const bits = [];
  if (f.target && f.target !== '*') bits.push(f.target);
  if (VALUE_TEXT[f.kind] && f.value !== undefined && f.value !== null) bits.push(VALUE_TEXT[f.kind](f.value));
  return `${label}${bits.length ? ` (${bits.join(', ')})` : ''}, min ${f.from}–${f.to}`;
}

const PROFILE_PT = { none: 'sem falhas', mild: 'falhas leves', severe: 'falhas severas', random: 'falhas aleatórias' };

// describeParams: one line with the scenario's parameters ("30 ônibus, 12 carregadores, ...").
// The reading age shows only when there is one; the number of seeds only on request (a single
// run has no seeds to speak of).
export function describeParams(p, { seeds = false } = {}) {
  const bits = [`${p.buses} ônibus`, `${p.chargers} carregadores`, `limite ${fmtNum(p.limit_kw, 0)} kW`, PROFILE_PT[p.profile] || p.profile];
  if (seeds) bits.push(`${p.seeds} sementes`);
  if (p.reading_age_min > 0) bits.push(`leitura com ${p.reading_age_min} min de idade`);
  bits.push(`operadores ${p.follow_swaps ? 'seguem' : 'não seguem'} os rodízios`);
  bits.push(...describeCost(p));
  return bits.join(', ');
}
