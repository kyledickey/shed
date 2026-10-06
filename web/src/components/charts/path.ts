// Geometry helpers shared by the chart components.

export type Point = [number, number];

/**
 * monotonePath returns an SVG path through points using monotone cubic
 * interpolation (Fritsch–Carlson), which never overshoots the data.
 */
export function monotonePath(pts: Point[]): string {
  const n = pts.length;
  if (n === 0) return "";
  const first = pts[0]!;
  if (n === 1) return `M${first[0]},${first[1]}`;

  const dx: number[] = [];
  const slope: number[] = [];
  for (let i = 0; i < n - 1; i++) {
    const a = pts[i]!;
    const b = pts[i + 1]!;
    dx.push(b[0] - a[0]);
    slope.push((b[1] - a[1]) / (b[0] - a[0] || 1));
  }
  const tangent: number[] = [slope[0]!];
  for (let i = 1; i < n - 1; i++) {
    const s0 = slope[i - 1]!;
    const s1 = slope[i]!;
    tangent.push(
      s0 * s1 <= 0
        ? 0
        : (3 * (dx[i - 1]! + dx[i]!)) /
            ((2 * dx[i]! + dx[i - 1]!) / s0 + (dx[i]! + 2 * dx[i - 1]!) / s1),
    );
  }
  tangent.push(slope[n - 2]!);

  let d = `M${first[0]},${first[1]}`;
  for (let i = 0; i < n - 1; i++) {
    const a = pts[i]!;
    const b = pts[i + 1]!;
    const h = dx[i]! / 3;
    d += `C${a[0] + h},${a[1] + h * tangent[i]!} ${b[0] - h},${b[1] - h * tangent[i + 1]!} ${b[0]},${b[1]}`;
  }
  return d;
}

/** segments splits a series into runs of non-null points. */
export function segments(values: (number | null)[], toPoint: (v: number, i: number) => Point) {
  const runs: Point[][] = [];
  let run: Point[] = [];
  values.forEach((v, i) => {
    if (v === null) {
      if (run.length) runs.push(run);
      run = [];
    } else {
      run.push(toPoint(v, i));
    }
  });
  if (run.length) runs.push(run);
  return runs;
}

/**
 * niceMax rounds a maximum up to the next of 1, 1.2, 1.5, 2, 2.5, 3, 4, 5, 6
 * or 8 times a power of ten, so the axis hugs the data. With
 * binary, it rounds within the power of 1024 that formatBytes would pick, so
 * byte axes end on round numbers like "50 KB" rather than "46.6 GB".
 */
export function niceMax(v: number, binary = false): number {
  if (v <= 0) return 1;
  if (binary) {
    const unit = 1024 ** Math.floor(Math.log(Math.max(v, 1)) / Math.log(1024));
    return niceMax(v / unit) * unit;
  }
  const pow = 10 ** Math.floor(Math.log10(v));
  const m = v / pow;
  const nice = [1, 1.2, 1.5, 2, 2.5, 3, 4, 5, 6, 8, 10].find((step) => m <= step) ?? 10;
  return nice * pow;
}
