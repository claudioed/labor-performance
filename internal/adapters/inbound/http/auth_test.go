package http_test

import (
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/claudioed/labor-performance/internal/adapters/inbound/auth"
	inboundhttp "github.com/claudioed/labor-performance/internal/adapters/inbound/http"
	"github.com/claudioed/labor-performance/internal/adapters/outbound/events"
	"github.com/claudioed/labor-performance/internal/adapters/outbound/memory"
	"github.com/claudioed/labor-performance/internal/application/usecases"
)

const (
	testReadKey = "r-key"
	testRWKey   = "rw-key"
)

// newAuthRouter builds the OLTP router with the fleet-standard auth
// middleware in enforce mode (ADR 0011), the way cmd/labor wires it when
// keys are configured. The rest of the package's tests build the router
// with no Auth at all and so keep running in off mode — this file is the
// only place the 401/403 branches are exercised end-to-end.
func newAuthRouter(t *testing.T, mode auth.Mode) http.Handler {
	t.Helper()
	standards := memory.NewStandardRepo()
	performances := memory.NewPerformanceRepo()
	clock := memory.FixedClock{At: time.Date(2026, 9, 7, 12, 0, 0, 0, time.UTC)}
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))

	server := &inboundhttp.Server{
		DefineStandard:         &usecases.DefineStandard{Standards: standards, Events: events.NewLogPublisher(nil), Clock: clock},
		GetStandard:            &usecases.GetStandard{Standards: standards},
		GetAssociateScorecard:  &usecases.GetAssociateScorecard{Performances: performances},
		GetTaskTypePerformance: &usecases.GetTaskTypePerformance{Performances: performances},
		Auth: &auth.Middleware{
			Authn:  auth.NewStaticKeyAuth(map[string]auth.Scope{testReadKey: auth.ScopeRead, testRWKey: auth.ScopeReadWrite}),
			Mode:   mode,
			Logger: logger,
		},
	}
	return inboundhttp.NewRouter(server, logger, "")
}

func doAuth(t *testing.T, h http.Handler, method, path, token, body string) *httptest.ResponseRecorder {
	t.Helper()
	var reader io.Reader
	if body != "" {
		reader = strings.NewReader(body)
	}
	req := httptest.NewRequest(method, path, reader)
	if body != "" {
		req.Header.Set("Content-Type", "application/json")
	}
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	return rec
}

func TestRouterAuth_EnforceTable(t *testing.T) {
	h := newAuthRouter(t, auth.ModeEnforce)
	const defineBody = `{"taskType":"PICK","expectedSeconds":45}`

	cases := []struct {
		name   string
		method string
		path   string
		token  string
		body   string
		want   int
	}{
		{"healthz needs no token", http.MethodGet, "/healthz", "", "", http.StatusOK},
		{"no token GET -> 401", http.MethodGet, "/task-types/PICK/performance", "", "", http.StatusUnauthorized},
		{"bad token GET -> 401", http.MethodGet, "/task-types/PICK/performance", "nope", "", http.StatusUnauthorized},
		{"read key GET -> 2xx", http.MethodGet, "/task-types/PICK/performance", testReadKey, "", http.StatusOK},
		{"no token POST -> 401", http.MethodPost, "/standards", "", defineBody, http.StatusUnauthorized},
		{"read key POST -> 403", http.MethodPost, "/standards", testReadKey, defineBody, http.StatusForbidden},
		{"rw key POST -> 2xx", http.MethodPost, "/standards", testRWKey, defineBody, http.StatusCreated},
		{"rw key GET -> 2xx", http.MethodGet, "/standards/PICK", testRWKey, "", http.StatusOK},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			rec := doAuth(t, h, c.method, c.path, c.token, c.body)
			if rec.Code != c.want {
				t.Fatalf("%s %s token=%q: want %d got %d body=%s", c.method, c.path, c.token, c.want, rec.Code, rec.Body.String())
			}
			if rec.Code == http.StatusUnauthorized && rec.Header().Get("WWW-Authenticate") == "" {
				t.Fatal("401 must carry WWW-Authenticate")
			}
			if rec.Code == http.StatusUnauthorized || rec.Code == http.StatusForbidden {
				assertAuthProblem(t, rec, c.path)
			}
		})
	}
}

// assertAuthProblem checks the RFC 7807 body an auth rejection carries
// uses this service's own problem-type namespace, not the template's
// placeholder.
func assertAuthProblem(t *testing.T, rec *httptest.ResponseRecorder, path string) {
	t.Helper()
	if ct := rec.Header().Get("Content-Type"); ct != "application/problem+json" {
		t.Fatalf("want application/problem+json, got %q", ct)
	}
	var body map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("body is not JSON: %v", err)
	}
	typ, _ := body["type"].(string)
	if !strings.HasPrefix(typ, "https://errors.labor-performance.warehouse-systems.dev/") {
		t.Fatalf("problem type must use this service's namespace, got %q", typ)
	}
	if body["status"] != float64(rec.Code) || body["instance"] != path {
		t.Fatalf("problem body mismatch: %v", body)
	}
}

func TestRouterAuth_LogModeLetsUnauthenticatedThrough(t *testing.T) {
	h := newAuthRouter(t, auth.ModeLog)
	if rec := doAuth(t, h, http.MethodGet, "/task-types/PICK/performance", "", ""); rec.Code != http.StatusOK {
		t.Fatalf("log mode must pass an unauthenticated GET, got %d", rec.Code)
	}
	if rec := doAuth(t, h, http.MethodPost, "/standards", testReadKey, `{"taskType":"PACK","expectedSeconds":30}`); rec.Code != http.StatusCreated {
		t.Fatalf("log mode must pass an under-scoped POST, got %d", rec.Code)
	}
}

func TestRouterAuth_NilAuthIsOff(t *testing.T) {
	// The existing handler tests construct a Server without Auth; that
	// must keep meaning "no middleware", not "reject everything".
	env := newTestEnv(t, time.Date(2026, 9, 7, 12, 0, 0, 0, time.UTC))
	if rec := doAuth(t, env.handler, http.MethodGet, "/task-types/PICK/performance", "", ""); rec.Code != http.StatusOK {
		t.Fatalf("nil Auth must be off mode, got %d", rec.Code)
	}
}
