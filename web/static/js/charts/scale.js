// linear maps [d0, d1] to [r0, r1]; f.invert maps back. A degenerate domain maps to r0.
export function linear(d0, d1, r0, r1) {
  const k = d1 === d0 ? 0 : (r1 - r0) / (d1 - d0);
  const f = (v) => r0 + (v - d0) * k;
  f.invert = (p) => (k === 0 ? d0 : d0 + (p - r0) / k);
  return f;
}

// niceTicks returns round tick values between min and max (about `count` of them).
export function niceTicks(min, max, count = 5) {
  if (!(max > min)) return [min];
  const raw = (max - min) / count;
  const pow = 10 ** Math.floor(Math.log10(raw));
  const f = raw / pow;
  const step = (f < 1.5 ? 1 : f < 3 ? 2 : f < 7 ? 5 : 10) * pow;
  const out = [];
  for (let v = Math.ceil(min / step) * step; v <= max + step * 1e-9; v += step) out.push(+v.toFixed(10));
  return out;
}

export function timeTicks(horizon, stepMin = 60) {
  const out = [];
  for (let m = 0; m <= horizon; m += stepMin) out.push(m);
  return out;
}
