/**
 * Chart geometry: scales, ticks and paths.
 *
 * Pure and platform-free, so what a chart draws is tested in Node rather than
 * eyeballed. The components in ./index.tsx only turn these numbers into SVG.
 */

/** Maps a domain onto pixels. */
export function linearScale(domain: [number, number], range: [number, number]) {
  const [d0, d1] = domain;
  const [r0, r1] = range;
  const span = d1 - d0 || 1;
  return (value: number) => r0 + ((value - d0) / span) * (r1 - r0);
}

/**
 * Round tick values covering [min, max] — 0, 250, 500 rather than 0, 237,
 * 474. Always includes zero when the data does not go negative, because a bar
 * or area that does not start at zero misstates its size.
 */
export function niceTicks(min: number, max: number, target = 4): number[] {
  let lo = Math.min(0, min);
  let hi = Math.max(0, max);
  if (lo === hi) hi = lo + 1;
  const rough = (hi - lo) / Math.max(1, target);
  const magnitude = 10 ** Math.floor(Math.log10(rough));
  const residual = rough / magnitude;
  const step = (residual >= 7 ? 10 : residual >= 3 ? 5 : residual >= 1.5 ? 2 : 1) * magnitude;
  lo = Math.floor(lo / step) * step;
  hi = Math.ceil(hi / step) * step;
  const ticks: number[] = [];
  for (let v = lo; v <= hi + step / 2; v += step) ticks.push(Number(v.toPrecision(12)));
  return ticks;
}

export interface Point { x: number; y: number }

/** A polyline through the points; gaps (null values) break the line. */
export function linePath(points: (Point | null)[]): string {
  let d = '';
  let pen = false;
  for (const p of points) {
    if (!p) { pen = false; continue; }
    d += `${pen ? 'L' : 'M'}${round(p.x)},${round(p.y)}`;
    pen = true;
  }
  return d;
}

/** The area under a line down to the baseline, for each unbroken run. */
export function areaPath(points: (Point | null)[], baselineY: number): string {
  const runs: Point[][] = [];
  let current: Point[] = [];
  for (const p of points) {
    if (p) current.push(p);
    else if (current.length) { runs.push(current); current = []; }
  }
  if (current.length) runs.push(current);
  return runs
    .map((run) => {
      const first = run[0]!;
      const last = run[run.length - 1]!;
      return `M${round(first.x)},${round(baselineY)}${run.map((p) => `L${round(p.x)},${round(p.y)}`).join('')}L${round(last.x)},${round(baselineY)}Z`;
    })
    .join('');
}

/**
 * A bar with its data end rounded and its baseline end square: the rounding
 * marks where the value is, the flat end marks where it is measured from.
 */
export function barPath(x: number, width: number, baselineY: number, valueY: number, radius = 4): string {
  const up = valueY <= baselineY;
  const h = Math.abs(baselineY - valueY);
  const r = Math.max(0, Math.min(radius, width / 2, h));
  if (h === 0) return '';
  if (up) {
    const top = valueY;
    return `M${round(x)},${round(baselineY)}V${round(top + r)}Q${round(x)},${round(top)} ${round(x + r)},${round(top)}H${round(x + width - r)}Q${round(x + width)},${round(top)} ${round(x + width)},${round(top + r)}V${round(baselineY)}Z`;
  }
  const bottom = valueY;
  return `M${round(x)},${round(baselineY)}V${round(bottom - r)}Q${round(x)},${round(bottom)} ${round(x + r)},${round(bottom)}H${round(x + width - r)}Q${round(x + width)},${round(bottom)} ${round(x + width)},${round(bottom - r)}V${round(baselineY)}Z`;
}

/** An annular segment, for the donut. Angles in radians, 0 at twelve o'clock. */
export function arcPath(cx: number, cy: number, outer: number, inner: number, start: number, end: number): string {
  const sweep = end - start;
  if (sweep >= Math.PI * 2 - 1e-6) {
    // A full ring cannot be drawn as one arc; two halves make it.
    return arcPath(cx, cy, outer, inner, start, start + Math.PI) + arcPath(cx, cy, outer, inner, start + Math.PI, end);
  }
  const large = sweep > Math.PI ? 1 : 0;
  const pt = (r: number, a: number) => `${round(cx + r * Math.sin(a))},${round(cy - r * Math.cos(a))}`;
  return `M${pt(outer, start)}A${outer},${outer} 0 ${large} 1 ${pt(outer, end)}L${pt(inner, end)}A${inner},${inner} 0 ${large} 0 ${pt(inner, start)}Z`;
}

/** Evenly spaced band centres across a width, for categories on the x axis. */
export function bands(count: number, width: number, padding = 0.2) {
  const step = count > 0 ? width / count : width;
  const inner = step * (1 - padding);
  return {
    step,
    inner,
    start: (i: number) => i * step + (step - inner) / 2,
    centre: (i: number) => i * step + step / 2,
  };
}

/**
 * Which of `count` evenly spaced labels to draw so they do not collide: every
 * label if there is room, else every nth, always keeping the last.
 */
export function thinLabels(count: number, width: number, minGap = 56): number[] {
  if (count <= 0) return [];
  const every = Math.max(1, Math.ceil((count * minGap) / Math.max(1, width)));
  const keep: number[] = [];
  for (let i = 0; i < count; i += every) keep.push(i);
  if (keep[keep.length - 1] !== count - 1) {
    if (keep.length > 1 && count - 1 - keep[keep.length - 1]! < every / 2) keep.pop();
    keep.push(count - 1);
  }
  return keep;
}

function round(n: number): number {
  return Math.round(n * 10) / 10;
}
