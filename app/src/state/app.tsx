/**
 * Everything the screens share: the database, the API client, who is signed
 * in, and the sync loop.
 *
 * One provider rather than several because these four are not independent —
 * the API client needs the token store, sync needs both the database and the
 * client, and signing out has to clear all of it together. Splitting them
 * would only invite a screen to hold a client for an account that is gone.
 */

import React, {
  createContext, useCallback, useContext, useEffect, useMemo, useRef, useState,
} from 'react';
import { AppState as RNAppState, type AppStateStatus } from 'react-native';
import Constants from 'expo-constants';

import { ApiClient } from '@/api/client';
import type { Account } from '@/api/types';
import { openDatabase } from '@/db';
import { getMeta, setMeta, type Database } from '@/db/types';
import { secureTokens } from '@/auth/tokens';
import { synchronise, type SyncReport } from '@/sync/engine';
import { DEMO_ACCOUNT, seedDemo } from '@/demo/seed';
import * as outbox from '@/sync/outbox';

/** How often a foregrounded app tries to drain the outbox. */
const SYNC_INTERVAL_MS = 45_000;

/**
 * A build with no server behind it.
 *
 * Not a mock: every screen already reads local SQLite, so this is the real app
 * with the sync engine idle. What it cannot do is anything the server decides
 * — burn a credit, recognise revenue, settle an invoice — so those stay
 * queued, exactly as they would on a phone with no signal.
 */
export const DEMO = process.env.EXPO_PUBLIC_DEMO === '1';

const ACCOUNT_KEY = 'auth.account';
const LAST_SYNC_KEY = 'sync.last_at';

export interface SyncState {
  running: boolean;
  offline: boolean;
  lastSyncAt: string | null;
  pending: number;
  failed: number;
  error: string | null;
}

interface AppContextValue {
  /** Null only before the database has opened. */
  db: Database | null;
  /**
   * Set when the local database could not be opened at all.
   *
   * Worth its own state rather than a thrown error: without a database there
   * is no app, and the honest thing is to say so. Leaving `ready` false
   * instead left the launch spinner turning for ever, which looks identical
   * to a slow network and tells the trainer nothing.
   */
  fatal: string | null;
  api: ApiClient;
  account: Account | null;
  /** False while the database opens and the stored account is restored. */
  ready: boolean;
  sync: SyncState;
  /** Bumped after any local write, so open screens re-read. */
  revision: number;
  /** Tells open screens their data changed. */
  touch: () => void;
  signIn: (email: string, password: string) => Promise<void>;
  signUp: (input: {
    email: string; password: string; display_name: string;
    business_name?: string; currency?: string;
  }) => Promise<void>;
  signOut: () => Promise<void>;
  syncNow: () => Promise<SyncReport | null>;
}

const AppContext = createContext<AppContextValue | null>(null);

/**
 * Where the API lives.
 *
 * The environment variable comes first because `localhost` on a phone is the
 * phone. Testing on a real device means pointing at the machine running the
 * API, and an env var is a flag on the dev-server command rather than an edit
 * to a checked-in file that then wants unstaging before every commit.
 *
 * EXPO_PUBLIC_ is Expo's own convention: it is inlined at build time, so this
 * works in a release build too.
 */
function baseUrl(): string {
  const fromEnv = process.env.EXPO_PUBLIC_API_URL;
  if (fromEnv) return fromEnv.replace(/\/+$/, '');

  const configured = (Constants.expoConfig?.extra as { apiBaseUrl?: string } | undefined)?.apiBaseUrl;
  return configured ?? 'http://localhost:8080';
}

export function AppProvider({ children }: { children: React.ReactNode }) {
  const [db, setDb] = useState<Database | null>(null);
  const [account, setAccount] = useState<Account | null>(null);
  const [ready, setReady] = useState(false);
  const [fatal, setFatal] = useState<string | null>(null);
  const [revision, setRevision] = useState(0);
  const [sync, setSync] = useState<SyncState>({
    running: false, offline: false, lastSyncAt: null, pending: 0, failed: 0, error: null,
  });

  // Held in a ref so the API client, built once, can reach the current setter
  // without being rebuilt — rebuilding it would throw away the in-flight
  // refresh promise that keeps concurrent 401s from spending the token twice.
  const signedOut = useRef<() => void>(() => {});

  const api = useMemo(
    () => new ApiClient({
      baseUrl: baseUrl(),
      tokens: secureTokens,
      onSignedOut: () => signedOut.current(),
    }),
    [],
  );

  const touch = useCallback(() => setRevision((r) => r + 1), []);

  const refreshCounts = useCallback(async (database: Database) => {
    const counts = await outbox.counts(database);
    setSync((s) => ({ ...s, pending: counts.pending, failed: counts.failed }));
  }, []);

  // Open the database and restore the signed-in account, in that order: the
  // account is stored locally so a launch with no signal still lands on the
  // roster rather than on a login screen.
  useEffect(() => {
    let cancelled = false;
    (async () => {
      let database: Database;
      try {
        database = await openDatabase();
      } catch (cause) {
        if (cancelled) return;
        setFatal(cause instanceof Error ? cause.message : String(cause));
        setReady(true);
        return;
      }
      if (cancelled) return;

      if (DEMO) {
        await seedDemo(database);
        if (cancelled) return;
        setDb(database);
        setAccount(DEMO_ACCOUNT);
        await refreshCounts(database);
        setReady(true);
        return;
      }

      const stored = await getMeta(database, ACCOUNT_KEY);
      const lastSync = await getMeta(database, LAST_SYNC_KEY);
      const token = await secureTokens.refreshToken();

      if (cancelled) return;
      setDb(database);
      // Both halves must be present. A stored account with no token would show
      // a roster the app cannot refresh; a token with no account has nobody to
      // show it to.
      if (stored && token) setAccount(JSON.parse(stored) as Account);
      setSync((s) => ({ ...s, lastSyncAt: lastSync }));
      await refreshCounts(database);
      setReady(true);
    })();
    return () => { cancelled = true; };
  }, [refreshCounts]);

  const syncNow = useCallback(async (): Promise<SyncReport | null> => {
    if (!db || !account) return null;
    // Nothing to sync with. The outbox still fills up, which is the point:
    // the badge shows what would be sent.
    if (DEMO) {
      const counts = await outbox.counts(db);
      setSync((s) => ({ ...s, ...counts, running: false, offline: true, error: null }));
      return null;
    }

    setSync((s) => ({ ...s, running: true, error: null }));
    const report = await synchronise(db, api);
    const at = new Date().toISOString();
    if (!report.offline) await setMeta(db, LAST_SYNC_KEY, at);

    const counts = await outbox.counts(db);
    setSync({
      running: false,
      offline: report.offline,
      lastSyncAt: report.offline ? null : at,
      pending: counts.pending,
      failed: counts.failed,
      error: report.error ?? null,
    });
    // Rows arrived, so anything on screen is now stale.
    if (report.pulled > 0 || report.pushed > 0) touch();
    return report;
  }, [db, account, api, touch]);

  // Keep the mirror honest: on sign-in, on a timer, and whenever the trainer
  // brings the app back to the foreground — which is the moment they are most
  // likely to have walked back into signal.
  useEffect(() => {
    if (!db || !account) return;
    if (DEMO) return;

    void syncNow();
    const timer = setInterval(() => { void syncNow(); }, SYNC_INTERVAL_MS);
    const subscription = RNAppState.addEventListener('change', (status: AppStateStatus) => {
      if (status === 'active') void syncNow();
    });

    return () => {
      clearInterval(timer);
      subscription.remove();
    };
  }, [db, account, syncNow]);

  // Local writes change the outbox, so the indicator follows the revision.
  useEffect(() => {
    if (db) void refreshCounts(db);
  }, [db, revision, refreshCounts]);

  const persistAccount = useCallback(async (next: Account | null) => {
    setAccount(next);
    if (db) await setMeta(db, ACCOUNT_KEY, next ? JSON.stringify(next) : '');
  }, [db]);

  const signIn = useCallback(async (email: string, password: string) => {
    const session = await api.login(email, password);
    await persistAccount(session.account);
  }, [api, persistAccount]);

  const signUp = useCallback(async (input: {
    email: string; password: string; display_name: string;
    business_name?: string; currency?: string;
  }) => {
    const session = await api.signup(input);
    await persistAccount(session.account);
  }, [api, persistAccount]);

  /**
   * Signs out.
   *
   * The local mirror is deliberately *not* wiped. A trainer who signs out with
   * unsent attendance in the outbox has real work queued on this device, and
   * dropping it to be tidy would lose money that was actually earned. The
   * tokens go; the evidence stays.
   */
  const signOut = useCallback(async () => {
    await secureTokens.clear();
    await persistAccount(null);
  }, [persistAccount]);

  signedOut.current = () => { void persistAccount(null); };

  const value = useMemo<AppContextValue>(
    () => ({ db, api, account, ready, fatal, sync, revision, touch, signIn, signUp, signOut, syncNow }),
    [db, api, account, ready, fatal, sync, revision, touch, signIn, signUp, signOut, syncNow],
  );

  return <AppContext.Provider value={value}>{children}</AppContext.Provider>;
}

export function useApp(): AppContextValue {
  const value = useContext(AppContext);
  if (!value) throw new Error('useApp must be used inside <AppProvider>');
  return value;
}

export interface QueryResult<T> {
  data: T | null;
  loading: boolean;
  error: Error | null;
  reload: () => void;
}

/**
 * Runs a local read and re-runs it when the data changes.
 *
 * There is no cache and no subscription machinery: these queries hit an
 * on-device SQLite file with a few thousand rows in it, so re-running one is
 * cheaper than the bookkeeping that would avoid re-running it.
 */
export function useQuery<T>(
  run: (db: Database) => Promise<T>,
  deps: React.DependencyList = [],
): QueryResult<T> {
  const { db, revision } = useApp();
  const [data, setData] = useState<T | null>(null);
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState<Error | null>(null);
  const [local, setLocal] = useState(0);

  // The caller passes a fresh closure each render; the dependency list they
  // give is what decides when it actually re-runs.
  const runRef = useRef(run);
  runRef.current = run;

  useEffect(() => {
    if (!db) return;
    let cancelled = false;
    setLoading(true);
    runRef.current(db)
      .then((result) => {
        if (cancelled) return;
        setData(result);
        setError(null);
      })
      .catch((cause: unknown) => {
        if (!cancelled) setError(cause instanceof Error ? cause : new Error(String(cause)));
      })
      .finally(() => { if (!cancelled) setLoading(false); });
    return () => { cancelled = true; };
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [db, revision, local, ...deps]);

  const reload = useCallback(() => setLocal((n) => n + 1), []);
  return { data, loading, error, reload };
}
