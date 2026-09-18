# ADR 0005: One Expo codebase for iOS, Android and web

Status: Accepted
Date: 2026-09-18

## Context

The PRD requires native mobile apps optimized for one-handed gym-floor use and
a desktop web portal optimized for multi-week program design and financial
reporting. These are genuinely different interaction models.

## Decision

Build both from a single Expo / React Native codebase, using Expo Router's web
output for the desktop portal, with platform-specific layouts for the screens
where the two diverge most (program builder, bulk invoicing, P&L).

Local persistence is `expo-sqlite` with Drizzle, mirroring the server schema,
with an outbox table draining through the sync engine.

## Consequences

Good:
- One domain model, one sync engine, one API client for all three targets.
- A fix to credit-burn logic ships everywhere at once.

Costs:
- Dense desktop tables in React Native Web are workable but less rich than a
  dedicated React web app. Accepted for Phase 1; revisitable in Phase 2 if the
  financial reporting screens outgrow it.
- Some screens need real platform branching rather than responsive CSS alone.
