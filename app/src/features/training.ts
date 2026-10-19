/**
 * Gym-floor arithmetic: what to load next, and what plates make it.
 *
 * Pure functions, kept apart from the screens so they can be tested without
 * a device — a wrong plate count is a wrong lift.
 */

const LB = 453.59237;

/** The smallest sensible jump for a lift, in grams. */
export function increment(category: string | null | undefined, units: 'kg' | 'lb'): number {
  const lower = ['squat', 'hinge', 'lunge'].includes((category ?? '').replace(/\s+/g, '_').toLowerCase());
  if (units === 'lb') return Math.round((lower ? 10 : 5) * LB);
  return lower ? 5000 : 2500;
}

export interface PreviousSet {
  reps: number | null;
  loadGrams: number | null;
  completed: number;
}

export interface Suggestion {
  loadGrams: number;
  reps: number | null;
  /** Why — shown beside the number, because a suggestion with no reason is just a guess. */
  reason: string;
  direction: 'up' | 'same' | 'first';
}

/**
 * Double progression.
 *
 * If every working set last time reached the top of the rep range, add the
 * smallest increment and go back to the bottom of the range. Otherwise keep
 * the load and chase more reps. With no previous session, use the prescribed
 * load if there is one.
 *
 * This is a suggestion, shown and never applied silently: the trainer can see
 * the client and the spreadsheet cannot.
 */
export function suggest(opts: {
  previous: PreviousSet[];
  repsMin?: number | null;
  repsMax?: number | null;
  targetLoadGrams?: number | null;
  category?: string | null;
  units: 'kg' | 'lb';
}): Suggestion | null {
  const done = opts.previous.filter((s) => s.completed === 1 && s.loadGrams && s.loadGrams > 0);
  if (done.length === 0) {
    if (opts.targetLoadGrams) {
      return { loadGrams: opts.targetLoadGrams, reps: opts.repsMin ?? null, reason: 'Prescribed load', direction: 'first' };
    }
    return null;
  }
  const top = Math.max(...done.map((s) => s.loadGrams!));
  const atTop = done.filter((s) => s.loadGrams === top);
  const target = opts.repsMax ?? opts.repsMin ?? null;

  if (target !== null && atTop.every((s) => (s.reps ?? 0) >= target)) {
    return {
      loadGrams: top + increment(opts.category, opts.units),
      reps: opts.repsMin ?? target,
      reason: `Hit ${target} reps on every set last time`,
      direction: 'up',
    };
  }
  const bestReps = Math.max(...atTop.map((s) => s.reps ?? 0));
  return {
    loadGrams: top,
    reps: target !== null ? Math.min(target, bestReps + 1) : bestReps || null,
    reason: target !== null ? `Same load — aim for ${Math.min(target, bestReps + 1)} reps` : 'Same as last time',
    direction: 'same',
  };
}

export interface PlateLoad {
  barGrams: number;
  /** Plates for one side, heaviest first, in display units. */
  perSide: number[];
  /** What cannot be made with the plates available, in grams; 0 when exact. */
  remainderGrams: number;
}

const KG_PLATES = [25, 20, 15, 10, 5, 2.5, 1.25];
const LB_PLATES = [45, 35, 25, 10, 5, 2.5];

/** The plates for one side of the bar, greedily from the heaviest. */
export function plates(totalGrams: number, units: 'kg' | 'lb', barGrams?: number): PlateLoad {
  const bar = barGrams ?? (units === 'kg' ? 20000 : Math.round(45 * LB));
  const unit = units === 'kg' ? 1000 : LB;
  const available = units === 'kg' ? KG_PLATES : LB_PLATES;
  let side = Math.max(0, (totalGrams - bar) / 2 / unit);
  // Loads are stored in whole grams, so a pound load converted back can sit a
  // fraction of a gram under a plate. The tolerance is far below the smallest
  // plate, so it absorbs rounding without ever adding a plate that isn't there.
  const TOLERANCE = 0.01;
  const perSide: number[] = [];
  for (const p of available) {
    while (side + TOLERANCE >= p) {
      perSide.push(p);
      side -= p;
    }
  }
  const remainder = side > TOLERANCE ? Math.round(side * unit * 2) : 0;
  return { barGrams: bar, perSide, remainderGrams: remainder };
}

/**
 * A warm-up ramp to a working weight: empty bar, then roughly 40, 60 and 80
 * percent, rounded to loadable jumps and never repeating a weight.
 */
export function warmups(workingGrams: number, units: 'kg' | 'lb'): { loadGrams: number; reps: number }[] {
  const bar = units === 'kg' ? 20000 : Math.round(45 * LB);
  if (workingGrams <= bar * 1.25) return [];
  const step = units === 'kg' ? 2500 : Math.round(5 * LB);
  const round = (g: number) => Math.max(bar, Math.round(g / step) * step);
  const ramp = [
    { loadGrams: bar, reps: 10 },
    { loadGrams: round(workingGrams * 0.4), reps: 5 },
    { loadGrams: round(workingGrams * 0.6), reps: 3 },
    { loadGrams: round(workingGrams * 0.8), reps: 2 },
  ];
  return ramp.filter((s, i) => s.loadGrams < workingGrams && (i === 0 || s.loadGrams > ramp[i - 1]!.loadGrams));
}

/** Grams in the trainer's unit, as a number for a stepper. */
export function toUnits(grams: number, units: 'kg' | 'lb'): number {
  return units === 'kg' ? grams / 1000 : Math.round((grams / LB) * 10) / 10;
}

export function fromUnits(value: number, units: 'kg' | 'lb'): number {
  return Math.round(units === 'kg' ? value * 1000 : value * LB);
}
