# CoachPulse v2: plan for sign-off

**Status:** in progress — slices 1–4 shipped, 5 mostly · **Date:** 2026-10-19

## Decisions already made

| Question | Answer |
|---|---|
| Who uses the app | **Trainers only.** Clients don't get accounts. |
| How clients pay | **Manual only:** cash, bank transfer, P2P. No card gateway. |
| Visual direction | **New design system.** Replaces the lime-on-charcoal look. |
| Process | Plan first (this doc), then build in slices on this branch. |
| Visual idiom | **iOS 27** — Liquid Glass, system palette, SF Symbols. |
| Share links | Yes (later slice) — read-only pages, no client accounts. |
| Primary colour | iOS system tint (blue); system colours for status only. |
| Units | kg / lb toggle, per device. |
| Templates | Nine starter programmes written for v2 (see `app/src/features/library.ts`). |
| Pricing | Single plan; no client limits or plan gating. |

---

## 1. What makes this worth $20 a month

Trainers compare against TrueCoach (~$19–26), Trainerize (~$10 and up) and
spreadsheets plus WhatsApp. With no client app, the $20 has to come from
three things, all of which a trainer feels within a week:

1. **It saves hours of admin.** Programmes built from templates in minutes,
   a calendar that books recurring clients once, invoices that write themselves.
2. **It makes them money.** Nobody trains for free by accident, every renewal
   gets surfaced before the pack runs out, and every debt is chased.
3. **It makes them look professional.** Clients get a polished link for their
   workout, their progress report and their invoice, with no login.

**What "trainer-only" means in practice:** clients never sign in, but they can
still *receive* things. The app already sends invoice share links over
WhatsApp; v2 uses the same tokenised-link mechanism for workout cards and
progress reports. These are read-only pages, not a client app.
**(Open question 1 below. Say no and those pages drop out.)**

---

## 2. Feature set

✅ = server already built, app screen missing · 🆕 = new on both sides ·
♻️ = exists, redesigned

### A. Today (home) ♻️
- Timeline of the day: sessions as cards with time, client, session type,
  credits left and a status ring.
- Swipe or tap to mark: Completed / No-show / Late cancel / Early cancel.
  The renewal-or-overdraft decision stays exactly as it works today.
- **Session prep card** 🆕: tap a session to see last workout, today's
  programmed day, injuries and flags, and the last check-in note.
- **Nudges strip** 🆕: "3 clients are low on credits", "2 invoices overdue",
  "Maya hasn't trained in 14 days". Each one is one tap from its fix.
- Week at a glance: sessions booked, earned so far, attendance rate.

### B. Calendar and booking ✅ + 🆕
- Day, week and agenda views. Create, reschedule (drag on tablet or web) and cancel.
- **Recurring sessions** ✅ (`session_series` exists): "every Mon/Thu 07:00 for
  12 weeks", with edit-this vs edit-all.
- Session types with colour, duration, capacity and credit cost ✅.
- Semi-private and small-group sessions with per-attendee outcomes ✅.
- Conflict warning using the existing buffer constraint ✅.
- Availability and working hours 🆕, used to suggest open slots when booking.
- **Device calendar export** 🆕 (ICS feed subscribed from Google or Apple):
  one-way, read-only. Two-way sync stays out of scope.

### C. Clients (CRM) ♻️
- Roster with search, filters (active, low credits, inactive 14d+, has
  programme, tag) and sort.
- **Client profile redesigned as tabs:** Overview · Training · Progress · Billing · Notes.
  - *Overview:* credits, next session, current programme and week, adherence %,
    flags (PAR-Q needs clearance, waiver unsigned, injury notes).
  - *Training:* assigned programme, workout history, per-lift PR and
    progression charts ✅ (endpoint exists).
  - *Progress:* weight and body-fat charts, circumferences, **progress photos**
    side by side with a date slider ✅ (media exists, never shown in the app).
  - *Billing:* packs, credit ledger, invoices, payments.
  - *Notes:* timestamped coach notes 🆕 and an injury log 🆕.
- **Intake flow** ♻️: new client → contact → goals 🆕 → PAR-Q ✅ → waiver
  signature ✅ → starting measurements → first package. Each step can be skipped.
- Goals 🆕: e.g. "Squat 100 kg by March", "−6 kg". Shown as progress bars.
- Tags ✅ (server has them; the app doesn't).

### D. Programming (the biggest new surface) ✅ + 🆕
- **Programme builder** ✅ server / 🆕 UI: Programme → Blocks (weeks) → Days →
  Exercises with sets, reps range, load, RPE/RIR, rest, tempo, superset group
  and notes. Phone-friendly, and a two-pane layout on tablet and web.
- **Template library** 🆕: ships with 8–10 starter templates (Full-body 3×,
  Upper/Lower 4×, PPL, Beginner strength, Hypertrophy block, Fat-loss
  circuit, Mobility, Return-from-injury). Duplicate a template, tweak it, assign it.
- **Assign to client** ✅ with start date. The app knows "Week 3, Day B" for
  every client.
- **Progression rules** 🆕 per exercise: "+2.5 kg when all sets hit the top
  of the range", "+1 rep per week", or manual. Applied when the next session
  is started, shown as a suggestion the trainer accepts or overrides.
  Never applied silently.
- **Exercise library** ♻️: 66 seeded ✅ plus custom ones. Filter by movement
  pattern, equipment and muscle. Each has instructions and a video link ✅.
- **Superset and circuit support** in builder and logger (`superset_group` exists).

### E. Workout logger ♻️
- Opens on the programmed day with targets pre-filled as ghosts. The "logged
  but not completed" rule stays.
- One-hand set entry: big steppers for load and reps, RPE chips, tap to complete.
- **Rest timer** 🆕 that starts on set completion, with haptic and sound at
  the end, and keeps running across screens.
- **Plate calculator** 🆕 and warm-up set generator 🆕.
- Swap exercise mid-session (keeps the prescription slot) 🆕.
- **PR detection** 🆕: a "New PR" badge for estimated 1RM, rep PR or volume PR.
- Session summary 🆕: volume, PRs, duration and trainer notes, with an optional
  share link to the client.

### F. Progress reports 🆕
- One-tap **monthly client report**: sessions attended, adherence, PRs,
  measurement deltas, photos and trainer comment. Shared as a link or PDF
  (same template, per ADR 0006).
- Turns an invisible service into something a client sees, which is the
  retention lever.

### G. Business ♻️ + ✅
- **Money tab redesigned** as Overview · Invoices · Payments · Expenses.
  - Overview: earned this month (recognised revenue), cash collected,
    outstanding, unearned (prepaid) balance, and a 6-month trend.
  - **Reports** ✅ server / 🆕 UI: P&L, receivables ageing, trial balance.
  - **Expenses** 🆕 UI (accounts and posting rules already seeded): amount,
    category, method, receipt photo.
  - **CSV/tax export** 🆕 per period.
- Packages and pricing catalogue 🆕: define "10-pack €500" once, sell it in two taps.
- Invoice PDF ✅ (ADR 0006) and WhatsApp share ✅.
- **Recurring invoice drafts and package-expiry sweeps**: needs the worker
  (STATUS.md item 1) 🆕.

### H. Retention and insights 🆕
- **At-risk list:** low credits ✅, inactive N days, falling attendance, pack
  expiring soon.
- **Practice insights:** retention rate, average client lifetime, revenue per
  client, busiest hours, no-show rate. Small, honest charts, not a chart wall.
- **Message templates:** "Your pack has 2 sessions left", "Invoice reminder",
  "Missed you today". Opens WhatsApp or SMS with the text filled in.
  No messaging backend needed.

### I. Settings and account ♻️
- Business profile: logo, name, currency, IBAN and payment instructions ✅,
  invoice terms.
- Working hours, session buffer, low-credit threshold ✅, cancellation policy.
- Theme (system, light, dark) and units (kg/lb).
- Data export (all data as CSV/JSON) and account deletion.

### Kept exactly as it is
Offline-first sync, the outbox, the ledger rules, RLS, gap-free invoice
numbers, demo mode. The redesign changes what the trainer sees, not those guarantees.

---

## 3. Navigation

Five tabs with icons and labels, and a context-aware **+** button:

| Tab | Holds |
|---|---|
| **Today** | Timeline, nudges, session prep |
| **Calendar** | Day / week / agenda, booking |
| **Clients** | Roster, profiles, intake |
| **Programs** | Templates, builder, exercise library |
| **Business** | Money, reports, expenses, insights |

The **+** opens a quick-action sheet: *Book session · New client · Log
workout · Record payment · Sell package · Add expense*.

Settings and Sync move under the avatar at the top of Today. The sync
badge stays visible in the header.

On **tablet and web (≥ 900 px)** the tab bar becomes a left sidebar, and list
screens turn into list + detail panes. The programme builder is designed for
this layout first.

---

## 4. Design system: "Studio"

The current look is a gym-floor tool. v2 should feel like a premium consumer
fitness product, and still read in bad light.

- **Themes:** light and dark, following the system by default. Every colour
  is a semantic token (`bg`, `surface`, `text`, `textMuted`, `primary`,
  `success`, `warning`, `danger`, `info`) with a value per theme.
- **Palette:** off-white `#F7F7F5` / ink `#0E0F12` grounds, and one primary,
  **Electric Indigo `#4F46E5`** (`#7C83FF` on dark). Green, amber and red
  appear only as status, never as decoration.
  *(Alternative if you'd rather keep energy: Coral `#FF5A36`. Open question 2.)*
- **Type:** Inter (bundled, works offline). Tabular numerals for every metric
  so columns line up. Scale: 34 / 28 / 22 / 17 / 15 / 13 / 11.
- **Icons:** Lucide via `lucide-react-native`, one stroke weight, always next
  to a label in navigation.
- **Shape:** 8-pt grid, 12 px card radius, 10 px control radius, soft
  elevation on light and hairline borders on dark.
- **Motion:** 150–250 ms ease-out. Haptics on set complete, attendance mark and
  timer end.
- **Charts:** a small in-house set on `react-native-svg` (sparkline, line,
  bar, ring), built to the dataviz guidance: direct labels, no legends where
  avoidable, colour-blind-safe.
- **Components:** AppShell, Header, Tab bar / Sidebar, Card, ListRow,
  Avatar, Badge, Chip, Button (primary/secondary/ghost/destructive),
  IconButton, Segmented, Sheet (bottom sheet), Stepper, NumberPad, TextField,
  SearchField, EmptyState, Skeleton, Toast, Charts, StatTile, ProgressRing.
- **Accessibility:** 44 pt minimum targets (56 pt in the logger), AA contrast
  in both themes, Dynamic Type up to 130%, screen-reader labels on every
  icon button.

---

## 5. Build slices

Each slice ships working, tested, and visible in demo mode before the next
starts. "Server" means Go plus a migration, OpenAPI, sync collections and tests.

| # | Slice | Server work | Size |
|---|---|---|---|
| 1 | **Design system + app shell:** tokens, themes, fonts, icons, components, 5-tab nav / sidebar, quick-action sheet; existing screens re-skinned | none | L |
| 2 | **Today + client profile redesign:** timeline, nudges, session prep, tabbed profile, charts, photos | none (reads existing data) | L |
| 3 | **Calendar and booking:** views, create, reschedule, recurring series, session types | expose series and session edit through sync push | L |
| 4 | **Programme builder + templates + assign** | sync program blocks/days/exercises; template seed; push ops for builder edits | XL |
| 5 | **Logger v2:** programmed targets, rest timer, plate calc, swap, PRs, summary | progression-rule columns | L |
| 6 | **Business:** money overview, reports, expenses, package catalogue, CSV export | report endpoints, expenses, package catalogue | L |
| 7 | **Intake, goals, notes, injuries, tags** | goals, notes, injuries tables | M |
| 8 | **Share links:** workout card, monthly progress report, PDF | token pages like invoices | M |
| 9 | **Worker:** recurring invoices, pack expiry, insights | `cmd/worker` with leader lock | M |
| 10 | **Retention insights + message templates + settings + ICS feed** | insights endpoint, ICS endpoint | M |

Slices 1–2 are pure client work, so you'll see the new look quickly.
Slice 4 is the one that most justifies the price.

## 6. Out of scope for v2

Client accounts or app · card payments · in-app messaging · nutrition
tracking · two-way calendar sync · multi-trainer teams · wearables.
Each could be a later tier. Teams in particular is the natural "$40 Studio plan".

## 7. Progress

| # | Slice | State |
|---|---|---|
| 1 | Design system + shell | **Done** — native Liquid Glass tab bar on iOS, glass fallback on web/Android, light/dark |
| 2 | Today + client profile | **Done** — nudges, session prep, tabbed profile, charts, message templates |
| 3 | Calendar and booking | **Done** — day/agenda views, book/repeat/reschedule/cancel offline, server ops + tests |
| 4 | Programme builder + templates | **Done** — builder, 9 templates, assign, pull carries structure, server ops + tests |
| 5 | Logger v2 | **Mostly** — programmed day, progression suggestions, rest timer, plates, warm-ups, PRs, summary. Swap-exercise not yet |
| 6–10 | Business reports, intake/goals, share links, worker, insights | Not started (Business tab shows device-side figures and the server's earned revenue) |

## 8. Open questions (answered)

The questions below were settled as recorded in the decisions table at the top.


1. **Share links (workout card, progress report):** OK to include? They're
   read-only pages, not client accounts.
2. **Primary colour:** Electric Indigo (calm, premium) or Coral (energetic)?
3. **Units:** kg only, or a kg/lb toggle from day one?
4. **Starter templates:** fine for me to write them, or do you have your own
   programmes to load?
5. **Pricing tiers:** single $20 plan, or a cheaper tier capped at N clients?
   (This affects whether we build client limits and plan gating now.)
