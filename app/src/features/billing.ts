/**
 * Selling a package.
 *
 * The one flow in the app that is deliberately **online-only**.
 *
 * Everything else here queues an operation and converges later, but issuing an
 * invoice takes a gap-free number from a counter the server holds under a row
 * lock. Two devices issuing offline would both mint "2026-014", and in most of
 * the EU a duplicated invoice number is not a bug to reconcile later — it is a
 * tax problem. So this asks the server, and says so plainly when there is no
 * signal.
 *
 * Each step carries an idempotency key, because a retry after a timeout must
 * not sell the client a second pack.
 */

import type { ApiClient } from '@/api/client';
import type { Invoice, ShareLink } from '@/api/types';
import { newId } from '@/lib/id';

export interface PackageSale {
  clientId: string;
  /** What the client sees on the invoice, e.g. "10-session personal training pack". */
  description: string;
  credits: number;
  /** Price of one session, in minor units. */
  unitPriceMinor: number;
  /**
   * The whole pack's price, when it is sold as one — from a price-list
   * offer, say. 3,500.00 for 10 is not 350.00 × 10 once VAT or a discount
   * is involved, and the server carries the part that does not divide onto
   * the pack's last session rather than losing it.
   */
  packPriceMinor?: number;
  currency?: string;
  dueDate?: string | null;
  expiresOn?: string | null;
}

/**
 * Drafts, issues and shares a package invoice.
 *
 * Three calls rather than one because that is the state machine: a draft posts
 * nothing and takes no number, so a mistake is deleted rather than reversed.
 * Issuing is what credits Deferred Revenue and grants the credits.
 */
export async function sellPackage(
  api: ApiClient,
  sale: PackageSale,
): Promise<{ invoice: Invoice; share: ShareLink | null }> {
  const draftKey = newId();

  const draft = await api.post<Invoice>('/v1/invoices', {
    client_id: sale.clientId,
    currency: sale.currency,
    due_date: sale.dueDate ?? null,
    lines: [{
      kind: 'package',
      description: sale.description,
      // `package_credits` is credits *per unit*, and the server grants
      // quantity × package_credits. Ten sessions is therefore ten units of one
      // credit, not ten units of ten — which would grant a hundred and, worse,
      // set the per-credit price to a tenth of the real one, so each delivered
      // session recognised a tenth of the revenue and Deferred Revenue never
      // drained.
      ...(sale.packPriceMinor !== undefined
        ? { quantity: 1, unit_price_minor: sale.packPriceMinor, package_credits: sale.credits }
        : { quantity: sale.credits, unit_price_minor: sale.unitPriceMinor, package_credits: 1 }),
      credits_expire_on: sale.expiresOn ?? null,
    }],
  }, draftKey);

  const invoice = await api.post<Invoice>(
    `/v1/invoices/${draft.id}/issue`,
    { due_date: sale.dueDate ?? null },
    newId(),
  );

  // The link is a convenience, not part of the sale. A failure here leaves a
  // perfectly good issued invoice, so it must not fail the whole flow — the
  // trainer can mint another link from the invoice at any time.
  let share: ShareLink | null = null;
  try {
    share = await api.post<ShareLink>(`/v1/invoices/${invoice.id}/share`, {});
  } catch {
    share = null;
  }

  return { invoice, share };
}
