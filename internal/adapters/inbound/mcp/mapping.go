package mcp

import (
	"github.com/claudioed/labor-performance/internal/application/ports"
	"github.com/claudioed/labor-performance/internal/application/usecases"
	"github.com/claudioed/labor-performance/internal/domain/standard"
)

// Compact projections of the read models -- the MCP-facing shape, kept
// separate from both the domain aggregates and the HTTP DTOs (dto.go in
// internal/adapters/inbound/http) even though the JSON field names happen
// to match: this package must never import the http adapter, and a future
// divergence between the two surfaces should not require touching this
// file's own shape.

// taskTypeBreakdownDTO is one TaskType's slice of a scorecardDTO.
type taskTypeBreakdownDTO struct {
	TaskCount         int      `json:"taskCount"`
	MeanEfficiencyPct *float64 `json:"meanEfficiencyPct"`
}

// scorecardDTO is the get_associate_scorecard tool's output and the
// scorecard resource's body.
type scorecardDTO struct {
	AssociateId       string                          `json:"associateId"`
	TaskCount         int                             `json:"taskCount"`
	MeanEfficiencyPct *float64                        `json:"meanEfficiencyPct"`
	ByTaskType        map[string]taskTypeBreakdownDTO `json:"byTaskType"`
	// Trend is one of IMPROVING, DECLINING, STABLE, or INSUFFICIENT_DATA
	// -- see performance.ClassifyTrend. Always present, never omitted.
	Trend string `json:"trend"`
	// CoachingFlag is true iff this associate's most recent 3 scored
	// tasks were all below the coaching floor. Visibility only, never
	// an automated action -- an agent reading this tool's output must
	// treat it the same way: a signal to surface to a human, not a
	// trigger to act on autonomously.
	CoachingFlag bool `json:"coachingFlag"`
}

func toScorecardDTO(sc ports.Scorecard) scorecardDTO {
	byTaskType := make(map[string]taskTypeBreakdownDTO, len(sc.ByTaskType))
	for tt, b := range sc.ByTaskType {
		byTaskType[string(tt)] = taskTypeBreakdownDTO{
			TaskCount:         b.TaskCount,
			MeanEfficiencyPct: b.MeanEfficiencyPct,
		}
	}
	return scorecardDTO{
		AssociateId:       string(sc.AssociateId),
		TaskCount:         sc.TaskCount,
		MeanEfficiencyPct: sc.MeanEfficiencyPct,
		ByTaskType:        byTaskType,
		Trend:             string(sc.Trend),
		CoachingFlag:      sc.CoachingFlag,
	}
}

// taskTypePerformanceDTO is the get_task_type_performance tool's output.
type taskTypePerformanceDTO struct {
	TaskType          string   `json:"taskType"`
	TaskCount         int      `json:"taskCount"`
	MeanEfficiencyPct *float64 `json:"meanEfficiencyPct"`
	// MeanActualSeconds is the real measured mean duration for this
	// TaskType, independent of whether an engineered standard exists --
	// see ports.TaskTypePerformance's own doc comment for the full
	// distinction from MeanEfficiencyPct.
	MeanActualSeconds *float64 `json:"meanActualSeconds"`
}

func toTaskTypePerformanceDTO(p ports.TaskTypePerformance) taskTypePerformanceDTO {
	return taskTypePerformanceDTO{
		TaskType:          string(p.TaskType),
		TaskCount:         p.TaskCount,
		MeanEfficiencyPct: p.MeanEfficiencyPct,
		MeanActualSeconds: p.MeanActualSeconds,
	}
}

// utilizationDTO is the get_task_type_utilization tool's output.
type utilizationDTO struct {
	TaskType       string `json:"taskType"`
	Associates     int    `json:"associates"`
	WindowSeconds  int64  `json:"windowSeconds"`
	TaskSeconds    int64  `json:"taskSeconds"`
	IdleSeconds    int64  `json:"idleSeconds"`
	OpenGapSeconds int64  `json:"openGapSeconds"`
	// UtilizationPct is nil when there was nothing to compute a share
	// of over the window -- never a fabricated number.
	UtilizationPct *float64 `json:"utilizationPct"`
}

func toUtilizationDTO(r usecases.UtilizationResult) utilizationDTO {
	return utilizationDTO{
		TaskType:       string(r.TaskType),
		Associates:     r.Associates,
		WindowSeconds:  r.WindowSeconds,
		TaskSeconds:    r.TaskSeconds,
		IdleSeconds:    r.IdleSeconds,
		OpenGapSeconds: r.OpenGapSeconds,
		UtilizationPct: r.UtilizationPct,
	}
}

// standardDTO is the get_labor_standard tool's output.
type standardDTO struct {
	TaskType        string  `json:"taskType"`
	ExpectedSeconds int64   `json:"expectedSeconds"`
	EffectiveFrom   string  `json:"effectiveFrom"`
	EffectiveTo     *string `json:"effectiveTo,omitempty"`
}

func toStandardDTO(s *standard.LaborStandard) standardDTO {
	dto := standardDTO{
		TaskType:        string(s.TaskType()),
		ExpectedSeconds: s.ExpectedSeconds(),
		EffectiveFrom:   s.EffectiveFrom().UTC().Format("2006-01-02T15:04:05Z07:00"),
	}
	if to := s.EffectiveTo(); to != nil {
		formatted := to.UTC().Format("2006-01-02T15:04:05Z07:00")
		dto.EffectiveTo = &formatted
	}
	return dto
}
