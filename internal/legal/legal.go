// Package legal serves the privacy policy and terms of service.
//
// Both app stores require a privacy policy at a stable URL, and the app links
// to both from sign-up and settings. They are served by the API, in the style
// of the shared invoice page, so they need no separate website and are live
// wherever the API is.
//
// The text is a starting point, not legal advice: it describes what this
// software actually does with data, and the operator's lawyer should review
// it before launch. Who the operator is — the company, its address, its
// contact and the law that governs — comes from configuration, which
// production refuses to start without, so a published page never shows a
// placeholder.
package legal

import (
	"embed"
	"html/template"
	"net/http"

	"github.com/go-chi/chi/v5"

	"github.com/NewMux/mtdrb_go/internal/platform/logger"
)

//go:embed pages/*.html
var pages embed.FS

var templates = template.Must(template.ParseFS(pages, "pages/*.html"))

// Operator is who publishes the service, as the pages name them.
type Operator struct {
	Entity       string // the legal entity, e.g. "CoachPulse FZ-LLC"
	Address      string // its registered address
	ContactEmail string // where privacy requests and notices go
	Jurisdiction string // the law that governs the terms, e.g. "the Emirate of Dubai, UAE"
	Effective    string // the date the current text took effect, e.g. "1 October 2026"
	// PurgeDays is how long a deleted practice waits before it is removed.
	PurgeDays int
}

// withPlaceholders fills what is missing, for development only; production
// configuration refuses to start without the real values.
func (o Operator) withPlaceholders() Operator {
	fill := func(v *string, placeholder string) {
		if *v == "" {
			*v = placeholder
		}
	}
	fill(&o.Entity, "[Operator legal name]")
	fill(&o.Address, "[Registered address]")
	fill(&o.ContactEmail, "privacy@example.com")
	fill(&o.Jurisdiction, "[Governing law]")
	fill(&o.Effective, "[Effective date]")
	if o.PurgeDays <= 0 {
		o.PurgeDays = 30
	}
	return o
}

// Routes serves /privacy and /terms.
func Routes(o Operator) http.Handler {
	o = o.withPlaceholders()
	r := chi.NewRouter()
	r.Get("/privacy", page("privacy.html", o))
	r.Get("/terms", page("terms.html", o))
	return r
}

func page(name string, o Operator) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		w.Header().Set("Cache-Control", "public, max-age=3600")
		if err := templates.ExecuteTemplate(w, name, o); err != nil {
			logger.From(r.Context()).ErrorContext(r.Context(), "render legal page", "page", name, "error", err)
		}
	}
}
