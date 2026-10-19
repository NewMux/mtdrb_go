/**
 * Content that ships with the app: the exercise library and the starter
 * programmes.
 *
 * The library matches the 66 exercises the server seeds for every new
 * practice, name for name, so a template written against it resolves to real
 * exercise ids on any account — and the demo build seeds the same list.
 *
 * Templates are not stored on the server until a trainer uses one. Copying a
 * template creates an ordinary programme through the same save path as one
 * built by hand, so a template edited in the gallery's next release never
 * rewrites a programme a client is already on.
 */

import type { Hue } from '@/ui/theme';
import type { IconName } from '@/ui/icon';

export const MOVEMENT_PATTERNS = [
  'squat', 'hinge', 'lunge', 'horizontal_push', 'vertical_push',
  'horizontal_pull', 'vertical_pull', 'carry', 'core', 'olympic',
  'conditioning', 'mobility', 'other',
] as const;

export type MovementPattern = (typeof MOVEMENT_PATTERNS)[number];

export const PATTERN_LABEL: Record<string, string> = {
  squat: 'Squat', hinge: 'Hinge', lunge: 'Lunge',
  horizontal_push: 'Horizontal Push', vertical_push: 'Vertical Push',
  horizontal_pull: 'Horizontal Pull', vertical_pull: 'Vertical Pull',
  carry: 'Carry', core: 'Core', olympic: 'Olympic', conditioning: 'Conditioning',
  mobility: 'Mobility', other: 'Other',
};

export const PATTERN_HUE: Record<string, Hue> = {
  squat: 'tint', hinge: 'indigo', lunge: 'cyan', horizontal_push: 'orange', vertical_push: 'pink',
  horizontal_pull: 'teal', vertical_pull: 'green', carry: 'yellow', core: 'purple',
  olympic: 'red', conditioning: 'red', mobility: 'mint', other: 'gray',
};

/** [name, movement pattern, primary muscle, equipment] */
export const EXERCISE_LIBRARY: [string, MovementPattern, string, string][] = [
  ['Back Squat', 'squat', 'Quadriceps', 'Barbell'],
  ['Front Squat', 'squat', 'Quadriceps', 'Barbell'],
  ['Goblet Squat', 'squat', 'Quadriceps', 'Dumbbell'],
  ['Leg Press', 'squat', 'Quadriceps', 'Machine'],
  ['Hack Squat', 'squat', 'Quadriceps', 'Machine'],
  ['Bodyweight Squat', 'squat', 'Quadriceps', 'Bodyweight'],
  ['Conventional Deadlift', 'hinge', 'Hamstrings', 'Barbell'],
  ['Sumo Deadlift', 'hinge', 'Glutes', 'Barbell'],
  ['Romanian Deadlift', 'hinge', 'Hamstrings', 'Barbell'],
  ['Dumbbell Romanian Deadlift', 'hinge', 'Hamstrings', 'Dumbbell'],
  ['Hip Thrust', 'hinge', 'Glutes', 'Barbell'],
  ['Kettlebell Swing', 'hinge', 'Glutes', 'Kettlebell'],
  ['Back Extension', 'hinge', 'Erectors', 'Bodyweight'],
  ['Seated Leg Curl', 'hinge', 'Hamstrings', 'Machine'],
  ['Walking Lunge', 'lunge', 'Quadriceps', 'Dumbbell'],
  ['Reverse Lunge', 'lunge', 'Quadriceps', 'Dumbbell'],
  ['Bulgarian Split Squat', 'lunge', 'Quadriceps', 'Dumbbell'],
  ['Step Up', 'lunge', 'Quadriceps', 'Dumbbell'],
  ['Bench Press', 'horizontal_push', 'Chest', 'Barbell'],
  ['Incline Bench Press', 'horizontal_push', 'Chest', 'Barbell'],
  ['Dumbbell Bench Press', 'horizontal_push', 'Chest', 'Dumbbell'],
  ['Machine Chest Press', 'horizontal_push', 'Chest', 'Machine'],
  ['Cable Fly', 'horizontal_push', 'Chest', 'Cable'],
  ['Push-Up', 'horizontal_push', 'Chest', 'Bodyweight'],
  ['Dip', 'horizontal_push', 'Chest', 'Bodyweight'],
  ['Overhead Press', 'vertical_push', 'Shoulders', 'Barbell'],
  ['Seated Dumbbell Press', 'vertical_push', 'Shoulders', 'Dumbbell'],
  ['Arnold Press', 'vertical_push', 'Shoulders', 'Dumbbell'],
  ['Lateral Raise', 'vertical_push', 'Shoulders', 'Dumbbell'],
  ['Triceps Pushdown', 'vertical_push', 'Triceps', 'Cable'],
  ['Skull Crusher', 'vertical_push', 'Triceps', 'Barbell'],
  ['Barbell Row', 'horizontal_pull', 'Back', 'Barbell'],
  ['Pendlay Row', 'horizontal_pull', 'Back', 'Barbell'],
  ['Single-Arm Dumbbell Row', 'horizontal_pull', 'Back', 'Dumbbell'],
  ['Seated Cable Row', 'horizontal_pull', 'Back', 'Cable'],
  ['Chest-Supported Row', 'horizontal_pull', 'Back', 'Machine'],
  ['Face Pull', 'horizontal_pull', 'Rear Delts', 'Cable'],
  ['Inverted Row', 'horizontal_pull', 'Back', 'Bodyweight'],
  ['Pull-Up', 'vertical_pull', 'Lats', 'Bodyweight'],
  ['Chin-Up', 'vertical_pull', 'Lats', 'Bodyweight'],
  ['Lat Pulldown', 'vertical_pull', 'Lats', 'Cable'],
  ['Straight-Arm Pulldown', 'vertical_pull', 'Lats', 'Cable'],
  ['Barbell Curl', 'vertical_pull', 'Biceps', 'Barbell'],
  ['Dumbbell Curl', 'vertical_pull', 'Biceps', 'Dumbbell'],
  ['Hammer Curl', 'vertical_pull', 'Biceps', 'Dumbbell'],
  ["Farmer's Carry", 'carry', 'Full Body', 'Dumbbell'],
  ['Suitcase Carry', 'carry', 'Obliques', 'Kettlebell'],
  ['Plank', 'core', 'Abdominals', 'Bodyweight'],
  ['Side Plank', 'core', 'Obliques', 'Bodyweight'],
  ['Hanging Leg Raise', 'core', 'Abdominals', 'Bodyweight'],
  ['Cable Crunch', 'core', 'Abdominals', 'Cable'],
  ['Pallof Press', 'core', 'Obliques', 'Cable'],
  ['Dead Bug', 'core', 'Abdominals', 'Bodyweight'],
  ['Power Clean', 'olympic', 'Full Body', 'Barbell'],
  ['Hang Clean', 'olympic', 'Full Body', 'Barbell'],
  ['Push Press', 'olympic', 'Shoulders', 'Barbell'],
  ['Snatch', 'olympic', 'Full Body', 'Barbell'],
  ['Rowing Machine', 'conditioning', 'Full Body', 'Machine'],
  ['Assault Bike', 'conditioning', 'Full Body', 'Machine'],
  ['Treadmill Run', 'conditioning', 'Legs', 'Machine'],
  ['Battle Ropes', 'conditioning', 'Full Body', 'Ropes'],
  ['Box Jump', 'conditioning', 'Legs', 'Plyo Box'],
  ['Couch Stretch', 'mobility', 'Hip Flexors', 'Bodyweight'],
  ['Thoracic Rotation', 'mobility', 'Thoracic Spine', 'Bodyweight'],
  ['90/90 Hip Stretch', 'mobility', 'Hips', 'Bodyweight'],
  ['Banded Shoulder Dislocate', 'mobility', 'Shoulders', 'Band'],
];

/* ───────────────────────────── templates ───────────────────────────── */

export interface TemplateExercise {
  name: string;
  sets: number;
  /** [min, max]; equal for a fixed count. For holds and carries, seconds. */
  reps: [number, number];
  rpe?: number;
  rest?: number;
  notes?: string;
  /** Exercises sharing a letter are performed back to back. */
  superset?: string;
}

export interface TemplateDay {
  name: string;
  exercises: TemplateExercise[];
}

export interface ProgramTemplate {
  key: string;
  name: string;
  summary: string;
  level: 'Beginner' | 'Intermediate' | 'Advanced' | 'All levels';
  goal: 'Strength' | 'Hypertrophy' | 'Fat Loss' | 'General Fitness' | 'Mobility' | 'Return to Training' | 'Power';
  daysPerWeek: number;
  weeks: number;
  hue: Hue;
  icon: IconName;
  days: TemplateDay[];
}

const ex = (
  name: string, sets: number, reps: number | [number, number],
  extra: Omit<TemplateExercise, 'name' | 'sets' | 'reps'> = {},
): TemplateExercise => ({ name, sets, reps: typeof reps === 'number' ? [reps, reps] : reps, ...extra });

export const TEMPLATES: ProgramTemplate[] = [
  {
    key: 'foundations',
    name: 'Foundations Full Body',
    summary: 'Three full-body days that teach the main movement patterns with dumbbells and machines. The first programme for almost every new client.',
    level: 'Beginner', goal: 'General Fitness', daysPerWeek: 3, weeks: 4, hue: 'green', icon: 'sparkles',
    days: [
      { name: 'Day A', exercises: [
        ex('Goblet Squat', 3, [8, 10], { rpe: 7, rest: 90 }),
        ex('Dumbbell Bench Press', 3, [8, 10], { rpe: 7, rest: 90 }),
        ex('Single-Arm Dumbbell Row', 3, [10, 12], { rpe: 7, rest: 60 }),
        ex('Dumbbell Romanian Deadlift', 3, [10, 12], { rpe: 7, rest: 90 }),
        ex('Plank', 3, 30, { rest: 45, notes: 'Seconds' }),
      ] },
      { name: 'Day B', exercises: [
        ex('Leg Press', 3, [10, 12], { rpe: 7, rest: 90 }),
        ex('Push-Up', 3, [6, 12], { rest: 60, notes: 'Elevate hands if needed' }),
        ex('Lat Pulldown', 3, [10, 12], { rpe: 7, rest: 60 }),
        ex('Hip Thrust', 3, [10, 12], { rpe: 7, rest: 90 }),
        ex('Dead Bug', 3, [8, 10], { rest: 45, notes: 'Per side' }),
      ] },
      { name: 'Day C', exercises: [
        ex('Reverse Lunge', 3, [8, 10], { rpe: 7, rest: 60, notes: 'Per leg' }),
        ex('Seated Dumbbell Press', 3, [8, 10], { rpe: 7, rest: 90 }),
        ex('Seated Cable Row', 3, [10, 12], { rpe: 7, rest: 60 }),
        ex('Back Extension', 3, [12, 15], { rest: 60 }),
        ex("Farmer's Carry", 3, 40, { rest: 60, notes: 'Seconds' }),
      ] },
    ],
  },
  {
    key: 'strength-5x5',
    name: 'Strength 5×5',
    summary: 'The classic linear barbell progression. Two alternating days, add load every session the last set is clean.',
    level: 'Intermediate', goal: 'Strength', daysPerWeek: 3, weeks: 8, hue: 'tint', icon: 'strength',
    days: [
      { name: 'Workout A', exercises: [
        ex('Back Squat', 5, 5, { rpe: 8, rest: 180 }),
        ex('Bench Press', 5, 5, { rpe: 8, rest: 180 }),
        ex('Barbell Row', 5, 5, { rpe: 8, rest: 120 }),
      ] },
      { name: 'Workout B', exercises: [
        ex('Back Squat', 5, 5, { rpe: 8, rest: 180 }),
        ex('Overhead Press', 5, 5, { rpe: 8, rest: 180 }),
        ex('Conventional Deadlift', 1, 5, { rpe: 8, rest: 240, notes: 'One top set after warm-ups' }),
      ] },
    ],
  },
  {
    key: 'upper-lower',
    name: 'Upper / Lower Hypertrophy',
    summary: 'Four days, each muscle trained twice a week with a heavy and a volume day. Built for visible muscle gain.',
    level: 'Intermediate', goal: 'Hypertrophy', daysPerWeek: 4, weeks: 6, hue: 'orange', icon: 'flame',
    days: [
      { name: 'Upper — Heavy', exercises: [
        ex('Bench Press', 4, [5, 7], { rpe: 8, rest: 150 }),
        ex('Barbell Row', 4, [6, 8], { rpe: 8, rest: 120 }),
        ex('Seated Dumbbell Press', 3, [8, 10], { rpe: 8, rest: 90 }),
        ex('Pull-Up', 3, [6, 10], { rpe: 8, rest: 90 }),
        ex('Skull Crusher', 3, [10, 12], { rest: 60, superset: 'A' }),
        ex('Barbell Curl', 3, [10, 12], { rest: 60, superset: 'A' }),
      ] },
      { name: 'Lower — Heavy', exercises: [
        ex('Back Squat', 4, [5, 7], { rpe: 8, rest: 180 }),
        ex('Romanian Deadlift', 3, [6, 8], { rpe: 8, rest: 150 }),
        ex('Bulgarian Split Squat', 3, [8, 10], { rpe: 8, rest: 90, notes: 'Per leg' }),
        ex('Seated Leg Curl', 3, [10, 12], { rest: 60 }),
        ex('Hanging Leg Raise', 3, [10, 15], { rest: 60 }),
      ] },
      { name: 'Upper — Volume', exercises: [
        ex('Incline Bench Press', 3, [8, 12], { rpe: 8, rest: 90 }),
        ex('Chest-Supported Row', 3, [10, 12], { rpe: 8, rest: 90 }),
        ex('Cable Fly', 3, [12, 15], { rest: 60 }),
        ex('Lat Pulldown', 3, [10, 12], { rest: 60 }),
        ex('Lateral Raise', 4, [12, 15], { rest: 45, superset: 'A' }),
        ex('Face Pull', 4, [15, 20], { rest: 45, superset: 'A' }),
      ] },
      { name: 'Lower — Volume', exercises: [
        ex('Hack Squat', 3, [10, 12], { rpe: 8, rest: 120 }),
        ex('Hip Thrust', 3, [10, 12], { rpe: 8, rest: 90 }),
        ex('Walking Lunge', 3, [10, 12], { rest: 90, notes: 'Per leg' }),
        ex('Back Extension', 3, [12, 15], { rest: 60 }),
        ex('Cable Crunch', 3, [12, 15], { rest: 60 }),
      ] },
    ],
  },
  {
    key: 'ppl',
    name: 'Push · Pull · Legs',
    summary: 'A three-day split run twice through the week for experienced lifters who can recover from high frequency.',
    level: 'Advanced', goal: 'Hypertrophy', daysPerWeek: 6, weeks: 6, hue: 'purple', icon: 'layers',
    days: [
      { name: 'Push', exercises: [
        ex('Bench Press', 4, [6, 8], { rpe: 8, rest: 150 }),
        ex('Overhead Press', 3, [6, 8], { rpe: 8, rest: 120 }),
        ex('Incline Bench Press', 3, [8, 10], { rest: 90 }),
        ex('Lateral Raise', 4, [12, 15], { rest: 45 }),
        ex('Triceps Pushdown', 3, [10, 12], { rest: 45 }),
      ] },
      { name: 'Pull', exercises: [
        ex('Conventional Deadlift', 3, [4, 6], { rpe: 8, rest: 180 }),
        ex('Pull-Up', 4, [6, 10], { rest: 120 }),
        ex('Pendlay Row', 3, [6, 8], { rest: 120 }),
        ex('Face Pull', 3, [15, 20], { rest: 45 }),
        ex('Hammer Curl', 3, [10, 12], { rest: 45 }),
      ] },
      { name: 'Legs', exercises: [
        ex('Back Squat', 4, [6, 8], { rpe: 8, rest: 180 }),
        ex('Romanian Deadlift', 3, [8, 10], { rest: 120 }),
        ex('Leg Press', 3, [10, 12], { rest: 90 }),
        ex('Seated Leg Curl', 3, [10, 12], { rest: 60 }),
        ex('Hanging Leg Raise', 3, [10, 15], { rest: 60 }),
      ] },
    ],
  },
  {
    key: 'metabolic',
    name: 'Fat-Loss Circuits',
    summary: 'Strength supersets finished with conditioning intervals. Keeps muscle while the scale moves, and keeps sessions moving.',
    level: 'All levels', goal: 'Fat Loss', daysPerWeek: 3, weeks: 6, hue: 'red', icon: 'heartPulse',
    days: [
      { name: 'Circuit 1', exercises: [
        ex('Goblet Squat', 3, 12, { rest: 0, superset: 'A' }),
        ex('Push-Up', 3, [10, 15], { rest: 60, superset: 'A' }),
        ex('Kettlebell Swing', 3, 15, { rest: 0, superset: 'B' }),
        ex('Inverted Row', 3, [8, 12], { rest: 60, superset: 'B' }),
        ex('Assault Bike', 6, 20, { rest: 40, notes: 'Seconds hard, 40s easy' }),
      ] },
      { name: 'Circuit 2', exercises: [
        ex('Reverse Lunge', 3, 10, { rest: 0, superset: 'A', notes: 'Per leg' }),
        ex('Seated Dumbbell Press', 3, 12, { rest: 60, superset: 'A' }),
        ex('Dumbbell Romanian Deadlift', 3, 12, { rest: 0, superset: 'B' }),
        ex('Single-Arm Dumbbell Row', 3, 12, { rest: 60, superset: 'B' }),
        ex('Rowing Machine', 5, 250, { rest: 60, notes: 'Metres' }),
      ] },
      { name: 'Circuit 3', exercises: [
        ex('Step Up', 3, 10, { rest: 0, superset: 'A', notes: 'Per leg' }),
        ex('Dumbbell Bench Press', 3, 12, { rest: 60, superset: 'A' }),
        ex('Box Jump', 4, 6, { rest: 0, superset: 'B' }),
        ex('Battle Ropes', 4, 30, { rest: 45, superset: 'B', notes: 'Seconds' }),
        ex("Farmer's Carry", 3, 45, { rest: 45, notes: 'Seconds' }),
      ] },
    ],
  },
  {
    key: 'glutes',
    name: 'Glute & Lower Focus',
    summary: 'Two lower-body days built around the hip thrust and hinge, plus one full upper day for balance.',
    level: 'Intermediate', goal: 'Hypertrophy', daysPerWeek: 3, weeks: 8, hue: 'pink', icon: 'target',
    days: [
      { name: 'Lower — Glute Strength', exercises: [
        ex('Hip Thrust', 4, [6, 8], { rpe: 8, rest: 120 }),
        ex('Romanian Deadlift', 3, [8, 10], { rpe: 8, rest: 120 }),
        ex('Bulgarian Split Squat', 3, [8, 10], { rest: 90, notes: 'Per leg' }),
        ex('Seated Leg Curl', 3, [10, 12], { rest: 60 }),
      ] },
      { name: 'Upper', exercises: [
        ex('Dumbbell Bench Press', 3, [8, 10], { rest: 90 }),
        ex('Lat Pulldown', 3, [10, 12], { rest: 75 }),
        ex('Seated Dumbbell Press', 3, [8, 10], { rest: 75 }),
        ex('Seated Cable Row', 3, [10, 12], { rest: 75 }),
        ex('Pallof Press', 3, 10, { rest: 45, notes: 'Per side' }),
      ] },
      { name: 'Lower — Glute Volume', exercises: [
        ex('Sumo Deadlift', 3, [6, 8], { rpe: 8, rest: 150 }),
        ex('Walking Lunge', 3, [12, 12], { rest: 90, notes: 'Per leg' }),
        ex('Hip Thrust', 3, [12, 15], { rest: 75, notes: 'Pause at the top' }),
        ex('Back Extension', 3, [15, 20], { rest: 60 }),
      ] },
    ],
  },
  {
    key: 'power',
    name: 'Athletic Power',
    summary: 'Olympic lifts, jumps and heavy compounds for clients who play a sport. Speed first, strength second.',
    level: 'Advanced', goal: 'Power', daysPerWeek: 3, weeks: 6, hue: 'yellow', icon: 'bolt',
    days: [
      { name: 'Lower Power', exercises: [
        ex('Box Jump', 4, 3, { rest: 90, notes: 'Full reset each rep' }),
        ex('Power Clean', 5, 3, { rpe: 7, rest: 150 }),
        ex('Front Squat', 4, [3, 5], { rpe: 8, rest: 180 }),
        ex('Bulgarian Split Squat', 3, 6, { rest: 90, notes: 'Per leg' }),
      ] },
      { name: 'Upper Power', exercises: [
        ex('Push Press', 5, 3, { rpe: 7, rest: 150 }),
        ex('Pull-Up', 4, [5, 8], { rest: 120 }),
        ex('Bench Press', 4, [3, 5], { rpe: 8, rest: 180 }),
        ex('Pendlay Row', 4, 5, { rest: 120 }),
      ] },
      { name: 'Full Body', exercises: [
        ex('Hang Clean', 4, 3, { rpe: 7, rest: 120 }),
        ex('Conventional Deadlift', 3, [3, 5], { rpe: 8, rest: 180 }),
        ex('Kettlebell Swing', 3, 10, { rest: 60 }),
        ex('Suitcase Carry', 3, 30, { rest: 60, notes: 'Seconds per side' }),
      ] },
    ],
  },
  {
    key: 'mobility',
    name: 'Mobility & Core Reset',
    summary: 'Two short sessions for desk-bound clients: open the hips and upper back, build a stable trunk.',
    level: 'All levels', goal: 'Mobility', daysPerWeek: 2, weeks: 4, hue: 'mint', icon: 'run',
    days: [
      { name: 'Hips & Trunk', exercises: [
        ex('90/90 Hip Stretch', 2, 60, { rest: 0, notes: 'Seconds per side' }),
        ex('Couch Stretch', 2, 60, { rest: 0, notes: 'Seconds per side' }),
        ex('Dead Bug', 3, 10, { rest: 30, notes: 'Per side' }),
        ex('Side Plank', 3, 30, { rest: 30, notes: 'Seconds per side' }),
        ex('Goblet Squat', 3, 8, { rest: 60, notes: 'Slow, pause at the bottom' }),
      ] },
      { name: 'Shoulders & Upper Back', exercises: [
        ex('Banded Shoulder Dislocate', 2, 15, { rest: 0 }),
        ex('Thoracic Rotation', 2, 10, { rest: 0, notes: 'Per side' }),
        ex('Face Pull', 3, 15, { rest: 45 }),
        ex('Pallof Press', 3, 10, { rest: 30, notes: 'Per side' }),
        ex('Plank', 3, 40, { rest: 30, notes: 'Seconds' }),
      ] },
    ],
  },
  {
    key: 'return',
    name: 'Return to Training',
    summary: 'Low load, low fatigue, full range. For clients back from injury, illness or a long break — progress by feel.',
    level: 'Beginner', goal: 'Return to Training', daysPerWeek: 2, weeks: 4, hue: 'teal', icon: 'bandage',
    days: [
      { name: 'Session 1', exercises: [
        ex('Bodyweight Squat', 2, [10, 12], { rpe: 5, rest: 60 }),
        ex('Machine Chest Press', 2, [10, 12], { rpe: 5, rest: 60 }),
        ex('Seated Cable Row', 2, [10, 12], { rpe: 5, rest: 60 }),
        ex('Dead Bug', 2, 8, { rest: 45, notes: 'Per side' }),
        ex('Treadmill Run', 1, 10, { notes: 'Minutes, easy walk or jog' }),
      ] },
      { name: 'Session 2', exercises: [
        ex('Step Up', 2, [8, 10], { rpe: 5, rest: 60, notes: 'Per leg' }),
        ex('Lat Pulldown', 2, [10, 12], { rpe: 5, rest: 60 }),
        ex('Back Extension', 2, [10, 12], { rpe: 5, rest: 60 }),
        ex('Side Plank', 2, 20, { rest: 45, notes: 'Seconds per side' }),
        ex('Rowing Machine', 1, 8, { notes: 'Minutes, conversational pace' }),
      ] },
    ],
  },
];
