package mcp

import (
	"context"
	"fmt"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/trace"

	"github.com/claudioed/labor-performance/internal/application/usecases"
	"github.com/claudioed/labor-performance/internal/domain/shared"
)

// tracerName is the OTel instrumentation scope for MCP tool spans.
const tracerName = "github.com/claudioed/labor-performance/internal/adapters/inbound/mcp"

// Deps is everything the MCP tools need, injected by the composition root.
// It carries the SAME read use cases the HTTP adapter uses; the adapter
// never constructs an outbound adapter itself.
//
// labor-performance exposes no write use case over MCP: DefineStandard and
// RecordTaskPerformance are driven by an operator (chi HTTP) and by
// fulfillment-execution's TaskCompleted event (Kafka consumer)
// respectively -- neither is a decision an MCP-calling agent should make on
// this context's behalf, so neither gets wired here at all.
type Deps struct {
	// GetAssociateScorecard is the existing read use case behind
	// get_associate_scorecard and the scorecard resource, reused unchanged.
	GetAssociateScorecard *usecases.GetAssociateScorecard
	// GetTaskTypePerformance is the existing read use case behind
	// get_task_type_performance.
	GetTaskTypePerformance *usecases.GetTaskTypePerformance
	// GetStandard is the existing read use case behind get_labor_standard.
	GetStandard *usecases.GetStandard
}

// --- get_associate_scorecard --------------------------------------------------

type associateScorecardInput struct {
	AssociateId string `json:"associateId" jsonschema:"the id of the associate whose scorecard to return"`
}

func (d Deps) getAssociateScorecard(ctx context.Context, in associateScorecardInput) (scorecardDTO, error) {
	if in.AssociateId == "" {
		return scorecardDTO{}, fmt.Errorf("associateId is required")
	}
	sc, err := d.GetAssociateScorecard.Execute(ctx, shared.AssociateId(in.AssociateId))
	if err != nil {
		return scorecardDTO{}, err
	}
	return toScorecardDTO(sc), nil
}

// --- get_task_type_performance -------------------------------------------------

type taskTypePerformanceInput struct {
	TaskType string `json:"taskType" jsonschema:"the task type to report on: PICK, PACK, or SLAM"`
}

func (d Deps) getTaskTypePerformance(ctx context.Context, in taskTypePerformanceInput) (taskTypePerformanceDTO, error) {
	taskType, err := shared.NewTaskType(in.TaskType)
	if err != nil {
		return taskTypePerformanceDTO{}, err
	}
	p, err := d.GetTaskTypePerformance.Execute(ctx, taskType)
	if err != nil {
		return taskTypePerformanceDTO{}, err
	}
	return toTaskTypePerformanceDTO(p), nil
}

// --- get_labor_standard ---------------------------------------------------------

type laborStandardInput struct {
	TaskType string `json:"taskType" jsonschema:"the task type whose currently-active engineered labor standard to return: PICK, PACK, or SLAM"`
}

func (d Deps) getLaborStandard(ctx context.Context, in laborStandardInput) (standardDTO, error) {
	taskType, err := shared.NewTaskType(in.TaskType)
	if err != nil {
		return standardDTO{}, err
	}
	s, err := d.GetStandard.Execute(ctx, taskType)
	if err != nil {
		return standardDTO{}, err
	}
	return toStandardDTO(s), nil
}

// --- registration -------------------------------------------------------------

// registerTools adds every tool to the server, each wrapped so its handler
// runs inside an OTel span named "mcp.tool <name>".
//
// labor-performance exposes no write use case over MCP (see Deps' own doc
// comment): every registered tool is a read tool.
func (d Deps) registerTools(server *mcp.Server) {
	readOnly := true

	addTool(server, &mcp.Tool{
		Name:        "get_associate_scorecard",
		Description: "Return one associate's performance scorecard: task count, mean efficiency percent, a per-task-type breakdown, and a trend/coaching-flag signal computed over their most recent tasks. Use it to answer 'how is this associate doing' questions. The coaching flag is a visibility signal only, never an automated action -- surface it to a human, do not act on it autonomously.",
		Annotations: &mcp.ToolAnnotations{ReadOnlyHint: readOnly},
	}, d.getAssociateScorecard)

	addTool(server, &mcp.Tool{
		Name:        "get_task_type_performance",
		Description: "Return the fleet-wide (all-associates) performance read model for one task type (PICK, PACK, or SLAM): task count, mean efficiency percent, and the real measured mean duration. Use it to answer 'how is this task type performing across the whole floor' questions, independent of any single associate.",
		Annotations: &mcp.ToolAnnotations{ReadOnlyHint: readOnly},
	}, d.getTaskTypePerformance)

	addTool(server, &mcp.Tool{
		Name:        "get_labor_standard",
		Description: "Return the currently-active engineered labor standard (expected seconds) for one task type. Use it to answer 'what is the target pace for this task type' questions, e.g. before judging whether an observed pace is fast or slow.",
		Annotations: &mcp.ToolAnnotations{ReadOnlyHint: readOnly},
	}, d.getLaborStandard)
}

// addTool registers one tool. It centralises the cross-cutting concern
// every tool shares: a span per call, and mapping a handler error onto the
// span before returning it.
func addTool[In, Out any](
	server *mcp.Server,
	tool *mcp.Tool,
	handle func(context.Context, In) (Out, error),
) {
	mcp.AddTool(server, tool, func(ctx context.Context, _ *mcp.CallToolRequest, in In) (*mcp.CallToolResult, Out, error) {
		var zero Out
		ctx, span := otel.Tracer(tracerName).Start(ctx, "mcp.tool "+tool.Name,
			trace.WithAttributes(
				attribute.String("mcp.tool.name", tool.Name),
			),
		)
		defer span.End()

		out, err := handle(ctx, in)
		if err != nil {
			span.SetStatus(codes.Error, err.Error())
			return nil, zero, err
		}
		return nil, out, nil
	})
}
