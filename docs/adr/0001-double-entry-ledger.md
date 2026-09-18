# ADR 0001: Model finances as a true double-entry ledger

Status: Accepted
Date: 2026-09-18

## Context

CoachPulse trainers sell prepaid session packs and settle off-platform in cash,
bank wires and P2P transfers. The PRD requires a P&L, receivables ageing and a
tax-ready export.

The naive model — an `invoices` table with a `paid` flag, plus an `expenses`
table — breaks on the product's central case. A client pays $500 for ten
sessions in January and trains through April. Treating that payment as January
revenue overstates January, understates February through April, and produces a
tax export that does not match reality. It also cannot answer "how much service
do I still owe my clients?", which is a real liability on the trainer's books.

## Decision

Model money as a real double-entry ledger:

- `accounts`, `journal_entries`, `journal_lines`
- Entries are immutable. Corrections are reversing entries, never `UPDATE`.
- A `DEFERRABLE INITIALLY DEFERRED` constraint trigger asserts debits equal
  credits per entry at commit time, so an unbalanced entry cannot physically
  exist in the database.
- Prepaid packs credit **Deferred Revenue** (a liability), not revenue. Revenue
  is recognized when a session is delivered: DR Deferred Revenue, CR Training
  Revenue.
- All posting funnels through a single `ledger.Post` function. Domain packages
  never construct journal lines themselves.

## Consequences

Good:
- P&L, receivables ageing and the tax export are derived from the journal
  rather than reconstructed by ad-hoc queries, so they agree by construction.
- "Unearned session liability" becomes a first-class, queryable number.
- Every financial figure is auditable back to a dated, immutable entry.
- Multi-currency (PRD Phase 3) is additive: lines already carry a currency.

Costs:
- More write-path complexity than a `paid` boolean. Every money-touching
  feature must define its posting rule up front.
- Trainers are not accountants, so the UI must never expose account codes.
  Double entry is an implementation guarantee, not a user-facing concept.

## Alternatives rejected

**Simple tables, derive debits/credits at export time.** Cheaper initially, but
the export would be a second implementation of the financial model and would
drift from the dashboard. It also cannot represent partial payments and
reversals without effectively reinventing a journal.
