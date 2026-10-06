// fixedAbs formats |v| with `digits` decimals like Go's %.Nf: the exact binary value is
// rounded, and an exact tie goes to the even digit. JS toFixed picks the larger one on
// ties (36.25 -> "36.3", Go "36.2"), so ties are found and settled here; everything else
// is toFixed, which already rounds the exact value.
function fixedAbs(v, digits) {
  const a = Math.abs(v);
  // a*10^d is exactly k+0.5 iff a*2^(d+1) is an odd integer (scaling by 2^n is exact).
  const t = a * 2 ** (digits + 1);
  if (digits > 20 || !Number.isInteger(t) || t % 2 !== 1) return a.toFixed(digits);
  const half = BigInt(t) * 5n ** BigInt(digits); // 2 * a*10^d, odd
  let k = (half - 1n) / 2n;
  if (k % 2n === 1n) k += 1n; // k is odd: the even neighbour is above
  const s = k.toString().padStart(digits + 1, '0');
  return digits === 0 ? s : `${s.slice(0, -digits)}.${s.slice(-digits)}`;
}

export function fmtNum(v, digits = 1) {
  if (typeof v !== 'number' || !Number.isFinite(v)) return '—';
  const fixed = fixedAbs(v, digits);
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
