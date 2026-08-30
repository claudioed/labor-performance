// Package http is the inbound chi adapter: DTOs, handlers, routing, and
// domain-error-to-HTTP-status mapping. Domain structs never cross this
// boundary — every response below is a DTO owned by this package.
package http

// defineStandardRequest is the POST /standards request body.
type defineStandardRequest struct {
	TaskType        string `json:"taskType"`
	ExpectedSeconds int64  `json:"expectedSeconds"`
}

// standardResponse is the response body for DefineStandard and
// GetStandard.
type standardResponse struct {
	TaskType        string  `json:"taskType"`
	ExpectedSeconds int64   `json:"expectedSeconds"`
	EffectiveFrom   string  `json:"effectiveFrom"`
	EffectiveTo     *string `json:"effectiveTo,omitempty"`
}

// taskTypeBreakdownResponse is one TaskType's slice of a
// scorecardResponse.
type taskTypeBreakdownResponse struct {
	TaskCount         int      `json:"taskCount"`
	MeanEfficiencyPct *float64 `json:"meanEfficiencyPct"`
}

// scorecardResponse is the GET /associates/{associateId}/scorecard
// response body.
type scorecardResponse struct {
	AssociateId       string                               `json:"associateId"`
	TaskCount         int                                  `json:"taskCount"`
	MeanEfficiencyPct *float64                             `json:"meanEfficiencyPct"`
	ByTaskType        map[string]taskTypeBreakdownResponse `json:"byTaskType"`
	// Trend is one of IMPROVING, DECLINING, STABLE, or
	// INSUFFICIENT_DATA — see performance.ClassifyTrend. Always
	// present, never omitted: INSUFFICIENT_DATA is a real, meaningful
	// value, not an absence.
	Trend string `json:"trend"`
	// CoachingFlag is true iff this associate's most recent 3 scored
	// tasks were all below the coaching floor — see
	// performance.DetectCoachingFlag. Visibility only, never an
	// automated action.
	CoachingFlag bool `json:"coachingFlag"`
}

// taskTypePerformanceResponse is the GET /task-types/{taskType}/performance
// response body.
type taskTypePerformanceResponse struct {
	TaskType          string   `json:"taskType"`
	TaskCount         int      `json:"taskCount"`
	MeanEfficiencyPct *float64 `json:"meanEfficiencyPct"`
	// MeanActualSeconds is the real measured mean duration for this
	// TaskType, independent of whether an engineered standard exists.
	// See ports.TaskTypePerformance's doc comment for the full
	// distinction from MeanEfficiencyPct.
	MeanActualSeconds *float64 `json:"meanActualSeconds"`
}

// problemDetails is the RFC 7807 (Problem Details for HTTP APIs) response
// body used for every error response in this service — the same shape the
// other six services in this fleet emit.
type problemDetails struct {
	Type     string `json:"type"`
	Title    string `json:"title"`
	Status   int    `json:"status"`
	Detail   string `json:"detail"`
	Instance string `json:"instance,omitempty"`
}
