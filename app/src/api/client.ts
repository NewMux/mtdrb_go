/**
 * The HTTP client.
 *
 * Its main job beyond fetch is turning the server's typed errors into
 * something the app can branch on, and refreshing an expired access token
 * once — transparently, because a trainer mid-set should never see a login
 * screen because a fifteen-minute token lapsed.
 */

import type {
  ApiErrorBody, DashboardSummary, PullResult, PushOperation, PushResult, SessionResponse,
} from './types';

/** An error carrying the server's machine code. */
export class ApiError extends Error {
  constructor(
    readonly status: number,
    readonly code: string,
    message: string,
    readonly meta?: Record<string, unknown>,
    readonly fields?: Record<string, string>,
  ) {
    super(message);
    this.name = 'ApiError';
  }

  /** True when the request failed for a reason retrying cannot fix. */
  get isPermanent(): boolean {
    return this.status >= 400 && this.status < 500 && this.status !== 429;
  }
}

/**
 * The code a demo build answers every write with: there is no server to send
 * it to. Here rather than in src/demo so a screen can recognise it without
 * pulling the demo into a normal build.
 */
export const DEMO_READ_ONLY = 'demo_read_only';

/** A transport failure — no signal, DNS, a dropped connection. */
export class NetworkError extends Error {
  constructor(cause: unknown) {
    super(cause instanceof Error ? cause.message : 'network request failed');
    this.name = 'NetworkError';
  }
}

export interface TokenStore {
  accessToken(): Promise<string | null>;
  refreshToken(): Promise<string | null>;
  save(access: string, refresh: string): Promise<void>;
  clear(): Promise<void>;
}

export interface ClientOptions {
  baseUrl: string;
  tokens: TokenStore;
  /** Injected so tests do not need a network. */
  fetchImpl?: typeof fetch;
  /** Called when refreshing fails and the trainer really must sign in again. */
  onSignedOut?: () => void;
  /**
   * Keep the refresh token in an httpOnly cookie rather than the body. The
   * web build does: a browser has nowhere safe to hold one, and a cookie the
   * page's scripts cannot read is the least bad place.
   */
  cookieTransport?: boolean;
}

/** The sentinel a token store returns for "the refresh token is a cookie". */
export const COOKIE_REFRESH = 'cookie';

export interface RequestOptions {
  method?: string;
  body?: unknown;
  /** Makes a mutating request safe to retry. */
  idempotencyKey?: string;
  /** Internal: prevents a refresh loop. */
  isRetry?: boolean;
  /** Ask for, or present, the refresh cookie. */
  cookie?: boolean;
}

export class ApiClient {
  private readonly fetchImpl: typeof fetch;
  /** Shared across concurrent 401s so one refresh serves all of them. */
  private refreshing: Promise<boolean> | null = null;

  constructor(private readonly options: ClientOptions) {
    // Wrapped, not captured. `fetch` held in a field and then called as
    // `this.fetchImpl(...)` is invoked with the client as its receiver, and a
    // browser rejects that: "Failed to execute 'fetch' on 'Window': Illegal
    // invocation". Node's fetch does not care, so every test passed while the
    // web build could not make a single request — and because the failure
    // arrives as a rejected promise, the app reported it as "No connection"
    // and simply looked offline for ever.
    this.fetchImpl = options.fetchImpl ?? ((input, init) => fetch(input, init));
  }

  async request<T>(path: string, opts: RequestOptions = {}): Promise<T> {
    const headers: Record<string, string> = { Accept: 'application/json' };
    if (opts.body !== undefined) headers['Content-Type'] = 'application/json';
    if (opts.idempotencyKey) headers['Idempotency-Key'] = opts.idempotencyKey;
    const cookie = opts.cookie && this.options.cookieTransport;
    if (cookie) headers['X-Refresh-Transport'] = 'cookie';

    const token = await this.options.tokens.accessToken();
    if (token) headers['Authorization'] = `Bearer ${token}`;

    let response: Response;
    try {
      response = await this.fetchImpl(this.options.baseUrl + path, {
        method: opts.method ?? 'GET',
        headers,
        body: opts.body === undefined ? undefined : JSON.stringify(opts.body),
        ...(cookie ? { credentials: 'include' as const } : {}),
      });
    } catch (cause) {
      // A transport failure is not the trainer's problem to see; the sync
      // engine will try again.
      throw new NetworkError(cause);
    }

    if (response.status === 401 && !opts.isRetry) {
      if (await this.refresh()) {
        return this.request<T>(path, { ...opts, isRetry: true });
      }
    }

    if (response.status === 204) return undefined as T;

    const text = await response.text();
    const payload = text ? (JSON.parse(text) as unknown) : null;

    if (!response.ok) {
      const body = (payload as { error?: ApiErrorBody } | null)?.error;
      throw new ApiError(
        response.status,
        body?.code ?? 'unknown',
        body?.message ?? `request failed with ${response.status}`,
        body?.meta,
        body?.fields,
      );
    }
    return payload as T;
  }

  /**
   * Refreshes the access token, once, even under concurrent 401s.
   *
   * Without the shared promise, ten parallel requests would each present the
   * same refresh token — and the server treats a reused refresh token as theft
   * and revokes the whole family. Serialising here is what keeps that from
   * signing the trainer out.
   */
  private async refresh(): Promise<boolean> {
    if (this.refreshing) return this.refreshing;

    this.refreshing = (async () => {
      try {
        const refreshToken = await this.options.tokens.refreshToken();
        if (!refreshToken) return false;

        const cookie = this.options.cookieTransport === true;
        const response = await this.fetchImpl(this.options.baseUrl + '/v1/auth/refresh', {
          method: 'POST',
          headers: {
            'Content-Type': 'application/json',
            Accept: 'application/json',
            ...(cookie ? { 'X-Refresh-Transport': 'cookie' } : {}),
          },
          body: JSON.stringify(cookie ? {} : { refresh_token: refreshToken }),
          ...(cookie ? { credentials: 'include' as const } : {}),
        });
        if (!response.ok) {
          await this.options.tokens.clear();
          this.options.onSignedOut?.();
          return false;
        }
        const session = (await response.json()) as SessionResponse;
        await this.keep(session);
        return true;
      } catch {
        // A network failure during refresh is not a sign-out: the token may
        // still be perfectly good once there is signal again.
        return false;
      } finally {
        this.refreshing = null;
      }
    })();

    return this.refreshing;
  }

  // -- Authentication -------------------------------------------------------

  /**
   * Signs in. With two-step sign-in on, this throws an ApiError whose code
   * is `mfa_required` and whose meta carries the `mfa_token` to pass to
   * verifyMfa with the code.
   */
  async login(email: string, password: string): Promise<SessionResponse> {
    const session = await this.request<SessionResponse>('/v1/auth/login', {
      method: 'POST',
      body: { email, password },
      cookie: true,
    });
    await this.keep(session);
    return session;
  }

  async verifyMfa(mfaToken: string, code: string): Promise<SessionResponse> {
    const session = await this.request<SessionResponse>('/v1/auth/mfa', {
      method: 'POST',
      body: { mfa_token: mfaToken, code },
      cookie: true,
    });
    await this.keep(session);
    return session;
  }

  /** Accepted whether or not the address has an account. */
  forgotPassword(email: string): Promise<void> {
    return this.request<void>('/v1/auth/password/forgot', { method: 'POST', body: { email } });
  }

  resetPassword(token: string, password: string): Promise<void> {
    return this.request<void>('/v1/auth/password/reset', { method: 'POST', body: { token, password } });
  }

  /** Ends this device's session on the server, and forgets its tokens. */
  async logout(): Promise<void> {
    try {
      const refresh = await this.options.tokens.refreshToken();
      const cookie = this.options.cookieTransport === true;
      await this.request<void>('/v1/session/logout', {
        method: 'POST',
        body: cookie || !refresh ? {} : { refresh_token: refresh },
        cookie: true,
        isRetry: true,
      });
    } finally {
      await this.options.tokens.clear();
    }
  }

  private async keep(session: SessionResponse): Promise<void> {
    await this.options.tokens.save(
      session.tokens.access_token,
      this.options.cookieTransport ? COOKIE_REFRESH : session.tokens.refresh_token,
    );
  }

  async signup(input: {
    email: string;
    password: string;
    display_name: string;
    business_name?: string;
    currency?: string;
    timezone?: string;
  }): Promise<SessionResponse> {
    const session = await this.request<SessionResponse>('/v1/auth/signup', {
      method: 'POST',
      body: input,
      cookie: true,
    });
    await this.keep(session);
    return session;
  }

  // -- Sync -----------------------------------------------------------------

  /**
   * `reset` names collections to restart from the beginning — for a device
   * whose local schema gained columns its stored rows lack.
   */
  pull(cursor: string, limit = 500, reset: readonly string[] = []): Promise<PullResult> {
    const query = new URLSearchParams();
    if (cursor) query.set('cursor', cursor);
    query.set('limit', String(limit));
    if (reset.length > 0) query.set('reset', reset.join(','));
    return this.request<PullResult>(`/v1/sync/pull?${query.toString()}`);
  }

  push(operations: PushOperation[]): Promise<PushResult> {
    return this.request<PushResult>('/v1/sync/push', {
      method: 'POST',
      body: { operations },
    });
  }

  // -- Reads that are not worth mirroring locally ---------------------------
  //
  // Reports are computed from the ledger server-side and are meaningless
  // stale, so they are fetched rather than synced.

  get<T>(path: string): Promise<T> {
    return this.request<T>(path);
  }

  dashboard(): Promise<DashboardSummary> {
    return this.request<DashboardSummary>('/v1/dashboard');
  }

  post<T>(path: string, body: unknown, idempotencyKey?: string): Promise<T> {
    return this.request<T>(path, { method: 'POST', body, idempotencyKey });
  }

  patch<T>(path: string, body: unknown): Promise<T> {
    return this.request<T>(path, { method: 'PATCH', body });
  }

  delete(path: string): Promise<void> {
    return this.request<void>(path, { method: 'DELETE' });
  }
}
