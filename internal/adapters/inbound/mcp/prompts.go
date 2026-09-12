package mcp

import (
	"context"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// reviewAssociatePerformanceSOP is the operational standard-operating-
// procedure the review_associate_performance prompt hands to the model. Per
// the charter, prompts encode how to interpret results and what "done"
// means -- they standardise agent behaviour across clients rather than
// leaving procedure implicit.
const reviewAssociatePerformanceSOP = `You are reviewing an associate's labor performance to answer a coaching or staffing question. Use only the MCP tools; never assume a number. This context is READ-ONLY over performance data -- it has no write tool for you to call.

Procedure:
1. Call get_associate_scorecard with the associate's id. This returns their overall task count, mean efficiency percent (nullable -- null means nothing scorable yet, not zero), a per-task-type breakdown, a trend classification, and a coaching flag.
2. To judge whether that efficiency is good or bad in absolute terms, call get_labor_standard for the relevant task type -- it returns the currently-active expected-seconds target. Efficiency percent is already computed against this standard, but the raw target is useful context when explaining a number to a human.
3. To see whether an associate's performance is unusual for their task type, or typical of the whole floor, call get_task_type_performance for that task type and compare its fleet-wide mean against the associate's own.

Interpretation:
- meanEfficiencyPct is null when nothing in the window was scorable (no active standard existed, or no duration was measured) -- treat null as "no data", never as a bad score.
- trend is one of IMPROVING, DECLINING, STABLE, or INSUFFICIENT_DATA -- INSUFFICIENT_DATA is a real, meaningful value (too few recent scored tasks to classify), not an error or an absence.
- coachingFlag is a VISIBILITY signal only: it means the associate's most recent 3 scored tasks were all below the coaching floor. It is never itself grounds for an automated action -- surface it to a human as "this may be worth a conversation," do not escalate, discipline, or reassign based on it alone.

Done means: you have named the specific numbers (efficiency percent, trend, coaching flag) that answer the question, each justified from tool output, with any coaching-flag finding explicitly framed as a signal for human follow-up rather than a conclusion.`

// registerPrompts adds the workflow prompts (operational SOPs).
func (d Deps) registerPrompts(server *mcp.Server) {
	server.AddPrompt(&mcp.Prompt{
		Name:        "review_associate_performance",
		Description: "Standard operating procedure for reviewing an associate's labor performance (scorecard, trend, coaching flag) against the active standard and fleet-wide task-type performance, using the read tools.",
	}, func(ctx context.Context, _ *mcp.GetPromptRequest) (*mcp.GetPromptResult, error) {
		return &mcp.GetPromptResult{
			Description: "How to review an associate's performance: read their scorecard, compare against the active standard and fleet-wide task-type performance, and interpret trend/coaching-flag signals responsibly -- read-only.",
			Messages: []*mcp.PromptMessage{{
				Role:    "user",
				Content: &mcp.TextContent{Text: reviewAssociatePerformanceSOP},
			}},
		}, nil
	})
}
