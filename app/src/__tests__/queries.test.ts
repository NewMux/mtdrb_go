import { MemoryDatabase } from './support';
import {
  todaysRoster, listClients, previousSets, outstandingInvoices,
  biometricHistory, clientDetail, openWorkout, recentWorkouts, lowBalanceClients,
} from '@/features/queries';

async function seed(db: MemoryDatabase) {
  await db.execute(`INSERT INTO clients (id, full_name, status, notes) VALUES
    ('c1','Dana Rivers','active',''), ('c2','Morgan Hale','active','')`);
  await db.execute(`INSERT INTO packages (id, client_id, credits_total, credits_remaining, status)
    VALUES ('p1','c1',10,7,'active')`);
  await db.execute(`INSERT INTO session_types (id, name, duration_minutes, capacity, credit_cost)
    VALUES ('st1','1-on-1',60,1,1)`);
}

describe('local queries', () => {
  let db: MemoryDatabase;
  beforeEach(async () => { db = new MemoryDatabase(); await seed(db); });
  afterEach(() => { db.close(); });

  it("builds the day's roster in time order", async () => {
    const day = new Date(2026, 4, 1);
    const at = (h: number) => new Date(2026, 4, 1, h).toISOString();

    await db.execute(
      `INSERT INTO sessions (id, session_type_id, starts_at, ends_at, status) VALUES
       ('s2','st1',?,?,'scheduled'), ('s1','st1',?,?,'scheduled')`,
      [at(11), at(12), at(9), at(10)],
    );
    await db.execute(`INSERT INTO session_attendees (id, session_id, client_id, status) VALUES
      ('a2','s2','c2','scheduled'), ('a1','s1','c1','scheduled')`);

    const roster = await todaysRoster(db, day);
    expect(roster.map((r) => r.clientName)).toEqual(['Dana Rivers', 'Morgan Hale']);
    // The balance comes from the local mirror so the floor screen can show it
    // without a round trip.
    expect(roster[0]?.creditsRemaining).toBe(7);
  });

  it('leaves cancelled sessions off the roster', async () => {
    const at = (h: number) => new Date(2026, 4, 1, h).toISOString();
    await db.execute(
      `INSERT INTO sessions (id, session_type_id, starts_at, ends_at, status)
       VALUES ('s1','st1',?,?,'cancelled')`, [at(9), at(10)],
    );
    await db.execute(`INSERT INTO session_attendees (id, session_id, client_id, status)
      VALUES ('a1','s1','c1','scheduled')`);

    expect(await todaysRoster(db, new Date(2026, 4, 1))).toHaveLength(0);
  });

  it('searches clients by name, email and phone', async () => {
    await db.execute(`UPDATE clients SET email='dana@example.com', phone='+49301234' WHERE id='c1'`);
    expect(await listClients(db, 'riv')).toHaveLength(1);
    expect(await listClients(db, 'example.com')).toHaveLength(1);
    expect(await listClients(db, '3012')).toHaveLength(1);
    expect(await listClients(db, '')).toHaveLength(2);
  });

  it('hides archived clients', async () => {
    await db.execute(`UPDATE clients SET deleted_at='2026-05-01' WHERE id='c2'`);
    expect(await listClients(db)).toHaveLength(1);
  });

  it('prefills from the last working sets, never from a warm-up', async () => {
    await db.execute(`INSERT INTO exercises (id, name) VALUES ('e1','Back Squat')`);
    await db.execute(`INSERT INTO workout_sessions (id, client_id, performed_on, status)
      VALUES ('w1','c1','2026-04-24','completed'), ('w2','c1','2026-05-01','in_progress')`);
    await db.execute(
      `INSERT INTO set_logs (id, workout_session_id, exercise_id, set_index, reps, load_grams, is_warmup, completed)
       VALUES ('s0','w1','e1',1,10,40000,1,1),
              ('s1','w1','e1',2,8,100000,0,1),
              ('s2','w1','e1',3,8,100000,0,1)`,
    );

    const previous = await previousSets(db, 'c1', 'e1', 'w2');
    expect(previous).toHaveLength(2);
    // Last week's 40kg opener must not become this week's working weight.
    expect(previous.every((s) => s.loadGrams === 100000)).toBe(true);
  });

  it('returns nothing to prefill for a first-time lift', async () => {
    await db.execute(`INSERT INTO exercises (id, name) VALUES ('e9','Snatch')`);
    expect(await previousSets(db, 'c1', 'e9')).toEqual([]);
  });

  it('lists outstanding invoices oldest due first', async () => {
    await db.execute(`INSERT INTO invoices (id, client_id, number, status, currency, total_minor, due_date)
      VALUES ('i2','c1','INV-2','issued','EUR',20000,'2026-05-20'),
             ('i1','c1','INV-1','issued','EUR',50000,'2026-05-01'),
             ('i3','c2','INV-3','settled','EUR',10000,'2026-04-01')`);
    await db.execute(`INSERT INTO payments (id, invoice_id, amount_minor) VALUES ('pay1','i1',20000)`);

    const outstanding = await outstandingInvoices(db);
    expect(outstanding.map((i) => i.number)).toEqual(['INV-1', 'INV-2']);
    // Settled invoices are off the chase list.
    expect(outstanding.find((i) => i.number === 'INV-3')).toBeUndefined();
    expect(outstanding[0]?.paidMinor).toBe(20000);
  });
  it('narrows the chase list to one client', async () => {
    await db.execute(`INSERT INTO invoices (id, client_id, number, status, currency, total_minor)
      VALUES ('i1','c1','INV-1','issued','EUR',50000),
             ('i2','c2','INV-2','issued','EUR',30000)`);

    expect((await outstandingInvoices(db, 'c1')).map((i) => i.number)).toEqual(['INV-1']);
    expect(await outstandingInvoices(db)).toHaveLength(2);
  });

  it('reads a profile with its credit balance and next expiry', async () => {
    await db.execute(`UPDATE clients SET email='dana@example.com' WHERE id='c1'`);
    await db.execute(`INSERT INTO packages (id, client_id, credits_remaining, status, expires_at)
      VALUES ('p2','c1',3,'active','2026-12-01'), ('p3','c1',0,'active','2026-07-01')`);

    const profile = await clientDetail(db, 'c1');
    expect(profile?.fullName).toBe('Dana Rivers');
    expect(profile?.creditsRemaining).toBe(10); // 7 from the seed, plus 3
    // A pack with nothing left on it has no expiry worth warning about.
    expect(profile?.nextExpiry).toBe('2026-12-01');
  });

  it('shows an overdrawn client as negative, not as zero', async () => {
    await db.execute(`INSERT INTO packages (id, client_id, credits_remaining, status)
      VALUES ('p9','c2',-2,'exhausted')`);

    // Hiding the debt behind a zero is how a trainer ends up training someone
    // for free without knowing it.
    expect((await clientDetail(db, 'c2'))?.creditsRemaining).toBe(-2);
  });

  it('returns nothing for a client this device has not seen', async () => {
    expect(await clientDetail(db, 'nobody')).toBeNull();
  });

  it('summarises recent training by volume of working sets', async () => {
    await db.execute(`INSERT INTO exercises (id, name) VALUES ('e1','Back Squat')`);
    await db.execute(`INSERT INTO workout_sessions (id, client_id, performed_on, status)
      VALUES ('w1','c1','2026-04-24','completed'), ('w2','c1','2026-05-01','completed')`);
    await db.execute(
      `INSERT INTO set_logs (id, workout_session_id, exercise_id, set_index, reps, load_grams, is_warmup, completed)
       VALUES ('s0','w2','e1',1,10,40000,1,1),
              ('s1','w2','e1',2,5,100000,0,1),
              ('s2','w2','e1',3,5,100000,0,0)`,
    );

    const workouts = await recentWorkouts(db, 'c1');
    expect(workouts.map((w) => w.id)).toEqual(['w2', 'w1']);
    // Two completed sets, but only one working set counts toward volume, and
    // the set that was logged and not performed counts toward neither.
    expect(workouts[0]?.setCount).toBe(2);
    expect(workouts[0]?.volumeGrams).toBe(500_000);
  });

  it('finds a workout left open, so the floor logger resumes it', async () => {
    await db.execute(`INSERT INTO workout_sessions (id, client_id, performed_on, status)
      VALUES ('w1','c1','2026-05-01','completed'), ('w2','c1','2026-05-02','in_progress')`);

    expect((await openWorkout(db, 'c1'))?.id).toBe('w2');
    expect(await openWorkout(db, 'c2')).toBeNull();
  });

  it('lists measurements newest first', async () => {
    await db.execute(`INSERT INTO biometric_entries (id, client_id, measured_on, weight_grams, body_fat_bp, notes)
      VALUES ('b1','c1','2026-04-01',84000,1700,''), ('b2','c1','2026-05-01',82400,1550,'')`);

    const history = await biometricHistory(db, 'c1');
    expect(history.map((b) => b.id)).toEqual(['b2', 'b1']);
    expect(history[0]?.weightGrams).toBe(82400);
  });

  describe('the renewal list', () => {
    it('lists whoever is nearly out, emptiest first', async () => {
      // c1 has 7 from the seed. Give c2 one, and add someone with none.
      await db.execute(`INSERT INTO packages (id, client_id, credits_remaining, status)
        VALUES ('p2','c2',1,'active')`);
      await db.execute(`INSERT INTO clients (id, full_name, status, notes)
        VALUES ('c3','Never Bought','active','')`);

      const low = await lowBalanceClients(db, 2);
      expect(low.map((c) => c.fullName)).toEqual(['Never Bought', 'Morgan Hale']);
      expect(low[0]?.creditsRemaining).toBe(0);
      expect(low[1]?.creditsRemaining).toBe(1);
    });

    it('leaves a client with credits alone', async () => {
      // Dana has 7 from the seed, well clear of the threshold.
      const low = await lowBalanceClients(db, 2);
      expect(low.map((c) => c.fullName)).not.toContain('Dana Rivers');
    });

    it('shows an overdrawn client as negative rather than zero', async () => {
      await db.execute(`INSERT INTO packages (id, client_id, credits_remaining, status)
        VALUES ('p9','c2',-2,'exhausted')`);

      const low = await lowBalanceClients(db, 2);
      const morgan = low.find((c) => c.fullName === 'Morgan Hale');
      expect(morgan?.creditsRemaining).toBe(-2);
    });

    it('ignores archived clients', async () => {
      await db.execute(`UPDATE clients SET status = 'archived' WHERE id = 'c2'`);
      const low = await lowBalanceClients(db, 2);
      expect(low.map((c) => c.fullName)).not.toContain('Morgan Hale');
    });
  });
});
