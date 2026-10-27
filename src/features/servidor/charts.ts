/* Trazados SVG de las gráficas pequeñas, sin librerías. */
export function sparkPath(arr: number[], max: number, w: number, h: number, area?: boolean) {
  const n = arr.length;
  if (n < 2) return '';
  let d = '';
  for (let i = 0; i < n; i++) {
    const x = (i / (n - 1)) * w, y = h - (Math.min(arr[i], max) / max) * (h - 2) - 1;
    d += (i ? 'L' : 'M') + x.toFixed(1) + ' ' + y.toFixed(1);
  }
  if (area) d += 'L' + w + ' ' + h + 'L0 ' + h + 'Z';
  return d;
}

export function peak(v: number[], t: string[]) {
  let mi = 0;
  for (let i = 0; i < v.length; i++) if (v[i] > v[mi]) mi = i;
  return { v: v[mi] ?? 0, t: t[mi] };
}
