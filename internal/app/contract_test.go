package app_test

import (
	"context"
	"log/slog"
	"net/http"
	"os"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"
	"gopkg.in/yaml.v3"

	"github.com/NewMux/mtdrb_go/internal/api"
	"github.com/NewMux/mtdrb_go/internal/app"
	"github.com/NewMux/mtdrb_go/internal/config"
	"github.com/NewMux/mtdrb_go/internal/media"
)

type nopPresigner struct{}

func (nopPresigner) PresignPut(context.Context, string, string, int64, time.Duration) (string, error) {
	return "", nil
}
func (nopPresigner) PresignGet(context.Context, string, time.Duration) (string, error) {
	return "", nil
}
func (nopPresigner) Delete(context.Context, string) error { return nil }
func (nopPresigner) Stat(context.Context, string) (media.StoredObject, error) {
	return media.StoredObject{}, media.ErrNotStored
}

// Every route the server registers is documented in api/openapi.yaml, and
// every documented operation exists.
//
// Five of the bugs STATUS.md records were one side of the wire honouring a
// format the other side had invented, because nothing held either to the spec.
// The client's types are now generated from this file, so a route missing from
// it is a route the client cannot call correctly.
func TestEveryRouteIsInTheOpenAPISpec(t *testing.T) {
	services := app.New(app.Options{
		JWTSigningKey: []byte(strings.Repeat("k", 32)),
		ColumnKey:     []byte(strings.Repeat("c", 32)),
		Presigner:     nopPresigner{},
	})
	srv := api.New(config.Config{}, nil, slog.New(slog.DiscardHandler), services.Handlers())
	routes, ok := srv.Handler().(chi.Routes)
	if !ok {
		t.Fatal("the server's handler is not a chi router")
	}

	registered := map[string]bool{}
	if err := chi.Walk(routes, func(method, route string, _ http.Handler, _ ...func(http.Handler) http.Handler) error {
		registered[operation(method, route)] = true
		return nil
	}); err != nil {
		t.Fatal(err)
	}

	documented := specOperations(t)

	var undocumented, stale []string
	for op := range registered {
		if !documented[op] {
			undocumented = append(undocumented, op)
		}
	}
	for op := range documented {
		if !registered[op] {
			stale = append(stale, op)
		}
	}
	sort.Strings(undocumented)
	sort.Strings(stale)
	for _, op := range undocumented {
		t.Errorf("registered but not in api/openapi.yaml: %s", op)
	}
	for _, op := range stale {
		t.Errorf("in api/openapi.yaml but not registered: %s", op)
	}
}

// operation normalises a chi route to the spec's form: a mounted subrouter's
// root arrives with a trailing slash, and the spec writes it without.
func operation(method, route string) string {
	route = strings.ReplaceAll(route, "/*/", "/")
	if len(route) > 1 {
		route = strings.TrimSuffix(route, "/")
	}
	return strings.ToUpper(method) + " " + route
}

func specOperations(t *testing.T) map[string]bool {
	t.Helper()
	raw, err := os.ReadFile("../../api/openapi.yaml")
	if err != nil {
		t.Fatal(err)
	}
	var spec struct {
		Paths map[string]map[string]yaml.Node `yaml:"paths"`
	}
	if err := yaml.Unmarshal(raw, &spec); err != nil {
		t.Fatalf("api/openapi.yaml does not parse: %v", err)
	}
	methods := map[string]bool{"get": true, "post": true, "put": true, "patch": true, "delete": true}
	ops := map[string]bool{}
	for path, item := range spec.Paths {
		for method := range item {
			if methods[method] {
				ops[strings.ToUpper(method)+" "+path] = true
			}
		}
	}
	return ops
}
