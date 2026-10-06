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
