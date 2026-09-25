package billing

import (
	"html/template"
	"net/http"

	"github.com/NewMux/mtdrb_go/internal/httpx"
	"github.com/NewMux/mtdrb_go/internal/platform/errs"
	"github.com/NewMux/mtdrb_go/internal/platform/logger"
	"github.com/NewMux/mtdrb_go/internal/platform/money"
)

// This is the page a client opens from a WhatsApp message. It has one job:
// show what is owed and exactly how to pay it, on a phone, without an account.
//
// It is also the template PDF export will render when that ships, which is why
// the layout lives in HTML rather than in imperative drawing calls — one
// source, so the emailed PDF and the shared link cannot drift apart.

// formatMoney renders minor units for display. Templates cannot do arithmetic,
// and a currency shown wrong on a bill is worse than no bill — a Kuwaiti
// dinar has three decimals, not two.
func formatMoney(minor int64, currency string) string { return money.Format(minor, currency) }

var invoiceTemplate = template.Must(template.New("invoice").Funcs(template.FuncMap{
	"money": formatMoney,
	"date": func(t any) string {
		type dater interface{ Format(string) string }
		if d, ok := t.(dater); ok {
			return d.Format("2 January 2006")
		}
		return ""
	},
}).Parse(`<!DOCTYPE html>
<html lang="en">
<head>
<meta charset="utf-8">
<meta name="viewport" content="width=device-width, initial-scale=1">
<!-- A shared invoice must not end up in a search index or a proxy cache. -->
<meta name="robots" content="noindex, nofollow, noarchive">
<title>Invoice {{.Number}} — {{.BusinessName}}</title>
<style>
  :root {
    color-scheme: light dark;
    --bg: #f6f7f9; --card: #ffffff; --ink: #14171f; --muted: #5d6470;
    --line: #e4e7ec; --accent: #1f6feb; --warn: #b42318;
  }
  @media (prefers-color-scheme: dark) {
    :root { --bg:#0f1115; --card:#171a21; --ink:#e9ecf1; --muted:#9aa3b2;
            --line:#272c36; --accent:#60a5fa; --warn:#f97066; }
  }
  * { box-sizing: border-box; }
  body { margin:0; padding:24px 16px; background:var(--bg); color:var(--ink);
         font:16px/1.55 -apple-system,BlinkMacSystemFont,"Segoe UI",Roboto,sans-serif; }
  .sheet { max-width:640px; margin:0 auto; background:var(--card);
           border:1px solid var(--line); border-radius:14px; padding:28px; }
  header { display:flex; justify-content:space-between; align-items:flex-start;
           gap:16px; flex-wrap:wrap; margin-bottom:24px; }
  h1 { font-size:20px; margin:0 0 2px; }
  .muted { color:var(--muted); font-size:14px; }
  .status { display:inline-block; padding:3px 10px; border-radius:999px;
            font-size:12px; font-weight:600; text-transform:uppercase;
            letter-spacing:.04em; border:1px solid var(--line); }
  .status.settled { color:#067647; border-color:#067647; }
  .status.overdue { color:var(--warn); border-color:var(--warn); }
  .amount { font-size:30px; font-weight:650; margin:4px 0 0; }
  table { width:100%; border-collapse:collapse; margin:20px 0; font-size:15px; }
  th { text-align:left; font-size:12px; text-transform:uppercase;
       letter-spacing:.04em; color:var(--muted); padding:0 0 8px; font-weight:600; }
  td { padding:10px 0; border-top:1px solid var(--line); vertical-align:top; }
  td.num, th.num { text-align:right; white-space:nowrap; }
  tfoot td { border-top:2px solid var(--line); font-weight:650; }
  .pay { border:1px solid var(--line); border-radius:10px; padding:16px; margin-top:8px; }
  .pay h2 { font-size:13px; text-transform:uppercase; letter-spacing:.04em;
            color:var(--muted); margin:0 0 12px; }
  .method { padding:12px 0; border-top:1px solid var(--line); }
  .method:first-of-type { border-top:0; padding-top:0; }
  .method h3 { font-size:15px; margin:0 0 6px; }
  dl { display:grid; grid-template-columns:auto 1fr; gap:4px 14px; margin:0; font-size:14px; }
  dt { color:var(--muted); }
  dd { margin:0; font-family:ui-monospace,SFMono-Regular,Menlo,monospace; word-break:break-all; }
  .ref { margin-top:14px; padding:12px; border-radius:8px;
         background:color-mix(in srgb, var(--accent) 10%, transparent);
         border:1px solid color-mix(in srgb, var(--accent) 35%, transparent); font-size:14px; }
  footer { margin-top:24px; text-align:center; }
  @media print {
    body { background:#fff; padding:0; }
    .sheet { border:0; max-width:none; }
  }
</style>
</head>
<body>
<main class="sheet">
  <header>
    <div>
      <h1>{{.BusinessName}}</h1>
      <div class="muted">Invoice {{.Number}}{{if .IssueDate}} · {{date .IssueDate}}{{end}}</div>
      <div class="muted">For {{.ClientName}}</div>
    </div>
    <div style="text-align:right">
      {{if eq (printf "%s" .Status) "settled"}}
        <span class="status settled">Paid</span>
      {{else if .IsOverdue}}
        <span class="status overdue">Overdue</span>
      {{else}}
        <span class="status">{{.Status}}</span>
      {{end}}
      <p class="amount">{{money .BalanceMinor .Currency}}</p>
      {{if .DueDate}}<div class="muted">Due {{date .DueDate}}</div>{{end}}
    </div>
  </header>

  <table>
    <thead>
      <tr><th>Description</th><th class="num">Qty</th><th class="num">Unit</th><th class="num">Amount</th></tr>
    </thead>
    <tbody>
      {{range .Lines}}
      <tr>
        <td>{{.Description}}</td>
        <td class="num">{{.Quantity}}</td>
        <td class="num">{{money .UnitPriceMinor $.Currency}}</td>
        <td class="num">{{money .AmountMinor $.Currency}}</td>
      </tr>
      {{end}}
    </tbody>
    <tfoot>
      <tr><td colspan="3">Total</td><td class="num">{{money .TotalMinor .Currency}}</td></tr>
      {{if gt .PaidMinor 0}}
      <tr><td colspan="3">Paid</td><td class="num">{{money .PaidMinor .Currency}}</td></tr>
      <tr><td colspan="3">Balance</td><td class="num">{{money .BalanceMinor .Currency}}</td></tr>
      {{end}}
    </tfoot>
  </table>

  {{if .Notes}}<p class="muted">{{.Notes}}</p>{{end}}

  {{if and .Instructions (gt .BalanceMinor 0)}}
  <section class="pay">
    <h2>How to pay</h2>
    {{range .Instructions.Methods}}
    <div class="method">
      <h3>{{.Label}}</h3>
      <dl>
        {{with .Details.AccountHolder}}<dt>Account holder</dt><dd>{{.}}</dd>{{end}}
        {{with .Details.BankName}}<dt>Bank</dt><dd>{{.}}</dd>{{end}}
        {{with .Details.IBAN}}<dt>IBAN</dt><dd>{{.}}</dd>{{end}}
        {{with .Details.SwiftBIC}}<dt>SWIFT/BIC</dt><dd>{{.}}</dd>{{end}}
        {{with .Details.AccountNumber}}<dt>Account</dt><dd>{{.}}</dd>{{end}}
        {{with .Details.RoutingNumber}}<dt>Routing</dt><dd>{{.}}</dd>{{end}}
        {{with .Details.Handle}}<dt>Send to</dt><dd>{{.}}</dd>{{end}}
      </dl>
      {{with .Instructions}}<p class="muted" style="margin:8px 0 0">{{.}}</p>{{end}}
    </div>
    {{end}}
    {{with .Instructions.Reference}}
    <p class="ref">Please quote <strong>{{.}}</strong> as the payment reference.</p>
    {{end}}
  </section>
  {{end}}

  <footer class="muted">Questions about this invoice? Reply to {{.BusinessName}} directly.</footer>
</main>
</body>
</html>
`))

// renderInvoicePage writes the client-facing invoice page.
func renderInvoicePage(w http.ResponseWriter, r *http.Request, invoice PublicInvoice) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	// The link is an unguessable secret, so the page must not be cached by a
	// proxy or indexed, and must not leak its own URL through a Referer.
	w.Header().Set("Cache-Control", "no-store, private")
	w.Header().Set("X-Robots-Tag", "noindex, nofollow")
	w.Header().Set("Referrer-Policy", "no-referrer")

	if err := invoiceTemplate.Execute(w, invoice); err != nil {
		logger.From(r.Context()).ErrorContext(r.Context(), "rendering invoice page failed")
		httpx.Error(w, r, errs.Internal(err, "render invoice"))
	}
}
