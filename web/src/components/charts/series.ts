import type { Tone } from "../tone";

/** Series is one line on a time-series chart, sampled every `step` seconds. */
export type Series = {
  key: string;
  label: string;
  tone: Tone;
  values: (number | null)[];
};

/**
 * chartColors maps tones to hex for ECharts, which paints on a canvas and
 * can't read CSS variables. They follow Kumo's chart palette; accent is
 * shed's green.
 */
export const chartColors: Record<Tone, string> = {
  accent: "#2EA45B",
  grass: "#2EA45B",
  sky: "#4290F0",
  sunflower: "#F5B647",
  bubblegum: "#E8649D",
  grape: "#8D58EE",
  teal: "#50C3B6",
  tangerine: "#D37536",
  tomato: "#FC574A",
  neutral: "#878787",
};

/** areaFill is a series' fill under its line: its color fading from 40% at the line to clear at the base, as Kumo's gradient. */
export function areaFill(color: string) {
  const alpha = (a: number) =>
    color +
    Math.round(a * 255)
      .toString(16)
      .padStart(2, "0");
  return {
    color: {
      type: "linear" as const,
      x: 0,
      y: 0,
      x2: 0,
      y2: 1,
      colorStops: [
        { offset: 0, color: alpha(0.4) },
        { offset: 1, color: alpha(0) },
      ],
    },
  };
}

/** toPoints pairs samples with their timestamps, skipping gaps. */
export function toPoints(values: (number | null)[], start: string, step: number) {
  const t0 = Date.parse(start);
  const points: [number, number][] = [];
  values.forEach((v, i) => {
    if (v !== null) points.push([t0 + i * step * 1000, v]);
  });
  return points;
}

const clock = new Intl.DateTimeFormat(undefined, { hour: "2-digit", minute: "2-digit" });
const day = new Intl.DateTimeFormat(undefined, { month: "short", day: "numeric" });

/** axisTime labels a time axis tick: the clock within a day, the date across days. */
export function axisTime(spanSeconds: number) {
  const fmt = spanSeconds > 2 * 86400 ? day : clock;
  return (ms: number) => fmt.format(ms);
}
