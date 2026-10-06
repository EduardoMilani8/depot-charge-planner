// dotStrip places one dot per seed on a 0..100 strip; neighbours are spread over three
// rows (by position in the list) so equal values do not hide each other.
export function dotStrip(values, width, height, pad = 8) {
  const inner = width - 2 * pad;
  const rows = 3;
  return values.map((v, i) => {
    const clamped = Math.min(100, Math.max(0, v.value));
    return { seed: v.seed, value: v.value, x: pad + (inner * clamped) / 100, y: (height / (rows + 1)) * ((i % rows) + 1) };
  });
}

// Margins shared by every time-based chart, so their time axes line up.
export const MARGIN = { left: 120, right: 16, top: 12, bottom: 28 };

const num = (n) => Math.round(n * 10) / 10;

// runs run-length encodes an array into [{from, to (inclusive), value}].
export function runs(arr) {
  const out = [];
  arr.forEach((value, i) => {
    const last = out[out.length - 1];
    if (last && last.value === value) last.to = i;
    else out.push({ from: i, to: i, value });
  });
  return out;
}

// stackLayers stacks series (missing values count as 0): [{lower, upper}] per series.
export function stackLayers(series) {
  if (series.length === 0) return [];
  let base = new Array(series[0].length).fill(0);
  return series.map((s) => {
    const upper = base.map((b, i) => b + (Number.isFinite(s[i]) ? s[i] : 0));
    const layer = { lower: base, upper };
    base = upper;
    return layer;
  });
}

export function areaPath(lower, upper, x, y) {
  if (upper.length === 0) return '';
  let d = `M${num(x(0))},${num(y(upper[0]))}`;
  for (let i = 1; i < upper.length; i++) d += `L${num(x(i))},${num(y(upper[i]))}`;
  for (let i = lower.length - 1; i >= 0; i--) d += `L${num(x(i))},${num(y(lower[i]))}`;
  return d + 'Z';
}

// linePath draws values[i] at x(i); `step` holds the previous value until the next
// minute (for a limit that changes at an instant); a missing value breaks the line.
export function linePath(values, x, y, step = false) {
  let d = '';
  let pen = false;
  let prevY = 0;
  values.forEach((v, i) => {
    if (!Number.isFinite(v)) { pen = false; return; }
    const px = num(x(i));
    const py = num(y(v));
    if (!pen) { d += `M${px},${py}`; pen = true; }
    else if (step) d += `L${px},${prevY}L${px},${py}`;
    else d += `L${px},${py}`;
    prevY = py;
  });
  return d;
}

// assignLanes gives each {from, to} interval a lane so overlapping ones do not share it.
export function assignLanes(items) {
  const laneEnd = [];
  return items.map((it) => {
    let lane = laneEnd.findIndex((end) => end < it.from);
    if (lane < 0) { lane = laneEnd.length; laneEnd.push(it.to); } else laneEnd[lane] = it.to;
    return lane;
  });
}

// valuesAt reads the run-level series at a minute (clamped); missing values are null.
export function valuesAt(run, minute) {
  const sr = run.series;
  const i = Math.min(Math.max(0, Math.round(minute)), sr.limit_kw.length - 1);
  const at = (a) => (a[i] === undefined ? null : a[i]);
  return { physical: at(sr.physical_kw), commanded: at(sr.commanded_kw), limit: at(sr.limit_kw), layer: sr.layer[i] };
}
