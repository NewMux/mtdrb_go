/**
 * Selling a package.
 *
 * The only flow that is online-only, so what matters is the order of the three
 * calls, that each carries its own idempotency key, and that a failure to mint
 * the share link does not throw away an invoice that was successfully issued.
 */

import { ApiClient, NetworkError } from '@/api/client';
import { sellPackage } from '@/features/billing';
import { memoryTokens, stubFetch } from './support';

const sale = {
  clientId: 'c1',
  description: '10-session personal training package',
  credits: 10,
  unitPriceMinor: 5_000,
  currency: 'EUR',
};

function client(handler: Parameters<typeof stubFetch>[0]) {
  const stub = stubFetch(handler);
  const api = new ApiClient({
    baseUrl: 'https://api.test',
    tokens: memoryTokens(),
    fetchImpl: stub.fetch,
  });
  return { api, stub };
}

describe('sellPackage', () => {
  it('drafts, issues and shares, in that order', async () => {
    const { api, stub } = client((url) => {
      if (url.endsWith('/v1/invoices')) {
        return { status: 201, body: { id: 'inv1', client_id: 'c1', status: 'draft', currency: 'EUR', total_minor: 50_000 } };
      }
      if (url.endsWith('/issue')) {
        return { status: 200, body: { id: 'inv1', client_id: 'c1', number: '2026-014', status: 'issued', currency: 'EUR', total_minor: 50_000 } };
      }
      return { status: 201, body: { url: 'https://pay.test/abc', token: 'abc' } };
    });

    const result = await sellPackage(api, sale);

    expect(stub.calls.map((c) => c.url)).toEqual([
      'https://api.test/v1/invoices',
      'https://api.test/v1/invoices/inv1/issue',
      'https://api.test/v1/invoices/inv1/share',
    ]);
    expect(result.invoice.number).toBe('2026-014');
    expect(result.share?.url).toBe('https://pay.test/abc');
  });

  it('sends the pack as a package line, not a service line', async () => {
    let drafted: Record<string, unknown> = {};
    const { api } = client((url, init) => {
      if (url.endsWith('/v1/invoices')) {
        drafted = JSON.parse(String(init?.body)) as Record<string, unknown>;
        return { status: 201, body: { id: 'inv1', client_id: 'c1', status: 'draft', currency: 'EUR', total_minor: 50_000 } };
      }
      if (url.endsWith('/issue')) return { status: 200, body: { id: 'inv1', client_id: 'c1', status: 'issued', currency: 'EUR', total_minor: 50_000 } };
      return { status: 201, body: { url: 'https://pay.test/abc', token: 'abc' } };
    });

    await sellPackage(api, sale);

    // A package line credits Deferred Revenue; a service line would recognise
    // the whole sale as income the day it was sold.
    const lines = drafted.lines as Record<string, unknown>[];
    expect(lines[0]?.kind).toBe('package');
    expect(lines[0]?.package_credits).toBe(10);
  });

  it('gives the draft and the issue their own idempotency keys', async () => {
    const keys: (string | undefined)[] = [];
    const { api } = client((url, init) => {
      const headers = (init?.headers ?? {}) as Record<string, string>;
      keys.push(headers['Idempotency-Key']);
      if (url.endsWith('/v1/invoices')) return { status: 201, body: { id: 'inv1', client_id: 'c1', status: 'draft', currency: 'EUR', total_minor: 50_000 } };
      if (url.endsWith('/issue')) return { status: 200, body: { id: 'inv1', client_id: 'c1', status: 'issued', currency: 'EUR', total_minor: 50_000 } };
      return { status: 201, body: { url: 'https://pay.test/abc', token: 'abc' } };
    });

    await sellPackage(api, sale);

    // A retry after a timeout must not sell a second pack, and the two steps
    // must not share a key — the server would replay the draft's response for
    // the issue.
    expect(keys[0]).toBeTruthy();
    expect(keys[1]).toBeTruthy();
    expect(keys[0]).not.toBe(keys[1]);
  });

  it('keeps the issued invoice when the share link fails', async () => {
    const { api } = client((url) => {
      if (url.endsWith('/v1/invoices')) return { status: 201, body: { id: 'inv1', client_id: 'c1', status: 'draft', currency: 'EUR', total_minor: 50_000 } };
      if (url.endsWith('/issue')) return { status: 200, body: { id: 'inv1', client_id: 'c1', number: '2026-014', status: 'issued', currency: 'EUR', total_minor: 50_000 } };
      return { status: 500, body: { error: { code: 'internal', message: 'boom' } } };
    });

    const result = await sellPackage(api, sale);

    // The sale happened. Throwing here would leave the trainer believing it
    // had not, and issuing a second invoice for the same pack.
    expect(result.invoice.number).toBe('2026-014');
    expect(result.share).toBeNull();
  });

  it('fails loudly when there is no signal', async () => {
    const api = new ApiClient({
      baseUrl: 'https://api.test',
      tokens: memoryTokens(),
      fetchImpl: (() => Promise.reject(new Error('offline'))) as unknown as typeof fetch,
    });

    // Queueing this would have two devices mint the same invoice number, which
    // in most of the EU is a tax problem rather than a merge conflict.
    await expect(sellPackage(api, sale)).rejects.toBeInstanceOf(NetworkError);
  });
});
