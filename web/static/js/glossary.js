import { fmtNum, fmtPct, fmtBRL } from './format.js';

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

export const CONTROLLER_HELP = {
  fifo: 'Carrega por ordem de chegada, no máximo de potência, até acabar o limite.',
  edf: 'Carrega primeiro quem sai mais cedo.',
  'fifo-unplug': 'Ordem de chegada mais a rotina de hoje: operadores desconectam ônibus já carregados quando outro espera.',
  safe: 'Só o perfil seguro: divide o limite igualmente entre os ônibus conectados.',
  planner: 'O planejador: menor folga primeiro, potência na hora certa e rodízios recomendados.',
};

export function formatCell(col, metrics) {
  return col.fmt(metrics[col.key]);
}
