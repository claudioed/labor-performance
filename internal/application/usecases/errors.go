// Package usecases implements the application's use cases: one struct per
// use case, depending only on the domain and on application/ports. No use
// case imports an adapter package.
package usecases

import "errors"

var (
	// ErrStandardNotFound is returned by GetStandard when no active
	// standard exists for a TaskType.
	ErrStandardNotFound = errors.New("no active labor standard for this task type")

	// ErrAssociateNotFound is returned by GetAssociateScorecard when the
	// associate has zero recorded TaskPerformance rows — this service has
	// literally never heard of them. Distinct from an associate with 1+
	// rows but an all-nil EfficiencyPct, which returns 200 with
	// meanEfficiencyPct: null.
	ErrAssociateNotFound = errors.New("no task performance recorded for this associate")
)
