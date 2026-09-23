/**
 * The demo build's server: a recording of the real one.
 *
 * `cmd/demo` runs a practice through the real API against a throwaway
 * database and records what a device would receive — its first sync, and the
 * answers to the reads the app makes. Replaying that gives the demo real
 * ledger output: the dashboard's earnings, the receivables ageing and every
 * credit balance come from the same code a trainer's account runs, and none
 * of it is re-implemented here.
 *
 * The device is seeded the way a real one is: the sync engine pulls, and this
 * answers. Writes are refused with `demo_read_only`, which the screens that
 * write online explain in plain words; the offline-first writes still land
 * locally and queue, exactly as on a phone with no signal.
 */

import { ApiError, ApiClient, DEMO_READ_ONLY, type RequestOptions, type TokenStore } from '@/api/client';
import type { Account, PullResult } from '@/api/types';
import type { Database } from '@/db/types';
import { pull, resetCursor } from '@/sync/engine';

export interface Recording {
  /** The scenario's "now". */
  recorded_at: string;
  timezone: string;
  /** The studio's offset from UTC, in minutes. */
  utc_offset_minutes: number;
  account: Account;
  pull: { collection: string; rows: Record<string, unknown>[] }[];
  /** Server answers, keyed by path without its query. */
  reads: Record<string, unknown>;
}

export interface Shift {
  /** Whole days added to every date. */
  days: number;
  /** Milliseconds added to every instant. */
  ms: number;
}

const DAY_MS = 86_400_000;

/**
 * How far to move the recording so its "today" is the viewer's today.
 *
 * Whole days, so the recorded day — with a session already done and the rest
 * still ahead — is the day the viewer opens the demo on, whatever that is.
 * Weekdays move with it, which nobody can tell; a demo opened on a Friday
 * showing the Wednesday it was recorded on, with that day's sessions long
 * past and unmarked, anyone would.
 *
 * Instants also absorb the difference between the studio's offset and the
 * viewer's, so the 07:00 session reads 07:00 on any clock. A Dubai studio
 * seen from London at 03:00 would look like a practice run by insomniacs.
 */
export function shiftFor(recording: Pick<Recording, 'recorded_at' | 'utc_offset_minutes'>, now: Date): Shift {
  const recorded = new Date(Date.parse(recording.recorded_at) + recording.utc_offset_minutes * 60_000);
  const recordedDay = Date.UTC(recorded.getUTCFullYear(), recorded.getUTCMonth(), recorded.getUTCDate());
  const today = Date.UTC(now.getFullYear(), now.getMonth(), now.getDate());
  const days = Math.round((today - recordedDay) / DAY_MS);
  const viewerOffset = -now.getTimezoneOffset();
  return { days, ms: days * DAY_MS + (recording.utc_offset_minutes - viewerOffset) * 60_000 };
}

const DATE = /^\d{4}-\d{2}-\d{2}$/;
const INSTANT = /^\d{4}-\d{2}-\d{2}T\d{2}:\d{2}(:\d{2}(\.\d+)?)?(Z|[+-]\d{2}:?\d{2})$/;
/** A date the server encoded as midnight UTC: still a date, not an instant. */
const MIDNIGHT_UTC = /T00:00(:00(\.0+)?)?(Z|\+00:?00)$/;

function shiftString(value: string, shift: Shift): string {
  if (DATE.test(value)) {
    return new Date(Date.parse(`${value}T00:00:00Z`) + shift.days * DAY_MS).toISOString().slice(0, 10);
  }
  if (INSTANT.test(value)) {
    const offset = MIDNIGHT_UTC.test(value) ? shift.days * DAY_MS : shift.ms;
    return new Date(Date.parse(value) + offset).toISOString();
  }
  return value;
}

/** Moves every date and instant in a value, however deep. */
export function shiftDeep<T>(value: T, shift: Shift): T {
  if (typeof value === 'string') return shiftString(value, shift) as T;
  if (Array.isArray(value)) return value.map((v) => shiftDeep(v, shift)) as T;
  if (value && typeof value === 'object') {
    const out: Record<string, unknown> = {};
    for (const [key, v] of Object.entries(value as Record<string, unknown>)) out[key] = shiftDeep(v, shift);
    return out as T;
  }
  return value;
}

/** Holds no tokens: nothing is ever sent. */
const noTokens: TokenStore = {
  accessToken: async () => null,
  refreshToken: async () => null,
  save: async () => {},
  clear: async () => {},
};

/** The cursor the one recorded page ends at. */
const DEMO_CURSOR = 'demo';

/** Answers the app's requests from a recording. */
export class DemoApi extends ApiClient {
  constructor(
    private readonly recording: Recording,
    private readonly now: () => Date = () => new Date(),
  ) {
    super({ baseUrl: 'demo:', tokens: noTokens });
  }

  override async request<T>(path: string, opts: RequestOptions = {}): Promise<T> {
    if ((opts.method ?? 'GET') !== 'GET') {
      throw new ApiError(403, DEMO_READ_ONLY, 'The demo has no server to send this to.');
    }
    const [route = path, query = ''] = path.split('?');
    const now = this.now();
    const shift = shiftFor(this.recording, now);

    if (route === '/v1/sync/pull') {
      const done = new URLSearchParams(query).get('cursor') === DEMO_CURSOR;
      const result: PullResult = {
        cursor: DEMO_CURSOR,
        changes: done ? [] : shiftDeep(this.recording.pull, shift),
        has_more: false,
        server_time: now.toISOString(),
      };
      return result as T;
    }

    if (!(route in this.recording.reads)) {
      throw new ApiError(404, 'demo_not_recorded', `The demo recording has no answer for ${route}.`);
    }
    return shiftDeep(this.recording.reads[route], shift) as T;
  }
}

/**
 * Seeds the device from the recording, through the real sync engine.
 *
 * Every launch, not only the first: the shift is relative to today, so a
 * device seeded yesterday is a day behind. Rows are keyed by the recorded
 * ids, so pulling again overwrites them in place.
 */
export async function replay(db: Database, api: DemoApi): Promise<void> {
  await resetCursor(db);
  await pull(db, api);
}
