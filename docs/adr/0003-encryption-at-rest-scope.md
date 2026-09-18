# ADR 0003: Storage-level encryption at rest, with narrow column encryption

Status: Accepted
Date: 2026-09-18

## Context

The PRD asks for "AES-256 encryption at rest for all database tables and image
storage". Read literally as per-column encryption, this is self-defeating:
encrypted columns cannot be indexed, range-scanned or aggregated. The ledger
would become unqueryable — no P&L, no receivables ageing, no date filtering.

## Decision

Deliver encryption at rest in three layers rather than one:

1. **Volume/disk encryption** for the database (encrypted EBS or equivalent,
   AES-256). This is what actually satisfies "all tables at rest".
2. **Bucket encryption** (SSE-S3 or SSE-KMS, AES-256) for receipts and progress
   photos, with no public read on any object.
3. **Column-level `pgcrypto`** for the narrow set of genuinely sensitive
   free-text fields that are never queried by value — currently emergency
   medical notes.

TLS 1.3 is enforced for data in transit.

## Consequences

Good:
- Meets the intent of the requirement without breaking the product's core
  capability.
- The highest-sensitivity field is protected even against a database-level read.

Costs:
- This is a deliberate, documented divergence from a literal reading of the
  PRD. It must be stated plainly in any security review rather than presented
  as full per-column encryption.
- Encrypted columns cannot be searched. If medical notes ever need search, that
  requires a separate decision.
