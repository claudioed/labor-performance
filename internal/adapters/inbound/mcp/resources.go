package mcp

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/claudioed/labor-performance/internal/domain/shared"
)

// scorecardURIScheme is the scheme+authority prefix of the associate
// scorecard resource URI. A concrete resource URI is
// scorecardURIScheme + "<associateId>".
const scorecardURIScheme = "scorecard://labor/"

// registerResources adds the scoped read-model resource. Per the charter,
// resources are bounded-context contracts tied to a decision, not bulk
// dumps: the scorecard resource answers "how is this one associate doing?",
// backed by the same GetAssociateScorecard read model the tool uses.
//
// The resource is registered as a template (scorecard://labor/{associateId})
// so a client can read any associate's scorecard by URI without a
// per-associate registration.
func (d Deps) registerResources(server *mcp.Server, scopeOf func(context.Context) Scope) {
	server.AddResourceTemplate(&mcp.ResourceTemplate{
		URITemplate: scorecardURIScheme + "{associateId}",
		Name:        "associate scorecard",
		Description: "One associate's performance scorecard (task count, mean efficiency, per-task-type breakdown, trend, coaching flag), addressed by associate id, e.g. scorecard://labor/assoc-42.",
		MIMEType:    "application/json",
	}, func(ctx context.Context, req *mcp.ReadResourceRequest) (*mcp.ReadResourceResult, error) {
		uri := req.Params.URI
		if !scopeAllows(scopeOf(ctx), ScopeRead) {
			return nil, fmt.Errorf("resource %q requires read scope", uri)
		}
		associateId, ok := strings.CutPrefix(uri, scorecardURIScheme)
		if !ok || associateId == "" {
			return nil, fmt.Errorf("resource %q is not a valid scorecard URI", uri)
		}
		sc, err := d.GetAssociateScorecard.Execute(ctx, shared.AssociateId(associateId))
		if err != nil {
			return nil, err
		}
		body, err := json.Marshal(toScorecardDTO(sc))
		if err != nil {
			return nil, err
		}
		return &mcp.ReadResourceResult{
			Contents: []*mcp.ResourceContents{{
				URI:      uri,
				MIMEType: "application/json",
				Text:     string(body),
			}},
		}, nil
	})
}
