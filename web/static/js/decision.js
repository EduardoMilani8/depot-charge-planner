import { clock, duration, fmtNum } from './format.js';

// decisionAt finds the last decision taken at or before `minute`, its index and the
// minutes it stayed in force (until the next decision, or the horizon).
export function decisionAt(decisions, minute, horizon) {
  let lo = 0;
  let hi = decisions.length - 1;
  let ans = -1;
  while (lo <= hi) {
    const mid = (lo + hi) >> 1;
    if (decisions[mid].minute <= minute) { ans = mid; lo = mid + 1; } else hi = mid - 1;
  }
  if (ans < 0) return null;
  const next = decisions[ans + 1];
  return { decision: decisions[ans], index: ans, from: decisions[ans].minute, to: next ? next.minute - 1 : horizon };
}

// statusesAt rebuilds the bus statuses the planner produced in decision k. The API sends
// only the entries that changed (`buses`) and the ids that left the list (`gone`), so the
// result is accumulated from decision 0 through k. Sorted by bus id; `sf` absent reads 0.
export function statusesAt(decisions, k) {
  const listed = new Map();
  const last = Math.min(k, decisions.length - 1);
  for (let i = 0; i <= last; i++) {
    const d = decisions[i];
    for (const b of d.buses ?? []) listed.set(b.b, { ...b, sf: b.sf ?? 0 });
    for (const id of d.gone ?? []) listed.delete(id);
  }
  return [...listed.values()].sort((a, b) => (a.b < b.b ? -1 : a.b > b.b ? 1 : 0));
}

// decisionNotes resolves a decision's note indices through the run's `notes` table.
export function decisionNotes(data, decision) {
  const table = data.notes ?? [];
  return (decision.notes ?? []).map((i) => table[i]).filter((t) => t !== undefined);
}

// swapMarkers lists, per recommended swap, one marker for the bus that takes the charger
// and one for the bus that gives it up.
export function swapMarkers(decisions) {
  const out = [];
  for (const d of decisions) {
    for (const sw of d.swaps ?? []) {
      out.push({ minute: d.minute, bus: sw.in, role: 'in', charger: sw.charger, reason: sw.reason },
        { minute: d.minute, bus: sw.out, role: 'out', charger: sw.charger, reason: sw.reason });
    }
  }
  return out;
}

// chargerOccupancy gives, per charger, the bus plugged in at each minute ('' = none).
export function chargerOccupancy(data) {
  const n = data.series.limit_kw.length;
  const occ = data.series.chargers.map(() => new Array(n).fill(''));
  for (const b of data.series.buses) {
    b.charger.forEach((ci, i) => { if (ci >= 0 && occ[ci]) occ[ci][i] = b.id; });
  }
  return occ;
}

// busExposure counts the minutes a bus spent in each situation while it was present.
export function busExposure(data, busIdx) {
  const b = data.series.buses[busIdx];
  const e = { waiting: 0, charging: 0, onFaulted: 0, onOffline: 0, noReading: 0, present: 0 };
  b.state.forEach((st, i) => {
    if (st !== 1) return;
    e.present++;
    if (b.observed_kwh[i] === null || b.observed_kwh[i] === undefined) e.noReading++;
    const ci = b.charger[i];
    if (ci < 0) { e.waiting++; return; }
    const c = data.series.chargers[ci];
    if (c.status[i] === 1) e.onFaulted++;
    else if (c.status[i] === 2) e.onOffline++;
    if (c.physical_kw[i] > 0) e.charging++;
  });
  return e;
}

// Thresholds of the cause analysis in explainBus.
const REACHED_TOL_KWH = 1; // outcomes are rounded to 0.1 kWh; within 1 kWh of the forecast counts as reached
const MIN_CAUSE_MIN = 10; // minutes on a failed charger or waiting before it is worth naming (or half of a short stay)
const READING_ERR_KWH = 5; // mean |reading - truth| (kWh) from which the readings are suspect
const READING_ERR_SHARE = 0.5; // ...and it must be at least this share of the shortfall to explain it

// readingError is the mean |observed - true SoC| (kWh) over the minutes the bus was in the
// depot with a reading; null when the run has no true SoC series or no reading at all.
export function readingError(data, busIdx) {
  const b = data.series.buses[busIdx];
  if (!b.true_soc_kwh || !b.observed_kwh) return null;
  let sum = 0;
  let n = 0;
  b.state.forEach((st, i) => {
    const obs = b.observed_kwh[i];
    const truth = b.true_soc_kwh[i];
    if (st !== 1 || obs === null || obs === undefined || truth === null || truth === undefined) return;
    sum += Math.abs(obs - truth);
    n++;
  });
  return n > 0 ? sum / n : null;
}

// explainBus answers "why did this bus (not) leave ready?" from the run's truth. The causes
// the planner could not see (real consumption above the forecast, a stay too short for any
// charger, wrong readings) come before the ones it could act on (failed charger, queue,
// power); the first one named is the primary.
export function explainBus(data, busIdx) {
  const b = data.series.buses[busIdx];
  const o = data.outcomes.find((x) => x.id === b.id);
  const start = data.scenario.start_clock_min;
  const out = [];
  if (!o) return out;
  if (!o.departed) {
    out.push(`Não saiu dentro do horizonte da simulação (chegada às ${clock(start, o.arrival)}).`);
    return out;
  }
  const have = `${fmtNum(o.final_soc_kwh, 0)} kWh de ${fmtNum(o.true_target_kwh, 0)} kWh necessários`;
  out.push(o.ready
    ? `Saiu pronto às ${clock(start, o.departure)}: ${have}.`
    : `Saiu sem a carga às ${clock(start, o.departure)}: ${have} (faltaram ${fmtNum(o.shortfall_kwh, 0)} kWh).`);
  const overForecast = o.true_target_kwh > o.forecast_target_kwh + 0.5;
  if (overForecast) {
    out.push(`O consumo real passou do previsto: o planejador contava com ${fmtNum(o.forecast_target_kwh, 0)} kWh e a rota exigiu ${fmtNum(o.true_target_kwh, 0)} kWh.`);
  }
  const e = busExposure(data, busIdx);
  const bits = [`carregando ${duration(e.charging)}`];
  if (e.waiting) bits.push(`esperando carregador ${duration(e.waiting)}`);
  if (e.onFaulted) bits.push(`em carregador com falha ${duration(e.onFaulted)}`);
  if (e.onOffline) bits.push(`em carregador sem comunicação ${duration(e.onOffline)}`);
  if (e.noReading) bits.push(`sem leitura de carga ${duration(e.noReading)}`);
  out.push(`No pátio por ${duration(e.present)}: ${bits.join(', ')}.`);
  if (o.ready) return out;

  const causes = [];
  // (b) Not enough time: even at the fastest charger the stay could not deliver the energy.
  const maxKw = Math.max(0, ...(data.scenario.chargers ?? []).map((c) => c.max_kw));
  const dwell = o.departure - o.arrival;
  const needKwh = o.true_target_kwh - o.initial_soc_kwh;
  const capKwh = (dwell / 60) * maxKw;
  const impossible = maxKw > 0 && needKwh > capKwh + 0.5
    ? `fisicamente impossível: em ${duration(dwell)} de pátio, um carregador de ${fmtNum(maxKw, 0)} kW entrega no máximo ${fmtNum(capKwh, 0)} kWh e eram necessários ${fmtNum(needKwh, 0)} kWh`
    : null;
  // (a) The bus got what the planner aimed for: the miss is on the forecast, not on the charging.
  if (overForecast && o.final_soc_kwh >= o.forecast_target_kwh - REACHED_TOL_KWH) {
    causes.push('o consumo real passou do previsto (a previsão não tinha como saber)');
    if (impossible) causes.push(impossible);
  } else {
    if (impossible) causes.push(impossible);
    // (c) Wrong or late readings over the stay.
    const err = readingError(data, busIdx);
    if (err !== null && err >= READING_ERR_KWH && err >= READING_ERR_SHARE * o.shortfall_kwh) {
      causes.push(`leituras de carga imprecisas ou atrasadas (erro médio de ${fmtNum(err, 0)} kWh entre a leitura e a carga real)`);
    }
    // (d) What the planner could act on.
    const notable = (min) => min > 0 && (min >= MIN_CAUSE_MIN || min >= e.present / 2);
    if (notable(e.onFaulted + e.onOffline)) causes.push('ficou em carregador com falha ou sem comunicação');
    if (notable(e.waiting)) causes.push('esperou por um carregador livre');
    if (causes.length === 0) {
      causes.push('teve carregador o tempo todo; a potência disponível (limite da garagem ou prioridade dada a outros ônibus) não bastou');
    }
  }
  out.push(`Possível causa: ${causes[0]}.` + (causes.length > 1 ? ` Também pesou: ${causes.slice(1).join('; ')}.` : ''));
  return out;
}

// createSelection holds the selected bus id (or null) and notifies on change.
export function createSelection() {
  let id = null;
  const subs = new Set();
  return {
    get: () => id,
    set(v) {
      if (v === id) return;
      id = v;
      subs.forEach((fn) => fn(v));
    },
    onChange(fn) { subs.add(fn); return () => subs.delete(fn); },
  };
}
