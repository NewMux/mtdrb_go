/**
 * Compile-time checks that the hand-written wire types match the spec.
 *
 * `schema.d.ts` is generated from api/openapi.yaml, and a Go test holds every
 * server route to that file. These assertions close the loop on the client:
 * every field a hand-written type reads must exist in the spec, so a renamed or
 * invented field fails `tsc` rather than arriving as `undefined` on a gym floor.
 * New code should use `Schema<'Name'>` directly.
 *
 * Five of the bugs STATUS.md records were each side asserting a wire format it
 * had invented. This file emits nothing at runtime.
 */

import type { components } from './schema';
import type * as T from './types';

/** A schema object from the spec, by name. */
export type Schema<Name extends keyof components['schemas']> = components['schemas'][Name];

type NoExtraKeys<Hand, Spec> = Exclude<keyof Hand, keyof NonNullable<Spec>> extends never
  ? true
  : { error: 'fields not in api/openapi.yaml'; fields: Exclude<keyof Hand, keyof NonNullable<Spec>> };
type Assert<Check extends true> = Check;

export type Conformance = [
  Assert<NoExtraKeys<T.DashboardSummary, Schema<'DashboardSummary'>>>,
  Assert<NoExtraKeys<T.LowBalanceClient, Schema<'LowBalanceClient'>>>,
  Assert<NoExtraKeys<T.Invoice, Schema<'Invoice'>>>,
  Assert<NoExtraKeys<T.InvoiceLine, Schema<'InvoiceLine'>>>,
  Assert<NoExtraKeys<T.InvoiceDraftInput, Schema<'InvoiceDraftInput'>>>,
  Assert<NoExtraKeys<T.PullResult, Schema<'PullResult'>>>,
  Assert<NoExtraKeys<T.PushResult, Schema<'PushResult'>>>,
  Assert<NoExtraKeys<T.PushOperation, Schema<'PushOperation'>>>,
  Assert<NoExtraKeys<T.Attendee, Schema<'Attendee'>>>,
  Assert<NoExtraKeys<T.MarkResult, Schema<'MarkResult'>>>,
  Assert<NoExtraKeys<T.CreditBalance, Schema<'CreditBalance'>>>,
  Assert<NoExtraKeys<T.Money, Schema<'Money'>>>,
];
