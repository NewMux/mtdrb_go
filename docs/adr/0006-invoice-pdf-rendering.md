# ADR 0006: Render invoice PDFs from HTML via headless Chromium

Status: Accepted
Date: 2026-09-18

## Context

Invoices must be branded, exportable as PDF, and shareable as a web link. The
web link and the PDF should not drift apart visually.

## Decision

Author one HTML template and render it to PDF with headless Chromium
(`chromedp`). The same template serves the shareable web link.

## Consequences

Good:
- One template, two outputs, guaranteed consistent.
- Trainer branding (logo, colors, payment instructions) is plain HTML/CSS
  rather than imperative PDF layout calls.

Costs:
- The API container needs a Chromium layer, which is a meaningful image-size
  and memory cost.
- Rendering is slower than direct PDF generation and must run off the request
  path for bulk invoice export.

## Fallback

If a slim image becomes a requirement, `go-pdf/fpdf` can replace the renderer
behind the same interface, at the cost of maintaining the layout twice.
