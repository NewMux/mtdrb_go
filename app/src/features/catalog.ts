/**
 * The practice's places and price list, read from the local mirror.
 *
 * Both are changed online (a location's name must be unique, an offer is
 * priced in the ledger's currency — the server checks both) and read offline:
 * the calendar colours by place and the sell screen starts from an offer in a
 * gym with no signal.
 */

import type { Database } from '@/db/types';

export type LocationKind = 'studio' | 'gym' | 'outdoor' | 'client_home' | 'online';

export interface LocalLocation {
  id: string;
  name: string;
  kind: LocationKind;
  address: string;
  region: string;
  colour: string | null;
  isPrimary: boolean;
  archived: boolean;
  /** Sessions booked here from today on. */
  upcoming: number;
}

export async function listLocations(db: Database, includeArchived = false): Promise<LocalLocation[]> {
  const rows = await db.select<Omit<LocalLocation, 'isPrimary' | 'archived'> & { isPrimary: number; archived: number }>(
    `SELECT l.id, l.name, coalesce(l.kind, 'studio') AS kind, coalesce(l.address, '') AS address,
            coalesce(l.region, '') AS region, l.colour,
            coalesce(l.is_primary, 0) AS isPrimary, l.archived_at IS NOT NULL AS archived,
            (SELECT count(*) FROM sessions s
              WHERE s.location_id = l.id AND s.status != 'cancelled' AND s.starts_at >= ?) AS upcoming
       FROM locations l
      WHERE ? OR l.archived_at IS NULL
      ORDER BY l.archived_at IS NOT NULL, coalesce(l.is_primary, 0) DESC, lower(l.name)`,
    [new Date(new Date().setHours(0, 0, 0, 0)).toISOString(), includeArchived ? 1 : 0],
  );
  return rows.map((r) => ({ ...r, isPrimary: Boolean(r.isPrimary), archived: Boolean(r.archived) }));
}

export type OfferKind = 'session_pack' | 'monthly_coaching' | 'online_coaching' | 'semi_private';
export type Cycle = 'one_off' | 'weekly' | 'monthly' | 'annual';

export interface LocalOffer {
  id: string;
  name: string;
  description: string;
  kind: OfferKind;
  credits: number | null;
  priceMinor: number;
  currency: string;
  priceIncludesVat: boolean;
  validityDays: number | null;
  cycle: Cycle;
  archived: boolean;
}

export async function listOffers(db: Database, includeArchived = false): Promise<LocalOffer[]> {
  const rows = await db.select<Omit<LocalOffer, 'priceIncludesVat' | 'archived'> & { priceIncludesVat: number; archived: number }>(
    `SELECT id, name, coalesce(description, '') AS description, coalesce(kind, 'session_pack') AS kind,
            credits, coalesce(price_minor, 0) AS priceMinor, coalesce(currency, '') AS currency,
            coalesce(price_includes_vat, 1) AS priceIncludesVat, validity_days AS validityDays,
            coalesce(cycle, 'one_off') AS cycle, archived_at IS NOT NULL AS archived
       FROM package_offers
      WHERE ? OR archived_at IS NULL
      ORDER BY archived_at IS NOT NULL, coalesce(sort_order, 0), price_minor, lower(name)`,
    [includeArchived ? 1 : 0],
  );
  return rows.map((r) => ({ ...r, priceIncludesVat: Boolean(r.priceIncludesVat), archived: Boolean(r.archived) }));
}

/** The date an offer's credits run out if sold today, as YYYY-MM-DD. */
export function expiryFor(offer: Pick<LocalOffer, 'validityDays'>, soldOn: Date = new Date()): string | null {
  if (!offer.validityDays) return null;
  const d = new Date(soldOn.getFullYear(), soldOn.getMonth(), soldOn.getDate() + offer.validityDays);
  return `${d.getFullYear()}-${String(d.getMonth() + 1).padStart(2, '0')}-${String(d.getDate()).padStart(2, '0')}`;
}
