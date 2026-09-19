import { stubFetch, memoryTokens } from './support';
import { ApiClient, ApiError, NetworkError } from '@/api/client';

describe('ApiClient', () => {
  it('surfaces the server machine code and meta', async () => {
    // insufficient_credits carries the remaining balance, which is what drives
    // the renewal prompt at the moment a pack runs out.
    const { fetch } = stubFetch(() => ({
      status: 422,
      body: {
        error: {
          code: 'insufficient_credits',
          message: 'this client has 0 credits remaining but 1 are needed',
          meta: { remaining: 0, required: 1 },
        },
      },
    }));
    const api = new ApiClient({ baseUrl: 'https://api.test', tokens: memoryTokens(), fetchImpl: fetch });

    await expect(api.get('/v1/anything')).rejects.toMatchObject({
      code: 'insufficient_credits',
      meta: { remaining: 0, required: 1 },
    });
  });

  it('distinguishes a transport failure from a refusal', async () => {
    const { fetch } = stubFetch(() => { throw new Error('no signal'); });
    const api = new ApiClient({ baseUrl: 'https://api.test', tokens: memoryTokens(), fetchImpl: fetch });

    await expect(api.get('/v1/anything')).rejects.toBeInstanceOf(NetworkError);
  });

  it('refreshes once on a 401 and retries the original request', async () => {
    let calls = 0;
    const { fetch } = stubFetch((url) => {
      if (url.includes('/auth/refresh')) {
        return {
          status: 200,
          body: { account: {}, tokens: { access_token: 'fresh', refresh_token: 'fresh-r' } },
        };
      }
      calls++;
      return calls === 1
        ? { status: 401, body: { error: { code: 'token_expired', message: 'expired' } } }
        : { status: 200, body: { ok: true } };
    });

    const tokens = memoryTokens();
    const api = new ApiClient({ baseUrl: 'https://api.test', tokens, fetchImpl: fetch });

    await expect(api.get('/v1/clients')).resolves.toEqual({ ok: true });
    expect(tokens.current().access).toBe('fresh');
  });

  it('refreshes only once under concurrent 401s', async () => {
    // The server treats a reused refresh token as theft and revokes the whole
    // family, so ten parallel 401s must not each present it.
    let refreshes = 0;
    const { fetch } = stubFetch((url) => {
      if (url.includes('/auth/refresh')) {
        refreshes++;
        return {
          status: 200,
          body: { account: {}, tokens: { access_token: 'fresh', refresh_token: 'fresh-r' } },
        };
      }
      return refreshes === 0
        ? { status: 401, body: { error: { code: 'token_expired', message: 'expired' } } }
        : { status: 200, body: { ok: true } };
    });

    const api = new ApiClient({ baseUrl: 'https://api.test', tokens: memoryTokens(), fetchImpl: fetch });
    await Promise.all([api.get('/v1/a'), api.get('/v1/b'), api.get('/v1/c')]);

    expect(refreshes).toBe(1);
  });

  it('signs out only when refreshing is refused, not when it cannot be reached', async () => {
    let signedOut = false;
    const { fetch } = stubFetch((url) => {
      if (url.includes('/auth/refresh')) throw new Error('no signal');
      return { status: 401, body: { error: { code: 'token_expired', message: 'expired' } } };
    });
    const api = new ApiClient({
      baseUrl: 'https://api.test', tokens: memoryTokens(), fetchImpl: fetch,
      onSignedOut: () => { signedOut = true; },
    });

    await expect(api.get('/v1/clients')).rejects.toBeInstanceOf(ApiError);
    expect(signedOut).toBe(false);
  });

  it('attaches an idempotency key when one is given', async () => {
    const { fetch, calls } = stubFetch(() => ({ status: 200, body: {} }));
    const api = new ApiClient({ baseUrl: 'https://api.test', tokens: memoryTokens(), fetchImpl: fetch });

    await api.post('/v1/invoices/x/payments', { amount_minor: 100 }, 'outbox-key-0001');

    const headers = calls[0]?.init?.headers as Record<string, string>;
    expect(headers['Idempotency-Key']).toBe('outbox-key-0001');
  });
});

describe('the default fetch', () => {
  it('is called without the client as its receiver', async () => {
    // A browser refuses `fetch` invoked with anything but the window as its
    // receiver, so holding the global in a field and calling it as a method
    // throws "Illegal invocation" — in a browser only. Node does not care,
    // which is exactly why this went unnoticed until the web build could not
    // reach the server at all.
    const original = globalThis.fetch;
    const receivers: unknown[] = [];
    try {
      globalThis.fetch = function (this: unknown) {
        receivers.push(this);
        return Promise.resolve(new Response('{}', {
          status: 200, headers: { 'Content-Type': 'application/json' },
        }));
      } as unknown as typeof fetch;

      const api = new ApiClient({ baseUrl: 'https://api.test', tokens: memoryTokens() });
      await api.get('/v1/receivables');

      expect(receivers).toHaveLength(1);
      expect(receivers[0]).not.toBeInstanceOf(ApiClient);
    } finally {
      globalThis.fetch = original;
    }
  });

  it('resolves the global at call time rather than at construction', async () => {
    // Capturing the reference in the constructor is the same mistake wearing a
    // different hat: it also makes the client unmockable after it is built.
    const original = globalThis.fetch;
    try {
      const api = new ApiClient({ baseUrl: 'https://api.test', tokens: memoryTokens() });

      let called = false;
      globalThis.fetch = (() => {
        called = true;
        return Promise.resolve(new Response('{}', {
          status: 200, headers: { 'Content-Type': 'application/json' },
        }));
      }) as unknown as typeof fetch;

      await api.get('/v1/receivables');
      expect(called).toBe(true);
    } finally {
      globalThis.fetch = original;
    }
  });
});

