package mcp

import (
	"context"
	"net/http"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// scopeKey is the context key under which the authenticated scope is carried
// from the auth middleware into tool/resource handlers.
type scopeKey struct{}

// scopeFromContext returns the scope stored by the auth middleware, or the
// empty scope if none is present (which scopeAllows treats as unauthorized).
func scopeFromContext(ctx context.Context) Scope {
	if s, ok := ctx.Value(scopeKey{}).(Scope); ok {
		return s
	}
	return ""
}

// NewServer builds the MCP server for this bounded context with every read
// tool and the scoped scorecard resource registered. Handlers read the
// authenticated scope from their context (placed there by Handler's
// middleware).
//
// labor-performance is a pure read-side reporter to the rest of the fleet —
// it consumes fulfillment-execution's TaskCompleted but exposes no write
// use case of its own for an agent to call, so no write tool is registered.
func NewServer(deps Deps) *mcp.Server {
	server := mcp.NewServer(
		&mcp.Implementation{Name: "labor-performance-mcp", Version: "1.0.0"},
		&mcp.ServerOptions{
			Instructions: "Read-only access to labor-performance: per-associate scorecards (trend, coaching flag), per-task-type fleet performance, and active engineered labor standards. Start with the review_associate_performance prompt.",
		},
	)

	deps.registerTools(server, scopeFromContext)
	deps.registerResources(server, scopeFromContext)
	deps.registerPrompts(server, scopeFromContext)

	return server
}

// Handler returns the Streamable HTTP handler for the MCP server, wrapped in
// the auth middleware. Every request must carry a valid bearer key; the scope
// it grants is placed in the request context for handlers to enforce per-tool.
//
// This is the single seam described in ADR-0009 ("MCP inbound adapter"):
// replacing StaticKeyAuth with an OAuth 2.1 resource-server Authenticator
// changes only what is passed here, not any handler.
func Handler(server *mcp.Server, auth Authenticator) http.Handler {
	streamable := mcp.NewStreamableHTTPHandler(func(*http.Request) *mcp.Server { return server }, nil)

	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		scope, ok := auth.Authenticate(r)
		if !ok {
			// Signal how to authenticate without leaking any detail about why
			// the credential failed.
			w.Header().Set("WWW-Authenticate", `Bearer realm="labor-performance-mcp"`)
			http.Error(w, "unauthorized", http.StatusUnauthorized)
			return
		}
		ctx := context.WithValue(r.Context(), scopeKey{}, scope)
		streamable.ServeHTTP(w, r.WithContext(ctx))
	})
}
