package usecases

import (
	"context"

	"github.com/claudioed/labor-performance/internal/application/ports"
	"github.com/claudioed/labor-performance/internal/domain/shared"
	"github.com/claudioed/labor-performance/internal/domain/standard"
)

// GetStandard returns the currently-active LaborStandard for taskType, or
// ErrStandardNotFound if none is active.
type GetStandard struct {
	Standards ports.StandardRepo
}

func (uc *GetStandard) Execute(ctx context.Context, taskType shared.TaskType) (*standard.LaborStandard, error) {
	s, err := uc.Standards.FindCurrentlyActive(ctx, taskType)
	if err != nil {
		return nil, err
	}
	if s == nil {
		return nil, ErrStandardNotFound
	}
	return s, nil
}
