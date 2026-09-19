/**
 * Types mirroring api/openapi.yaml.
 *
 * Hand-written rather than generated. A generator would produce the same
 * shapes with worse names and no room for the comments that say *why* a field
 * is an integer, and the spec is stable enough that drift is caught by the
 * server's own tests.
 *
 * Every amount is integer minor units. Every load is grams. RPE is tenths.
 * There are no floats in this file, and that is deliberate.
 */

export interface Money {
  minor: number;
  currency: string;
}

/** The machine codes a client branches on. Messages are for humans. */
export const ErrorCode = {
  Validation: 'validation_failed',
  InvalidCredentials: 'invalid_credentials',
  TokenExpired: 'token_expired',
  TokenReused: 'refresh_token_reused',
  NotFound: 'not_found',
  InsufficientCredits: 'insufficient_credits',
  InvalidTransition: 'invalid_state_transition',
  InvoiceNotPayable: 'invoice_not_payable',
  Overpayment: 'overpayment',
  SchedulingConflict: 'scheduling_conflict',
  IdempotencyMismatch: 'idempotency_key_reused',
} as const;

export type ErrorCodeValue = (typeof ErrorCode)[keyof typeof ErrorCode];

export interface ApiErrorBody {
  code: string;
  message: string;
  fields?: Record<string, string>;
  /**
   * Structured context. An insufficient_credits refusal carries `remaining`
   * and `required`, which is what drives the renewal prompt.
   */
  meta?: Record<string, unknown>;
  request_id?: string;
}

export type OperationType =
  | 'attendance.mark'
  | 'workout.start'
  | 'workout.log_set'
  | 'workout.complete'
  | 'payment.record'
  | 'client.create'
  | 'client.update'
  | 'biometrics.record';

export interface PushOperation {
  id: string;
  type: OperationType;
  queued_at: string;
  data: Record<string, unknown>;
}

export interface OperationResult {
  id: string;
  type: OperationType;
  status: 'applied' | 'conflict' | 'rejected';
  result?: Record<string, unknown>;
  code?: string;
  message?: string;
}

export interface PushResult {
  results: OperationResult[];
  applied: number;
  conflicts: number;
  rejected: number;
  cursor: string;
}

export interface CollectionChanges {
  collection: string;
  rows: Record<string, unknown>[];
}

export interface PullResult {
  cursor: string;
  changes: CollectionChanges[];
  has_more: boolean;
  server_time: string;
}

export interface Account {
  user_id: string;
  tenant_id: string;
  email: string;
  display_name: string;
  role: string;
  currency: string;
}

export interface Tokens {
  access_token: string;
  refresh_token: string;
  expires_at: string;
  token_type: string;
}

export interface SessionResponse {
  account: Account;
  tokens: Tokens;
}

export type AttendanceStatus =
  | 'scheduled'
  | 'completed'
  | 'late_cancel'
  | 'early_cancel'
  | 'no_show';

export interface Attendee {
  id: string;
  session_id: string;
  client_id: string;
  client_name?: string;
  status: AttendanceStatus;
  credits_charged: number;
  marked_at?: string | null;
  notes: string;
}

export interface MarkResult {
  attendee: Attendee;
  /** Journey A turns on this number: at zero, offer a renewal. */
  credits_remaining: number;
  revenue_recognised?: Money | null;
  journal_entry_id?: string | null;
}

export interface CreditBalance {
  client_id: string;
  /** May be negative when a client is overdrawn. */
  remaining: number;
  next_expiry?: string | null;
}

export type PaymentInstrument = 'cash' | 'bank_transfer' | 'cheque' | 'digital_wallet';

export interface ShareLink {
  url: string;
  token: string;
}

export type InvoiceStatus = 'draft' | 'issued' | 'partially_paid' | 'settled' | 'void';

export interface InvoiceLine {
  id?: string;
  kind: 'package' | 'service';
  description: string;
  quantity: number;
  unit_price_minor: number;
  amount_minor?: number;
  package_credits?: number | null;
  credits_expire_on?: string | null;
}

export interface Invoice {
  id: string;
  client_id: string;
  number?: string | null;
  status: InvoiceStatus;
  currency: string;
  issue_date?: string | null;
  due_date?: string | null;
  total_minor: number;
  balance_minor?: number;
  notes?: string;
  payment_instructions_snapshot?: string;
  lines?: InvoiceLine[];
}

export interface InvoiceDraftInput {
  client_id: string;
  currency?: string;
  due_date?: string | null;
  notes?: string;
  lines: InvoiceLine[];
}

export interface LowBalanceClient {
  client_id: string;
  client_name: string;
  remaining: number;
  next_expiry?: string | null;
  last_session_on?: string | null;
}

/**
 * The five numbers, plus the renewal list.
 *
 * Fetched rather than mirrored: every figure is derived from the ledger and
 * the calendar at read time, and a payment recorded on another device moves
 * three of them at once. A stale dashboard is worse than an absent one.
 */
export interface DashboardSummary {
  sessions_today: number;
  sessions_today_unmarked: number;
  sessions_left_this_week: number;
  unpaid_invoices: number;
  outstanding: Money;
  low_balance_count: number;
  low_balance: LowBalanceClient[];
  low_balance_threshold: number;
  income_this_month: Money;
  currency: string;
}
