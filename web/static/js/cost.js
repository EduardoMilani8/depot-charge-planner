// Pure helpers of the energy-cost part of the lab: the cost parameters, the monthly bill sentences,
// the peak window and the deferred buses. Nothing here touches the DOM or the network.
import { clock, fmtNum, fmtBRL2 } from './format.js';

// The ten optional parameters of the cost group. Their default is the zero value (false / 0), and a
// default is never put in a hash or a request, so everything that existed before is unchanged.
export const COST_KEYS = [
  'cost_aware', 'defer_margin_min', 'demand_target_kw', 'demand_peak_price', 'demand_offpeak_price',
  'demand_contracted_kw', 'demand_verde', 'arrival_from_min', 'arrival_to_min', 'nights_per_month',
];
export const COST_KINDS = {
  cost_aware: 'bool', defer_margin_min: 'int', demand_target_kw: 'float', demand_peak_price: 'float', demand_offpeak_price: 'float',
  demand_contracted_kw: 'float', demand_verde: 'bool', arrival_from_min: 'int', arrival_to_min: 'int', nights_per_month: 'int',
};
// The server rejects these two unless cost_aware is on.
export const NEEDS_COST_AWARE = ['defer_margin_min', 'demand_target_kw'];

// normalizeCostParams returns a copy of `p` with the cost keys that are at their default removed and,
// with cost_aware off, the two keys that only make sense with it.
export function normalizeCostParams(p) {
  const out = { ...p };
  for (const k of COST_KEYS) {
    const v = out[k];
    if (v === undefined || v === false || v === 0 || (typeof v === 'number' && Number.isNaN(v))) delete out[k];
  }
  if (!out.cost_aware) for (const k of NEEDS_COST_AWARE) delete out[k];
  return out;
}

// costActive: some cost or demand setting is on (what makes the peak window worth drawing).
export function costActive(p) {
  return Boolean(p && (p.cost_aware || p.demand_peak_price > 0 || p.demand_offpeak_price > 0 || p.demand_contracted_kw > 0));
}

// The arrival fields are typed in hours after 18:00 and travel in minutes.
export const hoursToMin = (hours) => Math.round(hours * 60);
export const minToHours = (min) => Math.round((min / 60) * 100) / 100;

const planner = (data) => data?.controllers?.find((c) => c.name === 'planner');
const monthly = (data, name) => data?.controllers?.find((c) => c.name === name)?.aggregate?.bill?.monthly_brl;

// hasBill: the comparison carries a bill (the server sends one only when demand is priced).
export function hasBill(data) {
  return (data?.controllers ?? []).some((c) => c.aggregate?.bill);
}

// savingsSentence: what the planner saves per month against fifo and fifo-unplug, from the same
// monthly figures the table shows. null without a bill. With only savings the lead is "Economia";
// as soon as a difference is a cost (or nothing changes) the lead is neutral and each clause carries
// its own verb, so a mixed result never reads as a contradiction. The equivalence caveat is added only
// when the planner readies more buses than a baseline it is compared with (a fifo that leaves buses
// uncharged spends less, so the comparison is not like for like).
export function savingsSentence(controllers) {
  const data = { controllers };
  const pl = monthly(data, 'planner');
  if (!Number.isFinite(pl)) return null;
  const readyOf = (name) => controllers.find((c) => c.name === name)?.aggregate?.ready_pct;
  const clauses = [];
  let unequal = false;
  for (const base of ['fifo', 'fifo-unplug']) {
    const bm = monthly(data, base);
    if (!Number.isFinite(bm)) continue;
    const saved = bm - pl;
    const text = fmtBRL2(saved);
    if (readyOf('planner') > readyOf(base)) unequal = true;
    if (text.startsWith('-')) clauses.push({ base, saved: false, text: `custa ${fmtBRL2(-saved)} a mais contra ${base}` });
    else if (text === 'R$ 0,00') clauses.push({ base, saved: false, text: `não muda contra ${base} (R$ 0,00)` });
    else clauses.push({ base, saved: true, cost: false, text: `${text} contra ${base}`, verb: `economiza ${text} contra ${base}` });
  }
  if (clauses.length === 0) return null;
  const allSavings = clauses.every((c) => c.saved);
  const body = allSavings ? clauses.map((c) => c.text) : clauses.map((c) => c.verb ?? c.text);
  const lead = allSavings ? 'Economia mensal estimada do planner' : 'Diferença mensal estimada do planner';
  return `${lead}: ${body.join(' e ')} (estimativa: demanda de uma noite tratada como a do mês).`
    + (unequal ? ' Compare com a porcentagem de ônibus prontos: um fifo que deixa ônibus sem carga gasta menos, então a comparação não é equivalente.' : '');
}

// hoursMessage rewrites a server message about the arrival window, which speaks in minutes since
// 18:00, for the fields that are typed in hours after 18:00 ("599 minutos desde as 18:00" -> "9,98 horas
// depois das 18:00"). The server's own text is left alone.
export function hoursMessage(message) {
  const hours = (min) => String(minToHours(Number(min))).replace('.', ',');
  return String(message)
    .replace(/(\d+) minutos desde as 18:00/g, (_, m) => `${hours(m)} horas depois das 18:00`)
    .replace(/\((\d+) = (\d{2}:\d{2})\)/g, (_, m, t) => `(${hours(m)} = ${t})`);
}

// withoutTarget: the same parameters with the demand target removed (cost_aware stays).
export function withoutTarget(params) {
  const { demand_target_kw: _target, ...rest } = params;
  return rest;
}

// targetComparison compares the planner's estimated month with and without the demand target
// ({text, tone}). A cap can raise the bill (spec, section 4), so a rise is a warning.
export function targetComparison(withTarget, without, targetKw) {
  const a = monthly(withTarget, 'planner');
  const b = monthly(without, 'planner');
  if (!Number.isFinite(a) || !Number.isFinite(b)) return null;
  const diff = a - b;
  const money = fmtBRL2(Math.abs(diff));
  const target = `${fmtNum(targetKw, 0)} kW`;
  const both = `(${fmtBRL2(a)} com a meta contra ${fmtBRL2(b)} sem ela, no mesmo modo consciente de custo; estimativa)`;
  if (money === 'R$ 0,00') {
    return { tone: 'info', text: `Com a meta de demanda de ${target} não muda a fatura mensal estimada do planner ${both}.` };
  }
  if (diff > 0) {
    return {
      tone: 'warn',
      text: `Atenção: com a meta de demanda de ${target} a fatura mensal estimada do planner sobe ${money} por mês ${both}. `
        + 'Limitar a demanda não é, por si só, economia na fatura.',
    };
  }
  return { tone: 'info', text: `Com a meta de demanda de ${target} a fatura mensal estimada do planner cai ${money} por mês ${both}.` };
}

// needsTargetComparison: a demand target is set on a cost-aware planner and the comparison has a bill, so
// the "with and without the target" sentence can be built (it takes one more request). `data` is the
// comparison reply; its `params` are the server's, with every default filled in.
export function needsTargetComparison(data) {
  const p = data?.params;
  return Boolean(p && p.cost_aware && p.demand_target_kw > 0 && hasBill(data));
}

// billSummary: the bill beside the demand, for the summary of a run. null without a bill.
export function billSummary(metrics) {
  const b = metrics?.bill;
  if (!b) return null;
  return `demanda ${fmtNum(b.demand_kw, 0)} kW (${fmtBRL2(b.demand_brl)}/mês, proxy) · fatura mensal estimada ${fmtBRL2(b.monthly_brl)} (estimativa)`;
}

// ---- the peak window and the deferred buses ----

// The generator's tariff has its peak at 18:00-21:00 (internal/sim/gen.go); the API does not send
// the window, so it is drawn from these clock minutes.
const PEAK_FROM_CLOCK = 18 * 60;
const PEAK_LENGTH = 3 * 60;

// peakBand: the peak window in scenario minutes, clipped to the horizon; null when there is nothing
// to draw (no cost setting, an imported night with its own tariff, or a window past the horizon).
export function peakBand(data) {
  if (data.source || !costActive(data.params)) return null;
  const horizon = data.scenario.horizon_min;
  const start = ((data.scenario.start_clock_min % 1440) + 1440) % 1440;
  // A night that starts inside the peak has only what is left of it.
  if (start > PEAK_FROM_CLOCK && start < PEAK_FROM_CLOCK + PEAK_LENGTH) return { from: 0, to: Math.min(horizon, PEAK_FROM_CLOCK + PEAK_LENGTH - start) };
  const from = (((PEAK_FROM_CLOCK - start) % 1440) + 1440) % 1440;
  if (from >= horizon) return null;
  return { from, to: Math.min(horizon, from + PEAK_LENGTH) };
}

// The planner writes this reason for a bus held back until the peak ends (internal/planner/cost.go,
// candidate.deferredStatus). The Go test TestDeferredReasonPrefixIsPinnedForTheLab pins the prefix.
export const isDeferredReason = (text) => typeof text === 'string' && text.startsWith('adiado');

// deferredWindows: per bus id, the minute ranges during which the planner kept it deferred.
// The decisions list only what changed, so the state is carried from one decision to the next.
export function deferredWindows(data) {
  const open = new Map(); // id -> minute the deferral began
  const out = new Map();
  const close = (id, last) => {
    if (!out.has(id)) out.set(id, []);
    out.get(id).push({ from: open.get(id), to: last });
    open.delete(id);
  };
  for (const d of data.decisions) {
    for (const b of d.buses ?? []) {
      const deferred = b.r >= 0 && isDeferredReason(data.reasons[b.r]);
      if (deferred && !open.has(b.b)) open.set(b.b, d.minute);
      else if (!deferred && open.has(b.b)) close(b.b, d.minute - 1);
    }
    for (const id of d.gone ?? []) if (open.has(id)) close(id, d.minute - 1);
  }
  for (const id of [...open.keys()]) close(id, data.scenario.horizon_min);
  return out;
}

// ---- the scenario in words ----

const money = (v) => fmtNum(v, Number.isInteger(v) ? 0 : 2);

// describeCost: the cost settings of a scenario as phrases (empty when none is set).
export function describeCost(p) {
  const bits = [];
  if (p.arrival_to_min > 0) bits.push(`chegadas das ${clock(1080, p.arrival_from_min || 0)} às ${clock(1080, p.arrival_to_min)}`);
  if (p.cost_aware) {
    const inner = [];
    if (p.defer_margin_min > 0) inner.push(`folga de ${p.defer_margin_min} min`);
    if (p.demand_target_kw > 0) inner.push(`meta de demanda de ${fmtNum(p.demand_target_kw, 0)} kW`);
    bits.push(`planejador consciente de custo${inner.length ? ` (${inner.join(', ')})` : ''}`);
  }
  if (p.demand_verde) {
    if (p.demand_offpeak_price > 0) bits.push(`demanda Verde: R$ ${money(p.demand_offpeak_price)}/kW`);
  } else if (p.demand_peak_price > 0 || p.demand_offpeak_price > 0) {
    bits.push(`demanda Azul: R$ ${money(p.demand_peak_price || 0)}/kW na ponta e R$ ${money(p.demand_offpeak_price || 0)}/kW fora dela`);
  }
  if (p.demand_contracted_kw > 0) bits.push(`contrato de ${fmtNum(p.demand_contracted_kw, 0)} kW`);
  if (p.nights_per_month > 0) bits.push(`${p.nights_per_month} noites por mês`);
  return bits;
}
