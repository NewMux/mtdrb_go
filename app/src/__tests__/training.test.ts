/**
 * Gym-floor arithmetic. A wrong plate count is a wrong lift, and a silent
 * jump in load is an injury, so these are pinned down exactly.
 */

import { fromUnits, increment, plates, suggest, toUnits, warmups } from '@/features/training';

describe('suggest (double progression)', () => {
  const set = (loadKg: number, reps: number, completed = 1) => ({ loadGrams: loadKg * 1000, reps, completed });

  it('adds the smallest jump when every top set hit the top of the range', () => {
    const tip = suggest({ previous: [set(80, 8), set(80, 8), set(80, 8)], repsMin: 6, repsMax: 8, category: 'horizontal_push', units: 'kg' });
    expect(tip).toMatchObject({ loadGrams: 82_500, reps: 6, direction: 'up' });
  });

  it('jumps twice as far on lower-body lifts', () => {
    const tip = suggest({ previous: [set(100, 5), set(100, 5)], repsMin: 5, repsMax: 5, category: 'squat', units: 'kg' });
    expect(tip?.loadGrams).toBe(105_000);
  });

  it('holds the load and chases a rep when any set fell short', () => {
    const tip = suggest({ previous: [set(80, 8), set(80, 7), set(80, 6)], repsMin: 6, repsMax: 8, category: 'squat', units: 'kg' });
    expect(tip).toMatchObject({ loadGrams: 80_000, reps: 8, direction: 'same' });
  });

  it('ignores sets that were cloned but never performed', () => {
    const tip = suggest({ previous: [set(80, 8, 0), set(70, 8)], repsMin: 6, repsMax: 8, category: 'squat', units: 'kg' });
    expect(tip?.loadGrams).toBe(75_000);
  });

  it('falls back to the prescribed load on a first session, and to nothing without one', () => {
    expect(suggest({ previous: [], targetLoadGrams: 40_000, repsMin: 10, units: 'kg' })).toMatchObject({ loadGrams: 40_000, direction: 'first' });
    expect(suggest({ previous: [], units: 'kg' })).toBeNull();
  });

  it('uses pound plates in pounds', () => {
    expect(increment('squat', 'lb')).toBe(Math.round(10 * 453.59237));
    expect(increment('core', 'lb')).toBe(Math.round(5 * 453.59237));
  });
});

describe('plates', () => {
  it('loads 100 kg as 25 + 15 a side on a 20 kg bar', () => {
    expect(plates(100_000, 'kg')).toEqual({ barGrams: 20_000, perSide: [25, 15], remainderGrams: 0 });
  });

  it('uses change plates and reports what cannot be made', () => {
    expect(plates(62_500, 'kg').perSide).toEqual([20, 1.25]);
    expect(plates(61_000, 'kg').remainderGrams).toBe(1000);
  });

  it('is just the bar at or under bar weight', () => {
    expect(plates(20_000, 'kg').perSide).toEqual([]);
    expect(plates(10_000, 'kg').perSide).toEqual([]);
  });

  it('loads 225 lb as two 45s a side', () => {
    expect(plates(fromUnits(225, 'lb'), 'lb').perSide).toEqual([45, 45]);
  });
});

describe('warmups', () => {
  it('ramps from the empty bar without repeating or reaching the working weight', () => {
    const ramp = warmups(100_000, 'kg');
    expect(ramp[0]).toEqual({ loadGrams: 20_000, reps: 10 });
    const loads = ramp.map((r) => r.loadGrams);
    expect(new Set(loads).size).toBe(loads.length);
    expect(Math.max(...loads)).toBeLessThan(100_000);
    expect(loads).toEqual([...loads].sort((a, b) => a - b));
  });

  it('skips warm-ups for something barely heavier than the bar', () => {
    expect(warmups(22_500, 'kg')).toEqual([]);
  });
});

describe('unit conversion', () => {
  it('round-trips kilograms exactly', () => {
    expect(fromUnits(toUnits(82_500, 'kg'), 'kg')).toBe(82_500);
  });
  it('round-trips pounds to the gram', () => {
    expect(Math.abs(fromUnits(toUnits(fromUnits(185, 'lb'), 'lb'), 'lb') - fromUnits(185, 'lb'))).toBeLessThanOrEqual(50);
  });
});
