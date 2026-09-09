package http

import (
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/claudioed/labor-performance/internal/adapters/inbound/auth"
)

// newAuthReportsRouter builds the reports router with the fleet-standard
// auth middleware in enforce mode (ADR 0011), the way cmd/labor-reports
// wires it when keys are configured. Every /reports/* route requires the
// read scope; /healthz stays open.
func newAuthReportsRouter() http.Handler {
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	return NewReportsRouter(&ReportsHandlers{
		Store: &stubReportStore{},
		Auth: &auth.Middleware{
			Authn:  auth.NewStaticKeyAuth(map[string]auth.Scope{"r-key": auth.ScopeRead, "rw-key": auth.ScopeReadWrite}),
			Mode:   auth.ModeEnforce,
			Logger: logger,
		},
	}, logger)
}

func TestReportsRouterAuth_EnforceTable(t *testing.T) {
	h := newAuthReportsRouter()
	const reportPath = "/reports/performance?from=2026-09-05T00:00:00Z&to=2026-09-06T00:00:00Z"

	cases := []struct {
		name  string
		path  string
		token string
		want  int
	}{
		{"healthz needs no token", "/healthz", "", http.StatusOK},
		{"no token report -> 401", reportPath, "", http.StatusUnauthorized},
		{"bad token report -> 401", reportPath, "nope", http.StatusUnauthorized},
		{"read key report -> 200", reportPath, "r-key", http.StatusOK},
		{"rw key report -> 200", reportPath, "rw-key", http.StatusOK},
		{"no token freshness -> 401", "/reports/performance/freshness", "", http.StatusUnauthorized},
		{"read key freshness -> 200", "/reports/performance/freshness", "r-key", http.StatusOK},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodGet, c.path, nil)
			if c.token != "" {
				req.Header.Set("Authorization", "Bearer "+c.token)
			}
			rec := httptest.NewRecorder()
			h.ServeHTTP(rec, req)
			if rec.Code != c.want {
				t.Fatalf("GET %s token=%q: want %d got %d body=%s", c.path, c.token, c.want, rec.Code, rec.Body.String())
			}
			if rec.Code == http.StatusUnauthorized {
				if rec.Header().Get("WWW-Authenticate") == "" {
					t.Fatal("401 must carry WWW-Authenticate")
				}
				if ct := rec.Header().Get("Content-Type"); ct != "application/problem+json" {
					t.Fatalf("want application/problem+json, got %q", ct)
				}
			}
		})
	}
}

func TestAuthMiddleware_NilIsOff(t *testing.T) {
	// A nil *auth.Middleware must be a pass-through, which is what keeps
	// every other test in this package (no Auth configured) unchanged.
	called := false
	next := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { called = true; w.WriteHeader(http.StatusNoContent) })
	rec := httptest.NewRecorder()
	authMiddleware(nil, nil)(next).ServeHTTP(rec, httptest.NewRequest(http.MethodDelete, "/anything", nil))
	if !called || rec.Code != http.StatusNoContent {
		t.Fatalf("nil middleware must pass through, called=%v code=%d", called, rec.Code)
	}
}

func TestAuthMiddleware_DefaultsProblemBase(t *testing.T) {
	// The composition root never sets ProblemBase; the adapter fills in
	// this service's own RFC 7807 namespace so rejections match every
	// other problem this service emits.
	mw := &auth.Middleware{Authn: auth.NewStaticKeyAuth(nil), Mode: auth.ModeEnforce}
	rec := httptest.NewRecorder()
	authMiddleware(mw, nil)(http.NotFoundHandler()).ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/x", nil))
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("want 401, got %d", rec.Code)
	}
	if body := rec.Body.String(); !strings.Contains(body, problemBaseURI+"unauthenticated") {
		t.Fatalf("problem type must use %s, got %s", problemBaseURI, body)
	}
}
