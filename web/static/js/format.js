export function fmtNum(v, digits = 1) {
  if (typeof v !== 'number' || !Number.isFinite(v)) return '—';
  const fixed = Math.abs(v).toFixed(digits);
  const [int, frac] = fixed.split('.');
  const grouped = int.replace(/\B(?=(\d{3})+(?!\d))/g, '.');
  const out = frac ? `${grouped},${frac}` : grouped;
  return v < 0 && Number(fixed) !== 0 ? '-' + out : out;
}

export const fmtPct = (v) => fmtNum(v, 1) + '%';
export const fmtBRL = (v) => 'R$ ' + fmtNum(v, 0);

// clock returns the wall-clock time (HH:MM) of a scenario minute.
export function clock(startClockMin, minute) {
  const m = (((startClockMin + minute) % 1440) + 1440) % 1440;
  return `${String(Math.floor(m / 60)).padStart(2, '0')}:${String(m % 60).padStart(2, '0')}`;
}

export function duration(min) {
  const m = Math.max(0, Math.round(min));
  if (m < 60) return `${m} min`;
  return `${Math.floor(m / 60)} h ${String(m % 60).padStart(2, '0')} min`;
}
